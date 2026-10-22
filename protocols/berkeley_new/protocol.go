package main

import (
	"fmt"
	"math"
	"net/rpc"
	"sort"
	"sync"
	"time"
)

// messages between nodes. the json tags are also the field names in the UI
// input forms, don't rename them casually.
type Empty struct{}

type PollReply struct {
	TimeMS     float64 `json:"timeMS"`
	Instance   string  `json:"instance"`   // boot id of the answering node
	Generation uint64  `json:"generation"` // bumps whenever the clock is reset/adjusted
}
type AdjustArgs struct {
	Coordinator     string  `json:"coordinator"`
	CoordinatorBoot string  `json:"coordinatorBoot"`
	Round           uint64  `json:"round"`
	Instance        string  `json:"instance"`
	Generation      uint64  `json:"generation"`
	DeltaMS         float64 `json:"deltaMS"` // RELATIVE correction, never an absolute time
}
type AdjustReply struct {
	Scheduled bool      `json:"scheduled"` // accepted and started, NOT finished
	Clock     ClockView `json:"clock"`
}
type ComputeArgs struct {
	OutlierThresholdMS float64 `json:"outlierThresholdMS"` // 0 means the 2000 default
}

// one row per node in the round worksheet. step 1 fills the measurements,
// step 2 included+delta, step 3 scheduled (or Error).
type Sample struct {
	Node       string  `json:"node"`
	TimeMS     float64 `json:"timeMS"`
	RTTMS      float64 `json:"rttMS"`
	OffsetMS   float64 `json:"offsetMS"` // peer clock minus our clock at receipt
	Instance   string  `json:"instance"`
	Generation uint64  `json:"generation"`
	Included   bool    `json:"included"`
	DeltaMS    float64 `json:"deltaMS"`
	Scheduled  bool    `json:"scheduled"`
	Error      string  `json:"error,omitempty"`
}

type Round struct {
	Number      uint64   `json:"number"`
	Phase       string   `json:"phase"` // idle | polled | computed | sent
	ThresholdMS float64  `json:"thresholdMS"`
	MedianMS    float64  `json:"medianMS"`
	MeanMS      float64  `json:"meanMS"`
	Samples     []Sample `json:"samples"`
}

type State struct {
	Node        string    `json:"node"`
	Role        string    `json:"role"`
	Coordinator string    `json:"coordinator"`
	Clock       ClockView `json:"clock"`
	Busy        bool      `json:"busy"`
	Phase       string    `json:"phase"`
	NextAction  string    `json:"nextAction"`
	Round       *Round    `json:"round"`
	LastApplied uint64    `json:"lastAppliedRound"`
	LastDeltaMS float64   `json:"lastDeltaMS"`
	LastError   string    `json:"lastError"`
}

const (
	phaseIdle     = "idle"
	phasePolled   = "polled"
	phaseComputed = "computed"
	phaseSent     = "sent"
)

func phaseOf(round *Round) string {
	if round == nil {
		return phaseIdle
	}
	return round.Phase
}

func nextActionFor(round *Round) string {
	switch phaseOf(round) {
	case phasePolled:
		return "Application.ComputeDeltas"
	case phaseComputed:
		return "Application.SendAdjustments"
	default:
		return "Application.PollPeers"
	}
}

// copy the worksheet before reporting it - a step may still be filling the
// original in while the report serializes it.
func cloneRound(round *Round) *Round {
	if round == nil {
		return nil
	}
	clone := *round
	clone.Samples = make([]Sample, len(round.Samples))
	copy(clone.Samples, round.Samples)
	return &clone
}

func (s *TimeService) stateLocked() State {
	role, next := "participant", ""
	if s.id == s.coordinator {
		role, next = "coordinator", nextActionFor(s.round)
	}
	return State{
		Node: s.id, Role: role, Coordinator: s.coordinator,
		Clock: s.clock.view(time.Now()), Busy: s.busy,
		Phase: phaseOf(s.round), NextAction: next, Round: cloneRound(s.round),
		LastApplied: s.lastApplied, LastDeltaMS: s.lastDeltaMS, LastError: s.lastError,
	}
}

// node-to-node rpcs, called by the coordinator during a round.

// Poll just reads the clock. but a clock still slewing is a moving target so
// we refuse and let the coordinator try again later.
func (s *TimeService) Poll(_ Empty, reply *PollReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.clock.pending(now) != 0 {
		return fmt.Errorf("clock is still slewing; retry in a later round")
	}
	*reply = PollReply{TimeMS: s.clock.read(now), Instance: s.instance, Generation: s.generation}
	return nil
}

// Adjust schedules the gradual correction. the guards all exist because a
// caller timeout does NOT cancel a net/rpc call and applying the same delta
// twice would drift the clock instead of fixing it.
func (s *TimeService) Adjust(args AdjustArgs, reply *AdjustReply) error {
	s.mu.Lock()
	// only our known coordinator, sane round and sane correction size
	if args.Coordinator != s.coordinator || args.CoordinatorBoot == "" || args.Round == 0 ||
		!finite(args.DeltaMS) || math.Abs(args.DeltaMS) > 120000 {
		s.mu.Unlock()
		return fmt.Errorf("invalid coordinator, round or correction")
	}
	// a restarted coordinator would restart round numbering, refuse it
	if s.coordinatorBoot != "" && s.coordinatorBoot != args.CoordinatorBoot {
		s.mu.Unlock()
		return fmt.Errorf("coordinator restarted; start a fresh experiment (no failover protocol)")
	}
	// rounds only move forward, never apply the same one twice
	if args.Round <= s.lastApplied {
		s.mu.Unlock()
		return fmt.Errorf("duplicate or stale adjustment for round %d", args.Round)
	}
	// the correction must match what THIS clock reported in step 1
	if args.Instance != s.instance || args.Generation != s.generation {
		s.mu.Unlock()
		return fmt.Errorf("clock changed since poll; the measured offset is stale")
	}
	// never overlap corrections, replacing one would drop the unpaid part
	now := time.Now()
	if s.clock.pending(now) != 0 {
		s.mu.Unlock()
		return fmt.Errorf("previous correction is still slewing")
	}
	s.clock.slew(args.DeltaMS, now)
	s.generation++
	s.coordinatorBoot, s.lastApplied, s.lastDeltaMS = args.CoordinatorBoot, args.Round, args.DeltaMS
	*reply = AdjustReply{Scheduled: true, Clock: s.clock.view(now)}
	st := s.stateLocked()
	s.mu.Unlock()
	return s.report(st)
}

// application actions, available on any node.

// Read reports the state. watch pendingMS drain while timeMS keeps rising.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	*reply = s.stateLocked()
	st := *reply
	s.mu.Unlock()
	return s.report(st)
}

// ConfigureClock is an EXTERNAL disturbance for experiments: it rebuilds the
// clock, may jump time and cancel a pending slew. the algorithm itself never
// does this, it only calls clock.slew.
func (a *Application) ConfigureClock(args ClockArgs, reply *State) error {
	if err := validateClock(args); err != nil {
		return err
	}
	s := a.service
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("a step is running right now; retry in a moment")
	}
	s.clock = newClock(time.Now(), args)
	s.generation++ // makes every older measurement stale
	s.lastDeltaMS = 0
	*reply = s.stateLocked()
	st := *reply
	s.mu.Unlock()
	return s.report(st)
}

// runPhase wraps one step: only the coordinator runs steps, only one at a
// time, and the outcome always gets reported.
func (s *TimeService) runPhase(name string, phase func() (*Round, error), reply *Round) error {
	s.mu.Lock()
	if s.id != s.coordinator {
		coordinator := s.coordinator
		s.mu.Unlock()
		return fmt.Errorf("%s runs on the coordinator %s; this is %s", name, coordinator, s.id)
	}
	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("another step is still running; wait for its result")
	}
	s.busy = true
	s.mu.Unlock()

	round, err := phase() // runs without the lock, rpc calls take seconds

	s.mu.Lock()
	s.busy = false
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
		*reply = *round
	}
	st := s.stateLocked()
	s.mu.Unlock()
	if reportErr := s.report(st); err == nil {
		err = reportErr
	}
	return err
}

// ---- step 1: poll every clock -------------------------------------------

func (s *TimeService) phase1PollPeers() (*Round, error) {
	s.mu.Lock()
	// the last round's correction may still be paying off here
	if pending := s.clock.pending(time.Now()); pending != 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("the correction from the last round is still slewing (%.0f ms left)", pending)
	}
	number := uint64(1)
	if s.round != nil {
		number = s.round.Number + 1
	}
	round := &Round{Number: number, Phase: phasePolled, Samples: make([]Sample, len(s.members))}
	// slot 0 is the coordinator itself: offset 0, no invented RTT. it's a
	// normal participant here, NOT a time authority.
	round.Samples[0] = Sample{Node: s.id, TimeMS: s.clock.read(time.Now()), Instance: s.instance, Generation: s.generation}
	members := s.members
	s.mu.Unlock()

	// poll everyone at once so one dead node costs one timeout, not N.
	// each goroutine owns its own slot so no locking needed here.
	var workers sync.WaitGroup
	for i := 1; i < len(members); i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			round.Samples[index] = s.pollOne(members[index])
		}(i)
	}
	workers.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.round = round
	s.lastError = ""
	return round, nil
}

// bounded rpc call. a call into a dead or blocked node must not hang the
// whole action - the platform gives us ~10s and then the command dies. NOTE:
// on timeout the reply must NOT be read, the decoder may still be writing it.
func call(client *rpc.Client, method string, args, reply any) error {
	done := make(chan error, 1)
	go func() { done <- client.Call(method, args, reply) }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		return fmt.Errorf("%s: no reply within 3s; remote outcome unknown", method)
	}
}

func (s *TimeService) pollOne(peer string) Sample {
	sample := Sample{Node: peer}
	client := s.peers[peer]
	if client == nil {
		sample.Error = "no RPC connection to " + peer
		return sample
	}
	start := time.Now()
	var reply PollReply
	err := call(client, "TimeService.Poll", Empty{}, &reply)
	end := time.Now()
	sample.RTTMS = end.Sub(start).Seconds() * 1000
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	s.mu.Lock()
	localAtReceipt := s.clock.read(end)
	s.mu.Unlock()
	// the peer read its clock while the request was in flight. assuming
	// symmetric paths that was at the midpoint, so
	// offset = peerTime + RTT/2 - ourTimeAtReceipt. same trick as Cristian.
	sample.TimeMS = reply.TimeMS
	sample.OffsetMS = reply.TimeMS + sample.RTTMS/2 - localAtReceipt
	sample.Instance, sample.Generation = reply.Instance, reply.Generation
	if !finite(sample.OffsetMS) {
		sample.Error = "non-finite clock offset"
	}
	return sample
}

// ---- step 2: median -> outliers -> mean -> deltas ------------------------

func (s *TimeService) phase2ComputeDeltas(thresholdMS float64) (*Round, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if phaseOf(s.round) != phasePolled {
		return nil, fmt.Errorf("step 2 needs the step-1 samples; run Application.PollPeers first (round is %s)", phaseOf(s.round))
	}
	median, mean, err := averageOffsets(s.round.Samples, thresholdMS)
	if err != nil {
		return nil, err // not enough trustworthy readings, leave every clock alone
	}
	s.round.Phase = phaseComputed
	s.round.ThresholdMS, s.round.MedianMS, s.round.MeanMS = thresholdMS, median, mean
	s.lastError = ""
	return s.round, nil
}

// worked example, offsets 0/100/200/10000 with threshold 300:
// median 150 -> keep 0/100/200 -> mean 100 -> deltas +100/0/-100/-9900.
// the outlier loses its VOTE but still gets the biggest correction.
func averageOffsets(samples []Sample, threshold float64) (median, mean float64, err error) {
	if !finite(threshold) || threshold <= 0 {
		return 0, 0, fmt.Errorf("outlier threshold must be positive and finite")
	}
	var offsets []float64
	for _, sample := range samples {
		if sample.Error == "" && finite(sample.OffsetMS) {
			offsets = append(offsets, sample.OffsetMS)
		}
	}
	if len(offsets) < 2 {
		return 0, 0, fmt.Errorf("need at least two responsive, stable clocks")
	}
	sort.Float64s(offsets)
	middle := len(offsets) / 2
	median = offsets[middle]
	if len(offsets)%2 == 0 {
		median = (offsets[middle-1] + offsets[middle]) / 2
	}
	count := 0
	for i := range samples {
		sample := &samples[i]
		sample.Included = sample.Error == "" && finite(sample.OffsetMS) && math.Abs(sample.OffsetMS-median) <= threshold
		if sample.Included {
			mean += sample.OffsetMS
			count++
		}
	}
	if count < 2 {
		return median, 0, fmt.Errorf("outlier filter retained fewer than two clocks; no correction")
	}
	mean /= float64(count)
	for i := range samples {
		if samples[i].Error == "" {
			samples[i].DeltaMS = mean - samples[i].OffsetMS // relative, not an absolute time
		}
	}
	return median, mean, nil
}

// ---- step 3: send the corrections ---------------------------------------

func (s *TimeService) phase3SendAdjustments() (*Round, error) {
	s.mu.Lock()
	if phaseOf(s.round) != phaseComputed {
		s.mu.Unlock()
		return nil, fmt.Errorf("step 3 needs the step-2 deltas; run Application.ComputeDeltas first (round is %s)", phaseOf(s.round))
	}
	round := s.round
	round.Phase = phaseSent
	request := AdjustArgs{Coordinator: s.id, CoordinatorBoot: s.instance, Round: round.Number}
	self := s.id
	s.mu.Unlock()

	// sends at once again, and results land per node - this is not atomic
	// broadcast, some nodes can succeed while others fail.
	var workers sync.WaitGroup
	for i := range round.Samples {
		if round.Samples[i].Error != "" {
			continue // failed in step 1, nothing measured to correct
		}
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			sample := &round.Samples[index]
			adjustment := request
			adjustment.Instance, adjustment.Generation, adjustment.DeltaMS = sample.Instance, sample.Generation, sample.DeltaMS
			var reply AdjustReply
			var err error
			if sample.Node == self {
				err = s.Adjust(adjustment, &reply) // our own clock needs no rpc
			} else {
				err = call(s.peers[sample.Node], "TimeService.Adjust", adjustment, &reply)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if err != nil {
				sample.Error = err.Error() // "unknown outcome" after a timeout = no confirmation
				return
			}
			sample.Scheduled = reply.Scheduled
		}(i)
	}
	workers.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = ""
	return round, nil
}

// the three steps you trigger.

func (a *Application) PollPeers(_ Empty, reply *Round) error {
	return a.service.runPhase("Application.PollPeers", a.service.phase1PollPeers, reply)
}

func (a *Application) ComputeDeltas(args ComputeArgs, reply *Round) error {
	threshold := args.OutlierThresholdMS
	if threshold == 0 {
		threshold = 2000
	}
	if !finite(threshold) || threshold <= 0 || threshold > 120000 {
		return fmt.Errorf("outlierThresholdMS must be in (0, 120000], or zero for the 2000 ms default")
	}
	return a.service.runPhase("Application.ComputeDeltas", func() (*Round, error) {
		return a.service.phase2ComputeDeltas(threshold)
	}, reply)
}

func (a *Application) SendAdjustments(_ Empty, reply *Round) error {
	return a.service.runPhase("Application.SendAdjustments", a.service.phase3SendAdjustments, reply)
}

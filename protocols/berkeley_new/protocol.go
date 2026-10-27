package main

import (
	"fmt"
	"math"
	"net/rpc"
	"sort"
	"sync"
	"time"
)

const (
	phaseIdle     = "idle"
	phasePolled   = "polled"
	phaseComputed = "computed"
	phaseSending  = "sending"
	phaseSent     = "sent"
	phasePartial  = "partial_failure"
)

func cloneRound(r *Round) *Round {
	if r == nil {
		return nil
	}

	cp := *r
	cp.Samples = append([]Sample(nil), r.Samples...)
	return &cp
}

// bounded rpc call so a dead/blocked node can't hang the action past the
// platform's ~10s command deadline. on timeout the reply must NOT be read.
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

type Empty struct{}

type PollReply struct {
	TimeMS     float64 `json:"timeMS"`
	Instance   string  `json:"instance"`
	Generation uint64  `json:"generation"`
}
type AdjustArgs struct {
	Coordinator     string  `json:"coordinator"`
	CoordinatorBoot string  `json:"coordinatorBoot"`
	Round           uint64  `json:"round"`
	Instance        string  `json:"instance"`
	Generation      uint64  `json:"generation"`
	DeltaMS         float64 `json:"deltaMS"` // RELATIVE correction
}
type AdjustReply struct {
	Scheduled bool      `json:"scheduled"`
	Clock     ClockView `json:"clock"`
}
type ComputeArgs struct {
	OutlierThresholdMS float64 `json:"outlierThresholdMS"`
}
type ModeArgs struct {
	Step bool `json:"step"` // true = jump immediately, false = smooth slew
}

// one row per node: step 1 fills the measurements, step 2 included+delta,
// step 3 scheduled (or Error).
type Sample struct {
	Node       string  `json:"node"`
	TimeMS     float64 `json:"timeMS"`
	RTTMS      float64 `json:"rttMS"`
	OffsetMS   float64 `json:"offsetMS"`
	Instance   string  `json:"instance"`
	Generation uint64  `json:"generation"`
	Included   bool    `json:"included"`
	DeltaMS    float64 `json:"deltaMS"`
	Scheduled  bool    `json:"scheduled"`
	Error      string  `json:"error,omitempty"`
}

type Round struct {
	Number      uint64   `json:"number"`
	Phase       string   `json:"phase"`
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
	StepMode    bool      `json:"stepMode"` // true = one-time jump, false = smooth slew
	Phase       string    `json:"phase"`
	Round       *Round    `json:"round"`
	LastApplied uint64    `json:"lastAppliedRound"`
	LastAdjMs   float64   `json:"lastAdjMs"` // size of the most recent correction
	LastError   string    `json:"lastError"`
}

func phaseOf(round *Round) string {
	if round == nil {
		return phaseIdle
	}
	return round.Phase
}

func (s *TimeService) stateLocked() State {
	role := "participant"
	if s.id == s.coordinator {
		role = "coordinator"
	}

	return State{
		Node:        s.id,
		Role:        role,
		Coordinator: s.coordinator,
		Clock:       s.clock.view(time.Now()),
		Busy:        s.busy,
		StepMode:    s.stepMode,
		Phase:       phaseOf(s.round),
		Round:       cloneRound(s.round),
		LastApplied: s.lastApplied,
		LastAdjMs:   s.lastDeltaMS,
		LastError:   s.lastError,
	}
}

// node-to-node rpcs.

// Poll just reads the clock, but a slewing clock is a moving target so we
// refuse and let the coordinator try again later.
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

// Adjust schedules the gradual correction. the guards exist because a caller
// timeout does NOT cancel a net/rpc call and applying the same delta twice
// would drift the clock instead of fixing it.
func (s *TimeService) Adjust(args AdjustArgs, reply *AdjustReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// sane input only
	if args.Coordinator != s.coordinator || args.CoordinatorBoot == "" || args.Round == 0 ||
		!finite(args.DeltaMS) || math.Abs(args.DeltaMS) > 120000 {
		return fmt.Errorf("invalid coordinator, round or correction")
	}
	// a restarted coordinator would restart round numbering, refuse it
	if s.coordinatorBoot != "" && s.coordinatorBoot != args.CoordinatorBoot {
		return fmt.Errorf("coordinator restarted; start a fresh experiment (no failover protocol)")
	}
	// no duplicate rounds, no stale measurements, no overlapping slews
	if args.Round <= s.lastApplied {
		return fmt.Errorf("duplicate or stale adjustment for round %d", args.Round)
	}
	if args.Instance != s.instance || args.Generation != s.generation {
		return fmt.Errorf("clock changed since poll; the measured offset is stale")
	}
	now := time.Now()
	if s.clock.pending(now) != 0 {
		return fmt.Errorf("previous correction is still slewing")
	}
	if s.stepMode {
		s.clock.step(args.DeltaMS, now) // jump (scenario C)
	} else {
		s.clock.slew(args.DeltaMS, now) // smooth (the algorithm's real behaviour)
	}
	s.generation++
	s.coordinatorBoot, s.lastApplied, s.lastDeltaMS = args.CoordinatorBoot, args.Round, args.DeltaMS
	*reply = AdjustReply{Scheduled: true, Clock: s.clock.view(now)}
	return s.report(s.stateLocked())
}

// application actions.

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}

func (a *Application) Sync(args ComputeArgs, reply *Round) error {
	threshold := args.OutlierThresholdMS
	if threshold == 0 {
		threshold = 2000
	}

	if !finite(threshold) || threshold <= 0 || threshold > 120000 {
		return fmt.Errorf(
			"outlierThresholdMS must be in (0, 120000], or zero for the 2000 ms default",
		)
	}

	s := a.service

	return s.runPhase("Application.Sync", func() (*Round, error) {
		// Step 1: poll all clocks.
		round, err := s.phase1PollPeers()
		if err != nil {
			return round, err
		}

		// Step 2: calculate median, filter outliers, compute mean and deltas.
		round, err = s.phase2ComputeDeltas(threshold)
		if err != nil {
			return nil, err
		}

		// Step 3: schedule gradual clock adjustments.
		return s.phase3SendAdjustments()
	}, reply)
}

// ConfigureClock is an external disturbance for experiments: it rebuilds the
// clock and may jump time / cancel a slew. the algorithm itself never jumps.
func (a *Application) ConfigureClock(args ClockArgs, reply *State) error {
	if err := validateClock(args); err != nil {
		return err
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("a step is running right now; retry in a moment")
	}
	s.clock = newClock(time.Now(), args)
	s.generation++ // makes every older measurement stale
	s.lastDeltaMS = 0
	*reply = s.stateLocked()
	return s.report(*reply)
}

// SetMode chooses how the NEXT corrections are applied: step=true jumps the
// clock immediately (bad: time can go backward), step=false slews gradually
// (the algorithm's real behaviour). Scenario C compares the two trajectories.
func (a *Application) SetMode(args ModeArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("a step is running right now; retry in a moment")
	}
	s.stepMode = args.Step
	s.lastError = ""
	*reply = s.stateLocked()
	return s.report(*reply)
}

func (s *TimeService) runPhase(
	name string,
	phase func() (*Round, error),
	reply *Round,
) error {
	s.mu.Lock()

	if s.id != s.coordinator {
		coordinator := s.coordinator
		s.mu.Unlock()
		return fmt.Errorf(
			"%s runs on coordinator %s; this is %s",
			name, coordinator, s.id,
		)
	}

	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("another operation is running; retry later")
	}

	s.busy = true
	s.mu.Unlock()

	round, operationErr := phase()

	s.mu.Lock()
	s.busy = false

	if round != nil {
		*reply = *cloneRound(round)
	}

	if operationErr != nil {
		s.lastError = operationErr.Error()
	} else {
		s.lastError = ""
	}

	state := s.stateLocked()
	reportErr := s.report(state)
	s.mu.Unlock()

	if operationErr != nil {
		return operationErr
	}
	if reportErr != nil {
		return fmt.Errorf(
			"operation completed but reporting state failed: %w",
			reportErr,
		)
	}

	return nil
}

func (s *TimeService) phase1PollPeers() (*Round, error) {
	s.mu.Lock()
	if pending := s.clock.pending(time.Now()); pending != 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("the correction from the last round is still slewing (%.0f ms left)", pending)
	}
	number := uint64(1)
	if s.round != nil {
		number = s.round.Number + 1
	}
	round := &Round{Number: number, Phase: phasePolled, Samples: make([]Sample, len(s.members))}
	// slot 0 is the coordinator itself: offset 0, no invented rtt. it is a
	// normal participant here, NOT a time authority.
	round.Samples[0] = Sample{Node: s.id, TimeMS: s.clock.read(time.Now()), Instance: s.instance, Generation: s.generation}
	members := s.members
	s.mu.Unlock()

	// poll everyone at once so one dead node costs one timeout, not N.
	// each goroutine owns its own slot.
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
	// the peer read its clock mid-flight; assuming symmetric paths that was at
	// the midpoint: offset = peerTime + RTT/2 - ourTimeAtReceipt. same trick
	// as cristian.
	sample.TimeMS = reply.TimeMS
	sample.OffsetMS = reply.TimeMS + sample.RTTMS/2 - localAtReceipt
	sample.Instance, sample.Generation = reply.Instance, reply.Generation
	if !finite(sample.OffsetMS) {
		sample.Error = "non-finite clock offset"
	}
	return sample
}

func (s *TimeService) phase2ComputeDeltas(thresholdMS float64) (*Round, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if phaseOf(s.round) != phasePolled {
		return nil, fmt.Errorf("step 2 needs the step-1 samples; the round is currently in phase %s", phaseOf(s.round))
	}
	median, mean, err := averageOffsets(s.round.Samples, thresholdMS)
	if err != nil {
		return nil, err
	}
	s.round.Phase = phaseComputed
	s.round.ThresholdMS, s.round.MedianMS, s.round.MeanMS = thresholdMS, median, mean
	s.lastError = ""
	return s.round, nil
}

// offsets 0/100/200/10000 with threshold 300: median 150 -> keep 0/100/200 ->
// mean 100 -> deltas +100/0/-100/-9900. the outlier loses its VOTE but still
// gets the biggest correction.
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
			samples[i].DeltaMS = mean - samples[i].OffsetMS
		}
	}
	return median, mean, nil
}

func (s *TimeService) phase3SendAdjustments() (*Round, error) {
	s.mu.Lock()

	if phaseOf(s.round) != phaseComputed {
		current := phaseOf(s.round)
		s.mu.Unlock()
		return nil, fmt.Errorf(
			"step 3 requires computed deltas; current phase is %s",
			current,
		)
	}

	round := s.round
	round.Phase = phaseSending

	request := AdjustArgs{
		Coordinator:     s.id,
		CoordinatorBoot: s.instance,
		Round:           round.Number,
	}
	self := s.id

	s.mu.Unlock()

	var workers sync.WaitGroup

	for i := range round.Samples {
		s.mu.Lock()
		sample := round.Samples[i]
		s.mu.Unlock()

		if sample.Error != "" {
			continue
		}

		workers.Add(1)

		go func(index int, sample Sample) {
			defer workers.Done()

			adjustment := request
			adjustment.Instance = sample.Instance
			adjustment.Generation = sample.Generation
			adjustment.DeltaMS = sample.DeltaMS

			var reply AdjustReply
			var err error

			if sample.Node == self {
				err = s.Adjust(adjustment, &reply)
			} else {
				client := s.peers[sample.Node]
				if client == nil {
					err = fmt.Errorf(
						"no RPC connection to %s",
						sample.Node,
					)
				} else {
					err = call(
						client,
						"TimeService.Adjust",
						adjustment,
						&reply,
					)
				}
			}

			s.mu.Lock()
			defer s.mu.Unlock()

			if err != nil {
				round.Samples[index].Error = err.Error()
				round.Samples[index].Scheduled = false
				return
			}

			if !reply.Scheduled {
				round.Samples[index].Error =
					"adjustment was not confirmed"
				round.Samples[index].Scheduled = false
				return
			}

			round.Samples[index].Scheduled = true
		}(i, sample)
	}

	workers.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	failures := 0
	for _, sample := range round.Samples {
		if sample.Error != "" || !sample.Scheduled {
			failures++
		}
	}

	if failures > 0 {
		round.Phase = phasePartial
		s.lastError = fmt.Sprintf(
			"%d of %d adjustments were not confirmed",
			failures,
			len(round.Samples),
		)
		return round, fmt.Errorf("%s", s.lastError)
	}

	round.Phase = phaseSent
	s.lastError = ""
	return round, nil
}

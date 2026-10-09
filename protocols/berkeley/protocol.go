package main

import (
	"crypto/rand" // Text creates an unpredictable boot identity; it is not clock synchronization.
	"fmt"
	"math"
	"net/rpc"
	"sort" // Float64s sorts a copy to find the median without rearranging node records.
	"sync"
	"time"

	lab "distvis/sdk/rpc"
)

// There is one fixed coordinator, node.Nodes[0]. The lecture mentions election
// as a possible response to failure; this CLOCK lesson does not implement an
// election protocol. See README for the explicit crash/restart boundary.
type Empty struct{}
type SyncArgs struct {
	OutlierThresholdMS float64 `json:"outlierThresholdMS"` // Distance from median; zero defaults to 2000.
}
type PollReply struct {
	TimeMS     float64 `json:"timeMS"`
	Instance   string  `json:"instance"`   // Boot identity distinguishes a restarted clock.
	Generation uint64  `json:"generation"` // Changes on ConfigureClock or adjustment.
}
type AdjustArgs struct {
	Coordinator     string  `json:"coordinator"`
	CoordinatorBoot string  `json:"coordinatorBoot"`
	Round           uint64  `json:"round"`
	Instance        string  `json:"instance"`
	Generation      uint64  `json:"generation"`
	DeltaMS         float64 `json:"deltaMS"` // Relative correction, NEVER an absolute target time.
}
type AdjustReply struct {
	Scheduled bool      `json:"scheduled"` // Scheduled is not the same as finished slewing.
	Clock     ClockView `json:"clock"`
}

// Each poll becomes one Sample. Included says whether it contributed to the
// mean; an outlier may still receive a correction, because a faulty clock also
// needs to be brought back toward the group. A failed poll cannot be corrected.
type Sample struct {
	Node       string  `json:"node"`
	OffsetMS   float64 `json:"offsetMS"` // Peer clock minus coordinator clock, estimated at receipt.
	RTTMS      float64 `json:"rttMS"`
	Included   bool    `json:"included"`
	DeltaMS    float64 `json:"deltaMS"`
	Scheduled  bool    `json:"scheduled"`
	Error      string  `json:"error,omitempty"`
	Instance   string  `json:"instance"`
	Generation uint64  `json:"generation"`
}
type Round struct {
	Number      uint64   `json:"number"`
	MedianMS    float64  `json:"medianMS"`
	MeanMS      float64  `json:"meanMS"`
	ThresholdMS float64  `json:"thresholdMS"`
	Samples     []Sample `json:"samples"`
}

// average implements a concrete outlier rule (the lecture leaves the rule
// open): retain finite successful offsets within threshold of their median,
// then average the survivors. The coordinator contributes offset ZERO as a
// normal participant; it is NOT an external time authority.
func average(samples []Sample, threshold float64) (median, mean float64, err error) {
	if !finite(threshold) || threshold <= 0 {
		return 0, 0, fmt.Errorf("outlier threshold must be positive and finite")
	}
	var offsets []float64 // nil slice; append can grow it as values are collected.
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
	if len(offsets)%2 == 0 { // % is the remainder operator: even length has two middle values.
		median = (offsets[middle-1] + offsets[middle]) / 2
	}
	count := 0
	for i := range samples {
		sample := &samples[i] // A pointer updates the slice element, not a range-loop copy.
		sample.Included = sample.Error == "" && finite(sample.OffsetMS) && math.Abs(sample.OffsetMS-median) <= threshold
		if sample.Included {
			mean += sample.OffsetMS
			count++
		}
	}
	if count < 2 {
		return median, 0, fmt.Errorf("outlier filter retained fewer than two clocks; no correction")
	}
	mean /= float64(count) // Explicit conversion: Go does not silently mix int and float64.
	for i := range samples {
		if samples[i].Error == "" {
			// If a peer is +300 ms ahead and the mean offset is +100 ms,
			// it needs 100 - 300 = -200 ms. The coordinator needs +100 ms.
			samples[i].DeltaMS = mean - samples[i].OffsetMS
		}
	}
	return median, mean, nil
}

type State struct {
	Node        string    `json:"node"`
	Role        string    `json:"role"`
	Coordinator string    `json:"coordinator"`
	Clock       ClockView `json:"clock"`
	Busy        bool      `json:"busy"`
	LastApplied uint64    `json:"lastAppliedRound"`
	LastDeltaMS float64   `json:"lastDeltaMS"`
	LastRound   *Round    `json:"lastRound"`
	LastError   string    `json:"lastError"`
}
type TimeService struct {
	mu              sync.Mutex
	clock           clock
	busy            bool
	generation      uint64
	round           uint64
	lastApplied     uint64
	lastDeltaMS     float64
	coordinatorBoot string
	lastRound       *Round
	lastError       string
	id              string
	coordinator     string
	instance        string
	members         []string
	peers           map[string]*rpc.Client
	report          func(any) error
}
type Application struct{ service *TimeService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	index := 0
	for i, id := range node.Nodes {
		if id == node.ID {
			index = i
		}
	}
	s := &TimeService{id: node.ID, coordinator: node.Nodes[0], instance: rand.Text(),
		members: node.Nodes, peers: peers, report: node.Report,
		clock: newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 750})}
	if err := node.RegisterName("TimeService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	return s.report(s.stateLocked())
}
func (s *TimeService) stateLocked() State {
	role := "participant"
	if s.id == s.coordinator {
		role = "coordinator"
	}
	return State{Node: s.id, Role: role, Coordinator: s.coordinator, Clock: s.clock.view(time.Now()),
		Busy: s.busy, LastApplied: s.lastApplied, LastDeltaMS: s.lastDeltaMS,
		LastRound: s.lastRound, LastError: s.lastError}
}
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}
func (a *Application) ConfigureClock(args ClockArgs, reply *State) error {
	if err := validateClock(args); err != nil {
		return err
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("wait for the coordinator's round to finish")
	}
	// This external test disturbance IS allowed to jump time and cancel an
	// old slew. The synchronization algorithm itself only calls clock.slew.
	s.clock = newClock(time.Now(), args)
	s.generation++
	s.lastRound, s.lastError, s.lastDeltaMS = nil, "", 0
	*reply = s.stateLocked()
	return s.report(*reply)
}
func (s *TimeService) Poll(_ Empty, reply *PollReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.clock.pending(now) != 0 {
		return fmt.Errorf("still slewing; retry in a later round")
	}
	*reply = PollReply{TimeMS: s.clock.read(now), Instance: s.instance, Generation: s.generation}
	return nil
}

// Adjust rejects stale, duplicate or invalid instructions. These guards are
// important because a caller timing out does not cancel a net/rpc method.
// We never automatically retry a non-idempotent relative correction.
func (s *TimeService) Adjust(args AdjustArgs, reply *AdjustReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if args.Coordinator != s.coordinator || args.CoordinatorBoot == "" || args.Round == 0 ||
		!finite(args.DeltaMS) || math.Abs(args.DeltaMS) > 120000 {
		return fmt.Errorf("invalid coordinator, round or correction")
	}
	if s.coordinatorBoot != "" && s.coordinatorBoot != args.CoordinatorBoot {
		return fmt.Errorf("coordinator restarted; start a fresh experiment (no failover protocol)")
	}
	if args.Instance != s.instance || args.Generation != s.generation || args.Round <= s.lastApplied {
		return fmt.Errorf("stale/duplicate adjustment or clock changed since poll")
	}
	now := time.Now()
	if s.clock.pending(now) != 0 {
		return fmt.Errorf("previous correction is still slewing")
	}
	s.clock.slew(args.DeltaMS, now)
	s.generation++
	s.coordinatorBoot, s.lastApplied, s.lastDeltaMS = args.CoordinatorBoot, args.Round, args.DeltaMS
	*reply = AdjustReply{Scheduled: true, Clock: s.clock.view(now)}
	return s.report(s.stateLocked())
}

func (s *TimeService) poll(peer string) Sample {
	s.mu.Lock()
	start := time.Now()
	s.mu.Unlock()
	response, err := callRPC[PollReply](s.peers[peer], "TimeService.Poll", Empty{})
	end := time.Now()
	out := Sample{Node: peer, RTTMS: end.Sub(start).Seconds() * 1000}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	s.mu.Lock()
	localAtReceipt := s.clock.read(end)
	s.mu.Unlock()
	// Convert each peer reading to an OFFSET relative to the coordinator.
	// Averaging raw timestamps sampled at different instants would be wrong.
	// Like Cristian, this assumes roughly symmetric paths and quick handlers.
	out.OffsetMS = response.TimeMS + out.RTTMS/2 - localAtReceipt
	out.Instance, out.Generation = response.Instance, response.Generation
	if !finite(out.OffsetMS) {
		out.Error = "non-finite clock offset"
	}
	return out
}

func (a *Application) Sync(args SyncArgs, reply *Round) (err error) {
	s := a.service
	if s.id != s.coordinator {
		return fmt.Errorf("run Sync on coordinator %s", s.coordinator)
	}
	threshold := args.OutlierThresholdMS
	if threshold == 0 {
		threshold = 2000
	}
	if !finite(threshold) || threshold <= 0 || threshold > 120000 {
		return fmt.Errorf("outlierThresholdMS must be in (0, 120000], or zero for default")
	}
	s.mu.Lock()
	if s.busy || s.clock.pending(time.Now()) != 0 {
		s.mu.Unlock()
		return fmt.Errorf("a round or gradual correction is still in progress")
	}
	s.busy = true
	s.round++
	round := Round{Number: s.round, ThresholdMS: threshold, Samples: make([]Sample, len(s.members))}
	// The first member is ourselves: no network call or synthetic RTT needed.
	round.Samples[0] = Sample{Node: s.id, Instance: s.instance, Generation: s.generation}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy, s.lastError = false, ""
		s.lastRound = &round
		if err != nil {
			s.lastError = err.Error()
		}
		if reportErr := s.report(s.stateLocked()); err == nil {
			err = reportErr
		}
	}()

	// Phase 1: poll peers concurrently. One unavailable peer costs at most one
	// timeout, not (number of peers) timeouts. Each goroutine owns its own slot.
	var workers sync.WaitGroup
	for i := 1; i < len(s.members); i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			round.Samples[index] = s.poll(s.members[index])
		}(i)
	}
	workers.Wait()
	round.MedianMS, round.MeanMS, err = average(round.Samples, threshold)
	if err != nil {
		return err // Not enough trustworthy readings: leave EVERY clock alone.
	}

	// Phase 2: issue relative corrections, including to the coordinator itself.
	// Each destination validates its poll generation before scheduling a slew.
	// This is not atomic broadcast/consensus: some nodes can succeed while
	// others fail. The result records per-node confirmation, never "all synced".
	for i := range round.Samples {
		if round.Samples[i].Error != "" {
			continue
		}
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			sample := &round.Samples[index]
			request := AdjustArgs{Coordinator: s.id, CoordinatorBoot: s.instance, Round: round.Number,
				Instance: sample.Instance, Generation: sample.Generation, DeltaMS: sample.DeltaMS}
			var response AdjustReply
			var callErr error
			if sample.Node == s.id {
				callErr = s.Adjust(request, &response) // Local function call; no self RPC.
			} else {
				response, callErr = callRPC[AdjustReply](s.peers[sample.Node], "TimeService.Adjust", request)
			}
			if callErr != nil {
				sample.Error = callErr.Error()
			} else {
				sample.Scheduled = response.Scheduled
			}
		}(i)
	}
	workers.Wait()
	*reply = round
	return nil // Read Samples[i].Scheduled/Error to see partial failures.
}

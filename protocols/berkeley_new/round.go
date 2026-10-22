package main

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"sync"
	"time"
)

// ─── The three steps of one round ────────────────────────────────────────────
// A Berkeley round is driven by you: one Application action per step.
//
//	step 1  Application.PollPeers       poll every clock        (read-only)
//	step 2  Application.ComputeDeltas   median -> mean -> deltas (read-only)
//	step 3  Application.SendAdjustments send the corrections    (changes clocks)
//
// Steps 1 and 2 may be repeated freely: no clock has been touched yet. Only
// step 3 commits, and it commits per node — this is not atomic broadcast, so
// some nodes can succeed while others fail. The worksheet reports what
// happened to each node individually.

// A round moves through four phases; State.NextAction points at the next step.
const (
	phaseIdle     = "idle"     // No worksheet yet.
	phasePolled   = "polled"   // Step 1 done: samples collected.
	phaseComputed = "computed" // Step 2 done: mean and deltas known.
	phaseSent     = "sent"     // Step 3 done: corrections scheduled.
)

func phaseOf(round *Round) string {
	if round == nil {
		return phaseIdle
	}
	return round.Phase
}

// nextActionFor tells the UI which step to run next, so the platform shows you
// where you are in the algorithm without memorizing the flow.
func nextActionFor(round *Round) string {
	switch phaseOf(round) {
	case phasePolled:
		return "Application.ComputeDeltas"
	case phaseComputed:
		return "Application.SendAdjustments"
	default: // idle or sent: start a fresh round once the corrections finished.
		return "Application.PollPeers"
	}
}

// cloneRound copies the worksheet so a step can work on it privately and
// publish it under the lock when it is done. A published worksheet is never
// mutated again; a Read during a step therefore always sees a consistent
// snapshot instead of half-filled columns.
func cloneRound(round *Round) *Round {
	if round == nil {
		return nil
	}
	clone := *round
	clone.Samples = slices.Clone(round.Samples)
	return &clone
}

// runPhase is the wrapper around every step: only the coordinator runs steps,
// only one at a time, and the outcome is always reported to the platform.
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

	// The step runs WITHOUT the lock: it makes RPC calls that may take seconds.
	round, err := phase()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
		*reply = *round
	}
	if reportErr := s.report(s.stateLocked()); err == nil {
		err = reportErr
	}
	return err
}

// ─── Step 1: POLL ────────────────────────────────────────────────────────────

// phase1PollPeers asks every node what time it thinks it is and turns each
// answer into an OFFSET relative to the coordinator. Nothing is decided yet
// and no clock changes: this step can be repeated freely.
func (s *TimeService) phase1PollPeers() (*Round, error) {
	s.mu.Lock()
	// The last round's correction may still be paid off on our own clock.
	// Polling mid-slew would measure a moving target, so wait for it to finish
	// (or reset a clock with ConfigureClock to prepare a new experiment).
	if pending := s.clock.pending(time.Now()); pending != 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("the correction from the last round is still slewing (%.0f ms left); wait for it to finish", pending)
	}
	number := uint64(1)
	if s.round != nil {
		number = s.round.Number + 1
	}
	round := &Round{Number: number, Phase: phasePolled, Samples: make([]Sample, len(s.members))}
	// Slot 0 is the coordinator itself: no network call and no RTT to invent
	// (its offset from itself is exactly zero), but it reports its own clock
	// reading like everyone else. The coordinator is a normal participant
	// here, NOT a time authority.
	round.Samples[0] = Sample{Node: s.id, TimeMS: s.clock.read(time.Now()),
		Instance: s.instance, Generation: s.generation}
	members := s.members
	s.mu.Unlock()

	// Poll every other node at the same time: one unreachable node then costs
	// at most one timeout, not one timeout per node. Each goroutine owns
	// exactly one slot of the samples slice, so no locking is needed here.
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
	s.round = round // Publish: from now on this worksheet is read-only.
	s.lastError = ""
	return round, nil
}

// pollOne asks one node for its clock and converts the answer into an offset.
// This is the only place where a clock reading meets the network, so it is
// worth reading slowly.
func (s *TimeService) pollOne(peer string) Sample {
	client := s.peers[peer]
	if client == nil {
		return Sample{Node: peer, Error: "no RPC connection to " + peer}
	}
	start := time.Now()
	response, err := callRPC[PollReply](client, "TimeService.Poll", Empty{})
	end := time.Now()
	sample := Sample{Node: peer, RTTMS: end.Sub(start).Seconds() * 1000}
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	s.mu.Lock()
	localAtReceipt := s.clock.read(end) // Our own clock the moment the answer arrived.
	s.mu.Unlock()
	// The peer read its clock while the request was in flight. Assuming roughly
	// symmetric paths, that happened at the midpoint of the round trip, so:
	//
	//	offset = peerClock - (our clock at the midpoint)
	//	       = peerClock + RTT/2 - ourClockAtReceipt
	//
	// Averaging raw timestamps sampled at different instants would be wrong:
	// the OFFSET is what the group must agree on. Same trick as Cristian's
	// algorithm, same assumption of symmetric paths and quick handlers.
	sample.TimeMS = response.TimeMS
	sample.OffsetMS = response.TimeMS + sample.RTTMS/2 - localAtReceipt
	sample.Instance, sample.Generation = response.Instance, response.Generation
	if !finite(sample.OffsetMS) {
		sample.Error = "non-finite clock offset"
	}
	return sample
}

// ─── Step 2: COMPUTE ─────────────────────────────────────────────────────────

// phase2ComputeDeltas is pure arithmetic on the step-1 samples: find the
// median offset, throw away the outliers, average the survivors, and give
// every node the difference between the group mean and its own offset. Still
// no clock changes, so a refused step 2 leaves the worksheet at "polled" and
// you can simply retry with a different threshold.
func (s *TimeService) phase2ComputeDeltas(thresholdMS float64) (*Round, error) {
	s.mu.Lock()
	round := cloneRound(s.round)
	s.mu.Unlock()

	if phaseOf(round) != phasePolled {
		return nil, fmt.Errorf("step 2 needs the step-1 samples; run Application.PollPeers first (round is %s)", phaseOf(round))
	}

	median, mean, err := averageOffsets(round.Samples, thresholdMS)
	if err != nil {
		return nil, err // Not enough trustworthy readings: leave EVERY clock alone.
	}
	round.Phase = phaseComputed
	round.ThresholdMS, round.MedianMS, round.MeanMS = thresholdMS, median, mean

	s.mu.Lock()
	defer s.mu.Unlock()
	s.round = round // Publish.
	s.lastError = ""
	return round, nil
}

// averageOffsets implements the one rule the lecture leaves open: which clocks
// to trust. Retain finite successful offsets within thresholdMS of their
// median, then average the survivors. The coordinator's own offset (zero) is
// one vote among many — the coordinator is NOT an external time authority.
//
// Worked example, offsets 0/100/200/10000 with threshold 300:
//
//	median 150 -> keep 0, 100, 200 -> mean 100
//	deltas: +100, 0, -100, -9900   (delta = mean - own offset)
//
// An outlier is excluded from VOTING, not from being repaired: the faulty
// clock needs the biggest correction of all.
func averageOffsets(samples []Sample, threshold float64) (median, mean float64, err error) {
	if !finite(threshold) || threshold <= 0 {
		return 0, 0, fmt.Errorf("outlier threshold must be positive and finite")
	}
	var offsets []float64 // nil slice; append grows it as values are collected.
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

// ─── Step 3: SEND ────────────────────────────────────────────────────────────

// phase3SendAdjustments is the first step that CHANGES anything. Every node
// that answered step 1 receives its relative correction and schedules a
// gradual slew. The coordinator adjusts its own clock too: it is a normal
// participant, not a time authority. Each destination validates the round and
// generation before accepting, and the result records per-node confirmation —
// never a promise that "everyone is synced".
func (s *TimeService) phase3SendAdjustments() (*Round, error) {
	s.mu.Lock()
	round := cloneRound(s.round)
	request := AdjustArgs{Coordinator: s.id, CoordinatorBoot: s.instance}
	self := s.id
	s.mu.Unlock()

	if phaseOf(round) != phaseComputed {
		return nil, fmt.Errorf("step 3 needs the step-2 deltas; run Application.ComputeDeltas first (round is %s)", phaseOf(round))
	}
	request.Round = round.Number
	round.Phase = phaseSent

	// Send to all nodes at the same time, again so one slow or broken node
	// costs one timeout instead of stalling the others.
	var workers sync.WaitGroup
	for i := range round.Samples {
		if round.Samples[i].Error != "" {
			continue // Failed in step 1: there is nothing measured to correct.
		}
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			sample := &round.Samples[index]
			adjustment := request
			adjustment.Instance, adjustment.Generation, adjustment.DeltaMS =
				sample.Instance, sample.Generation, sample.DeltaMS
			var response AdjustReply
			var err error
			if sample.Node == self {
				err = s.Adjust(adjustment, &response) // Our own clock needs no RPC.
			} else {
				response, err = callRPC[AdjustReply](s.peers[sample.Node], "TimeService.Adjust", adjustment)
			}
			if err != nil {
				// "Unknown outcome" after a timeout is reported as an error:
				// no confirmation, not proof that nothing happened.
				sample.Error = err.Error()
				return
			}
			sample.Scheduled = response.Scheduled
		}(i)
	}
	workers.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.round = round // Publish.
	s.lastError = ""
	return round, nil
}

// ─── The step actions you trigger ────────────────────────────────────────────

// PollPeers is STEP 1 of the round: poll every clock. Every call starts a
// fresh worksheet — a round that never reached step 3 simply disappears, and
// since steps 1-2 changed nothing, there is nothing to undo.
func (a *Application) PollPeers(_ Empty, reply *Round) error {
	return a.service.runPhase("Application.PollPeers", a.service.phase1PollPeers, reply)
}

// ComputeDeltas is STEP 2 of the round: filter the outliers and compute each
// node's correction. outlierThresholdMS is the maximum distance from the
// median that still counts as healthy; zero uses the 2000 ms default.
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

// SendAdjustments is STEP 3 of the round: send every node its correction. This
// is the only step that changes clocks, and the changes are gradual — read a
// node afterwards and watch clock.pendingMS drain toward zero.
func (a *Application) SendAdjustments(_ Empty, reply *Round) error {
	return a.service.runPhase("Application.SendAdjustments", a.service.phase3SendAdjustments, reply)
}

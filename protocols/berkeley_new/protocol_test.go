package main

import (
	"math"
	"net"
	"net/rpc"
	"strings"
	"testing"
	"time"
)

func TestStep2MeanMedianAndRelativeDeltas(t *testing.T) {
	// The coordinator itself is offset 0, two healthy clocks are at +100 and
	// +200, and one clock is broken at +10000.
	samples := []Sample{{OffsetMS: 0}, {OffsetMS: 100}, {OffsetMS: 200}, {OffsetMS: 10000}}
	median, mean, err := averageOffsets(samples, 300)
	if err != nil || median != 150 || mean != 100 {
		t.Fatalf("median=%v mean=%v err=%v", median, mean, err)
	}
	wantDelta := []float64{100, 0, -100, -9900}
	for i, sample := range samples {
		if sample.DeltaMS != wantDelta[i] || sample.Included != (i < 3) {
			t.Errorf("sample %d: %+v", i, sample)
		}
	}
	// Discarding an outlier's VOTE does not mean leaving its clock unrepaired:
	// the broken clock receives the largest correction of all (-9900 ms).
}

func TestStep2RefusesInsufficientEvidence(t *testing.T) {
	for _, samples := range [][]Sample{
		{{OffsetMS: 0}, {Error: "unreachable"}},
		{{OffsetMS: 0}, {OffsetMS: 10000}}, // Median 5000; threshold 10 keeps neither.
	} {
		if _, _, err := averageOffsets(samples, 10); err == nil {
			t.Fatal("expected a refusal rather than a fabricated mean")
		}
	}
}

func TestStepsRunInOrder(t *testing.T) {
	s := participant(0) // node-1 is the coordinator.
	app := &Application{service: s}
	var round Round

	// Steps 2 and 3 need the output of the previous step.
	if err := app.ComputeDeltas(ComputeArgs{OutlierThresholdMS: 500}, &round); err == nil {
		t.Fatal("step 2 without step-1 samples must be refused")
	}
	if err := app.SendAdjustments(Empty{}, &round); err == nil {
		t.Fatal("step 3 without step-2 deltas must be refused")
	}

	// No peers are wired up here, so step 1 records the coordinator's own
	// sample plus one error sample per missing peer ...
	if err := app.PollPeers(Empty{}, &round); err != nil {
		t.Fatal(err)
	}
	if round.Phase != phasePolled || len(round.Samples) != 3 {
		t.Fatalf("unexpected worksheet after step 1: %+v", round)
	}
	// ... and step 2 refuses: one responsive clock is not enough evidence to
	// decide anything, and a refusal must not advance the worksheet.
	if err := app.ComputeDeltas(ComputeArgs{OutlierThresholdMS: 500}, &round); err == nil {
		t.Fatal("expected refusal when only the coordinator itself answered")
	}
	if phaseOf(s.round) != phasePolled {
		t.Fatalf("a refused step 2 must keep the worksheet at %q, got %q", phasePolled, phaseOf(s.round))
	}
}

func TestClockSlewIsContinuousAndNeverBackwards(t *testing.T) {
	// A synthetic time origin makes this an exact, fast test: no sleeping.
	start := time.Unix(1000, 0)
	for _, delta := range []float64{-200, 200} {
		c := newClock(start, ClockArgs{OffsetMS: 300, DriftPPM: -10000})
		before := c.read(start)
		c.slew(delta, start)
		if c.read(start) != before {
			t.Fatal("scheduling a slew jumped the clock")
		}
		previous := before
		for tick := 1; tick <= 50; tick++ {
			now := start.Add(time.Duration(tick) * 100 * time.Millisecond)
			current := c.read(now)
			if current <= previous {
				t.Fatalf("clock moved backwards at tick %d: %v <= %v", tick, current, previous)
			}
			previous = current
		}
		end := start.Add(5 * time.Second)
		if c.pending(end) != 0 || math.Abs(c.applied(end)-delta) > 1e-9 {
			t.Fatalf("slew overshot or did not finish: %+v", c.view(end))
		}
	}
}

func TestConfigureClockValidation(t *testing.T) {
	if validateClock(ClockArgs{OffsetMS: math.Inf(1)}) == nil ||
		validateClock(ClockArgs{DriftPPM: -1000000}) == nil {
		t.Fatal("invalid clock setting would break the finite/forward-running assumptions")
	}
}

func participant(index int) *TimeService {
	members := []string{"node-1", "node-2", "node-3"}
	return &TimeService{
		id: members[index], coordinator: members[0], members: members,
		instance: members[index] + "-boot", peers: map[string]*rpc.Client{},
		clock:  newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 100}),
		report: func(any) error { return nil },
	}
}

func TestAdjustRejectsStaleDuplicateAndRestart(t *testing.T) {
	s := participant(1) // node-2: a participant the coordinator polled in step 1.
	request := AdjustArgs{Coordinator: "node-1", CoordinatorBoot: "original", Round: 1,
		Instance: s.instance, Generation: 0, DeltaMS: 100}
	if err := s.Adjust(request, &AdjustReply{}); err != nil {
		t.Fatal(err)
	}
	// Re-sending the same instruction must not pay the correction twice.
	err := s.Adjust(request, &AdjustReply{})
	if err == nil || !strings.Contains(err.Error(), "round") {
		t.Fatalf("duplicate round accepted or misreported: %v", err)
	}
	// A manual ConfigureClock bumps the generation: the step-1 measurement of
	// this clock is now stale and its correction must be refused.
	if err := (&Application{service: s}).ConfigureClock(ClockArgs{}, &State{}); err != nil {
		t.Fatal(err)
	}
	request.Round = 2
	err = s.Adjust(request, &AdjustReply{})
	if err == nil || !strings.Contains(err.Error(), "clock changed") {
		t.Fatalf("stale measurement accepted or misreported: %v", err)
	}
	// A restarted coordinator would restart round numbering from 1: refuse
	// instead of silently replaying old rounds.
	request.Generation = s.generation
	request.CoordinatorBoot = "restarted"
	err = s.Adjust(request, &AdjustReply{})
	if err == nil || !strings.Contains(err.Error(), "coordinator restarted") {
		t.Fatalf("coordinator restart accepted or misreported: %v", err)
	}
	// While a correction is still being paid off, the next one is refused
	// instead of silently replacing the unfinished part.
	request = AdjustArgs{Coordinator: "node-1", CoordinatorBoot: "original", Round: 3,
		Instance: s.instance, Generation: s.generation, DeltaMS: 50}
	if err := s.Adjust(request, &AdjustReply{}); err != nil {
		t.Fatal(err)
	}
	request.Round, request.Generation = 4, s.generation
	err = s.Adjust(request, &AdjustReply{})
	if err == nil || !strings.Contains(err.Error(), "slewing") {
		t.Fatalf("overlapping correction accepted or misreported: %v", err)
	}
}

func TestFullRoundOverRealRPC(t *testing.T) {
	coordinator := participant(0)
	peers := []*TimeService{}
	for i := 1; i < 3; i++ {
		peer := participant(i)
		server := rpc.NewServer()
		if err := server.RegisterName("TimeService", peer); err != nil {
			t.Fatal(err)
		}
		local, remote := net.Pipe()
		client := rpc.NewClient(local)
		coordinator.peers[peer.id] = client
		go server.ServeConn(remote)
		t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
		peers = append(peers, peer)
	}
	app := &Application{service: coordinator}

	// Step 1: every answer becomes an offset relative to the coordinator.
	// The clocks were built at +0, +100, +200 ms.
	var round Round
	if err := app.PollPeers(Empty{}, &round); err != nil {
		t.Fatal(err)
	}
	if round.Phase != phasePolled {
		t.Fatalf("phase = %q", round.Phase)
	}
	for i, sample := range round.Samples {
		if sample.Error != "" {
			t.Fatalf("poll failed for %s: %s", sample.Node, sample.Error)
		}
		if sample.TimeMS == 0 {
			t.Errorf("%s: the sample must report a real clock reading", sample.Node)
		}
		if want := float64(i) * 100; math.Abs(sample.OffsetMS-want) > 1 {
			t.Errorf("%s: offset = %v, want ~%v", sample.Node, sample.OffsetMS, want)
		}
	}

	// Step 2: the mean of 0/100/200 is 100, and every delta is mean minus the
	// node's own offset — a relative correction, never an absolute target time.
	if err := app.ComputeDeltas(ComputeArgs{OutlierThresholdMS: 500}, &round); err != nil {
		t.Fatal(err)
	}
	if round.Phase != phaseComputed || math.Abs(round.MeanMS-100) > 1 {
		t.Fatalf("mean = %v, phase = %q", round.MeanMS, round.Phase)
	}

	// Step 3: every node schedules its correction, the coordinator included.
	if err := app.SendAdjustments(Empty{}, &round); err != nil {
		t.Fatal(err)
	}
	if round.Phase != phaseSent {
		t.Fatalf("phase = %q", round.Phase)
	}
	for i, sample := range round.Samples {
		if !sample.Scheduled || sample.Error != "" {
			t.Errorf("node was not scheduled: %+v", sample)
		}
		if math.Abs(sample.DeltaMS-(round.MeanMS-sample.OffsetMS)) > 1e-9 {
			t.Errorf("wrong delta for %s: %v", sample.Node, sample.DeltaMS)
		}
		node := coordinator
		if i > 0 {
			node = peers[i-1]
		}
		if node.lastApplied != round.Number || node.lastDeltaMS != sample.DeltaMS {
			t.Errorf("%s did not record the correction", node.id)
		}
	}
	if coordinator.busy {
		t.Fatal("the step did not release its busy guard")
	}
	// The coordinator's own correction (+100 ms) is now being paid off ...
	if coordinator.clock.pending(time.Now()) <= 0 {
		t.Fatal("the coordinator should be slewing its own correction")
	}
	// ... which is why the next round is refused until the slew has finished.
	err := app.PollPeers(Empty{}, &round)
	if err == nil || !strings.Contains(err.Error(), "slewing") {
		t.Fatalf("expected refusal while a correction is slewing, got %v", err)
	}
}

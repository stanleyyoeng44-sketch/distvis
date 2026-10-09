package main

import (
	"math"
	"net"
	"net/rpc"
	"testing"
	"time"
)

func TestFilteredMeanAndRelativeDeltas(t *testing.T) {
	// Coordinator=0, two healthy clocks=100 and 200, outlier=10000.
	// Median=150; a 300-ms threshold keeps 0/100/200. Mean=100.
	samples := []Sample{{OffsetMS: 0}, {OffsetMS: 100}, {OffsetMS: 200}, {OffsetMS: 10000}}
	median, mean, err := average(samples, 300)
	if err != nil || median != 150 || mean != 100 {
		t.Fatalf("median=%v mean=%v err=%v", median, mean, err)
	}
	wantDelta := []float64{100, 0, -100, -9900}
	for i, sample := range samples {
		if sample.DeltaMS != wantDelta[i] || sample.Included != (i < 3) {
			t.Errorf("sample %d: %+v", i, sample)
		}
	}
	// Discarding an outlier's vote does NOT mean leaving its clock unrepaired.
}
func TestFilterRefusesInsufficientEvidence(t *testing.T) {
	for _, samples := range [][]Sample{
		{{OffsetMS: 0}, {Error: "unreachable"}},
		{{OffsetMS: 0}, {OffsetMS: 10000}}, // Median is 5000; threshold 10 retains neither.
	} {
		if _, _, err := average(samples, 10); err == nil {
			t.Fatal("expected refusal rather than a fabricated mean")
		}
	}
}
func TestSlewIsContinuousAndNeverBackwards(t *testing.T) {
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
func TestConfigurationValidation(t *testing.T) {
	if validateClock(ClockArgs{OffsetMS: math.Inf(1)}) == nil ||
		validateClock(ClockArgs{DriftPPM: -1000000}) == nil {
		t.Fatal("invalid clock setting would break finite/forward-running assumptions")
	}
}

func participant(index int) *TimeService {
	members := []string{"node-1", "node-2", "node-3"}
	return &TimeService{id: members[index], coordinator: members[0], members: members,
		instance: members[index] + "-boot", peers: map[string]*rpc.Client{},
		clock:  newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 100}),
		report: func(any) error { return nil }}
}
func TestAdjustRejectsStaleDuplicateAndNewCoordinator(t *testing.T) {
	s := participant(1)
	request := AdjustArgs{Coordinator: "node-1", CoordinatorBoot: "original", Round: 1,
		Instance: s.instance, Generation: 0, DeltaMS: 100}
	if err := s.Adjust(request, &AdjustReply{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Adjust(request, &AdjustReply{}); err == nil {
		t.Fatal("duplicate delta must not be applied twice")
	}
	// A manual configuration increments generation; the previous poll is stale.
	if err := (&Application{service: s}).ConfigureClock(ClockArgs{}, &State{}); err != nil {
		t.Fatal(err)
	}
	request.Round = 2
	if err := s.Adjust(request, &AdjustReply{}); err == nil {
		t.Fatal("accepted an adjustment based on the pre-configuration clock")
	}
	request.Generation = s.generation
	request.CoordinatorBoot = "restarted"
	if err := s.Adjust(request, &AdjustReply{}); err == nil {
		t.Fatal("coordinator restart must not silently reset round ordering")
	}
}
func TestRealRPCPollingAndScheduling(t *testing.T) {
	master := participant(0)
	for i := 1; i < 3; i++ {
		peer := participant(i)
		server := rpc.NewServer()
		if err := server.RegisterName("TimeService", peer); err != nil {
			t.Fatal(err)
		}
		local, remote := net.Pipe()
		client := rpc.NewClient(local)
		master.peers[peer.id] = client
		go server.ServeConn(remote)
		t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
	}
	var round Round
	if err := (&Application{service: master}).Sync(SyncArgs{OutlierThresholdMS: 500}, &round); err != nil {
		t.Fatal(err)
	}
	for _, sample := range round.Samples {
		if !sample.Included || !sample.Scheduled || sample.Error != "" {
			t.Errorf("node was not included and scheduled: %+v", sample)
		}
		if math.Abs(sample.DeltaMS-(round.MeanMS-sample.OffsetMS)) > 1e-9 {
			t.Fatal("sent absolute times or used wrong delta sign")
		}
	}
	if master.busy {
		t.Fatal("round did not release its busy guard")
	}
}

package main

// Files ending in _test.go are compiled by `go test`, not by `go build`.
// Using package main lets these tests call unexported algorithm helpers without
// making implementation details public solely for testing.
import (
	"fmt"
	"math"
	"net" // Pipe creates two connected in-memory endpoints; it does not use TCP ports.
	"net/rpc"
	"sync"
	"testing" // TestXxx(*testing.T) functions are discovered automatically.
	"time"
)

func TestEstimateAndUncertainty(t *testing.T) {
	got, err := estimate(1000, 700, 100, 10)
	if err != nil {
		t.Fatal(err) // Stop this test immediately; there is no useful result to inspect.
	}
	if got.EstimatedMS != 1050 || got.CorrectionMS != 350 || got.UncertaintyMS != 40 ||
		got.EarliestMS != 1010 || got.LatestMS != 1090 {
		t.Fatalf("incorrect RTT midpoint or bounds: %+v", got) // %+v prints field names too.
	}
}

func TestInvalidMeasurements(t *testing.T) {
	// Table-driven tests run many related cases without duplicating the body.
	cases := []struct {
		name               string
		s, b, rtt, minimum float64
	}{
		{"negative RTT", 1, 1, -1, 0},
		{"negative bound", 1, 1, 10, -1},
		{"impossible bound", 1, 1, 10, 6},
		{"NaN", math.NaN(), 1, 10, 0},
		{"infinite RTT", 1, 1, math.Inf(1), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := estimate(tc.s, tc.b, tc.rtt, tc.minimum); err == nil {
				t.Fatal("expected invalid sample to be rejected")
			}
		})
	}
}

func TestAsymmetricPathStillHasError(t *testing.T) {
	// True time starts at 1000. Forward transit=20, reverse transit=180.
	// Server stamps 1020; true time at reply receipt is 1200. RTT/2 cannot
	// discover this asymmetry: its midpoint is 1120, eighty milliseconds low.
	got, err := estimate(1020, 900, 200, 0)
	if err != nil || got.EstimatedMS != 1120 || math.Abs(got.EstimatedMS-1200) > got.UncertaintyMS {
		t.Fatalf("asymmetry/bound calculation: %+v, %v", got, err)
	}
}

func TestSoftwareClockDriftAndStep(t *testing.T) {
	start := time.Unix(1000, 0)
	c := newClock(start, ClockArgs{OffsetMS: 100, DriftPPM: 1000})
	later := start.Add(10 * time.Second)
	// Ten seconds at +1000 ppm gains ten milliseconds, on top of initial skew.
	if got := c.read(later) - hostMS(later); math.Abs(got-110) > 1e-6 {
		t.Fatalf("offset+drift = %v, want 110", got)
	}
	c.step(-110, later)
	if got := c.read(later) - hostMS(later); math.Abs(got) > 1e-6 {
		t.Fatalf("step left offset %v", got)
	}
	if c.driftPPM != 1000 {
		t.Fatal("offset correction must not silently remove oscillator drift")
	}
	if validateClock(ClockArgs{DriftPPM: math.NaN()}) == nil {
		t.Fatal("NaN drift accepted")
	}
}

// RejectingTime is a real net/rpc service used ONLY in tests. It exercises
// encoding, asynchronous completion and failure cleanup, not a fake network
// simulator. Real DistVis/Docker execution is checked separately.
type RejectingTime struct{}

func (*RejectingTime) Time(Empty, *TimeReply) error { return fmt.Errorf("test server unavailable") }

func TestFailedRPCDoesNotAdjustClock(t *testing.T) {
	server := rpc.NewServer()
	if err := server.RegisterName("TimeService", &RejectingTime{}); err != nil {
		t.Fatal(err)
	}
	local, remote := net.Pipe()
	client := rpc.NewClient(local)
	go server.ServeConn(remote)
	t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
	s := &TimeService{id: "node-2", reference: "node-1", peers: map[string]*rpc.Client{"node-1": client},
		clock: newClock(time.Now(), ClockArgs{OffsetMS: 750}), report: func(any) error { return nil }}
	before := s.clock
	err := (&Application{service: s}).Sync(SyncArgs{}, &Measurement{})
	if err == nil || s.busy || s.last != nil || s.lastError == "" || s.clock != before {
		t.Fatalf("failure modified clock or failed cleanup: err=%v state=%+v", err, s.stateLocked())
	}
}

func TestConcurrentReadsAndConfiguration(t *testing.T) {
	s := &TimeService{id: "node-2", reference: "node-1", clock: newClock(time.Now(), ClockArgs{}),
		report: func(any) error { return nil }}
	app := &Application{service: s}
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(offset int) {
			defer workers.Done()
			if err := app.ConfigureClock(ClockArgs{OffsetMS: float64(offset)}, &State{}); err != nil {
				t.Error(err)
			}
			if err := app.Read(Empty{}, &State{}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	workers.Wait() // `go test -race` checks these overlapping service calls.
}

package main

import (
	"distvis/sdk"
	lab "distvis/sdk/rpc"
	"math"
	"strings"
	"testing"
	"time"
)

type checkedWriter func([]byte) (int, error)

func (w checkedWriter) Write(p []byte) (int, error) { return w(p) }

func TestPublishHoldsStateLock(t *testing.T) {
	n := &node{clock: newClock(time.Now(), 0, 0)}
	writes := 0
	output := checkedWriter(func(p []byte) (int, error) {
		writes++
		if n.mu.TryLock() {
			n.mu.Unlock()
			t.Error("report snapshot was unlocked before publication")
		}
		return len(p), nil
	})
	transport, err := sdk.OpenStreams(strings.NewReader("{\"type\":\"init\",\"node\":\"node-1\",\"nodes\":[\"node-1\",\"node-2\"],\"protocol\":\"custom\"}\n"), output)
	if err != nil {
		t.Fatal(err)
	}
	n.rt = &lab.Runtime{Node: transport}
	n.publish()
	if writes == 0 {
		t.Fatal("no report written")
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestEstimateModes(t *testing.T) {
	// Client sends at 1000, server answers 5000, reply arrives at 1200 (RTT 200).
	cases := []struct {
		mode               string
		est, oneWay, bound float64
	}{
		{ModeHalf, 5100, 100, 100},
		{ModeZero, 5000, 0, 200},
		{ModeFull, 5200, 200, 200},
	}
	for _, c := range cases {
		est, oneWay, bound, err := estimate(c.mode, 1000, 5000, 1200)
		if err != nil || !near(est, c.est) || !near(oneWay, c.oneWay) || !near(bound, c.bound) {
			t.Errorf("%s: got est=%v oneWay=%v bound=%v err=%v", c.mode, est, oneWay, bound, err)
		}
	}
	if _, _, _, err := estimate("bogus", 0, 0, 0); err == nil {
		t.Error("unknown mode accepted")
	}
}

// Asymmetric path: request takes 2000 ms, reply 200 ms. The true time at t1 is
// ts+200, Cristian assumes ts+1100, so the client ends up 900 ms ahead.
func TestAsymmetricError(t *testing.T) {
	trueOffset := 0.0 // client clock already correct
	t0 := 0.0
	ts := t0 + 2000 + trueOffset
	t1 := t0 + 2200
	est, _, bound, _ := estimate(ModeHalf, t0, ts, t1)
	trueAtT1 := ts + 200
	if got := est - trueAtT1; !near(got, 900) || got > bound {
		t.Fatalf("error=%v bound=%v, want 900 within bound", got, bound)
	}
}

func TestClockDriftAndStep(t *testing.T) {
	start := time.Now()
	c := newClock(start, 750, 1000) // +750 ms, gains 1 ms per second
	later := start.Add(10 * time.Second)
	if e := c.errorMs(later); math.Abs(e-760) > 0.01 {
		t.Fatalf("after 10 s error=%v, want 760", e)
	}
	c.step(later, -760)
	if e := c.errorMs(later); math.Abs(e) > 0.01 {
		t.Fatalf("after step error=%v, want 0", e)
	}
	if e := c.errorMs(later.Add(5 * time.Second)); math.Abs(e-5) > 0.01 {
		t.Fatalf("drift after step error=%v, want 5", e)
	}
}

func TestInitialClocksDiffer(t *testing.T) {
	if o, d := initialClock("node-1"); o != 0 || d != 0 {
		t.Fatalf("server must be the reference, got %v %v", o, d)
	}
	seen := map[float64]bool{}
	for _, id := range []string{"node-2", "node-3", "node-4", "node-5"} {
		o, _ := initialClock(id)
		if o == 0 || seen[o] {
			t.Fatalf("%s offset %v not distinct/non-zero", id, o)
		}
		seen[o] = true
	}
}

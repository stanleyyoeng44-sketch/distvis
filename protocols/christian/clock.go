package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Clock is a SIMULATED wall clock, not the operating system clock.
// Its reading is baseMs plus the real time elapsed since anchor, sped up or
// slowed down by driftPPM. All containers share the host's real clock, which
// is what lets us measure each simulated clock's error (errorMs) at all.
type Clock struct {
	anchor   time.Time // real instant at which the clock read baseMs
	baseMs   float64   // clock reading (Unix milliseconds) at anchor
	driftPPM float64   // parts per million: +1000 = gains 1 ms per real second
}

// newClock starts a clock that is offsetMs away from real time and drifts at driftPPM.
func newClock(now time.Time, offsetMs, driftPPM float64) Clock {
	return Clock{anchor: now, baseMs: realMs(now) + offsetMs, driftPPM: driftPPM}
}

// readAt returns this clock's reading (Unix ms) at the real instant now.
// time.Now() values carry a monotonic reading, so now.Sub(anchor) is not
// disturbed if the host adjusts its wall clock during a run.
func (c *Clock) readAt(now time.Time) float64 {
	elapsedMs := float64(now.Sub(c.anchor).Microseconds()) / 1000
	return c.baseMs + elapsedMs*(1+c.driftPPM/1e6)
}

// step adds deltaMs to the clock at the instant now (an abrupt jump, as in
// textbook Cristian; a negative delta moves the clock backwards).
func (c *Clock) step(now time.Time, deltaMs float64) {
	c.baseMs = c.readAt(now) + deltaMs
	c.anchor = now
}

// errorMs is how far this clock is from real time (positive = clock ahead).
// It is an OBSERVER's diagnostic for the experiment; the algorithm never uses it.
func (c *Clock) errorMs(now time.Time) float64 {
	return c.readAt(now) - realMs(now)
}

func realMs(t time.Time) float64 { return float64(t.UnixMicro()) / 1000 }

// initialClock gives every node a different, clearly visible starting error.
// node-1 (the time server) is the accurate reference: offset 0, no drift.
//
//	node-2: +750 ms, +1000 ppm   node-3: -1200 ms, -2000 ppm
//	node-4: +1650 ms, +3000 ppm  node-5: -2100 ms, -1000 ppm  ...
func initialClock(id string) (offsetMs, driftPPM float64) {
	k := nodeNumber(id)
	if k <= 1 {
		return 0, 0
	}
	sign := 1.0
	if k%2 == 1 {
		sign = -1
	}
	return sign * float64(300+450*(k-1)), sign * float64(1000*((k-2)%3+1))
}

// nodeNumber returns 3 for "node-3"; 0 if the id has no numeric suffix.
func nodeNumber(id string) int {
	n, err := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
	if err != nil {
		return 0
	}
	return n
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func validateClock(offsetMs, driftPPM float64) error {
	if !finite(offsetMs) || math.Abs(offsetMs) > 60000 {
		return fmt.Errorf("offsetMs must be between -60000 and 60000")
	}
	if !finite(driftPPM) || math.Abs(driftPPM) > 10000 {
		return fmt.Errorf("driftPPM must be between -10000 and 10000")
	}
	return nil
}

// round1 rounds to 0.1 ms so reported numbers stay readable.
func round1(x float64) float64 { return math.Round(x*10) / 10 }

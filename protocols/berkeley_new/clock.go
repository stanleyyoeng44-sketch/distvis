package main

import (
	"fmt"
	"math"
	"time"
)

// ─── The software clock ──────────────────────────────────────────────────────
// Every node runs its own clock. Berkeley does not STEP a clock onto an
// absolute time: a pending correction is paid off GRADUALLY by adding (or
// subtracting) a small fraction of every elapsed millisecond. Negative
// corrections slow the clock down; they never assign an earlier timestamp, so
// time never runs backwards. All access is protected by TimeService.mu.

// A 10% rate change makes convergence visible in seconds rather than hours.
// This is deliberately much larger than a production clock discipline rate.
// With drift bounded to +/-1%, even a negative slew runs at least 89% as fast
// as real elapsed time: 1 - 0.01 - 0.10 > 0, so time cannot run backwards.
const slewRate = 0.10

type clock struct {
	anchor     time.Time // The instant the numbers below were fixed.
	baseMS     float64   // Clock reading at anchor, before any correction.
	driftPPM   float64   // Rate error in parts per million.
	correction float64   // Total correction scheduled at anchor, not the remaining amount.
}

// ClockArgs is what the ConfigureClock action accepts.
type ClockArgs struct {
	OffsetMS float64 `json:"offsetMS"` // Offset from the host clock, in ms.
	DriftPPM float64 `json:"driftPPM"` // Rate error in parts per million.
}

// ClockView is how the platform shows a clock.
type ClockView struct {
	TimeMS           float64 `json:"timeMS"`
	HostOffsetMS     float64 `json:"hostOffsetMS"` // Observer diagnostic, never used to synchronize.
	DriftPPM         float64 `json:"driftPPM"`
	PendingMS        float64 `json:"pendingMS"` // Correction still to be paid off.
	SlewRateFraction float64 `json:"slewRateFraction"`
}

// These math helpers reject NaN/infinity, which would otherwise poison every
// timestamp or slip past ordinary comparison-based input validation.
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func validateClock(args ClockArgs) error {
	if !finite(args.OffsetMS) || math.Abs(args.OffsetMS) > 60000 {
		return fmt.Errorf("offsetMS must be finite and between -60000 and 60000")
	}
	if !finite(args.DriftPPM) || math.Abs(args.DriftPPM) > 10000 {
		return fmt.Errorf("driftPPM must be finite and between -10000 and 10000")
	}
	return nil
}

func hostMS(now time.Time) float64 { return float64(now.UnixMilli()) }

// newClock builds a clock that reads the host clock plus offsetMS right now.
func newClock(now time.Time, args ClockArgs) clock {
	return clock{anchor: now, baseMS: hostMS(now) + args.OffsetMS, driftPPM: args.DriftPPM}
}

// applied is a pure calculation, not a mutating tick. Time.Sub uses the
// monotonic part of time.Now, so OS wall-clock changes do not change elapsed
// time. math.Min prevents overshooting the requested correction. Copysign
// restores its sign after we have calculated a nonnegative magnitude.
func (c *clock) applied(now time.Time) float64 {
	elapsedMS := math.Max(0, now.Sub(c.anchor).Seconds()*1000)
	magnitude := math.Min(math.Abs(c.correction), elapsedMS*slewRate)
	return math.Copysign(magnitude, c.correction)
}

// read is the current software time: the value at anchor, plus elapsed time
// scaled by the drift rate, plus the part of the correction paid off so far.
func (c *clock) read(now time.Time) float64 {
	elapsedMS := now.Sub(c.anchor).Seconds() * 1000
	return c.baseMS + elapsedMS*(1+c.driftPPM/1e6) + c.applied(now)
}

// pending is how much of the scheduled correction is still missing.
func (c *clock) pending(now time.Time) float64 { return c.correction - c.applied(now) }

// slew rebases at the current value, preserving continuity at this instant:
// read() returns the same number just before and just after slew. The callers
// refuse new rounds while a correction remains; replacing a pending correction
// would otherwise quietly discard the part not paid off yet.
func (c *clock) slew(deltaMS float64, now time.Time) {
	c.baseMS = c.read(now)
	c.anchor, c.correction = now, deltaMS
}

func (c *clock) view(now time.Time) ClockView {
	value := c.read(now)
	return ClockView{TimeMS: value, HostOffsetMS: value - hostMS(now), DriftPPM: c.driftPPM,
		PendingMS: c.pending(now), SlewRateFraction: slewRate}
}

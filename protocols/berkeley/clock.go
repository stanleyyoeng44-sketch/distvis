package main

import (
	"fmt"
	"math"
	"time"
)

// Berkeley's clock differs from the step-adjusted Cristian/NTP clock. A pending
// correction is paid off gradually by adding/subtracting a small fraction of
// each elapsed millisecond. Negative corrections slow the clock; they never
// assign an earlier timestamp. All access is protected by TimeService.mu.
type clock struct {
	anchor     time.Time
	baseMS     float64
	driftPPM   float64
	correction float64 // Total correction scheduled at anchor, not the remaining amount.
}

// A 10% rate change makes convergence visible in seconds rather than hours.
// This is deliberately much larger than a production clock discipline rate.
// With drift bounded to +/-1%, even a negative slew runs at least 89% as fast
// as real elapsed time: 1 - 0.01 - 0.10 > 0, so time cannot run backwards.
const slewRate = 0.10

type ClockArgs struct {
	OffsetMS float64 `json:"offsetMS"`
	DriftPPM float64 `json:"driftPPM"`
}
type ClockView struct {
	TimeMS           float64 `json:"timeMS"`
	HostOffsetMS     float64 `json:"hostOffsetMS"` // Observer diagnostic, never used to synchronize.
	DriftPPM         float64 `json:"driftPPM"`
	PendingMS        float64 `json:"pendingMS"`
	SlewRateFraction float64 `json:"slewRateFraction"`
}

// These math functions reject NaN/infinity, which would otherwise poison every
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
func (c *clock) read(now time.Time) float64 {
	elapsedMS := now.Sub(c.anchor).Seconds() * 1000
	return c.baseMS + elapsedMS*(1+c.driftPPM/1e6) + c.applied(now)
}
func (c *clock) pending(now time.Time) float64 { return c.correction - c.applied(now) }

// slew rebases at the current value, preserving continuity at this instant.
// The caller rejects new rounds while a correction remains; otherwise replacing
// a pending correction could quietly discard part of a previous round.
func (c *clock) slew(deltaMS float64, now time.Time) {
	c.baseMS = c.read(now)
	c.anchor, c.correction = now, deltaMS
}
func (c *clock) view(now time.Time) ClockView {
	value := c.read(now)
	return ClockView{TimeMS: value, HostOffsetMS: value - hostMS(now), DriftPPM: c.driftPPM,
		PendingMS: c.pending(now), SlewRateFraction: slewRate}
}

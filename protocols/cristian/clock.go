package main

import (
	"fmt"  // Errorf builds an error value with printf-style substitutions.
	"math" // IsNaN/IsInf reject special floating-point values; Abs measures magnitude.
	"time" // Time represents an instant; Duration represents elapsed nanoseconds.
)

// clock is a SOFTWARE wall clock, never the operating system's clock. Its time
// is baseMS plus elapsed time at a configurable rate. Lower-case names are
// private to this package. The protocol's mutex protects this mutable object;
// clock itself deliberately has no second mutex to complicate lock ordering.
//
// This small helper is repeated in the NTP module so either lesson can be
// imported independently. It is not an RPC service and sends no messages.
type clock struct {
	anchor   time.Time // Keeps time.Now's monotonic component for elapsed-time measurement.
	baseMS   float64   // Software Unix milliseconds at anchor, including initial skew.
	driftPPM float64   // Parts per million: +1000 means 1 ms gained per real second.
}

// ClockArgs is exported (capital C) because net/rpc needs exported argument
// types. Struct tags are metadata read by reflection: json controls field names
// in DistVis's automatically generated application form and JSON state output.
type ClockArgs struct {
	OffsetMS float64 `json:"offsetMS"` // Initial difference from the host clock, not a delta.
	DriftPPM float64 `json:"driftPPM"` // Clock frequency error, not a time offset.
}

// ClockView is a snapshot, not a live clock. Read again to see time advance.
// HostOffsetMS is an experimental diagnostic ONLY: algorithms never read it.
// Containers on this machine share a host clock, but real distributed machines
// cannot use a shared clock to magically learn their synchronization error.
type ClockView struct {
	TimeMS       float64 `json:"timeMS"`
	HostOffsetMS float64 `json:"hostOffsetMS"`
	DriftPPM     float64 `json:"driftPPM"`
}

// finite is necessary even when the UI checks numbers: our RPC methods can also
// be called by Go code. NaN would otherwise make comparisons silently false.
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func validateClock(args ClockArgs) error {
	if !finite(args.OffsetMS) || math.Abs(args.OffsetMS) > 60000 {
		return fmt.Errorf("offsetMS must be finite and between -60000 and 60000")
	}
	if !finite(args.DriftPPM) || math.Abs(args.DriftPPM) > 10000 {
		return fmt.Errorf("driftPPM must be finite and between -10000 and 10000")
	}
	return nil // nil is the conventional successful error result in Go.
}

// newClock returns a VALUE. Passing now in, instead of reading time.Now inside,
// lets tests move time forward deterministically without sleeping.
func newClock(now time.Time, args ClockArgs) clock {
	return clock{anchor: now, baseMS: hostMS(now) + args.OffsetMS, driftPPM: args.DriftPPM}
}

// UnixMilli is about 10^12 today and fits safely in a float64. Unlike UnixNano
// it also fits in JavaScript's exact integer range. Fractional elapsed
// milliseconds are retained; these are teaching timestamps, not atomic clocks.
func hostMS(now time.Time) float64 { return float64(now.UnixMilli()) }

// A pointer receiver (*clock) accesses the original clock rather than copying
// it. Time.Sub uses monotonic readings when both Times carry them; a host wall
// clock step therefore does not make our measured elapsed duration negative.
func (c *clock) read(now time.Time) float64 {
	elapsedMS := now.Sub(c.anchor).Seconds() * 1000
	return c.baseMS + elapsedMS*(1+c.driftPPM/1e6)
}

// step adds an offset at the CURRENT instant. Setting an old measured timestamp
// directly would lose all time spent after taking that measurement. This step
// can move time backwards: intentional in the textbook Cristian/NTP lessons,
// unlike the gradual, non-backwards Berkeley adjustment in its own clock.go.
func (c *clock) step(deltaMS float64, now time.Time) {
	c.baseMS = c.read(now) + deltaMS
	c.anchor = now
}

func (c *clock) view(now time.Time) ClockView {
	value := c.read(now)
	return ClockView{TimeMS: value, HostOffsetMS: value - hostMS(now), DriftPPM: c.driftPPM}
}

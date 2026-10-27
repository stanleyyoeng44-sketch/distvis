// distvis:name Berkeley Clock Synchronization HANDMADE
// distvis:description A fixed coordinator polls software clocks, rejects outliers, averages offsets and sends relative deltas for gradual adjustment.
// distvis:start node-1 Application.Sync {"outlierThresholdMS":2000} Poll the group and move all reachable, stable clocks toward their filtered mean.
package main

import (
	"crypto/rand"
	"fmt"
	"math"
	"net/rpc"
	"sync"
	"time"

	lab "distvis/sdk/rpc"
)

func main() {
	lab.Main(configure)
}

// my software clock. corrections are never applied as a jump, only as a
// temporary rate change (max 10%), so time can never go backwards.
type clock struct {
	anchor     time.Time
	baseMS     float64
	driftPPM   float64
	correction float64 // total correction scheduled at anchor
}

const slewRate = 0.10 // exaggerated so the demo converges in seconds

type ClockArgs struct {
	OffsetMS float64 `json:"offsetMS"`
	DriftPPM float64 `json:"driftPPM"`
}
type ClockView struct {
	TimeMS       float64 `json:"timeMS"`
	HostOffsetMS float64 `json:"hostOffsetMS"` // software clock minus host clock: the observable error (errorMs)
	DriftPPM     float64 `json:"driftPPM"`
	SlewMsLeft   float64 `json:"slewMsLeft"` // smooth-adjustment remaining; 0 when settled or after a step
}

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

func newClock(now time.Time, args ClockArgs) clock {
	return clock{anchor: now, baseMS: float64(now.UnixMilli()) + args.OffsetMS, driftPPM: args.DriftPPM}
}

func (c *clock) read(now time.Time) float64 {
	elapsed := now.Sub(c.anchor).Seconds() * 1000
	return c.baseMS + elapsed*(1+c.driftPPM/1e6) + c.applied(now)
}

func (c *clock) applied(now time.Time) float64 {
	elapsed := math.Max(0, now.Sub(c.anchor).Seconds()*1000)
	mag := math.Min(math.Abs(c.correction), elapsed*slewRate) // don't overshoot
	return math.Copysign(mag, c.correction)                   // keep the sign
}

func (c *clock) pending(now time.Time) float64 { return c.correction - c.applied(now) }

// slew rebases on the current value so scheduling a correction never jumps.
func (c *clock) slew(deltaMS float64, now time.Time) {
	c.baseMS = c.read(now)
	c.anchor, c.correction = now, deltaMS
}

// step applies the correction as an immediate jump. This is the WRONG way to
// do it (a negative delta makes time go backward); it exists only so scenario C
// can contrast a jump against a smooth slew.
func (c *clock) step(deltaMS float64, now time.Time) {
	c.baseMS = c.read(now) + deltaMS
	c.anchor, c.correction = now, 0
}

func (c *clock) view(now time.Time) ClockView {
	value := c.read(now)
	return ClockView{TimeMS: value, HostOffsetMS: value - float64(now.UnixMilli()), DriftPPM: c.driftPPM,
		SlewMsLeft: c.pending(now)}
}

// node state, everything behind mu.
type TimeService struct {
	mu              sync.Mutex
	id              string
	coordinator     string
	members         []string
	instance        string // random boot id, changes when the node restarts
	peers           map[string]*rpc.Client
	report          func(any) error
	clock           clock
	generation      uint64
	coordinatorBoot string // which coordinator restart we accepted first
	lastApplied     uint64
	lastDeltaMS     float64
	busy            bool
	stepMode        bool // true = jump immediately (scenario C), false = slew
	lastError       string
	round           *Round // the worksheet of the current round
}

type Application struct{ service *TimeService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	index := 0
	for i, id := range node.Nodes { // range yields both the index and its value.
		if id == node.ID {
			index = i
		}
	}
	s := &TimeService{
		id: node.ID, coordinator: node.Nodes[0], members: node.Nodes,
		instance: rand.Text(), peers: peers, report: node.Report,
		// everyone starts with a different offset so the first round has work to do
		clock: newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 750}),
	}
	if err := node.RegisterName("TimeService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	return s.report(s.stateLocked())
}

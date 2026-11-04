// distvis:name Cristian Clock Synchronization HANDMADE
package main

import (
	lab "distvis/sdk/rpc"
	"net/rpc"
	"sync"
	"time"
)

func main() {
	lab.Main(configure)
}

type Application struct {
	node   *lab.Runtime
	nodeID string
	clock  *Clock
	peers  map[string]*rpc.Client
}

type Clock struct {
	mu         sync.Mutex
	origintime time.Time
	originread float64
	drift      float64
}

func (c *Clock) readAt(now time.Time) float64 {
	elapsedSeconds := now.Sub(c.origintime).Seconds()
	return c.originread + elapsedSeconds*c.drift
}

func (c *Clock) changeAt(now time.Time, delta float64) {
	c.originread = c.readAt(now) + delta
	c.origintime = now
}

func newClock() Clock {
	return Clock{origintime: time.Now(), originread: 0, drift: 1}
}

type TimeService struct {
	clock *Clock
}

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	clock := newClock()
	if node.ID == "node-1" {
		ts := TimeService{clock: &clock}
		err := node.RegisterName("TimeService", &ts)
		if err != nil {
			return err
		}
	}
	if err := node.RegisterName("Application", &Application{clock: &clock, nodeID: node.ID, peers: peers, node: node}, lab.Application()); err != nil {
		return err
	}
	return node.Report("hey dude, I'm ready")
}

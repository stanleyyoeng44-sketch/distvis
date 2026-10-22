// distvis:name Berkeley Sync · Step by Step
// distvis:description One Berkeley round, driven by you: poll every clock, compute the group mean and the outliers, then send the gradual corrections. Each step is its own action, so nothing happens behind your back.
// distvis:start node-1 Application.PollPeers {} Step 1: ask every node what time it thinks it is.

// package main tells Go to build an executable rather than an importable library.
// main.go is deliberately small: start here, then read clock.go, protocol.go
// and round.go in that order.
package main

// This is a package alias. The standard net/rpc API is used in protocol.go and
// round.go; lab supplies membership, transport integration and lifecycle.
import (
	"crypto/rand" // rand.Text() makes an unpredictable boot identity; it is not clock time.
	"time"

	lab "distvis/sdk/rpc"
)

func main() {
	// Main invokes configure with this node's Runtime. It also owns stdin/stdout,
	// RPC dispatch, signal handling and shared connection cleanup. Never print
	// ordinary text to stdout: DistVis uses it as a machine-readable channel.
	lab.Main(configure)
}

// configure builds this node's state and registers exactly two services:
//
//   - TimeService holds the two node-to-node RPCs the coordinator calls during
//     a round (Poll in step 1, Adjust in step 3). It creates no input forms.
//   - Application holds the actions YOU trigger from the platform UI. Only
//     methods registered with lab.Application() become input forms.
func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	// The coordinator is simply the first name in the membership list that the
	// platform provides. This lesson has no election protocol (see README).
	members := node.Nodes
	index := 0
	for i, id := range members {
		if id == node.ID {
			index = i
		}
	}
	s := &TimeService{
		id:          node.ID,
		coordinator: members[0],
		members:     members,
		instance:    rand.Text(), // Who am I after a restart? Not a timestamp, not authentication.
		peers:       peers,       // Owned by the runtime: never Close these here.
		report:      node.Report,
		// Each node starts with a deliberately different clock so the first
		// round has something to fix: node-1 +0 ms, node-2 +750 ms, ...
		clock: newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 750}),
	}
	if err := node.RegisterName("TimeService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	s.mu.Lock()
	state := s.stateLocked()
	s.mu.Unlock()
	return node.Report(state)
}

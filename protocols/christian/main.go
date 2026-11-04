// distvis:name Cristian Clock Synchronization
// distvis:description Cristian's algorithm: a client asks the time server (node-1) for its time Ts, measures the round-trip time RTT on its own clock, and sets its clock to Ts + RTT/2. Every node has a simulated clock with its own initial offset and exaggerated drift; errorMs shows how far it is from real time.
// distvis:start node-2 Application.Sync {"server":"node-1","mode":"half"} Synchronize node-2's clock with the time server node-1.
package main

import (
	"context"
	"fmt"
	"net/rpc"
	"os"
	"sync"
	"time"

	lab "distvis/sdk/rpc"
)

func main() {
	lab.Main(configure)
}

// defaultServer is the time server clients use when no server is chosen.
const defaultServer = "node-1"

// reportEvery: how often each node re-reports its state, so drift is visible.
const reportEvery = 2 * time.Second

// node holds everything one process knows. mu protects every field below it;
// it is never held while waiting for an RPC, so Status stays responsive.
type node struct {
	rt    *lab.Runtime
	peers map[string]*rpc.Client

	mu            sync.Mutex
	clock         Clock
	busy          bool        // a Sync is waiting for the server's reply
	serverDelayMs float64     // simulated queuing delay before answering Ask
	served        int         // Ask requests answered (as a server)
	syncs         int         // successful syncs (as a client)
	failedSyncs   int         // syncs that got no reply
	lastSync      *SyncResult // most recent SUCCESSFUL sync
	lastError     string      // error of the most recent failed sync, "" after a success
}

// TimeService is the internal RPC used between nodes (not shown as an input).
type TimeService struct{ n *node }

// Application methods become input forms in the DistVis UI.
type Application struct{ n *node }

func configure(rt *lab.Runtime) error {
	peers, err := rt.RPCPeers()
	if err != nil {
		return err
	}
	offset, drift := initialClock(rt.ID)
	n := &node{rt: rt, peers: peers, clock: newClock(time.Now(), offset, drift)}

	// Every node can answer time requests, so a client may also pick a
	// non-reference server (and inherit that server's error).
	if err := rt.RegisterName("TimeService", &TimeService{n}); err != nil {
		return err
	}
	if err := rt.RegisterName("Application", &Application{n}, lab.Application()); err != nil {
		return err
	}

	// Periodic report: lets you watch errorMs grow between syncs (drift).
	rt.OnStart(func(ctx context.Context) error {
		ticker := time.NewTicker(reportEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				n.publish()
			}
		}
	})
	return rt.Report(n.view())
}

// StateView is the full state reported to the node panel (and returned by Status).
type StateView struct {
	Role          string      `json:"role"`
	Clock         string      `json:"clock"`   // this node's clock, human-readable (UTC)
	ClockMs       float64     `json:"clockMs"` // this node's clock, Unix milliseconds
	ErrorMs       float64     `json:"errorMs"` // clock minus real time (+ = ahead)
	DriftPPM      float64     `json:"driftPPM"`
	ServerDelayMs float64     `json:"serverDelayMs"`
	Served        int         `json:"served"`
	Syncs         int         `json:"syncs"`
	FailedSyncs   int         `json:"failedSyncs"`
	Busy          bool        `json:"busy"`
	LastSync      *SyncResult `json:"lastSync"`
	LastError     string      `json:"lastError"`
}

func (n *node) view() StateView {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.viewLocked()
}

func (n *node) viewLocked() StateView {
	now := time.Now()
	clockMs := n.clock.readAt(now)
	role := "client"
	if n.rt.ID == defaultServer {
		role = "server"
	}
	return StateView{
		Role:          role,
		Clock:         time.UnixMicro(int64(clockMs * 1000)).UTC().Format("15:04:05.000"),
		ClockMs:       round1(clockMs),
		ErrorMs:       n.clock.errorMs(now),
		DriftPPM:      n.clock.driftPPM,
		ServerDelayMs: n.serverDelayMs,
		Served:        n.served,
		Syncs:         n.syncs,
		FailedSyncs:   n.failedSyncs,
		Busy:          n.busy,
		LastSync:      n.lastSync,
		LastError:     n.lastError,
	}
}

// publish reports the current state. A failed report only means the
// coordinator connection is gone, so it is logged rather than turned into an
// RPC error (which would make a successful sync look failed).
func (n *node) publish() {
	// Snapshot and publication share one lock so a periodic report cannot arrive
	// after a newer successful-sync report and make the displayed state regress.
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.rt.Report(n.viewLocked()); err != nil {
		fmt.Fprintln(os.Stderr, "report failed:", err)
	}
}

// distvis:name Lamport Mutual Exclusion
// distvis:description Timestamp-ordered requests, deferred acknowledgements and explicit releases. Select Ricart–Agrawala before any requests to compare the release-free variant.
// distvis:start node-1 Application.Acquire {"holdMs":2000} Start an acquisition; watch operation progress from waiting to critical to completed.
package main

import (
	"context"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
	"time"

	lab "distvis/sdk/rpc"
)

func main() { lab.Main(configure) }

// priority compares numeric node IDs, so node-2 precedes node-10.
func priority(id string) int {
	parts := strings.Split(id, "-")
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

type lamportClock struct{ now uint64 }

func (c *lamportClock) tick() uint64 {
	c.now++
	return c.now
}

func (c *lamportClock) onReceive(remote uint64) uint64 {
	if remote > c.now {
		c.now = remote
	}
	return c.tick()
}

const (
	roleIdle     = "idle"
	roleWaiting  = "waiting"
	roleCritical = "critical"
	lamport      = "lamport"
	ricart       = "ricart-agrawala"
)

type MutexService struct {
	mu        sync.Mutex
	id        string
	members   []string
	peers     map[string]*rpc.Client
	report    func(any) error
	ctx       context.Context
	algorithm string
	clock     lamportClock
	role      string
	myTS      uint64
	acks      map[string]bool
	queue     []QueueEntry
	deferred  []QueueEntry
	// Released request timestamps prevent delayed messages from re-queuing them.
	released map[string]uint64

	entries          int
	completed        int
	lastWaitMS       float64
	waitStart        time.Time
	entered          chan struct{}
	operationRunning bool
	operation        string
	autoRunning      bool
	sent             uint64
	sendErrors       uint64
	lastErr          string
	reportSeq        uint64
}

type Application struct{ service *MutexService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &MutexService{
		id: node.ID, members: node.Nodes, peers: peers, report: node.Report,
		ctx: context.Background(), algorithm: lamport, role: roleIdle,
		operation: "none", released: make(map[string]uint64),
	}
	// Inputs can arrive as soon as the runtime publishes its schema. Cancel a
	// stable context on shutdown rather than replacing it in the start hook.
	ctx, cancel := context.WithCancel(s.ctx)
	s.ctx = ctx
	node.OnStart(func(ctx context.Context) error {
		<-ctx.Done()
		cancel()
		return nil
	})
	if err := node.RegisterName("MutexService", s); err != nil {
		cancel()
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		cancel()
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reportLocked()
}

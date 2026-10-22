// distvis:name Lamport Mutual Exclusion HANDMADE
package main

import (
	"net/rpc"
	"sync"

	lab "distvis/sdk/rpc"
)

func main() {
	lab.Main(configure)
}

// lamport clock - just a counter, not a real clock. every event ticks it and
// every receive does max(local, msg)+1 so the values are comparable.
type lamportClock struct {
	now uint64
}

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

// requests are totally ordered by (ts, node). timestamps can tie, the id
// breaks them - comparing id strings would be wrong ("node-10" < "node-2").
type requestKey struct {
	ts   uint64
	node string
}

func before(a, b requestKey) bool {
	if a.ts != b.ts {
		return a.ts < b.ts
	}
	return a.node < b.node
}

type MutexService struct {
	mu       sync.Mutex
	id       string
	members  []string
	peers    map[string]*rpc.Client
	report   func(any) error
	clock    lamportClock
	status   string // idle | requesting | in-cs
	myTS     uint64 // 0 when idle
	acks     map[string]bool
	queue    []QueueEntry
	deferred []QueueEntry // requests I'm withholding an ack for
	entered  []QueueEntry
	events   []string
	lastErr  string
}

type Application struct{ service *MutexService }

const (
	statusIdle       = "idle"
	statusRequesting = "requesting"
	statusInCS       = "in-cs"
)

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
	s := &MutexService{
		id: node.ID, members: node.Nodes, peers: peers, report: node.Report,
		clock:  lamportClock{now: uint64(index)}, // start at the index like the lecture figure
		status: statusIdle,
	}
	if err := node.RegisterName("MutexService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	s.mu.Lock()
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return node.Report("hey dude, I'm ready")
}

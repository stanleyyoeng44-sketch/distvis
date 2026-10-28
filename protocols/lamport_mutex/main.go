// distvis:name Lamport Mutual Exclusion
// distvis:description Decentralized mutual exclusion with Lamport clocks: a node broadcasts Request(ts), others queue it in timestamp order and reply Ack; it enters the critical section once every Ack has arrived and its request is first, then broadcasts Release.
// distvis:start node-1 Application.Acquire {"holdMs":2000} Request the critical section and hold it for 2 seconds.
package main

import (
	"net/rpc"
	"strconv"
	"strings"
	"sync"
	"time"

	lab "distvis/sdk/rpc"
)

func main() {
	lab.Main(configure)
}

// priority is the numeric part of the node id.
func priority(id string) int {
	parts := strings.Split(id, "-")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}

// lamportClock is a Lamport logical clock.
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

const (
	roleIdle     = "idle"
	roleWaiting  = "waiting"
	roleCritical = "critical"
)

type MutexService struct {
	mu          sync.Mutex
	id          string
	members     []string
	peers       map[string]*rpc.Client
	report      func(any) error
	clock       lamportClock
	role        string // idle | waiting | critical
	myTS        uint64 // timestamp of my current request; 0 when idle
	acks        map[string]bool
	queue       []QueueEntry
	deferred    []QueueEntry // requests I am withholding an Ack for
	entries     int          // times entered the critical section
	lastWaitMS  float64      // how long the last acquire waited to enter
	waitStart   time.Time
	entered     chan struct{} // closed when the current acquire enters
	sent        uint64
	autoRunning bool
	lastErr     string
}

type Application struct{ service *MutexService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &MutexService{
		id: node.ID, members: node.Nodes, peers: peers, report: node.Report,
		role: roleIdle,
	}
	if err := node.RegisterName("MutexService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	return s.report(s.stateLocked())
}

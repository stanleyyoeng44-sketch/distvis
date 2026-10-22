// distvis:name Bully Election HANDMADE
package main

import (
	"net/rpc"
	"strconv"
	"strings"
	"sync"

	lab "distvis/sdk/rpc"
)

func main() {
	lab.Main(configure)
}

// priority is the number in the node id: node-3 outranks node-2. DON'T
// compare the id strings - "node-10" would lose against "node-2".
func priority(id string) int {
	parts := strings.Split(id, "-")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}

type ElectionService struct {
	mu            sync.Mutex
	id            string
	priority      int
	members       []string
	peers         map[string]*rpc.Client
	report        func(any) error
	status        string // follower | candidate | coordinator
	coordinator   string
	electionRound uint64
	won           int
	backedDown    int
	events        []string
	lastErr       string
}

type Application struct{ service *ElectionService }

const (
	statusFollower    = "follower"
	statusCandidate   = "candidate"
	statusCoordinator = "coordinator"
)

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &ElectionService{
		id: node.ID, priority: priority(node.ID), members: node.Nodes,
		peers: peers, report: node.Report,
		status: statusFollower, // nobody is coordinator until the first election
	}
	if err := node.RegisterName("ElectionService", s); err != nil {
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

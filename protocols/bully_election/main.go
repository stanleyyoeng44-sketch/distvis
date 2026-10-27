// distvis:name Bully Election
// distvis:description Manual Bully election: nothing is sent on startup. Trigger Application.StartElection on any node; it sends Election to every higher node, higher nodes answer OK and start their own election, and the highest live node broadcasts Coordinator.
// distvis:start node-1 Application.StartElection {} Manually start an election on node-1.
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

// priority is the number in the node id: node-3 outranks node-2. Compare the
// number, never the id string ("node-10" would lose against "node-2").
func priority(id string) int {
	parts := strings.Split(id, "-")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}

const (
	roleFollower    = "follower"
	roleCandidate   = "candidate"
	roleCoordinator = "coordinator"
)

// ElectionService has no background goroutine: an election runs only when the
// application action StartElection is invoked. That keeps an idle experiment
// quiet and lets the operator decide when a failure is detected.
type ElectionService struct {
	mu            sync.Mutex
	id            string
	priority      int
	members       []string
	peers         map[string]*rpc.Client
	report        func(any) error
	role          string
	coordinator   string
	electionRound uint64
	won           int
	backedDown    int
	sent          uint64
	lastErr       string
}

type Application struct{ service *ElectionService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &ElectionService{
		id: node.ID, priority: priority(node.ID), members: node.Nodes,
		peers: peers, report: node.Report,
		role: roleFollower, // nobody is coordinator until the first election
	}
	if err := node.RegisterName("ElectionService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	// Intentionally no OnStart hook: no election and no heartbeat traffic until
	// the operator invokes Application.StartElection.
	return s.report(s.stateLocked())
}

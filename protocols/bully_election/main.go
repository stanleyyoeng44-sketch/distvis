// distvis:name Bully Election
// distvis:description Automatic startup election, coordinator failure detection and recovery takeover by the highest reachable numeric node ID.
package main

import (
	"context"
	lab "distvis/sdk/rpc"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main()                  { lab.Main(configure) }
func priority(id string) int { n, _ := strconv.Atoi(strings.TrimPrefix(id, "node-")); return n }

const (
	roleFollower    = "follower"
	roleCandidate   = "candidate"
	roleCoordinator = "coordinator"
)

type ElectionService struct {
	mu              sync.Mutex
	id              string
	priority        int
	members         []string
	peers           map[string]*rpc.Client
	report          func(any) error
	role            string
	coordinator     string
	electionRound   uint64
	won, backedDown int
	sent            uint64
	lastErr         string
	waitingUntil    time.Time
	automatic       bool
}
type Application struct{ service *ElectionService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &ElectionService{id: node.ID, priority: priority(node.ID), members: node.Nodes, peers: peers, report: node.Report, role: roleFollower, automatic: true}
	if err = node.RegisterName("ElectionService", s); err != nil {
		return err
	}
	if err = node.RegisterName("Application", &Application{s}, lab.Application()); err != nil {
		return err
	}
	node.OnStart(func(ctx context.Context) error {
		// Delay the first election briefly so every node has registered its services.
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		go s.startElection()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				s.detectFailure()
			}
		}
	})
	return s.report(s.stateLocked())
}

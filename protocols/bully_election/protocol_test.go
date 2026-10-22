package main

import (
	"net"
	"net/rpc"
	"testing"
	"time"
)

// ─── Pure logic ──────────────────────────────────────────────────────────────

func TestPriorityUsesNumbersNotStrings(t *testing.T) {
	// Lexical order would make "node-10" lose against "node-2" — wrong.
	if priority("node-3") != 3 || priority("node-10") != 10 {
		t.Fatalf("bad priorities: %d %d", priority("node-3"), priority("node-10"))
	}
	if priority("node-10") <= priority("node-9") {
		t.Fatal("node-10 must outrank node-9")
	}
	if priority("node-1") != 1 || priority("weird") != 0 {
		t.Fatal("bad fallback")
	}
}

func TestRefusesToOKNodeItDoesNotOutrank(t *testing.T) {
	s := &ElectionService{id: "node-2", priority: 2, members: []string{"node-1", "node-2", "node-3"},
		peers: map[string]*rpc.Client{}, report: func(any) error { return nil }, status: statusFollower}
	// ELECTION from a HIGHER node should never arrive (senders only contact
	// higher priorities). The defensive answer is a refusal, not an OK.
	var reply ElectionReply
	if err := s.Election(ElectionMsg{From: "node-3", ID: 3}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.OK || s.status != statusFollower {
		t.Fatalf("must refuse a higher-ranked sender: ok=%v status=%s", reply.OK, s.status)
	}
	// An unknown sender is rejected outright.
	if err := s.Election(ElectionMsg{From: "node-9", ID: 9}, &reply); err == nil {
		t.Fatal("election from outside the membership must be rejected")
	}
}

func TestVictoryStandsMeDown(t *testing.T) {
	s := &ElectionService{id: "node-1", priority: 1, members: []string{"node-1", "node-2", "node-3"},
		peers: map[string]*rpc.Client{}, report: func(any) error { return nil }}
	s.status, s.electionRound = statusCandidate, 5
	if err := s.Victory(VictoryMsg{From: "node-2", ID: 2}, &VictoryReply{}); err != nil {
		t.Fatal(err)
	}
	st := snap(s)
	if st.Role != statusFollower || st.Coordinator != "node-2" {
		t.Fatalf("victory not accepted: %+v", st)
	}
	if st.ElectionRound != 6 {
		t.Fatalf("the campaign in flight was not invalidated: round %d", st.ElectionRound)
	}
	// A SITTING coordinator must also step aside when somebody else wins —
	// otherwise a healed partition leaves two nodes with the leader role.
	s2 := &ElectionService{id: "node-1", priority: 1, members: []string{"node-1", "node-2", "node-3"},
		peers: map[string]*rpc.Client{}, report: func(any) error { return nil }}
	s2.status, s2.coordinator = statusCoordinator, "node-1"
	if err := s2.Victory(VictoryMsg{From: "node-3", ID: 3}, &VictoryReply{}); err != nil {
		t.Fatal(err)
	}
	if st := snap(s2); st.Role != statusFollower || st.Coordinator != "node-3" {
		t.Fatalf("sitting coordinator did not step aside: %+v", st)
	}
}

func TestStartElectionRefusesDoubleStart(t *testing.T) {
	s := &ElectionService{id: "node-1", priority: 1, members: []string{"node-1", "node-2"},
		peers: map[string]*rpc.Client{}, report: func(any) error { return nil }, status: statusCandidate}
	if err := (&Application{service: s}).StartElection(Empty{}, &State{}); err == nil {
		t.Fatal("a second election while one is running must be refused")
	}
}

// ─── Real net/rpc scenarios ──────────────────────────────────────────────────

func TestHighestPriorityAliveWins(t *testing.T) {
	nodes := cluster(t, []string{"node-1", "node-2", "node-3"}, nil)
	apps := applications(nodes)

	// The LOWEST-priority node starts: it must lose to the highest alive.
	var state State
	if err := apps[0].StartElection(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	if state.Role == statusCoordinator {
		t.Fatal("node-1 must not win while node-3 lives")
	}
	// Wait for the SETTLED state: agreeing on the winner is not enough, because
	// a fresh ELECTION bullies the winner into a quick re-campaign (Role shows
	// "candidate" for a moment while it reasserts itself).
	waitUntil(t, 8*time.Second, "every node agreeing on node-3", func() bool {
		for i, node := range nodes {
			st := snap(node)
			if st.Coordinator != "node-3" {
				return false
			}
			if i == 2 && st.Role != statusCoordinator {
				return false
			}
			if i < 2 && st.Role != statusFollower {
				return false
			}
		}
		return true
	})
	for i, node := range nodes {
		st := snap(node)
		if i == 2 && st.ElectionsWon < 1 {
			t.Fatalf("node-3 should have won at least once: %+v", st)
		}
		if i < 2 && st.BackedDown < 1 {
			t.Fatalf("%s should have abandoned its election: %+v", node.id, st)
		}
	}
}

func TestNextHighestTakesOverWhenTheBestIsDown(t *testing.T) {
	// node-3 exists in the membership but is CRASHED: its connections are dead.
	nodes := cluster(t, []string{"node-1", "node-2", "node-3"}, map[string]bool{"node-3": true})
	apps := applications(nodes)

	var state State
	if err := apps[0].StartElection(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 8*time.Second, "node-2 elected", func() bool {
		return snap(nodes[0]).Coordinator == "node-2" && snap(nodes[1]).Role == statusCoordinator
	})
	if st := snap(nodes[1]); st.ElectionsWon < 1 {
		t.Fatalf("node-2 should have won: %+v", st)
	}

	// Bullying the leader: a fresh election while the coordinator is alive
	// converges on the SAME node — it reasserts itself.
	if err := apps[0].StartElection(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 8*time.Second, "node-2 re-elected", func() bool {
		return snap(nodes[0]).Coordinator == "node-2" && snap(nodes[1]).ElectionsWon >= 2
	})
}

// ─── Harness ─────────────────────────────────────────────────────────────────

func snap(s *ElectionService) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func applications(nodes []*ElectionService) []*Application {
	apps := make([]*Application, len(nodes))
	for i, node := range nodes {
		apps[i] = &Application{service: node}
	}
	return apps
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

// cluster builds fully connected processes over real net/rpc pipes: one
// connection per direction. Crashed nodes are listed in the membership but
// their connections are closed, exactly like calls into a dead container.
func cluster(t *testing.T, ids []string, crashed map[string]bool) []*ElectionService {
	t.Helper()
	nodes := make([]*ElectionService, len(ids))
	for i, id := range ids {
		nodes[i] = &ElectionService{
			id: id, priority: priority(id), members: ids,
			peers: map[string]*rpc.Client{}, report: func(any) error { return nil },
			status: statusFollower,
		}
	}
	for _, from := range nodes {
		for _, to := range nodes {
			if from.id == to.id {
				continue
			}
			if crashed[to.id] {
				local, remote := net.Pipe()
				client := rpc.NewClient(local)
				_ = client.Close() // Every call into this node fails immediately.
				_ = remote.Close()
				from.peers[to.id] = client
				continue
			}
			local, remote := net.Pipe()
			server := rpc.NewServer()
			if err := server.RegisterName("ElectionService", to); err != nil {
				t.Fatal(err)
			}
			client := rpc.NewClient(local)
			go server.ServeConn(remote)
			from.peers[to.id] = client
			t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
		}
	}
	return nodes
}

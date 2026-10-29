package main

import (
	"sync/atomic"
	"testing"
)

func TestReportsHoldStateLock(t *testing.T) {
	s := &ElectionService{id: "node-5", priority: 5, members: []string{"node-5"}, role: roleFollower}
	var unlocked atomic.Bool
	s.report = func(any) error {
		if s.mu.TryLock() {
			unlocked.Store(true)
			s.mu.Unlock()
		}
		return nil
	}
	s.startElection()
	s.mu.Lock()
	s.members = []string{"node-5", "node-7"}
	s.mu.Unlock()
	if err := s.Coordinator(CoordinatorMsg{From: "node-7", ID: 7}, &CoordinatorReply{}); err != nil {
		t.Fatal(err)
	}
	if unlocked.Load() {
		t.Fatal("state snapshot can be published after a newer state")
	}
}

func TestNumericPriority(t *testing.T) {
	if priority("node-10") <= priority("node-2") {
		t.Fatal("IDs sorted lexically")
	}
}
func TestHighestNodeWinsWithoutHigherPeers(t *testing.T) {
	s := &ElectionService{id: "node-5", priority: 5, members: []string{"node-5"}, report: func(any) error { return nil }, role: roleFollower}
	s.startElection()
	if s.role != roleCoordinator || s.coordinator != "node-5" || s.won != 1 {
		t.Fatal(s.stateLocked())
	}
}
func TestHigherAnnouncementCancelsCampaign(t *testing.T) {
	s := &ElectionService{id: "node-2", priority: 2, members: []string{"node-2", "node-5"}, report: func(any) error { return nil }, role: roleCandidate, electionRound: 1}
	if err := s.Coordinator(CoordinatorMsg{From: "node-5", ID: 5}, &CoordinatorReply{}); err != nil {
		t.Fatal(err)
	}
	if s.electionRound != 2 || s.role != roleFollower || s.coordinator != "node-5" {
		t.Fatal(s.stateLocked())
	}
}

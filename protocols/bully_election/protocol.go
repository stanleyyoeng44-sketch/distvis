package main

import (
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

// bounded rpc call so a dead/blocked node can't hang the action past the
// platform's ~10s command deadline. on timeout the reply must NOT be read.
func call(client *rpc.Client, method string, args, reply any) error {
	done := make(chan error, 1)
	go func() { done <- client.Call(method, args, reply) }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		return fmt.Errorf("%s: no reply within 3s; remote outcome unknown", method)
	}
}

// the bully algorithm (garcia-molina). "highest priority alive wins":
//
//  1. StartElection: become candidate, send ELECTION to every HIGHER node.
//  2. each higher node answers OK and starts its OWN election (the bullying).
//  3. any OK means I back down and wait for its victory.
//  4. nobody answered -> I am the highest alive: broadcast VICTORY.
//  5. receiving VICTORY records the coordinator and ends my election.
type Empty struct{}

type ElectionMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"`
}
type ElectionReply struct {
	OK bool `json:"ok"` // "I am alive and I outrank you"
}

type VictoryMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"`
}
type VictoryReply struct{}

type State struct {
	Node          string   `json:"node"`
	ID            int      `json:"id"`
	Role          string   `json:"role"` // follower | candidate | coordinator
	Coordinator   string   `json:"coordinator"`
	ElectionRound uint64   `json:"electionRound"`
	ElectionsWon  int      `json:"electionsWon"`
	BackedDown    int      `json:"electionsBackedDown"`
	HigherPeers   []string `json:"higherPeers"`
	LastError     string   `json:"lastError"`
}

func (s *ElectionService) isMember(id string) bool {
	for _, member := range s.members {
		if member == id {
			return true
		}
	}
	return false
}

// every node with a strictly higher priority - the ones I'd send ELECTION to.
func (s *ElectionService) higherPeersLocked() []string {
	var higher []string
	for _, member := range s.members {
		if priority(member) > s.priority {
			higher = append(higher, member)
		}
	}
	return higher
}

func (s *ElectionService) stateLocked() State {
	higher := make([]string, 0, len(s.members))
	for _, member := range s.members {
		if priority(member) > s.priority {
			higher = append(higher, member)
		}
	}
	return State{
		Node: s.id, ID: s.priority, Role: s.status, Coordinator: s.coordinator,
		ElectionRound: s.electionRound, ElectionsWon: s.won, BackedDown: s.backedDown,
		HigherPeers: higher, LastError: s.lastErr,
	}
}

// campaign runs one full round: ask every higher node, then back down or win.
// round identifies this campaign so a victory arriving meanwhile can kill it.
func (s *ElectionService) campaign(round uint64) {
	s.mu.Lock()
	self := s.id
	message := ElectionMsg{From: s.id, ID: s.priority}
	higher := s.higherPeersLocked()
	s.mu.Unlock()

	// ask everyone at once so one dead node costs one timeout. the RPC REPLY
	// is the classic OK.
	var workers sync.WaitGroup
	var countMu sync.Mutex
	okCount := 0
	for _, peer := range higher {
		workers.Add(1)
		go func(to string) {
			defer workers.Done()
			client := s.peers[to]
			if client == nil {
				return
			}
			var reply ElectionReply
			if err := call(client, "ElectionService.Election", message, &reply); err != nil {
				return // treated as "down"
			}
			if reply.OK {
				countMu.Lock()
				okCount++
				countMu.Unlock()
			}
		}(peer)
	}
	workers.Wait()

	s.mu.Lock()
	if s.electionRound != round || s.status != statusCandidate {
		// a VICTORY arrived while I was campaigning, I already stood down
		s.mu.Unlock()
		return
	}
	if okCount > 0 {
		s.status = statusFollower
		s.backedDown++
		st := s.stateLocked()
		s.mu.Unlock()
		_ = s.report(st)
		return
	}

	// nobody higher answered -> I am the highest node ALIVE.
	s.status = statusCoordinator
	s.coordinator = s.id
	s.won++
	victory := VictoryMsg{From: s.id, ID: s.priority}
	peers := append([]string(nil), s.members...)
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)

	var deliver sync.WaitGroup
	for _, peer := range peers {
		if peer == self {
			continue
		}
		deliver.Add(1)
		go func(to string) {
			defer deliver.Done()
			client := s.peers[to]
			if client == nil {
				return
			}
			_ = call(client, "ElectionService.Victory", victory, &VictoryReply{})
		}(peer)
	}
	deliver.Wait()
}

// incoming messages.

// Election: answer OK (I outrank you) and start my own election - the
// bullying takeover. the reply IS the OK the sender is waiting for.
func (s *ElectionService) Election(args ElectionMsg, reply *ElectionReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("election from unknown node %q", args.From)
	}
	if s.priority <= args.ID {
		// defensive: senders only contact HIGHER priorities, so this means a
		// sender mis-ranked me. a node I don't outrank can't win while I live.
		reply.OK = false
	} else {
		reply.OK = true
		if s.status != statusCandidate {
			s.electionRound++
			round := s.electionRound
			s.status = statusCandidate
			go s.campaign(round) // if I'm already campaigning that one continues
		}
	}
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

// Victory: record the new coordinator and abandon my own election. this is
// unconditional in the textbook - with reliable delivery the announcements
// converge on the highest node alive.
func (s *ElectionService) Victory(args VictoryMsg, reply *VictoryReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("victory from unknown node %q", args.From)
	}
	s.coordinator = args.From
	if s.status == statusCandidate {
		s.electionRound++ // kills the campaign still in flight
		s.backedDown++
	}
	// note: a sitting coordinator also steps aside here, otherwise a healed
	// partition keeps two nodes with the leader role.
	s.status = statusFollower
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

// the action you trigger.

// StartElection runs one round from THIS node. you are the failure detector:
// click it when you believe the coordinator is down, or when you want to
// bully the current one. the exchange then runs by itself.
func (a *Application) StartElection(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	if s.status == statusCandidate {
		s.mu.Unlock()
		return fmt.Errorf("an election is already running here; let it finish or wait for the victory")
	}
	s.electionRound++
	round := s.electionRound
	s.status = statusCandidate
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)

	s.campaign(round) // the whole round: OKs, back-down or victory

	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	_ = s.report(*reply)
	return nil
}

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}

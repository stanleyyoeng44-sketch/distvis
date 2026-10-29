package main

import (
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

// call performs a peer RPC with a timeout.
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

type Empty struct{}

type ElectionMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"`
}
type ElectionReply struct {
	OK bool `json:"ok"` // true if the receiver outranks the sender
}

type CoordinatorMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"`
}
type CoordinatorReply struct{}

type State struct {
	Node          string `json:"node"`
	ID            int    `json:"id"`
	Role          string `json:"role"` // follower | candidate | coordinator
	Coordinator   string `json:"coordinator"`
	ElectionRound uint64 `json:"electionRound"`
	ElectionsWon  int    `json:"electionsWon"`
	BackedDown    int    `json:"backedDown"`
	Sent          uint64 `json:"sent"` // election messages sent (Election + Coordinator)
	LastError     string `json:"lastError"`
}

func (s *ElectionService) isMember(id string) bool {
	for _, member := range s.members {
		if member == id {
			return true
		}
	}
	return false
}

func (s *ElectionService) stateLocked() State {
	return State{
		Node: s.id, ID: s.priority, Role: s.role, Coordinator: s.coordinator,
		ElectionRound: s.electionRound, ElectionsWon: s.won, BackedDown: s.backedDown,
		Sent: s.sent, LastError: s.lastErr,
	}
}

func (s *ElectionService) bump() {
	s.mu.Lock()
	s.sent++
	_ = s.report(s.stateLocked())
	s.mu.Unlock()
}

// sendElection delivers one Election and returns the OK answer.
func (s *ElectionService) sendElection(to string, msg ElectionMsg) (ElectionReply, error) {
	client := s.peers[to]
	if client == nil {
		return ElectionReply{}, fmt.Errorf("no connection to %s", to)
	}
	s.bump()
	var reply ElectionReply
	err := call(client, "ElectionService.Election", msg, &reply)
	if err != nil {
		return ElectionReply{}, err
	} // Never read a reply still owned by a timed-out decoder.
	return reply, nil
}

// sendCoordinator broadcasts the winner announcement.
func (s *ElectionService) sendCoordinator(to string, msg CoordinatorMsg) {
	client := s.peers[to]
	if client == nil {
		return
	}
	s.bump()
	_ = call(client, "ElectionService.Coordinator", msg, &CoordinatorReply{})
}

// startElection begins one round from this node if none is already running.
func (s *ElectionService) startElection() {
	s.mu.Lock()
	if s.role == roleCandidate {
		s.mu.Unlock()
		return
	}
	s.electionRound++
	round := s.electionRound
	s.role = roleCandidate
	s.coordinator = ""
	_ = s.report(s.stateLocked())
	s.mu.Unlock()
	s.campaign(round)
}

// campaign runs one full round: ask every higher node, then back down or win.
func (s *ElectionService) campaign(round uint64) {
	s.mu.Lock()
	self := s.id
	message := ElectionMsg{From: s.id, ID: s.priority}
	higher := make([]string, 0, len(s.members))
	for _, member := range s.members {
		if priority(member) > s.priority {
			higher = append(higher, member)
		}
	}
	s.mu.Unlock()

	var workers sync.WaitGroup
	var countMu sync.Mutex
	okCount := 0
	for _, peer := range higher {
		workers.Add(1)
		go func(to string) {
			defer workers.Done()
			reply, err := s.sendElection(to, message)
			if err != nil {
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
	if s.electionRound != round || s.role != roleCandidate {
		s.mu.Unlock()
		return // a Coordinator arrived while we were asking; we stood down
	}
	if okCount > 0 {
		s.role = roleFollower
		s.waitingUntil = time.Now().Add(5 * time.Second)
		s.backedDown++
		_ = s.report(s.stateLocked())
		s.mu.Unlock()
		return
	}

	// Nobody higher answered: we are the highest node alive.
	s.role = roleCoordinator
	s.coordinator = s.id
	s.won++
	victory := CoordinatorMsg{From: s.id, ID: s.priority}
	peers := append([]string(nil), s.members...)
	_ = s.report(s.stateLocked())
	s.mu.Unlock()

	var deliver sync.WaitGroup
	for _, peer := range peers {
		if peer == self {
			continue
		}
		deliver.Add(1)
		go func(to string) {
			defer deliver.Done()
			s.sendCoordinator(to, victory)
		}(peer)
	}
	deliver.Wait()
}

// Election replies OK and starts the receiver's own election.
func (s *ElectionService) Election(args ElectionMsg, reply *ElectionReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("election from unknown node %q", args.From)
	}
	if s.priority <= args.ID {
		s.mu.Unlock()
		reply.OK = false
		return nil
	}
	reply.OK = true
	s.sent++ // OK is an algorithm message carried by the RPC response.
	_ = s.report(s.stateLocked())

	if s.role == roleCoordinator {
		coord := s.coordinator
		s.mu.Unlock()
		if coord != "" {
			// Reannounce to every member, not only the initiator. After healing,
			// another component may still believe its lower coordinator won.
			for _, peer := range s.members {
				if peer != s.id {
					go s.sendCoordinator(peer, CoordinatorMsg{From: coord, ID: priority(coord)})
				}
			}
		}
		return nil
	}

	started := false
	if s.role != roleCandidate {
		s.electionRound++
		round := s.electionRound
		s.role = roleCandidate
		go s.campaign(round)
		started = true
	}
	if started {
		_ = s.report(s.stateLocked())
	}
	s.mu.Unlock()
	return nil
}

// Coordinator records the announced coordinator.
func (s *ElectionService) Coordinator(args CoordinatorMsg, reply *CoordinatorReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("coordinator from unknown node %q", args.From)
	}
	if priority(args.From) <= s.priority {
		role, self := s.role, s.id
		s.mu.Unlock()
		if role == roleCoordinator {
			go s.sendCoordinator(args.From, CoordinatorMsg{From: self, ID: priority(self)})
		} else {
			go s.startElection()
		}
		return nil
	}

	changed := s.coordinator != args.From || s.role != roleFollower
	s.coordinator = args.From
	if s.role == roleCandidate {
		s.electionRound++ // kills the campaign still in flight
		s.backedDown++
	}
	s.role = roleFollower
	if changed {
		_ = s.report(s.stateLocked())
	}
	s.mu.Unlock()
	return nil
}

// StartElection runs one round from this node.
func (a *Application) StartElection(_ Empty, reply *State) error {
	s := a.service
	go s.startElection()
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

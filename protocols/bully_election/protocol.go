package main

import (
	"fmt"
	"sync"
)

// the bully algorithm (garcia-molina). "highest priority alive wins":
//
//  1. StartElection: I become candidate and send ELECTION to every HIGHER node.
//  2. each higher node answers OK and immediately starts its OWN election
//     (that's the bullying takeover).
//  3. any OK means I back down and wait for its victory.
//  4. nobody answered (all higher nodes are down) -> I am the highest alive,
//     broadcast VICTORY and become coordinator.
//  5. receiving VICTORY records the new coordinator and ends my election.
type Empty struct{}

type ElectionMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"` // sender priority, for logs and sanity checks
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
	Recent        []string `json:"recentEvents"`
	LastEvent     string   `json:"lastEvent"`
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

func (s *ElectionService) logLocked(format string, args ...any) {
	s.events = append(s.events, fmt.Sprintf("round %d: %s", s.electionRound, fmt.Sprintf(format, args...)))
	if len(s.events) > 12 {
		s.events = s.events[len(s.events)-12:]
	}
}

func (s *ElectionService) logEvent(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logLocked(format, args...)
}

func (s *ElectionService) stateLocked() State {
	recent := make([]string, len(s.events))
	copy(recent, s.events)
	last := ""
	if len(recent) > 0 {
		last = recent[len(recent)-1]
	}
	higher := make([]string, 0, len(s.members))
	for _, member := range s.members {
		if priority(member) > s.priority {
			higher = append(higher, member)
		}
	}
	return State{
		Node: s.id, ID: s.priority, Role: s.status, Coordinator: s.coordinator,
		ElectionRound: s.electionRound, ElectionsWon: s.won, BackedDown: s.backedDown,
		HigherPeers: higher, Recent: recent, LastEvent: last, LastError: s.lastErr,
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
				s.logEvent("no RPC connection to %s", to)
				return
			}
			var reply ElectionReply
			if err := client.Call("ElectionService.Election", message, &reply); err != nil {
				s.logEvent("ELECTION to %s failed: %v", to, err)
				return
			}
			if reply.OK {
				countMu.Lock()
				okCount++
				countMu.Unlock()
				s.logEvent("recv OK from %s: it outranks me", to)
			} else {
				s.logEvent("recv refusal from %s (it does not outrank me)", to)
			}
		}(peer)
	}
	workers.Wait()

	s.mu.Lock()
	if s.electionRound != round || s.status != statusCandidate {
		// a VICTORY arrived while I was campaigning, I already stood down
		s.logLocked("drop campaign result: superseded by a victory")
		s.mu.Unlock()
		return
	}
	if okCount > 0 {
		s.status = statusFollower
		s.backedDown++
		s.logLocked("BACKED DOWN: %d higher node(s) answered OK; wait for its victory", okCount)
		st := s.stateLocked()
		s.mu.Unlock()
		_ = s.report(st)
		return
	}

	// nobody higher answered -> I am the highest node ALIVE.
	s.status = statusCoordinator
	s.coordinator = s.id
	s.won++
	s.logLocked("WON the election; broadcast VICTORY to %d node(s)", len(s.members)-1)
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
				s.logEvent("no RPC connection to %s", to)
				return
			}
			if err := client.Call("ElectionService.Victory", victory, &VictoryReply{}); err != nil {
				s.logEvent("VICTORY to %s failed: %v", to, err)
			}
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
	s.logLocked("recv ELECTION from %s (priority %d)", args.From, args.ID)

	if s.priority <= args.ID {
		// defensive: senders only contact HIGHER priorities so this means a
		// sender mis-ranked me. a node I don't outrank can't win while I live.
		reply.OK = false
		s.logLocked("refuse OK to %s: I do not outrank it", args.From)
	} else {
		reply.OK = true
		s.logLocked("send OK to %s", args.From)
		if s.status != statusCandidate {
			s.electionRound++
			round := s.electionRound
			s.status = statusCandidate
			s.logLocked("BULLY %s: start my own election", args.From)
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
	stoodDown := ""
	if s.status == statusCandidate {
		s.electionRound++ // kills the campaign still in flight
		s.backedDown++
		stoodDown = ", stood down from my own election"
	} else if s.status == statusCoordinator {
		// somebody else just won: my leadership is over even though I never
		// lost a campaign. without this a healed partition keeps two leaders.
		stoodDown = ", stepped aside as coordinator"
	}
	s.status = statusFollower
	s.logLocked("recv VICTORY from %s%s; coordinator is now %s", args.From, stoodDown, args.From)
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
	s.logLocked("START election; send ELECTION to all higher-priority nodes")
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)

	s.campaign(round) // the whole round: OKs, back-down or victory

	s.mu.Lock()
	st = s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	*reply = st
	return nil
}

// Read just reports the state.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	*reply = s.stateLocked()
	st := *reply
	s.mu.Unlock()
	return s.report(st)
}

package main

import (
	"fmt"
	"sync"
)

// ─── The bully algorithm (Garcia-Molina) ─────────────────────────────────────
// "Highest priority alive wins." One election round works like this:
//
//	1. StartElection: I become a candidate and send ELECTION to every node
//	   with a HIGHER priority. (Nobody higher? I win immediately.)
//	2. Each higher node answers OK and — the bullying part — immediately
//	   starts its OWN election, because it outranks me and will take over.
//	3. If any OK arrives, I BACK DOWN and wait; the higher node will win.
//	4. If nobody answers (all higher nodes are down), I am the highest alive:
//	   I broadcast VICTORY and become coordinator.
//	5. Any node that receives VICTORY records the new coordinator and abandons
//	   its own election.
//
// Every StartElection therefore converges the cluster on the highest-priority
// node that is still alive — crash some nodes and the next-highest takes over.

// campaign runs one election round for the caller: contact every higher node,
// then either back down or win and announce victory. It is called either
// directly by the StartElection action or in the background when a lower node
// bullies this one into action. round identifies this campaign so a victory
// that arrives meanwhile can invalidate it (see ElectionService.Victory).
func (s *ElectionService) campaign(round uint64) {
	s.mu.Lock()
	self := s.id
	message := ElectionMsg{From: s.id, ID: s.priority}
	higher := s.higherPeersLocked()
	s.mu.Unlock()

	// Send ELECTION to all higher-priority nodes at the same time: one dead
	// node then costs one timeout instead of stalling the whole election. The
	// RPC REPLY is the OK, so each goroutine learns its verdict immediately.
	var workers sync.WaitGroup
	var countMu sync.Mutex // Guards okCount across goroutines.
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
			reply, err := callRPC[ElectionReply](client, "ElectionService.Election", message)
			if err != nil {
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
		// A VICTORY arrived while I was campaigning: I already stood down, so
		// this result is history and must not resurrect my candidacy.
		s.logLocked("drop campaign result: superseded by a victory")
		s.mu.Unlock()
		return
	}
	if okCount > 0 {
		s.status = statusFollower
		s.backedDown++
		s.logLocked("BACKED DOWN: %d higher node(s) answered OK; wait for its victory", okCount)
		state := s.stateLocked()
		s.mu.Unlock()
		_ = s.report(state)
		return
	}

	// No higher node answered: I am the highest-priority node ALIVE. The
	// election is over the moment this branch runs; VICTORY is just gossip.
	s.status = statusCoordinator
	s.coordinator = s.id
	s.won++
	s.logLocked("WON the election; broadcast VICTORY to %d node(s)", len(s.members)-1)
	victory := VictoryMsg{From: s.id, ID: s.priority}
	peers := append([]string(nil), s.members...)
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)

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
			if _, err := callRPC[VictoryReply](client, "ElectionService.Victory", victory); err != nil {
				s.logEvent("VICTORY to %s failed: %v", to, err)
			}
		}(peer)
	}
	deliver.Wait()
}

// ─── Incoming messages ───────────────────────────────────────────────────────

// Election handles ELECTION from another node. The classic rule: answer OK
// (I outrank you) and start MY OWN election — that is the bullying takeover.
// The reply is the classic OK the sender is waiting for.
func (s *ElectionService) Election(args ElectionMsg, reply *ElectionReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("election from unknown node %q", args.From)
	}
	s.logLocked("recv ELECTION from %s (priority %d)", args.From, args.ID)

	if s.priority <= args.ID {
		// Defensive: senders only contact HIGHER priorities, so this branch
		// means a sender mis-ranked me. A node I do not outrank can never win
		// while I live, so I do not hand it an OK.
		reply.OK = false
		s.logLocked("refuse OK to %s: I do not outrank it", args.From)
	} else {
		reply.OK = true // The classic OK: "I am alive and I outrank you".
		s.logLocked("send OK to %s", args.From)
		if s.status != statusCandidate {
			// Bullying: I take over by running my own election NOW. If I am
			// already campaigning, that campaign continues unchanged.
			s.electionRound++
			round := s.electionRound
			s.status = statusCandidate
			s.logLocked("BULLY %s: start my own election", args.From)
			go s.campaign(round)
		}
	}
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	return nil
}

// Victory handles VICTORY from the winner: record the new coordinator and
// abandon any election of my own. This is unconditional in the textbook —
// with reliable delivery the announcements always converge on the highest
// node alive, and a stuck situation can always be resolved by triggering one
// more StartElection (the README discusses the delayed-OK edge).
func (s *ElectionService) Victory(args VictoryMsg, reply *VictoryReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("victory from unknown node %q", args.From)
	}
	s.coordinator = args.From
	stoodDown := ""
	if s.status == statusCandidate {
		s.electionRound++ // Invalidates the campaign still in flight.
		s.backedDown++    // I abandoned my election, whichever reason applied.
		stoodDown = ", stood down from my own election"
	} else if s.status == statusCoordinator {
		// Somebody else just won: my leadership is over even though I never
		// lost a campaign. Without this demotion two nodes would keep the
		// "coordinator" role after a partition heals.
		stoodDown = ", stepped aside as coordinator"
	}
	s.status = statusFollower
	s.logLocked("recv VICTORY from %s%s; coordinator is now %s", args.From, stoodDown, args.From)
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	return nil
}

// ─── The action you trigger ──────────────────────────────────────────────────

// StartElection begins an election on THIS node. You are the failure detector:
// you click it when you believe the coordinator is down — or when you want to
// bully the current one. The ELECTION/OK/VICTORY exchange then runs by itself
// and this call returns with the outcome (coordinator, or backed down).
//
// StartElection is allowed even while a coordinator is known: that is exactly
// the "bully the leader" takeover demo.
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
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)

	s.campaign(round) // Runs the whole round: OKs, back-down or victory.

	s.mu.Lock()
	state = s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	*reply = state
	return nil
}

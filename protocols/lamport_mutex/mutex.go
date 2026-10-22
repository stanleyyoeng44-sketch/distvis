package main

import (
	"fmt"
	"sync"
)

// ─── Lamport's mutual exclusion algorithm ────────────────────────────────────
// One rule set, executed independently by every process:
//
//	REQUEST: tick; broadcast req(myTS, me); wait for a reply from ALL others.
//	ON req:  tick to max(L,ts)+1; queue it; reply now UNLESS I am in CS or my
//	         own pending request sorts before it — otherwise DEFER the reply.
//	ON ack:  tick; remember who replied; enter when everyone replied to me and
//	         my request is at the queue head.
//	RELEASE: remove my request from the queue; broadcast release; send every
//	         deferred reply that is now allowed.
//
// This is the version WITH the Release broadcast (3(n-1) messages per entry),
// exactly as the lecture states it. Ricart-Agrawala is the optimization that
// drops the Release message (2(n-1)) — see the lab's §4.3 vs §4.4.
//
// The decision "ack now or defer?" is ONE function (blockedByMeLocked), used
// identically when a request arrives and when a release frees the queue. That
// is the whole safety argument: a request is only ever blocked by a strictly
// earlier request, so requests enter in total order (TS, node).

// blockedByMeLocked reports whether THIS node must withhold its ack for the
// request (theirTS, theirNode). Callers hold s.mu.
func (s *MutexService) blockedByMeLocked(their requestKey) bool {
	if s.status == statusInCS {
		return true // The critical section itself: my request outranks everything.
	}
	if s.status == statusRequesting {
		// My pending request sorts first? Then I keep the ack until I am done.
		return before(requestKey{ts: s.myTS, node: s.id}, their)
	}
	return false // Idle: no reason to delay anyone.
}

func (s *MutexService) blockedByMeReasonLocked(their requestKey) string {
	if s.status == statusInCS {
		return "I am in the critical section"
	}
	return fmt.Sprintf("my request (%d,%s) is earlier", s.myTS, s.id)
}

// tryEnterLocked checks the entry condition after every state change: my
// request is at the queue head AND every other process has acked me. Callers
// hold s.mu. Entering is automatic — no action of yours is needed.
func (s *MutexService) tryEnterLocked() {
	if s.status != statusRequesting || len(s.acks) != len(s.members)-1 {
		return
	}
	head, ok := queueHead(s.queue)
	if !ok || key(head) != (requestKey{ts: s.myTS, node: s.id}) {
		// Ricart-Agrawala's proof says this can never fail once all acks are
		// in (no ack is sent while an earlier request exists). We check
		// anyway: the queue is the ground truth students can inspect.
		return
	}
	s.status = statusInCS
	s.entered = append(s.entered, head)
	s.logLocked("ENTER the critical section as req(%d,%s)", s.myTS, s.id)
}

// ─── Outgoing messages ───────────────────────────────────────────────────────
// Every send happens in its own goroutine so no handler ever holds s.mu while
// waiting for a network call (a handler that blocks would block the sender of
// its own reply — a classic self-deadlock).

func (s *MutexService) sendAck(to string, theirTS, ackTS uint64) {
	client := s.peers[to]
	if client == nil {
		s.logEvent("no RPC connection to %s", to)
		return
	}
	_, err := callRPC[AckReply](client, "MutexService.Ack", AckMsg{From: s.id, TS: ackTS, ForTS: theirTS})
	if err != nil {
		s.logEvent("ack to %s failed: %v", to, err)
	}
}

func (s *MutexService) sendRelease(to string, releaseTS uint64) {
	client := s.peers[to]
	if client == nil {
		s.logEvent("no RPC connection to %s", to)
		return
	}
	_, err := callRPC[ReleaseReply](client, "MutexService.Release", ReleaseMsg{From: s.id, TS: releaseTS})
	if err != nil {
		s.logEvent("release to %s failed: %v", to, err)
	}
}

func (s *MutexService) sendRequest(to string, reqTS uint64) {
	client := s.peers[to]
	if client == nil {
		s.logEvent("no RPC connection to %s", to)
		return
	}
	_, err := callRPC[ReqReply](client, "MutexService.Request", ReqMsg{From: s.id, TS: reqTS})
	if err != nil {
		s.logEvent("req to %s failed: %v", to, err)
	}
}

// logEvent is the lock-taking twin of logLocked, for goroutines that are NOT
// holding the state lock.
func (s *MutexService) logEvent(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logLocked(format, args...)
}

// ─── Incoming messages ───────────────────────────────────────────────────────

// Request handles req(ts, from): merge clocks, queue the request, then either
// ack immediately or defer. This is the receive half of a peer's RequestCS.
func (s *MutexService) Request(args ReqMsg, reply *ReqReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 {
		s.mu.Unlock()
		return fmt.Errorf("malformed request")
	}
	known := false
	for _, id := range s.members {
		known = known || id == args.From
	}
	if !known {
		s.mu.Unlock()
		return fmt.Errorf("request from unknown node %q", args.From)
	}

	// Rule 2: receiving merges clocks. The request's timestamp is now in my
	// past, and my clock moves beyond it.
	s.clock.onReceive(args.TS)
	s.logLocked("recv req(%d) from %s", args.TS, args.From)

	their := requestKey{ts: args.TS, node: args.From}
	s.queue = insertRequest(s.queue, QueueEntry{TS: args.TS, Node: args.From})

	if s.blockedByMeLocked(their) {
		s.deferred = append(s.deferred, QueueEntry{TS: args.TS, Node: args.From})
		s.logLocked("DEFER ack for req(%d,%s): %s", args.TS, args.From, s.blockedByMeReasonLocked(their))
	} else {
		// Sending the ack is an event of its own: one more tick, and the ack
		// carries the clock value of THIS event plus which request it approves.
		ackTS := s.clock.tick()
		s.logLocked("send ack(%d) to %s for req(%d)", ackTS, args.From, args.TS)
		go s.sendAck(args.From, args.TS, ackTS)
	}
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	return nil
}

// Ack handles "you may enter" for one of my requests.
func (s *MutexService) Ack(args AckMsg, reply *AckReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || args.ForTS == 0 {
		s.mu.Unlock()
		return fmt.Errorf("malformed ack")
	}
	s.clock.onReceive(args.TS)

	// Only an ack for the CURRENT request counts. A late ack for an old
	// request (I already entered and released) must never open the door for a
	// newer request — that would break mutual exclusion.
	if s.status != statusRequesting || args.ForTS != s.myTS {
		s.logLocked("ignore stale ack(%d) from %s for req(%d), my req is (%d)", args.TS, args.From, args.ForTS, s.myTS)
	} else {
		if s.acks == nil {
			s.acks = map[string]bool{}
		}
		s.acks[args.From] = true // A set: duplicates change nothing.
		s.logLocked("recv ack(%d) from %s (%d/%d)", args.TS, args.From, len(s.acks), len(s.members)-1)
		s.tryEnterLocked()
	}
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	return nil
}

// Release handles release(from): the departing request leaves the queue, and
// every deferred request that is no longer blocked by me gets its ack now.
func (s *MutexService) Release(args ReleaseMsg, reply *ReleaseReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 {
		s.mu.Unlock()
		return fmt.Errorf("malformed release")
	}
	s.clock.onReceive(args.TS)
	s.queue = removeRequest(s.queue, args.From)
	s.logLocked("recv release(%d) from %s; req(%d) left the queue", args.TS, args.From, args.TS)

	// Re-evaluate the deferred list with the SAME rule used on arrival: the
	// only thing that changed is that this request left the queue.
	still := s.deferred[:0]
	for _, entry := range s.deferred {
		their := key(entry)
		if s.blockedByMeLocked(their) {
			still = append(still, entry)
			continue
		}
		ackTS := s.clock.tick()
		s.logLocked("send ack(%d) to %s for req(%d) after release", ackTS, entry.Node, entry.TS)
		go s.sendAck(entry.Node, entry.TS, ackTS)
	}
	s.deferred = append([]QueueEntry(nil), still...)
	s.tryEnterLocked() // Defensive: entry needs acks, but re-check never hurts.
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	return nil
}

// ─── The actions you trigger ─────────────────────────────────────────────────

// RequestCS asks for the critical section. You trigger the REQUEST; the ack
// exchange then runs by itself, and the node enters CS automatically once
// every process has agreed and its request is first in the queue.
func (a *Application) RequestCS(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	// Only the two busy states block a new request; status starts at "idle".
	if s.status == statusRequesting || s.status == statusInCS {
		status := s.status
		s.mu.Unlock()
		return fmt.Errorf("a request is already %s; only idle nodes can request", status)
	}
	// Rule 1: the request event ticks the clock. ONE timestamp is assigned and
	// every copy of the broadcast carries it — the request is one logical
	// event, even though it travels in N-1 separate messages.
	s.myTS = s.clock.tick()
	s.status = statusRequesting
	s.acks = map[string]bool{}
	s.queue = insertRequest(s.queue, QueueEntry{TS: s.myTS, Node: s.id})
	s.logLocked("REQUEST CS as req(%d,%s); broadcast", s.myTS, s.id)
	self := s.id
	reqTS := s.myTS
	peers := append([]string(nil), s.members...)
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)

	// Broadcast outside the lock and wait for the sends to be handed over, so
	// when this action returns, every peer has been asked.
	var workers sync.WaitGroup
	for _, peer := range peers {
		if peer == self {
			continue
		}
		workers.Add(1)
		go func(to string) {
			defer workers.Done()
			s.sendRequest(to, reqTS)
		}(peer)
	}
	workers.Wait()

	// The first acks may already have arrived while we were broadcasting.
	// tryEnterLocked also covers a single-process cluster: zero acks needed.
	s.mu.Lock()
	s.tryEnterLocked()
	state = s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	*reply = state
	return nil
}

// Release leaves the critical section: my request leaves the queue, deferred
// acks are sent, and every peer hears the release. Only the node INSIDE the
// critical section may call it — that is the whole point of the algorithm.
func (a *Application) Release(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	if s.status != statusInCS {
		status := s.status
		s.mu.Unlock()
		return fmt.Errorf("only a node in the critical section can release (this node is %s)", status)
	}
	s.queue = removeRequest(s.queue, s.id)
	s.status = statusIdle
	s.myTS = 0
	s.acks = nil
	s.logLocked("RELEASE the critical section; queue opened")

	// Everything I deferred while requesting/in CS is now allowed: idle nodes
	// block nobody. The rule itself is re-checked per entry (blockedByMeLocked)
	// so the code would stay correct even if I had a new request already.
	still := s.deferred[:0]
	for _, entry := range s.deferred {
		if s.blockedByMeLocked(key(entry)) {
			still = append(still, entry)
			continue
		}
		ackTS := s.clock.tick()
		s.logLocked("send ack(%d) to %s for req(%d) after my release", ackTS, entry.Node, entry.TS)
		go s.sendAck(entry.Node, entry.TS, ackTS)
	}
	s.deferred = append([]QueueEntry(nil), still...)

	releaseTS := s.clock.tick() // The release event itself.
	self := s.id
	peers := append([]string(nil), s.members...)
	state := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)

	var workers sync.WaitGroup
	for _, peer := range peers {
		if peer == self {
			continue
		}
		workers.Add(1)
		go func(to string) {
			defer workers.Done()
			s.sendRelease(to, releaseTS)
		}(peer)
	}
	workers.Wait()

	s.mu.Lock()
	state = s.stateLocked()
	s.mu.Unlock()
	_ = s.report(state)
	*reply = state
	return nil
}

// Read reports this node's state without changing anything: the queue, the
// ack count, the deferred list and the last events.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	*reply = s.stateLocked()
	s.mu.Unlock()
	return s.report(*reply)
}

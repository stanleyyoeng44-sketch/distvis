package main

import (
	"fmt"
	"sort"
	"sync"
)

type Empty struct{}

// req(ts, from): "I want the critical section". TS is the timestamp of the
// request event - one broadcast, one timestamp, every copy identical.
type ReqMsg struct {
	From string `json:"from"`
	TS   uint64 `json:"ts"`
}
type ReqReply struct{} // the answer is a separate Ack message, not this reply

// ForTS names the request being approved so a late ack for an OLD request can
// never open the door for a new one. TS is the sender's clock (for merging).
type AckMsg struct {
	From  string `json:"from"`
	TS    uint64 `json:"ts"`
	ForTS uint64 `json:"forTS"`
}
type AckReply struct{}

type ReleaseMsg struct {
	From string `json:"from"`
	TS   uint64 `json:"ts"`
}
type ReleaseReply struct{}

type QueueEntry struct {
	TS   uint64 `json:"ts"`
	Node string `json:"node"`
}

type State struct {
	Node       string       `json:"node"`
	Clock      uint64       `json:"clock"`
	Status     string       `json:"status"` // idle | requesting | in-cs
	MyTS       uint64       `json:"myTS"`
	AcksHave   int          `json:"acksHave"`
	AcksNeed   int          `json:"acksNeed"` // always members-1
	Queue      []QueueEntry `json:"queue"`
	Deferred   []string     `json:"deferred"` // who is waiting for an ack from me
	EntryOrder []QueueEntry `json:"entryOrder"`
	Recent     []string     `json:"recentEvents"`
	LastEvent  string       `json:"lastEvent"`
	LastError  string       `json:"lastError"`
}

func key(e QueueEntry) requestKey { return requestKey{ts: e.TS, node: e.Node} }

// the queue is a SET of requests kept sorted by (ts,node). duplicates (a
// retransmitted message) are ignored.
func insertRequest(queue []QueueEntry, entry QueueEntry) []QueueEntry {
	for _, existing := range queue {
		if existing == entry {
			return queue
		}
	}
	position := sort.Search(len(queue), func(i int) bool {
		return before(key(entry), key(queue[i]))
	})
	queue = append(queue, QueueEntry{})
	copy(queue[position+1:], queue[position:])
	queue[position] = entry
	return queue
}

func removeRequest(queue []QueueEntry, node string) []QueueEntry {
	for i, entry := range queue {
		if entry.Node == node {
			return append(queue[:i], queue[i+1:]...)
		}
	}
	return queue
}

func queueHead(queue []QueueEntry) (QueueEntry, bool) {
	if len(queue) == 0 {
		return QueueEntry{}, false
	}
	return queue[0], true
}

func (s *MutexService) isMember(id string) bool {
	for _, member := range s.members {
		if member == id {
			return true
		}
	}
	return false
}

func (s *MutexService) logLocked(format string, args ...any) {
	entry := fmt.Sprintf("L=%d %s", s.clock.now, fmt.Sprintf(format, args...))
	s.events = append(s.events, entry)
	if len(s.events) > 12 {
		s.events = s.events[len(s.events)-12:]
	}
}

func (s *MutexService) logEvent(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logLocked(format, args...)
}

func (s *MutexService) stateLocked() State {
	recent := make([]string, len(s.events))
	copy(recent, s.events)
	last := ""
	if len(recent) > 0 {
		last = recent[len(recent)-1]
	}
	deferred := make([]string, len(s.deferred))
	for i, d := range s.deferred {
		deferred[i] = d.Node
	}
	queue := make([]QueueEntry, len(s.queue))
	copy(queue, s.queue)
	entered := make([]QueueEntry, len(s.entered))
	copy(entered, s.entered)
	return State{
		Node: s.id, Clock: s.clock.now, Status: s.status, MyTS: s.myTS,
		AcksHave: len(s.acks), AcksNeed: len(s.members) - 1,
		Queue: queue, Deferred: deferred, EntryOrder: entered,
		Recent: recent, LastEvent: last, LastError: s.lastErr,
	}
}

// the whole safety argument in one function: a request is only ever blocked
// by a strictly EARLIER request (or by me being in the cs), so requests enter
// in total order, one at a time.
func (s *MutexService) blockedByMeLocked(their requestKey) bool {
	if s.status == statusInCS {
		return true
	}
	if s.status == statusRequesting {
		return before(requestKey{ts: s.myTS, node: s.id}, their)
	}
	return false // idle nodes block nobody
}

func (s *MutexService) blockedByMeReasonLocked(their requestKey) string {
	if s.status == statusInCS {
		return "I am in the critical section"
	}
	return fmt.Sprintf("my request (%d,%s) is earlier", s.myTS, s.id)
}

// enter when everyone replied to me AND my request is at the queue head.
// the head check is technically redundant (the reply rule guarantees it) but
// the queue is what students can inspect, so check it anyway.
func (s *MutexService) tryEnterLocked() {
	if s.status != statusRequesting || len(s.acks) != len(s.members)-1 {
		return
	}
	head, ok := queueHead(s.queue)
	if !ok || key(head) != (requestKey{ts: s.myTS, node: s.id}) {
		return
	}
	s.status = statusInCS
	s.entered = append(s.entered, head)
	s.logLocked("ENTER the critical section as req(%d,%s)", s.myTS, s.id)
}

// outgoing messages run in their own goroutine: never hold the state lock
// while waiting for the network or you deadlock yourself.
func (s *MutexService) sendAck(to string, theirTS, ackTS uint64) {
	client := s.peers[to]
	if client == nil {
		s.logEvent("no RPC connection to %s", to)
		return
	}
	if err := client.Call("MutexService.Ack", AckMsg{From: s.id, TS: ackTS, ForTS: theirTS}, &AckReply{}); err != nil {
		s.logEvent("ack to %s failed: %v", to, err)
	}
}

func (s *MutexService) sendRelease(to string, releaseTS uint64) {
	client := s.peers[to]
	if client == nil {
		s.logEvent("no RPC connection to %s", to)
		return
	}
	if err := client.Call("MutexService.Release", ReleaseMsg{From: s.id, TS: releaseTS}, &ReleaseReply{}); err != nil {
		s.logEvent("release to %s failed: %v", to, err)
	}
}

func (s *MutexService) sendRequest(to string, reqTS uint64) {
	client := s.peers[to]
	if client == nil {
		s.logEvent("no RPC connection to %s", to)
		return
	}
	if err := client.Call("MutexService.Request", ReqMsg{From: s.id, TS: reqTS}, &ReqReply{}); err != nil {
		s.logEvent("req to %s failed: %v", to, err)
	}
}

// incoming messages.

// Request handles req(ts,from): merge clocks, queue it, then either reply
// now or defer the reply while I'm blocking it.
func (s *MutexService) Request(args ReqMsg, reply *ReqReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("request from unknown node %q", args.From)
	}
	s.clock.onReceive(args.TS) // rule 2: the request's ts is now in my past
	s.logLocked("recv req(%d) from %s", args.TS, args.From)
	their := requestKey{ts: args.TS, node: args.From}
	s.queue = insertRequest(s.queue, QueueEntry{TS: args.TS, Node: args.From})

	if s.blockedByMeLocked(their) {
		s.deferred = append(s.deferred, QueueEntry{TS: args.TS, Node: args.From})
		s.logLocked("DEFER ack for req(%d,%s): %s", args.TS, args.From, s.blockedByMeReasonLocked(their))
	} else {
		ackTS := s.clock.tick() // sending the ack is an event too
		s.logLocked("send ack(%d) to %s for req(%d)", ackTS, args.From, args.TS)
		go s.sendAck(args.From, args.TS, ackTS)
	}
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

// Ack handles "you may enter" for one of my requests.
func (s *MutexService) Ack(args AckMsg, reply *AckReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || args.ForTS == 0 || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("malformed ack")
	}
	s.clock.onReceive(args.TS)
	// only an ack for the CURRENT request counts
	if s.status != statusRequesting || args.ForTS != s.myTS {
		s.logLocked("ignore stale ack(%d) from %s for req(%d), my req is (%d)", args.TS, args.From, args.ForTS, s.myTS)
	} else {
		if s.acks == nil {
			s.acks = map[string]bool{}
		}
		s.acks[args.From] = true // it's a set, duplicates change nothing
		s.logLocked("recv ack(%d) from %s (%d/%d)", args.TS, args.From, len(s.acks), len(s.members)-1)
		s.tryEnterLocked()
	}
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

// Release handles release(from): the request leaves the queue and every
// deferred reply I no longer owe gets sent.
func (s *MutexService) Release(args ReleaseMsg, reply *ReleaseReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("malformed release")
	}
	s.clock.onReceive(args.TS)
	s.queue = removeRequest(s.queue, args.From)
	s.logLocked("recv release(%d) from %s; req(%d) left the queue", args.TS, args.From, args.TS)

	still := s.deferred[:0]
	for _, entry := range s.deferred {
		if s.blockedByMeLocked(key(entry)) {
			still = append(still, entry)
			continue
		}
		ackTS := s.clock.tick()
		s.logLocked("send ack(%d) to %s for req(%d) after release", ackTS, entry.Node, entry.TS)
		go s.sendAck(entry.Node, entry.TS, ackTS)
	}
	s.deferred = append([]QueueEntry(nil), still...)
	s.tryEnterLocked()
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

// the actions you trigger.

// RequestCS broadcasts req(myTS, me). entry happens AUTOMATICALLY once every
// process replied and my request is at the queue head.
func (a *Application) RequestCS(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	if s.status == statusRequesting || s.status == statusInCS {
		status := s.status
		s.mu.Unlock()
		return fmt.Errorf("a request is already %s; only idle nodes can request", status)
	}
	// rule 1: the request event ticks. one timestamp for the whole broadcast.
	s.myTS = s.clock.tick()
	s.status = statusRequesting
	s.acks = map[string]bool{}
	s.queue = insertRequest(s.queue, QueueEntry{TS: s.myTS, Node: s.id})
	s.logLocked("REQUEST CS as req(%d,%s); broadcast", s.myTS, s.id)
	self := s.id
	reqTS := s.myTS
	peers := append([]string(nil), s.members...)
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)

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
	workers.Wait() // by now every peer has been asked

	s.mu.Lock()
	s.tryEnterLocked()
	st = s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	*reply = st
	return nil
}

// Release leaves the critical section: my request leaves the queue, deferred
// acks go out and everyone hears the release. only the node IN the critical
// section may call it - that's the whole point of the algorithm.
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

	// everything I deferred while requesting/in cs is now allowed - idle
	// nodes block nobody. the rule is re-checked per entry just in case.
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

	releaseTS := s.clock.tick() // the release event itself
	self := s.id
	peers := append([]string(nil), s.members...)
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)

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
	st = s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	*reply = st
	return nil
}

// Read just reports the state: queue, ack count, deferred list, events.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	*reply = s.stateLocked()
	st := *reply
	s.mu.Unlock()
	return s.report(st)
}

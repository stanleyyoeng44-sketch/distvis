package main

import (
	"fmt"
	"net/rpc"
	"sort"
	"sync"
	"time"
)

// call bounds a peer operation so a dead node costs one timeout instead of a
// hang past the platform's ~10s command deadline. On timeout the reply must
// NOT be read.
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

// Request(ts, from): one broadcast, one timestamp, every copy identical.
type ReqMsg struct {
	From string `json:"from"`
	TS   uint64 `json:"ts"`
}
type ReqReply struct{} // the answer is a separate Ack, not this reply

// ForTS names the request being approved, so a late Ack for an OLD request can
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

type AcquireArgs struct {
	HoldMs float64 `json:"holdMs"` // time to hold the lock, milliseconds
}
type AutoArgs struct {
	Count int `json:"count"` // number of acquire cycles; 0 defaults to 5
}

type State struct {
	Node         string       `json:"node"`
	Clock        uint64       `json:"clock"`
	Role         string       `json:"role"` // idle | waiting | critical
	MyTS         uint64       `json:"myTS"`
	AcksHave     int          `json:"acksHave"`
	AcksNeed     int          `json:"acksNeed"`
	Queue        []QueueEntry `json:"queue"`
	Deferred     []string     `json:"deferred"` // who is waiting for an Ack from me
	Entries      int          `json:"entries"`
	LastWaitMs   float64      `json:"lastWaitMs"`
	MessagesSent uint64       `json:"messagesSent"`
	LastError    string       `json:"lastError"`
}

func entryLess(a, b QueueEntry) bool {
	if a.TS != b.TS {
		return a.TS < b.TS
	}
	return priority(a.Node) < priority(b.Node)
}

// The queue is a SET of requests sorted by (ts, node); duplicates (a
// retransmitted message) are ignored.
func insertRequest(queue []QueueEntry, entry QueueEntry) []QueueEntry {
	for _, existing := range queue {
		if existing == entry {
			return queue
		}
	}
	position := sort.Search(len(queue), func(i int) bool {
		return entryLess(entry, queue[i])
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

func (s *MutexService) stateLocked() State {
	deferred := make([]string, len(s.deferred))
	for i, d := range s.deferred {
		deferred[i] = d.Node
	}
	queue := make([]QueueEntry, len(s.queue))
	copy(queue, s.queue)
	return State{
		Node: s.id, Clock: s.clock.now, Role: s.role, MyTS: s.myTS,
		AcksHave: len(s.acks), AcksNeed: len(s.members) - 1,
		Queue: queue, Deferred: deferred,
		Entries: s.entries, LastWaitMs: s.lastWaitMS, MessagesSent: s.sent,
		LastError: s.lastErr,
	}
}

// A request is blocked only by a strictly earlier request (or by me being in
// the critical section), so requests enter in total order, one at a time.
func (s *MutexService) blockedByMeLocked(their QueueEntry) bool {
	if s.role == roleCritical {
		return true
	}
	if s.role == roleWaiting {
		return entryLess(QueueEntry{TS: s.myTS, Node: s.id}, their)
	}
	return false // idle nodes block nobody
}

// Enter when everyone replied to me AND my request is at the queue head. The
// head check is redundant (the reply rule guarantees it) but it is what you
// can inspect.
func (s *MutexService) tryEnterLocked() {
	if s.role != roleWaiting || len(s.acks) != len(s.members)-1 {
		return
	}
	head, ok := queueHead(s.queue)
	if !ok || head != (QueueEntry{TS: s.myTS, Node: s.id}) {
		return
	}
	s.role = roleCritical
	s.entries++
	s.lastWaitMS = float64(time.Since(s.waitStart).Milliseconds())
	if s.entered != nil {
		close(s.entered)
		s.entered = nil
	}
}

func (s *MutexService) bump() {
	s.mu.Lock()
	s.sent++
	s.mu.Unlock()
}

// send delivers one RPC and records it; a dead peer just gets a drop.
func (s *MutexService) send(to, method string, args, reply any) {
	client := s.peers[to]
	if client == nil {
		return
	}
	s.bump()
	_ = call(client, method, args, reply)
}

// --- incoming messages ------------------------------------------------------

// Request: merge clocks, queue the request, then reply now or defer the reply
// while I am blocking it.
func (s *MutexService) Request(args ReqMsg, reply *ReqReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("request from unknown node %q", args.From)
	}
	s.clock.onReceive(args.TS)
	their := QueueEntry{TS: args.TS, Node: args.From}
	s.queue = insertRequest(s.queue, their)

	if s.blockedByMeLocked(their) {
		s.deferred = append(s.deferred, their)
	} else {
		ackTS := s.clock.tick() // sending the Ack is an event too
		go s.send(args.From, "MutexService.Ack", AckMsg{From: s.id, TS: ackTS, ForTS: args.TS}, &AckReply{})
	}
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

func (s *MutexService) Ack(args AckMsg, reply *AckReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || args.ForTS == 0 || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("malformed ack")
	}
	s.clock.onReceive(args.TS)
	// only an Ack for the CURRENT request counts
	if s.role == roleWaiting && args.ForTS == s.myTS {
		if s.acks == nil {
			s.acks = map[string]bool{}
		}
		s.acks[args.From] = true
		s.tryEnterLocked()
	}
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}

// Release: the request leaves the queue and every deferred reply I no longer
// owe gets sent.
func (s *MutexService) Release(args ReleaseMsg, reply *ReleaseReply) error {
	s.mu.Lock()
	if args.From == "" || args.From == s.id || args.TS == 0 || !s.isMember(args.From) {
		s.mu.Unlock()
		return fmt.Errorf("malformed release")
	}
	s.clock.onReceive(args.TS)
	s.queue = removeRequest(s.queue, args.From)

	still := s.deferred[:0]
	for _, entry := range s.deferred {
		if s.blockedByMeLocked(entry) {
			still = append(still, entry)
			continue
		}
		go s.send(entry.Node, "MutexService.Ack", AckMsg{From: s.id, TS: s.clock.tick(), ForTS: entry.TS}, &AckReply{})
	}
	s.deferred = append([]QueueEntry(nil), still...)
	s.tryEnterLocked()
	st := s.stateLocked()
	s.mu.Unlock()
	_ = s.report(st)
	return nil
}


// acquire is the full request -> wait -> hold -> release cycle, shared by the
// Acquire action and Auto.
func (s *MutexService) acquire(holdMs float64) error {
	s.mu.Lock()
	if s.role != roleIdle {
		r := s.role
		s.mu.Unlock()
		return fmt.Errorf("cannot request: role is %s", r)
	}
	s.myTS = s.clock.tick() // the request event; one timestamp for the broadcast
	s.role = roleWaiting
	s.acks = map[string]bool{}
	s.queue = insertRequest(s.queue, QueueEntry{TS: s.myTS, Node: s.id})
	s.waitStart = time.Now()
	s.entered = make(chan struct{})
	entered := s.entered
	reqTS := s.myTS
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
			s.send(to, "MutexService.Request", ReqMsg{From: self, TS: reqTS}, &ReqReply{})
		}(peer)
	}
	workers.Wait()

	// Normally the last Ack handler flips us into the critical section. A
	// single-node group has no peers to Ack, so check once here.
	s.mu.Lock()
	s.tryEnterLocked()
	s.mu.Unlock()

	// Wait until every peer Acked and our request is at the queue head. If a
	// peer crashed this never completes, which is the point of fault scenario B.
	<-entered

	// Hold the critical section, then leave.
	time.Sleep(time.Duration(holdMs) * time.Millisecond)
	s.release()
	return nil
}

// release leaves the critical section: my request leaves the queue, deferred
// Acks go out, and everyone hears the Release.
func (s *MutexService) release() {
	s.mu.Lock()
	if s.role != roleCritical {
		s.mu.Unlock()
		return
	}
	s.queue = removeRequest(s.queue, s.id)
	s.role = roleIdle
	s.myTS = 0
	s.acks = nil

	// Idle nodes block nobody, so everything deferred is now allowed.
	still := s.deferred[:0]
	for _, entry := range s.deferred {
		if s.blockedByMeLocked(entry) {
			still = append(still, entry)
			continue
		}
		go s.send(entry.Node, "MutexService.Ack", AckMsg{From: s.id, TS: s.clock.tick(), ForTS: entry.TS}, &AckReply{})
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
			s.send(to, "MutexService.Release", ReleaseMsg{From: self, TS: releaseTS}, &ReleaseReply{})
		}(peer)
	}
	workers.Wait()
}

func (a *Application) Acquire(args AcquireArgs, reply *State) error {
	s := a.service
	holdMs := args.HoldMs
	if holdMs == 0 {
		holdMs = 2000
	}
	if holdMs <= 0 || holdMs > 120000 {
		return fmt.Errorf("holdMs must be in (0, 120000] ms")
	}
	if err := s.acquire(holdMs); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	_ = s.report(*reply)
	return nil
}

// Auto runs a sequence of Acquire cycles in the background.
func (a *Application) Auto(args AutoArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	if s.autoRunning {
		s.mu.Unlock()
		return fmt.Errorf("auto mode is already running")
	}
	s.autoRunning = true
	count := args.Count
	if count <= 0 {
		count = 5
	}
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.autoRunning = false
			s.mu.Unlock()
		}()
		for i := 0; i < count; i++ {
			if err := s.acquire(1000); err != nil {
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}

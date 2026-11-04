package main

import (
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

type Empty struct{}

type ReqMsg struct {
	From      string `json:"from"`
	TS        uint64 `json:"ts"`
	Algorithm string `json:"algorithm"`
}

type AckMsg struct {
	From      string `json:"from"`
	TS        uint64 `json:"ts"`
	ForTS     uint64 `json:"forTS"` // the request being acknowledged
	Algorithm string `json:"algorithm"`
}

type ReleaseMsg struct {
	From      string `json:"from"`
	TS        uint64 `json:"ts"`
	ForTS     uint64 `json:"forTS"` // remove this request, not a later one
	Algorithm string `json:"algorithm"`
}

type QueueEntry struct {
	TS   uint64 `json:"ts"`
	Node string `json:"node"`
}

type AcquireArgs struct {
	HoldMs float64 `json:"holdMs"` // 0 defaults to 2000 ms
}

type AutoArgs struct {
	Count int `json:"count"` // 0 defaults to 5 cycles; maximum 100
}

type AlgorithmArgs struct {
	Algorithm string `json:"algorithm"` // lamport or ricart-agrawala
}

type State struct {
	Node             string       `json:"node"`
	Algorithm        string       `json:"algorithm"`
	ReportSeq        uint64       `json:"reportSeq"`
	Clock            uint64       `json:"clock"`
	Role             string       `json:"role"`
	MyTS             uint64       `json:"myTS"`
	AcksHave         int          `json:"acksHave"`
	AcksNeed         int          `json:"acksNeed"`
	MissingAcks      []string     `json:"missingAcks"`
	Queue            []QueueEntry `json:"queue"`
	Deferred         []string     `json:"deferred"`
	Entries          int          `json:"entries"`
	Completed        int          `json:"completed"`
	LastWaitMs       float64      `json:"lastWaitMs"`
	MessagesSent     uint64       `json:"messagesSent"`
	SendErrors       uint64       `json:"sendErrors"`
	LastError        string       `json:"lastError"`
	Operation        string       `json:"operation"` // none | waiting | critical | releasing | completed | stopped
	OperationRunning bool         `json:"operationRunning"`
	AutoRunning      bool         `json:"autoRunning"`
}

func entryLess(a, b QueueEntry) bool {
	if a.TS != b.TS {
		return a.TS < b.TS
	}
	return priority(a.Node) < priority(b.Node)
}

func insertRequest(queue []QueueEntry, entry QueueEntry) []QueueEntry {
	for _, existing := range queue {
		if existing == entry {
			return queue
		}
	}
	position := sort.Search(len(queue), func(i int) bool { return entryLess(entry, queue[i]) })
	queue = append(queue, QueueEntry{})
	copy(queue[position+1:], queue[position:])
	queue[position] = entry
	return queue
}

func removeRequest(queue []QueueEntry, entry QueueEntry) []QueueEntry {
	for i, existing := range queue {
		if existing == entry {
			return append(queue[:i], queue[i+1:]...)
		}
	}
	return queue
}

func (s *MutexService) validPeer(id string) bool {
	if id == s.id {
		return false
	}
	for _, member := range s.members {
		if member == id {
			return true
		}
	}
	return false
}

func (s *MutexService) stateLocked() State {
	deferred := make([]string, 0, len(s.deferred))
	for _, entry := range s.deferred {
		deferred = append(deferred, entry.Node)
	}
	missing := make([]string, 0)
	if s.role == roleWaiting {
		for _, node := range s.members {
			if node != s.id && !s.acks[node] {
				missing = append(missing, node)
			}
		}
	}
	queue := make([]QueueEntry, len(s.queue))
	copy(queue, s.queue)
	return State{
		Node: s.id, Algorithm: s.algorithm, ReportSeq: s.reportSeq,
		Clock: s.clock.now, Role: s.role, MyTS: s.myTS,
		AcksHave: len(s.acks), AcksNeed: len(s.members) - 1, MissingAcks: missing,
		Queue: queue, Deferred: deferred, Entries: s.entries, Completed: s.completed,
		LastWaitMs: s.lastWaitMS, MessagesSent: s.sent, SendErrors: s.sendErrors,
		LastError: s.lastErr, Operation: s.operation,
		OperationRunning: s.operationRunning, AutoRunning: s.autoRunning,
	}
}

// Publish under the state lock: an old snapshot must never follow a newer one.
func (s *MutexService) reportLocked() error {
	s.reportSeq++
	err := s.report(s.stateLocked())
	if err != nil {
		log.Printf("%s state report: %v", s.id, err)
	}
	return err
}

func (s *MutexService) errorLocked(err error) error {
	s.lastErr = err.Error()
	_ = s.reportLocked()
	return err
}

// Both teaching modes defer replies to lower-priority requests. The Lamport
// mode additionally maintains the distributed queue and broadcasts Release.
func (s *MutexService) blockedByMeLocked(their QueueEntry) bool {
	return s.role == roleCritical || (s.role == roleWaiting &&
		entryLess(QueueEntry{TS: s.myTS, Node: s.id}, their))
}

func (s *MutexService) tryEnterLocked() {
	if s.role != roleWaiting || len(s.acks) != len(s.members)-1 {
		return
	}
	if s.algorithm == lamport && (len(s.queue) == 0 || s.queue[0] != (QueueEntry{TS: s.myTS, Node: s.id})) {
		return
	}
	s.role, s.operation = roleCritical, "critical"
	s.entries++
	s.lastWaitMS = float64(time.Since(s.waitStart).Microseconds()) / 1000
	_ = s.reportLocked()
	// The worker cannot leave before the critical report has been published.
	if s.entered != nil {
		close(s.entered)
		s.entered = nil
	}
}

// Count algorithm RPC attempts, not the RPC layer's empty responses. Never
// hold the state lock while waiting for a remote method to finish.
func (s *MutexService) send(to, method string, args any) {
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.sent++
	_ = s.reportLocked()
	client := s.peers[to]
	s.mu.Unlock()

	var err error
	if client == nil {
		err = fmt.Errorf("no client for peer")
	} else {
		call := client.Go(method, args, &Empty{}, nil)
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case result := <-call.Done:
			err = result.Error
		case <-timer.C:
			err = fmt.Errorf("no reply within 3s; remote outcome unknown")
		case <-s.ctx.Done():
			return
		}
	}
	if err != nil {
		s.mu.Lock()
		s.sendErrors++
		_ = s.errorLocked(fmt.Errorf("%s to %s: %w", method, to, err))
		s.mu.Unlock()
	}
}

func (s *MutexService) ackLocked(entry QueueEntry) {
	msg := AckMsg{From: s.id, TS: s.clock.tick(), ForTS: entry.TS, Algorithm: s.algorithm}
	go s.send(entry.Node, "MutexService.Ack", msg)
}

func (s *MutexService) Request(args ReqMsg, _ *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validPeer(args.From) || args.TS == 0 || args.Algorithm != s.algorithm {
		return s.errorLocked(fmt.Errorf("invalid request or algorithm mismatch from %q", args.From))
	}
	s.clock.onReceive(args.TS)
	if s.algorithm == lamport && args.TS <= s.released[args.From] {
		return s.reportLocked()
	}
	entry := QueueEntry{TS: args.TS, Node: args.From}
	if s.algorithm == lamport {
		s.queue = insertRequest(s.queue, entry)
	}
	if s.blockedByMeLocked(entry) {
		s.deferred = insertRequest(s.deferred, entry)
	} else {
		s.ackLocked(entry)
	}
	return s.reportLocked()
}

func (s *MutexService) Ack(args AckMsg, _ *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validPeer(args.From) || args.TS == 0 || args.ForTS == 0 || args.Algorithm != s.algorithm {
		return s.errorLocked(fmt.Errorf("invalid acknowledgement or algorithm mismatch"))
	}
	s.clock.onReceive(args.TS)
	if s.role == roleWaiting && args.ForTS == s.myTS {
		s.acks[args.From] = true
		s.tryEnterLocked()
	}
	return s.reportLocked()
}

func (s *MutexService) Release(args ReleaseMsg, _ *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validPeer(args.From) || args.TS == 0 || args.ForTS == 0 || args.Algorithm != s.algorithm || s.algorithm != lamport {
		return s.errorLocked(fmt.Errorf("invalid release or algorithm mismatch"))
	}
	s.clock.onReceive(args.TS)
	entry := QueueEntry{TS: args.ForTS, Node: args.From}
	s.queue = removeRequest(s.queue, entry)
	s.deferred = removeRequest(s.deferred, entry)
	if args.ForTS > s.released[args.From] {
		s.released[args.From] = args.ForTS
	}
	s.tryEnterLocked()
	return s.reportLocked()
}

// Reserve the request before returning from the application method, so a
// second Acquire cannot race the background worker and start another cycle.
func (s *MutexService) beginLocked() (<-chan struct{}, ReqMsg, error) {
	if s.ctx.Err() != nil {
		return nil, ReqMsg{}, s.ctx.Err()
	}
	if s.role != roleIdle || s.operationRunning {
		return nil, ReqMsg{}, fmt.Errorf("an acquisition is already %s", s.operation)
	}
	s.myTS = s.clock.tick()
	s.role, s.operation = roleWaiting, "waiting"
	s.operationRunning = true
	s.lastErr = ""
	s.acks = make(map[string]bool)
	if s.algorithm == lamport {
		s.queue = insertRequest(s.queue, QueueEntry{TS: s.myTS, Node: s.id})
	}
	s.waitStart = time.Now()
	s.entered = make(chan struct{})
	entered := s.entered
	_ = s.reportLocked()
	s.tryEnterLocked()
	return entered, ReqMsg{From: s.id, TS: s.myTS, Algorithm: s.algorithm}, nil
}

func (s *MutexService) broadcast(method string, args any) {
	var workers sync.WaitGroup
	for _, peer := range s.members {
		if peer == s.id {
			continue
		}
		workers.Add(1)
		go func(to string) {
			defer workers.Done()
			s.send(to, method, args)
		}(peer)
	}
	workers.Wait()
}

func (s *MutexService) runCycle(entered <-chan struct{}, request ReqMsg, hold time.Duration) bool {
	go s.broadcast("MutexService.Request", request)
	select {
	case <-entered:
	case <-s.ctx.Done():
		s.stopped()
		return false
	}
	timer := time.NewTimer(hold)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-s.ctx.Done():
		s.stopped()
		return false
	}
	s.release()
	return true
}

func (s *MutexService) stopped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operationRunning = false
	s.operation = "stopped"
	// Shutdown is not a successful distributed release; retain the last role.
	_ = s.reportLocked()
}

func (s *MutexService) release() {
	s.mu.Lock()
	requestTS := s.myTS
	s.queue = removeRequest(s.queue, QueueEntry{TS: requestTS, Node: s.id})
	s.role, s.operation = roleIdle, "releasing"
	s.myTS = 0
	s.acks = nil
	_ = s.reportLocked()
	for _, entry := range s.deferred {
		s.ackLocked(entry)
	}
	s.deferred = nil
	msg := ReleaseMsg{From: s.id, TS: s.clock.tick(), ForTS: requestTS, Algorithm: s.algorithm}
	_ = s.reportLocked()
	s.mu.Unlock()

	if msg.Algorithm == lamport {
		s.broadcast("MutexService.Release", msg)
	}
	s.mu.Lock()
	s.completed++
	s.operationRunning = false
	s.operation = "completed"
	_ = s.reportLocked()
	s.mu.Unlock()
}

func holdDuration(ms float64) (time.Duration, error) {
	if ms == 0 {
		ms = 2000
	}
	if math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 1 || ms > 120000 {
		return 0, fmt.Errorf("holdMs must be between 1 and 120000 ms (0 selects 2000)")
	}
	return time.Duration(ms * float64(time.Millisecond)), nil
}

// Success means accepted, not acquired. Observe operation/entries/completed.
func (a *Application) Acquire(args AcquireArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	hold, err := holdDuration(args.HoldMs)
	if err != nil {
		return s.errorLocked(err)
	}
	if s.autoRunning {
		return s.errorLocked(fmt.Errorf("auto mode owns the acquisition sequence"))
	}
	entered, request, err := s.beginLocked()
	if err != nil {
		return s.errorLocked(err)
	}
	*reply = s.stateLocked()
	go s.runCycle(entered, request, hold)
	return nil
}

func (a *Application) Auto(args AutoArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	count := args.Count
	if count == 0 {
		count = 5
	}
	if count < 1 || count > 100 {
		return s.errorLocked(fmt.Errorf("count must be between 1 and 100 (0 selects 5)"))
	}
	if s.autoRunning || s.operationRunning {
		return s.errorLocked(fmt.Errorf("an acquisition or auto sequence is already running"))
	}
	s.autoRunning = true
	entered, request, err := s.beginLocked()
	if err != nil {
		s.autoRunning = false
		return s.errorLocked(err)
	}
	*reply = s.stateLocked()
	go func() {
		defer func() {
			s.mu.Lock()
			s.autoRunning = false
			_ = s.reportLocked()
			s.mu.Unlock()
		}()
		for i := 0; i < count; i++ {
			if !s.runCycle(entered, request, time.Second) || i == count-1 {
				return
			}
			timer := time.NewTimer(time.Duration(100+rand.IntN(500)) * time.Millisecond)
			select {
			case <-timer.C:
			case <-s.ctx.Done():
				timer.Stop()
				return
			}
			s.mu.Lock()
			entered, request, err = s.beginLocked()
			if err != nil {
				_ = s.errorLocked(err)
			}
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return nil
}

// All nodes must choose the same mode before any protocol traffic. Changing
// just this switch implements the design comparison without a copied protocol.
func (a *Application) Configure(args AlgorithmArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if args.Algorithm != lamport && args.Algorithm != ricart {
		return s.errorLocked(fmt.Errorf("algorithm must be lamport or ricart-agrawala"))
	}
	if s.clock.now != 0 || s.operationRunning || s.autoRunning {
		return s.errorLocked(fmt.Errorf("choose the algorithm on every node before any requests; otherwise start a fresh run"))
	}
	s.algorithm = args.Algorithm
	if err := s.reportLocked(); err != nil {
		return err
	}
	*reply = s.stateLocked()
	return nil
}

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reportLocked(); err != nil {
		return err
	}
	*reply = s.stateLocked()
	return nil
}

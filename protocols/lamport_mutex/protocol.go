package main

import (
	"fmt"
	"net/rpc"
	"sort"
	"sync"
)

// ─── Messages ────────────────────────────────────────────────────────────────
// These plain structs are what nodes send each other (gob over net/rpc). All
// three carry a Lamport timestamp so every receiver can merge clocks (rule 2).

// Empty is a request with no fields at all (RequestCS, Release, Read).
type Empty struct{}

// ReqMsg is req(ts, from): "I want the critical section". TS is the timestamp
// of the REQUEST EVENT — one broadcast, one timestamp, every copy identical.
type ReqMsg struct {
	From string `json:"from"`
	TS   uint64 `json:"ts"`
}

// ReqReply is intentionally empty: in this algorithm the answer to a request
// is a separate Ack MESSAGE (so the platform draws it as its own arrow), not
// the return value of the request RPC.
type ReqReply struct{}

// AckMsg is "you may enter". ForTS names the request being acknowledged, so a
// late ack for an OLD request can never be mistaken for one for the current
// request. TS is the sender's clock when the ack was sent (clock propagation).
type AckMsg struct {
	From  string `json:"from"`
	TS    uint64 `json:"ts"`
	ForTS uint64 `json:"forTS"`
}
type AckReply struct{}

// ReleaseMsg is release(from): "my request is done; the queue is open again".
type ReleaseMsg struct {
	From string `json:"from"`
	TS   uint64 `json:"ts"`
}
type ReleaseReply struct{}

// QueueEntry is one request in a node's queue, ordered by (TS, node).
type QueueEntry struct {
	TS   uint64 `json:"ts"`
	Node string `json:"node"`
}

// State is what a node reports after every event. It is the window into the
// algorithm: the queue, who has acked, which acks this node is withholding,
// and a short log of the latest events.
type State struct {
	Node       string       `json:"node"`
	Clock      uint64       `json:"clock"`      // Current Lamport clock.
	Status     string       `json:"status"`     // idle | requesting | in-cs
	MyTS       uint64       `json:"myTS"`       // Timestamp of my request; 0 when none.
	AcksHave   int          `json:"acksHave"`   // Acks collected for the current request.
	AcksNeed   int          `json:"acksNeed"`   // Always members-1: everyone must agree.
	Queue      []QueueEntry `json:"queue"`      // Sorted by (TS, node); head goes first.
	Deferred   []string     `json:"deferred"`   // Nodes this node is withholding an ack from.
	EntryOrder []QueueEntry `json:"entryOrder"` // Requests in the order they entered CS.
	Recent     []string     `json:"recentEvents"`
	LastEvent  string       `json:"lastEvent"`
	LastError  string       `json:"lastError"`
}

// ─── Node state ──────────────────────────────────────────────────────────────

// MutexService is the complete state of one process. mu guards every field.
// There is no coordinator: every node runs the same code and decides for
// itself, using only the total order of request keys.
type MutexService struct {
	mu       sync.Mutex
	id       string
	members  []string
	peers    map[string]*rpc.Client // Owned by the runtime; never Close them here.
	report   func(any) error
	clock    lamportClock
	status   string // idle | requesting | in-cs
	myTS     uint64 // Timestamp of my current request; 0 when idle.
	acks     map[string]bool
	queue    []QueueEntry
	deferred []QueueEntry // Requests waiting for an ack from me.
	entered  []QueueEntry // Every request that entered CS, in entry order.
	events   []string     // Ring buffer of recent events for teaching.
	lastErr  string
}

// Application holds the actions you trigger from the platform UI.
type Application struct{ service *MutexService }

const (
	statusIdle       = "idle"
	statusRequesting = "requesting"
	statusInCS       = "in-cs"
)

// logLocked records one algorithm event (callers hold s.mu). The last 12 are
// kept so the UI shows how this node reached its current state.
func (s *MutexService) logLocked(format string, args ...any) {
	entry := fmt.Sprintf("L=%d %s", s.clock.now, fmt.Sprintf(format, args...))
	s.events = append(s.events, entry)
	if len(s.events) > 12 {
		s.events = s.events[len(s.events)-12:]
	}
}

// stateLocked builds the reported snapshot. Callers must hold s.mu. Slices are
// copied with make+copy so an empty list is reported as [] and never as null.
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
		Queue: queue, Deferred: deferred,
		EntryOrder: entered, Recent: recent, LastEvent: last, LastError: s.lastErr,
	}
}

// ─── Queue helpers ───────────────────────────────────────────────────────────
// The queue holds every request this node knows about, kept sorted by the
// total order (TS, node). The head is the request allowed to enter next.

func key(e QueueEntry) requestKey { return requestKey{ts: e.TS, node: e.Node} }

// insertRequest keeps the queue sorted. Duplicate requests (same TS and node,
// e.g. a retransmitted message) are ignored: the queue is a set of requests.
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

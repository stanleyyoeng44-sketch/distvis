package main

import (
	"fmt"
	"net/rpc"
	"os"      // IsNotExist distinguishes a first boot from an unreadable state file.
	"slices"  // Equal checks ordered membership, not just its size.
	"strings" // TrimSpace helps reject labels containing only whitespace.
	"sync"

	"distvis/sdk" // Save/Load persist counters across a node crash in the same run.
	lab "distvis/sdk/rpc"
)

// Modeled events are explicit Local calls and DATA-message sends/receives.
// Read, Compare, coordinator commands, Report, and net/rpc acknowledgements
// are observation/control traffic, NOT modeled events. Consequently the vector
// clocks describe this selected event graph, not every CPU/RPC action. Modeling
// replies as application messages too would require stamping and merging them,
// adding events not present in the lecture's six-event example.
type Empty struct{}
type LocalArgs struct {
	Label string `json:"label"`
}
type SendArgs struct {
	To           string `json:"to" distvis:"node"`
	Label        string `json:"label"`        // Label on the sending node (e.g. b).
	ReceiveLabel string `json:"receiveLabel"` // Label on the receiving node (e.g. c).
	Payload      string `json:"payload"`
}
type CompareArgs struct {
	Left  []uint64 `json:"left"` // JSON array, in the membership order shown in State.
	Right []uint64 `json:"right"`
}
type CompareReply struct {
	Relation string `json:"relation"`
	Note     string `json:"note"`
}

// Event is an immutable history record once constructed. Its ID is based on
// our own vector component, which counts every modeled local event and is
// persisted before we publish or send it. User labels need not be unique.
type Event struct {
	ID        string    `json:"id"`
	Node      string    `json:"node"`
	Kind      string    `json:"kind"`
	Label     string    `json:"label"`
	Peer      string    `json:"peer,omitempty"`
	MessageID string    `json:"messageID,omitempty"`
	Payload   string    `json:"payload,omitempty"`
	Timestamp Timestamp `json:"timestamp"`
}
type Message struct {
	From         string    `json:"from"`
	ID           string    `json:"id"`
	ReceiveLabel string    `json:"receiveLabel"`
	Payload      string    `json:"payload"`
	Timestamp    Timestamp `json:"timestamp"` // Piggybacks BOTH clocks on the same data message.
}
type Ack struct {
	Recorded bool   `json:"recorded"`
	EventID  string `json:"eventID"`
	// Deliberately no timestamp: this is transport confirmation, not a data event.
}
type Delivery struct {
	MessageID string `json:"messageID"`
	To        string `json:"to"`
	Confirmed bool   `json:"confirmed"`
	Error     string `json:"error,omitempty"`
}
type SendReply struct {
	Event    Event    `json:"event"`
	Delivery Delivery `json:"delivery"`
}

// DiskState contains durable algorithm state only, not network clients, locks
// or function values (those cannot be meaningfully serialized). Saving clocks
// before emitting a message prevents counter reuse after a crash/recovery.
type DiskState struct {
	Node    string    `json:"node"`
	Members []string  `json:"members"`
	Clock   Timestamp `json:"clock"`
	Events  []Event   `json:"events"`
}
type State struct {
	DiskState
	LastDelivery *Delivery `json:"lastDelivery"`
	StorageError string    `json:"storageError"`
}
type ClockService struct {
	mu           sync.Mutex
	data         DiskState
	lastDelivery *Delivery
	storageError string
	index        int
	peers        map[string]*rpc.Client
	save         func(any) error // Injected functions keep tests independent of the filesystem/UI.
	report       func(any) error
}
type Application struct{ service *ClockService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	index := 0
	for i, id := range node.Nodes {
		if id == node.ID {
			index = i
		}
	}
	s := &ClockService{index: index, peers: peers, save: sdk.Save, report: node.Report,
		data: DiskState{Node: node.ID, Members: append([]string(nil), node.Nodes...),
			Clock: Timestamp{Vector: make([]uint64, len(node.Nodes))}, Events: []Event{}}}
	// Load decodes JSON into the pointed-to object. A missing file is expected
	// on the first boot; malformed JSON/permission errors are NOT "fresh state".
	if err := sdk.Load(&s.data); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("restore logical clock: %w", err) // %w wraps an error for later inspection.
	}
	if s.data.Node != node.ID || !slices.Equal(s.data.Members, node.Nodes) {
		return fmt.Errorf("stored state belongs to a different node or membership")
	}
	if err := validateVector(s.data.Clock.Vector, len(node.Nodes)); err != nil {
		return fmt.Errorf("stored vector: %w", err)
	}
	if s.data.Clock.Lamport > maxCounter {
		return fmt.Errorf("stored Lamport counter exceeds limit")
	}
	if err := node.RegisterName("ClockService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	return s.report(s.stateLocked())
}

// stateLocked returns a snapshot. Event/vector arrays are never modified after
// publication; commitLocked allocates a fresh history array on every event.
// Thus returning a slice header here does not introduce aliasing races with a
// later RPC serializer, which runs after we release mu.
func (s *ClockService) stateLocked() State {
	return State{DiskState: s.data, LastDelivery: s.lastDelivery, StorageError: s.storageError}
}
func labelOK(label string) bool { return strings.TrimSpace(label) != "" && len(label) <= 128 }

// commitLocked is used by all THREE event kinds. One central mutation path
// prevents a send or receive from accidentally incrementing only one clock.
// The caller must hold mu for advance + persist + report as one local action.
func (s *ClockService) commitLocked(kind, label, peer, messageID, payload string, incoming *Timestamp) (Event, error) {
	if s.storageError != "" {
		return Event{}, fmt.Errorf("node stopped recording after storage failure: %s", s.storageError)
	}
	next, err := advance(s.data.Clock, s.index, incoming)
	if err != nil {
		return Event{}, err
	}
	event := Event{ID: fmt.Sprintf("%s:%d", s.data.Node, next.Vector[s.index]), Node: s.data.Node,
		Kind: kind, Label: label, Peer: peer, MessageID: messageID, Payload: payload, Timestamp: next}
	if kind == "send" {
		event.MessageID = event.ID
	}
	s.data.Clock = next
	// Keep the most recent 64 events. Older events remain in DistVis's run
	// history, but we must not repeatedly report unbounded state to stdout.
	events := append([]Event(nil), s.data.Events...)
	events = append(events, event)
	if len(events) > 64 {
		events = events[len(events)-64:]
	}
	s.data.Events = events
	if err := s.save(s.data); err != nil {
		// Fail closed: do not send/acknowledge an event that was not durably
		// recorded, and do not continue generating possibly reusable counters.
		s.storageError = err.Error()
		_ = s.report(s.stateLocked()) // Best-effort diagnostics; preserve the storage error.
		return Event{}, fmt.Errorf("persist event: %w", err)
	}
	if err := s.report(s.stateLocked()); err != nil {
		return Event{}, err // Durable event remains; reporting is not a rollback.
	}
	return event, nil
}

func (a *Application) Local(args LocalArgs, reply *Event) error {
	if !labelOK(args.Label) {
		return fmt.Errorf("label must contain non-whitespace text and at most 128 bytes")
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	event, err := s.commitLocked("local", args.Label, "", "", "", nil)
	*reply = event
	return err
}

func (a *Application) Send(args SendArgs, reply *SendReply) error {
	s := a.service
	client := s.peers[args.To]
	if client == nil || !labelOK(args.Label) || !labelOK(args.ReceiveLabel) || len(args.Payload) > 512 {
		return fmt.Errorf("choose another member, two nonempty labels <=128 bytes, and payload <=512 bytes")
	}
	s.mu.Lock()
	event, err := s.commitLocked("send", args.Label, args.To, "", args.Payload, nil)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// Network I/O happens AFTER unlocking, so two nodes can send to each other
	// concurrently without both holding a lock needed by the other's receiver.
	ack, callErr := callRPC[Ack](client, "ClockService.Deliver", Message{From: event.Node, ID: event.ID,
		ReceiveLabel: args.ReceiveLabel, Payload: args.Payload, Timestamp: event.Timestamp})
	delivery := Delivery{MessageID: event.ID, To: args.To, Confirmed: callErr == nil && ack.Recorded}
	if callErr != nil {
		delivery.Error = callErr.Error()
	} else if !ack.Recorded {
		delivery.Error = "receiver did not confirm recording the event"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastDelivery = &delivery
	*reply = SendReply{Event: event, Delivery: delivery}
	// A successful command means a SEND EVENT was recorded. Confirmed is the
	// separate delivery outcome. Timeout leaves the send in history: receipt
	// might have happened but its acknowledgment might have been lost.
	return s.report(s.stateLocked())
}

func (s *ClockService) Deliver(args Message, reply *Ack) error {
	if s.peers[args.From] == nil || args.ID == "" || len(args.ID) > 128 ||
		!labelOK(args.ReceiveLabel) || len(args.Payload) > 512 {
		return fmt.Errorf("invalid sender, message ID, receive label or payload")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateVector(args.Timestamp.Vector, len(s.data.Members)); err != nil {
		return err
	}
	// A message cannot legitimately know more events from THIS process than
	// this process has recorded. Such a vector indicates corrupt/reused state
	// or a malformed message, not a reason to silently invent local events.
	if args.Timestamp.Vector[s.index] > s.data.Clock.Vector[s.index] {
		return fmt.Errorf("message claims unknown events from the receiving process")
	}
	event, err := s.commitLocked("receive", args.ReceiveLabel, args.From, args.ID, args.Payload, &args.Timestamp)
	if err != nil {
		return err
	}
	*reply = Ack{Recorded: true, EventID: event.ID}
	return nil
}

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply) // Observation is not a modeled event; no increment or Save.
}
func (a *Application) Compare(args CompareArgs, reply *CompareReply) error {
	size := len(a.service.data.Members) // Immutable membership needs no lock.
	if err := validateVector(args.Left, size); err != nil {
		return err
	}
	if err := validateVector(args.Right, size); err != nil {
		return err
	}
	r, err := relation(args.Left, args.Right)
	if err != nil {
		return err
	}
	*reply = CompareReply{Relation: r,
		Note: "Vector relation describes modeled data events. Lamport L(a)<L(b), or N*L+i ordering, alone does NOT prove causality."}
	return nil
}

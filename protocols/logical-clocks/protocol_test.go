package main

import (
	"encoding/json" // Marshal/Unmarshal emulate persisted snapshots without creating real state files.
	"errors"
	"net"
	"net/rpc"
	"reflect" // DeepEqual compares nested slices/structs by value in assertions.
	"sync"
	"testing"
)

// Three members begin with zero vectors. This test reproduces lecture sections
// 3.2 and 3.4 exactly, including the zero-based N*L+i tie-breaker.
func TestLectureSixEvents(t *testing.T) {
	states := []Timestamp{{Vector: make([]uint64, 3)}, {Vector: make([]uint64, 3)}, {Vector: make([]uint64, 3)}}
	events := map[string]Timestamp{}
	tick := func(label string, index int, incoming *Timestamp) {
		t.Helper()
		next, err := advance(states[index], index, incoming)
		if err != nil {
			t.Fatal(err)
		}
		states[index], events[label] = next, next
	}
	tick("a", 0, nil)
	tick("e", 2, nil)
	tick("b", 0, nil)
	b := events["b"]
	tick("c", 1, &b)
	tick("d", 1, nil)
	d := events["d"]
	tick("f", 2, &d)
	want := map[string]Timestamp{
		"a": {Lamport: 1, Vector: []uint64{1, 0, 0}, Total: 3},
		"b": {Lamport: 2, Vector: []uint64{2, 0, 0}, Total: 6},
		"c": {Lamport: 3, Vector: []uint64{2, 1, 0}, Total: 10},
		"d": {Lamport: 4, Vector: []uint64{2, 2, 0}, Total: 13},
		"e": {Lamport: 1, Vector: []uint64{0, 0, 1}, Total: 5},
		"f": {Lamport: 5, Vector: []uint64{2, 2, 2}, Total: 17},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lecture trace mismatch:\ngot  %#v\nwant %#v", events, want)
	}
}
func TestVectorPartialOrder(t *testing.T) {
	cases := []struct {
		left, right []uint64
		want        string
	}{
		{[]uint64{2, 0, 0}, []uint64{2, 2, 2}, "happens-before"},
		{[]uint64{2, 2, 2}, []uint64{2, 0, 0}, "happens-after"},
		{[]uint64{2, 0, 0}, []uint64{0, 0, 1}, "concurrent"},
		{[]uint64{1, 0, 0}, []uint64{1, 0, 0}, "equal"},
	}
	for _, tc := range cases {
		got, err := relation(tc.left, tc.right)
		if err != nil || got != tc.want {
			t.Errorf("relation(%v,%v)=%q,%v; want %q", tc.left, tc.right, got, err, tc.want)
		}
	}
	if _, err := relation([]uint64{1, 0}, []uint64{1, 0, 0}); err == nil {
		t.Fatal("different memberships cannot be compared")
	}
}
func TestAdvanceDoesNotAliasAndRejectsOverflow(t *testing.T) {
	current := Timestamp{Lamport: 1, Vector: []uint64{1, 0, 0}, Total: 3}
	incoming := Timestamp{Lamport: 8, Vector: []uint64{0, 3, 0}}
	next, err := advance(current, 0, &incoming)
	if err != nil || next.Lamport != 9 || !reflect.DeepEqual(next.Vector, []uint64{2, 3, 0}) {
		t.Fatalf("merge then increment failed: %+v, %v", next, err)
	}
	if current.Vector[0] != 1 || incoming.Vector[0] != 0 {
		t.Fatal("advancing one clock mutated an old event/message")
	}
	if _, err := advance(Timestamp{Lamport: maxCounter, Vector: []uint64{0, 0}}, 0, nil); err == nil {
		t.Fatal("counter overflow must not wrap")
	}
	if _, err := advance(current, 0, &Timestamp{Vector: []uint64{1, 2}}); err == nil {
		t.Fatal("malformed incoming vector accepted")
	}
}

func newService(index int) *ClockService {
	members := []string{"node-1", "node-2", "node-3"}
	return &ClockService{index: index, peers: map[string]*rpc.Client{},
		save: func(any) error { return nil }, report: func(any) error { return nil },
		data: DiskState{Node: members[index], Members: members,
			Clock: Timestamp{Vector: make([]uint64, 3)}, Events: []Event{}}}
}
func TestPersistencePrecedesPublicationAndSurvivesRestore(t *testing.T) {
	s := newService(0)
	var saved []byte
	s.save = func(value any) error {
		var err error
		saved, err = json.Marshal(value)
		return err
	}
	s.report = func(any) error {
		if len(saved) == 0 {
			t.Error("reported event before persistence")
		}
		return nil
	}
	var first Event
	if err := (&Application{service: s}).Local(LocalArgs{Label: "before crash"}, &first); err != nil {
		t.Fatal(err)
	}
	// A restarted node recreates connections/mutexes but loads its counters.
	restarted := newService(0)
	if err := json.Unmarshal(saved, &restarted.data); err != nil {
		t.Fatal(err)
	}
	var second Event
	if err := (&Application{service: restarted}).Local(LocalArgs{Label: "after crash"}, &second); err != nil {
		t.Fatal(err)
	}
	if second.Timestamp.Lamport != 2 || second.ID == first.ID {
		t.Fatal("restore reused a timestamp or event ID")
	}
}
func TestStorageFailureStopsFurtherEvents(t *testing.T) {
	s := newService(0)
	s.save = func(any) error { return errors.New("disk unavailable") }
	app := &Application{service: s}
	if err := app.Local(LocalArgs{Label: "failed persistence"}, &Event{}); err == nil {
		t.Fatal("storage error was ignored")
	}
	before := s.data.Clock.Lamport
	if err := app.Local(LocalArgs{Label: "must not proceed"}, &Event{}); err == nil || s.data.Clock.Lamport != before {
		t.Fatal("node kept issuing timestamps after failed persistence")
	}
}
func TestConcurrentLocalEventsAndReadSnapshots(t *testing.T) {
	s := newService(0)
	app := &Application{service: s}
	var snapshot State
	if err := app.Read(Empty{}, &snapshot); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 80; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := app.Local(LocalArgs{Label: "concurrent"}, &Event{}); err != nil {
				t.Error(err)
			}
			// Serialize an old snapshot while new events are being recorded.
			// -race detects accidental reuse of a mutable backing array.
			if _, err := json.Marshal(snapshot); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if s.data.Clock.Lamport != 80 || s.data.Clock.Vector[0] != 80 || len(s.data.Events) != 64 {
		t.Fatalf("lost increments or unbounded history: %+v", s.data.Clock)
	}
	if snapshot.Clock.Vector[0] != 0 {
		t.Fatal("old snapshot was mutated")
	}
}
func TestRealRPCSendReceiveCountsNoAckEvent(t *testing.T) {
	sender, receiver := newService(0), newService(1)
	// Deliver checks membership via this map; only the sender's connection is
	// actually used in this test. Runtime provides all clients in real runs.
	receiver.peers["node-1"] = new(rpc.Client)
	server := rpc.NewServer()
	if err := server.RegisterName("ClockService", receiver); err != nil {
		t.Fatal(err)
	}
	local, remote := net.Pipe()
	client := rpc.NewClient(local)
	sender.peers["node-2"] = client
	go server.ServeConn(remote)
	t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
	var result SendReply
	if err := (&Application{service: sender}).Send(SendArgs{To: "node-2", Label: "send", ReceiveLabel: "receive"}, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Delivery.Confirmed || sender.data.Clock.Lamport != 1 || receiver.data.Clock.Lamport != 2 {
		t.Fatalf("send/receive/ack event counts incorrect: %+v", result)
	}
	if !reflect.DeepEqual(receiver.data.Clock.Vector, []uint64{1, 1, 0}) {
		t.Fatalf("receive vector = %v", receiver.data.Clock.Vector)
	}
}

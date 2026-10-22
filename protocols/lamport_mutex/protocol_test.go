package main

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"testing"
	"time"
)

// ─── Pure logic ──────────────────────────────────────────────────────────────

func TestClockRules(t *testing.T) {
	c := lamportClock{now: 2}
	if got := c.tick(); got != 3 {
		t.Fatalf("tick: got %d, want 3", got)
	}
	// Rule 2: max(local, remote) + 1 — the receiver overtakes the sender.
	if got := c.onReceive(10); got != 11 {
		t.Fatalf("onReceive(10): got %d, want 11", got)
	}
	// A message from the past still ticks, but never rewinds.
	if got := c.onReceive(4); got != 12 {
		t.Fatalf("onReceive(4): got %d, want 12", got)
	}
}

func TestQueueTotalOrder(t *testing.T) {
	var queue []QueueEntry
	queue = insertRequest(queue, QueueEntry{TS: 5, Node: "node-2"})
	queue = insertRequest(queue, QueueEntry{TS: 5, Node: "node-1"}) // tie -> id decides
	queue = insertRequest(queue, QueueEntry{TS: 3, Node: "node-3"})
	queue = insertRequest(queue, QueueEntry{TS: 5, Node: "node-1"}) // duplicate ignored
	want := []QueueEntry{{TS: 3, Node: "node-3"}, {TS: 5, Node: "node-1"}, {TS: 5, Node: "node-2"}}
	if len(queue) != len(want) {
		t.Fatalf("queue = %+v", queue)
	}
	for i := range want {
		if queue[i] != want[i] {
			t.Fatalf("queue[%d] = %+v, want %+v", i, queue[i], want[i])
		}
	}
	queue = removeRequest(queue, "node-1")
	if head, ok := queueHead(queue); !ok || head != (QueueEntry{TS: 3, Node: "node-3"}) {
		t.Fatalf("head after removal = %+v", head)
	}
}

func TestStaleAckIsIgnored(t *testing.T) {
	s := &MutexService{id: "node-1", members: []string{"node-1", "node-2"},
		peers: map[string]*rpc.Client{}, queue: []QueueEntry{}, report: func(any) error { return nil }}
	s.clock.now = 5
	s.myTS, s.status, s.acks = 5, statusRequesting, map[string]bool{}
	s.queue = insertRequest(s.queue, QueueEntry{TS: 5, Node: "node-1"})

	// An ack for an OLD request must not open the door for the current one.
	if err := s.Ack(AckMsg{From: "node-2", TS: 9, ForTS: 4}, &AckReply{}); err != nil {
		t.Fatal(err)
	}
	if st := snap(s); st.AcksHave != 0 || st.Status != statusRequesting {
		t.Fatalf("stale ack was counted: %+v", st)
	}
	// The right ack enters immediately (2-node cluster: one ack is enough).
	if err := s.Ack(AckMsg{From: "node-2", TS: 10, ForTS: 5}, &AckReply{}); err != nil {
		t.Fatal(err)
	}
	st := snap(s)
	if st.AcksHave != 1 || st.Status != statusInCS {
		t.Fatalf("valid ack ignored: %+v", st)
	}
	// Clock merged both receives: 5 -> max(5,9)+1=10 -> max(10,10)+1=11.
	if st.Clock != 11 {
		t.Fatalf("clock = %d, want 11", st.Clock)
	}
}

func TestActionGuards(t *testing.T) {
	s := &MutexService{id: "node-1", members: []string{"node-1"},
		peers: map[string]*rpc.Client{}, queue: []QueueEntry{}, status: statusIdle,
		report: func(any) error { return nil }}
	app := &Application{service: s}
	var state State
	if err := app.Release(Empty{}, &state); err == nil {
		t.Fatal("an idle node must not be able to release")
	}
	if err := app.RequestCS(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	if st := snap(s); st.Status != statusInCS {
		t.Fatalf("a single-process cluster enters without acks: %+v", st)
	}
	if err := app.RequestCS(Empty{}, &state); err == nil {
		t.Fatal("a second request while in-cs must be refused")
	}
	if err := app.Release(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	if st := snap(s); st.Status != statusIdle || st.MyTS != 0 {
		t.Fatalf("release must reset the request: %+v", st)
	}
}

// ─── Real net/rpc scenarios ──────────────────────────────────────────────────

func TestDeferredAckOpensOnRelease(t *testing.T) {
	nodes := cluster(t, 2)
	a, b := nodes[0], nodes[1]
	appA, appB := &Application{service: a}, &Application{service: b}

	// a requests first and enters: b is idle, so its ack arrives immediately.
	var state State
	if err := appA.RequestCS(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "a in CS", func() bool { return snap(a).Status == statusInCS })

	// b requests while a is inside: a must DEFER its ack, so b cannot enter.
	if err := appB.RequestCS(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "a defers b", func() bool {
		st := snap(a)
		return len(st.Deferred) == 1 && st.Deferred[0] == "node-2"
	})
	if st := snap(b); st.Status == statusInCS || st.AcksHave != 0 {
		t.Fatalf("b entered without a's ack: %+v", st)
	}

	// a releases: the deferred ack is sent and b enters afterwards.
	if err := appA.Release(Empty{}, &state); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "b in CS", func() bool { return snap(b).Status == statusInCS })

	// b's request happened after a's (clocks merged in between), so the entry
	// order follows the total order exactly.
	keyA := snap(a).EntryOrder[0]
	keyB := snap(b).EntryOrder[0]
	if !before(key(keyA), key(keyB)) {
		t.Fatalf("entry order %v before %v violates the total order", keyA, keyB)
	}
	if st := snap(a); len(st.Deferred) != 0 || st.Status != statusIdle {
		t.Fatalf("a did not finish clean: %+v", st)
	}
}

func TestContendedEntryKeepsTotalOrder(t *testing.T) {
	nodes := cluster(t, 3)
	apps := applications(nodes)

	// Everyone asks at the same time: three requests, three competing keys.
	var wg sync.WaitGroup
	for i := range apps {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var state State
			if err := apps[i].RequestCS(Empty{}, &state); err != nil {
				t.Errorf("request: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// The winner enters, we release it, the next one enters, and so on. At no
	// point may two nodes be in the critical section together. The counting
	// pass is READ-ONLY: releasing inside it would let the next node enter
	// mid-pass and look like a simultaneous holder.
	var entries []requestKey
	released := map[string]bool{}
	deadline := time.Now().Add(5 * time.Second)
	for len(entries) < len(nodes) && time.Now().Before(deadline) {
		type holder struct {
			index int
			state State
		}
		var holders []holder
		for i, node := range nodes {
			if state := snap(node); state.Status == statusInCS {
				holders = append(holders, holder{i, state})
			}
		}
		if len(holders) > 1 {
			t.Fatalf("MUTUAL EXCLUSION VIOLATED: %d nodes in the critical section", len(holders))
		}
		// Mutate only after the count: record the entry, then release.
		for _, h := range holders {
			if !released[h.state.Node] {
				entries = append(entries, requestKey{ts: h.state.MyTS, node: h.state.Node})
				released[h.state.Node] = true
				var out State
				if err := apps[h.index].Release(Empty{}, &out); err != nil {
					t.Fatal(err)
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(entries) != len(nodes) {
		t.Fatalf("only %d of %d requests ever entered: %v", len(entries), len(nodes), entries)
	}
	for i := 1; i < len(entries); i++ {
		if !before(entries[i-1], entries[i]) {
			t.Fatalf("entry order breaks the (TS,node) total order: %v", entries)
		}
	}
	for _, node := range nodes {
		if state := snap(node); state.Status != statusIdle || len(state.Queue) != 0 {
			t.Fatalf("%s did not finish clean: %+v", node.id, state)
		}
	}
}

// ─── Harness ─────────────────────────────────────────────────────────────────

func snap(s *MutexService) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func applications(nodes []*MutexService) []*Application {
	apps := make([]*Application, len(nodes))
	for i, node := range nodes {
		apps[i] = &Application{service: node}
	}
	return apps
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

// cluster builds size fully connected processes over real net/rpc pipes: one
// connection per direction, exactly like the runtime's peer clients.
func cluster(t *testing.T, size int) []*MutexService {
	t.Helper()
	nodes := make([]*MutexService, size)
	members := make([]string, size)
	for i := range nodes {
		members[i] = fmt.Sprintf("node-%d", i+1)
	}
	for i := range nodes {
		nodes[i] = &MutexService{
			id: members[i], members: members, peers: map[string]*rpc.Client{},
			clock: lamportClock{now: uint64(i)}, queue: []QueueEntry{}, status: statusIdle,
			report: func(any) error { return nil },
		}
	}
	for i := range nodes {
		for j := range nodes {
			if i == j {
				continue
			}
			local, remote := net.Pipe()
			server := rpc.NewServer()
			if err := server.RegisterName("MutexService", nodes[j]); err != nil {
				t.Fatal(err)
			}
			client := rpc.NewClient(local)
			go server.ServeConn(remote)
			nodes[i].peers[nodes[j].id] = client
			t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
		}
	}
	return nodes
}

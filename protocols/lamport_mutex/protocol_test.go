package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/rpc"
	"reflect"
	"sync"
	"testing"
	"time"
)

func testService(t *testing.T, id string, members []string) *MutexService {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &MutexService{
		id: id, members: members, peers: make(map[string]*rpc.Client),
		ctx: ctx, role: roleIdle, algorithm: lamport, operation: "none",
		released: make(map[string]uint64), report: func(any) error { return nil },
	}
}

func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true within five seconds")
}

func TestClockAndQueue(t *testing.T) {
	c := lamportClock{now: 4}
	if c.onReceive(2) != 5 || c.onReceive(20) != 21 || c.tick() != 22 {
		t.Fatal("incorrect Lamport clock update")
	}
	var queue []QueueEntry
	for _, entry := range []QueueEntry{{2, "node-1"}, {1, "node-10"}, {1, "node-2"}, {1, "node-2"}} {
		queue = insertRequest(queue, entry)
	}
	want := []QueueEntry{{1, "node-2"}, {1, "node-10"}, {2, "node-1"}}
	if !reflect.DeepEqual(queue, want) {
		t.Fatalf("queue=%v, want %v", queue, want)
	}
	if got := removeRequest(queue, QueueEntry{3, "node-1"}); !reflect.DeepEqual(got, want) {
		t.Fatal("release for a different request removed the current request")
	}
}

func TestEntryAndStaleAck(t *testing.T) {
	s := testService(t, "node-1", []string{"node-1", "node-2", "node-3"})
	s.mu.Lock()
	_, req, err := s.beginLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	ack := func(from string, forTS uint64) {
		t.Helper()
		if err := s.Ack(AckMsg{From: from, TS: 5, ForTS: forTS, Algorithm: lamport}, &Empty{}); err != nil {
			t.Fatal(err)
		}
	}
	ack("node-2", req.TS+1)
	if len(s.acks) != 0 {
		t.Fatal("wrong-request Ack counted")
	}
	ack("node-2", req.TS)
	ack("node-2", req.TS)
	if s.role != roleWaiting || len(s.acks) != 1 {
		t.Fatal("duplicate acknowledgement counted twice")
	}
	s.queue = insertRequest(s.queue, QueueEntry{0, "node-3"})
	ack("node-3", req.TS)
	if s.role != roleWaiting {
		t.Fatal("entered when request was not at queue head")
	}
	s.mu.Lock()
	s.queue = removeRequest(s.queue, QueueEntry{0, "node-3"})
	s.tryEnterLocked()
	s.mu.Unlock()
	if s.role != roleCritical || s.entries != 1 {
		t.Fatal("failed to enter after both conditions held")
	}
}

func TestReportPublicationHoldsStateLock(t *testing.T) {
	s := testService(t, "node-1", []string{"node-1", "node-2"})
	var seq uint64
	s.report = func(value any) error {
		if s.mu.TryLock() {
			s.mu.Unlock()
			t.Error("published a snapshot without holding the state lock")
		}
		state := value.(State)
		if state.ReportSeq <= seq {
			t.Error("report sequence did not increase")
		}
		seq = state.ReportSeq
		return nil
	}
	s.mu.Lock()
	_, request, _ := s.beginLocked()
	s.mu.Unlock()
	if err := s.Ack(AckMsg{From: "node-2", TS: 2, ForTS: request.TS, Algorithm: lamport}, &Empty{}); err != nil {
		t.Fatal(err)
	}
	s.release()
	if seq == 0 {
		t.Fatal("no state reports")
	}
}

func TestExactReleaseAndDelayedRequest(t *testing.T) {
	s := testService(t, "node-1", []string{"node-1", "node-2"})
	s.queue = []QueueEntry{{4, "node-2"}, {8, "node-2"}}
	msg := ReleaseMsg{From: "node-2", TS: 10, ForTS: 4, Algorithm: lamport}
	if err := s.Release(msg, &Empty{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.queue, []QueueEntry{{8, "node-2"}}) {
		t.Fatalf("old release removed wrong request: %v", s.queue)
	}
	if err := s.Request(ReqMsg{From: "node-2", TS: 4, Algorithm: lamport}, &Empty{}); err != nil {
		t.Fatal(err)
	}
	if len(s.queue) != 1 {
		t.Fatal("late request reappeared after its release")
	}
}

func TestDeferredRequestsDeduplicated(t *testing.T) {
	s := testService(t, "node-1", []string{"node-1", "node-2"})
	s.role = roleCritical
	for i := 0; i < 2; i++ {
		if err := s.Request(ReqMsg{From: "node-2", TS: 4, Algorithm: lamport}, &Empty{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.queue) != 1 || len(s.deferred) != 1 {
		t.Fatal("duplicate request duplicated the queue or deferred reply")
	}
}

func TestValidationAndConfiguration(t *testing.T) {
	for _, ms := range []float64{-1, .5, 120001, math.NaN(), math.Inf(1)} {
		if _, err := holdDuration(ms); err == nil {
			t.Errorf("accepted invalid holdMs %v", ms)
		}
	}
	if d, _ := holdDuration(1.5); d != 1500*time.Microsecond {
		t.Fatal("fractional milliseconds were truncated")
	}
	s := testService(t, "node-1", []string{"node-1", "node-2"})
	a := &Application{s}
	if err := a.Configure(AlgorithmArgs{ricart}, &State{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Request(ReqMsg{From: "node-2", TS: 1, Algorithm: lamport}, &Empty{}); err == nil {
		t.Fatal("accepted mixed algorithms")
	}
	if err := a.Auto(AutoArgs{-1}, &State{}); err == nil {
		t.Fatal("accepted invalid auto count")
	}
	if err := a.Acquire(AcquireArgs{15000}, &State{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Acquire(AcquireArgs{1}, &State{}); err == nil {
		t.Fatal("allowed overlapping local acquisition workers")
	}
	if err := a.Configure(AlgorithmArgs{lamport}, &State{}); err == nil {
		t.Fatal("allowed algorithm changes during a request")
	}
}

func TestMissingPeerErrorAndCountAreReported(t *testing.T) {
	s := testService(t, "node-1", []string{"node-1", "node-2"})
	var last State
	s.report = func(value any) error { last = value.(State); return nil }
	s.send("node-2", "MutexService.Request", ReqMsg{})
	if last.MessagesSent != 1 || last.SendErrors != 1 || last.LastError == "" {
		t.Fatalf("send failure was not reported: %+v", last)
	}
}

func testCluster(t *testing.T, mode string, n int) ([]*MutexService, func() map[string]State) {
	t.Helper()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("node-%d", i+1)
	}
	var reportsMu sync.Mutex
	latest := make(map[string]State)
	services := make([]*MutexService, n)
	for i, id := range ids {
		s := testService(t, id, ids)
		s.algorithm = mode
		s.report = func(value any) error {
			reportsMu.Lock()
			defer reportsMu.Unlock()
			st := value.(State)
			if previous, ok := latest[st.Node]; ok && st.ReportSeq <= previous.ReportSeq {
				t.Error("reports were reordered")
			}
			latest[st.Node] = st
			critical := 0
			for _, v := range latest {
				if v.Role == roleCritical {
					critical++
				}
			}
			if critical > 1 {
				t.Error("critical sections overlapped")
			}
			return nil
		}
		services[i] = s
	}
	for _, from := range services {
		for _, to := range services {
			if from == to {
				continue
			}
			serverSide, clientSide := net.Pipe()
			client := rpc.NewClient(clientSide)
			from.peers[to.id] = client
			server := rpc.NewServer()
			if err := server.RegisterName("MutexService", to); err != nil {
				t.Fatal(err)
			}
			go server.ServeConn(serverSide)
			t.Cleanup(func() { client.Close(); serverSide.Close() })
		}
	}
	return services, func() map[string]State {
		reportsMu.Lock()
		defer reportsMu.Unlock()
		copy := make(map[string]State)
		for k, v := range latest {
			copy[k] = v
		}
		return copy
	}
}

func TestConcurrentCyclesAndMessageCounts(t *testing.T) {
	for _, mode := range []string{lamport, ricart} {
		t.Run(mode, func(t *testing.T) {
			services, states := testCluster(t, mode, 3)
			for cycle := 1; cycle <= 5; cycle++ {
				var wg sync.WaitGroup
				for _, s := range services {
					wg.Add(1)
					go func(s *MutexService) {
						defer wg.Done()
						if err := (&Application{s}).Acquire(AcquireArgs{2}, &State{}); err != nil {
							t.Error(err)
						}
					}(s)
				}
				wg.Wait()
				await(t, func() bool {
					all := states()
					if len(all) != 3 {
						return false
					}
					for _, st := range all {
						if st.Completed != cycle || st.OperationRunning || len(st.Queue) != 0 {
							return false
						}
					}
					return true
				})
			}
			var sent uint64
			for _, st := range states() {
				sent += st.MessagesSent
			}
			want := uint64(5 * 3 * 3 * 2)
			if mode == ricart {
				want = 5 * 3 * 2 * 2
			}
			if sent != want {
				t.Fatalf("algorithm messages=%d, want %d", sent, want)
			}
		})
	}
}

func TestAutoPublishesCompletion(t *testing.T) {
	services, states := testCluster(t, ricart, 2)
	if err := (&Application{services[0]}).Auto(AutoArgs{2}, &State{}); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool {
		st := states()["node-1"]
		return st.Completed == 2 && !st.AutoRunning && !st.OperationRunning
	})
}

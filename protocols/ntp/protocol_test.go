package main

import (
	"math"
	"net"
	"net/rpc"
	"sync"
	"testing"
	"time"
)

// These tests are intentionally readable numeric examples, not just assertions
// about file shape. All times in the formulas are milliseconds.
func TestFourTimestampsSubtractProcessing(t *testing.T) {
	// Client sends at 1000. Server is +300 ahead; each path takes 50 ms.
	// Server receives at 1350, works 200 ms, sends at 1550. Client receives
	// at 1300. Total elapsed=300, processing=200, network delay=100, offset=300.
	sample, err := measure(1000, 1350, 1550, 1300)
	if err != nil || sample.DelayMS != 100 || sample.OffsetMS != 300 {
		t.Fatalf("got %+v, %v; want delay=100, offset=300", sample, err)
	}
	// Removing server work changes the timestamps, but not the two estimates.
	quick, err := measure(1000, 1350, 1350, 1100)
	if err != nil || quick.DelayMS != sample.DelayMS || quick.OffsetMS != sample.OffsetMS {
		t.Fatalf("processing was not subtracted: %+v, %v", quick, err)
	}
}
func TestAsymmetricOffsetBias(t *testing.T) {
	// Server truly +300, outbound=20, inbound=80. Formula returns +270:
	// offset error=(outbound-inbound)/2=-30; four timestamps cannot fix it.
	sample, err := measure(1000, 1320, 1320, 1100)
	if err != nil || sample.OffsetMS != 270 || sample.DelayMS != 100 {
		t.Fatalf("unexpected asymmetric estimate: %+v, %v", sample, err)
	}
}
func TestInvalidTimestamps(t *testing.T) {
	cases := [][4]float64{
		{100, 200, 210, 99},  // Client went backwards.
		{100, 210, 200, 120}, // Server went backwards.
		{100, 200, 300, 120}, // Negative estimated network delay.
		{math.NaN(), 0, 0, 0},
	}
	for _, times := range cases {
		if _, err := measure(times[0], times[1], times[2], times[3]); err == nil {
			t.Errorf("accepted invalid timestamps %v", times)
		}
	}
}
func TestMinimumDelayFilter(t *testing.T) {
	samples := []Sample{
		{DelayMS: 120, OffsetMS: 80, Stratum: 1},
		{DelayMS: 40, OffsetMS: 100, Stratum: 2}, // Select this, not average offset=90.
		{DelayMS: 0, OffsetMS: 900, Stratum: 1, Error: "timeout"},
		{DelayMS: 1, Stratum: 16}, // Unsynchronized source must not win.
	}
	if best := lowestDelay(samples); best != 1 {
		t.Fatalf("selected %d, want 1", best)
	}
	if best := lowestDelay([]Sample{{Error: "failed"}}); best != -1 {
		t.Fatal("no valid sample should produce selected=-1")
	}
}

// Test helpers construct a real ordinary net/rpc server over an in-memory
// connection. t.Cleanup closes only connections that the TEST owns; protocols
// themselves must not close Runtime-owned connections.
func serverClient(t *testing.T, s *TimeService) *rpc.Client {
	t.Helper() // A failure is attributed to the test call site, not this helper.
	server := rpc.NewServer()
	if err := server.RegisterName("TimeService", s); err != nil {
		t.Fatal(err)
	}
	local, remote := net.Pipe()
	client := rpc.NewClient(local)
	go server.ServeConn(remote)
	t.Cleanup(func() { _ = client.Close(); _ = remote.Close() })
	return client
}
func service(index int) *TimeService {
	nodes := []string{"node-1", "node-2", "node-3"}
	s := &TimeService{id: nodes[index], index: index, members: nodes, peers: map[string]*rpc.Client{},
		clock: newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 750}), stratum: 16,
		report: func(any) error { return nil }}
	if index == 0 {
		s.stratum = 1
	}
	return s
}
func TestHierarchyRequiresSynchronizedUpstream(t *testing.T) {
	root, middle, leaf := service(0), service(1), service(2)
	middle.peers[root.id] = serverClient(t, root)
	leaf.peers[middle.id] = serverClient(t, middle)
	before := leaf.clock
	leafApp := &Application{service: leaf}
	if err := leafApp.Sync(SyncArgs{Samples: 1}, &Round{}); err == nil || leaf.stratum != 16 || leaf.clock != before {
		t.Fatal("unsynchronized upstream must not adjust leaf")
	}
	if err := (&Application{service: middle}).Sync(SyncArgs{Samples: 3, ProcessingMS: 20}, &Round{}); err != nil {
		t.Fatal(err)
	}
	if err := leafApp.Sync(SyncArgs{Samples: 3}, &Round{}); err != nil {
		t.Fatal(err)
	}
	if middle.stratum != 2 || leaf.stratum != 3 || leaf.upstream != middle.id {
		t.Fatalf("wrong hierarchy: middle=%d leaf=%d upstream=%s", middle.stratum, leaf.stratum, leaf.upstream)
	}
	if err := (&Application{service: middle}).Sync(SyncArgs{Servers: []string{"node-3"}}, &Round{}); err == nil {
		t.Fatal("downstream reference would create a possible synchronization loop")
	}
	if err := leafApp.ConfigureClock(ClockArgs{}, &State{}); err != nil || leaf.stratum != 16 {
		t.Fatal("manual clock change must clear synchronization status")
	}
}
func TestClockChangeInvalidatesExchange(t *testing.T) {
	s := service(0)
	// Holding mu until the worker starts is not a way to order its timestamp;
	// instead repeatedly disturb the clock while a bounded exchange sleeps.
	var workers sync.WaitGroup
	workers.Add(1)
	done := make(chan error, 1)
	go func() {
		defer workers.Done()
		done <- s.Exchange(ExchangeArgs{T0: 1, ProcessingMS: 100}, &ExchangeReply{})
	}()
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		if err := (&Application{service: s}).ConfigureClock(ClockArgs{}, &State{}); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	if err := <-done; err == nil {
		t.Fatal("server clock changed inside exchange but sample was accepted")
	}
}

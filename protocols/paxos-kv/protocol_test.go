package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/rpc"
	"testing"
	"time"
)

func testServer(id string, index uint64) *Server {
	return &Server{id: id, index: index, members: []string{"node-1", "node-2", "node-3"}, peers: map[string]*rpc.Client{}, save: func(any) error { return nil }, report: func(any) error { return nil }, catching: map[int]bool{}, disk: Disk{Slots: map[int]Slot{}, Values: map[string]string{}, Results: map[string]Result{}}}
}
func TestPromiseAndVotePersistBeforeReply(t *testing.T) {
	s := testServer("node-1", 1)
	var saved []byte
	s.save = func(v any) error { var err error; saved, err = json.Marshal(v); return err }
	var reply Promise
	if err := s.Prepare(PrepareArgs{Slot: 1, Ballot: 17}, &reply); err != nil || !reply.OK {
		t.Fatal(reply, err)
	}
	restart := testServer("node-1", 1)
	if err := json.Unmarshal(saved, &restart.disk); err != nil {
		t.Fatal(err)
	}
	reply = Promise{}
	if err := restart.Prepare(PrepareArgs{Slot: 1, Ballot: 16}, &reply); err != nil || reply.OK {
		t.Fatal("forgot promise", reply, err)
	}
	s.save = func(any) error { return errors.New("disk failure") }
	reply = Promise{}
	if err := s.Accept(AcceptArgs{Slot: 1, Ballot: 17, Value: Operation{ID: "x", Kind: "put"}}, &reply); err == nil || reply.OK {
		t.Fatal("acknowledged undurable accept")
	}
}
func TestExecutionWaitsForHolesAndReadsAreOrdered(t *testing.T) {
	s := testServer("node-1", 1)
	if err := s.Decide(DecisionArgs{Slot: 2, Ballot: 17, Value: Operation{ID: "get", Kind: "get", Key: "x"}}, &Empty{}); err != nil {
		t.Fatal(err)
	}
	if s.disk.Applied != 0 {
		t.Fatal("executed across a hole")
	}
	if err := s.Decide(DecisionArgs{Slot: 1, Ballot: 17, Value: Operation{ID: "put", Kind: "put", Key: "x", Value: "1"}}, &Empty{}); err != nil {
		t.Fatal(err)
	}
	if s.disk.Results["get"].Value != "1" || s.disk.Applied != 2 {
		t.Fatal(s.disk)
	}
	if err := s.Decide(DecisionArgs{Slot: 1, Ballot: 33, Value: Operation{ID: "other"}}, &Empty{}); err == nil {
		t.Fatal("accepted conflicting decision")
	}
}
func TestHigherProposerReusesAcceptedValue(t *testing.T) {
	nodes := []*Server{testServer("node-1", 1), testServer("node-2", 2), testServer("node-3", 3)}
	for _, s := range nodes {
		for _, peer := range nodes {
			if s == peer {
				continue
			}
			server := rpc.NewServer()
			if err := server.RegisterName("Paxos", peer); err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			client := rpc.NewClient(a)
			s.peers[peer.id] = client
			go server.ServeConn(b)
			t.Cleanup(func() { client.Close(); b.Close() })
		}
	}
	old := Operation{ID: "old", Kind: "put", Key: "x", Value: "1"}
	for _, s := range nodes[:2] {
		var reply Promise
		if err := s.Accept(AcceptArgs{Slot: 1, Ballot: 17, Value: old}, &reply); err != nil || !reply.OK {
			t.Fatal(reply, err)
		}
	}
	got, err := nodes[2].choose(1, Operation{ID: "new", Kind: "put", Key: "x", Value: "2"}, time.Now().Add(3*time.Second))
	if err != nil || got != old {
		t.Fatal("lost chosen value", got, err)
	}
}

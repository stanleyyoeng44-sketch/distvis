package main

import (
	"context"
	"net/rpc"
	"os"
	"sync"
	"time"

	"distvis/sdk"
	lab "distvis/sdk/rpc"
)

type Item struct {
	Value   string `json:"value"`
	Version int    `json:"version"`
	Writer  string `json:"writer"`
}
type State struct {
	Role    string          `json:"role"`
	Version int             `json:"version"`
	Store   map[string]Item `json:"store"`
}
type WriteArgs struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type WriteReply struct {
	Version int `json:"version"`
}
type SyncArgs struct {
	Store map[string]Item `json:"store"`
}
type SyncReply struct{ Accepted bool }
type Application struct{ store *Store }
type Store struct {
	mu    sync.Mutex
	node  *lab.Runtime
	peers map[string]*rpc.Client
	state State
}

func newStore(node *lab.Runtime) (*Store, error) {
	s := &Store{node: node, state: State{Role: "replica", Store: map[string]Item{}}}
	err := sdk.Load(&s.state)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if s.state.Store == nil {
		s.state.Store = map[string]Item{}
	}
	if err != nil && node.ID == node.Nodes[0] {
		s.state.Version = 1
		s.state.Store["course"] = Item{"distributed-systems", 1, node.ID}
	}
	s.peers, err = node.RPCPeers()
	return s, err
}
func (s *Store) report() error {
	if err := sdk.Save(s.state); err != nil {
		return err
	}
	return s.node.Report(s.state)
}
func (a *Application) Write(args WriteArgs, reply *WriteReply) error {
	s := a.store
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Version++
	s.state.Store[args.Key] = Item{args.Value, s.state.Version, s.node.ID}
	reply.Version = s.state.Version
	return s.report()
}
func (s *Store) Sync(args SyncArgs, reply *SyncReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, item := range args.Store {
		if item.Version > s.state.Version {
			s.state.Version = item.Version
		}
		old, ok := s.state.Store[key]
		if !ok || item.Version > old.Version || (item.Version == old.Version && item.Writer > old.Writer) {
			s.state.Store[key] = item
		}
	}
	reply.Accepted = true
	return s.report()
}
func (s *Store) run(ctx context.Context) error {
	ticker := time.NewTicker(1600 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.mu.Lock()
			items := map[string]Item{}
			for key, item := range s.state.Store {
				items[key] = item
			}
			s.mu.Unlock()
			for _, client := range s.peers {
				// Periodic anti-entropy retries on later ticks after link recovery.
				go func(client *rpc.Client) { _ = client.Call("Replica.Sync", SyncArgs{items}, &SyncReply{}) }(client)
			}
		}
	}
}

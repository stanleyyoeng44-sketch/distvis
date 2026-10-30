package main

import (
	"fmt"
	"net/rpc"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"distvis/sdk"
	lab "distvis/sdk/rpc"
)

const (
	primaryNode = "node-1"

	rolePrimary = "primary"
	roleBackup  = "backup"

	replicationAsync = "async"
	replicationSync  = "sync"

	readLocal   = "local"
	readPrimary = "primary"
	readSession = "session"

	replicateTimeout = 4 * time.Second
	peerTimeout      = 10 * time.Second
	sessionTimeout   = 9 * time.Second
)

type Entry struct {
	Index int    `json:"index"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type WriteRecord struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Index       int    `json:"index"`
	Replication string `json:"replication"`
	Acks        int    `json:"acks"`
	Error       string `json:"error,omitempty"`
}

type ReadRecord struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Found    bool   `json:"found"`
	Mode     string `json:"mode"`
	Index    int    `json:"index"`
	Stale    bool   `json:"stale"`
	WaitedMs int    `json:"waitedMs"`
}

type State struct {
	Node               string            `json:"node"`
	Role               string            `json:"role"`
	Primary            string            `json:"primary"`
	Replication        string            `json:"replication"`
	ReplicationDelayMs int               `json:"replicationDelayMs"`
	Index              int               `json:"index"`
	AppliedIndex       int               `json:"appliedIndex"`
	Store              map[string]string `json:"store"`
	Log                []Entry           `json:"log"`
	Pending            int               `json:"pending"`
	LastWrite          *WriteRecord      `json:"lastWrite,omitempty"`
	LastGet            *ReadRecord       `json:"lastGet,omitempty"`
	LastError          string            `json:"lastError,omitempty"`
}

type PutArgs struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PutReply struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Index       int    `json:"index"`
	Acks        int    `json:"acks"`
	Replication string `json:"replication"`
	Error       string `json:"error,omitempty"`
}

type ApplyArgs struct {
	Entry Entry `json:"entry"`
}

type ApplyReply struct {
	Applied      bool `json:"applied"`
	AppliedIndex int  `json:"appliedIndex"`
}

type GetArgs struct {
	Key      string `json:"key"`
	Mode     string `json:"mode"`
	MinIndex int    `json:"minIndex"`
}

type GetReply struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Found    bool   `json:"found"`
	Mode     string `json:"mode"`
	Index    int    `json:"index"`
	Stale    bool   `json:"stale"`
	WaitedMs int    `json:"waitedMs"`
}

type ModeArgs struct {
	Replication        string `json:"replication"`
	ReplicationDelayMs int    `json:"replicationDelayMs"`
}

type ModeReply struct {
	Replication        string `json:"replication"`
	ReplicationDelayMs int    `json:"replicationDelayMs"`
}

type Application struct {
	server *Server
}

type Server struct {
	mu      sync.Mutex
	node    *lab.Runtime
	peers   map[string]*rpc.Client
	state   State
	pending map[int]Entry
}

func roleFor(id string) string {
	if id == primaryNode {
		return rolePrimary
	}
	return roleBackup
}

func newServer(node *lab.Runtime) (*Server, error) {
	s := &Server{
		node: node,
		state: State{
			Node:        node.ID,
			Role:        roleFor(node.ID),
			Primary:     primaryNode,
			Replication: replicationSync,
			Store:       map[string]string{},
		},
		pending: map[int]Entry{},
	}
	if err := sdk.Load(&s.state); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// Membership and the fixed primary come from the experiment, not from disk.
	s.state.Node = node.ID
	s.state.Role = roleFor(node.ID)
	s.state.Primary = primaryNode
	if s.state.Store == nil {
		s.state.Store = map[string]string{}
	}
	if s.state.Replication != replicationAsync && s.state.Replication != replicationSync {
		s.state.Replication = replicationSync
	}
	if s.state.ReplicationDelayMs < 0 {
		s.state.ReplicationDelayMs = 0
	}
	s.state.Pending = 0

	var err error
	s.peers, err = node.RPCPeers()
	return s, err
}

func (s *Server) reportLocked() error {
	s.state.Pending = len(s.pending)
	if err := sdk.Save(s.state); err != nil {
		s.state.LastError = err.Error()
		return err
	}
	return s.node.Report(s.state)
}

func (s *Server) report() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reportLocked()
}

// call performs one peer RPC with a deadline. A timed-out call may still
// execute on the receiver; the caller must treat timeout as "unknown".
func call(client *rpc.Client, method string, args, reply any, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("%s: no connection", method)
	}
	done := make(chan error, 1)
	go func() { done <- client.Call(method, args, reply) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("%s: no reply within %s", method, timeout)
	}
}

func (a *Application) Put(args PutArgs, reply *PutReply) error {
	s := a.server
	if strings.TrimSpace(args.Key) == "" {
		return fmt.Errorf("key is required")
	}
	if s.node.ID != primaryNode {
		var forwarded PutReply
		if err := call(s.peers[primaryNode], "Replica.Put", args, &forwarded, peerTimeout); err != nil {
			s.recordWrite(&WriteRecord{Key: args.Key, Value: args.Value, Replication: replicationSync, Error: err.Error()})
			return fmt.Errorf("forward to primary %s: %w", primaryNode, err)
		}
		s.recordWrite(&WriteRecord{
			Key:         forwarded.Key,
			Value:       forwarded.Value,
			Index:       forwarded.Index,
			Replication: forwarded.Replication,
			Acks:        forwarded.Acks,
			Error:       forwarded.Error,
		})
		*reply = forwarded
		if forwarded.Error != "" {
			return fmt.Errorf("forward to primary %s: %s", primaryNode, forwarded.Error)
		}
		return nil
	}
	if err := s.putPrimary(args, reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

func (s *Server) putPrimary(args PutArgs, reply *PutReply) error {
	s.mu.Lock()
	entry := Entry{Index: s.state.Index + 1, Key: args.Key, Value: args.Value}
	s.state.Index = entry.Index
	s.state.AppliedIndex = entry.Index
	s.state.Store[entry.Key] = entry.Value
	s.state.Log = append(s.state.Log, entry)
	s.state.LastWrite = &WriteRecord{
		Key:         entry.Key,
		Value:       entry.Value,
		Index:       entry.Index,
		Replication: s.state.Replication,
	}
	s.state.LastError = ""
	mode := s.state.Replication
	delay := time.Duration(s.state.ReplicationDelayMs) * time.Millisecond
	if err := s.reportLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	if mode == replicationAsync {
		go s.replicateAsync(entry, delay)
		*reply = PutReply{Key: entry.Key, Value: entry.Value, Index: entry.Index, Replication: mode}
		return nil
	}

	acks, failed := s.replicateSync(entry)
	s.mu.Lock()
	if s.state.LastWrite != nil && s.state.LastWrite.Index == entry.Index {
		s.state.LastWrite.Acks = acks
	}
	message := ""
	if len(failed) > 0 {
		message = fmt.Sprintf("index %d applied on primary but sync replication timed out; missing acks from %s", entry.Index, strings.Join(failed, ", "))
		s.state.LastError = message
		if s.state.LastWrite != nil && s.state.LastWrite.Index == entry.Index {
			s.state.LastWrite.Error = message
		}
	}
	err := s.reportLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	*reply = PutReply{Key: entry.Key, Value: entry.Value, Index: entry.Index, Acks: acks, Replication: mode, Error: message}
	return nil
}

func (s *Server) replicateSync(entry Entry) (int, []string) {
	if len(s.peers) == 0 {
		return 0, nil
	}
	type result struct {
		peer string
		err  error
	}
	results := make(chan result, len(s.peers))
	for peer, client := range s.peers {
		go func(peer string, client *rpc.Client) {
			var reply ApplyReply
			results <- result{peer, call(client, "Replica.Apply", ApplyArgs{Entry: entry}, &reply, replicateTimeout)}
		}(peer, client)
	}

	acks := 0
	seen := map[string]bool{}
	var failed []string
	deadline := time.After(replicateTimeout)
	for i := 0; i < len(s.peers); i++ {
		select {
		case r := <-results:
			seen[r.peer] = true
			if r.err != nil {
				failed = append(failed, r.peer+": "+r.err.Error())
			} else {
				acks++
			}
		case <-deadline:
			for peer := range s.peers {
				if !seen[peer] {
					failed = append(failed, peer+": no reply")
				}
			}
			sort.Strings(failed)
			return acks, failed
		}
	}
	sort.Strings(failed)
	return acks, failed
}

func (s *Server) replicateAsync(entry Entry, delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}
	type result struct {
		peer string
		err  error
	}
	results := make(chan result, len(s.peers))
	for peer, client := range s.peers {
		go func(peer string, client *rpc.Client) {
			var reply ApplyReply
			results <- result{peer, call(client, "Replica.Apply", ApplyArgs{Entry: entry}, &reply, replicateTimeout)}
		}(peer, client)
	}
	acks := 0
	var failed []string
	for i := 0; i < len(s.peers); i++ {
		r := <-results
		if r.err != nil {
			failed = append(failed, r.peer+": "+r.err.Error())
		} else {
			acks++
		}
	}
	sort.Strings(failed)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.LastWrite != nil && s.state.LastWrite.Index == entry.Index {
		s.state.LastWrite.Acks = acks
		if len(failed) > 0 {
			s.state.LastWrite.Error = strings.Join(failed, ", ")
			s.state.LastError = "async replication: " + strings.Join(failed, ", ")
		}
	}
	_ = s.reportLocked()
}

func (s *Server) recordWrite(rec *WriteRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.LastWrite = rec
	if rec.Error != "" {
		s.state.LastError = rec.Error
	}
	_ = s.reportLocked()
}

func (a *Application) Get(args GetArgs, reply *GetReply) error {
	s := a.server
	mode := strings.ToLower(strings.TrimSpace(args.Mode))
	if mode == "" {
		mode = readLocal
	}
	if strings.TrimSpace(args.Key) == "" {
		return fmt.Errorf("key is required")
	}
	started := time.Now()

	switch mode {
	case readLocal:
		*reply = s.localRead(args.Key, mode, s.node.ID != primaryNode, started)
		return nil
	case readPrimary:
		if s.node.ID == primaryNode {
			*reply = s.localRead(args.Key, mode, false, started)
			return nil
		}
		var forwarded GetReply
		if err := call(s.peers[primaryNode], "Replica.Get", GetArgs{Key: args.Key}, &forwarded, peerTimeout); err != nil {
			s.recordRead(&ReadRecord{Key: args.Key, Mode: mode, Stale: true, WaitedMs: int(time.Since(started).Milliseconds())})
			return fmt.Errorf("read from primary %s: %w", primaryNode, err)
		}
		forwarded.Mode = mode
		forwarded.WaitedMs = int(time.Since(started).Milliseconds())
		s.recordRead(&ReadRecord{
			Key: forwarded.Key, Value: forwarded.Value, Found: forwarded.Found,
			Mode: mode, Index: forwarded.Index, WaitedMs: forwarded.WaitedMs,
		})
		*reply = forwarded
		return nil
	case readSession:
		minIndex := args.MinIndex
		if minIndex < 0 {
			minIndex = 0
		}
		if s.node.ID != primaryNode && !s.waitApplied(minIndex) {
			waited := int(time.Since(started).Milliseconds())
			s.recordRead(&ReadRecord{
				Key: args.Key, Mode: mode, Stale: true, WaitedMs: waited,
			})
			return fmt.Errorf("session read: index %d not applied after %s (appliedIndex=%d)", minIndex, sessionTimeout, s.appliedIndex())
		}
		*reply = s.localRead(args.Key, mode, false, started)
		return nil
	default:
		return fmt.Errorf("unknown read mode %q: use local, primary or session", args.Mode)
	}
}

func (s *Server) localRead(key, mode string, stale bool, started time.Time) GetReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, found := s.state.Store[key]
	waited := int(time.Since(started).Milliseconds())
	reply := GetReply{
		Key: key, Value: value, Found: found, Mode: mode,
		Index: s.state.AppliedIndex, Stale: stale, WaitedMs: waited,
	}
	s.state.LastGet = &ReadRecord{
		Key: key, Value: value, Found: found, Mode: mode,
		Index: reply.Index, Stale: stale, WaitedMs: waited,
	}
	_ = s.reportLocked()
	return reply
}

func (s *Server) recordRead(rec *ReadRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.LastGet = rec
	_ = s.reportLocked()
}

func (s *Server) waitApplied(minIndex int) bool {
	deadline := time.Now().Add(sessionTimeout)
	for {
		if s.appliedIndex() >= minIndex {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (s *Server) appliedIndex() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.AppliedIndex
}

func (a *Application) Mode(args ModeArgs, reply *ModeReply) error {
	s := a.server
	replication := strings.ToLower(strings.TrimSpace(args.Replication))
	if replication != "" && replication != replicationAsync && replication != replicationSync {
		return fmt.Errorf("unknown replication mode %q: use async or sync", args.Replication)
	}
	if args.ReplicationDelayMs < 0 {
		return fmt.Errorf("replicationDelayMs must be >= 0")
	}
	if s.node.ID != primaryNode {
		var forwarded ModeReply
		if err := call(s.peers[primaryNode], "Replica.Mode", args, &forwarded, peerTimeout); err != nil {
			return fmt.Errorf("forward mode to primary %s: %w", primaryNode, err)
		}
		s.mu.Lock()
		s.state.Replication = forwarded.Replication
		s.state.ReplicationDelayMs = forwarded.ReplicationDelayMs
		err := s.reportLocked()
		s.mu.Unlock()
		if err != nil {
			return err
		}
		*reply = forwarded
		return nil
	}
	return s.setMode(args, reply)
}

func (s *Server) setMode(args ModeArgs, reply *ModeReply) error {
	s.mu.Lock()
	if strings.TrimSpace(args.Replication) != "" {
		s.state.Replication = strings.ToLower(strings.TrimSpace(args.Replication))
	}
	s.state.ReplicationDelayMs = args.ReplicationDelayMs
	config := ModeReply{Replication: s.state.Replication, ReplicationDelayMs: s.state.ReplicationDelayMs}
	err := s.reportLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	*reply = config

	// The policy lives on the primary; mirror it to backups so their reported
	// state shows the mode that is actually in effect.
	if s.node.ID == primaryNode {
		for _, client := range s.peers {
			go func(client *rpc.Client) {
				var mirror ModeReply
				_ = call(client, "Replica.Mode", ModeArgs{Replication: config.Replication, ReplicationDelayMs: config.ReplicationDelayMs}, &mirror, replicateTimeout)
			}(client)
		}
	}
	return nil
}

func (s *Server) Put(args PutArgs, reply *PutReply) error {
	if s.node.ID != primaryNode {
		return fmt.Errorf("%s is not the primary; primary is %s", s.node.ID, primaryNode)
	}
	if strings.TrimSpace(args.Key) == "" {
		return fmt.Errorf("key is required")
	}
	return s.putPrimary(args, reply)
}

func (s *Server) Get(args GetArgs, reply *GetReply) error {
	if s.node.ID != primaryNode {
		return fmt.Errorf("%s is not the primary; primary is %s", s.node.ID, primaryNode)
	}
	if strings.TrimSpace(args.Key) == "" {
		return fmt.Errorf("key is required")
	}
	*reply = s.localRead(args.Key, readPrimary, false, time.Now())
	return nil
}

func (s *Server) Apply(args ApplyArgs, reply *ApplyReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.node.ID == primaryNode {
		return fmt.Errorf("primary does not accept replica applies")
	}
	entry := args.Entry
	if entry.Index <= s.state.AppliedIndex {
		reply.AppliedIndex = s.state.AppliedIndex
		return nil
	}
	if entry.Index > s.state.Index {
		s.state.Index = entry.Index
	}
	if entry.Index > s.state.AppliedIndex+1 {
		// Keep the gap: the missing entry is still in flight.
		s.pending[entry.Index] = entry
		reply.AppliedIndex = s.state.AppliedIndex
		return s.reportLocked()
	}
	s.applyLocked(entry)
	for {
		next, ok := s.pending[s.state.AppliedIndex+1]
		if !ok {
			break
		}
		delete(s.pending, s.state.AppliedIndex+1)
		s.applyLocked(next)
	}
	reply.Applied = true
	reply.AppliedIndex = s.state.AppliedIndex
	return s.reportLocked()
}

func (s *Server) applyLocked(entry Entry) {
	s.state.Store[entry.Key] = entry.Value
	s.state.AppliedIndex = entry.Index
	s.state.Log = append(s.state.Log, entry)
}

func (s *Server) Mode(args ModeArgs, reply *ModeReply) error {
	replication := strings.ToLower(strings.TrimSpace(args.Replication))
	if replication != "" && replication != replicationAsync && replication != replicationSync {
		return fmt.Errorf("unknown replication mode %q: use async or sync", args.Replication)
	}
	if args.ReplicationDelayMs < 0 {
		return fmt.Errorf("replicationDelayMs must be >= 0")
	}
	return s.setMode(args, reply)
}

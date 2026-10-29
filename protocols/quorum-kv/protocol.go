package main

import (
	"fmt"
	"net/rpc"
	"sort"
	"time"
)

type Empty struct{}
type Version struct {
	Counter uint64 `json:"counter"`
	Writer  string `json:"writer"`
}
type Versioned struct {
	Value   string  `json:"value"`
	Version Version `json:"version"`
}
type Disk struct {
	Counter uint64               `json:"counter"`
	Values  map[string]Versioned `json:"values"`
}
type KeyArgs struct {
	Key        string `json:"key"`
	MinCounter uint64 `json:"minCounter"`
}
type PutArgs struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type StoreArgs struct {
	Key  string    `json:"key"`
	Item Versioned `json:"item"`
}
type Result struct {
	Key        string    `json:"key"`
	Item       Versioned `json:"item"`
	Responders []string  `json:"responders"`
	WaitedMs   float64   `json:"waitedMs"`
	Success    bool      `json:"success"`
	Stale      bool      `json:"stale"`
	Note       string    `json:"note"`
}
type State struct {
	Role         string               `json:"role"`
	N            int                  `json:"n"`
	W            int                  `json:"w"`
	R            int                  `json:"r"`
	Values       map[string]Versioned `json:"values"`
	MessagesSent uint64               `json:"messagesSent"`
	LastPut      *Result              `json:"lastPut"`
	LastGet      *Result              `json:"lastGet"`
	LastError    string               `json:"lastError"`
}

func newer(a, b Version) bool {
	return a.Counter > b.Counter || a.Counter == b.Counter && a.Writer > b.Writer
}
func (s *Replica) stateLocked() State {
	values := make(map[string]Versioned, len(s.disk.Values))
	for k, v := range s.disk.Values {
		values[k] = v
	}
	return State{Role: "replica", N: len(s.members), W: s.w, R: s.r, Values: values, MessagesSent: s.messages, LastPut: s.lastPut, LastGet: s.lastGet, LastError: s.lastError}
}
func (s *Replica) publishLocked() error { return s.report(s.stateLocked()) }
func (s *Replica) persistLocked() error {
	if s.storageError != "" {
		return fmt.Errorf("storage stopped: %s", s.storageError)
	}
	if err := s.save(s.disk); err != nil {
		s.storageError = err.Error()
		s.lastError = err.Error()
		return err
	}
	return nil
}
func (s *Replica) Fetch(args KeyArgs, reply *Versioned) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageError != "" {
		return fmt.Errorf("storage stopped")
	}
	*reply = s.disk.Values[args.Key]
	return nil
}
func (s *Replica) Store(args StoreArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageError != "" {
		return fmt.Errorf("storage stopped")
	}
	if newer(args.Item.Version, s.disk.Values[args.Key].Version) {
		s.disk.Values[args.Key] = args.Item
		s.disk.Counter = max(s.disk.Counter, args.Item.Version.Counter)
		if err := s.persistLocked(); err != nil {
			return err
		}
	}
	return s.publishLocked()
}

type answer struct {
	node string
	item Versioned
	err  error
}

// Local responses count as one replica, never as an extra vote. Every peer is
// contacted concurrently; a slow peer does not delay reaching the threshold.
func (s *Replica) gather(key string, item *Versioned, need int) (Versioned, []string, error) {
	answers := make(chan answer, len(s.members))
	invoke := func(id string) {
		var value Versioned
		var err error
		if id == s.id {
			if item == nil {
				err = s.Fetch(KeyArgs{Key: key}, &value)
			} else {
				err = s.Store(StoreArgs{Key: key, Item: *item}, &Empty{})
			}
		} else {
			s.mu.Lock()
			s.messages++
			s.mu.Unlock()
			var pending *rpc.Call
			if item == nil {
				pending = s.peers[id].Go("Replica.Fetch", KeyArgs{Key: key}, &value, make(chan *rpc.Call, 1))
			} else {
				pending = s.peers[id].Go("Replica.Store", StoreArgs{Key: key, Item: *item}, &Empty{}, make(chan *rpc.Call, 1))
			}
			timer := time.NewTimer(4 * time.Second)
			defer timer.Stop()
			select {
			case done := <-pending.Done:
				err = done.Error
			case <-timer.C:
				answers <- answer{node: id, err: fmt.Errorf("timeout")}
				return
			}
		}
		answers <- answer{node: id, item: value, err: err}
	}
	invoke(s.id)
	for _, id := range s.members {
		if id != s.id {
			go invoke(id)
		}
	}
	var best Versioned
	var responders []string
	for range s.members {
		response := <-answers
		if response.err != nil {
			continue
		}
		responders = append(responders, response.node)
		if newer(response.item.Version, best.Version) {
			best = response.item
		}
		if len(responders) >= need {
			sort.Strings(responders)
			return best, responders, nil
		}
	}
	return best, responders, fmt.Errorf("only %d of %d required replicas replied; writes may still take effect", len(responders), need)
}
func (a *Application) Put(args PutArgs, reply *Result) error {
	if args.Key == "" || len(args.Key) > 128 || len(args.Value) > 1024 {
		return fmt.Errorf("key required (<=128 bytes); value <=1024 bytes")
	}
	s := a.s
	s.operation.Lock()
	defer s.operation.Unlock()
	start := time.Now()
	s.mu.Lock()
	w, r := s.w, s.r
	s.mu.Unlock()
	latest, _, err := s.gather(args.Key, nil, r)
	var item Versioned
	var responders []string
	if err == nil {
		s.mu.Lock()
		s.disk.Counter = max(s.disk.Counter, latest.Version.Counter) + 1
		item = Versioned{Value: args.Value, Version: Version{Counter: s.disk.Counter, Writer: s.id}}
		err = s.persistLocked()
		s.mu.Unlock()
	}
	if err == nil {
		_, responders, err = s.gather(args.Key, &item, w)
	}
	*reply = Result{Key: args.Key, Item: item, Responders: responders, WaitedMs: float64(time.Since(start).Microseconds()) / 1000, Success: err == nil, Note: "timeout is not rollback; inspect replicas"}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *reply
	s.lastPut = &copy
	s.lastError = ""
	if err != nil {
		s.lastError = err.Error()
	}
	_ = s.publishLocked()
	return err
}
func (a *Application) Get(args KeyArgs, reply *Result) error {
	if args.Key == "" {
		return fmt.Errorf("key required")
	}
	s := a.s
	s.operation.Lock()
	defer s.operation.Unlock()
	start := time.Now()
	s.mu.Lock()
	w, r := s.w, s.r
	s.mu.Unlock()
	item, responders, err := s.gather(args.Key, nil, r)
	// Write back what was read before replying. This prevents a later read from
	// going backwards after seeing a partially completed concurrent write.
	if err == nil {
		_, _, err = s.gather(args.Key, &item, w)
	}
	*reply = Result{Key: args.Key, Item: item, Responders: responders, WaitedMs: float64(time.Since(start).Microseconds()) / 1000, Success: err == nil, Stale: item.Version.Counter < args.MinCounter, Note: "stale compares against caller minCounter; no replica knows an unreachable global latest value"}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *reply
	s.lastGet = &copy
	s.lastError = ""
	if err != nil {
		s.lastError = err.Error()
	}
	_ = s.publishLocked()
	return err
}

type ModeArgs struct {
	W int `json:"w"`
	R int `json:"r"`
}

func (a *Application) Mode(args ModeArgs, reply *State) error {
	s := a.s
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if args.W < 1 || args.R < 1 || args.W > len(s.members) || args.R > len(s.members) {
		return fmt.Errorf("quorum sizes must be 1..N")
	}
	s.w, s.r = args.W, args.R
	*reply = s.stateLocked()
	return s.publishLocked()
}
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.s
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.publishLocked()
}

package main

import (
	"fmt"
	"net/rpc"
	"sync"
	"time"

	lab "distvis/sdk/rpc"
)

// NTP here means the lecture's algorithm, NOT an implementation of the NTP
// packet format, UDP port 123, authentication, leap seconds, or clock discipline.
// All exchanges are ordinary Go RPCs carried by DistVis's controlled network.
type Empty struct{}
type SyncArgs struct {
	Servers      []string `json:"servers" distvis:"nodes"` // Empty selects our immediate upstream.
	Samples      int      `json:"samples"`                 // Zero defaults to three; 1..8 per server.
	ProcessingMS int      `json:"processingMS"`            // Artificial server work, 0..500 ms.
}

type ExchangeArgs struct {
	T0           float64 `json:"t0"` // Client send timestamp, echoed for explanation.
	ProcessingMS int     `json:"processingMS"`
}
type ExchangeReply struct {
	T0      float64 `json:"t0"`
	T1      float64 `json:"t1"` // Server receive timestamp.
	T2      float64 `json:"t2"` // Server reply timestamp, after processing.
	Stratum int     `json:"stratum"`
}

// A Sample exposes the raw timestamps and the two derived metrics. A failed
// sample is kept with its Error, but is NEVER a candidate for the filter.
type Sample struct {
	Server   string  `json:"server"`
	Number   int     `json:"number"`
	T0       float64 `json:"t0"`
	T1       float64 `json:"t1"`
	T2       float64 `json:"t2"`
	T3       float64 `json:"t3"`
	DelayMS  float64 `json:"delayMS"`
	OffsetMS float64 `json:"offsetMS"`
	Stratum  int     `json:"stratum"`
	Error    string  `json:"error,omitempty"` // omitempty leaves an empty error out of JSON.
}
type Round struct {
	Samples  []Sample `json:"samples"`
	Selected int      `json:"selected"` // Index into Samples, -1 if no valid measurement.
}

// Four timestamps eliminate server processing time from the RTT estimate:
//
//	delay  = (T3 - T0) - (T2 - T1)
//	offset = ((T1 - T0) + (T2 - T3)) / 2
//
// Positive offset means the SERVER is ahead: ADD offset to the CLIENT clock.
// Symmetric one-way delays are still an assumption, not something NTP proves.
func measure(t0, t1, t2, t3 float64) (Sample, error) {
	if !finite(t0) || !finite(t1) || !finite(t2) || !finite(t3) || t3 < t0 || t2 < t1 {
		return Sample{}, fmt.Errorf("timestamps must be finite and nondecreasing on each clock")
	}
	delay := (t3 - t0) - (t2 - t1)
	if delay < 0 {
		return Sample{}, fmt.Errorf("negative network delay: clock changed or sample is invalid")
	}
	return Sample{T0: t0, T1: t1, T2: t2, T3: t3, DelayMS: delay,
		OffsetMS: ((t1 - t0) + (t2 - t3)) / 2}, nil
}

// lowestDelay implements the lecture's filter, not an arithmetic mean. Keeping
// the first minimum makes ties deterministic despite goroutine scheduling.
func lowestDelay(samples []Sample) int {
	best := -1
	for i, sample := range samples {
		if sample.Error == "" && finite(sample.DelayMS) && finite(sample.OffsetMS) && sample.DelayMS >= 0 &&
			sample.Stratum >= 1 && sample.Stratum < 15 &&
			(best == -1 || sample.DelayMS < samples[best].DelayMS) {
			best = i
		}
	}
	return best
}

type State struct {
	Node      string    `json:"node"`
	Role      string    `json:"role"`
	Stratum   int       `json:"stratum"` // 1 = laboratory reference, 16 = unsynchronized.
	Upstream  string    `json:"upstream"`
	Eligible  []string  `json:"eligibleServers"`
	Clock     ClockView `json:"clock"`
	Busy      bool      `json:"busy"`
	Last      *Round    `json:"last"`
	LastError string    `json:"lastError"`
}

type TimeService struct {
	mu         sync.Mutex
	clock      clock
	busy       bool
	generation uint64 // Detects a clock adjustment during a server exchange.
	stratum    int
	upstream   string
	last       *Round
	lastError  string
	id         string
	index      int
	members    []string
	peers      map[string]*rpc.Client
	report     func(any) error
}
type Application struct{ service *TimeService }

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
	s := &TimeService{id: node.ID, index: index, members: node.Nodes, peers: peers, report: node.Report,
		stratum: 16, clock: newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 750})}
	if index == 0 {
		s.stratum = 1
	}
	if err := node.RegisterName("TimeService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	return s.report(s.stateLocked())
}

func (s *TimeService) stateLocked() State {
	role := "time-server/client"
	if s.index == 0 {
		role = "laboratory-reference"
	}
	// A slice is a view into an array. Here members is immutable, so this
	// prefix view is safe to share. The fixed membership rank forbids loops:
	// node-3 may use node-1 or node-2, never itself or a later-ranked node.
	return State{Node: s.id, Role: role, Stratum: s.stratum, Upstream: s.upstream,
		Eligible: s.members[:s.index], Clock: s.clock.view(time.Now()), Busy: s.busy,
		Last: s.last, LastError: s.lastError}
}

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}

func (a *Application) ConfigureClock(args ClockArgs, reply *State) error {
	if err := validateClock(args); err != nil {
		return err
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("wait for this node's sampling round to finish")
	}
	s.clock = newClock(time.Now(), args)
	s.generation++
	s.last, s.lastError, s.upstream = nil, "", ""
	s.stratum = 16 // Changing a clock invalidates its synchronization claim.
	if s.index == 0 {
		s.stratum = 1 // The lab reference is a chosen authority, not verified UTC.
	}
	*reply = s.stateLocked()
	return s.report(*reply)
}

// Exchange is the internal time-server RPC. Its two timestamps bracket the
// optional simulated workload. Reading only one timestamp would be Cristian,
// which cannot subtract this explicit server processing interval.
func (s *TimeService) Exchange(args ExchangeArgs, reply *ExchangeReply) error {
	if !finite(args.T0) || args.ProcessingMS < 0 || args.ProcessingMS > 500 {
		return fmt.Errorf("need a finite t0 and processingMS in 0..500")
	}
	s.mu.Lock()
	if s.stratum == 16 {
		s.mu.Unlock()
		return fmt.Errorf("%s has not synchronized yet; synchronize it upstream first", s.id)
	}
	t1, version, stratum := s.clock.read(time.Now()), s.generation, s.stratum
	s.mu.Unlock()
	// time.Sleep blocks only this goroutine, not the process. Never sleep with
	// mu held: Read and other exchanges should remain responsive.
	time.Sleep(time.Duration(args.ProcessingMS) * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != version {
		return fmt.Errorf("server clock changed between T1 and T2; discard this exchange")
	}
	*reply = ExchangeReply{T0: args.T0, T1: t1, T2: s.clock.read(time.Now()), Stratum: stratum}
	return nil
}

// query owns its returned Sample; no other goroutine writes that object. The
// busy flag prevents our own clock from changing between T0 and T3 (ordinary
// drift still continues). The server separately checks its clock generation.
func (s *TimeService) query(server string, number, processingMS int) Sample {
	s.mu.Lock()
	t0 := s.clock.read(time.Now())
	s.mu.Unlock()
	response, err := callRPC[ExchangeReply](s.peers[server], "TimeService.Exchange",
		ExchangeArgs{T0: t0, ProcessingMS: processingMS})
	s.mu.Lock()
	t3 := s.clock.read(time.Now())
	s.mu.Unlock()
	out := Sample{Server: server, Number: number, T0: t0, T3: t3}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	measured, err := measure(t0, response.T1, response.T2, t3)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	measured.Server, measured.Number, measured.Stratum = server, number, response.Stratum
	if response.T0 != t0 || response.Stratum < 1 || response.Stratum >= 15 {
		measured.Error = "invalid echo or server stratum"
	}
	return measured
}

func (a *Application) Sync(args SyncArgs, reply *Round) (err error) {
	s := a.service
	if s.index == 0 {
		return fmt.Errorf("the reference does not synchronize to its descendants")
	}
	if args.Samples == 0 {
		args.Samples = 3
	}
	if len(args.Servers) == 0 {
		args.Servers = []string{s.members[s.index-1]}
	}
	if args.Samples < 1 || args.Samples > 8 || len(args.Servers)*args.Samples > 16 ||
		args.ProcessingMS < 0 || args.ProcessingMS > 500 {
		return fmt.Errorf("use 1..8 samples/server, at most 16 total, and processingMS in 0..500")
	}
	seen := make(map[string]bool)
	for _, peer := range args.Servers {
		allowed := false
		for _, upstream := range s.members[:s.index] {
			allowed = allowed || peer == upstream
		}
		if !allowed || seen[peer] {
			return fmt.Errorf("server %q must be a distinct earlier-ranked member", peer)
		}
		seen[peer] = true
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("this node is already sampling")
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy, s.lastError = false, ""
		if err != nil {
			s.lastError = err.Error()
		}
		if reportErr := s.report(s.stateLocked()); err == nil {
			err = reportErr
		}
	}()

	// Allocate the full slice first, then give each goroutine its OWN element.
	// Do not append concurrently: appending changes the shared slice header.
	// Parallel samples keep one failed server from multiplying round latency;
	// queuing is part of each measured delay, so the minimum-delay filter still
	// applies. This bounded burst is a teaching choice, not NTP's poll scheduler.
	round := Round{Samples: make([]Sample, len(args.Servers)*args.Samples), Selected: -1}
	var workers sync.WaitGroup
	for i, server := range args.Servers {
		for j := 0; j < args.Samples; j++ {
			workers.Add(1)
			go func(slot int, peer string, number int) {
				defer workers.Done()
				round.Samples[slot] = s.query(peer, number, args.ProcessingMS)
			}(i*args.Samples+j, server, j+1)
		}
	}
	workers.Wait() // Ensures every write is complete before the filter reads it.
	round.Selected = lowestDelay(round.Samples)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = &round // Keep failed samples visible too.
	*reply = round
	if round.Selected == -1 {
		return fmt.Errorf("no valid synchronized upstream sample; clock left unchanged")
	}
	best := round.Samples[round.Selected]
	// Offset, not a stale absolute T3, is applied at the current instant.
	// No adjustment is made between samples, or their offsets would be based
	// on different client clocks and could not be compared meaningfully.
	s.clock.step(best.OffsetMS, time.Now())
	s.generation++
	s.stratum, s.upstream = best.Stratum+1, best.Server
	return nil
}

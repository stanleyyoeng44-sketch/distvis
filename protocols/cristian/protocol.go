package main

import (
	"fmt"
	"net/rpc"
	"sync" // Mutex serializes access to shared memory across RPC goroutines.
	"time"

	lab "distvis/sdk/rpc"
)

// Empty is the request type for actions without fields. An empty struct occupies
// no data storage; the named, exported type is suitable for net/rpc reflection.
type Empty struct{}
type TimeReply struct {
	ServerMS float64 `json:"serverMS"`
}
type SyncArgs struct {
	// A user-supplied lower bound, NOT learned from the RTT. Zero is the safe
	// choice when no positive minimum one-way network delay is known.
	MinDelayMS float64 `json:"minDelayMS"`
}

// Measurement exposes every term of the lecture's calculation. These values
// all refer to receipt of this particular response, not to the later UI read.
type Measurement struct {
	Server        string  `json:"server"`
	ServerMS      float64 `json:"serverMS"`
	RTTMS         float64 `json:"rttMS"`
	MinDelayMS    float64 `json:"minDelayMS"`
	BeforeMS      float64 `json:"beforeMS"`
	EstimatedMS   float64 `json:"estimatedMS"`
	CorrectionMS  float64 `json:"correctionMS"`
	EarliestMS    float64 `json:"earliestMS"`
	LatestMS      float64 `json:"latestMS"`
	UncertaintyMS float64 `json:"uncertaintyMS"`
}

// estimate is deliberately pure: no network, clock reads, or mutation. Tests
// can check the equation exactly, independently of Linux/Docker scheduling.
func estimate(serverMS, beforeMS, rttMS, minDelayMS float64) (Measurement, error) {
	if !finite(serverMS) || !finite(beforeMS) || !finite(rttMS) || !finite(minDelayMS) ||
		rttMS < 0 || minDelayMS < 0 || 2*minDelayMS > rttMS {
		return Measurement{}, fmt.Errorf("need finite timestamps and 0 <= 2*minDelayMS <= RTT")
	}
	target := serverMS + rttMS/2
	return Measurement{
		ServerMS: serverMS, RTTMS: rttMS, MinDelayMS: minDelayMS,
		BeforeMS: beforeMS, EstimatedMS: target, CorrectionMS: target - beforeMS,
		EarliestMS: serverMS + minDelayMS, LatestMS: serverMS + rttMS - minDelayMS,
		UncertaintyMS: rttMS/2 - minDelayMS,
	}, nil
}

// State is the full object reported to DistVis. *Measurement is a pointer so
// nil can mean "no successful synchronization yet" rather than a fake zero sample.
type State struct {
	Node      string       `json:"node"`
	Role      string       `json:"role"`
	Server    string       `json:"server"`
	Clock     ClockView    `json:"clock"`
	Busy      bool         `json:"busy"`
	Last      *Measurement `json:"last"`
	LastError string       `json:"lastError"`
}

// TimeService owns all mutable state. peers/id/reference/report are assigned
// once in configure and never changed. Only clock, busy, last and lastError
// need mu. A map is a key/value lookup table; these clients exclude ourselves.
type TimeService struct {
	mu        sync.Mutex
	clock     clock
	busy      bool
	last      *Measurement
	lastError string
	id        string
	reference string
	peers     map[string]*rpc.Client
	report    func(any) error // A function value also makes reporting easy to fake in tests.
}

// Application has only the methods a student may invoke. TimeService is a
// separate internal service; its Time method must not become a UI action.
type Application struct{ service *TimeService }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err // Go returns errors explicitly rather than throwing exceptions.
	}
	index := 0
	for i, id := range node.Nodes { // range yields both the index and its value.
		if id == node.ID {
			index = i
		}
	}
	s := &TimeService{ // & allocates a value and takes its address (a pointer).
		id: node.ID, reference: node.Nodes[0], peers: peers, report: node.Report,
		clock: newClock(time.Now(), ClockArgs{OffsetMS: float64(index) * 750}),
	}
	if err := node.RegisterName("TimeService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	// No initial RPC here: setup runs before message dispatch. This lesson is
	// manually driven, so no OnStart goroutine or periodic traffic is needed.
	return s.report(s.stateLocked())
}

// The Locked suffix documents a precondition: hold mu, or call during setup
// before the object is shared. Keeping report under the same lock prevents a
// slow older report from overwriting a newer state in the observer's display.
func (s *TimeService) stateLocked() State {
	role := "client"
	if s.id == s.reference {
		role = "reference-server"
	}
	return State{Node: s.id, Role: role, Server: s.reference, Clock: s.clock.view(time.Now()),
		Busy: s.busy, Last: s.last, LastError: s.lastError}
}

// net/rpc requires: exported method, exported request/reply types, a pointer
// reply, and exactly one error result. *reply writes through the pointer into
// the object the RPC runtime will serialize. No operating-system time is set.
func (s *TimeService) Time(_ Empty, reply *TimeReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id != s.reference {
		return fmt.Errorf("only %s is the reference server", s.reference)
	}
	// Timestamp at handler receipt; no intentional processing delay. Any real
	// handler/serialization/queue delay still contributes to Cristian's error.
	reply.ServerMS = s.clock.read(time.Now())
	return nil
}

func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.report(*reply)
}

// ConfigureClock intentionally resets the SOFTWARE clock for an experiment.
// This is an external disturbance, not part of the synchronization algorithm.
func (a *Application) ConfigureClock(args ClockArgs, reply *State) error {
	if err := validateClock(args); err != nil {
		return err
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("a synchronization is in progress; configure after it finishes")
	}
	s.clock = newClock(time.Now(), args)
	s.last, s.lastError = nil, ""
	*reply = s.stateLocked()
	return s.report(*reply)
}

// A named error result lets the deferred cleanup attach a reporting error only
// when the operation itself succeeded. defer always runs before the caller sees
// the result, including on an early return after an RPC failure.
func (a *Application) Sync(args SyncArgs, reply *Measurement) (err error) {
	s := a.service
	if !finite(args.MinDelayMS) || args.MinDelayMS < 0 {
		return fmt.Errorf("minDelayMS must be finite and nonnegative")
	}
	s.mu.Lock()
	if s.id == s.reference || s.busy {
		s.mu.Unlock()
		return fmt.Errorf("Sync requires an idle client, not the reference server")
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy = false
		s.lastError = ""
		if err != nil {
			s.lastError = err.Error()
		}
		if reportErr := s.report(s.stateLocked()); err == nil {
			err = reportErr
		}
	}()

	// Never hold mu across network I/O. Another goroutine must still be able
	// to serve Read; a shared lock held across peer calls can cause deadlocks.
	start := time.Now()
	response, err := callRPC[TimeReply](s.peers[s.reference], "TimeService.Time", Empty{})
	end := time.Now()
	if err != nil {
		return err // A failed sample must NOT adjust the clock.
	}
	s.mu.Lock()
	defer s.mu.Unlock() // Registered later, so this unlock runs BEFORE cleanup above.
	before := s.clock.read(end)
	measurement, err := estimate(response.ServerMS, before, end.Sub(start).Seconds()*1000, args.MinDelayMS)
	if err != nil {
		return err
	}
	measurement.Server = s.reference
	s.clock.step(measurement.CorrectionMS, time.Now())
	s.last = &measurement
	*reply = measurement
	return nil
}

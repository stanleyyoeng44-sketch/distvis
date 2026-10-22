package main

import (
	"fmt"
	"math"
	"net/rpc"
	"sync"
	"time"
)

// ─── Messages ────────────────────────────────────────────────────────────────
// These plain structs are what nodes send each other (gob over net/rpc). The
// request structs of Application methods also become the input forms in the
// DistVis UI: the json tags are the field names you type there.

// Empty is a request with no fields at all.
type Empty struct{}

// PollReply is a node's answer to "what time do you think it is?" (step 1).
type PollReply struct {
	TimeMS     float64 `json:"timeMS"`     // This node's own software clock reading.
	Instance   string  `json:"instance"`   // Boot identity: which restart of this node answered.
	Generation uint64  `json:"generation"` // Changes whenever this clock is reset or adjusted.
}

// AdjustArgs is the coordinator's instruction to one node (step 3): "the group
// decided your clock is off by DeltaMS; pay that correction off gradually".
type AdjustArgs struct {
	Coordinator     string  `json:"coordinator"`     // Only the real coordinator may adjust us.
	CoordinatorBoot string  `json:"coordinatorBoot"` // Which restart of the coordinator sent it.
	Round           uint64  `json:"round"`           // Must be newer than the last accepted round.
	Instance        string  `json:"instance"`        // Must match the identity captured in step 1.
	Generation      uint64  `json:"generation"`      // Must match the generation captured in step 1.
	DeltaMS         float64 `json:"deltaMS"`         // RELATIVE correction in ms, never an absolute time.
}

type AdjustReply struct {
	Scheduled bool      `json:"scheduled"` // Accepted and started; NOT the same as finished slewing.
	Clock     ClockView `json:"clock"`     // This node's clock right after scheduling.
}

// ComputeArgs configures step 2.
type ComputeArgs struct {
	OutlierThresholdMS float64 `json:"outlierThresholdMS"` // Max distance from the median; 0 means the 2000 ms default.
}

// Sample is one node's row in the round worksheet. Each step fills in its own
// columns, so the whole table is a complete picture of one round:
//
//	step 1  Node, TimeMS, RTTMS, OffsetMS, Instance, Generation
//	step 2  Included, DeltaMS
//	step 3  Scheduled   (or Error, when something went wrong for this node)
type Sample struct {
	Node       string  `json:"node"`
	TimeMS     float64 `json:"timeMS"`     // What this node reported in step 1.
	RTTMS      float64 `json:"rttMS"`      // Poll round-trip time; 0 for the coordinator itself.
	OffsetMS   float64 `json:"offsetMS"`   // This clock minus the coordinator clock, estimated at receipt.
	Instance   string  `json:"instance"`   // Boot identity captured in step 1.
	Generation uint64  `json:"generation"` // Clock generation captured in step 1.
	Included   bool    `json:"included"`   // Step 2: this sample voted on the mean.
	DeltaMS    float64 `json:"deltaMS"`    // Step 2: the correction this node should apply.
	Scheduled  bool    `json:"scheduled"`  // Step 3: this node confirmed the correction.
	Error      string  `json:"error,omitempty"`
}

// Round is the coordinator's worksheet for one synchronization round. Steps 1
// and 2 only fill it in; step 3 is the first one that changes any clock.
type Round struct {
	Number      uint64   `json:"number"`
	Phase       string   `json:"phase"`       // idle | polled | computed | sent
	ThresholdMS float64  `json:"thresholdMS"` // Step 2 input.
	MedianMS    float64  `json:"medianMS"`    // Step 2 output, before filtering.
	MeanMS      float64  `json:"meanMS"`      // Step 2 output: the group's agreed offset.
	Samples     []Sample `json:"samples"`
}

// State is what a node reports to the platform after every action. The
// coordinator additionally reports the worksheet and which step to run next.
type State struct {
	Node        string    `json:"node"`
	Role        string    `json:"role"` // "coordinator" or "participant".
	Coordinator string    `json:"coordinator"`
	Clock       ClockView `json:"clock"`
	Busy        bool      `json:"busy"`             // A step is executing right now.
	Phase       string    `json:"phase"`            // Where the coordinator's round stands.
	NextAction  string    `json:"nextAction"`       // Which Application.* step to run next.
	Round       *Round    `json:"round"`            // The worksheet (coordinator only).
	LastApplied uint64    `json:"lastAppliedRound"` // Newest round number accepted here.
	LastDeltaMS float64   `json:"lastDeltaMS"`      // The correction this node accepted last.
	LastError   string    `json:"lastError"`
}

// ─── Node state ──────────────────────────────────────────────────────────────

// TimeService is the complete state of one node. mu guards every field.
type TimeService struct {
	mu          sync.Mutex
	id          string
	coordinator string
	members     []string
	instance    string                 // Boot identity from crypto/rand; not a timestamp, not authentication.
	peers       map[string]*rpc.Client // Owned by the runtime; never Close them here.
	report      func(any) error

	clock      clock
	generation uint64 // Bumped by ConfigureClock and by every accepted adjustment.

	// Bookkeeping that stops the same correction being applied twice.
	coordinatorBoot string  // Coordinator identity of our first accepted adjustment.
	lastApplied     uint64  // Newest round number we accepted.
	lastDeltaMS     float64 // The correction we accepted in that round.

	busy      bool   // One of the three steps is executing right now.
	lastError string // Why the most recent step failed ("" when it succeeded).

	round *Round // The coordinator's worksheet; nil on participants and before step 1.
}

// Application holds the actions you trigger from the platform UI.
type Application struct{ service *TimeService }

// stateLocked builds the reported snapshot. Callers must hold s.mu. A
// published worksheet is never mutated afterwards (see cloneRound), so the
// shared pointer stays a consistent snapshot even after the lock is released.
func (s *TimeService) stateLocked() State {
	role, nextAction := "participant", ""
	if s.id == s.coordinator {
		role, nextAction = "coordinator", nextActionFor(s.round)
	}
	return State{
		Node: s.id, Role: role, Coordinator: s.coordinator,
		Clock: s.clock.view(time.Now()), Busy: s.busy,
		Phase: phaseOf(s.round), NextAction: nextAction, Round: s.round,
		LastApplied: s.lastApplied, LastDeltaMS: s.lastDeltaMS, LastError: s.lastError,
	}
}

// ─── Node-to-node RPCs (called by the coordinator during a round) ────────────

// Poll is the node's half of step 1: report our clock reading. Reading is
// harmless, but a clock that is still paying off a correction is a moving
// target, so we refuse and let the coordinator try again in a later round.
func (s *TimeService) Poll(_ Empty, reply *PollReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.clock.pending(now) != 0 {
		return fmt.Errorf("clock is still slewing; retry in a later round")
	}
	*reply = PollReply{TimeMS: s.clock.read(now), Instance: s.instance, Generation: s.generation}
	return nil
}

// Adjust is the node's half of step 3: schedule our gradual correction.
//
// Every guard below exists because a caller that timed out does NOT cancel a
// net/rpc method, and a relative correction is not idempotent: applying the
// same delta twice would drift the clock instead of fixing it. So each
// instruction is checked before anything changes, and a failed adjustment is
// never retried automatically.
func (s *TimeService) Adjust(args AdjustArgs, reply *AdjustReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Guard 1: only our known coordinator may adjust us, and only with a sane
	// round number and correction size (120 s is already absurd for one round).
	if args.Coordinator != s.coordinator || args.CoordinatorBoot == "" || args.Round == 0 ||
		!finite(args.DeltaMS) || math.Abs(args.DeltaMS) > 120000 {
		return fmt.Errorf("invalid coordinator, round or correction")
	}
	// Guard 2: a restarted coordinator would restart round numbering from 1.
	// Refuse instead of silently replaying old rounds. (This lesson has no
	// failover protocol — see README "Limits".)
	if s.coordinatorBoot != "" && s.coordinatorBoot != args.CoordinatorBoot {
		return fmt.Errorf("coordinator restarted; start a fresh experiment (no failover protocol)")
	}
	// Guard 3: rounds only move forward. A duplicated or replayed round must
	// never add the same correction twice.
	if args.Round <= s.lastApplied {
		return fmt.Errorf("duplicate or stale adjustment for round %d (already applied round %d)", args.Round, s.lastApplied)
	}
	// Guard 4: the correction must be based on what THIS clock reported in
	// step 1. Any reset or accepted adjustment since then bumps the generation
	// and makes the measurement stale.
	if args.Instance != s.instance || args.Generation != s.generation {
		return fmt.Errorf("clock changed since poll; the measured offset is stale")
	}
	// Guard 5: never overlap corrections. Replacing a pending correction would
	// quietly drop the part that has not been paid off yet.
	now := time.Now()
	if s.clock.pending(now) != 0 {
		return fmt.Errorf("previous correction is still slewing")
	}
	s.clock.slew(args.DeltaMS, now)
	s.generation++
	s.coordinatorBoot, s.lastApplied, s.lastDeltaMS = args.CoordinatorBoot, args.Round, args.DeltaMS
	*reply = AdjustReply{Scheduled: true, Clock: s.clock.view(now)}
	return s.report(s.stateLocked())
}

// ─── Actions available on any node ───────────────────────────────────────────

// Read reports this node's state without changing anything. Watch
// clock.pendingMS fall toward zero while clock.timeMS keeps rising: that is a
// correction being paid off gradually.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	*reply = s.stateLocked()
	s.mu.Unlock()
	return s.report(*reply)
}

// ConfigureClock is an EXTERNAL disturbance for experiments: it rebuilds this
// node's software clock, which may jump time and cancel a pending correction.
// That is NOT what the synchronization algorithm does — the algorithm only
// ever changes a clock gradually through clock.slew (see clock.go).
//
// Calling it between the steps of a round is deliberate. Change a node's clock
// after step 1 and watch step 3 reject that node's now-stale correction while
// the other nodes go through.
func (a *Application) ConfigureClock(args ClockArgs, reply *State) error {
	if err := validateClock(args); err != nil {
		return err
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("a step is running right now; retry in a moment")
	}
	s.clock = newClock(time.Now(), args)
	s.generation++ // Makes every earlier measurement of this clock stale.
	s.lastDeltaMS = 0
	*reply = s.stateLocked()
	return s.report(*reply)
}

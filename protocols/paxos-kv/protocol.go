package main

import (
	"fmt"
	"math/rand/v2"
	"net/rpc"
	"time"
)

// This file implements a replicated key-value store [several nodes execute the
// same agreed sequence of operations to maintain the same data]. Each slot
// [numbered position in that sequence] is a separate Paxos decision. A ballot
// [ordered identifier for a proposal attempt] orders competing attempts for a
// slot; it is not the slot number and does not order application operations.
//
// Read the main request path in this order:
//   Put/Get -> execute -> choose -> majority -> Prepare/Accept -> Decide
//   -> applyLocked -> execute returns the stored Result.
//
// Three different milestones matter:
//   - accepted: one node has durably recorded a proposal;
//   - chosen: a majority has accepted the same proposal for the same slot;
//   - applied: this node has executed the chosen operation after all earlier
//     slots. Learning a later decision does not allow skipping an earlier slot.
//
// Unlike leader-based Multi-Paxos [reusing an established leader's preparation
// across log positions], this implementation prepares each slot separately.

// Empty is used for RPC requests or replies that need no data fields.
type Empty struct{}

// Operation is the complete value that Paxos chooses for a slot. Paxos agrees
// on an operation, not merely the string written to a key. ID stays unchanged
// when execute retries that operation in another slot, allowing applyLocked to
// avoid executing the same operation twice. A separate application call gets a
// new ID, even if its key and value are identical to an earlier call.
type Operation struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "put", "get", or "noop" [do not change the store].
	Key   string `json:"key"`
	Value string `json:"value"` // Input for put; get obtains its value when applied.
}

// Slot records this node's knowledge about one log position. Different nodes
// can temporarily hold different promises or accepted proposals for that slot;
// Paxos prevents them from choosing two different operations for it.
type Slot struct {
	// Promised is the ballot below which this node refuses Prepare and Accept.
	// A promise restricts future votes; it does not mean a value was accepted.
	Promised uint64 `json:"promised"`
	// Accepted is the ballot recorded with an accepted proposal (or updated
	// when learning a decision). Zero means there is no recorded acceptance.
	// Prepare replies include this separately from Promised: a newer promise
	// may coexist with a value accepted under an older ballot.
	Accepted uint64    `json:"accepted"`
	Value    Operation `json:"value"` // Accepted operation, or learned decision.
	// Decided means this node has learned which operation was chosen. Merely
	// accepting a value does not set it: a single acceptance is not a majority.
	Decided bool `json:"decided"`
}

// Result records the outcome of executing an operation at its log position.
// Found distinguishes a missing key from a key whose stored string is empty.
// Error is also used for the local last-operation display when a request fails.
type Result struct {
	ID    string `json:"id"`
	Slot  int    `json:"slot"`
	Value string `json:"value"`
	Found bool   `json:"found"`
	Error string `json:"error"`
}

// Disk is saved as one state object. Acceptor promises and votes must survive
// a crash; otherwise a restarted node might vote for a proposal it had promised
// to reject. The applied position, store, and saved results are kept together
// so restoring the store also restores how far execution has progressed.
type Disk struct {
	Ballot   uint64            `json:"ballot"`   // Local round counter, not the full encoded ballot.
	Sequence uint64            `json:"sequence"` // Counter used to generate this node's operation IDs.
	Slots    map[int]Slot      `json:"slots"`    // Per-slot promises, acceptances, and decisions.
	Applied  int               `json:"applied"`  // Highest slot executed with no earlier slot missing.
	Values   map[string]string `json:"values"`   // Store produced by executing slots 1 through Applied.
	Results  map[string]Result `json:"results"`  // Saved outcomes, indexed by operation ID.
}

// State is a local snapshot for the DistVis display. It is not a message asking
// peers to agree, and publishing it does not save Disk or replicate the store.
type State struct {
	Role          string            `json:"role"`
	Phase         string            `json:"phase"`
	Log           map[int]Slot      `json:"log"`
	Applied       int               `json:"applied"`
	Values        map[string]string `json:"values"`
	LastOperation *Result           `json:"lastOperation"`
	Conflicts     uint64            `json:"conflicts"`    // Times execute encountered another operation in its target slot.
	MessagesSent  uint64            `json:"messagesSent"` // Outgoing RPC attempts; local calls are not counted.
	Amnesia       bool              `json:"amnesia"`
	StorageError  string            `json:"storageError"`
}

// PrepareArgs asks an acceptor to promise not to accept lower ballots for Slot.
// It carries no proposed operation: the proposer must first inspect the values
// reported by a majority before deciding what it is allowed to propose.
type PrepareArgs struct {
	Slot   int    `json:"slot"`
	Ballot uint64 `json:"ballot"`
}

// Promise is reused for both phases. For Prepare, OK means the promise was
// saved; for Accept, OK means the proposed operation was saved as accepted.
// Slot is returned on rejection too, letting the proposer discover a higher
// promise and use a sufficiently high ballot on a later retry.
type Promise struct {
	OK   bool `json:"ok"`
	Slot Slot `json:"slot"`
}

// AcceptArgs requests a vote for a specific operation at a specific ballot.
// choose may send an operation learned during Prepare rather than the one the
// application originally wanted: preserving earlier acceptances is mandatory.
type AcceptArgs struct {
	Slot   int       `json:"slot"`
	Ballot uint64    `json:"ballot"`
	Value  Operation `json:"value"`
}

// DecisionArgs announces an operation after a proposer obtains an accepting
// majority. The message itself does not contain proof of those votes; Decide
// trusts protocol participants to announce only genuinely chosen operations.
// This is not Byzantine fault tolerance [protection against dishonest nodes].
type DecisionArgs struct {
	Slot   int       `json:"slot"`
	Ballot uint64    `json:"ballot"`
	Value  Operation `json:"value"`
}

// stateLocked copies maps while holding s.mu. Returning the live maps would let
// callers observe later mutations and could race [access the same changing data
// concurrently] with a Paxos handler. Slot and string values copy by value.
func (s *Server) stateLocked() State {
	slots := make(map[int]Slot, len(s.disk.Slots))
	for k, v := range s.disk.Slots {
		slots[k] = v
	}
	values := make(map[string]string, len(s.disk.Values))
	for k, v := range s.disk.Values {
		values[k] = v
	}
	return State{Role: "proposer-acceptor-learner", Phase: s.phase, Log: slots, Applied: s.disk.Applied, Values: values, LastOperation: s.last, Conflicts: s.conflicts, MessagesSent: s.messages, Amnesia: s.amnesia, StorageError: s.storageError}
}

// publishLocked reports a complete display snapshot, not a persistence record.
func (s *Server) publishLocked() error { return s.report(s.stateLocked()) }

// persistLocked saves state before acknowledging promises, acceptances, or
// decisions. Holding s.mu prevents overlapping saves of changing state.
// A failed save is remembered: even if memory was already updated, the node
// must not treat that unsaved update as durable and later acknowledge success.
func (s *Server) persistLocked() error {
	if s.storageError != "" {
		return fmt.Errorf("storage stopped: %s", s.storageError)
	}
	if s.amnesia {
		return nil
	} // Deliberate unsafe variant; never enabled by default.
	if err := s.save(s.disk); err != nil {
		s.storageError = err.Error()
		return err
	}
	return nil
}

// Prepare is the acceptor side of phase 1 [establish a ballot and discover any
// earlier accepted value]. This handler decides only this node's response;
// majority, on the proposer, decides whether enough nodes have promised.
func (s *Server) Prepare(args PrepareArgs, reply *Promise) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if args.Slot < 1 || args.Slot > 1000 || args.Ballot == 0 {
		return fmt.Errorf("invalid slot or ballot")
	}
	if s.storageError != "" {
		return fmt.Errorf("storage stopped")
	}
	// Looking up a missing map entry gives an empty Slot: promise and accepted
	// ballot are zero, and Decided is false. No value has been recorded yet.
	slot := s.disk.Slots[args.Slot]
	// Equality permits a repeated prepare for the same ballot. A lower ballot
	// is rejected without changing the promise or the previously accepted value.
	if args.Ballot >= slot.Promised {
		slot.Promised = args.Ballot
		s.disk.Slots[args.Slot] = slot
		if err := s.persistLocked(); err != nil {
			return err
		}
		// Do not reply positively before the promise is saved across crashes.
		reply.OK = true
	}
	// A rejected request still learns our current promise. A successful one
	// also learns Accepted and Value, which choose needs for value selection.
	reply.Slot = slot
	// Seeing a later slot exposes holes [earlier positions whose decisions we
	// do not know], including after a crash. Try to recover each through Paxos,
	// not by assuming an empty slot or copying an arbitrary peer's value.
	for i := s.disk.Applied + 1; i < args.Slot; i++ {
		if !s.disk.Slots[i].Decided && !s.catching[i] {
			s.catching[i] = true
			// Start recovery separately so this Prepare can return its promise.
			// fillHole will acquire s.mu after this handler releases it.
			go s.fillHole(i)
		}
	}
	return s.publishLocked()
}

// Accept is the acceptor side of phase 2 [record a vote for a specific value].
// An acceptor need not have replied to this proposer's Prepare: it can accept
// if its own promise and recorded value permit it. The proposer is responsible
// for obtaining a prepare majority and following the value-selection rule.
func (s *Server) Accept(args AcceptArgs, reply *Promise) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if args.Slot < 1 || args.Slot > 1000 || args.Ballot == 0 || args.Value.ID == "" {
		return fmt.Errorf("invalid accept")
	}
	if s.storageError != "" {
		return fmt.Errorf("storage stopped")
	}
	slot := s.disk.Slots[args.Slot]
	// All three conditions must hold:
	// 1. Do not violate a promise to reject lower ballots.
	// 2. Do not replace a learned decision with a different operation.
	// 3. Do not accept two different operations under the same ballot.
	// Repeating the same value at the same ballot is allowed.
	if args.Ballot >= slot.Promised && (!slot.Decided || slot.Value == args.Value) && !(slot.Accepted == args.Ballot && slot.Value != args.Value) {
		// Also raise Promised: after accepting at this ballot, older ballots
		// must not be accepted even if this node did not see this Prepare.
		slot.Promised, slot.Accepted, slot.Value = args.Ballot, args.Ballot, args.Value
		s.disk.Slots[args.Slot] = slot
		if err := s.persistLocked(); err != nil {
			return err
		}
		reply.OK = true
	}
	// No call to applyLocked here: this is only one node's vote, not knowledge
	// that a majority accepted. Accepting alone must not change the KV store.
	reply.Slot = slot
	return s.publishLocked()
}

// Decide is the learner's handler [record the result of a successful majority
// vote]. It can arrive before an earlier slot's decision or on a node that did
// not vote for this operation. applyLocked handles the required execution order.
func (s *Server) Decide(args DecisionArgs, _ *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if args.Slot < 1 || args.Slot > 1000 || args.Value.ID == "" {
		return fmt.Errorf("invalid decision")
	}
	slot := s.disk.Slots[args.Slot]
	if slot.Decided && slot.Value != args.Value {
		return fmt.Errorf("conflicting decision for slot %d", args.Slot)
	}
	// Re-announcing the same decision is harmless. Keep the recorded accepted
	// ballot from moving backwards when decision messages arrive late.
	slot.Value, slot.Decided = args.Value, true
	slot.Accepted = max(slot.Accepted, args.Ballot)
	s.disk.Slots[args.Slot] = slot
	s.applyLocked()
	// Save both the learned decision and any application changes it enabled
	// before this handler reports success.
	if err := s.persistLocked(); err != nil {
		return err
	}
	return s.publishLocked()
}

// applyLocked executes the contiguous decided prefix [all known decisions from
// slot 1 up to the first missing decision]. For example, knowing slot 2 is Get(x)
// is insufficient while slot 1 is unknown: slot 1 might be Put(x, "new").
func (s *Server) applyLocked() {
	for {
		number := s.disk.Applied + 1
		slot := s.disk.Slots[number]
		if !slot.Decided {
			return
		}
		op := slot.Value
		// Retrying the same ID in multiple slots must not execute it twice or
		// change its saved result. In particular, a repeated get keeps the
		// value observed at its first execution, not a value from a later slot.
		if _, done := s.disk.Results[op.ID]; !done {
			result := Result{ID: op.ID, Slot: number}
			switch op.Kind {
			case "put":
				s.disk.Values[op.Key] = op.Value
				result.Value, result.Found = op.Value, true
			case "get":
				// The two-result map lookup distinguishes an absent key from
				// an existing key whose value happens to be an empty string.
				result.Value, result.Found = s.disk.Values[op.Key]
			case "noop":
				// Recovery may choose an operation that changes nothing. It
				// still occupies a slot and lets later decisions be applied.
			}
			s.disk.Results[op.ID] = result
		}
		// Advance even for a duplicate ID: this slot is accounted for, although
		// its operation was already executed at an earlier position.
		s.disk.Applied = number
	}
}

// rpc calls a Paxos handler locally or sends an RPC [remote method call] to a
// peer. This node participates in majorities too; local calls follow exactly
// the same promise, acceptance, persistence, and locking rules as remote calls.
func (s *Server) rpc(id, method string, args, reply any) error {
	if id == s.id {
		switch method {
		case "Paxos.Prepare":
			return s.Prepare(args.(PrepareArgs), reply.(*Promise))
		case "Paxos.Accept":
			return s.Accept(args.(AcceptArgs), reply.(*Promise))
		case "Paxos.Decide":
			return s.Decide(args.(DecisionArgs), reply.(*Empty))
		}
	}
	s.mu.Lock()
	s.messages++
	s.mu.Unlock()
	// Go starts an asynchronous call [send now, receive completion later].
	// The one-element completion channel lets net/rpc finish even if our
	// timer fires first and this function has already returned.
	call := s.peers[id].Go(method, args, reply, make(chan *rpc.Call, 1))
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case done := <-call.Done:
		return done.Error
	case <-timer.C:
		// This stops waiting; it does not cancel the remote handler or prevent
		// a late reply from being written into reply. A timeout therefore does
		// not prove that the peer failed to promise, accept, or learn a value.
		return fmt.Errorf("peer timeout")
	}
}

// response separates a completed protocol reply (which may be a rejection)
// from a call error, such as a timeout with no safely readable reply yet.
type response struct {
	reply Promise
	err   error
}

// majority sends one request to each member and stops at the first successful
// majority [more than half of the fixed membership; 3 out of 5, for example].
// It returns the positive replies, the highest promised ballot observed before
// returning, and an error if it did not obtain enough positive replies.
//
// One rejection is NOT a veto. It does not count as a yes, but we keep waiting
// for other members. Failed calls also do not count. If a majority never arrives,
// this implementation consumes all members' outcomes before returning an error;
// it does not stop early merely because enough rejections make success impossible.
func (s *Server) majority(method string, args any) ([]Promise, uint64, error) {
	// Each task sends exactly one outcome. Buffering for every member lets
	// slower tasks finish without blocking after we have already returned
	// with a majority and no longer receive from this channel.
	ch := make(chan response, len(s.members))
	for _, id := range s.members {
		go func(peer string) {
			var reply Promise
			err := s.rpc(peer, method, args, &reply)
			if err != nil {
				// Especially on timeout, net/rpc may still be writing reply.
				// Send only the error; copying reply here could race with it.
				ch <- response{err: err}
				return
			}
			ch <- response{reply: reply}
		}(id)
	}
	var yes []Promise
	var highest uint64
	for range s.members {
		r := <-ch
		if r.err != nil {
			continue
		}
		// Include rejected replies in the retry hint. Promised tells us which
		// ballot a future attempt must exceed; it does NOT select the value.
		highest = max(highest, r.reply.Slot.Promised)
		if r.reply.OK {
			yes = append(yes, r.reply)
		}
		if len(yes) > len(s.members)/2 {
			// Late replies are not part of this returned set. We neither wait
			// for the slowest node nor cancel requests still in progress.
			return yes, highest, nil
		}
	}
	return yes, highest, fmt.Errorf("no majority")
}

// choose tries to learn or choose ONE operation for ONE slot. Its successful
// return value may differ from wanted: a prepare majority can require adopting
// a previously accepted operation. execute handles retrying the user's own
// operation in a later slot rather than overwriting the other operation.
func (s *Server) choose(number int, wanted Operation, deadline time.Time) (Operation, error) {
	// seen tracks the highest promise observed during this choose call, so a
	// rejection helps the next attempt use a high enough ballot.
	var seen uint64
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if slot := s.disk.Slots[number]; slot.Decided {
			// Someone may have announced a decision while we were retrying.
			// A known decision never needs a new competition for this slot.
			s.mu.Unlock()
			return slot.Value, nil
		}
		// Ballot = round*16 + node index. With stable indices 1..15, different
		// nodes have distinct ballots and any higher round beats an older one.
		// DistVis's up-to-12-node membership fits this encoding. Dividing seen
		// by 16 extracts its round; incrementing then exceeds that whole round.
		s.disk.Ballot = max(s.disk.Ballot, seen/16) + 1
		ballot := s.disk.Ballot*16 + s.index
		// Persist before using the ballot, so a restart does not reuse an old
		// round to propose a different value under the same ballot identifier.
		err := s.persistLocked()
		s.phase = "prepare"
		_ = s.publishLocked()
		s.mu.Unlock()
		if err != nil {
			return Operation{}, err
		}
		// Do not hold s.mu across network waits: local Prepare/Accept and
		// incoming requests from competing proposers must be able to run.
		promises, higher, err := s.majority("Paxos.Prepare", PrepareArgs{Slot: number, Ballot: ballot})
		seen = max(seen, higher)
		value := wanted
		var accepted uint64
		if err == nil {
			// Select the value with the highest ACCEPTED ballot among this
			// successful prepare majority, not the highest PROMISED ballot
			// and not a hypothetical highest ballot anywhere in the system.
			// If no reply reports an acceptance, keep our own wanted value.
			//
			// A node outside these replies cannot influence this selection.
			// However, any two majorities overlap, and the accept/promise
			// rules preserve an already chosen value through later ballots.
			// Adopting an older acceptance is required even if we do not know
			// whether that acceptance previously reached a majority.
			for _, p := range promises {
				if p.Slot.Accepted > accepted {
					accepted = p.Slot.Accepted
					value = p.Slot.Value
				}
			}
			s.mu.Lock()
			s.phase = "accept"
			_ = s.publishLocked()
			s.mu.Unlock()
			// A prepare majority does not guarantee an accept majority: a
			// competing proposer may obtain higher promises in between.
			// The accept majority may contain different nodes than prepare's.
			_, higher, err = s.majority("Paxos.Accept", AcceptArgs{Slot: number, Ballot: ballot, Value: value})
			seen = max(seen, higher)
			if err == nil {
				// A majority accepted the same slot/ballot/value: the operation
				// is now chosen. Decide teaches that fact; it is not a third
				// voting phase and does not need acknowledgments from everyone.
				decision := DecisionArgs{Slot: number, Ballot: ballot, Value: value}
				if err = s.Decide(decision, &Empty{}); err != nil {
					return Operation{}, err
				}
				for _, id := range s.members {
					if id != s.id {
						// Notify peers in the background. A failed notification
						// does not undo the choice; a lagging node may recover
						// the missing decision through a later fillHole attempt.
						go func(peer string) { _ = s.rpc(peer, "Paxos.Decide", decision, &Empty{}) }(id)
					}
				}
				return value, nil
			}
		}
		// Random backoff [wait 40..199 ms before retrying] reduces the chance
		// of competing proposers repeatedly interrupting each other. Retry
		// starts at Prepare with a new ballot, not by resending Accept alone.
		time.Sleep(time.Duration(40+rand.IntN(160)) * time.Millisecond)
	}
	// The deadline bounds starting retries, not each in-flight handler. Losing
	// replies or stopping our wait cannot undo votes already recorded by peers.
	return Operation{}, fmt.Errorf("no majority before operation deadline; a timed-out proposal may still be chosen")
}

// fillHole recovers an earlier missing decision using the ordinary Paxos path.
// A no-op [operation that changes nothing] is only the preferred value: if the
// prepare majority reports an accepted operation, choose must adopt that instead.
// This preserves previously chosen work whose Decide notification was missed.
func (s *Server) fillHole(number int) {
	// Clear the in-progress flag on success or failure. A later Prepare can
	// trigger another attempt, but there is no automatic retry loop here after
	// this six-second choose attempt finishes.
	defer func() { s.mu.Lock(); delete(s.catching, number); s.mu.Unlock() }()
	_, _ = s.choose(number, Operation{ID: fmt.Sprintf("noop-%s-%d", s.id, number), Kind: "noop"}, time.Now().Add(6*time.Second))
}

// PutArgs is the user-facing write request. PauseMs is a teaching control:
// delaying after selecting a target slot makes concurrent proposals easier to
// arrange. It is not a network delay and is not needed for Paxos correctness.
type PutArgs struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	PauseMs int    `json:"pauseMs"`
}

// GetArgs requests a read ordered through the same log as writes.
type GetArgs struct {
	Key string `json:"key"`
}

// execute keeps trying to place one application operation into the log until
// it can return that operation's applied result or its time allowance expires.
// Choosing another operation for a slot is not an application success: that
// operation is preserved, and this request continues in a later slot.
func (s *Server) execute(kind, key, value string, pause int, reply *Result) error {
	if key == "" || len(key) > 128 || len(value) > 1024 || pause < 0 || pause > 1000 {
		return fmt.Errorf("key required (<=128 bytes), value <=1024 bytes, pauseMs 0..1000")
	}
	// Serialize this node's application operations [run them one at a time],
	// while permitting acceptor/learner handlers to run under the separate mu.
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	s.disk.Sequence++
	op := Operation{ID: fmt.Sprintf("%s-%d", s.id, s.disk.Sequence), Kind: kind, Key: key, Value: value}
	// Save the ID counter before sending the operation, so a restart does not
	// accidentally give a new request the identity of an earlier one.
	err := s.persistLocked()
	// Start at the first position not yet applied locally. It may already be
	// accepted/chosen elsewhere; choose must discover and preserve that work.
	number := s.disk.Applied + 1
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// Capture the slot before the optional pause, making a collision reproducible.
	// The six-second retry deadline starts after this pause, not before it.
	time.Sleep(time.Duration(pause) * time.Millisecond)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		chosen, err := s.choose(number, op, deadline)
		if err != nil {
			s.mu.Lock()
			s.phase = "failed"
			s.last = &Result{ID: op.ID, Error: err.Error()}
			_ = s.publishLocked()
			s.mu.Unlock()
			// Failure to obtain a result is not a rollback: this proposal
			// might still be accepted/chosen after a delayed or lost reply.
			return err
		}
		s.mu.Lock()
		if chosen.ID == op.ID {
			// Being chosen is not enough to return a read result. Earlier
			// slots must have been applied so the store has the correct value
			// at this read's log position. applyLocked records that result.
			result, applied := s.disk.Results[op.ID]
			if applied {
				*reply = result
				s.last = &result
				s.phase = "completed"
				_ = s.publishLocked()
				s.mu.Unlock()
				return nil
			}
		} else {
			// Another operation occupies this slot. Do not call it success
			// for our request and do not try to replace the chosen value.
			s.conflicts++
		}
		// Keep the SAME operation ID when moving on. If our operation was
		// chosen but not yet applied, this can propose it again at a later
		// position; applyLocked's saved results prevent duplicate execution.
		number++
		s.mu.Unlock()
	}
	return fmt.Errorf("operation deadline exceeded")
}

// Put returns success only after this write's operation has an applied result.
// It does not require every replica [node storing a copy] to have caught up.
func (a *Application) Put(args PutArgs, reply *Result) error {
	return a.s.execute("put", args.Key, args.Value, args.PauseMs, reply)
}

// Get is an ordered application read: it consumes a Paxos log slot just like
// Put. Its value is evaluated when that slot is applied, after all preceding
// operations. It requires a majority rather than serving an arbitrary local
// copy that may have missed newer writes.
func (a *Application) Get(args GetArgs, reply *Result) error {
	return a.s.execute("get", args.Key, "", 0, reply)
}

// Read is for inspection only, unlike Get. It returns this node's current
// snapshot without Paxos or any peer contact. During failures or delayed
// notifications, its Values may be behind the state observed by a successful Get.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.s
	s.mu.Lock()
	defer s.mu.Unlock()
	*reply = s.stateLocked()
	return s.publishLocked()
}

// AmnesiaArgs controls the deliberately unsafe persistence experiment.
type AmnesiaArgs struct {
	Enabled bool `json:"enabled"`
}

// Amnesia skips future disk saves when enabled, but leaves current memory
// intact. After a crash the node can therefore forget promises and accepted
// values, breaking the assumptions that make Paxos safe. This is a teaching
// fault, not a supported optimization or a normal recovery mode.
func (a *Application) Amnesia(args AmnesiaArgs, reply *State) error {
	s := a.s
	s.mu.Lock()
	defer s.mu.Unlock()
	// Require the choice before this node has recorded any proposal slots.
	// The option is local to this process and is not included in Disk.
	if len(s.disk.Slots) > 0 {
		return fmt.Errorf("choose amnesia before proposals")
	}
	s.amnesia = args.Enabled
	*reply = s.stateLocked()
	return s.publishLocked()
}

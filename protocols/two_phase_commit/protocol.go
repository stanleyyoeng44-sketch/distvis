package main

import (
	"context"
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

const (
	coordinatorNode = "node-1"

	decisionNone   = ""
	decisionCommit = "commit"
	decisionAbort  = "abort"

	voteYes = "yes"
	voteNo  = "no"

	phaseIdle      = "idle"
	phasePreparing = "preparing"
	phaseDecided   = "decided"
	phaseFinished  = "finished"

	stateIdle      = "idle"
	statePrepared  = "prepared"
	stateUncertain = "uncertain"
	stateCommitted = "committed"
	stateAborted   = "aborted"

	callTimeout    = 2 * time.Second
	statusInterval = 2 * time.Second
)

// call performs a peer RPC with a timeout.
func call(client *rpc.Client, method string, args, reply any) error {
	if client == nil {
		return fmt.Errorf("%s: no connection", method)
	}
	done := make(chan error, 1)
	go func() { done <- client.Call(method, args, reply) }()
	select {
	case err := <-done:
		return err
	case <-time.After(callTimeout):
		return fmt.Errorf("%s: no reply within %s", method, callTimeout)
	}
}

type Empty struct{}

type TransferArgs struct {
	From                  string `json:"from" distvis:"node"`
	To                    string `json:"to" distvis:"node"`
	Amount                int    `json:"amount"`
	PauseBeforeDecisionMs int    `json:"pauseBeforeDecisionMs"`
	PauseAfterDecisionMs  int    `json:"pauseAfterDecisionMs"`
}

type PrepareArgs struct {
	Txn   string `json:"txn"`
	Delta int    `json:"delta"`
}

type PrepareReply struct {
	Vote   string `json:"vote"` // yes | no
	Reason string `json:"reason,omitempty"`
}

type DecideArgs struct {
	Txn      string `json:"txn"`
	Decision string `json:"decision"` // commit | abort
}

type DecideReply struct {
	OK bool `json:"ok"`
}

type StatusArgs struct {
	Txn string `json:"txn"`
}

type StatusReply struct {
	Found    bool   `json:"found"`
	Decision string `json:"decision"`
}

type State struct {
	Node string `json:"node"`
	Role string `json:"role"` // TM | RM
	// coordinator (TM) fields
	Txn      string            `json:"txn"`
	From     string            `json:"from"`
	To       string            `json:"to"`
	Amount   int               `json:"amount"`
	Phase    string            `json:"phase"`
	Decision string            `json:"decision"`
	Reason   string            `json:"reason"`
	Votes    map[string]string `json:"votes,omitempty"` // RM -> yes | no
	Acks     map[string]bool   `json:"acks,omitempty"`
	// participant (RM) fields
	Balance   int    `json:"balance"`
	Delta     int    `json:"delta"`
	Vote      string `json:"vote"`
	State     string `json:"state"`
	LastError string `json:"lastError"`
}

type coordinatorLog struct {
	Seq      int               `json:"seq"`
	Txn      string            `json:"txn"`
	From     string            `json:"from"`
	To       string            `json:"to"`
	Amount   int               `json:"amount"`
	Phase    string            `json:"phase"`
	Decision string            `json:"decision"`
	Reason   string            `json:"reason,omitempty"`
	Votes    map[string]string `json:"votes"`
	Acks     map[string]bool   `json:"acks"`
}

type Coordinator struct {
	mu        sync.Mutex
	id        string
	rms       []string
	peers     map[string]*rpc.Client
	save      func(any) error
	report    func(any) error
	log       coordinatorLog
	announced bool
	lastErr   string
	recovery  sync.Once
}

func newCoordinator(id string, members []string, peers map[string]*rpc.Client, d deps) (*Coordinator, error) {
	c := &Coordinator{id: id, peers: peers, save: d.save, report: d.report}
	for _, member := range members {
		if member != coordinatorNode {
			c.rms = append(c.rms, member)
		}
	}
	if err := loadPersisted(d.load, &c.log); err != nil {
		return nil, err
	}
	if c.log.Votes == nil {
		c.log.Votes = map[string]string{}
	}
	if c.log.Acks == nil {
		c.log.Acks = map[string]bool{}
	}
	c.announced = c.log.Phase == phaseFinished
	return c, nil
}

func (c *Coordinator) isRM(id string) bool {
	for _, member := range c.rms {
		if member == id {
			return true
		}
	}
	return false
}

func (c *Coordinator) persistLocked() error {
	if err := c.save(c.log); err != nil {
		c.lastErr = err.Error()
		return err
	}
	return nil
}

func (c *Coordinator) publish() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.report(c.stateLocked())
}

func (c *Coordinator) reportLocked() { _ = c.report(c.stateLocked()) }

func (c *Coordinator) stateLocked() State {
	s := State{Node: c.id, Role: "TM", LastError: c.lastErr}
	if c.log.Txn != "" {
		s.Txn, s.From, s.To, s.Amount = c.log.Txn, c.log.From, c.log.To, c.log.Amount
		s.Phase, s.Decision, s.Reason = c.log.Phase, c.log.Decision, c.log.Reason
		s.Votes = make(map[string]string, len(c.log.Votes))
		for node, vote := range c.log.Votes {
			s.Votes[node] = vote
		}
		s.Acks = make(map[string]bool, len(c.log.Acks))
		for node, ack := range c.log.Acks {
			s.Acks[node] = ack
		}
	}
	return s
}

func (c *Coordinator) transfer(args TransferArgs, reply *State) error {
	if args.Amount <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}
	if args.Amount > 1_000_000 {
		return fmt.Errorf("amount must be at most 1000000")
	}
	if args.PauseBeforeDecisionMs < 0 || args.PauseBeforeDecisionMs > 60_000 ||
		args.PauseAfterDecisionMs < 0 || args.PauseAfterDecisionMs > 60_000 {
		return fmt.Errorf("pause values must be in [0, 60000] ms")
	}

	c.mu.Lock()
	if c.log.Txn != "" && c.log.Phase != phaseFinished {
		txn := c.log.Txn
		c.mu.Unlock()
		return fmt.Errorf("transaction %s is still in progress", txn)
	}
	if len(c.rms) < 2 {
		c.mu.Unlock()
		return fmt.Errorf("a transfer needs two resource managers; use at least 3 nodes")
	}
	if !c.isRM(args.From) || !c.isRM(args.To) {
		c.mu.Unlock()
		return fmt.Errorf("from and to must be resource managers (not %s)", coordinatorNode)
	}
	if args.From == args.To {
		c.mu.Unlock()
		return fmt.Errorf("from and to must be different accounts")
	}

	c.log.Seq++
	c.log.Txn = fmt.Sprintf("T%d", c.log.Seq)
	c.log.From, c.log.To, c.log.Amount = args.From, args.To, args.Amount
	c.log.Phase, c.log.Decision, c.log.Reason = phasePreparing, decisionNone, ""
	c.log.Votes = map[string]string{}
	c.log.Acks = map[string]bool{}
	c.lastErr = ""
	c.announced = false
	if err := c.persistLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	txnID := c.log.Txn
	from, to, amount := c.log.From, c.log.To, c.log.Amount
	c.reportLocked()
	c.mu.Unlock()

	deltas := map[string]int{from: -amount, to: +amount}
	votes := c.collectVotes(txnID, deltas)

	c.mu.Lock()
	if c.log.Txn != txnID {
		c.mu.Unlock()
		return fmt.Errorf("transaction %s disappeared", txnID)
	}
	c.log.Votes = votes
	decision := decisionCommit
	reason := "all participants voted YES"
	for _, member := range c.rms {
		if votes[member] != voteYes {
			decision = decisionAbort
			reason = fmt.Sprintf("%s voted NO (or did not answer)", member)
			break
		}
	}
	c.log.Reason = reason
	c.reportLocked()
	c.mu.Unlock()

	if args.PauseBeforeDecisionMs > 0 {
		time.Sleep(time.Duration(args.PauseBeforeDecisionMs) * time.Millisecond)
	}

	// The decision is persisted before any RM is notified.
	c.mu.Lock()
	if c.log.Txn != txnID {
		c.mu.Unlock()
		return fmt.Errorf("transaction %s disappeared before the commit point", txnID)
	}
	c.log.Decision = decision
	c.log.Phase = phaseDecided
	if err := c.persistLocked(); err != nil {
		c.log.Decision = decisionNone
		c.log.Phase = phasePreparing
		c.mu.Unlock()
		return fmt.Errorf("decision was not written to the log, so nothing was committed: %w", err)
	}
	c.reportLocked()
	c.mu.Unlock()

	if args.PauseAfterDecisionMs > 0 {
		time.Sleep(time.Duration(args.PauseAfterDecisionMs) * time.Millisecond)
	}

	c.mu.Lock()
	c.announced = true
	c.mu.Unlock()
	c.deliver(txnID)

	c.mu.Lock()
	*reply = c.stateLocked()
	c.reportLocked()
	c.mu.Unlock()
	return nil
}

func (c *Coordinator) collectVotes(txnID string, deltas map[string]int) map[string]string {
	votes := make(map[string]string, len(c.rms))
	var mu sync.Mutex
	var workers sync.WaitGroup
	for _, member := range c.rms {
		member := member
		workers.Add(1)
		go func() {
			defer workers.Done()
			var reply PrepareReply
			err := call(c.peers[member], "RM.Prepare",
				PrepareArgs{Txn: txnID, Delta: deltas[member]}, &reply)
			vote := voteNo
			if err == nil && reply.Vote == voteYes {
				vote = voteYes
			}
			mu.Lock()
			votes[member] = vote
			mu.Unlock()
		}()
	}
	workers.Wait()
	return votes
}

// deliver sends the decision to the RMs and retries until all acknowledge.
func (c *Coordinator) deliver(txnID string) {
	for {
		c.mu.Lock()
		if c.log.Txn != txnID {
			c.mu.Unlock()
			return
		}
		decision := c.log.Decision
		pending := make([]string, 0, len(c.rms))
		for _, member := range c.rms {
			if !c.log.Acks[member] {
				pending = append(pending, member)
			}
		}
		c.mu.Unlock()

		if decision == decisionNone || len(pending) == 0 {
			if decision == decisionNone {
				return
			}
			c.finish(txnID)
			return
		}

		method := "RM.Commit"
		if decision == decisionAbort {
			method = "RM.Abort"
		}
		acked := map[string]bool{}
		var mu sync.Mutex
		var workers sync.WaitGroup
		for _, member := range pending {
			member := member
			workers.Add(1)
			go func() {
				defer workers.Done()
				var reply DecideReply
				if err := call(c.peers[member], method,
					DecideArgs{Txn: txnID, Decision: decision}, &reply); err == nil && reply.OK {
					mu.Lock()
					acked[member] = true
					mu.Unlock()
				}
			}()
		}
		workers.Wait()

		c.mu.Lock()
		for member := range acked {
			c.log.Acks[member] = true
		}
		all := true
		for _, member := range c.rms {
			if !c.log.Acks[member] {
				all = false
				break
			}
		}
		if all {
			c.finishLocked(txnID)
		}
		c.reportLocked()
		c.mu.Unlock()
		if all {
			return
		}
		time.Sleep(statusInterval)
	}
}

func (c *Coordinator) finish(txnID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishLocked(txnID)
	c.reportLocked()
}

func (c *Coordinator) finishLocked(txnID string) {
	if c.log.Txn != txnID {
		return
	}
	c.log.Phase = phaseFinished
	_ = c.persistLocked()
}

// startRecovery restores an unfinished transaction from the log on startup.
func (c *Coordinator) startRecovery(_ context.Context) {
	c.recovery.Do(func() {
		c.mu.Lock()
		if c.log.Txn == "" || c.log.Phase == phaseFinished {
			c.mu.Unlock()
			return
		}
		if c.log.Decision == decisionNone {
			c.log.Decision = decisionAbort
			c.log.Phase = phaseDecided
			c.log.Reason = "presumed abort: TM restarted before writing its decision to the log"
			if err := c.persistLocked(); err != nil {
				c.lastErr = err.Error()
			}
		}
		c.announced = true
		txnID := c.log.Txn
		c.reportLocked()
		c.mu.Unlock()
		go c.deliver(txnID)
	})
}

// Status reports the outcome of a transaction to an RM.
func (c *Coordinator) Status(args StatusArgs, reply *StatusReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if args.Txn != "" && c.log.Txn == args.Txn {
		reply.Found = true
		if c.announced {
			reply.Decision = c.log.Decision
		}
	}
	return nil
}

type participantLog struct {
	Balance int    `json:"balance"`
	Txn     *rmTxn `json:"txn,omitempty"`
}

type rmTxn struct {
	ID    string `json:"id"`
	Delta int    `json:"delta"`
	Vote  string `json:"vote"`
	State string `json:"state"`
}

type ResourceManager struct {
	mu        sync.Mutex
	id        string
	peers     map[string]*rpc.Client
	save      func(any) error
	report    func(any) error
	log       participantLog
	uncertain bool
	lastErr   string
	recovery  sync.Once
}

func newResourceManager(id string, peers map[string]*rpc.Client, d deps) (*ResourceManager, error) {
	rm := &ResourceManager{
		id: id, peers: peers, save: d.save, report: d.report,
		log: participantLog{Balance: 100},
	}
	if err := loadPersisted(d.load, &rm.log); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rm *ResourceManager) persistLocked() error {
	if err := rm.save(rm.log); err != nil {
		rm.lastErr = err.Error()
		return err
	}
	return nil
}

func (rm *ResourceManager) publish() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.report(rm.stateLocked())
}

func (rm *ResourceManager) reportLocked() { _ = rm.report(rm.stateLocked()) }

func (rm *ResourceManager) stateLocked() State {
	s := State{Node: rm.id, Role: "RM", Balance: rm.log.Balance, State: stateIdle, LastError: rm.lastErr}
	if t := rm.log.Txn; t != nil {
		s.Txn, s.Delta, s.Vote = t.ID, t.Delta, t.Vote
		s.State = t.State
		if t.State == statePrepared && rm.uncertain {
			s.State = stateUncertain
		}
	}
	return s
}

// Prepare records the RM's vote for a transaction.
func (rm *ResourceManager) Prepare(args PrepareArgs, reply *PrepareReply) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if args.Txn == "" {
		return fmt.Errorf("txn is required")
	}
	if existing := rm.log.Txn; existing != nil {
		if existing.ID == args.Txn {
			reply.Vote = existing.Vote
			if existing.Vote == voteNo {
				reply.Reason = "already voted no"
			}
			return nil
		}
		if existing.State == statePrepared {
			return fmt.Errorf("still locked by transaction %s", existing.ID)
		}
	}

	vote := voteYes
	reason := ""
	if args.Delta < 0 && rm.log.Balance+args.Delta < 0 {
		vote = voteNo
		reason = "insufficient funds"
	}
	state := statePrepared
	if vote == voteNo {
		state = stateAborted
	}
	rm.log.Txn = &rmTxn{ID: args.Txn, Delta: args.Delta, Vote: vote, State: state}

	if err := rm.persistLocked(); err != nil {
		return err
	}

	reply.Vote = vote
	reply.Reason = reason
	rm.reportLocked()
	if vote == voteYes {
		go rm.monitor(args.Txn)
	}
	return nil
}

func (rm *ResourceManager) Commit(args DecideArgs, reply *DecideReply) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if args.Decision != decisionCommit {
		return fmt.Errorf("Commit called with decision %q", args.Decision)
	}
	txn := rm.log.Txn
	if txn == nil || txn.ID != args.Txn {
		return fmt.Errorf("no prepared record for %s", args.Txn)
	}
	if txn.State == stateCommitted {
		reply.OK = true
		return nil
	}
	if txn.State == stateAborted {
		return fmt.Errorf("%s was already aborted", args.Txn)
	}
	rm.log.Balance += txn.Delta
	txn.State = stateCommitted
	rm.uncertain = false
	if err := rm.persistLocked(); err != nil {
		return err
	}
	rm.reportLocked()
	reply.OK = true
	return nil
}

func (rm *ResourceManager) Abort(args DecideArgs, reply *DecideReply) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if args.Decision != decisionAbort {
		return fmt.Errorf("Abort called with decision %q", args.Decision)
	}
	txn := rm.log.Txn
	if txn == nil || txn.ID != args.Txn {
		reply.OK = true // nothing to release
		return nil
	}
	if txn.State == stateCommitted {
		reply.OK = true // never undo a committed transfer
		return nil
	}
	txn.State = stateAborted
	rm.uncertain = false
	if err := rm.persistLocked(); err != nil {
		return err
	}
	rm.reportLocked()
	reply.OK = true
	return nil
}

// monitor polls the TM for the outcome of a prepared transaction.
func (rm *ResourceManager) monitor(id string) {
	for {
		rm.mu.Lock()
		txn := rm.log.Txn
		if txn == nil || txn.ID != id || txn.State != statePrepared {
			rm.mu.Unlock()
			return
		}
		rm.mu.Unlock()

		var reply StatusReply
		err := call(rm.peers[coordinatorNode], "TM.Status", StatusArgs{Txn: id}, &reply)

		rm.mu.Lock()
		txn = rm.log.Txn
		if txn == nil || txn.ID != id || txn.State != statePrepared {
			rm.mu.Unlock()
			return
		}
		switch {
		case err != nil:
			if !rm.uncertain {
				rm.uncertain = true
				rm.reportLocked()
			}
		case reply.Found && reply.Decision == decisionCommit:
			rm.log.Balance += txn.Delta
			txn.State = stateCommitted
			rm.uncertain = false
			_ = rm.persistLocked()
			rm.reportLocked()
			rm.mu.Unlock()
			return
		case reply.Found && reply.Decision == decisionAbort:
			txn.State = stateAborted
			rm.uncertain = false
			_ = rm.persistLocked()
			rm.reportLocked()
			rm.mu.Unlock()
			return
		default:
			if rm.uncertain {
				rm.uncertain = false
				rm.reportLocked()
			}
		}
		rm.mu.Unlock()

		select {
		case <-time.After(statusInterval):
		}
	}
}

// startRecovery resumes monitoring a prepared transaction after a restart.
func (rm *ResourceManager) startRecovery(_ context.Context) {
	rm.recovery.Do(func() {
		rm.mu.Lock()
		txn := rm.log.Txn
		rm.mu.Unlock()
		if txn != nil && txn.State == statePrepared {
			go rm.monitor(txn.ID)
		}
	})
}

type Application struct {
	coordinator *Coordinator
	participant *ResourceManager
}

func (a *Application) Transfer(args TransferArgs, reply *State) error {
	if a.coordinator == nil {
		return fmt.Errorf("only the TM %s can start a transfer", coordinatorNode)
	}
	return a.coordinator.transfer(args, reply)
}

func (a *Application) Read(_ Empty, reply *State) error {
	if a.coordinator != nil {
		a.coordinator.mu.Lock()
		defer a.coordinator.mu.Unlock()
		*reply = a.coordinator.stateLocked()
		return a.coordinator.report(*reply)
	}
	if a.participant != nil {
		a.participant.mu.Lock()
		defer a.participant.mu.Unlock()
		*reply = a.participant.stateLocked()
		return a.participant.report(*reply)
	}
	return fmt.Errorf("node has no role")
}

package main

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ---- internal RPC: TimeService.Ask ------------------------------------------

type AskArgs struct{}
type AskReply struct {
	Server string  `json:"server"`
	TimeMs float64 `json:"timeMs"` // Ts: the server's clock when it answered
}

// Ask returns the server's clock reading Ts. If serverDelayMs > 0 the request
// first waits in a simulated queue, and Ts is read only AFTER that wait.
// Cristian's client cannot tell this waiting time apart from network delay.
func (t *TimeService) Ask(_ AskArgs, reply *AskReply) error {
	n := t.n
	n.mu.Lock()
	delay := n.serverDelayMs
	n.mu.Unlock()
	if delay > 0 {
		time.Sleep(time.Duration(delay * float64(time.Millisecond)))
	}
	n.mu.Lock()
	reply.Server = n.rt.ID
	reply.TimeMs = n.clock.readAt(time.Now())
	n.served++
	n.mu.Unlock()
	n.publish()
	return nil
}

// ---- the Cristian calculation (pure function, unit-tested) ------------------

// Modes for the design variant: how much of the round trip is assumed to be
// the reply's one-way delay.
const (
	ModeHalf = "half" // Cristian: Ts + RTT/2
	ModeZero = "zero" // variant: take Ts as-is
	ModeFull = "full" // variant: Ts + RTT
)

// estimate computes Cristian's new clock value.
//
//	t0 = client clock when the request was sent
//	ts = server clock in the reply
//	t1 = client clock when the reply arrived
//
// Assuming an accurate server and reference-rate elapsed-time measurement,
// true time at t1 lies in [ts, ts+rtt]. errorBound is the farthest endpoint's
// distance; client drift or a non-reference server invalidates that plain bound.
func estimate(mode string, t0, ts, t1 float64) (est, oneWay, errorBound float64, err error) {
	rtt := t1 - t0
	switch mode {
	case ModeHalf:
		oneWay = rtt / 2
	case ModeZero:
		oneWay = 0
	case ModeFull:
		oneWay = rtt
	default:
		return 0, 0, 0, fmt.Errorf("mode must be %q, %q or %q, got %q", ModeHalf, ModeZero, ModeFull, mode)
	}
	return ts + oneWay, oneWay, math.Max(oneWay, rtt-oneWay), nil
}

// ---- application inputs ------------------------------------------------------

type SyncArgs struct {
	Server string `json:"server" distvis:"node"` // empty = node-1
	Mode   string `json:"mode"`                  // half (default) | zero | full
}

// SyncResult records every number of one sync, so the arithmetic can be
// checked from the node panel. Times are this client's clock in Unix ms.
type SyncResult struct {
	Server        string  `json:"server"`
	Mode          string  `json:"mode"`
	T0Ms          float64 `json:"t0Ms"`
	TsMs          float64 `json:"tsMs"`
	T1Ms          float64 `json:"t1Ms"`
	RttMs         float64 `json:"rttMs"`
	AssumedOneWay float64 `json:"assumedOneWayMs"` // what the mode adds to Ts
	EstimateMs    float64 `json:"estimateMs"`
	CorrectionMs  float64 `json:"correctionMs"` // estimate - t1, added to the clock
	ErrorBoundMs  float64 `json:"errorBoundMs"` // network bound under accurate-server/reference-rate assumptions
	ErrorBeforeMs float64 `json:"errorBeforeMs"`
	ErrorAfterMs  float64 `json:"errorAfterMs"`
}

// Sync runs one round of Cristian's algorithm against a server.
func (a *Application) Sync(args SyncArgs, reply *SyncResult) error {
	n := a.n
	server, mode := args.Server, args.Mode
	if server == "" {
		server = defaultServer
	}
	if mode == "" {
		mode = ModeHalf
	}
	if _, _, _, err := estimate(mode, 0, 0, 0); err != nil {
		return err
	}
	client := n.peers[server]
	if client == nil {
		if server == n.rt.ID {
			return fmt.Errorf("%s cannot synchronize with itself; run Sync on a client or choose another server", server)
		}
		return fmt.Errorf("unknown server %q", server)
	}

	n.mu.Lock()
	if n.busy {
		n.mu.Unlock()
		return errors.New("a sync is already in progress on this node")
	}
	n.busy = true
	t0 := n.clock.readAt(time.Now())
	n.mu.Unlock()

	var ask AskReply // the lock is NOT held while waiting for the network
	callErr := client.Call("TimeService.Ask", AskArgs{}, &ask)

	n.mu.Lock()
	n.busy = false
	if callErr != nil {
		n.failedSyncs++
		n.lastError = fmt.Sprintf("sync with %s failed, clock unchanged: %v", server, callErr)
		n.mu.Unlock()
		n.publish()
		return callErr
	}
	now := time.Now()
	t1 := n.clock.readAt(now)
	errBefore := n.clock.errorMs(now)
	est, oneWay, bound, _ := estimate(mode, t0, ask.TimeMs, t1)
	correction := est - t1
	n.clock.step(now, correction) // relative step at "now": no elapsed time is lost
	result := SyncResult{
		Server: server, Mode: mode,
		T0Ms: round1(t0), TsMs: round1(ask.TimeMs), T1Ms: round1(t1),
		RttMs: round1(t1 - t0), AssumedOneWay: round1(oneWay),
		EstimateMs: round1(est), CorrectionMs: round1(correction),
		ErrorBoundMs:  round1(bound),
		ErrorBeforeMs: round1(errBefore), ErrorAfterMs: round1(n.clock.errorMs(now)),
	}
	n.syncs++
	n.lastSync = &result
	n.lastError = ""
	n.mu.Unlock()

	*reply = result
	n.publish()
	return nil
}

type Empty struct{}

// Status refreshes and returns this node's state.
func (a *Application) Status(_ Empty, reply *StateView) error {
	*reply = a.n.view()
	a.n.publish()
	return nil
}

type SetClockArgs struct {
	OffsetMs float64 `json:"offsetMs"` // new distance from real time (not a relative change)
	DriftPPM float64 `json:"driftPPM"` // +1000 = gains 1 ms per real second
}

// SetClock resets this node's clock to real time + offsetMs with a new drift.
func (a *Application) SetClock(args SetClockArgs, reply *StateView) error {
	if err := validateClock(args.OffsetMs, args.DriftPPM); err != nil {
		return err
	}
	n := a.n
	n.mu.Lock()
	if n.busy {
		n.mu.Unlock()
		return errors.New("a sync is in progress; changing the clock now would corrupt its measurement")
	}
	n.clock = newClock(time.Now(), args.OffsetMs, args.DriftPPM)
	n.mu.Unlock()
	*reply = n.view()
	n.publish()
	return nil
}

type ServerDelayArgs struct {
	DelayMs float64 `json:"delayMs"` // 0 turns the queuing delay off
}

// SetServerDelay makes this node, acting as a server, hold every Ask request
// for delayMs before reading its clock (scenario C: server queuing delay).
func (a *Application) SetServerDelay(args ServerDelayArgs, reply *StateView) error {
	if !finite(args.DelayMs) || args.DelayMs < 0 || args.DelayMs > 8000 {
		return errors.New("delayMs must be between 0 and 8000 (the RPC timeout is 10 s)")
	}
	n := a.n
	n.mu.Lock()
	n.serverDelayMs = args.DelayMs
	n.mu.Unlock()
	*reply = n.view()
	n.publish()
	return nil
}

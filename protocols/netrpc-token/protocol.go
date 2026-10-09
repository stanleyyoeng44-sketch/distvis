package main

import (
	"context"
	"errors"
	"fmt"
	"net/rpc"
	"os"
	"sync"
	"time"

	"distvis/sdk"
	lab "distvis/sdk/rpc"
)

// These are plain net/rpc request/response types; there is no form schema.
type SubmitArgs struct{ Payload string }
type SubmitReply struct {
	Queued bool
	Node   string
}
type TokenArgs struct {
	Payload string
	Origin  string
}
type TokenReply struct{ Accepted bool }
type State struct {
	Role          string    `json:"role"`
	HasToken      bool      `json:"hasToken"`
	Entries       int       `json:"entries"`
	Pending       string    `json:"pending"`
	LastProcessed string    `json:"lastProcessed"`
	LastReceived  TokenArgs `json:"lastReceived"`
}
type Ring struct {
	initial bool
	mu      sync.Mutex
	state   State
	id      string
	next    *rpc.Client
	node    *lab.Runtime
}
type Application struct{ ring *Ring }

// The protocol uses a ring topology; Runtime resolves IDs and owns connections.
func newRing(node *lab.Runtime) (*Ring, error) {
	next, err := node.RPC(node.Neighbor(1))
	if err != nil {
		return nil, err
	}
	r := &Ring{id: node.ID, node: node, next: next}
	err = sdk.Load(&r.state)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	r.initial = node.ID == node.Nodes[0] && err != nil
	r.state.Role, r.state.HasToken = "waiting", false
	r.report()
	return r, nil
}

// Only the first node on a fresh run creates the token. Recovery must not mint one.
func (r *Ring) run(ctx context.Context) error {
	if !r.initial {
		return nil
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return r.Pass(TokenArgs{}, &TokenReply{})
	}
}

func (a *Application) Submit(args SubmitArgs, reply *SubmitReply) error {
	a.ring.mu.Lock()
	defer a.ring.mu.Unlock()
	a.ring.state.Pending = args.Payload
	*reply = SubmitReply{true, a.ring.id}
	a.ring.report()
	return nil
}
func (r *Ring) report() {
	if err := sdk.Save(r.state); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err := r.node.Report(r.state); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
func (r *Ring) Pass(args TokenArgs, reply *TokenReply) error {
	r.mu.Lock()
	if r.state.HasToken {
		r.mu.Unlock()
		return errors.New("already holding token")
	}
	r.state.HasToken = true
	r.state.Role = "critical"
	r.state.Entries++
	r.state.LastReceived = args
	r.report()
	r.mu.Unlock()
	reply.Accepted = true
	go func() {
		time.Sleep(700 * time.Millisecond)
		r.mu.Lock()
		payload := r.state.Pending
		if payload != "" {
			r.state.LastProcessed = payload
			r.state.Pending = ""
		}
		r.state.HasToken = false
		r.state.Role = "waiting"
		r.report()
		r.mu.Unlock()
		var response TokenReply
		// Ordinary net/rpc code; arguments and reply are automatically visualized.
		if err := r.next.Call("Ring.Pass", TokenArgs{payload, r.id}, &response); err != nil {
			fmt.Fprintln(os.Stderr, "token delivery unconfirmed:", err)
		}
	}()
	return nil
}

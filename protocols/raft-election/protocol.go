// This lesson covers elections and proposal routing, not log replication/commit.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/rpc"
	"os"
	"sync"
	"time"

	"distvis/sdk"
	lab "distvis/sdk/rpc"
)

type Proposal struct {
	ID     string `json:"id"`
	Origin string `json:"origin"`
	Number int32  `json:"number"`
	Status string `json:"status"`
}
type State struct {
	Role                 string     `json:"role"`
	Term                 int        `json:"term"`
	VotedFor             string     `json:"votedFor"`
	Leader               string     `json:"leader"`
	LastProposal         *Proposal  `json:"lastProposal,omitempty"`
	LastReceivedProposal *Proposal  `json:"lastReceivedProposal,omitempty"`
	PendingProposals     []Proposal `json:"pendingProposals,omitempty"`
}
type VoteArgs struct {
	Term      int
	Candidate string
}
type VoteReply struct {
	Term    int
	Granted bool
}
type HeartbeatArgs struct {
	Term   int
	Leader string
}
type HeartbeatReply struct{ Term int }
type ProposeArgs struct {
	Number int32 `json:"number"`
}
type ProposeReply struct {
	ID     string
	Status string
}
type ForwardArgs struct {
	Term     int
	Proposal Proposal
}
type ForwardReply struct {
	Term     int
	Accepted bool
}
type Application struct{ raft *Raft }
type Raft struct {
	mu       sync.Mutex
	node     *lab.Runtime
	peers    map[string]*rpc.Client
	state    State
	deadline time.Time
	votes    map[string]bool
}

func newRaft(node *lab.Runtime) (*Raft, error) {
	r := &Raft{node: node, votes: map[string]bool{}}
	if err := sdk.Load(&r.state); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	r.state.Role, r.state.Leader = "follower", ""
	r.reset()
	var err error
	r.peers, err = node.RPCPeers()
	return r, err
}
func (r *Raft) reset() {
	r.deadline = time.Now().Add(time.Duration(1200+rand.Intn(1200)) * time.Millisecond)
}
func (r *Raft) report() error {
	if err := sdk.Save(r.state); err != nil {
		return err
	}
	return r.node.Report(r.state)
}
func (r *Raft) higher(term int) {
	if term > r.state.Term {
		r.state.Term, r.state.Role, r.state.VotedFor, r.state.Leader = term, "follower", "", ""
		r.reset()
	}
}
func (r *Raft) RequestVote(args VoteArgs, reply *VoteReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.higher(args.Term)
	reply.Granted = args.Term == r.state.Term && (r.state.VotedFor == "" || r.state.VotedFor == args.Candidate)
	if reply.Granted {
		r.state.VotedFor = args.Candidate
		r.reset()
	}
	reply.Term = r.state.Term
	return r.report()
}
func (r *Raft) Heartbeat(args HeartbeatArgs, reply *HeartbeatReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.higher(args.Term)
	if args.Term == r.state.Term {
		r.state.Role, r.state.Leader = "follower", args.Leader
		r.reset()
	}
	reply.Term = r.state.Term
	return r.report()
}
func (a *Application) Propose(args ProposeArgs, reply *ProposeReply) error {
	r := a.raft
	r.mu.Lock()
	defer r.mu.Unlock()
	p := Proposal{ID: fmt.Sprintf("%s-%d", r.node.ID, time.Now().UnixNano()), Origin: r.node.ID, Number: args.Number, Status: "等待 Leader 接收"}
	r.state.LastProposal = &p
	r.state.PendingProposals = append(r.state.PendingProposals, p)
	*reply = ProposeReply{p.ID, p.Status}
	return r.report()
}
func (r *Raft) Forward(args ForwardArgs, reply *ForwardReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	reply.Term = r.state.Term
	reply.Accepted = r.state.Role == "leader" && args.Term == r.state.Term
	if reply.Accepted {
		p := args.Proposal
		p.Status = "Leader 已接收（未提交）"
		r.state.LastReceivedProposal = &p
	}
	return r.report()
}
func (r *Raft) ack(p Proposal) {
	next := r.state.PendingProposals[:0]
	for _, item := range r.state.PendingProposals {
		if item.ID != p.ID {
			next = append(next, item)
		}
	}
	r.state.PendingProposals = next
	if r.state.LastProposal != nil && r.state.LastProposal.ID == p.ID {
		r.state.LastProposal.Status = "Leader 已接收（未提交）"
	}
}
func (r *Raft) run(ctx context.Context) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			r.mu.Lock()
			if r.state.Role != "leader" && now.After(r.deadline) {
				r.state.Term++
				r.state.Role, r.state.VotedFor, r.state.Leader = "candidate", r.node.ID, ""
				r.votes = map[string]bool{r.node.ID: true}
				r.reset()
				term := r.state.Term
				for peer, client := range r.peers {
					go r.vote(ctx, peer, client, term)
				}
			}
			term, leader := r.state.Term, r.state.Leader
			if r.state.Role == "leader" {
				for _, client := range r.peers {
					go r.heartbeat(ctx, client, term)
				}
			}
			for _, p := range append([]Proposal(nil), r.state.PendingProposals...) {
				if r.state.Role == "leader" {
					p.Status = "Leader 已接收（未提交）"
					r.state.LastReceivedProposal = &p
					r.ack(p)
				} else if client := r.peers[leader]; client != nil {
					go r.forward(ctx, client, term, p)
				}
			}
			err := r.report()
			r.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}
func (r *Raft) vote(ctx context.Context, peer string, client *rpc.Client, term int) {
	var reply VoteReply
	if err := client.Call("Raft.RequestVote", VoteArgs{term, r.node.ID}, &reply); err != nil || ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.higher(reply.Term)
	if term == r.state.Term && r.state.Role == "candidate" && reply.Granted {
		r.votes[peer] = true
		if len(r.votes) > len(r.node.Nodes)/2 {
			r.state.Role, r.state.Leader = "leader", r.node.ID
		}
	}
	if err := r.report(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
func (r *Raft) heartbeat(ctx context.Context, client *rpc.Client, term int) {
	var reply HeartbeatReply
	if err := client.Call("Raft.Heartbeat", HeartbeatArgs{term, r.node.ID}, &reply); err != nil || ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if reply.Term > r.state.Term {
		r.higher(reply.Term)
		if err := r.report(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}
func (r *Raft) forward(ctx context.Context, client *rpc.Client, term int, p Proposal) {
	var reply ForwardReply
	if err := client.Call("Raft.Forward", ForwardArgs{term, p}, &reply); err != nil || ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.higher(reply.Term)
	if reply.Accepted && reply.Term == r.state.Term {
		r.ack(p)
	}
	if err := r.report(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

// Teaching examples: Raft election only, token-ring mutual exclusion, and LWW replication.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"distvis/sdk"
)

type Item struct {
	Value   string `json:"value"`
	Version int    `json:"version"`
	Writer  string `json:"writer"`
}
type Proposal struct {
	ID     string  `json:"id"`
	Origin string  `json:"origin"`
	Number float64 `json:"number"`
	Status string  `json:"status,omitempty"`
}
type TokenInput struct {
	ID      string `json:"id"`
	Payload string `json:"payload"`
	Origin  string `json:"origin,omitempty"`
}
type State struct {
	Role                 string          `json:"role"`
	Term                 int             `json:"term"`
	VotedFor             string          `json:"votedFor"`
	Leader               string          `json:"leader,omitempty"`
	HasToken             bool            `json:"hasToken"`
	Entries              int             `json:"entries"`
	Version              int             `json:"version"`
	Store                map[string]Item `json:"store,omitempty"`
	PendingInput         *TokenInput     `json:"pendingInput,omitempty"`
	LastProcessedInput   *TokenInput     `json:"lastProcessedInput,omitempty"`
	LastReceivedPayload  *TokenInput     `json:"lastReceivedPayload,omitempty"`
	LastProposal         *Proposal       `json:"lastProposal,omitempty"`
	LastReceivedProposal *Proposal       `json:"lastReceivedProposal,omitempty"`
	PendingProposals     []Proposal      `json:"pendingProposals,omitempty"`
	SeenProposals        map[string]bool `json:"seenProposals,omitempty"`
}
type Payload struct {
	Type     string          `json:"type"`
	Term     int             `json:"term,omitempty"`
	Granted  bool            `json:"granted,omitempty"`
	Store    map[string]Item `json:"store,omitempty"`
	Proposal *Proposal       `json:"proposal,omitempty"`
	Payload  *TokenInput     `json:"payload,omitempty"`
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func main() {
	n, err := sdk.Open()
	must(err)
	state := State{Role: "follower", Store: map[string]Item{}}
	loadErr := sdk.Load(&state)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		must(loadErr)
	}
	recovered := loadErr == nil
	// The protocol, not the coordinator or browser, owns these application inputs.
	switch n.Protocol {
	case "raft":
		must(n.DeclareInput([]sdk.InputAction{{Action: "propose", Label: "提交提案",
			Description: "任意节点可提交数字，等待 Leader 后转发。Leader 接收不代表多数派提交；此示例不含日志复制。",
			Fields:      []sdk.InputField{{Name: "number", Label: "Proposed number", Type: "number", Required: true, Integer: true}}}}))
	case "token":
		must(n.DeclareInput([]sdk.InputAction{{Action: "enqueue", Label: "提交 payload",
			Description: "等待获得令牌后处理。再次提交会替换尚未处理的 payload。",
			Fields:      []sdk.InputField{{Name: "payload", Label: "Payload", Type: "text", Required: true, MaxLength: 4096}}}}))
	case "gossip":
		must(n.DeclareInput([]sdk.InputAction{{Action: "write", Label: "写入副本",
			Fields: []sdk.InputField{{Name: "key", Label: "键", Type: "text", Required: true, MaxLength: 100},
				{Name: "value", Label: "值", Type: "text", Required: true, MaxLength: 4096}}}}))
	}
	report := func() { must(sdk.Save(state)); must(n.Report(state)) }
	send := func(to string, p Payload) { must(n.Send(to, p)) }
	broadcast := func(p Payload) {
		for _, peer := range n.Nodes {
			if peer != n.ID {
				send(peer, p)
			}
		}
	}
	inbox := make(chan sdk.Message)
	go func() {
		defer close(inbox)
		for {
			msg, err := n.Receive(context.Background())
			if err != nil {
				return
			}
			inbox <- msg
		}
	}()
	index := 0
	for i, peer := range n.Nodes {
		if peer == n.ID {
			index = i
		}
	}
	deadline := time.Now()
	reset := func() { deadline = time.Now().Add(time.Duration(1000+rand.Intn(1000)) * time.Millisecond) }
	votes := map[string]bool{}
	nextSync := time.Now()
	holdingUntil := time.Time{}
	var wirePayload *TokenInput
	processInput := func() {
		if state.PendingInput == nil {
			return
		}
		state.LastProcessedInput = state.PendingInput
		wirePayload = state.PendingInput
		state.PendingInput = nil
	}
	enter := func() {
		state.HasToken = true
		state.Role = "critical"
		state.Entries++
		processInput()
		holdingUntil = time.Now().Add(900 * time.Millisecond)
		report()
	}
	put := func(key, value string) {
		state.Version++
		state.Store[key] = Item{value, state.Version, n.ID}
		report()
		broadcast(Payload{Type: "Sync", Store: state.Store})
	}
	if state.SeenProposals == nil {
		state.SeenProposals = map[string]bool{}
	}
	ack := func(p Proposal) {
		pending := state.PendingProposals[:0]
		for _, item := range state.PendingProposals {
			if item.ID != p.ID {
				pending = append(pending, item)
			}
		}
		state.PendingProposals = pending
		if state.LastProposal != nil && state.LastProposal.ID == p.ID {
			state.LastProposal.Status = "Leader 已接收（未提交）"
		}
		report()
	}
	accept := func(p Proposal) {
		if !state.SeenProposals[p.ID] {
			state.SeenProposals[p.ID] = true
			p.Status = "Leader 已接收（未提交）"
			state.LastReceivedProposal = &p
			report()
		}
		if p.Origin == n.ID {
			ack(p)
		} else {
			send(p.Origin, Payload{Type: "ProposalAck", Term: state.Term, Proposal: &p})
		}
	}
	flush := func() {
		for _, p := range append([]Proposal(nil), state.PendingProposals...) {
			if state.Role == "leader" {
				accept(p)
			} else if state.Leader != "" {
				send(state.Leader, Payload{Type: "Proposal", Term: state.Term, Proposal: &p})
			}
		}
	}
	switch n.Protocol {
	case "raft":
		state.Role = "follower"
		state.Leader = ""
		reset()
		report()
	case "token":
		state.Role = "waiting"
		state.HasToken = false
		report()
		if index == 0 && !recovered {
			enter()
		}
	case "gossip":
		state.Role = "replica"
		report()
		if index == 0 && !recovered {
			put("course", "distributed-systems")
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown example protocol")
		return
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case cmd, ok := <-n.Commands:
			if !ok {
				return
			}
			var values struct {
				Key     string  `json:"key"`
				Value   string  `json:"value"`
				Number  float64 `json:"number"`
				Payload string  `json:"payload"`
			}
			if json.Unmarshal(cmd.Values, &values) != nil {
				continue
			}
			switch n.Protocol {
			case "gossip":
				if cmd.Action == "write" {
					put(values.Key, values.Value)
				}
			case "token":
				if cmd.Action == "enqueue" {
					state.PendingInput = &TokenInput{ID: cmd.ID, Origin: n.ID, Payload: values.Payload}
					if state.HasToken {
						processInput()
					}
					report()
				}
			case "raft":
				if cmd.Action == "propose" {
					p := Proposal{ID: cmd.ID, Origin: n.ID, Number: values.Number, Status: "等待 Leader 接收"}
					state.LastProposal = &p
					state.PendingProposals = append(state.PendingProposals, p)
					report()
					flush()
				}
			}
		case msg, ok := <-inbox:
			if !ok {
				return
			}
			var p Payload
			if json.Unmarshal(msg.Payload, &p) != nil {
				continue
			}
			switch n.Protocol {
			case "raft":
				if p.Term > state.Term {
					state.Term = p.Term
					state.VotedFor = ""
					state.Role = "follower"
					state.Leader = ""
					reset()
					report()
				}
				switch p.Type {
				case "RequestVote":
					granted := p.Term == state.Term && (state.VotedFor == "" || state.VotedFor == msg.From)
					if granted {
						state.VotedFor = msg.From
						reset()
						report()
					}
					send(msg.From, Payload{Type: "Vote", Term: state.Term, Granted: granted})
				case "Vote":
					if p.Term == state.Term && p.Granted && state.Role == "candidate" {
						votes[msg.From] = true
						if len(votes) > len(n.Nodes)/2 {
							state.Role = "leader"
							state.Leader = n.ID
							report()
							broadcast(Payload{Type: "Heartbeat", Term: state.Term})
						}
					}
				case "Heartbeat":
					if p.Term == state.Term {
						state.Role = "follower"
						state.Leader = msg.From
						reset()
						report()
					}
					send(msg.From, Payload{Type: "HeartbeatAck", Term: state.Term})
				case "Proposal":
					if state.Role == "leader" && p.Proposal != nil {
						accept(*p.Proposal)
					}
				case "ProposalAck":
					if p.Term == state.Term && p.Proposal != nil {
						ack(*p.Proposal)
					}
				}
			case "token":
				if p.Type == "Token" && !state.HasToken {
					state.LastReceivedPayload = p.Payload
					enter()
				}
			case "gossip":
				if p.Type == "Sync" {
					changed := false
					for key, item := range p.Store {
						if item.Version > state.Version {
							state.Version = item.Version
							changed = true
						}
						old, ok := state.Store[key]
						if !ok || item.Version > old.Version || (item.Version == old.Version && item.Writer > old.Writer) {
							state.Store[key] = item
							changed = true
						}
					}
					if changed {
						report()
					}
				}
			}
		case now := <-ticker.C:
			switch n.Protocol {
			case "raft":
				if state.Role == "leader" {
					broadcast(Payload{Type: "Heartbeat", Term: state.Term})
				} else if now.After(deadline) {
					state.Term++
					state.Role = "candidate"
					state.VotedFor = n.ID
					state.Leader = ""
					votes = map[string]bool{n.ID: true}
					reset()
					report()
					broadcast(Payload{Type: "RequestVote", Term: state.Term})
				}
				flush()
			case "token":
				if state.HasToken && now.After(holdingUntil) {
					state.HasToken = false
					state.Role = "waiting"
					report()
					send(n.Nodes[(index+1)%len(n.Nodes)], Payload{Type: "Token", Payload: wirePayload})
					wirePayload = nil
				}
			case "gossip":
				if now.After(nextSync) {
					broadcast(Payload{Type: "Sync", Store: state.Store})
					nextSync = now.Add(1600 * time.Millisecond)
				}
			}
		}
	}
}

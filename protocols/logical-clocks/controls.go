package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

type EventCompareArgs struct {
	Left  Timestamp `json:"left"`
	Right Timestamp `json:"right"`
}
type EventComparison struct {
	Vector  string `json:"vector"`
	Lamport string `json:"lamport"`
	Total   string `json:"total"`
	Agrees  bool   `json:"agrees"`
}

func order(a, b uint64) string {
	if a < b {
		return "a<b"
	}
	if a > b {
		return "a>b"
	}
	return "equal"
}

// CompareEvents checks all timestamp representations for the same pair. Raw
// Compare remains available for entering only two vector arrays.
func (a *Application) CompareEvents(args EventCompareArgs, reply *EventComparison) error {
	s := a.service
	if err := validateVector(args.Left.Vector, len(s.data.Members)); err != nil {
		return err
	}
	if err := validateVector(args.Right.Vector, len(s.data.Members)); err != nil {
		return err
	}
	rel, err := relation(args.Left.Vector, args.Right.Vector)
	if err != nil {
		return err
	}
	agrees := rel != "happens-before" && rel != "happens-after" || rel == "happens-before" && args.Left.Lamport < args.Right.Lamport || rel == "happens-after" && args.Left.Lamport > args.Right.Lamport
	*reply = EventComparison{Vector: rel, Lamport: order(args.Left.Lamport, args.Right.Lamport), Total: order(args.Left.Total, args.Right.Total), Agrees: agrees}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *reply
	s.lastCompare = &copy
	return s.report(s.stateLocked())
}

type AutoArgs struct {
	Count int `json:"count"`
}

func (a *Application) Auto(args AutoArgs, reply *State) error {
	count := args.Count
	if count == 0 {
		count = 8
	}
	if count < 1 || count > 20 {
		return fmt.Errorf("count must be 1..20")
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.autoRunning {
		return fmt.Errorf("automatic events already running")
	}
	s.autoRunning = true
	*reply = s.stateLocked()
	go func() {
		defer func() { s.mu.Lock(); s.autoRunning = false; _ = s.report(s.stateLocked()); s.mu.Unlock() }()
		for i := 0; i < count; i++ {
			label := fmt.Sprintf("auto-%d", i+1)
			if rand.IntN(2) == 0 {
				_ = a.Local(LocalArgs{Label: label}, &Event{})
			} else {
				members := s.data.Members
				peer := members[(s.index+1+rand.IntN(len(members)-1))%len(members)]
				_ = a.Send(SendArgs{To: peer, Label: label, ReceiveLabel: label + "-receive"}, &SendReply{})
			}
			time.Sleep(time.Duration(150+rand.IntN(300)) * time.Millisecond)
		}
	}()
	return nil
}

type VariantArgs struct {
	OmitReceiveMerge bool `json:"omitReceiveMerge"`
}

func (a *Application) Variant(args VariantArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.autoRunning || s.data.Clock.Lamport != 0 {
		return fmt.Errorf("choose the variant before generating events")
	}
	s.omitMerge = args.OmitReceiveMerge
	*reply = s.stateLocked()
	return s.report(*reply)
}

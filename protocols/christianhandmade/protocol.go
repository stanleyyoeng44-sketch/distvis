package main

import (
	"time"
)

type Empty struct{}
type StatusReply struct {
	Realtime time.Time
	Read     float64
	Drift    float64
}
type AskReply struct {
	Time float64
}
type AlignRequest struct {
	Whom string
}

func (s *TimeService) Ask(_Empty, reply *AskReply) error {
	s.clock.mu.Lock()
	reply.Time = s.clock.readAt(time.Now().Local())
	s.clock.mu.Unlock()
	return nil
}

func (a *Application) Status(_ Empty, reply *StatusReply) error {
	now := time.Now().Local()
	reply.Realtime = now
	a.clock.mu.Lock()
	reply.Read = a.clock.readAt(now)
	reply.Drift = a.clock.drift
	a.clock.mu.Unlock()
	return a.node.Report(map[string]any{"now": now, "read": reply.Read, "subtraction": now.Add(-time.Duration(reply.Read * float64(time.Second))), "origin": a.clock.origintime})
}

func (a *Application) Align(request *AlignRequest, _ *Empty) error {
	a.clock.mu.Lock()
	send := a.clock.readAt(time.Now().Local())
	a.clock.mu.Unlock()
	client := a.peers[request.Whom]
	reply := new(AskReply)
	if err := client.Call("TimeService.Ask", Empty{}, reply); err != nil {
		a.node.Report(err)
		return err
	}
	a.clock.mu.Lock()
	get := a.clock.readAt(time.Now().Local())
	a.clock.changeAt(time.Now().Local(), reply.Time-(send+get)/2)
	a.clock.mu.Unlock()
	return a.node.Report(*reply)
}

package main

import "fmt"

type LamportClockValue struct {
	Seq int64
	Vec [16]int64
}
type Empty struct{}
type ClockReply struct {
	Seq      int64
	Seqtotal int64
	Vec      [16]int64
}
type SendToRequest struct {
	Id string
}

func (s Receiver) Ping(args *LamportClockValue, reply *Empty) error {
	//s.clock.mu.Lock()
	//defer s.clock.mu.Unlock()
	s.clock.mergewith(args)
	s.clock.step()
	return nil
}

func (c *lamportClock) mergewith(cv *LamportClockValue) {
	c.seq = max(c.seq, cv.Seq)
	for i := range c.vec {
		c.vec[i] = max(c.vec[i], cv.Vec[i])
	}
}

func (c *lamportClock) step() {
	c.seq += 1
	c.vec[c.index] += 1
}

func (c *lamportClock) seqtotal() int64 {
	return (c.seq << 4) + int64(c.index)
}

func (a Application) Step(_ *Empty, _ *Empty) error {
	//a.clock.mu.Lock()
	//defer a.clock.mu.Unlock()
	a.clock.step()
	return nil
}

func (a Application) PeekState(_ *Empty, reply *ClockReply) error {
	//a.clock.mu.Lock()
	//defer a.clock.mu.Unlock()
	reply.Seq = a.clock.seq
	reply.Seqtotal = a.clock.seqtotal()
	reply.Vec = a.clock.vec
	return nil
}

func (a Application) SendTo(req SendToRequest, _ *Empty) error {
	id := req.Id
	client, ok := a.peers[id]
	if !ok || client == nil {
		return fmt.Errorf("Unknown peer %q", id)
	}
	for i := 0; i < 1000; i++ {
		//a.clock.mu.Lock()
		a.clock.step()
		cv := LamportClockValue{a.clock.seq, a.clock.vec}
		//a.clock.mu.Unlock()
		go client.Call("Receiver.Ping", &cv, &Empty{})
	}
	return nil
}

//running this code without mutex for4 clients together, 10ms exp jitter, would cause the server to corrupt its state.

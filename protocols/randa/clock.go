package main

import (
	"sync"
)

type lamportClock struct {
	mu  sync.Mutex
	seq int64
}

var lc lamportClock

func (c *lamportClock) mergewith(seq1 int64) {
	c.seq = max(c.seq, seq1)
}

func (c *lamportClock) step() {
	c.seq += 1
}

func (c *lamportClock) seqtotal() int64 {
	return (c.seq << 4) + int64(index)
}

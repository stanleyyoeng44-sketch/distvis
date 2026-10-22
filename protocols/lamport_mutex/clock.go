package main

// ─── The Lamport logical clock ───────────────────────────────────────────────
// A Lamport clock is just a counter. It is NOT a real clock: it measures the
// happened-before order of events, nothing else. Two rules are enough:
//
//  1. Before/after every local event (including every message SEND), tick: L+1.
//  2. On RECEIVE of a message carrying timestamp t: L = max(L, t) + 1.
//
// Rule 2 is what makes the clocks comparable across processes: after a receive,
// the receiver's clock is strictly greater than anything the sender had seen.
type lamportClock struct {
	now uint64
}

// tick advances the clock for one local event and returns the new value. The
// returned value is what a message sent by this event carries.
func (c *lamportClock) tick() uint64 {
	c.now++
	return c.now
}

// onReceive merges a remote timestamp into the local clock (rule 2). Even a
// message from the "past" advances the clock: L = max(L, t) + 1 > t always.
func (c *lamportClock) onReceive(remote uint64) uint64 {
	if remote > c.now {
		c.now = remote
	}
	return c.tick()
}

// requestKey identifies one request uniquely in the TOTAL order. Timestamps
// can collide (two processes may hold the same L), so the node id breaks the
// tie — the lecture's N*L+i idea, reduced to "id decides". All comparisons of
// requests in this protocol go through before().
type requestKey struct {
	ts   uint64
	node string
}

// before reports whether request a sorts before request b.
func before(a, b requestKey) bool {
	if a.ts != b.ts {
		return a.ts < b.ts
	}
	return a.node < b.node
}

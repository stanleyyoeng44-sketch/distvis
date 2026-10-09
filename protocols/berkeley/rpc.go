package main

import (
	"fmt"
	"net/rpc" // Standard Go RPC Client and Call; DistVis supplies its transport.
	"time"
)

// callRPC bounds a peer operation more tightly than DistVis's 10-second
// application deadline. Berkeley, for example, needs TWO phases to fit inside
// that deadline. This helper is copied into each standalone lesson so there is
// no hidden shared module to learn or import.
//
// [T any] is a type parameter (Go generics): callRPC[TimeReply](...) returns a
// TimeReply. "any" means T may be any type. The input args can be any exported
// RPC request struct; net/rpc serializes its exported fields with encoding/gob.
func callRPC[T any](client *rpc.Client, method string, args any) (T, error) {
	var reply T // The zero value: struct fields start at zero/false/empty, not garbage.
	// Go starts an asynchronous RPC. Unlike the synchronous client.Call method,
	// it lets us select between completion and a timer. Done MUST be buffered;
	// capacity 1 lets net/rpc finish even if our timer already made us return.
	pending := client.Go(method, args, &reply, make(chan *rpc.Call, 1))
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop() // Runs on BOTH return paths; release the timer's resources.
	select {
	case completed := <-pending.Done:
		// Receiving from Done synchronizes with the decoder's writes to reply.
		return reply, completed.Error
	case <-timer.C:
		// Crucial concurrency detail: do NOT read reply on timeout! The RPC
		// decoder may still be writing it. Return a separate zero value instead.
		var zero T
		return zero, fmt.Errorf("%s: no reply within 3s; remote outcome unknown", method)
	}
	// net/rpc has no per-call cancellation API. This does not abort the remote
	// operation; the SDK eventually expires it. Never Close the shared Client
	// to cancel one call: that would break other calls using the same client.
}

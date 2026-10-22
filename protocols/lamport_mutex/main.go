// distvis:name Lamport Mutual Exclusion
// distvis:description Ricart-Agrawala mutual exclusion with Lamport timestamps: you trigger each process's request and release; the req/ack/release exchange, deferred acks and queue ordering run the algorithm.
// distvis:start node-1 Application.RequestCS {} Ask for the critical section on node-1 and watch req/ack messages flow.

// package main tells Go to build an executable rather than an importable library.
// main.go is deliberately small: start here, then read clock.go, protocol.go
// and mutex.go in that order.
package main

import (
	lab "distvis/sdk/rpc"
)

func main() {
	// Main invokes configure with this node's Runtime. It also owns stdin/stdout,
	// RPC dispatch, signal handling and shared connection cleanup. Never print
	// ordinary text to stdout: DistVis uses it as a machine-readable channel.
	lab.Main(configure)
}

// configure builds this node's state and registers exactly two services:
//
//   - MutexService holds the three node-to-node RPCs the algorithm needs
//     (Request, Ack, Release). It creates no input forms.
//   - Application holds the actions YOU trigger from the platform UI:
//     RequestCS, Release and Read.
//
// Every node is equal: there is no coordinator. "Who goes first" is decided
// purely by the total order of request timestamps (LamportClock, nodeID).
func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	index := 0
	for i, id := range node.Nodes {
		if id == node.ID {
			index = i
		}
	}
	s := &MutexService{
		id:      node.ID,
		members: node.Nodes,
		peers:   peers, // Owned by the runtime: never Close these here.
		report:  node.Report,
		status:  statusIdle,
		// Every process starts its Lamport clock at its membership index, like
		// the lecture picture (p0: L=0, p1: L=1, p2: L=2). Any starting value
		// is legal; the algorithm only compares timestamps, never reads them.
		clock: lamportClock{now: uint64(index)},
		queue: []QueueEntry{},
	}
	if err := node.RegisterName("MutexService", s); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{service: s}, lab.Application()); err != nil {
		return err
	}
	s.mu.Lock()
	state := s.stateLocked()
	s.mu.Unlock()
	return node.Report(state)
}

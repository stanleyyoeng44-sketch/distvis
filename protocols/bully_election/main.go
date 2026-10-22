// distvis:name Bully Election
// distvis:description The classic bully election: you trigger StartElection on any node; ELECTION/OK/VICTORY messages run automatically and the highest-priority node alive takes over. Crash nodes to watch the next-highest bully its way up.
// distvis:start node-1 Application.StartElection {} Start an election on the LOWEST-priority node and watch the highest one win.

// package main tells Go to build an executable rather than an importable library.
// main.go is deliberately small: start here, then read protocol.go and bully.go.
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
//   - ElectionService holds the two node-to-node RPCs (Election, Victory).
//     It creates no input forms.
//   - Application holds the actions YOU trigger: StartElection and Read.
//
// Priority comes from the node id: node-3 outranks node-2. node-N is the
// strongest "bully" — when it is alive, it wins every election.
func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &ElectionService{
		id:       node.ID,
		priority: priority(node.ID),
		members:  node.Nodes,
		peers:    peers, // Owned by the runtime: never Close these here.
		report:   node.Report,
		// Nobody is the coordinator until the first election decides it.
		status: statusFollower,
	}
	if err := node.RegisterName("ElectionService", s); err != nil {
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

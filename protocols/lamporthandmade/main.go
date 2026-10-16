package main

import (
	lab "distvis/sdk/rpc"
	"net/rpc"
	"sync"
)

func main() {
	lab.Main(configure)
}

type Receiver struct {
	clock *lamportClock //though i call it LamportClock, I actually implemented both Lamport and vector
}

type Application struct {
	node   *lab.Runtime
	nodeID string
	clock  *lamportClock
	peers  map[string]*rpc.Client
}

type lamportClock struct {
	mu    sync.Mutex
	seq   int64
	vec   [16]int64
	index int
}

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	index := 0
	for i, id := range node.Nodes { // range yields both the index and its value.
		if id == node.ID {
			index = i
		}
	}
	clock := lamportClock{index: index}
	if err := node.RegisterName("Receiver", &Receiver{clock: &clock}); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{node: node, nodeID: node.ID, clock: &clock, peers: peers}, lab.Application()); err != nil {
		return err
	}
	return node.Report("hey dude, I'm ready")
}

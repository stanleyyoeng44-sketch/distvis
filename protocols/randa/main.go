package main

import (
	lab "distvis/sdk/rpc"
	"net/rpc"
)

var peers map[string]*rpc.Client
var index int

func main() {
	lab.Main(configure)
}

func configure(node *lab.Runtime) error {
	var err error
	peers, err = node.RPCPeers()
	if err != nil {
		return err
	}
	for i, id := range node.Nodes { // range yields both the index and its value.
		if id == node.ID {
			index = i
		}
	}
	lc = lamportClock{}
	l := lock{}
	if err := node.RegisterName("Service", &Service{l: &l}); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{l: &l}, lab.Application()); err != nil {
		return err
	}
	return node.Report("hey dude, I'm ready")
}

// distvis:name Quorum KV
// distvis:description Durable versioned replication with configurable read and write quorums, read repair and explicit partial-write outcomes.
// distvis:start node-1 Application.Put {"key":"x","value":"1"} Write x using the current write quorum.
package main

import (
	"distvis/sdk"
	lab "distvis/sdk/rpc"
	"net/rpc"
	"os"
	"sync"
)

func main() { lab.Main(configure) }

type Replica struct {
	mu               sync.Mutex
	operation        sync.Mutex
	id               string
	members          []string
	peers            map[string]*rpc.Client
	disk             Disk
	save             func(any) error
	report           func(any) error
	w, r             int
	messages         uint64
	lastPut, lastGet *Result
	lastError        string
	storageError     string
}
type Application struct{ s *Replica }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	s := &Replica{id: node.ID, members: node.Nodes, peers: peers, save: sdk.Save, report: node.Report, w: len(node.Nodes)/2 + 1, r: len(node.Nodes)/2 + 1, disk: Disk{Values: map[string]Versioned{}}}
	if err = sdk.Load(&s.disk); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = node.RegisterName("Replica", s); err != nil {
		return err
	}
	if err = node.RegisterName("Application", &Application{s}, lab.Application()); err != nil {
		return err
	}
	return s.publishLocked()
}

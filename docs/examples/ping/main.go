// distvis:name RPC Ping
// distvis:description 从任意节点向另一个节点发起 RPC，观察请求、返回和节点状态。
package main

import (
	"fmt"
	"net/rpc"
	"sync"

	lab "distvis/sdk/rpc"
)

type PingArgs struct {
	Peer string `json:"peer" distvis:"node"`
	Text string `json:"text"`
}
type EchoArgs struct {
	Text string `json:"text"`
}
type EchoReply struct {
	Node string `json:"node"`
	Text string `json:"text"`
}
type EchoService struct {
	mu       sync.Mutex
	node     *lab.Runtime
	received int
}
type Application struct {
	node  *lab.Runtime
	peers map[string]*rpc.Client
}

func (s *EchoService) Echo(args EchoArgs, reply *EchoReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received++
	*reply = EchoReply{Node: s.node.ID, Text: args.Text}
	return s.node.Report(map[string]any{
		"role": "ready", "received": s.received, "lastText": args.Text,
	})
}

func (a *Application) Ping(args PingArgs, reply *EchoReply) error {
	peer := args.Peer
	if peer == "" {
		peer = a.node.Neighbor(1)
	}
	client := a.peers[peer]
	if client == nil {
		return fmt.Errorf("choose another node: %q", peer)
	}
	return client.Call("Echo.Echo", EchoArgs{Text: args.Text}, reply)
}

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	if err := node.RegisterName("Echo", &EchoService{node: node}); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{node: node, peers: peers}, lab.Application()); err != nil {
		return err
	}
	return node.Report(map[string]any{"role": "ready", "received": 0})
}

func main() { lab.Main(configure) }

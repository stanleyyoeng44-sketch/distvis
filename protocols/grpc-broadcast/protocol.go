package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	lab "distvis/sdk/rpc"
	pb "lesson/broadcast/api"
)

type Service struct {
	pb.UnimplementedApplicationServer
	pb.UnimplementedReplicationServer
	mu      sync.Mutex
	node    *lab.Runtime
	clients map[string]pb.ReplicationClient
}

func newService(node *lab.Runtime) (*Service, error) {
	clients, err := lab.GRPCPeers(node, pb.NewReplicationClient)
	return &Service{node: node, clients: clients}, err
}

// Ordinary generated gRPC server methods. No DeclareInput, Send or Receive.
func (s *Service) Apply(ctx context.Context, req *pb.NumberRequest) (*pb.NumberReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.node.Report(map[string]any{"role": "replica", "number": fmt.Sprint(req.Number), "note": req.Note})
	if err != nil {
		return nil, err
	}
	return &pb.NumberReply{Accepted: true, Number: req.Number, Node: s.node.ID}, nil
}
func (s *Service) Propose(ctx context.Context, req *pb.NumberRequest) (*pb.NumberReply, error) {
	reply, err := s.Apply(ctx, req)
	if err != nil {
		return nil, err
	}
	var group sync.WaitGroup
	for peer, client := range s.clients {
		group.Add(1)
		go func(peer string, client pb.ReplicationClient) {
			defer group.Done()
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if _, err := client.Apply(callCtx, req); err != nil {
				fmt.Fprintln(os.Stderr, peer, err)
			}
		}(peer, client)
	}
	group.Wait()
	return reply, nil
}

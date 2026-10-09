package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/rpc"
	"testing"
	"time"

	"distvis/sdk"
	pb "distvis/sdk/rpc/testpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

func awaitRuntime(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not finish")
		return nil
	}
}

func TestRuntimeStartupRPCAndShutdown(t *testing.T) {
	c := cluster(t)
	done := make(chan error, 2)
	runtimes := make(chan *Runtime, 2)
	results := make(chan EchoReply, 2)
	for _, h := range c.hosts {
		go func(node *sdk.Node) {
			done <- serveNode(c.ctx, node, func(r *Runtime) error {
				if err := r.RegisterName("Echo", &EchoService{}, Application()); err != nil {
					return err
				}
				peer := "node-1"
				if r.ID == peer {
					peer = "node-2"
				}
				client, err := r.RPC(peer)
				if err != nil {
					return err
				}
				peers, err := r.RPCPeers()
				if err != nil || len(peers) != 1 || peers[peer] != client || peers[r.ID] != nil {
					return errors.New("membership clients must exclude self and reuse connections")
				}
				delete(peers, peer)
				peers, err = r.RPCPeers()
				if err != nil || peers[peer] != client {
					return errors.New("caller map mutation changed runtime connections")
				}
				if r.Neighbor(1) != peer || r.Neighbor(-1) != peer || r.Neighbor(2) != r.ID {
					return errors.New("ring membership order is incorrect")
				}
				again, err := r.RPC(peer)
				if err != nil || client != again {
					return errors.New("RPC connection was not reused")
				}
				if _, err := r.RPC("unknown"); err == nil {
					return errors.New("unknown peer accepted")
				}
				r.OnStart(func(ctx context.Context) error {
					var reply EchoReply
					if err := client.Call("Echo.Echo", EchoArgs{Number: 42, Text: r.ID}, &reply); err != nil {
						return err
					}
					results <- reply
					return nil
				})
				runtimes <- r
				return nil
			})
		}(h.Node)
	}
	for range c.hosts {
		select {
		case result := <-results:
			if result.Args.Number != 42 {
				t.Fatalf("unexpected reply: %+v", result)
			}
		case err := <-done:
			t.Fatalf("runtime exited before startup RPC: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("startup RPC deadlocked")
		}
	}
	record := c.wait(func(r record) bool { return string(r.data["type"]) == `"input_schema"` })
	var schema []sdk.InputAction
	if err := json.Unmarshal(record.data["schema"], &schema); err != nil || len(schema) != 1 {
		t.Fatalf("automatic schema: %s (%v)", record.data["schema"], err)
	}
	c.cancel()
	for range c.hosts {
		if err := awaitRuntime(t, done); err != nil {
			t.Fatal(err)
		}
	}
	for range c.hosts {
		r := <-runtimes
		if _, err := r.RPC("node-1"); err == nil {
			t.Fatal("closed runtime accepted connection")
		}
		for _, client := range r.rpcPeers {
			if err := client.Call("Echo.Echo", EchoArgs{}, &EchoReply{}); err == nil {
				t.Fatal("cached client survived shutdown")
			}
		}
	}
}

func TestRuntimeGRPCPeersAndSetupCleanup(t *testing.T) {
	c := cluster(t)
	done := make(chan error, 1)
	var conn *grpc.ClientConn
	var client *rpc.Client
	reject := errors.New("invalid protocol configuration")
	err := serveNode(c.ctx, c.hosts[0].Node, func(r *Runtime) (err error) {
		conn, err = r.GRPCConn("node-2")
		if err != nil {
			return err
		}
		again, err := r.GRPCConn("node-2")
		if err != nil || again != conn {
			t.Fatal("gRPC connection was not reused")
		}
		client, err = r.RPC("node-2")
		if err != nil {
			return err
		}
		return reject
	})
	if !errors.Is(err, reject) || conn.GetState() != connectivity.Shutdown {
		t.Fatalf("setup failure did not close connection: %v", err)
	}
	if err := client.Call("Echo.Echo", EchoArgs{}, &EchoReply{}); err == nil {
		t.Fatal("setup failure leaked net/rpc client")
	}

	// The other node uses the public registration helpers and calls itself from
	// OnStart, proving that the dispatcher runs before startup work.
	reply := make(chan *pb.Reply, 1)
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	go func() {
		done <- serveNode(ctx, c.hosts[1].Node, func(r *Runtime) error {
			peers, err := GRPCPeers(r, pb.NewInternalClient)
			if err != nil {
				return err
			}
			if len(peers) != 1 || peers["node-1"] == nil {
				return errors.New("peer clients must exclude self")
			}
			svc := &grpcService{node: r.ID}
			if err := r.GRPC(func(s grpc.ServiceRegistrar) {
				pb.RegisterApplicationServer(s, svc)
				pb.RegisterInternalServer(s, svc)
			}, "rpctest.Application"); err != nil {
				return err
			}
			conn, err = r.GRPCConn(r.ID)
			if err != nil {
				return err
			}
			r.OnStart(func(ctx context.Context) error {
				out, err := pb.NewInternalClient(conn).Echo(ctx, &pb.Request{Text: "started"})
				reply <- out
				return err
			})
			return nil
		})
	}()
	select {
	case out := <-reply:
		if out == nil || out.Node != "node-2" || out.Received.Text != "started" {
			t.Fatalf("startup gRPC: %v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup gRPC deadlocked")
	}
	cancel()
	if err := awaitRuntime(t, done); err != nil {
		t.Fatal(err)
	}
	if conn.GetState() != connectivity.Shutdown {
		t.Fatal("gRPC connection survived shutdown")
	}
}

func TestRuntimeStartupFailure(t *testing.T) {
	for _, failure := range []error{errors.New("startup failed"), context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			c := cluster(t)
			done := make(chan error, 1)
			go func() {
				done <- serveNode(c.ctx, c.hosts[0].Node, func(r *Runtime) error {
					r.OnStart(func(context.Context) error { return failure })
					return nil
				})
			}()
			if err := awaitRuntime(t, done); !errors.Is(err, failure) {
				t.Fatalf("startup error lost: %v", err)
			}
		})
	}
}

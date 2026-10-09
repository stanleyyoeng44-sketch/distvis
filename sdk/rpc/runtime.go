package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/rpc"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"distvis/sdk"
	"google.golang.org/grpc"
)

// Runtime owns node initialization, service lifetime and shared peer connections.
// Setup registers services and builds protocol state; OnStart runs after dispatch
// is ready, so a protocol may safely initiate RPCs from its startup hook.
type Runtime struct {
	*sdk.Node
	host      *Host
	mu        sync.Mutex
	closed    bool
	rpcPeers  map[string]*rpc.Client
	grpcPeers map[string]*grpc.ClientConn
	start     []func(context.Context) error
}

// Main is the process entry point. Only protocol-specific setup belongs in setup.
// Errors go to stderr (stdout is reserved for the coordinator).
func Main(setup func(*Runtime) error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Serve(ctx, setup); err != nil {
		fmt.Fprintln(os.Stderr, "protocol:", err)
		os.Exit(1)
	}
}

// Serve is the error-returning variant, useful for embedding and testing.
func Serve(ctx context.Context, setup func(*Runtime) error) error {
	node, err := sdk.Open()
	if err != nil {
		return err
	}
	return serveNode(ctx, node, setup)
}

func serveNode(ctx context.Context, node *sdk.Node, setup func(*Runtime) error) error {
	r := &Runtime{Node: node, host: New(node), rpcPeers: map[string]*rpc.Client{}, grpcPeers: map[string]*grpc.ClientConn{}}
	defer r.close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := setup(r); err != nil {
		return err
	}
	ready, done := make(chan struct{}), make(chan error, 1)
	go func() { done <- r.host.run(ctx, ready) }()
	select {
	case <-ready:
	case err := <-done:
		return normalExit(ctx, err)
	}
	started := make(chan error, 1)
	go func() {
		for _, fn := range r.start {
			if err := fn(ctx); err != nil {
				started <- err
				return
			}
		}
		started <- nil
	}()
	select {
	case err := <-started:
		if err != nil {
			// A hook failure is not a normal host EOF. In particular, our own
			// cancellation below must not hide a hook's context.Canceled error.
			result := err
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				result = nil
			}
			cancel()
			<-done
			return result
		}
	case err := <-done:
		return normalExit(ctx, err)
	}
	return normalExit(ctx, <-done)
}
func normalExit(ctx context.Context, err error) error {
	if errors.Is(err, io.EOF) || (ctx.Err() != nil && errors.Is(err, context.Canceled)) {
		return nil
	}
	return err
}

// OnStart registers protocol startup work. Long-running hooks must honor ctx.
// Register hooks and services only during setup.
func (r *Runtime) OnStart(fn func(context.Context) error) { r.start = append(r.start, fn) }
func (r *Runtime) RegisterName(name string, receiver any, options ...ServiceOption) error {
	return r.host.RegisterName(name, receiver, options...)
}
func (r *Runtime) Register(receiver any, options ...ServiceOption) error {
	return r.host.Register(receiver, options...)
}
func (r *Runtime) GRPC(register func(grpc.ServiceRegistrar), applications ...string) error {
	server := grpc.NewServer()
	register(server)
	if err := r.host.ServeGRPC(server, applications...); err != nil {
		server.Stop()
		return err
	}
	return nil
}
func (r *Runtime) validPeer(peer string) error {
	if r.closed {
		return errors.New("runtime is closed")
	}
	for _, id := range r.Nodes {
		if id == peer {
			return nil
		}
	}
	return fmt.Errorf("unknown peer %q", peer)
}

// RPC returns a shared standard client, owned and closed by the runtime.
// Callers must not Close it; the same client is safe for concurrent RPCs.
func (r *Runtime) RPC(peer string) (*rpc.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validPeer(peer); err != nil {
		return nil, err
	}
	if c := r.rpcPeers[peer]; c != nil {
		return c, nil
	}
	c := r.host.Dial(peer)
	r.rpcPeers[peer] = c
	return c, nil
}

// RPCPeers returns standard clients for all other nodes in the experiment.
// Membership comes from the coordinator; clients are reused and closed by Runtime.
// The returned map belongs to the caller, but its clients must not be closed.
func (r *Runtime) RPCPeers() (map[string]*rpc.Client, error) {
	clients := make(map[string]*rpc.Client)
	for _, peer := range r.Nodes {
		if peer == r.ID {
			continue
		}
		client, err := r.RPC(peer)
		if err != nil {
			return nil, err
		}
		clients[peer] = client
	}
	return clients, nil
}

// Neighbor returns a node ID relative to this node in coordinator membership order.
// Offset 1 is the successor in a ring, -1 the predecessor, and 0 this node.
// This only describes topology; it does not skip failed nodes or recover lost tokens.
func (r *Runtime) Neighbor(offset int) string {
	for i, id := range r.Nodes {
		if id == r.ID {
			n := len(r.Nodes)
			return r.Nodes[(i+offset%n+n)%n]
		}
	}
	return "" // A malformed membership will be rejected by RPC.
}

// GRPCConn returns a shared connection for ordinary generated clients.
func (r *Runtime) GRPCConn(peer string) (*grpc.ClientConn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validPeer(peer); err != nil {
		return nil, err
	}
	if c := r.grpcPeers[peer]; c != nil {
		return c, nil
	}
	c, err := r.host.DialGRPC(peer)
	if err == nil {
		r.grpcPeers[peer] = c
	}
	return c, err
}

// GRPCPeers builds generated clients for every other node, with no dialing loop
// or connection cleanup required in protocol code.
func GRPCPeers[T any](r *Runtime, newClient func(grpc.ClientConnInterface) T) (map[string]T, error) {
	clients := map[string]T{}
	for _, peer := range r.Nodes {
		if peer == r.ID {
			continue
		}
		conn, err := r.GRPCConn(peer)
		if err != nil {
			return nil, err
		}
		clients[peer] = newClient(conn)
	}
	return clients, nil
}
func (r *Runtime) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for _, c := range r.rpcPeers {
		_ = c.Close()
	}
	for _, c := range r.grpcPeers {
		_ = c.Close()
	}
	r.host.Close()
}

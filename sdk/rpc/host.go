// Package rpc adapts ordinary Go RPC services to DistVis' observable transport.
// Service methods remain ordinary net/rpc or gRPC handlers. Only registration
// and dialing use this package. A Host owns its Node's Receive and Commands.
package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"distvis/sdk"
)

type packet struct {
	Type     string              `json:"type"`
	RPCID    string              `json:"rpcId"`
	Protocol string              `json:"protocol"`
	Method   string              `json:"method"`
	Body     json.RawMessage     `json:"body,omitempty"`
	Wire     []byte              `json:"wire,omitempty"`
	Error    string              `json:"error,omitempty"`
	Code     int32               `json:"code,omitempty"`
	Status   []byte              `json:"status,omitempty"`
	Deadline int64               `json:"deadline,omitempty"`
	Metadata map[string][]string `json:"metadata,omitempty"`
	Headers  map[string][]string `json:"headers,omitempty"`
	Trailers map[string][]string `json:"trailers,omitempty"`
}
type handler func(context.Context, packet) packet
type inputHandler struct {
	schema sdk.InputAction
	call   func(context.Context, json.RawMessage) (json.RawMessage, error)
}
type pendingCall struct {
	peer   string
	result chan packet
}

type Host struct {
	Node *sdk.Node
	// Timeout bounds calls without a shorter caller deadline, including net/rpc.
	Timeout  time.Duration
	prefix   string
	seq      atomic.Uint64
	mu       sync.Mutex
	pending  map[string]pendingCall
	active   map[string]context.CancelFunc
	handlers map[string]handler
	inputs   map[string]inputHandler
	schemas  []sdk.InputAction
	closers  []func()
	running  bool
	ctx      context.Context
	stop     context.CancelFunc
	slots    chan struct{}
	net      *netServices
}

func New(node *sdk.Node) *Host {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Host{Node: node, Timeout: 10 * time.Second, prefix: hex.EncodeToString(id[:]),
		pending: map[string]pendingCall{}, active: map[string]context.CancelFunc{},
		handlers: map[string]handler{}, inputs: map[string]inputHandler{},
		ctx: ctx, stop: cancel, slots: make(chan struct{}, 128)}
}

// Application marks an entire net/rpc service as a browser application entry.
// Internal services need no option; their methods are still logged.
type ServiceOption func(*serviceOptions)
type serviceOptions struct{ application bool }

func Application() ServiceOption { return func(o *serviceOptions) { o.application = true } }

func (h *Host) addInput(protocol, method string, fields []sdk.InputField, call func(context.Context, json.RawMessage) (json.RawMessage, error)) {
	key := fmt.Sprintf("rpc_%x", []byte(protocol+":"+method))
	// Keep action IDs within the platform's identifier size even for long names.
	if len(key) > 64 {
		key = actionID(protocol + ":" + method)
	}
	schema := sdk.InputAction{Action: key, Label: method, Description: protocol + " · 从 RPC 请求类型自动生成", Fields: fields}
	h.inputs[key] = inputHandler{schema, call}
	h.schemas = append(h.schemas, schema)
}
func (h *Host) call(ctx context.Context, peer string, request packet) (packet, error) {
	if peer == "" {
		return packet{}, errors.New("empty RPC peer")
	}
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return packet{}, err
	}
	request.Type = "RPCRequest"
	request.RPCID = fmt.Sprintf("%s-%d", h.prefix, h.seq.Add(1))
	if d, ok := ctx.Deadline(); ok {
		request.Deadline = d.UnixMilli()
	}
	p := pendingCall{peer: peer, result: make(chan packet, 1)}
	h.mu.Lock()
	h.pending[request.RPCID] = p
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pending, request.RPCID); h.mu.Unlock() }()
	if err := h.Node.Send(peer, request); err != nil {
		return packet{}, err
	}
	select {
	case response := <-p.result:
		return response, nil
	case <-h.ctx.Done():
		return packet{}, io.EOF
	case <-ctx.Done():
		_ = h.Node.Send(peer, packet{Type: "RPCCancel", RPCID: request.RPCID, Protocol: request.Protocol, Method: request.Method})
		return packet{}, ctx.Err()
	}
}

// Run publishes inferred application schemas and dispatches RPCs until canceled
// or the coordinator closes the node. Register all services before calling Run.
func (h *Host) Run(ctx context.Context) error {
	return h.run(ctx, nil)
}
func (h *Host) run(ctx context.Context, ready chan<- struct{}) error {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return errors.New("RPC host already started")
	}
	h.running = true
	h.mu.Unlock()
	if h.schemas == nil {
		h.schemas = []sdk.InputAction{}
	}
	if err := h.Node.DeclareInput(h.schemas); err != nil {
		return err
	}
	defer h.Close()
	errs := make(chan error, 1)
	go func() {
		for {
			msg, err := h.Node.Receive(h.ctx)
			if err != nil {
				errs <- err
				return
			}
			var p packet
			if json.Unmarshal(msg.Payload, &p) != nil {
				continue
			}
			switch p.Type {
			case "RPCResponse":
				h.mu.Lock()
				pending, ok := h.pending[p.RPCID]
				h.mu.Unlock()
				if ok && pending.peer == msg.From {
					select {
					case pending.result <- p:
					default:
					}
				}
			case "RPCCancel":
				h.mu.Lock()
				cancel := h.active[msg.From+":"+p.RPCID]
				h.mu.Unlock()
				if cancel != nil {
					cancel()
				}
			case "RPCRequest":
				h.dispatch(msg.From, p)
			}
		}
	}()
	if ready != nil {
		close(ready)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.ctx.Done():
			return io.EOF
		case err := <-errs:
			return err
		case cmd, ok := <-h.Node.Commands:
			if !ok {
				return io.EOF
			}
			select {
			case h.slots <- struct{}{}:
				go h.application(cmd)
			default:
				_ = h.Node.CommandResult(cmd.ID, nil, errors.New("RPC concurrency limit reached"))
			}
		}
	}
}
func (h *Host) application(cmd sdk.Command) {
	ctx, cancel := context.WithTimeout(h.ctx, h.Timeout)
	defer cancel()
	type outcome struct {
		result json.RawMessage
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		out := outcome{}
		defer func() {
			if r := recover(); r != nil {
				out.err = fmt.Errorf("RPC handler panic: %v", r)
			}
			done <- out
			// A net/rpc method has no context and cannot be forcibly canceled.
			// Keep its slot occupied until it really returns.
			<-h.slots
		}()
		if input, ok := h.inputs[cmd.Action]; ok {
			out.result, out.err = input.call(ctx, cmd.Values)
		} else {
			out.err = errors.New("unknown RPC application method")
		}
	}()
	select {
	case out := <-done:
		_ = h.Node.CommandResult(cmd.ID, out.result, out.err)
	case <-ctx.Done():
		_ = h.Node.CommandResult(cmd.ID, nil, ctx.Err())
	}
}
func (h *Host) dispatch(from string, p packet) {
	reply := packet{Type: "RPCResponse", RPCID: p.RPCID, Protocol: p.Protocol, Method: p.Method}
	select {
	case h.slots <- struct{}{}:
	default:
		reply.Error = "RPC concurrency limit reached"
		reply.Code = 8
		_ = h.Node.Send(from, reply)
		return
	}
	ctx, cancel := context.WithCancel(h.ctx)
	if p.Deadline != 0 {
		cancel()
		ctx, cancel = context.WithDeadline(h.ctx, time.UnixMilli(p.Deadline))
	}
	key := from + ":" + p.RPCID
	h.mu.Lock()
	h.active[key] = cancel
	h.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				reply.Error = fmt.Sprintf("RPC handler panic: %v", r)
				reply.Code = 13
			}
			_ = h.Node.Send(from, reply)
			h.mu.Lock()
			delete(h.active, key)
			h.mu.Unlock()
			cancel()
			<-h.slots
		}()
		if err := ctx.Err(); err != nil {
			reply.Error = err.Error()
			reply.Code = 4
			return
		}
		fn := h.handlers[p.Protocol+":"+p.Method]
		if fn == nil {
			reply.Error = "unknown RPC method"
			reply.Code = 12
			return
		}
		response := fn(ctx, p)
		response.Type = reply.Type
		response.RPCID = reply.RPCID
		response.Protocol = reply.Protocol
		response.Method = reply.Method
		reply = response
	}()
}
func (h *Host) Close() {
	h.stop()
	h.mu.Lock()
	closers := h.closers
	h.closers = nil
	h.mu.Unlock()
	for _, close := range closers {
		close()
	}
}

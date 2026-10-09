package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/rpc"
	"strings"
	"sync"
	"testing"
	"time"

	"distvis/sdk"
	pb "distvis/sdk/rpc/testpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type record struct {
	node string
	data map[string]json.RawMessage
}
type eventWriter struct {
	node   string
	events chan record
	ctx    context.Context
}

func (w eventWriter) Write(b []byte) (int, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(b, &data); err != nil {
		return 0, err
	}
	select {
	case w.events <- record{w.node, data}:
		return len(b), nil
	case <-w.ctx.Done():
		return 0, io.EOF
	}
}

type inputPipe struct {
	mu     sync.Mutex
	writer *io.PipeWriter
}

func (p *inputPipe) send(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return json.NewEncoder(p.writer).Encode(v)
}

type harness struct {
	t       *testing.T
	hosts   []*Host
	inputs  []*inputPipe
	mu      sync.Mutex
	logs    []record
	blocked map[string]bool
	ctx     context.Context
	cancel  context.CancelFunc
}

func cluster(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c := &harness{t: t, blocked: map[string]bool{}, ctx: ctx, cancel: cancel}
	output := make(chan record, 1024)
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("node-%d", i+1)
		reader, writer := io.Pipe()
		input := io.MultiReader(strings.NewReader(fmt.Sprintf("{\"type\":\"init\",\"node\":%q,\"nodes\":[\"node-1\",\"node-2\"]}\n", id)), reader)
		n, err := sdk.OpenStreams(input, eventWriter{id, output, ctx})
		if err != nil {
			t.Fatal(err)
		}
		c.hosts = append(c.hosts, New(n))
		c.inputs = append(c.inputs, &inputPipe{writer: writer})
		t.Cleanup(func() { writer.Close(); reader.Close() })
	}
	go func() {
		seq := 0
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-output:
				c.mu.Lock()
				c.logs = append(c.logs, r)
				c.mu.Unlock()
				var kind string
				_ = json.Unmarshal(r.data["type"], &kind)
				if kind != "send" {
					continue
				}
				var to string
				_ = json.Unmarshal(r.data["to"], &to)
				c.mu.Lock()
				blocked := c.blocked[r.node+">"+to]
				c.mu.Unlock()
				if blocked {
					continue
				}
				seq++
				dest := 0
				if to == "node-2" {
					dest = 1
				}
				_ = c.inputs[dest].send(map[string]any{"type": "message", "id": fmt.Sprint(seq), "from": r.node, "to": to, "payload": r.data["payload"]})
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		for _, h := range c.hosts {
			h.Close()
		}
	})
	return c
}
func (c *harness) start() {
	for _, h := range c.hosts {
		go func(h *Host) { _ = h.Run(c.ctx) }(h)
	}
	c.wait(func(r record) bool { return string(r.data["type"]) == `"input_schema"` })
}
func (c *harness) wait(predicate func(record) bool) record {
	c.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, r := range c.logs {
			if predicate(r) {
				c.mu.Unlock()
				return r
			}
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond * 5)
	}
	c.t.Fatal("record timeout")
	return record{}
}

type EchoArgs struct {
	Number  int32
	Text    string
	Enabled bool
	Nested  map[string][]string
}
type EchoReply struct{ Args EchoArgs }
type EchoService struct{}
type hiddenArgs struct{ Value string }

func (*EchoService) NotAnRPC(hiddenArgs, *EchoReply) error { return nil }

func (*EchoService) Echo(args EchoArgs, reply *EchoReply) error {
	if args.Text == "fail" {
		return errors.New("handler failure")
	}
	reply.Args = args
	return nil
}
func TestNetRPCStandardClientAndAutomaticInputs(t *testing.T) {
	c := cluster(t)
	for _, h := range c.hosts {
		if err := h.RegisterName("Application", &EchoService{}, Application()); err != nil {
			t.Fatal(err)
		}
	}
	c.start()
	client := c.hosts[0].Dial("node-2")
	defer client.Close()
	args := EchoArgs{42, "hello", false, map[string][]string{"values": {"a", "b"}}}
	var response EchoReply
	if err := client.Call("Application.Echo", args, &response); err != nil {
		t.Fatal(err)
	}
	if response.Args.Number != 42 || response.Args.Nested["values"][1] != "b" {
		t.Fatalf("%+v", response)
	}
	done := client.Go("Application.Echo", args, &EchoReply{}, make(chan *rpc.Call, 1))
	if err := (<-done.Done).Error; err != nil {
		t.Fatal(err)
	}
	if err := client.Call("Application.Echo", EchoArgs{Text: "fail"}, &response); err == nil || !strings.Contains(err.Error(), "handler failure") {
		t.Fatalf("error: %v", err)
	}
	var schema []sdk.InputAction
	r := c.wait(func(r record) bool { return r.node == "node-1" && string(r.data["type"]) == `"input_schema"` })
	if err := json.Unmarshal(r.data["schema"], &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema) != 1 || schema[0].Label != "Application.Echo" || len(schema[0].Fields) != 4 {
		t.Fatalf("%+v", schema)
	}
	_ = c.inputs[0].send(map[string]any{"type": "command", "id": "input-7", "action": schema[0].Action, "values": args})
	result := c.wait(func(r record) bool {
		return string(r.data["type"]) == `"command_result"` && string(r.data["commandId"]) == `"input-7"`
	})
	if !bytes.Contains(result.data["result"], []byte(`"Number":42`)) {
		t.Fatalf("%s", result.data["result"])
	}
	c.wait(func(r record) bool {
		var p packet
		_ = json.Unmarshal(r.data["payload"], &p)
		return p.Type == "RPCResponse" && p.Method == "Application.Echo" && len(p.Wire) > 0
	})
}
func TestNetRPCPartitionAndClientClose(t *testing.T) {
	c := cluster(t)
	_ = c.hosts[1].RegisterName("Echo", &EchoService{})
	c.hosts[0].Timeout = 50 * time.Millisecond
	c.start()
	c.mu.Lock()
	c.blocked["node-2>node-1"] = true
	c.mu.Unlock()
	client := c.hosts[0].Dial("node-2")
	err := client.Call("Echo.Echo", EchoArgs{Number: 1}, &EchoReply{})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected bounded lost-response timeout, got %v", err)
	}
	client.Close()
	if err := client.Call("Echo.Echo", EchoArgs{}, &EchoReply{}); err == nil {
		t.Fatal("closed client accepted call")
	}
}

type grpcService struct {
	pb.UnimplementedApplicationServer
	pb.UnimplementedInternalServer
	node string
}

func (s *grpcService) Echo(ctx context.Context, in *pb.Request) (*pb.Reply, error) {
	if in.Text == "wait" {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if in.Text == "fail" {
		return nil, status.Error(codes.FailedPrecondition, "rejected")
	}
	if in.Text == "metadata" {
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get("lesson")) == 0 {
			return nil, status.Error(codes.InvalidArgument, "missing metadata")
		}
		_ = grpc.SetHeader(ctx, metadata.Pairs("reply-header", "yes"))
		grpc.SetTrailer(ctx, metadata.Pairs("reply-trailer", "yes"))
	}
	return &pb.Reply{Received: in, Node: s.node}, nil
}
func TestGRPCGeneratedClientsProtobufAndDescriptors(t *testing.T) {
	c := cluster(t)
	for _, h := range c.hosts {
		server := grpc.NewServer()
		svc := &grpcService{node: h.Node.ID}
		pb.RegisterApplicationServer(server, svc)
		pb.RegisterInternalServer(server, svc)
		if err := h.ServeGRPC(server, "rpctest.Application"); err != nil {
			t.Fatal(err)
		}
	}
	c.start()
	conn, err := c.hosts[0].DialGRPC("node-2")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewInternalClient(conn)
	req := &pb.Request{Large: 9223372036854775807, Count: 0, Enabled: false, Text: "metadata", Tags: []string{"a", "文"}}
	var header, trailer metadata.MD
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("lesson", "test"))
	reply, err := client.Echo(ctx, req, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(req, reply.Received) || reply.Node != "node-2" {
		t.Fatalf("%+v", reply)
	}
	if header.Get("reply-header")[0] != "yes" || trailer.Get("reply-trailer")[0] != "yes" {
		t.Fatal("metadata lost")
	}
	_, err = client.Echo(ctx, &pb.Request{Text: "fail"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("%v", err)
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = client.Echo(cancelCtx, &pb.Request{Text: "wait"})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("%v", err)
	}
	var schema []sdk.InputAction
	r := c.wait(func(r record) bool { return r.node == "node-1" && string(r.data["type"]) == `"input_schema"` })
	_ = json.Unmarshal(r.data["schema"], &schema)
	if len(schema) != 1 || schema[0].Label != "/rpctest.Application/Echo" {
		t.Fatalf("internal service leaked: %+v", schema)
	}
	if schema[0].Fields[0].Type != "text" {
		t.Fatal("int64 must avoid JS rounding")
	}
	_ = c.inputs[0].send(map[string]any{"type": "command", "id": "input-9", "action": schema[0].Action, "values": map[string]any{"large": "9223372036854775807", "tags": []string{"test"}}})
	result := c.wait(func(r record) bool {
		return string(r.data["type"]) == `"command_result"` && string(r.data["commandId"]) == `"input-9"`
	})
	if !bytes.Contains(result.data["result"], []byte(`"9223372036854775807"`)) {
		t.Fatalf("%s %s", result.data["result"], result.data["error"])
	}
	c.wait(func(r record) bool {
		var p packet
		_ = json.Unmarshal(r.data["payload"], &p)
		return p.Type == "RPCRequest" && p.Protocol == "grpc" && bytes.Contains(p.Body, []byte(`"9223372036854775807"`))
	})
}

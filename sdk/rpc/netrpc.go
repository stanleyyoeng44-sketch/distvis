package rpc

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"net/rpc"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"
)

type netServices struct{ server *rpc.Server }

func rpcType(t reflect.Type) bool {
	t = baseType(t)
	first, _ := utf8.DecodeRuneInString(t.Name())
	return t.PkgPath() == "" || unicode.IsUpper(first)
}

func (h *Host) Register(receiver any, options ...ServiceOption) error {
	t := baseType(reflect.TypeOf(receiver))
	return h.RegisterName(t.Name(), receiver, options...)
}

// RegisterName has net/rpc's receiver contract. Application() is optional and
// exposes every suitable method of this service as a browser entry.
func (h *Host) RegisterName(name string, receiver any, options ...ServiceOption) error {
	if h.running {
		return errors.New("register services before Run")
	}
	if h.net == nil {
		h.net = &netServices{rpc.NewServer()}
	}
	if err := h.net.server.RegisterName(name, receiver); err != nil {
		return err
	}
	opts := serviceOptions{}
	for _, o := range options {
		o(&opts)
	}
	t := reflect.TypeOf(receiver)
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if m.PkgPath != "" || m.Type.NumIn() != 3 || m.Type.NumOut() != 1 || m.Type.Out(0) != reflect.TypeFor[error]() || m.Type.In(2).Kind() != reflect.Pointer || !rpcType(m.Type.In(1)) || !rpcType(m.Type.In(2)) {
			continue
		}
		method := name + "." + m.Name
		serve := func(_ context.Context, p packet) packet {
			codec := &requestCodec{request: rpc.Request{ServiceMethod: method}, body: p.Wire}
			err := h.net.server.ServeRequest(codec)
			if err != nil {
				return packet{Error: err.Error()}
			}
			return codec.response
		}
		h.handlers["net/rpc:"+method] = serve
		if opts.application {
			argType := baseType(m.Type.In(1))
			fields := goFields(argType)
			h.addInput("net/rpc", method, fields, func(ctx context.Context, values json.RawMessage) (json.RawMessage, error) {
				arg := reflect.New(argType)
				if err := json.Unmarshal(unwrap(values, fields, argType), arg.Interface()); err != nil {
					return nil, err
				}
				wire, err := gobBytes(arg.Interface())
				if err != nil {
					return nil, err
				}
				response := serve(ctx, packet{Wire: wire})
				if response.Error != "" {
					return nil, errors.New(response.Error)
				}
				return response.Body, nil
			})
		}
	}
	return nil
}

// Dial returns a real *net/rpc.Client. Call and Go retain their standard API.
// Unlike raw net/rpc, a missing response terminates at Host.Timeout.
func (h *Host) Dial(peer string) *rpc.Client {
	ctx, cancel := context.WithCancel(h.ctx)
	codec := &clientCodec{host: h, peer: peer, ctx: ctx, cancel: cancel, responses: make(chan clientResponse, 128)}
	return rpc.NewClientWithCodec(codec)
}
func gobBytes(value any) ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(value)
	return b.Bytes(), err
}
func displayJSON(value any) json.RawMessage {
	b, err := json.Marshal(value)
	if err != nil {
		b, _ = json.Marshal(map[string]string{"encoding": "gob", "displayError": err.Error()})
	}
	return b
}

type requestCodec struct {
	request  rpc.Request
	body     []byte
	response packet
}

func (c *requestCodec) ReadRequestHeader(r *rpc.Request) error { *r = c.request; return nil }
func (c *requestCodec) ReadRequestBody(v any) error {
	if v == nil {
		return nil
	}
	return gob.NewDecoder(bytes.NewReader(c.body)).Decode(v)
}
func (c *requestCodec) WriteResponse(r *rpc.Response, v any) error {
	c.response.Error = r.Error
	c.response.Body = displayJSON(v)
	var err error
	c.response.Wire, err = gobBytes(v)
	return err
}
func (c *requestCodec) Close() error { return nil }

type clientResponse struct {
	seq    uint64
	method string
	packet packet
}
type clientCodec struct {
	host      *Host
	peer      string
	ctx       context.Context
	cancel    context.CancelFunc
	once      sync.Once
	responses chan clientResponse
	current   packet
}

func (c *clientCodec) WriteRequest(r *rpc.Request, args any) error {
	wire, err := gobBytes(args)
	if err != nil {
		return err
	}
	seq, method := r.Seq, r.ServiceMethod
	p := packet{Protocol: "net/rpc", Method: method, Wire: wire, Body: displayJSON(args)}
	go func() {
		response, err := c.host.call(c.ctx, c.peer, p)
		if err != nil {
			response.Error = err.Error()
		}
		select {
		case c.responses <- clientResponse{seq, method, response}:
		case <-c.ctx.Done():
		}
	}()
	return nil
}
func (c *clientCodec) ReadResponseHeader(r *rpc.Response) error {
	select {
	case response := <-c.responses:
		c.current = response.packet
		*r = rpc.Response{Seq: response.seq, ServiceMethod: response.method, Error: response.packet.Error}
		return nil
	case <-c.ctx.Done():
		return io.EOF
	}
}
func (c *clientCodec) ReadResponseBody(v any) error {
	if v == nil {
		return nil
	}
	return gob.NewDecoder(bytes.NewReader(c.current.Wire)).Decode(v)
}
func (c *clientCodec) Close() error { c.once.Do(c.cancel); return nil }

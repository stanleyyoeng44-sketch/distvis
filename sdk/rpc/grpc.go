package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"distvis/sdk"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// ServeGRPC attaches an ordinary registered gRPC server. Generated registration,
// handlers, server interceptors and protobuf codecs run unchanged. Name only the
// application services; their unary methods become automatic browser inputs.
// Peer traffic is carried as protobuf bytes through the controlled transport.
func (h *Host) ServeGRPC(server *grpc.Server, applicationServices ...string) error {
	if h.running {
		return errors.New("register services before Run")
	}
	apps := map[string]bool{}
	for _, s := range applicationServices {
		apps[s] = true
	}
	services := server.GetServiceInfo()
	for s := range apps {
		if _, ok := services[s]; !ok {
			return fmt.Errorf("application service %q is not registered", s)
		}
	}
	listener := bufconn.Listen(1024 * 1024)
	conn, err := grpc.NewClient("passthrough:///distvis-local", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		return err
	}
	// Validate descriptors before starting or publishing any partial registration.
	type methodInfo struct {
		path        string
		method      protoreflect.MethodDescriptor
		application bool
	}
	methods := []methodInfo{}
	for service, info := range services {
		for _, m := range info.Methods {
			if m.IsClientStream || m.IsServerStream {
				conn.Close()
				listener.Close()
				return fmt.Errorf("%s/%s: streaming RPC is not supported by the teaching transport", service, m.Name)
			}
		}
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service))
		if err != nil {
			conn.Close()
			listener.Close()
			return fmt.Errorf("protobuf descriptor for %s: %w", service, err)
		}
		sd, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			conn.Close()
			listener.Close()
			return fmt.Errorf("%s is not a protobuf service", service)
		}
		for _, m := range info.Methods {
			md := sd.Methods().ByName(protoreflect.Name(m.Name))
			if md == nil {
				conn.Close()
				listener.Close()
				return fmt.Errorf("missing method descriptor %s/%s", service, m.Name)
			}
			methods = append(methods, methodInfo{"/" + service + "/" + m.Name, md, apps[service]})
		}
	}
	for _, m := range methods {
		path, md := m.path, m.method
		invoke := func(ctx context.Context, p packet) packet {
			request, response := dynamicpb.NewMessage(md.Input()), dynamicpb.NewMessage(md.Output())
			if err := proto.Unmarshal(p.Wire, request); err != nil {
				return grpcError(status.Error(codes.InvalidArgument, err.Error()))
			}
			ctx = metadata.NewOutgoingContext(ctx, metadata.MD(p.Metadata))
			var headers, trailers metadata.MD
			err := conn.Invoke(ctx, path, request, response, grpc.Header(&headers), grpc.Trailer(&trailers))
			if err != nil {
				out := grpcError(err)
				out.Headers = headers
				out.Trailers = trailers
				return out
			}
			wire, err := proto.Marshal(response)
			if err != nil {
				return grpcError(err)
			}
			body, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(response)
			if err != nil {
				return grpcError(err)
			}
			return packet{Wire: wire, Body: body, Headers: headers, Trailers: trailers}
		}
		h.handlers["grpc:"+path] = invoke
		if m.application {
			fields, wrapped := protoFields(md.Input())
			h.addInput("grpc", path, fields, func(ctx context.Context, values json.RawMessage) (json.RawMessage, error) {
				if wrapped {
					var v map[string]json.RawMessage
					if err := json.Unmarshal(values, &v); err != nil {
						return nil, err
					}
					values = v["request"]
				}
				request := dynamicpb.NewMessage(md.Input())
				if err := protojson.Unmarshal(values, request); err != nil {
					return nil, err
				}
				wire, err := proto.Marshal(request)
				if err != nil {
					return nil, err
				}
				out := invoke(ctx, packet{Wire: wire})
				if out.Error != "" || out.Code != 0 {
					return nil, packetError(out)
				}
				return out.Body, nil
			})
		}
	}
	h.closers = append(h.closers, func() { conn.Close(); server.Stop(); listener.Close() })
	go func() { _ = server.Serve(listener) }()
	return nil
}

// DialGRPC returns a genuine ClientConn for generated unary clients.
// Client deadline/cancellation, metadata and gRPC status details are preserved.
func (h *Host) DialGRPC(peer string) (*grpc.ClientConn, error) {
	intercept := func(ctx context.Context, method string, req, reply any, _ *grpc.ClientConn, _ grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		request, ok := req.(proto.Message)
		if !ok {
			return status.Error(codes.InvalidArgument, "request is not a protobuf message")
		}
		response, ok := reply.(proto.Message)
		if !ok {
			return status.Error(codes.InvalidArgument, "reply is not a protobuf message")
		}
		wire, err := proto.Marshal(request)
		if err != nil {
			return err
		}
		body, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(request)
		if err != nil {
			return err
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		out, err := h.call(ctx, peer, packet{Protocol: "grpc", Method: method, Body: body, Wire: wire, Metadata: md})
		if err != nil {
			return status.FromContextError(err).Err()
		}
		for _, o := range opts {
			switch v := o.(type) {
			case grpc.HeaderCallOption:
				*v.HeaderAddr = metadata.MD(out.Headers)
			case grpc.TrailerCallOption:
				*v.TrailerAddr = metadata.MD(out.Trailers)
			}
		}
		if out.Error != "" || out.Code != 0 {
			return packetError(out)
		}
		return proto.Unmarshal(out.Wire, response)
	}
	return grpc.NewClient("passthrough:///"+peer, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(intercept),
		grpc.WithStreamInterceptor(func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, grpc.Streamer, ...grpc.CallOption) (grpc.ClientStream, error) {
			return nil, status.Error(codes.Unimplemented, "DistVis currently supports unary RPC; streaming needs a stream-aware transport")
		}),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("RPC traffic must use the DistVis unary interceptor")
		}))
}
func grpcError(err error) packet {
	s := status.Convert(err)
	wire, _ := proto.Marshal(s.Proto())
	return packet{Error: s.Message(), Code: int32(s.Code()), Status: wire}
}
func packetError(p packet) error {
	if len(p.Status) > 0 {
		s := &statuspb.Status{}
		if proto.Unmarshal(p.Status, s) == nil {
			return status.FromProto(s).Err()
		}
	}
	code := codes.Code(p.Code)
	if code == codes.OK {
		code = codes.Unknown
	}
	return status.Error(code, p.Error)
}
func protoFields(desc protoreflect.MessageDescriptor) ([]sdk.InputField, bool) {
	fallback := func() ([]sdk.InputField, bool) {
		return []sdk.InputField{{Name: "request", Label: "Request (protobuf JSON)", Type: "json", Required: true}}, true
	}
	if desc.Fields().Len() > 16 || desc.ParentFile().Package() == "google.protobuf" {
		return fallback()
	}
	fields := []sdk.InputField{}
	for i := 0; i < desc.Fields().Len(); i++ {
		f := desc.Fields().Get(i)
		name := f.JSONName()
		if !fieldName.MatchString(name) || name == "constructor" || name == "prototype" {
			return fallback()
		}
		field := sdk.InputField{Name: name, Label: name, Type: "json", Required: f.Cardinality() == protoreflect.Required}
		if !f.IsList() && !f.IsMap() {
			switch f.Kind() {
			case protoreflect.BoolKind:
				// A oneof false must remain distinguishable from an absent field.
				if !f.HasPresence() {
					field.Type = "boolean"
				}
			case protoreflect.StringKind, protoreflect.BytesKind:
				field.Type = "text"
			case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
				field.Type = "text"
				field.Label = name + " (64-bit integer)"
			case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
				field.Type = "number"
				field.Integer = true
				field.Min = number(-2147483648)
				field.Max = number(2147483647)
			case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
				field.Type = "number"
				field.Integer = true
				field.Min = number(0)
				field.Max = number(4294967295)
			case protoreflect.FloatKind, protoreflect.DoubleKind:
				field.Type = "number"
			case protoreflect.EnumKind:
				field.Type = "select"
				for j := 0; j < f.Enum().Values().Len(); j++ {
					field.Options = append(field.Options, string(f.Enum().Values().Get(j).Name()))
				}
			}
		}
		fields = append(fields, field)
	}
	return fields, false
}

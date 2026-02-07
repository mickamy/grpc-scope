package replay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// ReplayMetadataKey is the gRPC metadata key attached to every replayed request.
// The interceptor captures this header so the TUI can filter out replay-originated events.
const ReplayMetadataKey = "x-grpc-scope-replay"

// Request holds the information needed to replay a gRPC call.
type Request struct {
	Method      string              // full method path, e.g. "/pkg.Service/Method"
	PayloadJSON string              // JSON request body
	Metadata    map[string][]string // metadata to forward
}

// Result holds the outcome of a replayed gRPC call.
type Result struct {
	ResponseJSON     string
	StatusCode       uint32
	StatusMessage    string
	Duration         time.Duration
	ResponseHeaders  metadata.MD
	ResponseTrailers metadata.MD
}

// Client manages a gRPC connection to the application server for replaying calls.
type Client struct {
	conn *grpc.ClientConn
}

// NewClient creates a new replay client connected to the given target address.
func NewClient(target string) (*Client, error) {
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("replay: dial %s: %w", target, err)
	}
	return &Client{conn: conn}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Send replays a gRPC unary call using server reflection to resolve types dynamically.
func (c *Client) Send(ctx context.Context, req Request) (*Result, error) {
	svc, method, err := ParseMethod(req.Method)
	if err != nil {
		return nil, err
	}

	inputDesc, outputDesc, err := c.resolveMethod(ctx, svc, method)
	if err != nil {
		return nil, err
	}

	payload := req.PayloadJSON
	if payload == "" {
		payload = "{}"
	}

	reqMsg := dynamicpb.NewMessage(inputDesc)
	if err := protojson.Unmarshal([]byte(payload), reqMsg); err != nil {
		return nil, fmt.Errorf("replay: unmarshal request JSON: %w", err)
	}

	respMsg := dynamicpb.NewMessage(outputDesc)

	md := FilterMetadata(req.Metadata)
	if md == nil {
		md = metadata.MD{}
	}
	md.Set(ReplayMetadataKey, "true")
	outCtx := metadata.NewOutgoingContext(ctx, md)

	callCtx, cancel := context.WithTimeout(outCtx, 30*time.Second)
	defer cancel()

	var respHeaders, respTrailers metadata.MD
	start := time.Now()
	invokeErr := c.conn.Invoke(
		callCtx,
		req.Method,
		reqMsg,
		respMsg,
		grpc.Header(&respHeaders),
		grpc.Trailer(&respTrailers),
	)
	elapsed := time.Since(start)

	result := &Result{
		Duration:         elapsed,
		ResponseHeaders:  respHeaders,
		ResponseTrailers: respTrailers,
	}

	if invokeErr != nil {
		st, _ := status.FromError(invokeErr)
		result.StatusCode = uint32(st.Code())
		result.StatusMessage = st.Message()
		return result, nil
	}

	respJSON, err := protojson.Marshal(respMsg)
	if err != nil {
		return nil, fmt.Errorf("replay: marshal response JSON: %w", err)
	}
	result.ResponseJSON = string(respJSON)

	return result, nil
}

// ParseMethod splits "/pkg.Service/Method" into ("pkg.Service", "Method").
func ParseMethod(fullMethod string) (string, string, error) {
	fullMethod = strings.TrimPrefix(fullMethod, "/")
	parts := strings.SplitN(fullMethod, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("replay: invalid method format %q (expected /service/method)", fullMethod)
	}
	return parts[0], parts[1], nil
}

// resolveMethod uses gRPC server reflection to find the input/output message descriptors
// for the given service and method.
func (c *Client) resolveMethod(ctx context.Context, svc, method string) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor, error) {
	refClient := reflectionpb.NewServerReflectionClient(c.conn)
	stream, err := refClient.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("replay: open reflection stream: %w", err)
	}
	defer func() { _ = stream.CloseSend() }()

	// Request the file containing the service symbol.
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: svc,
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("replay: send reflection request: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, nil, fmt.Errorf("replay: recv reflection response: %w", err)
	}

	fdResp := resp.GetFileDescriptorResponse()
	if fdResp == nil {
		if errResp := resp.GetErrorResponse(); errResp != nil {
			return nil, nil, fmt.Errorf("replay: reflection error: %s", errResp.GetErrorMessage())
		}
		return nil, nil, fmt.Errorf("replay: unexpected reflection response")
	}

	// Build a protoregistry.Files from the returned file descriptors.
	files := new(protoregistry.Files)
	for _, raw := range fdResp.GetFileDescriptorProto() {
		fdProto := new(descriptorpb.FileDescriptorProto)
		if err := proto.Unmarshal(raw, fdProto); err != nil {
			return nil, nil, fmt.Errorf("replay: unmarshal file descriptor: %w", err)
		}

		// Skip if already registered (dependencies may overlap).
		if _, regErr := files.FindFileByPath(fdProto.GetName()); regErr == nil {
			continue
		}

		fd, err := protodesc.NewFile(fdProto, files)
		if err != nil {
			return nil, nil, fmt.Errorf("replay: build file descriptor %s: %w", fdProto.GetName(), err)
		}
		if err := files.RegisterFile(fd); err != nil {
			return nil, nil, fmt.Errorf("replay: register file descriptor %s: %w", fdProto.GetName(), err)
		}
	}

	// Find the service descriptor.
	svcDesc, err := files.FindDescriptorByName(protoreflect.FullName(svc))
	if err != nil {
		return nil, nil, fmt.Errorf("replay: find service %q: %w", svc, err)
	}

	serviceDesc, ok := svcDesc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, nil, fmt.Errorf("replay: %q is not a service", svc)
	}

	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(method))
	if methodDesc == nil {
		return nil, nil, fmt.Errorf("replay: method %q not found in service %q", method, svc)
	}

	if methodDesc.IsStreamingClient() || methodDesc.IsStreamingServer() {
		return nil, nil, fmt.Errorf("replay: streaming methods cannot be replayed")
	}

	return methodDesc.Input(), methodDesc.Output(), nil
}

// FilterMetadata removes internal gRPC headers that should not be forwarded.
func FilterMetadata(md map[string][]string) metadata.MD {
	if md == nil {
		return nil
	}

	out := make(metadata.MD, len(md))
	for k, v := range md {
		lower := strings.ToLower(k)
		if lower == ":authority" ||
			lower == "content-type" ||
			lower == "user-agent" ||
			lower == "te" ||
			strings.HasPrefix(lower, "grpc-") {
			continue
		}
		out[lower] = v
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

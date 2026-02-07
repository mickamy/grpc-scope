package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/mickamy/grpc-scope/internal/domain"
	"github.com/mickamy/grpc-scope/internal/event"
	"github.com/mickamy/grpc-scope/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const defaultPort = 9090

// Option configures a Scope.
type Option func(*Scope)

// WithPort sets the port for the internal gRPC server.
func WithPort(port int) Option {
	return func(s *Scope) {
		s.port = port
	}
}

// Scope captures gRPC traffic and exposes it via an internal gRPC server.
type Scope struct {
	port   int
	broker *event.Broker
	server *server.Server
	nextID uint64
}

// New creates a new Scope and starts the internal gRPC server.
func New(opts ...Option) (*Scope, error) {
	s := &Scope{
		port:   defaultPort,
		broker: event.NewBroker(1024),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.server = server.New(s.broker)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return nil, fmt.Errorf("grpc-scope: failed to listen on port %d: %w", s.port, err)
	}

	go func() {
		if err := s.server.Serve(lis); err != nil {
			// server stopped
		}
	}()

	return s, nil
}

// SubscriberCount returns the number of active Watch subscribers.
func (s *Scope) SubscriberCount() int {
	return s.broker.SubscriberCount()
}

// Close stops the internal gRPC server.
func (s *Scope) Close() {
	s.server.GracefulStop()
}

// UnaryInterceptor returns a gRPC unary server interceptor that captures call events.
func (s *Scope) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		ev := domain.CallEvent{
			ID:              s.generateID(),
			Method:          info.FullMethod,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractMetadata(ctx),
			RequestPayload:  marshalPayload(req),
			ResponsePayload: marshalPayload(resp),
		}

		st, _ := status.FromError(err)
		ev.StatusCode = domain.StatusCode(st.Code() + 1) // +1 for Unspecified offset
		ev.StatusMessage = st.Message()

		s.broker.Publish(ev)

		return resp, err
	}
}

// StreamInterceptor returns a gRPC stream server interceptor that captures call events.
func (s *Scope) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		err := handler(srv, ss)

		ev := domain.CallEvent{
			ID:              s.generateID(),
			Method:          info.FullMethod,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractMetadata(ss.Context()),
		}

		st, _ := status.FromError(err)
		ev.StatusCode = domain.StatusCode(st.Code() + 1)
		ev.StatusMessage = st.Message()

		s.broker.Publish(ev)

		return err
	}
}

func (s *Scope) generateID() string {
	s.nextID++
	return fmt.Sprintf("call-%d", s.nextID)
}

func extractMetadata(ctx context.Context) domain.Metadata {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil
	}
	out := make(domain.Metadata, len(md))
	for k, vs := range md {
		out[k] = vs
	}
	return out
}

func marshalPayload(v any) string {
	if v == nil {
		return ""
	}
	if msg, ok := v.(proto.Message); ok {
		b, err := protojson.Marshal(msg)
		if err == nil {
			return string(b)
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

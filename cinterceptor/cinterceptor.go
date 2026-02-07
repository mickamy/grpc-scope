package cinterceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"connectrpc.com/connect"
	"github.com/mickamy/grpc-scope/internal/domain"
	"github.com/mickamy/grpc-scope/internal/event"
	"github.com/mickamy/grpc-scope/internal/server"
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

// Scope captures ConnectRPC traffic and exposes it via an internal gRPC server.
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

// Interceptor returns a connect.Interceptor that captures call events.
func (s *Scope) Interceptor() connect.Interceptor {
	return &interceptor{scope: s}
}

type interceptor struct {
	scope *Scope
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()

		resp, err := next(ctx, req)

		ev := domain.CallEvent{
			ID:              i.scope.generateID(),
			Method:          req.Spec().Procedure,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractHeaders(req.Header()),
			RequestPayload:  marshalPayload(req.Any()),
		}

		if err != nil {
			code := connect.CodeOf(err)
			ev.StatusCode = domain.StatusCode(code + 1) // +1 for Unspecified offset
			ev.StatusMessage = err.Error()
		} else {
			ev.StatusCode = domain.StatusOK
			ev.ResponsePayload = marshalPayload(resp.Any())
		}

		i.scope.broker.Publish(ev)

		return resp, err
	}
}

func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()

		err := next(ctx, conn)

		ev := domain.CallEvent{
			ID:              i.scope.generateID(),
			Method:          conn.Spec().Procedure,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractHeaders(conn.RequestHeader()),
		}

		if err != nil {
			code := connect.CodeOf(err)
			ev.StatusCode = domain.StatusCode(code + 1)
			ev.StatusMessage = err.Error()
		} else {
			ev.StatusCode = domain.StatusOK
		}

		i.scope.broker.Publish(ev)

		return err
	}
}

func (s *Scope) generateID() string {
	s.nextID++
	return fmt.Sprintf("call-%d", s.nextID)
}

func extractHeaders(h map[string][]string) domain.Metadata {
	if len(h) == 0 {
		return nil
	}
	out := make(domain.Metadata, len(h))
	for k, vs := range h {
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

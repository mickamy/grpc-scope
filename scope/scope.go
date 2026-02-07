package scope

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/mickamy/grpc-scope/domain"
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

// Scope manages the lifecycle of the event broker and internal gRPC server
// that exposes captured traffic to TUI clients.
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

// Publish sends a CallEvent to all connected subscribers.
func (s *Scope) Publish(ev domain.CallEvent) {
	s.broker.Publish(ev)
}

// GenerateID returns a unique sequential ID for a call event.
func (s *Scope) GenerateID() string {
	s.nextID++
	return fmt.Sprintf("call-%d", s.nextID)
}

// MarshalPayload serializes a value to a JSON string for display.
// It first attempts protojson for proto.Message values,
// then falls back to encoding/json, then fmt.Sprintf.
func MarshalPayload(v any) string {
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

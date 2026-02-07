package ginterceptor

import (
	"context"
	"time"

	"github.com/mickamy/grpc-scope/scope"
	"github.com/mickamy/grpc-scope/scope/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Option configures a Scope.
type Option = scope.Option

// WithPort sets the port for the internal gRPC server.
func WithPort(port int) Option {
	return scope.WithPort(port)
}

// Scope captures gRPC traffic and exposes it via an internal gRPC server.
type Scope struct {
	scope *scope.Scope
}

// New creates a new Scope and starts the internal gRPC server.
func New(opts ...Option) (*Scope, error) {
	s, err := scope.New(opts...)
	if err != nil {
		return nil, err
	}
	return &Scope{scope: s}, nil
}

// SubscriberCount returns the number of active Watch subscribers.
func (s *Scope) SubscriberCount() int {
	return s.scope.SubscriberCount()
}

// Close stops the internal gRPC server.
func (s *Scope) Close() {
	s.scope.Close()
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
			ID:              s.scope.GenerateID(),
			Method:          info.FullMethod,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractMetadata(ctx),
			RequestPayload:  scope.MarshalPayload(req),
			ResponsePayload: scope.MarshalPayload(resp),
		}

		st, _ := status.FromError(err)
		ev.StatusCode = domain.StatusCode(st.Code() + 1) // +1 for Unspecified offset
		ev.StatusMessage = st.Message()

		s.scope.Publish(ev)

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
			ID:              s.scope.GenerateID(),
			Method:          info.FullMethod,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractMetadata(ss.Context()),
		}

		st, _ := status.FromError(err)
		ev.StatusCode = domain.StatusCode(st.Code() + 1)
		ev.StatusMessage = st.Message()

		s.scope.Publish(ev)

		return err
	}
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

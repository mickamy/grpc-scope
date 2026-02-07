package cinterceptor

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/mickamy/grpc-scope/scope"
	"github.com/mickamy/grpc-scope/scope/domain"
)

// Option configures a Scope.
type Option = scope.Option

// WithPort sets the port for the internal gRPC server.
func WithPort(port int) Option {
	return scope.WithPort(port)
}

// Scope captures ConnectRPC traffic and exposes it via an internal gRPC server.
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

// Interceptor returns a connect.Interceptor that captures call events.
func (s *Scope) Interceptor() connect.Interceptor {
	return &interceptor{s: s.scope}
}

type interceptor struct {
	s *scope.Scope
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()

		resp, err := next(ctx, req)

		ev := domain.CallEvent{
			ID:              i.s.GenerateID(),
			Method:          req.Spec().Procedure,
			StartTime:       start,
			Duration:        time.Since(start),
			RequestMetadata: extractHeaders(req.Header()),
			RequestPayload:  scope.MarshalPayload(req.Any()),
		}

		if err != nil {
			code := connect.CodeOf(err)
			ev.StatusCode = domain.StatusCode(code + 1) // +1 for Unspecified offset
			ev.StatusMessage = err.Error()
		} else {
			ev.StatusCode = domain.StatusOK
			ev.ResponsePayload = scope.MarshalPayload(resp.Any())
		}

		i.s.Publish(ev)

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
			ID:              i.s.GenerateID(),
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

		i.s.Publish(ev)

		return err
	}
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

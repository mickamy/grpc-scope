package server

import (
	"net"

	"github.com/mickamy/grpc-scope/scope/domain"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"github.com/mickamy/grpc-scope/scope/internal/event"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server exposes a gRPC ScopeService for TUI clients to connect to.
type Server struct {
	grpcServer *grpc.Server
	broker     *event.Broker
}

// New creates a new Server backed by the given Broker.
func New(broker *event.Broker) *Server {
	gs := grpc.NewServer()
	svc := &scopeService{broker: broker}
	scopev1.RegisterScopeServiceServer(gs, svc)

	return &Server{
		grpcServer: gs,
		broker:     broker,
	}
}

// Serve starts the gRPC server on the given listener.
func (s *Server) Serve(lis net.Listener) error {
	return s.grpcServer.Serve(lis)
}

// GracefulStop gracefully stops the server.
func (s *Server) GracefulStop() {
	s.grpcServer.GracefulStop()
}

type scopeService struct {
	scopev1.UnimplementedScopeServiceServer
	broker *event.Broker
}

func (s *scopeService) Watch(_ *scopev1.WatchRequest, stream grpc.ServerStreamingServer[scopev1.WatchResponse]) error {
	ch, unsub := s.broker.Subscribe()
	defer unsub()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&scopev1.WatchResponse{
				Event: domainToProto(ev),
			}); err != nil {
				return err
			}
		}
	}
}

func domainToProto(e domain.CallEvent) *scopev1.CallEvent {
	return &scopev1.CallEvent{
		Id:               e.ID,
		Method:           e.Method,
		StartTime:        timestamppb.New(e.StartTime),
		Duration:         durationpb.New(e.Duration),
		StatusCode:       int32(e.StatusCode),
		StatusMessage:    e.StatusMessage,
		RequestMetadata:  metadataToProto(e.RequestMetadata),
		ResponseHeaders:  metadataToProto(e.ResponseHeaders),
		ResponseTrailers: metadataToProto(e.ResponseTrailers),
		RequestPayload:   e.RequestPayload,
		ResponsePayload:  e.ResponsePayload,
	}
}

func metadataToProto(md domain.Metadata) map[string]*scopev1.MetadataValues {
	if len(md) == 0 {
		return nil
	}
	out := make(map[string]*scopev1.MetadataValues, len(md))
	for k, vs := range md {
		out[k] = &scopev1.MetadataValues{Values: vs}
	}
	return out
}

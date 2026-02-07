package ginterceptor_test

import (
	"fmt"
	"net"
	"testing"
	"time"

	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"github.com/mickamy/grpc-scope/ginterceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type testService struct {
	scopev1.UnimplementedScopeServiceServer
}

func (t *testService) Watch(_ *scopev1.WatchRequest, _ grpc.ServerStreamingServer[scopev1.WatchResponse]) error {
	return status.Error(codes.Unimplemented, "not implemented")
}

func setupTest(t *testing.T) (scopev1.ScopeServiceClient, scopev1.ScopeServiceClient, *ginterceptor.Scope) {
	t.Helper()

	// Find a free port for the scope server
	scopeLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	scopePort := scopeLis.Addr().(*net.TCPAddr).Port
	_ = scopeLis.Close()

	scope, err := ginterceptor.New(ginterceptor.WithPort(scopePort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scope.Close)

	// Start a test gRPC server with the interceptor
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(scope.UnaryInterceptor()),
		grpc.StreamInterceptor(scope.StreamInterceptor()),
	)
	scopev1.RegisterScopeServiceServer(srv, &testService{})

	appLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		if err := srv.Serve(appLis); err != nil {
			// server stopped
		}
	}()
	t.Cleanup(srv.GracefulStop)

	// Client to the test app server
	appConn, err := grpc.NewClient(
		appLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appConn.Close() })
	appClient := scopev1.NewScopeServiceClient(appConn)

	// Client to the scope server (to Watch events)
	scopeConn, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", scopePort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scopeConn.Close() })
	scopeClient := scopev1.NewScopeServiceClient(scopeConn)

	return appClient, scopeClient, scope
}

func waitForSubscriber(t *testing.T, scope *ginterceptor.Scope, wantCount int) {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for scope.SubscriberCount() < wantCount {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d subscriber(s), got %d", wantCount, scope.SubscriberCount())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestStreamInterceptor_CapturesCall(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	appClient, scopeClient, scope := setupTest(t)

	stream, err := scopeClient.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, scope, 1)

	// Make a streaming call which goes through the stream interceptor
	watchStream, err := appClient.Watch(
		metadata.AppendToOutgoingContext(ctx, "x-test-key", "test-value"),
		&scopev1.WatchRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Read until error (the test service returns Unimplemented)
	_, recvErr := watchStream.Recv()
	if recvErr == nil {
		t.Fatal("expected error from test service")
	}

	// Receive the captured event from scope
	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := resp.GetEvent()
	if ev.GetMethod() != "/scope.v1.ScopeService/Watch" {
		t.Errorf("got method %q, want %q", ev.GetMethod(), "/scope.v1.ScopeService/Watch")
	}
	if ev.GetStatusCode() != int32(codes.Unimplemented)+1 { // +1 for Unspecified offset
		t.Errorf("got status code %d, want %d", ev.GetStatusCode(), int32(codes.Unimplemented)+1)
	}
	if ev.GetDuration().AsDuration() <= 0 {
		t.Error("expected positive duration")
	}
}

package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/mickamy/grpc-scope/cinterceptor"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	greeterv1 "github.com/mickamy/grpc-scope/examples/connect/gen/greeter/v1"
	"github.com/mickamy/grpc-scope/examples/connect/gen/greeter/v1/greeterv1connect"
)

func setupE2E(t *testing.T) (greeterv1connect.GreeterServiceClient, scopev1.ScopeServiceClient, *cinterceptor.Scope) {
	t.Helper()

	// Find a free port for the scope server
	scopeLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	scopePort := scopeLis.Addr().(*net.TCPAddr).Port
	_ = scopeLis.Close()

	scope, err := cinterceptor.New(cinterceptor.WithPort(scopePort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scope.Close)

	// Start the greeter Connect server with interceptor
	mux := http.NewServeMux()
	path, handler := greeterv1connect.NewGreeterServiceHandler(
		&greeterServer{},
		connect.WithInterceptors(scope.Interceptor()),
	)
	mux.Handle(path, handler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Connect client to the greeter server
	appClient := greeterv1connect.NewGreeterServiceClient(
		http.DefaultClient,
		srv.URL,
	)

	// gRPC client to the scope server (to Watch events)
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

func waitForSubscriber(t *testing.T, scope *cinterceptor.Scope, wantCount int) {
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

func TestE2E_SayHello(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	appClient, scopeClient, scope := setupE2E(t)

	// Start watching scope events
	stream, err := scopeClient.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, scope, 1)

	// Call SayHello
	resp, err := appClient.SayHello(ctx, connect.NewRequest(&greeterv1.SayHelloRequest{Name: "World"}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetMessage() != "Hello, World!" {
		t.Errorf("got message %q, want %q", resp.Msg.GetMessage(), "Hello, World!")
	}

	// Receive the captured event from scope
	watchResp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := watchResp.GetEvent()

	// Verify method
	if ev.GetMethod() != "/greeter.v1.GreeterService/SayHello" {
		t.Errorf("got method %q, want %q", ev.GetMethod(), "/greeter.v1.GreeterService/SayHello")
	}

	// Verify status (OK = 1)
	if ev.GetStatusCode() != 1 {
		t.Errorf("got status code %d, want %d", ev.GetStatusCode(), 1)
	}

	// Verify duration
	if ev.GetDuration().AsDuration() <= 0 {
		t.Error("expected positive duration")
	}

	// Verify request payload contains "World"
	if ev.GetRequestPayload() == "" {
		t.Error("expected non-empty request payload")
	}

	// Verify response payload contains "Hello, World!"
	if ev.GetResponsePayload() == "" {
		t.Error("expected non-empty response payload")
	}
}

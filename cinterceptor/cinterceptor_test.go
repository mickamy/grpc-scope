package cinterceptor_test

import (
	"context"
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
)

func setupTest(t *testing.T) (scopev1.ScopeServiceClient, *cinterceptor.Scope, string) {
	t.Helper()

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

	mux := http.NewServeMux()
	mux.Handle("/test.TestService/Echo", connect.NewUnaryHandler(
		"/test.TestService/Echo",
		func(_ context.Context, _ *connect.Request[scopev1.WatchRequest]) (*connect.Response[scopev1.WatchResponse], error) {
			return connect.NewResponse(&scopev1.WatchResponse{}), nil
		},
		connect.WithInterceptors(scope.Interceptor()),
	))
	mux.Handle("/test.TestService/Stream", connect.NewServerStreamHandler(
		"/test.TestService/Stream",
		func(_ context.Context, _ *connect.Request[scopev1.WatchRequest], _ *connect.ServerStream[scopev1.WatchResponse]) error {
			return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("not implemented"))
		},
		connect.WithInterceptors(scope.Interceptor()),
	))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	scopeConn, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", scopePort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scopeConn.Close() })
	scopeClient := scopev1.NewScopeServiceClient(scopeConn)

	return scopeClient, scope, srv.URL
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

func TestUnaryInterceptor_CapturesCall(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scopeClient, scope, serverURL := setupTest(t)

	stream, err := scopeClient.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, scope, 1)

	client := connect.NewClient[scopev1.WatchRequest, scopev1.WatchResponse](
		http.DefaultClient,
		serverURL+"/test.TestService/Echo",
	)
	_, err = client.CallUnary(ctx, connect.NewRequest(&scopev1.WatchRequest{}))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := resp.GetEvent()
	if ev.GetMethod() != "/test.TestService/Echo" {
		t.Errorf("got method %q, want %q", ev.GetMethod(), "/test.TestService/Echo")
	}
	if ev.GetStatusCode() != 1 { // domain.StatusOK
		t.Errorf("got status code %d, want %d", ev.GetStatusCode(), 1)
	}
	if ev.GetDuration().AsDuration() <= 0 {
		t.Error("expected positive duration")
	}
}

func TestStreamInterceptor_CapturesCall(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scopeClient, scope, serverURL := setupTest(t)

	stream, err := scopeClient.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, scope, 1)

	client := connect.NewClient[scopev1.WatchRequest, scopev1.WatchResponse](
		http.DefaultClient,
		serverURL+"/test.TestService/Stream",
	)
	serverStream, err := client.CallServerStream(ctx, connect.NewRequest(&scopev1.WatchRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer serverStream.Close()
	for serverStream.Receive() {
		// drain
	}
	if serverStream.Err() == nil {
		t.Fatal("expected error from test service")
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := resp.GetEvent()
	if ev.GetMethod() != "/test.TestService/Stream" {
		t.Errorf("got method %q, want %q", ev.GetMethod(), "/test.TestService/Stream")
	}
	if ev.GetStatusCode() != int32(connect.CodeUnimplemented)+1 { // +1 for Unspecified offset
		t.Errorf("got status code %d, want %d", ev.GetStatusCode(), int32(connect.CodeUnimplemented)+1)
	}
	if ev.GetDuration().AsDuration() <= 0 {
		t.Error("expected positive duration")
	}
}

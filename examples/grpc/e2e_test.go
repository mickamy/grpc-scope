package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/grpc-scope/ginterceptor"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	greeterv1 "github.com/mickamy/grpc-scope/examples/grpc/gen/greeter/v1"
	todov1 "github.com/mickamy/grpc-scope/examples/grpc/gen/todo/v1"
)

type e2eClients struct {
	greeter greeterv1.GreeterServiceClient
	todo    todov1.TodoServiceClient
	scope   scopev1.ScopeServiceClient
	scopeS  *ginterceptor.Scope
}

func setupE2E(t *testing.T) e2eClients {
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

	// Start the greeter gRPC server with interceptors
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(scope.UnaryInterceptor()),
		grpc.StreamInterceptor(scope.StreamInterceptor()),
	)
	greeterv1.RegisterGreeterServiceServer(srv, &greeterServer{})
	todov1.RegisterTodoServiceServer(srv, &todoServer{todos: make(map[string]*todov1.Todo)})

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

	// Client to the greeter server
	appConn, err := grpc.NewClient(
		appLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appConn.Close() })
	appClient := greeterv1.NewGreeterServiceClient(appConn)
	todoClient := todov1.NewTodoServiceClient(appConn)

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

	return e2eClients{
		greeter: appClient,
		todo:    todoClient,
		scope:   scopeClient,
		scopeS:  scope,
	}
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

func TestE2E_SayHello(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := setupE2E(t)

	// Start watching scope events
	stream, err := c.scope.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, c.scopeS, 1)

	// Call SayHello
	resp, err := c.greeter.SayHello(ctx, &greeterv1.SayHelloRequest{Name: "World"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetMessage() != "Hello, World!" {
		t.Errorf("got message %q, want %q", resp.GetMessage(), "Hello, World!")
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

func TestE2E_SayHello_LongPayload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := setupE2E(t)

	stream, err := c.scope.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, c.scopeS, 1)

	// Build a long name (~1000 chars) to produce a large request/response payload.
	longName := strings.Repeat("abcdefghij", 100)

	resp, err := c.greeter.SayHello(ctx, &greeterv1.SayHelloRequest{Name: longName})
	if err != nil {
		t.Fatal(err)
	}

	wantMessage := "Hello, " + longName + "!"
	if resp.GetMessage() != wantMessage {
		t.Errorf("got message length %d, want %d", len(resp.GetMessage()), len(wantMessage))
	}

	watchResp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := watchResp.GetEvent()

	if !strings.Contains(ev.GetRequestPayload(), longName) {
		t.Error("expected request payload to contain the long name")
	}

	if !strings.Contains(ev.GetResponsePayload(), longName) {
		t.Error("expected response payload to contain the long name")
	}
}

func TestE2E_CreateTodo(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := setupE2E(t)

	stream, err := c.scope.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, c.scopeS, 1)

	// Create a todo
	todo, err := c.todo.CreateTodo(ctx, &todov1.CreateTodoRequest{
		Title:       "Buy milk",
		Description: "Go to the store",
		Priority:    todov1.Priority_PRIORITY_HIGH,
		Tags:        []string{"shopping", "errands"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if todo.GetTitle() != "Buy milk" {
		t.Errorf("got title %q, want %q", todo.GetTitle(), "Buy milk")
	}
	if todo.GetPriority() != todov1.Priority_PRIORITY_HIGH {
		t.Errorf("got priority %v, want %v", todo.GetPriority(), todov1.Priority_PRIORITY_HIGH)
	}

	// Verify the scope event
	watchResp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := watchResp.GetEvent()

	if ev.GetMethod() != "/todo.v1.TodoService/CreateTodo" {
		t.Errorf("got method %q, want %q", ev.GetMethod(), "/todo.v1.TodoService/CreateTodo")
	}

	// OK = codes.OK(0) + 1 = 1
	if ev.GetStatusCode() != 1 {
		t.Errorf("got status code %d, want %d", ev.GetStatusCode(), 1)
	}

	if !strings.Contains(ev.GetRequestPayload(), "Buy milk") {
		t.Error("expected request payload to contain 'Buy milk'")
	}

	if !strings.Contains(ev.GetResponsePayload(), "Buy milk") {
		t.Error("expected response payload to contain 'Buy milk'")
	}
}

func TestE2E_GetTodo_NotFound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := setupE2E(t)

	stream, err := c.scope.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, c.scopeS, 1)

	// Try to get a non-existent todo
	_, err = c.todo.GetTodo(ctx, &todov1.GetTodoRequest{Id: "nonexistent"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the scope event
	watchResp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := watchResp.GetEvent()

	if ev.GetMethod() != "/todo.v1.TodoService/GetTodo" {
		t.Errorf("got method %q, want %q", ev.GetMethod(), "/todo.v1.TodoService/GetTodo")
	}

	// NotFound = codes.NotFound(5) + 1 = 6
	if ev.GetStatusCode() != 6 {
		t.Errorf("got status code %d, want %d (NotFound)", ev.GetStatusCode(), 6)
	}
}

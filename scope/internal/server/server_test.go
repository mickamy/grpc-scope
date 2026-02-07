package server_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/mickamy/grpc-scope/scope/domain"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"github.com/mickamy/grpc-scope/scope/internal/event"
	"github.com/mickamy/grpc-scope/scope/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func startServer(t *testing.T) (scopev1.ScopeServiceClient, *event.Broker) {
	t.Helper()

	broker := event.NewBroker(100)
	srv := server.New(broker)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		if err := srv.Serve(lis); err != nil {
			// server stopped
		}
	}()
	t.Cleanup(srv.GracefulStop)

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return scopev1.NewScopeServiceClient(conn), broker
}

// waitForSubscriber polls the broker until at least wantCount subscribers are registered.
func waitForSubscriber(t *testing.T, ctx context.Context, broker *event.Broker, wantCount int) {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for broker.SubscriberCount() < wantCount {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d subscriber(s), got %d", wantCount, broker.SubscriberCount())
		case <-ctx.Done():
			t.Fatal("context cancelled while waiting for subscriber")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestWatch_ReceivesPublishedEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, broker := startServer(t)

	stream, err := client.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, ctx, broker, 1)

	now := time.Now()
	broker.Publish(domain.CallEvent{
		ID:              "evt-1",
		Method:          "/test.v1.TestService/Get",
		StartTime:       now,
		Duration:        10 * time.Millisecond,
		StatusCode:      domain.StatusOK,
		StatusMessage:   "OK",
		RequestPayload:  `{"id":"123"}`,
		ResponsePayload: `{"name":"test"}`,
		RequestMetadata: domain.Metadata{
			"authorization": {"Bearer token"},
		},
	})

	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	ev := resp.GetEvent()
	if ev.GetId() != "evt-1" {
		t.Errorf("got ID %q, want %q", ev.GetId(), "evt-1")
	}
	if ev.GetMethod() != "/test.v1.TestService/Get" {
		t.Errorf("got Method %q, want %q", ev.GetMethod(), "/test.v1.TestService/Get")
	}
	if ev.GetStatusCode() != int32(domain.StatusOK) {
		t.Errorf("got StatusCode %d, want %d", ev.GetStatusCode(), domain.StatusOK)
	}
	if ev.GetRequestPayload() != `{"id":"123"}` {
		t.Errorf("got RequestPayload %q, want %q", ev.GetRequestPayload(), `{"id":"123"}`)
	}
	if ev.GetResponsePayload() != `{"name":"test"}` {
		t.Errorf("got ResponsePayload %q, want %q", ev.GetResponsePayload(), `{"name":"test"}`)
	}
	md := ev.GetRequestMetadata()
	if md == nil || len(md["authorization"].GetValues()) == 0 {
		t.Fatal("expected request metadata with authorization key")
	}
	if got := md["authorization"].GetValues()[0]; got != "Bearer token" {
		t.Errorf("got authorization %q, want %q", got, "Bearer token")
	}
}

func TestWatch_MultipleEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, broker := startServer(t)

	stream, err := client.Watch(ctx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, ctx, broker, 1)

	for i := range 3 {
		broker.Publish(domain.CallEvent{
			ID:         fmt.Sprintf("evt-%d", i),
			Method:     "/test.v1.TestService/List",
			StatusCode: domain.StatusOK,
		})
	}

	for range 3 {
		resp, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if resp.GetEvent() == nil {
			t.Fatal("expected non-nil event")
		}
	}
}

func TestWatch_ClientCancelStopsStream(t *testing.T) {
	t.Parallel()

	cancelCtx, cancel := context.WithCancel(t.Context())
	client, broker := startServer(t)

	stream, err := client.Watch(cancelCtx, &scopev1.WatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	waitForSubscriber(t, t.Context(), broker, 1)

	broker.Publish(domain.CallEvent{ID: "before-cancel", StatusCode: domain.StatusOK})
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	cancel()

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error after cancel, got nil")
	}
}

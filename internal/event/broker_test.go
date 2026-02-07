package event_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mickamy/grpc-scope/domain"
	"github.com/mickamy/grpc-scope/internal/event"
)

func TestBroker_SubscribeReceivesPublishedEvents(t *testing.T) {
	t.Parallel()

	b := event.NewBroker(10)
	ch, unsub := b.Subscribe()
	defer unsub()

	want := domain.CallEvent{
		ID:         "evt-1",
		Method:     "/test.Service/Method",
		StatusCode: domain.StatusOK,
	}

	b.Publish(want)

	select {
	case got := <-ch:
		if got.ID != want.ID {
			t.Errorf("got ID %q, want %q", got.ID, want.ID)
		}
		if got.Method != want.Method {
			t.Errorf("got Method %q, want %q", got.Method, want.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBroker_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	b := event.NewBroker(10)

	ch1, unsub1 := b.Subscribe()
	defer unsub1()

	ch2, unsub2 := b.Subscribe()
	defer unsub2()

	want := domain.CallEvent{ID: "evt-1"}
	b.Publish(want)

	for i, ch := range []<-chan domain.CallEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.ID != want.ID {
				t.Errorf("subscriber %d: got ID %q, want %q", i, got.ID, want.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestBroker_UnsubscribeStopsReceiving(t *testing.T) {
	t.Parallel()

	b := event.NewBroker(10)
	ch, unsub := b.Subscribe()
	unsub()

	b.Publish(domain.CallEvent{ID: "evt-after-unsub"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestBroker_UnsubscribeIsIdempotent(t *testing.T) {
	t.Parallel()

	b := event.NewBroker(10)
	_, unsub := b.Subscribe()

	// calling unsubscribe multiple times should not panic
	unsub()
	unsub()
}

func TestBroker_SlowSubscriberDoesNotBlockPublish(t *testing.T) {
	t.Parallel()

	b := event.NewBroker(1) // buffer of 1
	ch, unsub := b.Subscribe()
	defer unsub()

	// fill the buffer
	b.Publish(domain.CallEvent{ID: "evt-1"})

	// this should not block even though the buffer is full
	done := make(chan struct{})
	go func() {
		b.Publish(domain.CallEvent{ID: "evt-2"})
		close(done)
	}()

	select {
	case <-done:
		// success: Publish did not block
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}

	// only the first event should be in the buffer
	got := <-ch
	if got.ID != "evt-1" {
		t.Errorf("got ID %q, want %q", got.ID, "evt-1")
	}
}

func TestBroker_ConcurrentPublish(t *testing.T) {
	t.Parallel()

	b := event.NewBroker(100)
	ch, unsub := b.Subscribe()
	defer unsub()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func() {
			defer wg.Done()
			b.Publish(domain.CallEvent{ID: fmt.Sprintf("evt-%d", i)})
		}()
	}

	wg.Wait()

	received := 0
	for range n {
		select {
		case <-ch:
			received++
		case <-time.After(time.Second):
			t.Fatalf("timed out: received %d/%d events", received, n)
		}
	}

	if received != n {
		t.Errorf("received %d events, want %d", received, n)
	}
}

package event

import (
	"sync"

	"github.com/mickamy/grpc-scope/internal/domain"
)

// Broker fans out CallEvents to all active subscribers.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[int]chan domain.CallEvent
	nextID      int
	bufSize     int
}

// NewBroker creates a new Broker. bufSize controls the channel buffer size for each subscriber.
func NewBroker(bufSize int) *Broker {
	return &Broker{
		subscribers: make(map[int]chan domain.CallEvent),
		bufSize:     bufSize,
	}
}

// Subscribe returns a channel that receives published CallEvents and an unsubscribe function.
func (b *Broker) Subscribe() (<-chan domain.CallEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	ch := make(chan domain.CallEvent, b.bufSize)
	b.subscribers[id] = ch

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		if _, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(ch)
		}
	}

	return ch, unsubscribe
}

// Publish sends an event to all current subscribers.
// Slow subscribers that have full buffers will have the event dropped.
func (b *Broker) Publish(event domain.CallEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// drop event for slow subscriber
		}
	}
}

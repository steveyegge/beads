package api

import (
	"context"
	"sync"
	"sync/atomic"
)

// LocalEventDispatcher provides an in-process implementation of EventSource and EventPublisher.
type LocalEventDispatcher struct {
	nextID      uint64
	mu          sync.RWMutex
	subscribers map[uint64]chan IssueEvent
	buffer      int
}

// NewLocalEventDispatcher constructs a dispatcher with the provided per-subscriber buffer.
func NewLocalEventDispatcher(buffer int) *LocalEventDispatcher {
	if buffer <= 0 {
		buffer = 16
	}

	return &LocalEventDispatcher{
		subscribers: make(map[uint64]chan IssueEvent),
		buffer:      buffer,
	}
}

// Subscribe implements EventSource by registering a new listener and returning a channel of events.
func (d *LocalEventDispatcher) Subscribe(ctx context.Context) (<-chan IssueEvent, error) {
	ch := make(chan IssueEvent, d.buffer)
	id := atomic.AddUint64(&d.nextID, 1)

	d.mu.Lock()
	d.subscribers[id] = ch
	d.mu.Unlock()

	go func() {
		<-ctx.Done()
		d.mu.Lock()
		delete(d.subscribers, id)
		close(ch)
		d.mu.Unlock()
	}()

	return ch, nil
}

// Publish broadcasts the event to all active subscribers without blocking.
func (d *LocalEventDispatcher) Publish(evt IssueEvent) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, ch := range d.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop event if subscriber is slow; UI will receive next heartbeat/update.
		}
	}
}

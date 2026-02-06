package eventbus

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
)

// Bus dispatches events to registered handlers. It uses a local channel-based
// approach — no NATS dependency. The NATS integration wraps this for
// distributed dispatch.
type Bus struct {
	handlers []Handler
	mu       sync.RWMutex
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{}
}

// Register adds a handler to the bus. Handlers are sorted by priority on
// each Dispatch call, so registration order does not matter.
func (b *Bus) Register(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
}

// Dispatch sends an event to all registered handlers that handle its type.
// Handlers are called sequentially in priority order (lowest first).
// Handler errors are logged but do not stop the chain — the bus is resilient.
func (b *Bus) Dispatch(ctx context.Context, event *Event) (*Result, error) {
	if event == nil {
		return nil, fmt.Errorf("eventbus: nil event")
	}

	b.mu.RLock()
	matching := b.matchingHandlers(event.Type)
	b.mu.RUnlock()

	result := &Result{}

	for _, h := range matching {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("eventbus: context cancelled: %w", err)
		}

		if err := h.Handle(ctx, event, result); err != nil {
			log.Printf("eventbus: handler %q error for %s: %v", h.ID(), event.Type, err)
		}
	}

	return result, nil
}

// Handlers returns all registered handlers (for introspection/status reporting).
func (b *Bus) Handlers() []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Handler, len(b.handlers))
	copy(out, b.handlers)
	return out
}

// matchingHandlers returns handlers that handle the given event type, sorted
// by priority (lowest first). Must be called with at least a read lock held.
func (b *Bus) matchingHandlers(eventType EventType) []Handler {
	var matched []Handler
	for _, h := range b.handlers {
		for _, t := range h.Handles() {
			if t == eventType {
				matched = append(matched, h)
				break
			}
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Priority() < matched[j].Priority()
	})
	return matched
}

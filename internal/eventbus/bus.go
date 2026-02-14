package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Bus dispatches events to registered handlers and optionally publishes
// events to NATS JetStream for persistence and distributed consumption.
type Bus struct {
	handlers []Handler
	js       nats.JetStreamContext
	mu       sync.RWMutex
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{}
}

// SetJetStream attaches a JetStream context for event publishing.
// When set, Dispatch will publish events to JetStream after running
// local handlers. Publishing is async — errors are logged but do not
// affect handler results.
func (b *Bus) SetJetStream(js nats.JetStreamContext) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.js = js
}

// JetStreamEnabled returns true if JetStream publishing is configured.
func (b *Bus) JetStreamEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.js != nil
}

// JetStream returns the JetStream context, or nil if not configured.
func (b *Bus) JetStream() nats.JetStreamContext {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.js
}

// Register adds a handler to the bus. Handlers are sorted by priority on
// each Dispatch call, so registration order does not matter.
func (b *Bus) Register(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
}

// Unregister removes a handler by ID. Returns true if a handler was removed.
func (b *Bus) Unregister(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, h := range b.handlers {
		if h.ID() == id {
			b.handlers = append(b.handlers[:i], b.handlers[i+1:]...)
			return true
		}
	}
	return false
}

// Dispatch sends an event to all registered handlers that handle its type.
// Handlers are called sequentially in priority order (lowest first).
// Handler errors are logged but do not stop the chain — the bus is resilient.
// If JetStream is configured, the event is published after handler dispatch.
func (b *Bus) Dispatch(ctx context.Context, event *Event) (*Result, error) {
	if event == nil {
		return nil, fmt.Errorf("eventbus: nil event")
	}

	b.mu.RLock()
	matching := b.matchingHandlers(event.Type)
	js := b.js
	b.mu.RUnlock()

	result := &Result{}

	for _, h := range matching {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("eventbus: context canceled: %w", err)
		}

		if err := h.Handle(ctx, event, result); err != nil {
			log.Printf("eventbus: handler %q error for %s: %v", h.ID(), event.Type, err)
		}
	}

	// Publish to JetStream for persistence (fire-and-forget).
	if js != nil {
		b.publishToJetStream(js, event)
	}

	return result, nil
}

// publishToJetStream publishes an event to the HOOK_EVENTS JetStream stream.
// Errors are logged but never propagated — JetStream is supplementary to
// local dispatch, not a prerequisite.
//
// When Raw is set (normal daemon RPC path), the original Claude Code JSON is
// published as-is — this preserves maximum fidelity for external consumers
// like Coop. When Raw is empty (programmatic events), the Event struct is
// marshaled with a published_at timestamp.
func (b *Bus) publishToJetStream(js nats.JetStreamContext, event *Event) {
	subject := SubjectForEvent(event.Type)

	// For decision events, extract requested_by from the payload to build a
	// scoped subject (decisions.<requestedBy>.<EventType>). This allows agents
	// to subscribe only to their own decisions. (beads-bug-agents_receive_other_agents_decision)
	if event.Type.IsDecisionEvent() && len(event.Raw) > 0 {
		var peek struct {
			RequestedBy string `json:"requested_by"`
		}
		if json.Unmarshal(event.Raw, &peek) == nil {
			subject = SubjectForDecisionEvent(event.Type, peek.RequestedBy)
		}
	}

	// Use the raw JSON if available, otherwise marshal the event.
	var data []byte
	if len(event.Raw) > 0 {
		data = event.Raw
	} else {
		now := time.Now().UTC()
		event.PublishedAt = &now
		var err error
		data, err = json.Marshal(event)
		if err != nil {
			log.Printf("eventbus: failed to marshal event for JetStream: %v", err)
			return
		}
	}

	ack, err := js.Publish(subject, data)
	if err != nil {
		log.Printf("eventbus: JetStream publish to %s failed: %v", subject, err)
	} else {
		log.Printf("eventbus: JetStream published to %s (stream=%s seq=%d, %d bytes)",
			subject, ack.Stream, ack.Sequence, len(data))
	}
}

// PublishRaw publishes arbitrary JSON data to a JetStream subject.
// Used by non-hook event producers (e.g., mutation events) that don't flow
// through the Dispatch handler chain. Returns silently if JetStream is not enabled.
func (b *Bus) PublishRaw(subject string, data []byte) {
	b.mu.RLock()
	js := b.js
	b.mu.RUnlock()

	if js == nil {
		return
	}

	ack, err := js.Publish(subject, data)
	if err != nil {
		log.Printf("eventbus: JetStream publish to %s failed: %v", subject, err)
	} else {
		log.Printf("eventbus: JetStream published to %s (stream=%s seq=%d, %d bytes)",
			subject, ack.Stream, ack.Sequence, len(data))
	}
}

// Handlers returns all registered handlers (for introspection/status reporting).
func (b *Bus) Handlers() []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Handler, len(b.handlers))
	copy(out, b.handlers)
	return out
}

// LoadPersistedHandlers loads external handlers from config table entries
// with the "bus.handler." prefix. Each value is a JSON-encoded ExternalHandlerConfig.
// Returns the number of handlers loaded.
func (b *Bus) LoadPersistedHandlers(configs map[string]string) int {
	count := 0
	for key, value := range configs {
		if !strings.HasPrefix(key, HandlerConfigPrefix) {
			continue
		}
		var cfg ExternalHandlerConfig
		if err := json.Unmarshal([]byte(value), &cfg); err != nil {
			log.Printf("eventbus: skip bad config %q: %v", key, err)
			continue
		}
		if cfg.ID == "" || cfg.Command == "" || len(cfg.Events) == 0 {
			log.Printf("eventbus: skip incomplete config %q", key)
			continue
		}
		h := NewExternalHandler(cfg)
		b.Register(h)
		count++
		log.Printf("eventbus: loaded persisted handler %q (priority=%d, events=%v)", cfg.ID, h.Priority(), cfg.Events)
	}
	return count
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

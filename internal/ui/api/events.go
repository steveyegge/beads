package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// EventType enumerates the supported UI SSE event kinds.
type EventType string

const (
	// EventTypeCreated indicates a new issue was created.
	EventTypeCreated EventType = "created"
	// EventTypeUpdated indicates an issue was updated.
	EventTypeUpdated EventType = "updated"
	// EventTypeClosed indicates an issue was closed.
	EventTypeClosed EventType = "closed"
	// EventTypeDeleted indicates an issue was deleted.
	EventTypeDeleted EventType = "deleted"
)

// IssueEvent is emitted to clients that consume the SSE stream.
type IssueEvent struct {
	Type  EventType    `json:"type"`
	Issue IssueSummary `json:"issue"`
}

// EventSource provides a stream of issue events.
type EventSource interface {
	Subscribe(ctx context.Context) (<-chan IssueEvent, error)
}

// EventPublisher emits issue events to active subscribers.
type EventPublisher interface {
	Publish(evt IssueEvent)
}

// EventStreamOption configures the SSE handler.
type EventStreamOption func(*eventStreamConfig)

type eventStreamConfig struct {
	heartbeatInterval time.Duration
	now               func() time.Time
}

// WithHeartbeatInterval overrides the interval between heartbeat events.
func WithHeartbeatInterval(interval time.Duration) EventStreamOption {
	return func(cfg *eventStreamConfig) {
		cfg.heartbeatInterval = interval
	}
}

// WithNowFunc injects a custom clock, primarily for tests.
func WithNowFunc(now func() time.Time) EventStreamOption {
	return func(cfg *eventStreamConfig) {
		if now != nil {
			cfg.now = now
		}
	}
}

// NewEventStreamHandler returns an HTTP handler that serves Server-Sent Events.
func NewEventStreamHandler(source EventSource, opts ...EventStreamOption) http.Handler {
	cfg := eventStreamConfig{
		heartbeatInterval: 30 * time.Second,
		now:               time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if source == nil {
			WriteServiceUnavailable(w, "event stream unavailable", "Server-sent events require an active Beads daemon connection.")
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		events, err := source.Subscribe(ctx)
		if err != nil {
			WriteServiceUnavailable(w, "event stream unavailable", fmt.Sprintf("subscribe failed: %v", err))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		heartbeatInterval := cfg.heartbeatInterval
		var heartbeat <-chan time.Time
		if heartbeatInterval > 0 {
			ticker := time.NewTicker(heartbeatInterval)
			defer ticker.Stop()
			heartbeat = ticker.C
		}

		// Initial comment to confirm stream start.
		fmt.Fprintf(w, ": stream online %s\n\n", cfg.now().UTC().Format(time.RFC3339))
		flusher.Flush()

		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-events:
				if !ok {
					return
				}
				if err := writeSSEEvent(w, string(evt.Type), evt); err != nil {
					return
				}
				flusher.Flush()
			case <-heartbeat:
				if err := writeSSEEvent(w, "heartbeat", map[string]string{
					"at": cfg.now().UTC().Format(time.RFC3339),
				}); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

func writeSSEEvent(w http.ResponseWriter, event string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

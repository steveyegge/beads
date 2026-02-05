package eventbus

import "context"

// Handler processes events on the bus. Handlers are called in priority order
// (lower priority value = called earlier) for matching event types.
type Handler interface {
	// ID returns a unique identifier for this handler.
	ID() string

	// Handles returns the event types this handler processes.
	Handles() []EventType

	// Priority determines call order. Lower values are called first.
	Priority() int

	// Handle processes a single event and may modify the aggregated result.
	// Returning an error logs a warning but does not stop the handler chain.
	Handle(ctx context.Context, event *Event, result *Result) error
}

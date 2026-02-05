package eventbus

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

const (
	// StreamHookEvents is the JetStream stream for hook events.
	StreamHookEvents = "HOOK_EVENTS"

	// SubjectHookPrefix is the subject prefix for all hook events.
	SubjectHookPrefix = "hooks."
)

// SubjectForEvent returns the NATS subject for a given event type.
// Format: hooks.<event_type> (e.g., hooks.SessionStart, hooks.PreToolUse).
func SubjectForEvent(eventType EventType) string {
	return SubjectHookPrefix + string(eventType)
}

// EnsureStreams creates the required JetStream streams if they don't already
// exist. Called during daemon startup when NATS is enabled.
func EnsureStreams(js nats.JetStreamContext) error {
	_, err := js.StreamInfo(StreamHookEvents)
	if err == nil {
		return nil // Stream already exists.
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     StreamHookEvents,
		Subjects: []string{SubjectHookPrefix + ">"},
		Storage:  nats.FileStorage,
		// Retain last 10000 messages or 100MB, whichever comes first.
		MaxMsgs:  10000,
		MaxBytes: 100 << 20,
	})
	if err != nil {
		return fmt.Errorf("create %s stream: %w", StreamHookEvents, err)
	}

	return nil
}

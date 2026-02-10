package eventbus

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

const (
	// StreamHookEvents is the JetStream stream for hook events.
	StreamHookEvents = "HOOK_EVENTS"

	// StreamDecisionEvents is the JetStream stream for decision events (od-k3o.15.1).
	StreamDecisionEvents = "DECISION_EVENTS"

	// StreamOjEvents is the JetStream stream for OddJobs lifecycle events (bd-4q86.4).
	StreamOjEvents = "OJ_EVENTS"

	// SubjectHookPrefix is the subject prefix for all hook events.
	SubjectHookPrefix = "hooks."

	// SubjectDecisionPrefix is the subject prefix for decision events.
	SubjectDecisionPrefix = "decisions."

	// SubjectOjPrefix is the subject prefix for OddJobs events.
	SubjectOjPrefix = "oj."

	// StreamAgentEvents is the JetStream stream for agent lifecycle events (bd-e6vh).
	StreamAgentEvents = "AGENT_EVENTS"

	// SubjectAgentPrefix is the subject prefix for agent lifecycle events.
	SubjectAgentPrefix = "agents."
)

// SubjectForEvent returns the NATS subject for a given event type.
// Hook events use "hooks.<type>"; decision events use "decisions.<type>";
// OJ events use "oj.<type>".
func SubjectForEvent(eventType EventType) string {
	if eventType.IsDecisionEvent() {
		return SubjectDecisionPrefix + string(eventType)
	}
	if eventType.IsOjEvent() {
		return SubjectOjPrefix + string(eventType)
	}
	if eventType.IsAgentEvent() {
		return SubjectAgentPrefix + string(eventType)
	}
	return SubjectHookPrefix + string(eventType)
}

// EnsureStreams creates the required JetStream streams if they don't already
// exist. Called during daemon startup when NATS is enabled.
func EnsureStreams(js nats.JetStreamContext) error {
	// Hook events stream.
	if _, err := js.StreamInfo(StreamHookEvents); err != nil {
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
	}

	// Decision events stream (od-k3o.15.1).
	if _, err := js.StreamInfo(StreamDecisionEvents); err != nil {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     StreamDecisionEvents,
			Subjects: []string{SubjectDecisionPrefix + ">"},
			Storage:  nats.FileStorage,
			MaxMsgs:  10000,
			MaxBytes: 100 << 20,
		})
		if err != nil {
			return fmt.Errorf("create %s stream: %w", StreamDecisionEvents, err)
		}
	}

	// OddJobs lifecycle events stream (bd-4q86.4).
	if _, err := js.StreamInfo(StreamOjEvents); err != nil {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     StreamOjEvents,
			Subjects: []string{SubjectOjPrefix + ">"},
			Storage:  nats.FileStorage,
			MaxMsgs:  10000,
			MaxBytes: 100 << 20,
		})
		if err != nil {
			return fmt.Errorf("create %s stream: %w", StreamOjEvents, err)
		}
	}

	// Agent lifecycle events stream (bd-e6vh).
	if _, err := js.StreamInfo(StreamAgentEvents); err != nil {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     StreamAgentEvents,
			Subjects: []string{SubjectAgentPrefix + ">"},
			Storage:  nats.FileStorage,
			MaxMsgs:  10000,
			MaxBytes: 100 << 20,
		})
		if err != nil {
			return fmt.Errorf("create %s stream: %w", StreamAgentEvents, err)
		}
	}

	return nil
}

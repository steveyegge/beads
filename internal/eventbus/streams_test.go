package eventbus

import (
	"testing"
)

func TestSubjectForHookEvent(t *testing.T) {
	tests := []struct {
		eventType EventType
		want      string
	}{
		{EventSessionStart, "hooks.SessionStart"},
		{EventStop, "hooks.Stop"},
		{EventPreToolUse, "hooks.PreToolUse"},
		{EventNotification, "hooks.Notification"},
	}
	for _, tt := range tests {
		got := SubjectForEvent(tt.eventType)
		if got != tt.want {
			t.Errorf("SubjectForEvent(%s) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}

func TestSubjectForDecisionEvent(t *testing.T) {
	tests := []struct {
		eventType EventType
		want      string
	}{
		{EventDecisionCreated, "decisions.DecisionCreated"},
		{EventDecisionResponded, "decisions.DecisionResponded"},
		{EventDecisionEscalated, "decisions.DecisionEscalated"},
		{EventDecisionExpired, "decisions.DecisionExpired"},
	}
	for _, tt := range tests {
		got := SubjectForEvent(tt.eventType)
		if got != tt.want {
			t.Errorf("SubjectForEvent(%s) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}

func TestEnsureStreamsCreatesDecisionStream(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	// EnsureStreams was already called in startTestNATS. Verify both streams exist.
	info, err := js.StreamInfo(StreamHookEvents)
	if err != nil {
		t.Fatalf("StreamInfo(%s): %v", StreamHookEvents, err)
	}
	if info.Config.Name != StreamHookEvents {
		t.Errorf("expected stream name %q, got %q", StreamHookEvents, info.Config.Name)
	}

	info, err = js.StreamInfo(StreamDecisionEvents)
	if err != nil {
		t.Fatalf("StreamInfo(%s): %v", StreamDecisionEvents, err)
	}
	if info.Config.Name != StreamDecisionEvents {
		t.Errorf("expected stream name %q, got %q", StreamDecisionEvents, info.Config.Name)
	}
	// Verify the decision stream captures decision subjects.
	foundDecisionSubject := false
	for _, s := range info.Config.Subjects {
		if s == SubjectDecisionPrefix+">" {
			foundDecisionSubject = true
		}
	}
	if !foundDecisionSubject {
		t.Errorf("expected %q in stream subjects, got %v", SubjectDecisionPrefix+">", info.Config.Subjects)
	}
}

// TestEnsureStreamsIdempotent lives in bus_test.go (more thorough version).

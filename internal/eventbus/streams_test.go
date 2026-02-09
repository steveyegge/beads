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

func TestSubjectForOjEvent(t *testing.T) {
	tests := []struct {
		eventType EventType
		want      string
	}{
		{EventOjJobCreated, "oj.OjJobCreated"},
		{EventOjStepAdvanced, "oj.OjStepAdvanced"},
		{EventOjAgentSpawned, "oj.OjAgentSpawned"},
		{EventOjAgentIdle, "oj.OjAgentIdle"},
		{EventOjAgentEscalated, "oj.OjAgentEscalated"},
		{EventOjJobCompleted, "oj.OjJobCompleted"},
		{EventOjJobFailed, "oj.OjJobFailed"},
		{EventOjWorkerPollComplete, "oj.OjWorkerPollComplete"},
	}
	for _, tt := range tests {
		got := SubjectForEvent(tt.eventType)
		if got != tt.want {
			t.Errorf("SubjectForEvent(%s) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}

func TestEnsureStreamsCreatesOjStream(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	info, err := js.StreamInfo(StreamOjEvents)
	if err != nil {
		t.Fatalf("StreamInfo(%s): %v", StreamOjEvents, err)
	}
	if info.Config.Name != StreamOjEvents {
		t.Errorf("expected stream name %q, got %q", StreamOjEvents, info.Config.Name)
	}
	foundOjSubject := false
	for _, s := range info.Config.Subjects {
		if s == SubjectOjPrefix+">" {
			foundOjSubject = true
		}
	}
	if !foundOjSubject {
		t.Errorf("expected %q in stream subjects, got %v", SubjectOjPrefix+">", info.Config.Subjects)
	}
}

func TestIsOjEvent(t *testing.T) {
	ojEvents := []EventType{
		EventOjJobCreated, EventOjStepAdvanced, EventOjAgentSpawned,
		EventOjAgentIdle, EventOjAgentEscalated, EventOjJobCompleted,
		EventOjJobFailed, EventOjWorkerPollComplete,
	}
	for _, e := range ojEvents {
		if !e.IsOjEvent() {
			t.Errorf("expected %s.IsOjEvent() = true", e)
		}
		if e.IsDecisionEvent() {
			t.Errorf("expected %s.IsDecisionEvent() = false", e)
		}
	}

	// Non-OJ events should return false.
	nonOj := []EventType{EventSessionStart, EventStop, EventDecisionCreated}
	for _, e := range nonOj {
		if e.IsOjEvent() {
			t.Errorf("expected %s.IsOjEvent() = false", e)
		}
	}
}

// TestEnsureStreamsIdempotent lives in bus_test.go (more thorough version).

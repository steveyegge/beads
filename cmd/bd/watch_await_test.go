package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/rpc"
)

func TestWatchMutationSubject(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{rpc.MutationCreate, string(eventbus.EventMutationCreate)},
		{rpc.MutationUpdate, string(eventbus.EventMutationUpdate)},
		{rpc.MutationDelete, string(eventbus.EventMutationDelete)},
		{rpc.MutationComment, string(eventbus.EventMutationComment)},
		{rpc.MutationStatus, string(eventbus.EventMutationStatus)},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := watchMutationSubject(tt.input)
			if got != tt.want {
				t.Errorf("watchMutationSubject(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPayloadToMutationEvent(t *testing.T) {
	payload := eventbus.MutationEventPayload{
		Type:      "status",
		IssueID:   "bd-123",
		Title:     "Deploy Gate",
		Assignee:  "alice",
		Actor:     "bob",
		Timestamp: "2025-06-15T10:30:00.123456789Z",
		OldStatus: "open",
		NewStatus: "closed",
		ParentID:  "bd-epic",
		IssueType: "gate",
		Labels:    []string{"p0"},
		AwaitType: "decision",
	}

	evt := payloadToMutationEvent(payload)

	if evt.Type != "status" {
		t.Errorf("Type = %q, want %q", evt.Type, "status")
	}
	if evt.IssueID != "bd-123" {
		t.Errorf("IssueID = %q, want %q", evt.IssueID, "bd-123")
	}
	if evt.Title != "Deploy Gate" {
		t.Errorf("Title = %q, want %q", evt.Title, "Deploy Gate")
	}
	if evt.Assignee != "alice" {
		t.Errorf("Assignee = %q, want %q", evt.Assignee, "alice")
	}
	if evt.Actor != "bob" {
		t.Errorf("Actor = %q, want %q", evt.Actor, "bob")
	}
	if evt.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	if evt.Timestamp.Year() != 2025 || evt.Timestamp.Month() != 6 {
		t.Errorf("Timestamp parsed incorrectly: %v", evt.Timestamp)
	}
	if evt.OldStatus != "open" {
		t.Errorf("OldStatus = %q, want %q", evt.OldStatus, "open")
	}
	if evt.NewStatus != "closed" {
		t.Errorf("NewStatus = %q, want %q", evt.NewStatus, "closed")
	}
	if evt.ParentID != "bd-epic" {
		t.Errorf("ParentID = %q, want %q", evt.ParentID, "bd-epic")
	}
	if evt.IssueType != "gate" {
		t.Errorf("IssueType = %q, want %q", evt.IssueType, "gate")
	}
	if len(evt.Labels) != 1 || evt.Labels[0] != "p0" {
		t.Errorf("Labels = %v, want [p0]", evt.Labels)
	}
	if evt.AwaitType != "decision" {
		t.Errorf("AwaitType = %q, want %q", evt.AwaitType, "decision")
	}
}

func TestPayloadToMutationEvent_BadTimestamp(t *testing.T) {
	payload := eventbus.MutationEventPayload{
		Type:      "create",
		IssueID:   "bd-1",
		Timestamp: "garbage",
	}
	evt := payloadToMutationEvent(payload)
	if !evt.Timestamp.IsZero() {
		t.Errorf("expected zero time for unparseable timestamp, got %v", evt.Timestamp)
	}
	// Other fields should still be set
	if evt.Type != "create" {
		t.Errorf("Type = %q, want %q", evt.Type, "create")
	}
}

func TestPayloadToMutationEvent_EmptyPayload(t *testing.T) {
	payload := eventbus.MutationEventPayload{}
	evt := payloadToMutationEvent(payload)
	if evt.Type != "" {
		t.Errorf("Type = %q, want empty", evt.Type)
	}
	if !evt.Timestamp.IsZero() {
		t.Errorf("expected zero time for empty timestamp")
	}
}

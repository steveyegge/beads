package main

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestRenderFeedEvents_NoPanic(t *testing.T) {
	comment := "Fixed the bug"
	newVal := "Bug title"
	oldStatus := `{"status":"open"}`
	newStatus := `{"status":"in_progress"}`
	depVal := "test-002"

	events := []*types.Event{
		{ID: 1, IssueID: "test-001", EventType: types.EventCreated, Actor: "alice", NewValue: &newVal, CreatedAt: time.Now()},
		{ID: 2, IssueID: "test-001", EventType: types.EventStatusChanged, Actor: "bob", OldValue: &oldStatus, NewValue: &newStatus, CreatedAt: time.Now()},
		{ID: 3, IssueID: "test-001", EventType: types.EventClosed, Actor: "alice", Comment: &comment, CreatedAt: time.Now()},
		{ID: 4, IssueID: "test-001", EventType: types.EventCommented, Actor: "bob", Comment: &comment, CreatedAt: time.Now()},
		{ID: 5, IssueID: "test-001", EventType: types.EventReopened, Actor: "alice", CreatedAt: time.Now()},
		{ID: 6, IssueID: "test-001", EventType: types.EventDependencyAdded, Actor: "alice", NewValue: &depVal, CreatedAt: time.Now()},
		{ID: 7, IssueID: "test-001", EventType: types.EventDependencyRemoved, Actor: "alice", OldValue: &depVal, CreatedAt: time.Now()},
		{ID: 8, IssueID: "test-001", EventType: types.EventLabelAdded, Actor: "alice", NewValue: &depVal, CreatedAt: time.Now()},
		{ID: 9, IssueID: "test-001", EventType: types.EventLabelRemoved, Actor: "alice", OldValue: &depVal, CreatedAt: time.Now()},
		{ID: 10, IssueID: "test-001", EventType: types.EventUpdated, Actor: "alice", CreatedAt: time.Now()},
	}

	// Just verify no panic — renderFeedEvents writes to stdout
	renderFeedEvents(events)
}

func TestExtractJSONField(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		field    string
		expected string
	}{
		{"valid JSON", `{"status":"open","priority":1}`, "status", "open"},
		{"missing field", `{"status":"open"}`, "priority", ""},
		{"not JSON", "plain string", "status", "plain string"},
		{"empty JSON", "{}", "status", ""},
		{"nested value", `{"status":"closed","reason":"done"}`, "reason", "done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONField(tt.jsonStr, tt.field)
			if got != tt.expected {
				t.Errorf("extractJSONField(%q, %q) = %q, want %q", tt.jsonStr, tt.field, got, tt.expected)
			}
		})
	}
}

func TestFeedTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a very long string", 10, "this is..."},
		{"exact len!", 10, "exact len!"},
		{"multi\nline\ntext", 20, "multi line text"},
	}
	for _, tt := range tests {
		got := feedTruncate(tt.input, tt.maxLen)
		if got != tt.expected {
			t.Errorf("feedTruncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
		}
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"1w", 7 * 24 * time.Hour, false},
		{"30s", 30 * time.Second, false},
		{"2d", 2 * 24 * time.Hour, false},
		{"invalid", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

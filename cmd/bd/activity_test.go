package main

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
)

// TestParseDurationString tests the duration parsing function
func TestParseDurationString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  time.Duration
		expectErr bool
	}{
		// Standard Go duration formats
		{"5 minutes", "5m", 5 * time.Minute, false},
		{"1 hour", "1h", time.Hour, false},
		{"30 seconds", "30s", 30 * time.Second, false},
		{"2h30m", "2h30m", 2*time.Hour + 30*time.Minute, false},

		// Custom day format
		{"2 days", "2d", 2 * 24 * time.Hour, false},
		{"1 day", "1d", 24 * time.Hour, false},
		{"7 days", "7d", 7 * 24 * time.Hour, false},

		// Case insensitivity for custom formats
		{"uppercase D", "3D", 3 * 24 * time.Hour, false},
		{"uppercase H", "2H", 2 * time.Hour, false},
		{"uppercase M", "15M", 15 * time.Minute, false},
		{"uppercase S", "45S", 45 * time.Second, false},

		// Invalid formats
		{"empty string", "", 0, true},
		{"invalid unit", "5x", 0, true},
		{"no number", "m", 0, true},
		{"text only", "five minutes", 0, true},

		// Note: negative durations are actually valid in Go's time.ParseDuration
		// so we test it works rather than expecting an error
		{"negative duration", "-5m", -5 * time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDurationString(tt.input)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("for input %q: expected %v, got %v", tt.input, tt.expected, result)
				}
			}
		})
	}
}

// TestFilterEvents tests event filtering by --mol and --type
func TestFilterEvents(t *testing.T) {
	// Create test events
	events := []rpc.MutationEvent{
		{Type: rpc.MutationCreate, IssueID: "bd-abc123", Timestamp: time.Now()},
		{Type: rpc.MutationUpdate, IssueID: "bd-abc456", Timestamp: time.Now()},
		{Type: rpc.MutationDelete, IssueID: "bd-xyz789", Timestamp: time.Now()},
		{Type: rpc.MutationStatus, IssueID: "bd-abc789", Timestamp: time.Now()},
		{Type: rpc.MutationComment, IssueID: "bd-def123", Timestamp: time.Now()},
	}

	// Reset global filter vars before each test
	defer func() {
		activityMol = ""
		activityType = ""
	}()

	t.Run("no filters returns all events", func(t *testing.T) {
		activityMol = ""
		activityType = ""
		result := filterEvents(events)
		if len(result) != len(events) {
			t.Errorf("expected %d events, got %d", len(events), len(result))
		}
	})

	t.Run("filter by mol prefix", func(t *testing.T) {
		activityMol = "bd-abc"
		activityType = ""
		result := filterEvents(events)
		// Should match: bd-abc123, bd-abc456, bd-abc789
		if len(result) != 3 {
			t.Errorf("expected 3 events matching bd-abc*, got %d", len(result))
		}
		for _, e := range result {
			if e.IssueID[:6] != "bd-abc" {
				t.Errorf("unexpected event with ID %s", e.IssueID)
			}
		}
	})

	t.Run("filter by event type", func(t *testing.T) {
		activityMol = ""
		activityType = rpc.MutationCreate
		result := filterEvents(events)
		if len(result) != 1 {
			t.Errorf("expected 1 create event, got %d", len(result))
		}
		if len(result) > 0 && result[0].Type != rpc.MutationCreate {
			t.Errorf("expected create event, got %s", result[0].Type)
		}
	})

	t.Run("filter by both mol and type", func(t *testing.T) {
		activityMol = "bd-abc"
		activityType = rpc.MutationUpdate
		result := filterEvents(events)
		// Should match: only bd-abc456 (update)
		if len(result) != 1 {
			t.Errorf("expected 1 event, got %d", len(result))
		}
		if len(result) > 0 {
			if result[0].IssueID != "bd-abc456" {
				t.Errorf("expected bd-abc456, got %s", result[0].IssueID)
			}
			if result[0].Type != rpc.MutationUpdate {
				t.Errorf("expected update type, got %s", result[0].Type)
			}
		}
	})

	t.Run("filter returns empty for no matches", func(t *testing.T) {
		activityMol = "bd-nomatch"
		activityType = ""
		result := filterEvents(events)
		if len(result) != 0 {
			t.Errorf("expected 0 events, got %d", len(result))
		}
	})
}

// TestGetEventDisplay tests the symbol and message generation for all event types
func TestGetEventDisplay(t *testing.T) {
	tests := []struct {
		name           string
		event          rpc.MutationEvent
		expectedSymbol string
		checkMessage   func(string) bool
	}{
		{
			name:           "create event",
			event:          rpc.MutationEvent{Type: rpc.MutationCreate, IssueID: "bd-123"},
			expectedSymbol: "+",
			checkMessage:   func(m string) bool { return m == "bd-123 created" },
		},
		{
			name:           "update event",
			event:          rpc.MutationEvent{Type: rpc.MutationUpdate, IssueID: "bd-456"},
			expectedSymbol: "\u2192", // →
			checkMessage:   func(m string) bool { return m == "bd-456 updated" },
		},
		{
			name:           "delete event",
			event:          rpc.MutationEvent{Type: rpc.MutationDelete, IssueID: "bd-789"},
			expectedSymbol: "\u2298", // ⊘
			checkMessage:   func(m string) bool { return m == "bd-789 deleted" },
		},
		{
			name:           "comment event",
			event:          rpc.MutationEvent{Type: rpc.MutationComment, IssueID: "bd-abc"},
			expectedSymbol: "\U0001F4AC", // 💬
			checkMessage:   func(m string) bool { return m == "bd-abc comment" },
		},
		{
			name: "bonded event with step count",
			event: rpc.MutationEvent{
				Type:      rpc.MutationBonded,
				IssueID:   "bd-mol",
				StepCount: 5,
			},
			expectedSymbol: "+",
			checkMessage:   func(m string) bool { return m == "bd-mol bonded (5 steps)" },
		},
		{
			name:           "bonded event without step count",
			event:          rpc.MutationEvent{Type: rpc.MutationBonded, IssueID: "bd-mol2"},
			expectedSymbol: "+",
			checkMessage:   func(m string) bool { return m == "bd-mol2 bonded" },
		},
		{
			name:           "squashed event",
			event:          rpc.MutationEvent{Type: rpc.MutationSquashed, IssueID: "bd-wisp"},
			expectedSymbol: "\u25C9", // ◉
			checkMessage:   func(m string) bool { return m == "bd-wisp SQUASHED" },
		},
		{
			name:           "burned event",
			event:          rpc.MutationEvent{Type: rpc.MutationBurned, IssueID: "bd-burn"},
			expectedSymbol: "\U0001F525", // 🔥
			checkMessage:   func(m string) bool { return m == "bd-burn burned" },
		},
		{
			name: "status event - in_progress",
			event: rpc.MutationEvent{
				Type:      rpc.MutationStatus,
				IssueID:   "bd-wip",
				OldStatus: "open",
				NewStatus: "in_progress",
			},
			expectedSymbol: "\u2192", // →
			checkMessage:   func(m string) bool { return m == "bd-wip started" },
		},
		{
			name: "status event - closed",
			event: rpc.MutationEvent{
				Type:      rpc.MutationStatus,
				IssueID:   "bd-done",
				OldStatus: "in_progress",
				NewStatus: "closed",
			},
			expectedSymbol: "\u2713", // ✓
			checkMessage:   func(m string) bool { return m == "bd-done completed" },
		},
		{
			name: "status event - reopened",
			event: rpc.MutationEvent{
				Type:      rpc.MutationStatus,
				IssueID:   "bd-reopen",
				OldStatus: "closed",
				NewStatus: "open",
			},
			expectedSymbol: "\u21BA", // ↺
			checkMessage:   func(m string) bool { return m == "bd-reopen reopened" },
		},
		{
			name:           "unknown event type",
			event:          rpc.MutationEvent{Type: "custom", IssueID: "bd-custom"},
			expectedSymbol: "\u2022", // •
			checkMessage:   func(m string) bool { return m == "bd-custom custom" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, message := getEventDisplay(tt.event)

			if symbol != tt.expectedSymbol {
				t.Errorf("expected symbol %q, got %q", tt.expectedSymbol, symbol)
			}

			if !tt.checkMessage(message) {
				t.Errorf("unexpected message: %q", message)
			}
		})
	}
}

// TestFormatEvent tests the ActivityEvent formatting
func TestFormatEvent(t *testing.T) {
	now := time.Now()
	event := rpc.MutationEvent{
		Type:      rpc.MutationStatus,
		IssueID:   "bd-test",
		Timestamp: now,
		OldStatus: "open",
		NewStatus: "in_progress",
		ParentID:  "bd-parent",
		StepCount: 3,
	}

	result := formatEvent(event)

	if result.Timestamp != now {
		t.Errorf("expected timestamp %v, got %v", now, result.Timestamp)
	}
	if result.Type != rpc.MutationStatus {
		t.Errorf("expected type %s, got %s", rpc.MutationStatus, result.Type)
	}
	if result.IssueID != "bd-test" {
		t.Errorf("expected issue ID bd-test, got %s", result.IssueID)
	}
	if result.OldStatus != "open" {
		t.Errorf("expected OldStatus 'open', got %s", result.OldStatus)
	}
	if result.NewStatus != "in_progress" {
		t.Errorf("expected NewStatus 'in_progress', got %s", result.NewStatus)
	}
	if result.ParentID != "bd-parent" {
		t.Errorf("expected ParentID 'bd-parent', got %s", result.ParentID)
	}
	if result.StepCount != 3 {
		t.Errorf("expected StepCount 3, got %d", result.StepCount)
	}
	if result.Symbol == "" {
		t.Error("expected non-empty symbol")
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

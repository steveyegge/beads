package decision

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestSortDecisionsByUrgency tests that decisions are sorted by urgency then time.
func TestSortDecisionsByUrgency(t *testing.T) {
	m := New()
	m.filter = "all"

	now := time.Now()
	decisions := []DecisionItem{
		{ID: "1", Urgency: "low", RequestedAt: now.Add(-1 * time.Hour)},
		{ID: "2", Urgency: "high", RequestedAt: now.Add(-2 * time.Hour)},
		{ID: "3", Urgency: "medium", RequestedAt: now},
		{ID: "4", Urgency: "high", RequestedAt: now}, // newer high
		{ID: "5", Urgency: "low", RequestedAt: now},  // newer low
	}

	sorted := m.filterDecisions(decisions)

	// Expected order: high (newer first), medium, low (newer first)
	expectedIDs := []string{"4", "2", "3", "5", "1"}

	if len(sorted) != len(expectedIDs) {
		t.Fatalf("Expected %d decisions, got %d", len(expectedIDs), len(sorted))
	}

	for i, expected := range expectedIDs {
		if sorted[i].ID != expected {
			t.Errorf("Position %d: expected ID '%s', got '%s'", i, expected, sorted[i].ID)
		}
	}
}

// TestGetSessionName tests converting RequestedBy to tmux session name.
func TestGetSessionName(t *testing.T) {
	tests := []struct {
		name        string
		requestedBy string
		wantSession string
		wantErr     bool
	}{
		{
			name:        "crew path",
			requestedBy: "gastown/crew/decision",
			wantSession: "bd-gastown-crew-decision",
			wantErr:     false,
		},
		{
			name:        "rig path",
			requestedBy: "myrig/polecats/alpha",
			wantSession: "bd-myrig-polecats-alpha",
			wantErr:     false,
		},
		{
			name:        "overseer",
			requestedBy: "overseer",
			wantSession: "",
			wantErr:     true,
		},
		{
			name:        "human",
			requestedBy: "human",
			wantSession: "",
			wantErr:     true,
		},
		{
			name:        "empty",
			requestedBy: "",
			wantSession: "",
			wantErr:     true,
		},
		{
			name:        "single part",
			requestedBy: "something",
			wantSession: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := getSessionName(tt.requestedBy)
			if (err != nil) != tt.wantErr {
				t.Errorf("getSessionName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if session != tt.wantSession {
				t.Errorf("getSessionName() = %q, want %q", session, tt.wantSession)
			}
		})
	}
}

// TestNewModel tests the model constructor.
func TestNewModel(t *testing.T) {
	m := New()

	if m == nil {
		t.Fatal("New() returned nil")
	}

	if m.filter != "all" {
		t.Errorf("default filter = %q, want %q", m.filter, "all")
	}

	if m.inputMode != ModeNormal {
		t.Errorf("default inputMode = %v, want ModeNormal", m.inputMode)
	}

	if m.selected != 0 {
		t.Errorf("default selected = %d, want 0", m.selected)
	}

	if m.selectedOption != 0 {
		t.Errorf("default selectedOption = %d, want 0", m.selectedOption)
	}

	if m.showHelp {
		t.Error("default showHelp should be false")
	}

	if m.peeking {
		t.Error("default peeking should be false")
	}
}

// TestSetFilter tests the SetFilter method.
func TestSetFilter(t *testing.T) {
	m := New()

	tests := []string{"all", "high", "medium", "low"}
	for _, filter := range tests {
		m.SetFilter(filter)
		if m.filter != filter {
			t.Errorf("SetFilter(%q): filter = %q, want %q", filter, m.filter, filter)
		}
	}
}

// TestSetNotify tests the SetNotify method.
func TestSetNotify(t *testing.T) {
	m := New()

	if m.notify {
		t.Error("default notify should be false")
	}

	m.SetNotify(true)
	if !m.notify {
		t.Error("SetNotify(true): notify should be true")
	}

	m.SetNotify(false)
	if m.notify {
		t.Error("SetNotify(false): notify should be false")
	}
}

// TestFilterDecisions tests the filter functionality.
func TestFilterDecisions(t *testing.T) {
	m := New()
	now := time.Now()

	decisions := []DecisionItem{
		{ID: "1", Urgency: "high", RequestedAt: now.Add(-1 * time.Hour)},
		{ID: "2", Urgency: "medium", RequestedAt: now},
		{ID: "3", Urgency: "low", RequestedAt: now.Add(-2 * time.Hour)},
	}

	t.Run("filter all", func(t *testing.T) {
		m.SetFilter("all")
		result := m.filterDecisions(decisions)
		if len(result) != 3 {
			t.Errorf("filter 'all': got %d decisions, want 3", len(result))
		}
	})

	t.Run("filter high", func(t *testing.T) {
		m.SetFilter("high")
		result := m.filterDecisions(decisions)
		if len(result) != 1 {
			t.Errorf("filter 'high': got %d decisions, want 1", len(result))
		}
		if result[0].ID != "1" {
			t.Errorf("filter 'high': got ID %s, want '1'", result[0].ID)
		}
	})

	t.Run("filter medium", func(t *testing.T) {
		m.SetFilter("medium")
		result := m.filterDecisions(decisions)
		if len(result) != 1 {
			t.Errorf("filter 'medium': got %d decisions, want 1", len(result))
		}
		if result[0].ID != "2" {
			t.Errorf("filter 'medium': got ID %s, want '2'", result[0].ID)
		}
	})

	t.Run("filter low", func(t *testing.T) {
		m.SetFilter("low")
		result := m.filterDecisions(decisions)
		if len(result) != 1 {
			t.Errorf("filter 'low': got %d decisions, want 1", len(result))
		}
		if result[0].ID != "3" {
			t.Errorf("filter 'low': got ID %s, want '3'", result[0].ID)
		}
	})
}

// TestInputModeConstants tests that input mode constants are distinct.
func TestInputModeConstants(t *testing.T) {
	if ModeNormal == ModeRationale {
		t.Error("ModeNormal should not equal ModeRationale")
	}
	if ModeNormal == ModeText {
		t.Error("ModeNormal should not equal ModeText")
	}
	if ModeRationale == ModeText {
		t.Error("ModeRationale should not equal ModeText")
	}
}

// TestDecisionItemWithBeadsTypes tests that DecisionItem works with beads types.
func TestDecisionItemWithBeadsTypes(t *testing.T) {
	now := time.Now()

	d := DecisionItem{
		ID:     "bd-test-123",
		Prompt: "Which approach?",
		Options: []types.DecisionOption{
			{ID: "a", Label: "Fast approach", Description: "Quick but risky"},
			{ID: "b", Label: "Safe approach", Description: "Slower but reliable"},
		},
		Urgency:     "high",
		RequestedBy: "myrig/crew/agent1",
		RequestedAt: now,
		Context:     `{"key": "value"}`,
	}

	if d.ID != "bd-test-123" {
		t.Errorf("ID = %q, want %q", d.ID, "bd-test-123")
	}
	if len(d.Options) != 2 {
		t.Fatalf("len(Options) = %d, want 2", len(d.Options))
	}
	if d.Options[0].ID != "a" {
		t.Errorf("Options[0].ID = %q, want %q", d.Options[0].ID, "a")
	}
	if d.Options[1].Label != "Safe approach" {
		t.Errorf("Options[1].Label = %q, want %q", d.Options[1].Label, "Safe approach")
	}
}

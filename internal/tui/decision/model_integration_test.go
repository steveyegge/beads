package decision

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/steveyegge/beads/internal/types"
)

// Integration tests for the Decision TUI model.
// These tests verify the model's behavior with realistic message sequences.

// TestModelUpdateWithFetchDecisionsMsg tests the model's response to fetched decisions.
func TestModelUpdateWithFetchDecisionsMsg(t *testing.T) {
	m := New()

	now := time.Now()
	decisions := []DecisionItem{
		{ID: "bd-1", Prompt: "First decision", Urgency: "high", RequestedAt: now},
		{ID: "bd-2", Prompt: "Second decision", Urgency: "medium", RequestedAt: now.Add(-1 * time.Hour)},
	}

	msg := fetchDecisionsMsg{decisions: decisions}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if len(model.decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(model.decisions))
	}

	// Should be sorted by urgency (high first)
	if model.decisions[0].ID != "bd-1" {
		t.Errorf("expected first decision to be bd-1, got %s", model.decisions[0].ID)
	}

	if model.err != nil {
		t.Errorf("expected no error, got %v", model.err)
	}
}

// TestModelUpdateWithFetchError tests the model's response to fetch errors.
func TestModelUpdateWithFetchError(t *testing.T) {
	m := New()

	msg := fetchDecisionsMsg{err: errTestFetch}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if model.err == nil {
		t.Error("expected error to be set")
	}
}

var errTestFetch = errors.New("test fetch error")

// TestModelUpdateWithResolvedMsg tests the model's response to resolved decisions.
func TestModelUpdateWithResolvedMsg(t *testing.T) {
	m := New()
	m.selectedOption = 2
	m.rationale = "test rationale"

	t.Run("successful resolution", func(t *testing.T) {
		msg := resolvedMsg{id: "bd-123"}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.selectedOption != 0 {
			t.Errorf("expected selectedOption to be reset to 0, got %d", model.selectedOption)
		}
		if model.rationale != "" {
			t.Errorf("expected rationale to be cleared, got %q", model.rationale)
		}
		if model.err != nil {
			t.Errorf("expected no error, got %v", model.err)
		}
	})
}

// TestModelUpdateWithDismissedMsg tests the model's response to dismissed decisions.
func TestModelUpdateWithDismissedMsg(t *testing.T) {
	m := New()
	m.selectedOption = 1

	t.Run("successful dismissal", func(t *testing.T) {
		msg := dismissedMsg{id: "bd-123"}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.selectedOption != 0 {
			t.Errorf("expected selectedOption to be reset to 0, got %d", model.selectedOption)
		}
		if model.err != nil {
			t.Errorf("expected no error, got %v", model.err)
		}
	})
}

// TestModelUpdateWithPeekMsg tests the model's response to peek messages.
func TestModelUpdateWithPeekMsg(t *testing.T) {
	m := New()
	m.width = 80
	m.height = 24

	t.Run("successful peek", func(t *testing.T) {
		msg := peekMsg{sessionName: "bd-test-session", content: "Terminal content here"}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if !model.peeking {
			t.Error("expected peeking to be true")
		}
		if model.peekSessionName != "bd-test-session" {
			t.Errorf("expected peekSessionName to be 'bd-test-session', got %q", model.peekSessionName)
		}
		if model.peekContent != "Terminal content here" {
			t.Errorf("expected peekContent to be set, got %q", model.peekContent)
		}
	})

	t.Run("peek error", func(t *testing.T) {
		m2 := New()
		msg := peekMsg{sessionName: "bd-test", err: errTestPeek}
		updated, _ := m2.Update(msg)
		model := updated.(*Model)

		if model.peeking {
			t.Error("expected peeking to be false on error")
		}
	})
}

var errTestPeek = errors.New("test peek error")

// TestModelUpdateWithWindowSizeMsg tests window resize handling.
func TestModelUpdateWithWindowSizeMsg(t *testing.T) {
	m := New()

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if model.width != 120 {
		t.Errorf("expected width to be 120, got %d", model.width)
	}
	if model.height != 40 {
		t.Errorf("expected height to be 40, got %d", model.height)
	}
}

// TestModelKeyNavigation tests keyboard navigation.
func TestModelKeyNavigation(t *testing.T) {
	m := New()
	now := time.Now()
	m.decisions = []DecisionItem{
		{ID: "1", Urgency: "high", RequestedAt: now},
		{ID: "2", Urgency: "high", RequestedAt: now.Add(-1 * time.Hour)},
		{ID: "3", Urgency: "medium", RequestedAt: now},
	}

	t.Run("navigate down", func(t *testing.T) {
		m.selected = 0
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.selected != 1 {
			t.Errorf("expected selected to be 1, got %d", model.selected)
		}
	})

	t.Run("navigate up", func(t *testing.T) {
		m.selected = 1
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.selected != 0 {
			t.Errorf("expected selected to be 0, got %d", model.selected)
		}
	})

	t.Run("navigate up at top stays at top", func(t *testing.T) {
		m.selected = 0
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.selected != 0 {
			t.Errorf("expected selected to stay at 0, got %d", model.selected)
		}
	})

	t.Run("navigate down at bottom stays at bottom", func(t *testing.T) {
		m.selected = 2
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.selected != 2 {
			t.Errorf("expected selected to stay at 2, got %d", model.selected)
		}
	})
}

// TestModelOptionSelection tests option selection keys.
func TestModelOptionSelection(t *testing.T) {
	m := New()
	m.decisions = []DecisionItem{
		{
			ID: "1",
			Options: []types.DecisionOption{
				{ID: "a", Label: "A"},
				{ID: "b", Label: "B"},
				{ID: "c", Label: "C"},
				{ID: "d", Label: "D"},
			},
		},
	}

	tests := []struct {
		key      rune
		expected int
	}{
		{'1', 1},
		{'2', 2},
		{'3', 3},
		{'4', 4},
	}

	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			m.selectedOption = 0
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}}
			updated, _ := m.Update(msg)
			model := updated.(*Model)

			if model.selectedOption != tt.expected {
				t.Errorf("key %c: expected selectedOption to be %d, got %d", tt.key, tt.expected, model.selectedOption)
			}
		})
	}
}

// TestModelHelpToggle tests help display toggling.
func TestModelHelpToggle(t *testing.T) {
	m := New()

	if m.showHelp {
		t.Error("help should be hidden by default")
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if !model.showHelp {
		t.Error("expected help to be shown after '?' press")
	}

	updated, _ = model.Update(msg)
	model = updated.(*Model)

	if model.showHelp {
		t.Error("expected help to be hidden after second '?' press")
	}
}

// TestModelFilterKeys tests filter key bindings.
func TestModelFilterKeys(t *testing.T) {
	m := New()

	t.Run("filter to high with !", func(t *testing.T) {
		m.filter = "all"
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.filter != "high" {
			t.Errorf("expected filter to be 'high', got %q", model.filter)
		}
	})

	t.Run("filter to all with a", func(t *testing.T) {
		m.filter = "high"
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.filter != "all" {
			t.Errorf("expected filter to be 'all', got %q", model.filter)
		}
	})
}

// TestModelPeekModeNavigation tests navigation while in peek mode.
func TestModelPeekModeNavigation(t *testing.T) {
	m := New()
	m.peeking = true
	m.peekContent = "Some content"
	m.peekSessionName = "test-session"
	m.width = 80
	m.height = 24

	t.Run("escape peek mode", func(t *testing.T) {
		msg := tea.KeyMsg{Type: tea.KeyEsc}
		updated, _ := m.Update(msg)
		model := updated.(*Model)

		if model.peeking {
			t.Error("expected peeking to be false after Esc")
		}
		if model.peekContent != "" {
			t.Error("expected peekContent to be cleared")
		}
	})
}

// TestModelSelectionBoundsAfterFetch tests that selection stays valid after fetch.
func TestModelSelectionBoundsAfterFetch(t *testing.T) {
	m := New()
	now := time.Now()

	m.decisions = []DecisionItem{
		{ID: "1", Urgency: "high", RequestedAt: now},
		{ID: "2", Urgency: "high", RequestedAt: now.Add(-1 * time.Hour)},
		{ID: "3", Urgency: "medium", RequestedAt: now},
	}
	m.selected = 2

	msg := fetchDecisionsMsg{decisions: []DecisionItem{
		{ID: "1", Urgency: "high", RequestedAt: now},
	}}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if model.selected >= len(model.decisions) {
		t.Errorf("selected (%d) should be less than decisions length (%d)",
			model.selected, len(model.decisions))
	}
	if model.selected != 0 {
		t.Errorf("expected selected to be adjusted to 0, got %d", model.selected)
	}
}

// TestModelRationaleMode tests entering and exiting rationale mode.
func TestModelRationaleMode(t *testing.T) {
	m := New()
	m.selectedOption = 1

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if model.inputMode != ModeRationale {
		t.Errorf("expected inputMode to be ModeRationale, got %v", model.inputMode)
	}

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	updated, _ = model.Update(msg)
	model = updated.(*Model)

	if model.inputMode != ModeNormal {
		t.Errorf("expected inputMode to be ModeNormal after Esc, got %v", model.inputMode)
	}
}

// TestModelTextMode tests entering and exiting text mode.
func TestModelTextMode(t *testing.T) {
	m := New()

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if model.inputMode != ModeText {
		t.Errorf("expected inputMode to be ModeText, got %v", model.inputMode)
	}

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	updated, _ = model.Update(msg)
	model = updated.(*Model)

	if model.inputMode != ModeNormal {
		t.Errorf("expected inputMode to be ModeNormal after Esc, got %v", model.inputMode)
	}
}

// TestModelEmptyDecisionsList tests behavior with no decisions.
func TestModelEmptyDecisionsList(t *testing.T) {
	m := New()

	msg := fetchDecisionsMsg{decisions: []DecisionItem{}}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if len(model.decisions) != 0 {
		t.Errorf("expected 0 decisions, got %d", len(model.decisions))
	}

	// Navigation should not panic with empty list
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	_, _ = model.Update(keyMsg)

	keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	_, _ = model.Update(keyMsg)

	keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}}
	_, _ = model.Update(keyMsg)
}

// TestModelViewRendering tests that View() doesn't panic.
func TestModelViewRendering(t *testing.T) {
	m := New()
	m.width = 80
	m.height = 24

	// Test with no decisions
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}

	// Test with decisions
	m.decisions = []DecisionItem{
		{
			ID:      "1",
			Prompt:  "Test decision",
			Options: []types.DecisionOption{{ID: "a", Label: "A", Description: "Option A"}},
			Urgency: "high",
		},
	}
	view = m.View()
	if view == "" {
		t.Error("View() with decisions returned empty string")
	}

	// Test in peek mode
	m.peeking = true
	m.peekContent = "Terminal output"
	m.peekSessionName = "test-session"
	view = m.View()
	if view == "" {
		t.Error("View() in peek mode returned empty string")
	}
}

// TestDecisionWithPredecessor tests handling decisions with predecessor IDs.
func TestDecisionWithPredecessor(t *testing.T) {
	m := New()
	m.width = 80
	m.height = 24
	now := time.Now()

	m.decisions = []DecisionItem{
		{
			ID:            "bd-child",
			Prompt:        "Follow-up decision",
			Options:       []types.DecisionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
			Urgency:       "medium",
			RequestedAt:   now,
			PredecessorID: "bd-parent-123",
		},
	}

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string for chained decision")
	}
}

// TestDecisionWithJSONContext tests handling decisions with JSON context.
func TestDecisionWithJSONContext(t *testing.T) {
	m := New()
	m.width = 80
	m.height = 24
	now := time.Now()

	m.decisions = []DecisionItem{
		{
			ID:          "bd-json-ctx",
			Prompt:      "Decision with JSON context",
			Options:     []types.DecisionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
			Urgency:     "high",
			RequestedAt: now,
			Context:     `{"error_code": 500, "attempts": 3, "service": "api"}`,
		},
	}

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string for JSON context decision")
	}
}

// TestDecisionWithSuccessorSchemas tests handling decisions with successor_schemas in context.
func TestDecisionWithSuccessorSchemas(t *testing.T) {
	m := New()
	m.width = 80
	m.height = 24
	now := time.Now()

	contextWithSchemas := `{
		"diagnosis": "rate limiting",
		"successor_schemas": {
			"Fix upstream": {"required": ["fix_approach", "estimated_effort"]},
			"Add retry": {"required": ["backoff_strategy"]}
		}
	}`

	m.decisions = []DecisionItem{
		{
			ID:          "bd-schema",
			Prompt:      "How to handle rate limiting?",
			Options:     []types.DecisionOption{{ID: "a", Label: "Fix upstream"}, {ID: "b", Label: "Add retry"}},
			Urgency:     "high",
			RequestedAt: now,
			Context:     contextWithSchemas,
		},
	}

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string for decision with successor schemas")
	}
}

// TestChainedDecisionFetch tests fetching chained decisions.
func TestChainedDecisionFetch(t *testing.T) {
	m := New()
	now := time.Now()

	decisions := []DecisionItem{
		{
			ID:            "bd-3",
			Prompt:        "Third in chain",
			Urgency:       "low",
			RequestedAt:   now,
			PredecessorID: "bd-2",
		},
		{
			ID:            "bd-2",
			Prompt:        "Second in chain",
			Urgency:       "medium",
			RequestedAt:   now.Add(-1 * time.Hour),
			PredecessorID: "bd-1",
		},
		{
			ID:          "bd-1",
			Prompt:      "First in chain (root)",
			Urgency:     "high",
			RequestedAt: now.Add(-2 * time.Hour),
		},
	}

	msg := fetchDecisionsMsg{decisions: decisions}
	updated, _ := m.Update(msg)
	model := updated.(*Model)

	if len(model.decisions) != 3 {
		t.Errorf("expected 3 decisions, got %d", len(model.decisions))
	}

	// First should be high urgency
	if model.decisions[0].ID != "bd-1" {
		t.Errorf("expected first decision to be bd-1 (high urgency), got %s", model.decisions[0].ID)
	}

	// Check predecessors are preserved
	for _, d := range model.decisions {
		if d.ID == "bd-2" && d.PredecessorID != "bd-1" {
			t.Errorf("expected bd-2 predecessor to be bd-1, got %s", d.PredecessorID)
		}
		if d.ID == "bd-3" && d.PredecessorID != "bd-2" {
			t.Errorf("expected bd-3 predecessor to be bd-2, got %s", d.PredecessorID)
		}
	}
}

// TestViewWithChainingInfo tests that view renders chaining info correctly.
func TestViewWithChainingInfo(t *testing.T) {
	m := New()
	m.width = 100
	m.height = 40
	now := time.Now()

	m.decisions = []DecisionItem{
		{
			ID:            "bd-chained",
			Prompt:        "Chained decision with context",
			Options:       []types.DecisionOption{{ID: "a", Label: "Option A", Description: "First option"}, {ID: "b", Label: "Option B", Description: "Second option"}},
			Urgency:       "high",
			RequestedAt:   now,
			RequestedBy:   "myrig/crew/test",
			PredecessorID: "bd-parent",
			Context:       `{"key": "value", "nested": {"a": 1}}`,
		},
	}
	m.selected = 0

	view := m.View()
	if view == "" {
		t.Fatal("View() returned empty string")
	}
}

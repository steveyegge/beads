package tracker

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestParseBeadsStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected types.Status
	}{
		{"open", types.StatusOpen},
		{"in_progress", types.StatusInProgress},
		{"in-progress", types.StatusInProgress},
		{"inprogress", types.StatusInProgress},
		{"blocked", types.StatusBlocked},
		{"deferred", types.StatusDeferred},
		{"closed", types.StatusClosed},
		{"pinned", types.StatusPinned},
		{"unknown", types.StatusOpen}, // Default
		{"", types.StatusOpen},        // Empty string
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseBeadsStatus(tt.input)
			if result != tt.expected {
				t.Errorf("ParseBeadsStatus(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseIssueType(t *testing.T) {
	tests := []struct {
		input    string
		expected types.IssueType
	}{
		{"bug", types.TypeBug},
		{"feature", types.TypeFeature},
		{"task", types.TypeTask},
		{"epic", types.TypeEpic},
		{"chore", types.TypeChore},
		{"unknown", types.TypeTask}, // Default
		{"", types.TypeTask},        // Empty string
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseIssueType(tt.input)
			if result != tt.expected {
				t.Errorf("ParseIssueType(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultBaseMappingConfig(t *testing.T) {
	config := DefaultBaseMappingConfig()

	// Check priority map
	if len(config.PriorityMap) == 0 {
		t.Error("PriorityMap should not be empty")
	}
	if config.PriorityMap["0"] != 4 {
		t.Errorf("PriorityMap[\"0\"] = %d, want 4", config.PriorityMap["0"])
	}

	// Check state map
	if len(config.StateMap) == 0 {
		t.Error("StateMap should not be empty")
	}
	if config.StateMap["backlog"] != "open" {
		t.Errorf("StateMap[\"backlog\"] = %s, want \"open\"", config.StateMap["backlog"])
	}

	// Check label type map
	if len(config.LabelTypeMap) == 0 {
		t.Error("LabelTypeMap should not be empty")
	}
	if config.LabelTypeMap["bug"] != "bug" {
		t.Errorf("LabelTypeMap[\"bug\"] = %s, want \"bug\"", config.LabelTypeMap["bug"])
	}

	// Check relation map
	if len(config.RelationMap) == 0 {
		t.Error("RelationMap should not be empty")
	}
	if config.RelationMap["blocks"] != "blocks" {
		t.Errorf("RelationMap[\"blocks\"] = %s, want \"blocks\"", config.RelationMap["blocks"])
	}
}

func TestErrNotInitialized(t *testing.T) {
	err := &ErrNotInitialized{Tracker: "test"}
	expected := "test tracker not initialized; call Init() first"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

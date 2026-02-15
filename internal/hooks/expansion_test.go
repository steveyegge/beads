package hooks

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestSanitizeValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"normal-text_123", "normal-text_123"},
		{"$(whoami)", "whoami"},
		{"`id`", "id"},
		{"test; rm -rf /", "test rm -rf /"},
		{"a && b", "a  b"},
		{"a || b", "a  b"},
		{"test\nline", "testline"},
		{"it's", "its"},
		{`it"s`, "its"},
		{"hello $(echo pwned)", "hello echo pwned"},
		{"${IFS}", "IFS"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeValue(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExpandCommand(t *testing.T) {
	vars := &HookVars{
		BeadID:       "aegis-abc",
		BeadRig:      "aegis",
		BeadTitle:    "Fix the bug",
		BeadPriority: "P1",
		BeadType:     "bug",
		BeadEvent:    "create",
		BeadStatus:   "open",
	}

	tests := []struct {
		command  string
		expected string
	}{
		{
			"echo ${BEAD_ID}",
			"echo aegis-abc",
		},
		{
			"bobbin index-bead --id ${BEAD_ID} --rig ${BEAD_RIG}",
			"bobbin index-bead --id aegis-abc --rig aegis",
		},
		{
			"echo ${BEAD_TITLE} (${BEAD_PRIORITY})",
			"echo Fix the bug (P1)",
		},
		{
			"no-vars-here",
			"no-vars-here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			result := ExpandCommand(tt.command, vars)
			if result != tt.expected {
				t.Errorf("ExpandCommand(%q) = %q, want %q", tt.command, result, tt.expected)
			}
		})
	}
}

func TestExpandCommand_Sanitizes(t *testing.T) {
	vars := &HookVars{
		BeadID:    "aegis-abc",
		BeadTitle: "Fix $(whoami) bug",
	}

	result := ExpandCommand("echo ${BEAD_TITLE}", vars)
	if result != "echo Fix whoami bug" {
		t.Errorf("got %q, want injection-safe output", result)
	}
}

func TestExpandCommand_NilVars(t *testing.T) {
	result := ExpandCommand("echo ${BEAD_ID}", nil)
	if result != "echo ${BEAD_ID}" {
		t.Errorf("got %q, want unchanged command", result)
	}
}

func TestVarsFromIssue(t *testing.T) {
	issue := &types.Issue{
		ID:          "aegis-xyz",
		Title:       "Test issue",
		Priority:    2,
		IssueType:   "task",
		Status:      "open",
		CloseReason: "done",
	}

	vars := VarsFromIssue(issue, "close")

	if vars.BeadID != "aegis-xyz" {
		t.Errorf("BeadID = %q", vars.BeadID)
	}
	if vars.BeadRig != "aegis" {
		t.Errorf("BeadRig = %q", vars.BeadRig)
	}
	if vars.BeadPriority != "P2" {
		t.Errorf("BeadPriority = %q", vars.BeadPriority)
	}
	if vars.BeadEvent != "close" {
		t.Errorf("BeadEvent = %q", vars.BeadEvent)
	}
	if vars.BeadReason != "done" {
		t.Errorf("BeadReason = %q", vars.BeadReason)
	}
}

func TestVarsFromIssue_Nil(t *testing.T) {
	vars := VarsFromIssue(nil, "create")
	if vars.BeadEvent != "create" {
		t.Errorf("BeadEvent = %q, want %q", vars.BeadEvent, "create")
	}
	if vars.BeadID != "" {
		t.Errorf("BeadID should be empty, got %q", vars.BeadID)
	}
}

func TestExtractRig(t *testing.T) {
	tests := []struct {
		id       string
		expected string
	}{
		{"aegis-abc", "aegis"},
		{"bd-xyz", "bd"},
		{"my-project-123", "my-project"},
		{"nohash", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := extractRig(tt.id)
			if result != tt.expected {
				t.Errorf("extractRig(%q) = %q, want %q", tt.id, result, tt.expected)
			}
		})
	}
}

func TestPriorityString(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "P0"},
		{1, "P1"},
		{2, "P2"},
		{3, "P3"},
	}

	for _, tt := range tests {
		result := priorityString(tt.input)
		if result != tt.expected {
			t.Errorf("priorityString(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

package formula

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestFormulaToIssue(t *testing.T) {
	t.Run("basic workflow formula", func(t *testing.T) {
		f := &Formula{
			Formula:     "mol-polecat-work",
			Description: "Full polecat work lifecycle",
			Version:     1,
			Type:        TypeWorkflow,
			Phase:       "liquid",
			Steps: []*Step{
				{ID: "step-1", Title: "Load context"},
				{ID: "step-2", Title: "Implement", DependsOn: []string{"step-1"}},
			},
			Vars: map[string]*VarDef{
				"issue": {Description: "The issue ID", Required: true},
			},
		}

		issue, labels, err := FormulaToIssue(f, "bd-")
		if err != nil {
			t.Fatalf("FormulaToIssue failed: %v", err)
		}

		if issue.ID != "bd-formula-mol-polecat-work" {
			t.Errorf("ID = %q, want %q", issue.ID, "bd-formula-mol-polecat-work")
		}
		if issue.Title != "mol-polecat-work" {
			t.Errorf("Title = %q, want %q", issue.Title, "mol-polecat-work")
		}
		if issue.Description != "Full polecat work lifecycle" {
			t.Errorf("Description = %q, want %q", issue.Description, "Full polecat work lifecycle")
		}
		if issue.IssueType != types.TypeFormula {
			t.Errorf("IssueType = %q, want %q", issue.IssueType, types.TypeFormula)
		}
		if !issue.IsTemplate {
			t.Error("IsTemplate should be true")
		}
		if issue.SourceFormula != "mol-polecat-work" {
			t.Errorf("SourceFormula = %q, want %q", issue.SourceFormula, "mol-polecat-work")
		}
		if len(issue.Metadata) == 0 {
			t.Error("Metadata should not be empty")
		}

		// Check labels
		expectedLabels := map[string]bool{
			"formula-type:workflow": true,
			"phase:liquid":         true,
		}
		for _, l := range labels {
			if !expectedLabels[l] {
				t.Errorf("unexpected label %q", l)
			}
			delete(expectedLabels, l)
		}
		for l := range expectedLabels {
			t.Errorf("missing expected label %q", l)
		}
	})

	t.Run("nil formula", func(t *testing.T) {
		_, _, err := FormulaToIssue(nil, "bd-")
		if err == nil {
			t.Error("expected error for nil formula")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		_, _, err := FormulaToIssue(&Formula{}, "bd-")
		if err == nil {
			t.Error("expected error for empty formula name")
		}
	})

	t.Run("with requires_skills", func(t *testing.T) {
		f := &Formula{
			Formula:        "mol-test",
			Type:           TypeWorkflow,
			RequiresSkills: []string{"go", "testing"},
		}
		_, labels, err := FormulaToIssue(f, "bd-")
		if err != nil {
			t.Fatalf("FormulaToIssue failed: %v", err)
		}
		skillLabels := 0
		for _, l := range labels {
			if l == "skill:go" || l == "skill:testing" {
				skillLabels++
			}
		}
		if skillLabels != 2 {
			t.Errorf("expected 2 skill labels, got %d", skillLabels)
		}
	})
}

func TestIssueToFormula(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		original := &Formula{
			Formula:     "mol-test-roundtrip",
			Description: "A test formula",
			Version:     1,
			Type:        TypeWorkflow,
			Phase:       "vapor",
			Steps: []*Step{
				{ID: "s1", Title: "First step"},
				{ID: "s2", Title: "Second step", DependsOn: []string{"s1"}},
			},
			Vars: map[string]*VarDef{
				"name": {Description: "Thing name", Default: "world"},
			},
			RequiresSkills: []string{"go"},
		}

		issue, _, err := FormulaToIssue(original, "bd-")
		if err != nil {
			t.Fatalf("FormulaToIssue failed: %v", err)
		}

		restored, err := IssueToFormula(issue)
		if err != nil {
			t.Fatalf("IssueToFormula failed: %v", err)
		}

		// Verify fields survived round trip
		if restored.Formula != original.Formula {
			t.Errorf("Formula = %q, want %q", restored.Formula, original.Formula)
		}
		if restored.Description != original.Description {
			t.Errorf("Description = %q, want %q", restored.Description, original.Description)
		}
		if restored.Version != original.Version {
			t.Errorf("Version = %d, want %d", restored.Version, original.Version)
		}
		if restored.Type != original.Type {
			t.Errorf("Type = %q, want %q", restored.Type, original.Type)
		}
		if restored.Phase != original.Phase {
			t.Errorf("Phase = %q, want %q", restored.Phase, original.Phase)
		}
		if len(restored.Steps) != len(original.Steps) {
			t.Errorf("Steps count = %d, want %d", len(restored.Steps), len(original.Steps))
		}
		if len(restored.Vars) != len(original.Vars) {
			t.Errorf("Vars count = %d, want %d", len(restored.Vars), len(original.Vars))
		}
		if restored.Source != "bead:bd-formula-mol-test-roundtrip" {
			t.Errorf("Source = %q, want %q", restored.Source, "bead:bd-formula-mol-test-roundtrip")
		}
	})

	t.Run("nil issue", func(t *testing.T) {
		_, err := IssueToFormula(nil)
		if err == nil {
			t.Error("expected error for nil issue")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		issue := &types.Issue{
			ID:        "test-1",
			IssueType: types.TypeTask,
			Metadata:  json.RawMessage(`{}`),
		}
		_, err := IssueToFormula(issue)
		if err == nil {
			t.Error("expected error for wrong issue type")
		}
	})

	t.Run("empty metadata", func(t *testing.T) {
		issue := &types.Issue{
			ID:        "test-1",
			IssueType: types.TypeFormula,
		}
		_, err := IssueToFormula(issue)
		if err == nil {
			t.Error("expected error for empty metadata")
		}
	})

	t.Run("malformed metadata", func(t *testing.T) {
		issue := &types.Issue{
			ID:        "test-1",
			IssueType: types.TypeFormula,
			Metadata:  json.RawMessage(`{invalid json`),
		}
		_, err := IssueToFormula(issue)
		if err == nil {
			t.Error("expected error for malformed metadata")
		}
	})
}

func TestFormulaNameToSlug(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"mol-polecat-work", "mol-polecat-work"},
		{"My Formula Name", "my-formula-name"},
		{"test_formula", "test-formula"},
		{"Some.Formula.Name", "some-formula-name"},
		{"UPPER-CASE", "upper-case"},
		{"  spaces  ", "spaces"},
		{"multi---hyphens", "multi-hyphens"},
		{"special@chars!", "specialchars"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formulaNameToSlug(tt.name)
			if got != tt.want {
				t.Errorf("formulaNameToSlug(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestFormulaToIssuePreservesNestedStructures(t *testing.T) {
	f := &Formula{
		Formula: "mol-complex",
		Version: 1,
		Type:    TypeWorkflow,
		Compose: &ComposeRules{
			BondPoints: []*BondPoint{
				{ID: "bp1", AfterStep: "step-1"},
			},
		},
		Advice: []*AdviceRule{
			{Target: "test-*"},
		},
		Steps: []*Step{
			{
				ID:    "parent",
				Title: "Parent step",
				Children: []*Step{
					{ID: "child-1", Title: "Child step"},
				},
				Gate: &Gate{
					Type:    "human",
					Timeout: "30m",
				},
			},
		},
	}

	issue, _, err := FormulaToIssue(f, "bd-")
	if err != nil {
		t.Fatalf("FormulaToIssue failed: %v", err)
	}

	restored, err := IssueToFormula(issue)
	if err != nil {
		t.Fatalf("IssueToFormula failed: %v", err)
	}

	// Verify nested structures
	if restored.Compose == nil {
		t.Fatal("Compose should not be nil")
	}
	if len(restored.Compose.BondPoints) != 1 {
		t.Errorf("BondPoints count = %d, want 1", len(restored.Compose.BondPoints))
	}
	if len(restored.Advice) != 1 {
		t.Errorf("Advice count = %d, want 1", len(restored.Advice))
	}
	if len(restored.Steps) != 1 {
		t.Fatal("Steps count should be 1")
	}
	if len(restored.Steps[0].Children) != 1 {
		t.Errorf("Children count = %d, want 1", len(restored.Steps[0].Children))
	}
	if restored.Steps[0].Gate == nil {
		t.Error("Gate should not be nil")
	}
}

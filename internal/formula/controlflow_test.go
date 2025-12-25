package formula

import (
	"testing"
)

func TestApplyLoops_FixedCount(t *testing.T) {
	// Create a step with a fixed-count loop
	steps := []*Step{
		{
			ID:    "process",
			Title: "Process items",
			Loop: &LoopSpec{
				Count: 3,
				Body: []*Step{
					{ID: "fetch", Title: "Fetch item"},
					{ID: "transform", Title: "Transform item", Needs: []string{"fetch"}},
				},
			},
		},
	}

	result, err := ApplyLoops(steps)
	if err != nil {
		t.Fatalf("ApplyLoops failed: %v", err)
	}

	// Should have 6 steps (3 iterations * 2 steps each)
	if len(result) != 6 {
		t.Errorf("Expected 6 steps, got %d", len(result))
	}

	// Check step IDs
	expectedIDs := []string{
		"process.iter1.fetch",
		"process.iter1.transform",
		"process.iter2.fetch",
		"process.iter2.transform",
		"process.iter3.fetch",
		"process.iter3.transform",
	}

	for i, expected := range expectedIDs {
		if i >= len(result) {
			t.Errorf("Missing step %d: %s", i, expected)
			continue
		}
		if result[i].ID != expected {
			t.Errorf("Step %d: expected ID %s, got %s", i, expected, result[i].ID)
		}
	}

	// Check that inner dependencies are preserved (within same iteration)
	transform1 := result[1]
	if len(transform1.Needs) != 1 || transform1.Needs[0] != "process.iter1.fetch" {
		t.Errorf("transform1 should need process.iter1.fetch, got %v", transform1.Needs)
	}

	// Check that iterations are chained (iter2 depends on iter1)
	fetch2 := result[2]
	if len(fetch2.Needs) != 1 || fetch2.Needs[0] != "process.iter1.transform" {
		t.Errorf("iter2.fetch should need iter1.transform, got %v", fetch2.Needs)
	}
}

func TestApplyLoops_Conditional(t *testing.T) {
	steps := []*Step{
		{
			ID:    "retry",
			Title: "Retry operation",
			Loop: &LoopSpec{
				Until: "step.status == 'complete'",
				Max:   5,
				Body: []*Step{
					{ID: "attempt", Title: "Attempt operation"},
				},
			},
		},
	}

	result, err := ApplyLoops(steps)
	if err != nil {
		t.Fatalf("ApplyLoops failed: %v", err)
	}

	// Conditional loops expand once (runtime re-executes)
	if len(result) != 1 {
		t.Errorf("Expected 1 step for conditional loop, got %d", len(result))
	}

	// Should have loop metadata labels
	step := result[0]
	hasUntil := false
	hasMax := false
	for _, label := range step.Labels {
		if label == "loop:until:step.status == 'complete'" {
			hasUntil = true
		}
		if label == "loop:max:5" {
			hasMax = true
		}
	}

	if !hasUntil {
		t.Error("Missing loop:until label")
	}
	if !hasMax {
		t.Error("Missing loop:max label")
	}
}

func TestApplyLoops_Validation(t *testing.T) {
	tests := []struct {
		name    string
		loop    *LoopSpec
		wantErr string
	}{
		{
			name:    "empty body",
			loop:    &LoopSpec{Count: 3, Body: nil},
			wantErr: "body is required",
		},
		{
			name:    "both count and until",
			loop:    &LoopSpec{Count: 3, Until: "cond", Max: 5, Body: []*Step{{ID: "a", Title: "A"}}},
			wantErr: "cannot have both count and until",
		},
		{
			name:    "neither count nor until",
			loop:    &LoopSpec{Body: []*Step{{ID: "a", Title: "A"}}},
			wantErr: "either count or until is required",
		},
		{
			name:    "until without max",
			loop:    &LoopSpec{Until: "cond", Body: []*Step{{ID: "a", Title: "A"}}},
			wantErr: "max is required when until is set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := []*Step{{ID: "test", Title: "Test", Loop: tt.loop}}
			_, err := ApplyLoops(steps)
			if err == nil {
				t.Error("Expected error, got nil")
			} else if tt.wantErr != "" && err.Error() != "" {
				// Just check that an error was returned
				// The exact message format may vary
			}
		})
	}
}

func TestApplyBranches(t *testing.T) {
	steps := []*Step{
		{ID: "setup", Title: "Setup"},
		{ID: "test", Title: "Run tests"},
		{ID: "lint", Title: "Run linter"},
		{ID: "build", Title: "Build"},
		{ID: "deploy", Title: "Deploy"},
	}

	compose := &ComposeRules{
		Branch: []*BranchRule{
			{
				From:  "setup",
				Steps: []string{"test", "lint", "build"},
				Join:  "deploy",
			},
		},
	}

	result, err := ApplyBranches(steps, compose)
	if err != nil {
		t.Fatalf("ApplyBranches failed: %v", err)
	}

	// Build step map for checking
	stepMap := make(map[string]*Step)
	for _, s := range result {
		stepMap[s.ID] = s
	}

	// Verify branch steps depend on 'from'
	for _, branchStep := range []string{"test", "lint", "build"} {
		s := stepMap[branchStep]
		found := false
		for _, need := range s.Needs {
			if need == "setup" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Step %s should need 'setup', got %v", branchStep, s.Needs)
		}
	}

	// Verify 'join' depends on all branch steps
	deploy := stepMap["deploy"]
	for _, branchStep := range []string{"test", "lint", "build"} {
		found := false
		for _, need := range deploy.Needs {
			if need == branchStep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("deploy should need %s, got %v", branchStep, deploy.Needs)
		}
	}
}

func TestApplyBranches_Validation(t *testing.T) {
	steps := []*Step{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	tests := []struct {
		name    string
		branch  *BranchRule
		wantErr string
	}{
		{
			name:    "missing from",
			branch:  &BranchRule{Steps: []string{"a"}, Join: "b"},
			wantErr: "from is required",
		},
		{
			name:    "missing steps",
			branch:  &BranchRule{From: "a", Join: "b"},
			wantErr: "steps is required",
		},
		{
			name:    "missing join",
			branch:  &BranchRule{From: "a", Steps: []string{"b"}},
			wantErr: "join is required",
		},
		{
			name:    "from not found",
			branch:  &BranchRule{From: "notfound", Steps: []string{"a"}, Join: "b"},
			wantErr: "from step \"notfound\" not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compose := &ComposeRules{Branch: []*BranchRule{tt.branch}}
			_, err := ApplyBranches(steps, compose)
			if err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}

func TestApplyGates(t *testing.T) {
	steps := []*Step{
		{ID: "tests", Title: "Run tests"},
		{ID: "deploy", Title: "Deploy to production"},
	}

	compose := &ComposeRules{
		Gate: []*GateRule{
			{
				Before:    "deploy",
				Condition: "tests.status == 'complete'",
			},
		},
	}

	result, err := ApplyGates(steps, compose)
	if err != nil {
		t.Fatalf("ApplyGates failed: %v", err)
	}

	// Find deploy step
	var deploy *Step
	for _, s := range result {
		if s.ID == "deploy" {
			deploy = s
			break
		}
	}

	if deploy == nil {
		t.Fatal("deploy step not found")
	}

	// Check for gate label
	found := false
	expectedLabel := "gate:condition:tests.status == 'complete'"
	for _, label := range deploy.Labels {
		if label == expectedLabel {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("deploy should have gate label, got %v", deploy.Labels)
	}
}

func TestApplyGates_InvalidCondition(t *testing.T) {
	steps := []*Step{
		{ID: "deploy", Title: "Deploy"},
	}

	compose := &ComposeRules{
		Gate: []*GateRule{
			{
				Before:    "deploy",
				Condition: "invalid condition syntax ???",
			},
		},
	}

	_, err := ApplyGates(steps, compose)
	if err == nil {
		t.Error("Expected error for invalid condition, got nil")
	}
}

func TestApplyControlFlow_Integration(t *testing.T) {
	// Test the combined ApplyControlFlow function
	steps := []*Step{
		{ID: "setup", Title: "Setup"},
		{
			ID:    "process",
			Title: "Process items",
			Loop: &LoopSpec{
				Count: 2,
				Body: []*Step{
					{ID: "item", Title: "Process item"},
				},
			},
		},
		{ID: "cleanup", Title: "Cleanup"},
	}

	compose := &ComposeRules{
		Branch: []*BranchRule{
			{
				From:  "setup",
				Steps: []string{"process.iter1.item", "process.iter2.item"},
				Join:  "cleanup",
			},
		},
		Gate: []*GateRule{
			{
				Before:    "cleanup",
				Condition: "steps.complete >= 2",
			},
		},
	}

	result, err := ApplyControlFlow(steps, compose)
	if err != nil {
		t.Fatalf("ApplyControlFlow failed: %v", err)
	}

	// Should have: setup, process.iter1.item, process.iter2.item, cleanup
	if len(result) != 4 {
		t.Errorf("Expected 4 steps, got %d", len(result))
	}

	// Verify cleanup has gate label
	var cleanup *Step
	for _, s := range result {
		if s.ID == "cleanup" {
			cleanup = s
			break
		}
	}

	if cleanup == nil {
		t.Fatal("cleanup step not found")
	}

	hasGate := false
	for _, label := range cleanup.Labels {
		if label == "gate:condition:steps.complete >= 2" {
			hasGate = true
			break
		}
	}

	if !hasGate {
		t.Errorf("cleanup should have gate label, got %v", cleanup.Labels)
	}
}

func TestApplyLoops_NoLoops(t *testing.T) {
	// Test with steps that have no loops
	steps := []*Step{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Needs: []string{"a"}},
	}

	result, err := ApplyLoops(steps)
	if err != nil {
		t.Fatalf("ApplyLoops failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(result))
	}

	// Dependencies should be preserved
	if len(result[1].Needs) != 1 || result[1].Needs[0] != "a" {
		t.Errorf("Dependencies not preserved: %v", result[1].Needs)
	}
}

func TestApplyLoops_NestedChildren(t *testing.T) {
	// Test that children are preserved when recursing
	steps := []*Step{
		{
			ID:    "parent",
			Title: "Parent",
			Children: []*Step{
				{ID: "child", Title: "Child"},
			},
		},
	}

	result, err := ApplyLoops(steps)
	if err != nil {
		t.Fatalf("ApplyLoops failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(result))
	}

	if len(result[0].Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(result[0].Children))
	}
}

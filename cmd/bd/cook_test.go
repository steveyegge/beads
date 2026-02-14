package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// Cook Tests (gt-8tmz.23: Compile-time vs Runtime Cooking)
// =============================================================================

// TestSubstituteFormulaVars tests variable substitution in formulas
func TestSubstituteFormulaVars(t *testing.T) {
	tests := []struct {
		name          string
		formula       *formula.Formula
		vars          map[string]string
		wantDesc      string
		wantStepTitle string
	}{
		{
			name: "substitute single variable in description",
			formula: &formula.Formula{
				Description: "Build {{feature}} feature",
				Steps:       []*formula.Step{},
			},
			vars:     map[string]string{"feature": "auth"},
			wantDesc: "Build auth feature",
		},
		{
			name: "substitute variable in step title",
			formula: &formula.Formula{
				Description: "Feature work",
				Steps: []*formula.Step{
					{Title: "Implement {{name}}"},
				},
			},
			vars:          map[string]string{"name": "login"},
			wantDesc:      "Feature work",
			wantStepTitle: "Implement login",
		},
		{
			name: "substitute multiple variables",
			formula: &formula.Formula{
				Description: "Release {{version}} on {{date}}",
				Steps: []*formula.Step{
					{Title: "Tag {{version}}"},
					{Title: "Deploy to {{env}}"},
				},
			},
			vars: map[string]string{
				"version": "1.0.0",
				"date":    "2024-01-15",
				"env":     "production",
			},
			wantDesc:      "Release 1.0.0 on 2024-01-15",
			wantStepTitle: "Tag 1.0.0",
		},
		{
			name: "nested children substitution",
			formula: &formula.Formula{
				Description: "Epic for {{project}}",
				Steps: []*formula.Step{
					{
						Title: "Phase 1: {{project}} design",
						Children: []*formula.Step{
							{Title: "Design {{component}}"},
						},
					},
				},
			},
			vars: map[string]string{
				"project":   "checkout",
				"component": "cart",
			},
			wantDesc:      "Epic for checkout",
			wantStepTitle: "Phase 1: checkout design",
		},
		{
			name: "unsubstituted variable left as-is",
			formula: &formula.Formula{
				Description: "Build {{feature}} with {{extra}}",
				Steps:       []*formula.Step{},
			},
			vars:     map[string]string{"feature": "auth"},
			wantDesc: "Build auth with {{extra}}", // {{extra}} unchanged
		},
		{
			name: "empty vars map",
			formula: &formula.Formula{
				Description: "Keep {{placeholder}} intact",
				Steps:       []*formula.Step{},
			},
			vars:     map[string]string{},
			wantDesc: "Keep {{placeholder}} intact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			substituteFormulaVars(tt.formula, tt.vars)

			if tt.formula.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", tt.formula.Description, tt.wantDesc)
			}

			if tt.wantStepTitle != "" && len(tt.formula.Steps) > 0 {
				if tt.formula.Steps[0].Title != tt.wantStepTitle {
					t.Errorf("Steps[0].Title = %q, want %q", tt.formula.Steps[0].Title, tt.wantStepTitle)
				}
			}
		})
	}
}

// TestSubstituteStepVarsRecursive tests deep nesting works correctly
func TestSubstituteStepVarsRecursive(t *testing.T) {
	steps := []*formula.Step{
		{
			Title: "Root: {{name}}",
			Children: []*formula.Step{
				{
					Title: "Level 1: {{name}}",
					Children: []*formula.Step{
						{
							Title: "Level 2: {{name}}",
							Children: []*formula.Step{
								{Title: "Level 3: {{name}}"},
							},
						},
					},
				},
			},
		},
	}

	vars := map[string]string{"name": "test"}
	substituteStepVars(steps, vars)

	// Check all levels got substituted
	if steps[0].Title != "Root: test" {
		t.Errorf("Root title = %q, want %q", steps[0].Title, "Root: test")
	}
	if steps[0].Children[0].Title != "Level 1: test" {
		t.Errorf("Level 1 title = %q, want %q", steps[0].Children[0].Title, "Level 1: test")
	}
	if steps[0].Children[0].Children[0].Title != "Level 2: test" {
		t.Errorf("Level 2 title = %q, want %q", steps[0].Children[0].Children[0].Title, "Level 2: test")
	}
	if steps[0].Children[0].Children[0].Children[0].Title != "Level 3: test" {
		t.Errorf("Level 3 title = %q, want %q", steps[0].Children[0].Children[0].Children[0].Title, "Level 3: test")
	}
}

// TestCompileTimeVsRuntimeMode tests that compile-time preserves placeholders
// and runtime mode substitutes them
func TestCompileTimeVsRuntimeMode(t *testing.T) {
	// Simulate compile-time mode (no variable substitution)
	compileFormula := &formula.Formula{
		Description: "Feature: {{name}}",
		Steps: []*formula.Step{
			{Title: "Implement {{name}}"},
		},
	}

	// In compile-time mode, don't call substituteFormulaVars
	// Placeholders should remain intact
	if compileFormula.Description != "Feature: {{name}}" {
		t.Errorf("Compile-time: Description should preserve placeholder, got %q", compileFormula.Description)
	}

	// Simulate runtime mode (with variable substitution)
	runtimeFormula := &formula.Formula{
		Description: "Feature: {{name}}",
		Steps: []*formula.Step{
			{Title: "Implement {{name}}"},
		},
	}
	vars := map[string]string{"name": "auth"}
	substituteFormulaVars(runtimeFormula, vars)

	if runtimeFormula.Description != "Feature: auth" {
		t.Errorf("Runtime: Description = %q, want %q", runtimeFormula.Description, "Feature: auth")
	}
	if runtimeFormula.Steps[0].Title != "Implement auth" {
		t.Errorf("Runtime: Steps[0].Title = %q, want %q", runtimeFormula.Steps[0].Title, "Implement auth")
	}
}

// =============================================================================
// Gate Bead Tests (bd-4k3c: Gate beads created during cook)
// =============================================================================

// TestCreateGateIssue tests that createGateIssue creates proper gate issues
func TestCreateGateIssue(t *testing.T) {
	tests := []struct {
		name          string
		step          *formula.Step
		parentID      string
		wantID        string
		wantTitle     string
		wantAwaitType string
		wantAwaitID   string
	}{
		{
			name: "gh:run gate with ID",
			step: &formula.Step{
				ID:    "await-ci",
				Title: "Wait for CI",
				Gate: &formula.Gate{
					Type: "gh:run",
					ID:   "release-build",
				},
			},
			parentID:      "mol-release",
			wantID:        "mol-release.gate-await-ci",
			wantTitle:     "Gate: gh:run release-build",
			wantAwaitType: "gh:run",
			wantAwaitID:   "release-build",
		},
		{
			name: "gh:pr gate without ID",
			step: &formula.Step{
				ID:    "await-pr",
				Title: "Wait for PR",
				Gate: &formula.Gate{
					Type: "gh:pr",
				},
			},
			parentID:      "mol-feature",
			wantID:        "mol-feature.gate-await-pr",
			wantTitle:     "Gate: gh:pr",
			wantAwaitType: "gh:pr",
			wantAwaitID:   "",
		},
		{
			name: "timer gate",
			step: &formula.Step{
				ID:    "cooldown",
				Title: "Wait for cooldown",
				Gate: &formula.Gate{
					Type:    "timer",
					Timeout: "30m",
				},
			},
			parentID:      "mol-deploy",
			wantID:        "mol-deploy.gate-cooldown",
			wantTitle:     "Gate: timer",
			wantAwaitType: "timer",
			wantAwaitID:   "",
		},
		{
			name: "human gate",
			step: &formula.Step{
				ID:    "approval",
				Title: "Manual approval",
				Gate: &formula.Gate{
					Type:    "human",
					Timeout: "24h",
				},
			},
			parentID:      "mol-release",
			wantID:        "mol-release.gate-approval",
			wantTitle:     "Gate: human",
			wantAwaitType: "human",
			wantAwaitID:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateIssue := createGateIssue(tt.step, tt.parentID)

			if gateIssue == nil {
				t.Fatal("createGateIssue returned nil")
			}

			if gateIssue.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", gateIssue.ID, tt.wantID)
			}
			if gateIssue.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", gateIssue.Title, tt.wantTitle)
			}
			if gateIssue.AwaitType != tt.wantAwaitType {
				t.Errorf("AwaitType = %q, want %q", gateIssue.AwaitType, tt.wantAwaitType)
			}
			if gateIssue.AwaitID != tt.wantAwaitID {
				t.Errorf("AwaitID = %q, want %q", gateIssue.AwaitID, tt.wantAwaitID)
			}
			if gateIssue.IssueType != "gate" {
				t.Errorf("IssueType = %q, want %q", gateIssue.IssueType, "gate")
			}
			if !gateIssue.IsTemplate {
				t.Error("IsTemplate should be true")
			}
		})
	}
}

// TestCreateGateIssue_NilGate tests that nil Gate returns nil
func TestCreateGateIssue_NilGate(t *testing.T) {
	step := &formula.Step{
		ID:    "no-gate",
		Title: "Step without gate",
		Gate:  nil,
	}

	gateIssue := createGateIssue(step, "mol-test")
	if gateIssue != nil {
		t.Errorf("Expected nil for step without Gate, got %+v", gateIssue)
	}
}

// TestCreateGateIssue_Timeout tests that timeout is parsed correctly
func TestCreateGateIssue_Timeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		wantMinutes int
	}{
		{"30 minutes", "30m", 30},
		{"1 hour", "1h", 60},
		{"24 hours", "24h", 1440},
		{"invalid timeout", "invalid", 0},
		{"empty timeout", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &formula.Step{
				ID:    "timed-step",
				Title: "Timed step",
				Gate: &formula.Gate{
					Type:    "timer",
					Timeout: tt.timeout,
				},
			}

			gateIssue := createGateIssue(step, "mol-test")
			gotMinutes := int(gateIssue.Timeout.Minutes())

			if gotMinutes != tt.wantMinutes {
				t.Errorf("Timeout minutes = %d, want %d", gotMinutes, tt.wantMinutes)
			}
		})
	}
}

// TestCookFormulaToSubgraph_GateBeads tests that gate beads are created in subgraph
func TestCookFormulaToSubgraph_GateBeads(t *testing.T) {
	f := &formula.Formula{
		Formula:     "mol-test-gate",
		Description: "Test gate creation",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{
				ID:    "build",
				Title: "Build project",
			},
			{
				ID:    "await-ci",
				Title: "Wait for CI",
				Gate: &formula.Gate{
					Type: "gh:run",
					ID:   "ci-workflow",
				},
			},
			{
				ID:        "verify",
				Title:     "Verify deployment",
				DependsOn: []string{"await-ci"},
			},
		},
	}

	subgraph, err := cookFormulaToSubgraph(f, "mol-test-gate")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}

	// Should have: root + 3 steps + 1 gate = 5 issues
	if len(subgraph.Issues) != 5 {
		t.Errorf("Expected 5 issues, got %d", len(subgraph.Issues))
		for _, issue := range subgraph.Issues {
			t.Logf("  Issue: %s (%s)", issue.ID, issue.IssueType)
		}
	}

	// Find the gate issue
	var gateIssue *types.Issue
	for _, issue := range subgraph.Issues {
		if issue.IssueType == "gate" {
			gateIssue = issue
			break
		}
	}

	if gateIssue == nil {
		t.Fatal("Gate issue not found in subgraph")
	}

	if gateIssue.ID != "mol-test-gate.gate-await-ci" {
		t.Errorf("Gate ID = %q, want %q", gateIssue.ID, "mol-test-gate.gate-await-ci")
	}
	if gateIssue.AwaitType != "gh:run" {
		t.Errorf("Gate AwaitType = %q, want %q", gateIssue.AwaitType, "gh:run")
	}
	if gateIssue.AwaitID != "ci-workflow" {
		t.Errorf("Gate AwaitID = %q, want %q", gateIssue.AwaitID, "ci-workflow")
	}
}

// TestCookFormulaToSubgraph_GateDependencies tests that step depends on its gate
func TestCookFormulaToSubgraph_GateDependencies(t *testing.T) {
	f := &formula.Formula{
		Formula:     "mol-gate-deps",
		Description: "Test gate dependencies",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{
				ID:    "await-approval",
				Title: "Wait for approval",
				Gate: &formula.Gate{
					Type:    "human",
					Timeout: "24h",
				},
			},
		},
	}

	subgraph, err := cookFormulaToSubgraph(f, "mol-gate-deps")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}

	// Find the blocking dependency: step -> gate
	stepID := "mol-gate-deps.await-approval"
	gateID := "mol-gate-deps.gate-await-approval"

	var foundBlockingDep bool
	for _, dep := range subgraph.Dependencies {
		if dep.IssueID == stepID && dep.DependsOnID == gateID && dep.Type == "blocks" {
			foundBlockingDep = true
			break
		}
	}

	if !foundBlockingDep {
		t.Error("Expected blocking dependency from step to gate not found")
		t.Log("Dependencies found:")
		for _, dep := range subgraph.Dependencies {
			t.Logf("  %s -> %s (%s)", dep.IssueID, dep.DependsOnID, dep.Type)
		}
	}
}

// TestCookFormulaToSubgraph_GateParentChild tests that gate is a child of the parent
func TestCookFormulaToSubgraph_GateParentChild(t *testing.T) {
	f := &formula.Formula{
		Formula:     "mol-gate-parent",
		Description: "Test gate parent-child relationship",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{
				ID:    "gated-step",
				Title: "Gated step",
				Gate: &formula.Gate{
					Type: "mail",
				},
			},
		},
	}

	subgraph, err := cookFormulaToSubgraph(f, "mol-gate-parent")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}

	// Find the parent-child dependency: gate -> root
	gateID := "mol-gate-parent.gate-gated-step"
	rootID := "mol-gate-parent"

	var foundParentChildDep bool
	for _, dep := range subgraph.Dependencies {
		if dep.IssueID == gateID && dep.DependsOnID == rootID && dep.Type == "parent-child" {
			foundParentChildDep = true
			break
		}
	}

	if !foundParentChildDep {
		t.Error("Expected parent-child dependency for gate not found")
		t.Log("Dependencies found:")
		for _, dep := range subgraph.Dependencies {
			t.Logf("  %s -> %s (%s)", dep.IssueID, dep.DependsOnID, dep.Type)
		}
	}
}

// =============================================================================
// Decision Bead Tests (hq-946577.19: Decision gates created during cook)
// =============================================================================

// TestCreateDecisionIssue tests that createDecisionIssue creates proper decision issues
func TestCreateDecisionIssue(t *testing.T) {
	tests := []struct {
		name        string
		step        *formula.Step
		parentID    string
		wantID      string
		wantTitle   string
		wantPrompt  string
		wantDefault string
	}{
		{
			name: "basic decision with default",
			step: &formula.Step{
				ID:    "deploy-strategy",
				Title: "Choose deployment strategy",
				Decision: &formula.DecisionConfig{
					Prompt: "Select deployment strategy:",
					Options: []formula.DecisionOption{
						{ID: "staged", Short: "Staged", Label: "Staged rollout"},
						{ID: "direct", Short: "Direct", Label: "Direct deployment"},
					},
					Default: "staged",
					Timeout: "48h",
				},
			},
			parentID:    "mol-release",
			wantID:      "mol-release.decision-deploy-strategy",
			wantTitle:   "Select deployment strategy:",
			wantPrompt:  "Select deployment strategy:",
			wantDefault: "staged",
		},
		{
			name: "decision without default",
			step: &formula.Step{
				ID:    "approve",
				Title: "Approval required",
				Decision: &formula.DecisionConfig{
					Prompt: "Approve for production?",
					Options: []formula.DecisionOption{
						{ID: "yes", Short: "Yes", Label: "Yes, approve"},
						{ID: "no", Short: "No", Label: "No, reject"},
					},
				},
			},
			parentID:    "mol-feature",
			wantID:      "mol-feature.decision-approve",
			wantTitle:   "Approve for production?",
			wantPrompt:  "Approve for production?",
			wantDefault: "",
		},
		{
			name: "decision with long prompt (truncated title)",
			step: &formula.Step{
				ID:    "long-prompt",
				Title: "Long prompt decision",
				Decision: &formula.DecisionConfig{
					Prompt: "This is a very long prompt that exceeds the 100 character limit for titles and should be truncated to fit within the display constraints of issue titles",
					Options: []formula.DecisionOption{
						{ID: "a", Label: "Option A"},
						{ID: "b", Label: "Option B"},
					},
				},
			},
			parentID:   "mol-test",
			wantID:     "mol-test.decision-long-prompt",
			wantTitle:  "This is a very long prompt that exceeds the 100 character limit for titles and should be truncate...",
			wantPrompt: "This is a very long prompt that exceeds the 100 character limit for titles and should be truncated to fit within the display constraints of issue titles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue, dp := createDecisionIssue(tt.step, tt.parentID)

			if issue == nil {
				t.Fatal("createDecisionIssue returned nil issue")
			}
			if dp == nil {
				t.Fatal("createDecisionIssue returned nil decision point")
			}

			if issue.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", issue.ID, tt.wantID)
			}
			if issue.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", issue.Title, tt.wantTitle)
			}
			if issue.AwaitType != "decision" {
				t.Errorf("AwaitType = %q, want %q", issue.AwaitType, "decision")
			}
			if issue.IssueType != "gate" {
				t.Errorf("IssueType = %q, want %q", issue.IssueType, "gate")
			}
			if !issue.IsTemplate {
				t.Error("IsTemplate should be true")
			}

			// Check decision point
			if dp.IssueID != tt.wantID {
				t.Errorf("DecisionPoint.IssueID = %q, want %q", dp.IssueID, tt.wantID)
			}
			if dp.Prompt != tt.wantPrompt {
				t.Errorf("DecisionPoint.Prompt = %q, want %q", dp.Prompt, tt.wantPrompt)
			}
			if dp.DefaultOption != tt.wantDefault {
				t.Errorf("DecisionPoint.DefaultOption = %q, want %q", dp.DefaultOption, tt.wantDefault)
			}
		})
	}
}

// TestCreateDecisionIssue_NilDecision tests that nil Decision returns nil
func TestCreateDecisionIssue_NilDecision(t *testing.T) {
	step := &formula.Step{
		ID:       "no-decision",
		Title:    "Step without decision",
		Decision: nil,
	}

	issue, dp := createDecisionIssue(step, "mol-test")
	if issue != nil {
		t.Errorf("Expected nil issue for step without Decision, got %+v", issue)
	}
	if dp != nil {
		t.Errorf("Expected nil decision point for step without Decision, got %+v", dp)
	}
}

// TestCreateDecisionIssue_Timeout tests that timeout is parsed correctly
func TestCreateDecisionIssue_Timeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		wantMinutes int
	}{
		{"1 hour", "1h", 60},
		{"24 hours", "24h", 1440},
		{"48 hours", "48h", 2880},
		{"invalid timeout", "invalid", 0},
		{"empty timeout", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &formula.Step{
				ID:    "timed-decision",
				Title: "Timed decision",
				Decision: &formula.DecisionConfig{
					Prompt: "Choose:",
					Options: []formula.DecisionOption{
						{ID: "a", Label: "A"},
						{ID: "b", Label: "B"},
					},
					Timeout: tt.timeout,
				},
			}

			issue, _ := createDecisionIssue(step, "mol-test")
			gotMinutes := int(issue.Timeout.Minutes())

			if gotMinutes != tt.wantMinutes {
				t.Errorf("Timeout minutes = %d, want %d", gotMinutes, tt.wantMinutes)
			}
		})
	}
}

// TestCookFormulaToSubgraph_DecisionBeads tests that decision beads are created in subgraph
func TestCookFormulaToSubgraph_DecisionBeads(t *testing.T) {
	f := &formula.Formula{
		Formula:     "mol-test-decision",
		Description: "Test decision creation",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{
				ID:    "prepare",
				Title: "Prepare release",
			},
			{
				ID:    "choose-strategy",
				Title: "Choose strategy",
				Decision: &formula.DecisionConfig{
					Prompt: "Select deployment strategy:",
					Options: []formula.DecisionOption{
						{ID: "staged", Short: "Staged", Label: "Staged rollout"},
						{ID: "direct", Short: "Direct", Label: "Direct deployment"},
					},
					Default: "staged",
				},
			},
			{
				ID:        "deploy",
				Title:     "Deploy",
				DependsOn: []string{"choose-strategy"},
			},
		},
	}

	subgraph, err := cookFormulaToSubgraph(f, "mol-test-decision")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}

	// Should have: root + 3 steps + 1 decision = 5 issues
	if len(subgraph.Issues) != 5 {
		t.Errorf("Expected 5 issues, got %d", len(subgraph.Issues))
		for _, issue := range subgraph.Issues {
			t.Logf("  Issue: %s (%s)", issue.ID, issue.IssueType)
		}
	}

	// Find the decision issue
	var decisionIssue *types.Issue
	for _, issue := range subgraph.Issues {
		if issue.AwaitType == "decision" {
			decisionIssue = issue
			break
		}
	}

	if decisionIssue == nil {
		t.Fatal("Decision issue not found in subgraph")
	}

	if decisionIssue.ID != "mol-test-decision.decision-choose-strategy" {
		t.Errorf("Decision ID = %q, want %q", decisionIssue.ID, "mol-test-decision.decision-choose-strategy")
	}
	if decisionIssue.IssueType != "gate" {
		t.Errorf("Decision IssueType = %q, want %q", decisionIssue.IssueType, "gate")
	}
}

// TestCookFormulaToSubgraph_DecisionDependencies tests that step depends on its decision
func TestCookFormulaToSubgraph_DecisionDependencies(t *testing.T) {
	f := &formula.Formula{
		Formula:     "mol-decision-deps",
		Description: "Test decision dependencies",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{
				ID:    "await-approval",
				Title: "Wait for approval",
				Decision: &formula.DecisionConfig{
					Prompt: "Approve?",
					Options: []formula.DecisionOption{
						{ID: "yes", Label: "Yes"},
						{ID: "no", Label: "No"},
					},
				},
			},
		},
	}

	subgraph, err := cookFormulaToSubgraph(f, "mol-decision-deps")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}

	// Find the blocking dependency: step -> decision
	stepID := "mol-decision-deps.await-approval"
	decisionID := "mol-decision-deps.decision-await-approval"

	var foundBlockingDep bool
	for _, dep := range subgraph.Dependencies {
		if dep.IssueID == stepID && dep.DependsOnID == decisionID && dep.Type == "blocks" {
			foundBlockingDep = true
			break
		}
	}

	if !foundBlockingDep {
		t.Error("Expected blocking dependency from step to decision not found")
		t.Log("Dependencies found:")
		for _, dep := range subgraph.Dependencies {
			t.Logf("  %s -> %s (%s)", dep.IssueID, dep.DependsOnID, dep.Type)
		}
	}
}

// TestCookFormulaToSubgraph_DecisionPoints tests that DecisionPoints are collected
func TestCookFormulaToSubgraph_DecisionPoints(t *testing.T) {
	f := &formula.Formula{
		Formula:     "mol-decision-points",
		Description: "Test decision point collection",
		Version:     1,
		Type:        formula.TypeWorkflow,
		Steps: []*formula.Step{
			{
				ID:    "decide-cache",
				Title: "Choose caching strategy",
				Decision: &formula.DecisionConfig{
					Prompt: "Select caching approach:",
					Options: []formula.DecisionOption{
						{ID: "redis", Short: "Redis", Label: "Use Redis"},
						{ID: "memory", Short: "Memory", Label: "In-memory cache"},
					},
					Default: "redis",
					Timeout: "24h",
				},
			},
			{
				ID:    "decide-auth",
				Title: "Choose auth method",
				Decision: &formula.DecisionConfig{
					Prompt: "Select authentication:",
					Options: []formula.DecisionOption{
						{ID: "oauth", Short: "OAuth", Label: "OAuth2"},
						{ID: "jwt", Short: "JWT", Label: "JWT tokens"},
					},
				},
			},
		},
	}

	subgraph, err := cookFormulaToSubgraph(f, "mol-decision-points")
	if err != nil {
		t.Fatalf("cookFormulaToSubgraph failed: %v", err)
	}

	// Should have 2 decision points
	if len(subgraph.DecisionPoints) != 2 {
		t.Errorf("Expected 2 decision points, got %d", len(subgraph.DecisionPoints))
	}

	// Check first decision point
	dp1 := subgraph.DecisionPoints[0]
	if dp1.Prompt != "Select caching approach:" {
		t.Errorf("DecisionPoint[0].Prompt = %q, want %q", dp1.Prompt, "Select caching approach:")
	}
	if dp1.DefaultOption != "redis" {
		t.Errorf("DecisionPoint[0].DefaultOption = %q, want %q", dp1.DefaultOption, "redis")
	}

	// Check second decision point
	dp2 := subgraph.DecisionPoints[1]
	if dp2.Prompt != "Select authentication:" {
		t.Errorf("DecisionPoint[1].Prompt = %q, want %q", dp2.Prompt, "Select authentication:")
	}
	if dp2.DefaultOption != "" {
		t.Errorf("DecisionPoint[1].DefaultOption = %q, want empty", dp2.DefaultOption)
	}
}

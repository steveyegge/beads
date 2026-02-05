package formula

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// roundTrip is a test helper that serializes a Formula to an Issue and back.
func roundTrip(t *testing.T, f *Formula) *Formula {
	t.Helper()
	issue, _, err := FormulaToIssue(f, "bd-")
	if err != nil {
		t.Fatalf("FormulaToIssue failed: %v", err)
	}
	restored, err := IssueToFormula(issue)
	if err != nil {
		t.Fatalf("IssueToFormula failed: %v", err)
	}
	return restored
}

// jsonEqual compares two values by marshaling to JSON and comparing.
// This handles map ordering and pointer differences.
func jsonEqual(t *testing.T, field string, got, want interface{}) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got %s: %v", field, err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want %s: %v", field, err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("%s mismatch:\n  got:  %s\n  want: %s", field, gotJSON, wantJSON)
	}
}

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

// --- HIGH PRIORITY round-trip tests ---

func TestFormulaRoundTrip_Extends(t *testing.T) {
	original := &Formula{
		Formula: "mol-child",
		Version: 1,
		Type:    TypeWorkflow,
		Extends: []string{"parent-formula", "mixin-formula"},
		Steps:   []*Step{{ID: "s1", Title: "Step"}},
	}
	restored := roundTrip(t, original)
	if !reflect.DeepEqual(restored.Extends, original.Extends) {
		t.Errorf("Extends = %v, want %v", restored.Extends, original.Extends)
	}
}

func TestFormulaRoundTrip_Pointcuts(t *testing.T) {
	original := &Formula{
		Formula: "aspect-logging",
		Version: 1,
		Type:    TypeAspect,
		Pointcuts: []*Pointcut{
			{Glob: "*.implement"},
			{Type: "task"},
			{Label: "needs-review"},
			{Glob: "shiny.*", Type: "bug"},
		},
		Advice: []*AdviceRule{
			{
				Target: "*.implement",
				After: &AdviceStep{
					ID:    "log-{step.id}",
					Title: "Log completion of {step.id}",
					Type:  "task",
				},
			},
		},
	}
	restored := roundTrip(t, original)

	if len(restored.Pointcuts) != 4 {
		t.Fatalf("Pointcuts count = %d, want 4", len(restored.Pointcuts))
	}
	jsonEqual(t, "Pointcuts", restored.Pointcuts, original.Pointcuts)
	jsonEqual(t, "Advice", restored.Advice, original.Advice)
}

func TestFormulaRoundTrip_TypeExpansion(t *testing.T) {
	original := &Formula{
		Formula:     "exp-test-lint-build",
		Description: "Standard test + lint + build expansion",
		Version:     1,
		Type:        TypeExpansion,
		Template: []*Step{
			{ID: "{target}-test", Title: "Test {target}", Type: "task"},
			{ID: "{target}-lint", Title: "Lint {target}", DependsOn: []string{"{target}-test"}},
			{ID: "{target}-build", Title: "Build {target}", DependsOn: []string{"{target}-lint"}},
		},
	}
	restored := roundTrip(t, original)

	if restored.Type != TypeExpansion {
		t.Errorf("Type = %q, want %q", restored.Type, TypeExpansion)
	}
	if len(restored.Template) != 3 {
		t.Fatalf("Template count = %d, want 3", len(restored.Template))
	}
	jsonEqual(t, "Template", restored.Template, original.Template)
}

func TestFormulaRoundTrip_TypeAspect(t *testing.T) {
	original := &Formula{
		Formula: "aspect-security",
		Version: 1,
		Type:    TypeAspect,
		Pointcuts: []*Pointcut{
			{Glob: "*.implement"},
		},
		Advice: []*AdviceRule{
			{
				Target: "*.implement",
				Before: &AdviceStep{
					ID:    "pre-{step.id}-scan",
					Title: "Security scan before {step.id}",
					Args:  map[string]string{"scanner": "semgrep"},
				},
				After: &AdviceStep{
					ID:     "post-{step.id}-report",
					Title:  "Generate report for {step.id}",
					Output: map[string]string{"report_url": "output.url"},
				},
			},
			{
				Target: "review",
				Around: &AroundAdvice{
					Before: []*AdviceStep{{ID: "pre-review-gate", Title: "Pre-review gate"}},
					After:  []*AdviceStep{{ID: "post-review-sign", Title: "Sign off"}},
				},
			},
		},
	}
	restored := roundTrip(t, original)

	if restored.Type != TypeAspect {
		t.Errorf("Type = %q, want %q", restored.Type, TypeAspect)
	}
	jsonEqual(t, "Pointcuts", restored.Pointcuts, original.Pointcuts)
	jsonEqual(t, "Advice", restored.Advice, original.Advice)
}

func TestFormulaRoundTrip_StepComplexFields(t *testing.T) {
	prio := 2
	original := &Formula{
		Formula: "mol-complex-steps",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{
				ID:          "survey",
				Title:       "Survey workers",
				Description: "Find available workers",
				Type:        "task",
				Priority:    &prio,
				Labels:      []string{"infra", "survey"},
				Assignee:    "beads/crew/ops",
				Needs:       []string{},
				WaitsFor:    "all-children",
				Condition:   "{{run_survey}}",
				ExpandVars:  map[string]string{"region": "us-west-2"},
				OnComplete: &OnCompleteSpec{
					ForEach:  "output.polecats",
					Bond:     "mol-polecat-arm",
					Vars:     map[string]string{"name": "{item.name}", "rig": "{item.rig}"},
					Parallel: true,
				},
			},
			{
				ID:    "decide",
				Title: "Choose strategy",
				Decision: &DecisionConfig{
					Prompt: "Choose deployment strategy:",
					Options: []DecisionOption{
						{ID: "staged", Short: "Staged", Label: "Staged rollout (recommended)"},
						{ID: "direct", Short: "Direct", Label: "Direct deployment"},
					},
					Default: "staged",
					Timeout: "48h",
				},
			},
			{
				ID:    "iterate",
				Title: "Refine loop",
				Loop: &LoopSpec{
					Count: 3,
					Max:   5,
					Until: "quality == 'good'",
					Var:   "iteration",
					Body: []*Step{
						{ID: "refine-{iteration}", Title: "Refine pass {iteration}"},
					},
				},
			},
			{
				ID:    "conditional",
				Title: "Maybe run",
				Needs: []string{"survey"},
				RequiresSkills: []string{"go", "testing"},
			},
		},
	}
	restored := roundTrip(t, original)

	if len(restored.Steps) != 4 {
		t.Fatalf("Steps count = %d, want 4", len(restored.Steps))
	}

	// Step 0: survey - check all complex fields
	s := restored.Steps[0]
	if s.Type != "task" {
		t.Errorf("Step[0].Type = %q, want %q", s.Type, "task")
	}
	if s.Priority == nil || *s.Priority != 2 {
		t.Errorf("Step[0].Priority = %v, want 2", s.Priority)
	}
	jsonEqual(t, "Step[0].Labels", s.Labels, original.Steps[0].Labels)
	if s.Assignee != "beads/crew/ops" {
		t.Errorf("Step[0].Assignee = %q, want %q", s.Assignee, "beads/crew/ops")
	}
	if s.WaitsFor != "all-children" {
		t.Errorf("Step[0].WaitsFor = %q, want %q", s.WaitsFor, "all-children")
	}
	if s.Condition != "{{run_survey}}" {
		t.Errorf("Step[0].Condition = %q, want %q", s.Condition, "{{run_survey}}")
	}
	jsonEqual(t, "Step[0].ExpandVars", s.ExpandVars, original.Steps[0].ExpandVars)
	jsonEqual(t, "Step[0].OnComplete", s.OnComplete, original.Steps[0].OnComplete)

	// Step 1: decision
	jsonEqual(t, "Step[1].Decision", restored.Steps[1].Decision, original.Steps[1].Decision)

	// Step 2: loop
	jsonEqual(t, "Step[2].Loop", restored.Steps[2].Loop, original.Steps[2].Loop)

	// Step 3: needs + requires_skills
	jsonEqual(t, "Step[3].Needs", restored.Steps[3].Needs, original.Steps[3].Needs)
	jsonEqual(t, "Step[3].RequiresSkills", restored.Steps[3].RequiresSkills, original.Steps[3].RequiresSkills)
}

// --- MEDIUM PRIORITY round-trip tests ---

func TestFormulaRoundTrip_ComposeSubrules(t *testing.T) {
	original := &Formula{
		Formula: "mol-compose-full",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{ID: "s1", Title: "Step 1"},
			{ID: "s2", Title: "Step 2"},
			{ID: "s3", Title: "Step 3"},
		},
		Compose: &ComposeRules{
			BondPoints: []*BondPoint{
				{ID: "bp1", Description: "After step 1", AfterStep: "s1"},
				{ID: "bp2", BeforeStep: "s3", Parallel: true},
			},
			Hooks: []*Hook{
				{
					Trigger: "label:security",
					Attach:  "mol-security-scan",
					At:      "bp1",
					Vars:    map[string]string{"depth": "full"},
				},
			},
			Expand: []*ExpandRule{
				{Target: "s2", With: "exp-test-lint", Vars: map[string]string{"lang": "go"}},
			},
			Map: []*MapRule{
				{Select: "*.implement", With: "exp-standard-qa", Vars: map[string]string{"coverage": "80"}},
			},
			Branch: []*BranchRule{
				{From: "s1", Steps: []string{"s2", "s3"}, Join: "s3"},
			},
			Gate: []*GateRule{
				{Before: "s3", Condition: "tests.status == 'complete'"},
			},
			Aspects: []string{"security-audit", "logging"},
		},
	}
	restored := roundTrip(t, original)

	if restored.Compose == nil {
		t.Fatal("Compose should not be nil")
	}
	jsonEqual(t, "Compose.BondPoints", restored.Compose.BondPoints, original.Compose.BondPoints)
	jsonEqual(t, "Compose.Hooks", restored.Compose.Hooks, original.Compose.Hooks)
	jsonEqual(t, "Compose.Expand", restored.Compose.Expand, original.Compose.Expand)
	jsonEqual(t, "Compose.Map", restored.Compose.Map, original.Compose.Map)
	jsonEqual(t, "Compose.Branch", restored.Compose.Branch, original.Compose.Branch)
	jsonEqual(t, "Compose.Gate", restored.Compose.Gate, original.Compose.Gate)
	jsonEqual(t, "Compose.Aspects", restored.Compose.Aspects, original.Compose.Aspects)
}

func TestFormulaRoundTrip_VarDefCompleteness(t *testing.T) {
	original := &Formula{
		Formula: "mol-vars-full",
		Version: 1,
		Type:    TypeWorkflow,
		Vars: map[string]*VarDef{
			"env": {
				Description: "Target environment",
				Default:     "staging",
				Enum:        []string{"dev", "staging", "prod"},
				Type:        "string",
			},
			"port": {
				Description: "Service port",
				Required:    true,
				Pattern:     "^[0-9]{2,5}$",
				Type:        "int",
			},
			"verbose": {
				Description: "Enable verbose logging",
				Default:     "false",
				Type:        "bool",
			},
		},
		Steps: []*Step{{ID: "s1", Title: "Deploy"}},
	}
	restored := roundTrip(t, original)

	if len(restored.Vars) != 3 {
		t.Fatalf("Vars count = %d, want 3", len(restored.Vars))
	}

	for name, wantVar := range original.Vars {
		gotVar, ok := restored.Vars[name]
		if !ok {
			t.Errorf("Var %q missing after round-trip", name)
			continue
		}
		jsonEqual(t, "Vars["+name+"]", gotVar, wantVar)
	}
}

func TestFormulaRoundTrip_Minimal(t *testing.T) {
	original := &Formula{
		Formula: "mol-minimal",
		Version: 1,
		Type:    TypeWorkflow,
	}
	restored := roundTrip(t, original)

	if restored.Formula != "mol-minimal" {
		t.Errorf("Formula = %q, want %q", restored.Formula, "mol-minimal")
	}
	if restored.Version != 1 {
		t.Errorf("Version = %d, want 1", restored.Version)
	}
	if restored.Type != TypeWorkflow {
		t.Errorf("Type = %q, want %q", restored.Type, TypeWorkflow)
	}
	if restored.Description != "" {
		t.Errorf("Description = %q, want empty", restored.Description)
	}
	if len(restored.Steps) != 0 {
		t.Errorf("Steps count = %d, want 0", len(restored.Steps))
	}
	if len(restored.Vars) != 0 {
		t.Errorf("Vars count = %d, want 0", len(restored.Vars))
	}
	if restored.Compose != nil {
		t.Error("Compose should be nil")
	}
	if len(restored.Extends) != 0 {
		t.Errorf("Extends count = %d, want 0", len(restored.Extends))
	}
}

func TestFormulaRoundTrip_KitchenSink(t *testing.T) {
	prio0 := 0
	prio3 := 3
	original := &Formula{
		Formula:        "mol-kitchen-sink",
		Description:    "Every field populated simultaneously",
		Version:        2,
		Type:           TypeWorkflow,
		Phase:          "liquid",
		Extends:        []string{"base-workflow", "mixin-logging"},
		RequiresSkills: []string{"go", "docker", "k8s"},
		Vars: map[string]*VarDef{
			"component": {Description: "Component", Required: true, Type: "string"},
			"env":       {Default: "staging", Enum: []string{"dev", "staging", "prod"}, Pattern: "^[a-z]+$"},
		},
		Steps: []*Step{
			{
				ID:          "design",
				Title:       "Design {{component}}",
				Description: "Design the component",
				Type:        "task",
				Priority:    &prio0,
				Labels:      []string{"design", "frontend"},
				Assignee:    "team/design",
				Condition:   "{{needs_design}}",
			},
			{
				ID:        "implement",
				Title:     "Implement {{component}}",
				Type:      "task",
				Priority:  &prio3,
				DependsOn: []string{"design"},
				Needs:     []string{"design"},
				Labels:    []string{"code"},
				Expand:    "exp-test-lint",
				ExpandVars: map[string]string{"lang": "go"},
				Children: []*Step{
					{ID: "impl-core", Title: "Core logic"},
					{ID: "impl-tests", Title: "Tests", DependsOn: []string{"impl-core"}},
				},
				Gate: &Gate{Type: "human", ID: "review-gate", Timeout: "24h"},
				Decision: &DecisionConfig{
					Prompt:  "Approve implementation?",
					Options: []DecisionOption{
						{ID: "approve", Short: "OK", Label: "Approve"},
						{ID: "reject", Short: "No", Label: "Reject"},
					},
					Default: "approve",
					Timeout: "48h",
				},
				Loop: &LoopSpec{
					Count: 2,
					Until: "quality == 'good'",
					Max:   5,
					Range: "1..3",
					Var:   "iter",
					Body:  []*Step{{ID: "refine-{iter}", Title: "Refine {iter}"}},
				},
				OnComplete: &OnCompleteSpec{
					ForEach:    "output.artifacts",
					Bond:       "mol-deploy",
					Vars:       map[string]string{"artifact": "{item.path}"},
					Sequential: true,
				},
				WaitsFor:       "all-children",
				RequiresSkills: []string{"go"},
			},
		},
		Compose: &ComposeRules{
			BondPoints: []*BondPoint{
				{ID: "after-design", AfterStep: "design", Description: "After design"},
			},
			Hooks: []*Hook{
				{Trigger: "type:bug", Attach: "mol-triage", Vars: map[string]string{"urgency": "high"}},
			},
			Expand:  []*ExpandRule{{Target: "implement", With: "exp-qa", Vars: map[string]string{"strict": "true"}}},
			Map:     []*MapRule{{Select: "*.implement", With: "exp-lint"}},
			Branch:  []*BranchRule{{From: "design", Steps: []string{"implement"}, Join: "implement"}},
			Gate:    []*GateRule{{Before: "implement", Condition: "design.status == 'done'"}},
			Aspects: []string{"security-audit"},
		},
		Advice: []*AdviceRule{
			{
				Target: "*.implement",
				Before: &AdviceStep{ID: "pre-impl", Title: "Pre-impl check"},
			},
		},
		Pointcuts: []*Pointcut{
			{Glob: "*.implement", Type: "task"},
		},
	}

	restored := roundTrip(t, original)

	// Compare every top-level field
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
	jsonEqual(t, "Extends", restored.Extends, original.Extends)
	jsonEqual(t, "RequiresSkills", restored.RequiresSkills, original.RequiresSkills)
	jsonEqual(t, "Vars", restored.Vars, original.Vars)
	jsonEqual(t, "Steps", restored.Steps, original.Steps)
	jsonEqual(t, "Compose", restored.Compose, original.Compose)
	jsonEqual(t, "Advice", restored.Advice, original.Advice)
	jsonEqual(t, "Pointcuts", restored.Pointcuts, original.Pointcuts)
}

func TestFormulaRoundTrip_Unicode(t *testing.T) {
	original := &Formula{
		Formula:     "mol-ユニコード-test",
		Description: "Formule avec des caractères spéciaux: é, ñ, ü, 中文, 日本語",
		Version:     1,
		Type:        TypeWorkflow,
		Steps: []*Step{
			{ID: "étape-1", Title: "Première étape"},
			{ID: "步骤-2", Title: "第二步"},
		},
		Vars: map[string]*VarDef{
			"名前": {Description: "名前を入力してください", Default: "世界"},
		},
	}
	restored := roundTrip(t, original)

	if restored.Description != original.Description {
		t.Errorf("Description = %q, want %q", restored.Description, original.Description)
	}
	if len(restored.Steps) != 2 {
		t.Fatalf("Steps count = %d, want 2", len(restored.Steps))
	}
	if restored.Steps[0].Title != "Première étape" {
		t.Errorf("Step[0].Title = %q, want %q", restored.Steps[0].Title, "Première étape")
	}
	if restored.Steps[1].Title != "第二步" {
		t.Errorf("Step[1].Title = %q, want %q", restored.Steps[1].Title, "第二步")
	}

	// Verify slug generation strips non-ASCII and collapses hyphens
	slug := formulaNameToSlug(original.Formula)
	if slug != "mol-test" {
		t.Errorf("slug = %q, want %q", slug, "mol-test")
	}
}

func TestFormulaRoundTrip_SourceFieldsNotInMetadata(t *testing.T) {
	original := &Formula{
		Formula: "mol-source-test",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{
				ID:             "s1",
				Title:          "Step with source tracing",
				SourceFormula:  "parent-formula",
				SourceLocation: "steps[0]",
			},
		},
	}

	issue, _, err := FormulaToIssue(original, "bd-")
	if err != nil {
		t.Fatalf("FormulaToIssue failed: %v", err)
	}

	// Verify SourceFormula and SourceLocation are NOT in the metadata JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(issue.Metadata, &raw); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}

	stepsRaw, ok := raw["steps"].([]interface{})
	if !ok || len(stepsRaw) == 0 {
		t.Fatal("steps not found in metadata")
	}
	stepMap, ok := stepsRaw[0].(map[string]interface{})
	if !ok {
		t.Fatal("step[0] is not a map")
	}

	if _, exists := stepMap["source_formula"]; exists {
		t.Error("SourceFormula (json:\"-\") should NOT appear in metadata, but found source_formula key")
	}
	if _, exists := stepMap["SourceFormula"]; exists {
		t.Error("SourceFormula (json:\"-\") should NOT appear in metadata")
	}
	if _, exists := stepMap["source_location"]; exists {
		t.Error("SourceLocation (json:\"-\") should NOT appear in metadata, but found source_location key")
	}
	if _, exists := stepMap["SourceLocation"]; exists {
		t.Error("SourceLocation (json:\"-\") should NOT appear in metadata")
	}

	// Also verify that after round-trip, these fields are zero-valued (not preserved)
	restored, err := IssueToFormula(issue)
	if err != nil {
		t.Fatalf("IssueToFormula failed: %v", err)
	}
	if restored.Steps[0].SourceFormula != "" {
		t.Errorf("SourceFormula = %q, want empty (json:\"-\" fields should not round-trip)", restored.Steps[0].SourceFormula)
	}
	if restored.Steps[0].SourceLocation != "" {
		t.Errorf("SourceLocation = %q, want empty (json:\"-\" fields should not round-trip)", restored.Steps[0].SourceLocation)
	}
}

func TestFormulaRoundTrip_LoopRange(t *testing.T) {
	original := &Formula{
		Formula: "mol-loop-range",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{
				ID:    "hanoi",
				Title: "Tower of Hanoi",
				Loop: &LoopSpec{
					Range: "1..2^{disks}",
					Var:   "move_num",
					Body: []*Step{
						{ID: "move-{move_num}", Title: "Move {move_num}"},
					},
				},
			},
		},
	}
	restored := roundTrip(t, original)

	loop := restored.Steps[0].Loop
	if loop == nil {
		t.Fatal("Loop should not be nil")
	}
	if loop.Range != "1..2^{disks}" {
		t.Errorf("Loop.Range = %q, want %q", loop.Range, "1..2^{disks}")
	}
	if loop.Var != "move_num" {
		t.Errorf("Loop.Var = %q, want %q", loop.Var, "move_num")
	}
	if len(loop.Body) != 1 {
		t.Fatalf("Loop.Body count = %d, want 1", len(loop.Body))
	}
}

func TestFormulaRoundTrip_OnCompleteSequential(t *testing.T) {
	original := &Formula{
		Formula: "mol-oncomplete-seq",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{
				ID:    "discover",
				Title: "Discover items",
				OnComplete: &OnCompleteSpec{
					ForEach:    "output.items",
					Bond:       "mol-process-item",
					Vars:       map[string]string{"item_id": "{item.id}", "index": "{index}"},
					Sequential: true,
				},
			},
		},
	}
	restored := roundTrip(t, original)

	oc := restored.Steps[0].OnComplete
	if oc == nil {
		t.Fatal("OnComplete should not be nil")
	}
	if !oc.Sequential {
		t.Error("OnComplete.Sequential should be true")
	}
	if oc.Parallel {
		t.Error("OnComplete.Parallel should be false")
	}
	jsonEqual(t, "OnComplete.Vars", oc.Vars, original.Steps[0].OnComplete.Vars)
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

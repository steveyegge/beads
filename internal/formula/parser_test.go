package formula

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_BasicFormula(t *testing.T) {
	yaml := `
formula: mol-test
description: Test workflow
version: 1
type: workflow
vars:
  component:
    description: Component name
    required: true
  framework:
    description: Target framework
    default: react
    enum: [react, vue, angular]
steps:
  - id: design
    title: "Design {{component}}"
    type: task
    priority: 1
  - id: implement
    title: "Implement {{component}}"
    type: task
    depends_on: [design]
  - id: test
    title: "Test {{component}} with {{framework}}"
    type: task
    depends_on: [implement]
`
	p := NewParser()
	formula, err := p.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check basic fields
	if formula.Formula != "mol-test" {
		t.Errorf("Formula = %q, want mol-test", formula.Formula)
	}
	if formula.Description != "Test workflow" {
		t.Errorf("Description = %q, want 'Test workflow'", formula.Description)
	}
	if formula.Version != 1 {
		t.Errorf("Version = %d, want 1", formula.Version)
	}
	if formula.Type != TypeWorkflow {
		t.Errorf("Type = %q, want workflow", formula.Type)
	}

	// Check vars
	if len(formula.Vars) != 2 {
		t.Fatalf("len(Vars) = %d, want 2", len(formula.Vars))
	}
	if v := formula.Vars["component"]; v == nil || !v.Required {
		t.Error("component var should be required")
	}
	if v := formula.Vars["framework"]; v == nil || v.Default != "react" {
		t.Error("framework var should have default 'react'")
	}
	if v := formula.Vars["framework"]; v == nil || len(v.Enum) != 3 {
		t.Error("framework var should have 3 enum values")
	}

	// Check steps
	if len(formula.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3", len(formula.Steps))
	}
	if formula.Steps[0].ID != "design" {
		t.Errorf("Steps[0].ID = %q, want 'design'", formula.Steps[0].ID)
	}
	if formula.Steps[1].DependsOn[0] != "design" {
		t.Errorf("Steps[1].DependsOn = %v, want [design]", formula.Steps[1].DependsOn)
	}
}

func TestValidate_ValidFormula(t *testing.T) {
	formula := &Formula{
		Formula: "mol-valid",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{ID: "step1", Title: "Step 1"},
			{ID: "step2", Title: "Step 2", DependsOn: []string{"step1"}},
		},
	}

	if err := formula.Validate(); err != nil {
		t.Errorf("Validate failed for valid formula: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	formula := &Formula{
		Version: 1,
		Type:    TypeWorkflow,
		Steps:   []*Step{{ID: "step1", Title: "Step 1"}},
	}

	err := formula.Validate()
	if err == nil {
		t.Error("Validate should fail for formula without name")
	}
}

func TestValidate_DuplicateStepID(t *testing.T) {
	formula := &Formula{
		Formula: "mol-dup",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{ID: "step1", Title: "Step 1"},
			{ID: "step1", Title: "Step 1 again"}, // duplicate
		},
	}

	err := formula.Validate()
	if err == nil {
		t.Error("Validate should fail for duplicate step IDs")
	}
}

func TestValidate_InvalidDependency(t *testing.T) {
	formula := &Formula{
		Formula: "mol-bad-dep",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{ID: "step1", Title: "Step 1", DependsOn: []string{"nonexistent"}},
		},
	}

	err := formula.Validate()
	if err == nil {
		t.Error("Validate should fail for dependency on nonexistent step")
	}
}

func TestValidate_RequiredWithDefault(t *testing.T) {
	formula := &Formula{
		Formula: "mol-bad-var",
		Version: 1,
		Type:    TypeWorkflow,
		Vars: map[string]*VarDef{
			"bad": {Required: true, Default: "value"}, // can't have both
		},
		Steps: []*Step{{ID: "step1", Title: "Step 1"}},
	}

	err := formula.Validate()
	if err == nil {
		t.Error("Validate should fail for required var with default")
	}
}

func TestValidate_InvalidPriority(t *testing.T) {
	p := 10 // invalid: must be 0-4
	formula := &Formula{
		Formula: "mol-bad-priority",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{ID: "step1", Title: "Step 1", Priority: &p},
		},
	}

	err := formula.Validate()
	if err == nil {
		t.Error("Validate should fail for priority > 4")
	}
}

func TestValidate_ChildSteps(t *testing.T) {
	formula := &Formula{
		Formula: "mol-children",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{
				ID:    "epic1",
				Title: "Epic 1",
				Children: []*Step{
					{ID: "child1", Title: "Child 1"},
					{ID: "child2", Title: "Child 2", DependsOn: []string{"child1"}},
				},
			},
		},
	}

	if err := formula.Validate(); err != nil {
		t.Errorf("Validate failed for valid nested formula: %v", err)
	}
}

func TestValidate_BondPoints(t *testing.T) {
	formula := &Formula{
		Formula: "mol-compose",
		Version: 1,
		Type:    TypeWorkflow,
		Steps: []*Step{
			{ID: "step1", Title: "Step 1"},
			{ID: "step2", Title: "Step 2"},
		},
		Compose: &ComposeRules{
			BondPoints: []*BondPoint{
				{ID: "after-step1", AfterStep: "step1"},
				{ID: "before-step2", BeforeStep: "step2"},
			},
		},
	}

	if err := formula.Validate(); err != nil {
		t.Errorf("Validate failed for valid bond points: %v", err)
	}
}

func TestValidate_BondPointBothAnchors(t *testing.T) {
	formula := &Formula{
		Formula: "mol-bad-bond",
		Version: 1,
		Type:    TypeWorkflow,
		Steps:   []*Step{{ID: "step1", Title: "Step 1"}},
		Compose: &ComposeRules{
			BondPoints: []*BondPoint{
				{ID: "bad", AfterStep: "step1", BeforeStep: "step1"}, // can't have both
			},
		},
	}

	err := formula.Validate()
	if err == nil {
		t.Error("Validate should fail for bond point with both after_step and before_step")
	}
}

func TestExtractVariables(t *testing.T) {
	formula := &Formula{
		Formula:     "mol-vars",
		Description: "Build {{project}} for {{env}}",
		Steps: []*Step{
			{ID: "s1", Title: "Deploy {{project}} to {{env}}"},
			{ID: "s2", Title: "Notify {{owner}}"},
		},
	}

	vars := ExtractVariables(formula)
	want := map[string]bool{"project": true, "env": true, "owner": true}

	if len(vars) != len(want) {
		t.Errorf("ExtractVariables found %d vars, want %d", len(vars), len(want))
	}
	for _, v := range vars {
		if !want[v] {
			t.Errorf("Unexpected variable: %q", v)
		}
	}
}

func TestSubstitute(t *testing.T) {
	tests := []struct {
		input string
		vars  map[string]string
		want  string
	}{
		{
			input: "Deploy {{project}} to {{env}}",
			vars:  map[string]string{"project": "myapp", "env": "prod"},
			want:  "Deploy myapp to prod",
		},
		{
			input: "{{name}} version {{version}}",
			vars:  map[string]string{"name": "beads"},
			want:  "beads version {{version}}", // unresolved kept
		},
		{
			input: "No variables here",
			vars:  map[string]string{"unused": "value"},
			want:  "No variables here",
		},
	}

	for _, tt := range tests {
		got := Substitute(tt.input, tt.vars)
		if got != tt.want {
			t.Errorf("Substitute(%q, %v) = %q, want %q", tt.input, tt.vars, got, tt.want)
		}
	}
}

func TestValidateVars(t *testing.T) {
	formula := &Formula{
		Formula: "mol-vars",
		Vars: map[string]*VarDef{
			"required_var": {Required: true},
			"enum_var":     {Enum: []string{"a", "b", "c"}},
			"pattern_var":  {Pattern: `^[a-z]+$`},
			"optional_var": {Default: "default"},
		},
	}

	tests := []struct {
		name    string
		values  map[string]string
		wantErr bool
	}{
		{
			name:    "missing required",
			values:  map[string]string{},
			wantErr: true,
		},
		{
			name:    "all provided",
			values:  map[string]string{"required_var": "value"},
			wantErr: false,
		},
		{
			name:    "valid enum",
			values:  map[string]string{"required_var": "x", "enum_var": "a"},
			wantErr: false,
		},
		{
			name:    "invalid enum",
			values:  map[string]string{"required_var": "x", "enum_var": "invalid"},
			wantErr: true,
		},
		{
			name:    "valid pattern",
			values:  map[string]string{"required_var": "x", "pattern_var": "abc"},
			wantErr: false,
		},
		{
			name:    "invalid pattern",
			values:  map[string]string{"required_var": "x", "pattern_var": "123"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVars(formula, tt.values)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVars() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	formula := &Formula{
		Formula: "mol-defaults",
		Vars: map[string]*VarDef{
			"with_default":    {Default: "default_value"},
			"without_default": {},
		},
	}

	values := map[string]string{"without_default": "provided"}
	result := ApplyDefaults(formula, values)

	if result["with_default"] != "default_value" {
		t.Errorf("with_default = %q, want 'default_value'", result["with_default"])
	}
	if result["without_default"] != "provided" {
		t.Errorf("without_default = %q, want 'provided'", result["without_default"])
	}
}

func TestParseFile_AndResolve(t *testing.T) {
	// Create temp directory with test formulas
	dir := t.TempDir()
	formulaDir := filepath.Join(dir, ".beads", "formulas")
	if err := os.MkdirAll(formulaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write parent formula
	parent := `
formula: base-workflow
version: 1
type: workflow
vars:
  project:
    description: Project name
    required: true
steps:
  - id: init
    title: "Initialize {{project}}"
`
	if err := os.WriteFile(filepath.Join(formulaDir, "base-workflow.formula.yaml"), []byte(parent), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	// Write child formula that extends parent
	child := `
formula: extended-workflow
version: 1
type: workflow
extends:
  - base-workflow
vars:
  env:
    default: dev
steps:
  - id: deploy
    title: "Deploy {{project}} to {{env}}"
    depends_on: [init]
`
	childPath := filepath.Join(formulaDir, "extended-workflow.formula.yaml")
	if err := os.WriteFile(childPath, []byte(child), 0644); err != nil {
		t.Fatalf("write child: %v", err)
	}

	// Parse and resolve
	p := NewParser(formulaDir)
	formula, err := p.ParseFile(childPath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	resolved, err := p.Resolve(formula)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Check inheritance
	if len(resolved.Vars) != 2 {
		t.Errorf("len(Vars) = %d, want 2 (inherited + child)", len(resolved.Vars))
	}
	if resolved.Vars["project"] == nil {
		t.Error("inherited var 'project' not found")
	}
	if resolved.Vars["env"] == nil {
		t.Error("child var 'env' not found")
	}

	// Check steps (parent + child)
	if len(resolved.Steps) != 2 {
		t.Errorf("len(Steps) = %d, want 2", len(resolved.Steps))
	}
	if resolved.Steps[0].ID != "init" {
		t.Errorf("Steps[0].ID = %q, want 'init' (inherited)", resolved.Steps[0].ID)
	}
	if resolved.Steps[1].ID != "deploy" {
		t.Errorf("Steps[1].ID = %q, want 'deploy' (child)", resolved.Steps[1].ID)
	}
}

func TestResolve_CircularExtends(t *testing.T) {
	dir := t.TempDir()
	formulaDir := filepath.Join(dir, ".beads", "formulas")
	if err := os.MkdirAll(formulaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write formulas that extend each other (cycle)
	formulaA := `
formula: cycle-a
version: 1
type: workflow
extends: [cycle-b]
steps: [{id: a, title: A}]
`
	formulaB := `
formula: cycle-b
version: 1
type: workflow
extends: [cycle-a]
steps: [{id: b, title: B}]
`
	if err := os.WriteFile(filepath.Join(formulaDir, "cycle-a.formula.yaml"), []byte(formulaA), 0644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(formulaDir, "cycle-b.formula.yaml"), []byte(formulaB), 0644); err != nil {
		t.Fatalf("write b: %v", err)
	}

	p := NewParser(formulaDir)
	formula, err := p.ParseFile(filepath.Join(formulaDir, "cycle-a.formula.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	_, err = p.Resolve(formula)
	if err == nil {
		t.Error("Resolve should fail for circular extends")
	}
}

func TestGetStepByID(t *testing.T) {
	formula := &Formula{
		Formula: "mol-nested",
		Steps: []*Step{
			{
				ID:    "epic1",
				Title: "Epic 1",
				Children: []*Step{
					{ID: "child1", Title: "Child 1"},
					{
						ID:    "child2",
						Title: "Child 2",
						Children: []*Step{
							{ID: "grandchild", Title: "Grandchild"},
						},
					},
				},
			},
			{ID: "step2", Title: "Step 2"},
		},
	}

	tests := []struct {
		id   string
		want string
	}{
		{"epic1", "Epic 1"},
		{"child1", "Child 1"},
		{"grandchild", "Grandchild"},
		{"step2", "Step 2"},
		{"nonexistent", ""},
	}

	for _, tt := range tests {
		step := formula.GetStepByID(tt.id)
		if tt.want == "" {
			if step != nil {
				t.Errorf("GetStepByID(%q) = %v, want nil", tt.id, step)
			}
		} else {
			if step == nil || step.Title != tt.want {
				t.Errorf("GetStepByID(%q).Title = %v, want %q", tt.id, step, tt.want)
			}
		}
	}
}

func TestFormulaType_IsValid(t *testing.T) {
	tests := []struct {
		t    FormulaType
		want bool
	}{
		{TypeWorkflow, true},
		{TypeExpansion, true},
		{TypeAspect, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := tt.t.IsValid(); got != tt.want {
			t.Errorf("%q.IsValid() = %v, want %v", tt.t, got, tt.want)
		}
	}
}

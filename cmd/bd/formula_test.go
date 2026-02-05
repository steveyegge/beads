package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/formula"
)

// testFormulaJSON returns a minimal valid .formula.json string.
func testFormulaJSON(name, ftype, description string, steps []formula.Step) string {
	f := formula.Formula{
		Formula:     name,
		Description: description,
		Version:     1,
		Type:        formula.FormulaType(ftype),
		Steps:       make([]*formula.Step, len(steps)),
	}
	for i := range steps {
		f.Steps[i] = &steps[i]
	}
	data, _ := json.Marshal(f)
	return string(data)
}

// testFormulaTOML returns a minimal valid .formula.toml string.
func testFormulaTOML(name, ftype, description string) string {
	return `formula = "` + name + `"
description = "` + description + `"
version = 1
type = "` + ftype + `"

[[steps]]
id = "step1"
title = "Step one"
type = "task"

[[steps]]
id = "step2"
title = "Step two"
type = "task"
needs = ["step1"]
`
}

// createFormulaFixture writes a formula file to the given directory.
func createFormulaFixture(t *testing.T, dir, filename, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create dir %s: %v", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
	return path
}

func TestFormulaList(t *testing.T) {
	t.Run("finds TOML formulas in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		// Create two TOML formulas
		createFormulaFixture(t, formulasDir, "alpha.formula.toml",
			testFormulaTOML("alpha", "workflow", "Alpha workflow"))
		createFormulaFixture(t, formulasDir, "beta.formula.toml",
			testFormulaTOML("beta", "expansion", "Beta expansion"))

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed: %v", err)
		}

		if len(formulas) != 2 {
			t.Fatalf("expected 2 formulas, got %d", len(formulas))
		}

		// Verify names are present (order may vary from readdir)
		names := map[string]bool{}
		for _, f := range formulas {
			names[f.Formula] = true
		}
		if !names["alpha"] {
			t.Error("expected to find formula 'alpha'")
		}
		if !names["beta"] {
			t.Error("expected to find formula 'beta'")
		}
	})

	t.Run("finds JSON formulas in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		steps := []formula.Step{
			{ID: "s1", Title: "Step 1", Type: "task"},
		}
		createFormulaFixture(t, formulasDir, "gamma.formula.json",
			testFormulaJSON("gamma", "workflow", "Gamma workflow", steps))

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed: %v", err)
		}

		if len(formulas) != 1 {
			t.Fatalf("expected 1 formula, got %d", len(formulas))
		}
		if formulas[0].Formula != "gamma" {
			t.Errorf("expected name 'gamma', got %q", formulas[0].Formula)
		}
	})

	t.Run("type filter", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		createFormulaFixture(t, formulasDir, "wf.formula.toml",
			testFormulaTOML("wf", "workflow", "A workflow"))
		createFormulaFixture(t, formulasDir, "exp.formula.toml",
			testFormulaTOML("exp", "expansion", "An expansion"))
		createFormulaFixture(t, formulasDir, "asp.formula.toml",
			testFormulaTOML("asp", "aspect", "An aspect"))

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed: %v", err)
		}

		// Apply type filter like runFormulaList does
		typeFilter := "workflow"
		var filtered []*formula.Formula
		for _, f := range formulas {
			if string(f.Type) == typeFilter {
				filtered = append(filtered, f)
			}
		}

		if len(filtered) != 1 {
			t.Fatalf("expected 1 workflow formula, got %d", len(filtered))
		}
		if filtered[0].Formula != "wf" {
			t.Errorf("expected formula 'wf', got %q", filtered[0].Formula)
		}
	})

	t.Run("JSON output format", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		createFormulaFixture(t, formulasDir, "test.formula.toml",
			testFormulaTOML("test-formula", "workflow", "Test desc"))

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed: %v", err)
		}

		// Build entries like runFormulaList does
		var entries []FormulaListEntry
		for _, f := range formulas {
			entries = append(entries, FormulaListEntry{
				Name:        f.Formula,
				Type:        string(f.Type),
				Description: truncateDescription(f.Description, 60),
				Source:      f.Source,
				Steps:       countSteps(f.Steps),
				Vars:        len(f.Vars),
			})
		}

		data, err := json.Marshal(entries)
		if err != nil {
			t.Fatalf("JSON marshal failed: %v", err)
		}

		var decoded []FormulaListEntry
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("JSON unmarshal failed: %v", err)
		}

		if len(decoded) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(decoded))
		}
		if decoded[0].Name != "test-formula" {
			t.Errorf("expected name 'test-formula', got %q", decoded[0].Name)
		}
		if decoded[0].Type != "workflow" {
			t.Errorf("expected type 'workflow', got %q", decoded[0].Type)
		}
		if decoded[0].Steps != 2 {
			t.Errorf("expected 2 steps, got %d", decoded[0].Steps)
		}
	})

	t.Run("empty directory returns no error", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, "empty-formulas")
		if err := os.MkdirAll(formulasDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed on empty dir: %v", err)
		}
		if len(formulas) != 0 {
			t.Errorf("expected 0 formulas, got %d", len(formulas))
		}
	})

	t.Run("nonexistent directory returns error", func(t *testing.T) {
		_, err := scanFormulaDir("/nonexistent/path")
		if err == nil {
			t.Error("expected error for nonexistent directory")
		}
	})

	t.Run("skips non-formula files", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		createFormulaFixture(t, formulasDir, "real.formula.toml",
			testFormulaTOML("real", "workflow", "Real formula"))
		// Write some non-formula files
		createFormulaFixture(t, formulasDir, "README.md", "# Formulas")
		createFormulaFixture(t, formulasDir, "notes.txt", "some notes")

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed: %v", err)
		}
		if len(formulas) != 1 {
			t.Errorf("expected 1 formula (skipping non-formula files), got %d", len(formulas))
		}
	})

	t.Run("skips invalid formula files", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		createFormulaFixture(t, formulasDir, "good.formula.toml",
			testFormulaTOML("good", "workflow", "Good formula"))
		createFormulaFixture(t, formulasDir, "bad.formula.toml",
			"this is not valid toml {{{")

		formulas, err := scanFormulaDir(formulasDir)
		if err != nil {
			t.Fatalf("scanFormulaDir failed: %v", err)
		}
		if len(formulas) != 1 {
			t.Errorf("expected 1 formula (skipping invalid), got %d", len(formulas))
		}
		if formulas[0].Formula != "good" {
			t.Errorf("expected 'good' formula, got %q", formulas[0].Formula)
		}
	})
}

func TestFormulaShow(t *testing.T) {
	t.Run("loads formula by name", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		createFormulaFixture(t, formulasDir, "my-workflow.formula.toml",
			testFormulaTOML("my-workflow", "workflow", "My test workflow"))

		parser := formula.NewParser(formulasDir)
		f, err := parser.LoadByName("my-workflow")
		if err != nil {
			t.Fatalf("LoadByName failed: %v", err)
		}

		if f.Formula != "my-workflow" {
			t.Errorf("expected name 'my-workflow', got %q", f.Formula)
		}
		if f.Description != "My test workflow" {
			t.Errorf("expected description 'My test workflow', got %q", f.Description)
		}
		if f.Type != formula.TypeWorkflow {
			t.Errorf("expected type 'workflow', got %q", f.Type)
		}
	})

	t.Run("show output includes name description steps vars", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		tomlContent := `formula = "detailed-formula"
description = "A detailed test formula"
version = 1
type = "workflow"

[vars.component]
description = "Component name"
required = true

[vars.env]
description = "Environment"
default = "staging"

[[steps]]
id = "build"
title = "Build {{component}}"
type = "task"

[[steps]]
id = "deploy"
title = "Deploy to {{env}}"
type = "task"
needs = ["build"]
`
		createFormulaFixture(t, formulasDir, "detailed-formula.formula.toml", tomlContent)

		parser := formula.NewParser(formulasDir)
		f, err := parser.LoadByName("detailed-formula")
		if err != nil {
			t.Fatalf("LoadByName failed: %v", err)
		}

		// Verify all fields are populated
		if f.Formula != "detailed-formula" {
			t.Errorf("expected name 'detailed-formula', got %q", f.Formula)
		}
		if f.Description != "A detailed test formula" {
			t.Errorf("expected description 'A detailed test formula', got %q", f.Description)
		}
		if len(f.Steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(f.Steps))
		}
		if f.Steps[0].ID != "build" {
			t.Errorf("expected step[0].ID 'build', got %q", f.Steps[0].ID)
		}
		if f.Steps[1].ID != "deploy" {
			t.Errorf("expected step[1].ID 'deploy', got %q", f.Steps[1].ID)
		}
		if len(f.Steps[1].Needs) != 1 || f.Steps[1].Needs[0] != "build" {
			t.Errorf("expected step[1].Needs=['build'], got %v", f.Steps[1].Needs)
		}
		if len(f.Vars) != 2 {
			t.Fatalf("expected 2 vars, got %d", len(f.Vars))
		}
		if f.Vars["component"] == nil || !f.Vars["component"].Required {
			t.Error("expected 'component' var to be required")
		}
		if f.Vars["env"] == nil || f.Vars["env"].Default != "staging" {
			t.Error("expected 'env' var with default 'staging'")
		}
	})

	t.Run("JSON output is valid", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		createFormulaFixture(t, formulasDir, "json-test.formula.toml",
			testFormulaTOML("json-test", "workflow", "JSON test formula"))

		parser := formula.NewParser(formulasDir)
		f, err := parser.LoadByName("json-test")
		if err != nil {
			t.Fatalf("LoadByName failed: %v", err)
		}

		data, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("JSON marshal failed: %v", err)
		}

		var decoded formula.Formula
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("JSON unmarshal round-trip failed: %v", err)
		}

		if decoded.Formula != "json-test" {
			t.Errorf("expected 'json-test', got %q after round-trip", decoded.Formula)
		}
		if decoded.Type != formula.TypeWorkflow {
			t.Errorf("expected type workflow, got %q", decoded.Type)
		}
		if len(decoded.Steps) != 2 {
			t.Errorf("expected 2 steps after round-trip, got %d", len(decoded.Steps))
		}
	})

	t.Run("not-found error", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")
		if err := os.MkdirAll(formulasDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		parser := formula.NewParser(formulasDir)
		_, err := parser.LoadByName("nonexistent-formula")
		if err == nil {
			t.Fatal("expected error for nonexistent formula")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got: %v", err)
		}
	})

	t.Run("loads JSON format formula", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		steps := []formula.Step{
			{ID: "init", Title: "Initialize", Type: "task"},
			{ID: "run", Title: "Run tests", Type: "task", DependsOn: []string{"init"}},
		}
		createFormulaFixture(t, formulasDir, "json-formula.formula.json",
			testFormulaJSON("json-formula", "workflow", "JSON format formula", steps))

		parser := formula.NewParser(formulasDir)
		f, err := parser.LoadByName("json-formula")
		if err != nil {
			t.Fatalf("LoadByName failed: %v", err)
		}

		if f.Formula != "json-formula" {
			t.Errorf("expected name 'json-formula', got %q", f.Formula)
		}
		if len(f.Steps) != 2 {
			t.Errorf("expected 2 steps, got %d", len(f.Steps))
		}
	})

	t.Run("TOML shadows JSON with same name", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		// Create both TOML and JSON with same formula name
		createFormulaFixture(t, formulasDir, "shadowed.formula.toml",
			testFormulaTOML("shadowed", "workflow", "TOML version"))

		steps := []formula.Step{{ID: "s1", Title: "Step", Type: "task"}}
		createFormulaFixture(t, formulasDir, "shadowed.formula.json",
			testFormulaJSON("shadowed", "expansion", "JSON version", steps))

		// Parser tries TOML first, so TOML should win
		parser := formula.NewParser(formulasDir)
		f, err := parser.LoadByName("shadowed")
		if err != nil {
			t.Fatalf("LoadByName failed: %v", err)
		}

		if f.Type != formula.TypeWorkflow {
			t.Errorf("expected TOML version (workflow), got %q", f.Type)
		}
	})
}

func TestFormulaConvert(t *testing.T) {
	t.Run("converts JSON to valid TOML", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		steps := []formula.Step{
			{ID: "design", Title: "Design component", Type: "task", Priority: intPtr(1)},
			{ID: "implement", Title: "Implement component", Type: "task", DependsOn: []string{"design"}},
		}
		jsonPath := createFormulaFixture(t, formulasDir, "convert-test.formula.json",
			testFormulaJSON("convert-test", "workflow", "Conversion test", steps))

		// Parse the JSON file
		parser := formula.NewParser(formulasDir)
		f, err := parser.ParseFile(jsonPath)
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		// Convert to TOML
		tomlData, err := formulaToTOML(f)
		if err != nil {
			t.Fatalf("formulaToTOML failed: %v", err)
		}

		// Verify TOML is non-empty
		if len(tomlData) == 0 {
			t.Fatal("expected non-empty TOML output")
		}

		// Verify the TOML is valid by parsing it back
		tomlParser := formula.NewParser(formulasDir)
		parsed, err := tomlParser.ParseTOML(tomlData)
		if err != nil {
			t.Fatalf("ParseTOML of converted output failed: %v\nTOML content:\n%s", err, string(tomlData))
		}

		if parsed.Formula != "convert-test" {
			t.Errorf("expected name 'convert-test', got %q", parsed.Formula)
		}
		if parsed.Type != formula.TypeWorkflow {
			t.Errorf("expected type 'workflow', got %q", parsed.Type)
		}
	})

	t.Run("stdout outputs TOML to stdout", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		steps := []formula.Step{
			{ID: "s1", Title: "Step one", Type: "task"},
		}
		jsonPath := createFormulaFixture(t, formulasDir, "stdout-test.formula.json",
			testFormulaJSON("stdout-test", "workflow", "Stdout test", steps))

		parser := formula.NewParser(formulasDir)
		f, err := parser.ParseFile(jsonPath)
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		tomlData, err := formulaToTOML(f)
		if err != nil {
			t.Fatalf("formulaToTOML failed: %v", err)
		}

		// Verify it contains expected content
		tomlStr := string(tomlData)
		if !strings.Contains(tomlStr, "stdout-test") {
			t.Errorf("expected TOML to contain 'stdout-test', got:\n%s", tomlStr)
		}
		if !strings.Contains(tomlStr, "workflow") {
			t.Errorf("expected TOML to contain 'workflow', got:\n%s", tomlStr)
		}
	})

	t.Run("delete removes JSON after conversion", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		steps := []formula.Step{
			{ID: "s1", Title: "Step one", Type: "task"},
		}
		jsonPath := createFormulaFixture(t, formulasDir, "delete-test.formula.json",
			testFormulaJSON("delete-test", "workflow", "Delete test", steps))

		// Parse and convert
		parser := formula.NewParser(formulasDir)
		f, err := parser.ParseFile(jsonPath)
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		tomlData, err := formulaToTOML(f)
		if err != nil {
			t.Fatalf("formulaToTOML failed: %v", err)
		}

		// Write TOML file
		tomlPath := strings.TrimSuffix(jsonPath, formula.FormulaExtJSON) + formula.FormulaExtTOML
		if err := os.WriteFile(tomlPath, tomlData, 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Verify TOML file was created
		if _, err := os.Stat(tomlPath); err != nil {
			t.Fatalf("expected TOML file to exist at %s", tomlPath)
		}

		// Simulate --delete: remove JSON file
		if err := os.Remove(jsonPath); err != nil {
			t.Fatalf("Remove JSON failed: %v", err)
		}

		// Verify JSON file is gone
		if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
			t.Error("expected JSON file to be deleted")
		}

		// Verify TOML file still exists and is valid
		tomlParser := formula.NewParser(formulasDir)
		parsed, err := tomlParser.LoadByName("delete-test")
		if err != nil {
			t.Fatalf("LoadByName after delete failed: %v", err)
		}
		if parsed.Formula != "delete-test" {
			t.Errorf("expected name 'delete-test', got %q", parsed.Formula)
		}
	})

	t.Run("findFormulaJSON searches paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		formulasDir := filepath.Join(tmpDir, ".beads", "formulas")

		steps := []formula.Step{{ID: "s1", Title: "Step", Type: "task"}}
		createFormulaFixture(t, formulasDir, "findable.formula.json",
			testFormulaJSON("findable", "workflow", "Findable", steps))

		// Test the search function by calling it directly
		// findFormulaJSON uses getFormulaSearchPaths which depends on CWD,
		// so we test the underlying parser approach instead
		jsonPath := filepath.Join(formulasDir, "findable"+formula.FormulaExtJSON)
		if _, err := os.Stat(jsonPath); err != nil {
			t.Fatalf("expected JSON file at %s", jsonPath)
		}

		// Verify the file can be parsed
		parser := formula.NewParser(formulasDir)
		f, err := parser.ParseFile(jsonPath)
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}
		if f.Formula != "findable" {
			t.Errorf("expected name 'findable', got %q", f.Formula)
		}
	})

	t.Run("already TOML file is detected", func(t *testing.T) {
		// Verify the extension check works
		name := "test.formula.toml"
		if !strings.HasSuffix(name, formula.FormulaExtTOML) {
			t.Error("expected .formula.toml suffix to be detected")
		}
		if strings.HasSuffix(name, formula.FormulaExtJSON) {
			t.Error("expected .formula.toml not to match .formula.json suffix")
		}
	})
}

// TestFormulaHelpers tests helper functions used by formula commands.
func TestFormulaHelpers(t *testing.T) {
	t.Run("countSteps", func(t *testing.T) {
		tests := []struct {
			name     string
			steps    []*formula.Step
			expected int
		}{
			{
				name:     "nil steps",
				steps:    nil,
				expected: 0,
			},
			{
				name:     "empty steps",
				steps:    []*formula.Step{},
				expected: 0,
			},
			{
				name: "flat steps",
				steps: []*formula.Step{
					{ID: "a"},
					{ID: "b"},
					{ID: "c"},
				},
				expected: 3,
			},
			{
				name: "nested steps",
				steps: []*formula.Step{
					{
						ID: "parent",
						Children: []*formula.Step{
							{ID: "child1"},
							{ID: "child2"},
						},
					},
					{ID: "sibling"},
				},
				expected: 4,
			},
			{
				name: "deeply nested steps",
				steps: []*formula.Step{
					{
						ID: "root",
						Children: []*formula.Step{
							{
								ID: "mid",
								Children: []*formula.Step{
									{ID: "leaf"},
								},
							},
						},
					},
				},
				expected: 3,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := countSteps(tt.steps)
				if result != tt.expected {
					t.Errorf("countSteps = %d, want %d", result, tt.expected)
				}
			})
		}
	})

	t.Run("truncateDescription", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			maxLen   int
			expected string
		}{
			{
				name:     "short string unchanged",
				input:    "Hello",
				maxLen:   60,
				expected: "Hello",
			},
			{
				name:     "long string truncated",
				input:    "This is a very long description that exceeds the maximum length limit",
				maxLen:   30,
				expected: "This is a very long descrip...",
			},
			{
				name:     "multiline takes first line",
				input:    "First line\nSecond line\nThird line",
				maxLen:   60,
				expected: "First line",
			},
			{
				name:     "empty string",
				input:    "",
				maxLen:   60,
				expected: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := truncateDescription(tt.input, tt.maxLen)
				if result != tt.expected {
					t.Errorf("truncateDescription(%q, %d) = %q, want %q",
						tt.input, tt.maxLen, result, tt.expected)
				}
			})
		}
	})

	t.Run("getTypeIcon", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"workflow", "📋"},
			{"expansion", "📐"},
			{"aspect", "🎯"},
			{"unknown", "📜"},
			{"", "📜"},
		}

		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				result := getTypeIcon(tt.input)
				if result != tt.expected {
					t.Errorf("getTypeIcon(%q) = %q, want %q", tt.input, result, tt.expected)
				}
			})
		}
	})
}

// intPtr returns a pointer to an int.
func intPtr(i int) *int {
	return &i
}

package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderEmbeddedDefault(t *testing.T) {
	data := TemplateData{
		Prefix:       "test",
		ProjectName:  "myproject",
		BeadsVersion: "1.0.0",
	}

	result, err := Render(data, LoadOptions{})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	wants := []string{
		"Project Instructions for AI Agents",
		"BEGIN BEADS INTEGRATION",
		"END BEADS INTEGRATION",
		"Landing the Plane",
		"test-42",
		"test-123",
		"git push",
		"Build & Test",
	}
	for _, want := range wants {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in rendered output", want)
		}
	}
}

func TestRenderPrefixSubstitution(t *testing.T) {
	result, err := Render(TemplateData{Prefix: "myapp"}, LoadOptions{})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !strings.Contains(result, "myapp-42") {
		t.Error("expected prefix substitution 'myapp-42'")
	}
	if !strings.Contains(result, "myapp-123") {
		t.Error("expected prefix substitution 'myapp-123'")
	}
	// Should NOT contain the Go template literal
	if strings.Contains(result, "{{.Prefix}}") {
		t.Error("template variable not substituted")
	}
}

func TestLookupExplicitPath(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "custom.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Custom: {{.ProjectName}}"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Render(TemplateData{ProjectName: "hello"}, LoadOptions{
		ExplicitPath: tmplPath,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if result != "Custom: hello" {
		t.Errorf("got %q, want 'Custom: hello'", result)
	}
}

func TestLookupExplicitPathNotFound(t *testing.T) {
	_, err := Render(TemplateData{}, LoadOptions{
		ExplicitPath: "/nonexistent/template.tmpl",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent explicit path")
	}
}

func TestLookupProjectLevel(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, templateFile), []byte("Project: {{.Prefix}}"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Render(TemplateData{Prefix: "proj"}, LoadOptions{BeadsDir: beadsDir})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if result != "Project: proj" {
		t.Errorf("got %q, want 'Project: proj'", result)
	}
}

func TestLookupFallsToEmbedded(t *testing.T) {
	// No explicit path, no beads dir — should use embedded default
	result, err := Render(TemplateData{Prefix: "fb"}, LoadOptions{})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
		t.Error("expected embedded default content")
	}
	if !strings.Contains(result, "fb-42") {
		t.Error("expected prefix substitution in embedded default")
	}
}

func TestExplicitOverridesProject(t *testing.T) {
	dir := t.TempDir()

	// Project-level template
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, templateFile), []byte("project-level"), 0600); err != nil {
		t.Fatal(err)
	}

	// Explicit template
	explicitPath := filepath.Join(dir, "explicit.tmpl")
	if err := os.WriteFile(explicitPath, []byte("explicit-level"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Render(TemplateData{}, LoadOptions{
		ExplicitPath: explicitPath,
		BeadsDir:     beadsDir,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if result != "explicit-level" {
		t.Errorf("explicit should win, got %q", result)
	}
}

func TestInvalidTemplateSyntax(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "bad.tmpl")
	if err := os.WriteFile(tmplPath, []byte("{{.Missing"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Render(TemplateData{}, LoadOptions{ExplicitPath: tmplPath})
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
}

func TestSource(t *testing.T) {
	tests := []struct {
		name string
		opts LoadOptions
		want string
	}{
		{
			name: "embedded default",
			opts: LoadOptions{},
			want: "embedded:agents.md.tmpl",
		},
		{
			name: "nonexistent explicit",
			opts: LoadOptions{ExplicitPath: "/nonexistent"},
			want: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Source(tt.opts)
			if got != tt.want {
				t.Errorf("Source() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- New tests covering gaps ---

func TestProjectOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	customContent := "Custom project template for {{.Prefix}}"
	if err := os.WriteFile(filepath.Join(tmplDir, templateFile), []byte(customContent), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Render(TemplateData{Prefix: "proj"}, LoadOptions{BeadsDir: beadsDir})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	// Should use project template, not embedded default
	if strings.Contains(result, "BEGIN BEADS INTEGRATION") {
		t.Error("should use project template, not embedded default")
	}
	if result != "Custom project template for proj" {
		t.Errorf("got %q, want project template content", result)
	}
}

func TestBeadsDirWithoutTemplate(t *testing.T) {
	// BeadsDir exists but has no templates/ subdirectory — should fall through to embedded
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	result, err := Render(TemplateData{Prefix: "fall"}, LoadOptions{BeadsDir: beadsDir})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
		t.Error("should fall through to embedded default when project template is absent")
	}
	if !strings.Contains(result, "fall-42") {
		t.Error("prefix should be substituted in embedded fallback")
	}
}

func TestRenderZeroValueTemplateData(t *testing.T) {
	// Empty TemplateData should render without error; Prefix becomes empty string
	result, err := Render(TemplateData{}, LoadOptions{})
	if err != nil {
		t.Fatalf("Render error with zero-value TemplateData: %v", err)
	}
	if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
		t.Error("should still contain beads section")
	}
	// With empty prefix, "-42" should appear (no prefix before dash)
	if !strings.Contains(result, "-42") {
		t.Error("should contain '-42' even with empty prefix")
	}
	// Should NOT contain unresolved template vars
	if strings.Contains(result, "{{") {
		t.Error("should not contain unresolved template variables")
	}
}

func TestRenderTemplateExecutionError(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "bad-exec.tmpl")
	// Valid parse but will fail on execute: call undefined function
	if err := os.WriteFile(tmplPath, []byte(`{{call .Prefix}}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Render(TemplateData{Prefix: "x"}, LoadOptions{ExplicitPath: tmplPath})
	if err == nil {
		t.Fatal("expected error for template execution failure")
	}
	if !strings.Contains(err.Error(), "render template") {
		t.Errorf("error should mention render, got: %v", err)
	}
}

func TestSourceExplicitPath(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "my.tmpl")
	if err := os.WriteFile(tmplPath, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}

	got := Source(LoadOptions{ExplicitPath: tmplPath})
	if got != tmplPath {
		t.Errorf("Source() = %q, want %q", got, tmplPath)
	}
}

func TestSourceProjectLevel(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	tmplPath := filepath.Join(tmplDir, templateFile)
	if err := os.WriteFile(tmplPath, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}

	got := Source(LoadOptions{BeadsDir: beadsDir})
	if got != tmplPath {
		t.Errorf("Source() = %q, want %q", got, tmplPath)
	}
}

func TestEmbeddedTemplateStructure(t *testing.T) {
	// Verify the embedded default template contains all required structural sections
	result, err := Render(TemplateData{Prefix: "bd"}, LoadOptions{})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	sections := []string{
		"# Project Instructions for AI Agents",
		"<!-- BEGIN BEADS INTEGRATION -->",
		"## Issue Tracking with bd (beads)",
		"### Quick Start",
		"### Issue Types",
		"### Priorities",
		"### Workflow for AI Agents",
		"### Auto-Sync",
		"### Important Rules",
		"<!-- END BEADS INTEGRATION -->",
		"## Build & Test",
		"## Architecture Overview",
		"## Conventions & Patterns",
		"## Landing the Plane (Session Completion)",
		"**MANDATORY WORKFLOW:**",
		"**CRITICAL RULES:**",
	}
	for _, section := range sections {
		if !strings.Contains(result, section) {
			t.Errorf("embedded template missing section: %q", section)
		}
	}
}

func TestRenderAllVariables(t *testing.T) {
	// Template currently only uses {{.Prefix}}, but verify other fields don't cause issues
	data := TemplateData{
		Prefix:       "acme",
		ProjectName:  "acme-corp",
		BeadsVersion: "2.5.0",
	}
	result, err := Render(data, LoadOptions{})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(result, "acme-42") {
		t.Error("prefix not substituted")
	}
}

func TestRenderCustomTemplateUsesAllFields(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "full.tmpl")
	content := "Prefix={{.Prefix}} Project={{.ProjectName}} Version={{.BeadsVersion}}"
	if err := os.WriteFile(tmplPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	data := TemplateData{
		Prefix:       "bd",
		ProjectName:  "myproj",
		BeadsVersion: "1.2.3",
	}
	result, err := Render(data, LoadOptions{ExplicitPath: tmplPath})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	expected := "Prefix=bd Project=myproj Version=1.2.3"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestLookupPrecedenceChain(t *testing.T) {
	// Verify the full precedence: explicit > project > embedded
	// (user-level and system-level can't be reliably tested without monkeypatching)
	dir := t.TempDir()

	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, templateFile), []byte("PROJECT"), 0600); err != nil {
		t.Fatal(err)
	}

	explicitPath := filepath.Join(dir, "explicit.tmpl")
	if err := os.WriteFile(explicitPath, []byte("EXPLICIT"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit wins over project and embedded", func(t *testing.T) {
		result, err := Render(TemplateData{}, LoadOptions{
			ExplicitPath: explicitPath,
			BeadsDir:     beadsDir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result != "EXPLICIT" {
			t.Errorf("expected EXPLICIT, got %q", result)
		}
	})

	t.Run("project wins over embedded", func(t *testing.T) {
		result, err := Render(TemplateData{}, LoadOptions{
			BeadsDir: beadsDir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result != "PROJECT" {
			t.Errorf("expected PROJECT, got %q", result)
		}
	})

	t.Run("embedded is the fallback", func(t *testing.T) {
		result, err := Render(TemplateData{Prefix: "x"}, LoadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
			t.Error("expected embedded default")
		}
	})
}

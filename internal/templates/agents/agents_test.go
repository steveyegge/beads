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

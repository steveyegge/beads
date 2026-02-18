package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEmbeddedDefault(t *testing.T) {
	result, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	wants := []string{
		"Project Instructions for AI Agents",
		"BEGIN BEADS INTEGRATION",
		"END BEADS INTEGRATION",
		"Landing the Plane",
		"bd-42",
		"bd-123",
		"git push",
		"Build & Test",
	}
	for _, want := range wants {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in loaded output", want)
		}
	}
}

func TestLoadExplicitPath(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "custom.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Custom content here"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Load(LoadOptions{ExplicitPath: tmplPath})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if result != "Custom content here" {
		t.Errorf("got %q, want 'Custom content here'", result)
	}
}

func TestLoadExplicitPathNotFound(t *testing.T) {
	_, err := Load(LoadOptions{ExplicitPath: "/nonexistent/template.tmpl"})
	if err == nil {
		t.Fatal("expected error for nonexistent explicit path")
	}
}

func TestLoadProjectLevel(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, templateFile), []byte("Project template"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Load(LoadOptions{BeadsDir: beadsDir})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if result != "Project template" {
		t.Errorf("got %q, want 'Project template'", result)
	}
}

func TestLoadFallsToEmbedded(t *testing.T) {
	result, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
		t.Error("expected embedded default content")
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

	result, err := Load(LoadOptions{
		ExplicitPath: explicitPath,
		BeadsDir:     beadsDir,
	})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if result != "explicit-level" {
		t.Errorf("explicit should win, got %q", result)
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

func TestBeadsDirWithoutTemplate(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	result, err := Load(LoadOptions{BeadsDir: beadsDir})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
		t.Error("should fall through to embedded default when project template is absent")
	}
}

func TestEmbeddedTemplateStructure(t *testing.T) {
	result, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load error: %v", err)
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

func TestLookupPrecedenceChain(t *testing.T) {
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
		result, err := Load(LoadOptions{
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
		result, err := Load(LoadOptions{BeadsDir: beadsDir})
		if err != nil {
			t.Fatal(err)
		}
		if result != "PROJECT" {
			t.Errorf("expected PROJECT, got %q", result)
		}
	})

	t.Run("embedded is the fallback", func(t *testing.T) {
		result, err := Load(LoadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "BEGIN BEADS INTEGRATION") {
			t.Error("expected embedded default")
		}
	})
}

func TestEmbeddedContent(t *testing.T) {
	content, err := EmbeddedContent()
	if err != nil {
		t.Fatalf("EmbeddedContent error: %v", err)
	}
	if !strings.Contains(content, "BEGIN BEADS INTEGRATION") {
		t.Error("should contain beads integration markers")
	}
	if !strings.Contains(content, "Landing the Plane") {
		t.Error("should contain landing the plane section")
	}
}

func TestEmbeddedContentMatchesLoad(t *testing.T) {
	// EmbeddedContent should return the same thing as Load with no options
	embedded, err := EmbeddedContent()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if embedded != loaded {
		t.Error("EmbeddedContent() should match Load(LoadOptions{})")
	}
}

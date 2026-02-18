package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agents "github.com/steveyegge/beads/internal/templates/agents"
)

func TestWriteAgentsFileCreatesNew(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "bd"}

	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	checks := []string{
		"# Project Instructions for AI Agents",
		"<!-- BEGIN BEADS INTEGRATION -->",
		"<!-- END BEADS INTEGRATION -->",
		"## Build & Test",
		"## Landing the Plane",
		"bd-42",
	}
	for _, check := range checks {
		if !strings.Contains(string(content), check) {
			t.Errorf("new file missing: %q", check)
		}
	}
}

func TestWriteAgentsFileSkipsExistingBeadsSection(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	original := "# My Project\n\n<!-- BEGIN BEADS INTEGRATION -->\nExisting\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filename, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "bd"}
	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Error("file with existing beads section should not be modified")
	}
}

func TestWriteAgentsFileSkipsExistingLandingThePlane(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	original := "# My Project\n\n## Landing the Plane\n\nSome instructions\n"
	if err := os.WriteFile(filename, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "bd"}
	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Error("file with existing Landing the Plane should not be modified")
	}
}

func TestWriteAgentsFileAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	original := "# My Custom Project\n\nSome docs here."
	if err := os.WriteFile(filename, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "test"}
	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)

	// Original content should be preserved
	if !strings.Contains(s, "My Custom Project") {
		t.Error("original content should be preserved")
	}

	// Template content should be appended
	if !strings.Contains(s, "BEGIN BEADS INTEGRATION") {
		t.Error("beads section should be appended")
	}
	if !strings.Contains(s, "test-42") {
		t.Error("prefix should be substituted in appended content")
	}
}

func TestWriteAgentsFileAppendsNewline(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	// Content without trailing newline
	if err := os.WriteFile(filename, []byte("no trailing newline"), 0644); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "bd"}
	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	// Should have added a newline before the appended content
	if !strings.Contains(string(content), "no trailing newline\n\n") {
		t.Error("should add newline before appending")
	}
}

func TestWriteAgentsFileWithCustomPrefix(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "acme"}

	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)

	if !strings.Contains(s, "acme-42") {
		t.Error("custom prefix should be substituted")
	}
	if strings.Contains(s, "{{.Prefix}}") {
		t.Error("template variables should be resolved")
	}
}

func TestWriteAgentsFileWithProjectTemplate(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	customTmpl := "# Custom Template\nPrefix: {{.Prefix}}\n<!-- BEGIN BEADS INTEGRATION -->\nCustom section\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte(customTmpl), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "proj"}
	opts := agents.LoadOptions{BeadsDir: beadsDir}

	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)

	if !strings.Contains(s, "Custom Template") {
		t.Error("should use project-level template")
	}
	if !strings.Contains(s, "Prefix: proj") {
		t.Error("prefix should be substituted in project template")
	}
}

func TestWriteAgentsFileReadError(t *testing.T) {
	dir := t.TempDir()
	// Create a directory where the file should be — ReadFile will fail
	filename := filepath.Join(dir, "AGENTS.md")
	if err := os.Mkdir(filename, 0755); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "bd"}
	err := writeAgentsFile(filename, data, agents.LoadOptions{}, false)
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}

func TestWriteAgentsFileWithExplicitTemplate(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "custom.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Explicit: {{.Prefix}}"), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "ex"}
	opts := agents.LoadOptions{ExplicitPath: tmplPath}

	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Explicit: ex" {
		t.Errorf("got %q, want 'Explicit: ex'", string(content))
	}
}

func TestMustGetwd(t *testing.T) {
	result := mustGetwd()
	if result == "" {
		t.Error("mustGetwd should not return empty string")
	}
	if result == "." {
		t.Log("mustGetwd returned fallback '.' (Getwd failed)")
	}
}

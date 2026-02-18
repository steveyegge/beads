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

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
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

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
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

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
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

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)

	if !strings.Contains(s, "My Custom Project") {
		t.Error("original content should be preserved")
	}
	if !strings.Contains(s, "BEGIN BEADS INTEGRATION") {
		t.Error("beads section should be appended")
	}
}

func TestWriteAgentsFileAppendsNewline(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(filename, []byte("no trailing newline"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "no trailing newline\n\n") {
		t.Error("should add newline before appending")
	}
}

func TestWriteAgentsFileWithProjectTemplate(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	customTmpl := "# Custom Template\n<!-- BEGIN BEADS INTEGRATION -->\nCustom section\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte(customTmpl), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	opts := agents.LoadOptions{BeadsDir: beadsDir}

	if err := writeAgentsFile(filename, opts, false); err != nil {
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
}

func TestWriteAgentsFileReadError(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	if err := os.Mkdir(filename, 0755); err != nil {
		t.Fatal(err)
	}

	err := writeAgentsFile(filename, agents.LoadOptions{}, false)
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}

func TestWriteAgentsFileWithExplicitTemplate(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "custom.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Explicit content"), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	opts := agents.LoadOptions{ExplicitPath: tmplPath}

	if err := writeAgentsFile(filename, opts, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Explicit content" {
		t.Errorf("got %q, want 'Explicit content'", string(content))
	}
}

func TestWriteAgentsFileExplicitOverridesProject(t *testing.T) {
	dir := t.TempDir()

	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte("PROJECT"), 0600); err != nil {
		t.Fatal(err)
	}

	explicitPath := filepath.Join(dir, "explicit.tmpl")
	if err := os.WriteFile(explicitPath, []byte("EXPLICIT"), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	opts := agents.LoadOptions{
		ExplicitPath: explicitPath,
		BeadsDir:     beadsDir,
	}

	if err := writeAgentsFile(filename, opts, false); err != nil {
		t.Fatalf("writeAgentsFile error: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "EXPLICIT" {
		t.Errorf("explicit should override project, got %q", string(content))
	}
}

func TestAddAgentsInstructionsExplicitTemplate(t *testing.T) {
	dir := t.TempDir()

	tmplPath := filepath.Join(dir, "my-agents.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Custom agents template"), 0600); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	addAgentsInstructions(false, beadsDir, tmplPath)

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if string(content) != "Custom agents template" {
		t.Errorf("got %q, want 'Custom agents template'", string(content))
	}
}

func TestAddAgentsInstructionsEmptyExplicitUsesLookupChain(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	addAgentsInstructions(false, beadsDir, "")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if !strings.Contains(string(content), "BEGIN BEADS INTEGRATION") {
		t.Error("empty explicit template should fall through to embedded default")
	}
}

func TestInitCmdAgentsTemplateFlag(t *testing.T) {
	f := initCmd.Flags().Lookup("agents-template")
	if f == nil {
		t.Fatal("--agents-template flag not registered on initCmd")
	}
	if f.DefValue != "" {
		t.Errorf("default should be empty, got %q", f.DefValue)
	}
}

func TestInitCmdHelpMentionsAgentsTemplate(t *testing.T) {
	long := initCmd.Long
	checks := []string{
		"--agents-template",
		"init.agents-template",
		"bd agents-template init",
		".beads/templates/agents.md.tmpl",
	}
	for _, want := range checks {
		if !strings.Contains(long, want) {
			t.Errorf("init --help Long text missing %q", want)
		}
	}
}


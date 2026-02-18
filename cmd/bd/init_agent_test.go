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

func TestWriteAgentsFileExplicitOverridesProject(t *testing.T) {
	dir := t.TempDir()

	// Set up project-level template
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte("PROJECT"), 0600); err != nil {
		t.Fatal(err)
	}

	// Set up explicit template
	explicitPath := filepath.Join(dir, "explicit.tmpl")
	if err := os.WriteFile(explicitPath, []byte("EXPLICIT"), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "bd"}
	opts := agents.LoadOptions{
		ExplicitPath: explicitPath,
		BeadsDir:     beadsDir,
	}

	if err := writeAgentsFile(filename, data, opts, false); err != nil {
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
	// Test the full addAgentsInstructions flow with --agents-template flag
	dir := t.TempDir()

	// Create explicit template
	tmplPath := filepath.Join(dir, "my-agents.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Custom agents for {{.Prefix}}"), 0600); err != nil {
		t.Fatal(err)
	}

	// Change to temp dir so AGENTS.md is created there
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

	addAgentsInstructions(false, "test", beadsDir, tmplPath)

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if string(content) != "Custom agents for test" {
		t.Errorf("got %q, want 'Custom agents for test'", string(content))
	}
}

func TestAddAgentsInstructionsEmptyExplicitUsesLookupChain(t *testing.T) {
	// When explicitTemplate is "", the lookup chain should be used (falls to embedded)
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

	addAgentsInstructions(false, "bd", beadsDir, "")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if !strings.Contains(string(content), "BEGIN BEADS INTEGRATION") {
		t.Error("empty explicit template should fall through to embedded default")
	}
}

func TestInitCmdAgentsTemplateFlag(t *testing.T) {
	// Verify the --agents-template flag is registered on initCmd.
	f := initCmd.Flags().Lookup("agents-template")
	if f == nil {
		t.Fatal("--agents-template flag not registered on initCmd")
	}
	if f.DefValue != "" {
		t.Errorf("default should be empty, got %q", f.DefValue)
	}
}

func TestInitCmdHelpMentionsAgentsTemplate(t *testing.T) {
	// Verify the long help text documents --agents-template.
	long := initCmd.Long
	checks := []string{
		"--agents-template",
		"init.agents-template",
		"{{.Prefix}}",
		".beads/templates/agents.md.tmpl",
	}
	for _, want := range checks {
		if !strings.Contains(long, want) {
			t.Errorf("init --help Long text missing %q", want)
		}
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

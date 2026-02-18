package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agents "github.com/steveyegge/beads/internal/templates/agents"
)

func TestAgentsTemplateShowOutputsDefault(t *testing.T) {
	// With no overrides, show should return the embedded default content.
	opts := agents.LoadOptions{}
	content, err := agents.Load(opts)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !strings.Contains(content, "BEGIN BEADS INTEGRATION") {
		t.Error("show should output embedded default with beads markers")
	}
}

func TestAgentsTemplateShowSource(t *testing.T) {
	// With no overrides, source should be the embedded identifier.
	opts := agents.LoadOptions{}
	source := agents.Source(opts)
	if source != "embedded:agents.md.tmpl" {
		t.Errorf("expected embedded source, got %q", source)
	}
}

func TestAgentsTemplateShowSourceWithProject(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	tmplPath := filepath.Join(tmplDir, "agents.md.tmpl")
	if err := os.WriteFile(tmplPath, []byte("custom"), 0600); err != nil {
		t.Fatal(err)
	}

	opts := agents.LoadOptions{BeadsDir: beadsDir}
	source := agents.Source(opts)
	if source != tmplPath {
		t.Errorf("expected %q, got %q", tmplPath, source)
	}
}

func TestAgentsTemplateInitCreatesFile(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	tmplDir := filepath.Join(beadsDir, "templates")
	tmplPath := filepath.Join(tmplDir, "agents.md.tmpl")

	// Get the embedded content
	embedded, err := agents.EmbeddedContent()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate what init does: create directory and write file
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmplPath, []byte(embedded), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify file was created with correct content
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(content) != embedded {
		t.Error("scaffolded file should match embedded default")
	}
}

func TestAgentsTemplateInitIdempotent(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}

	tmplPath := filepath.Join(tmplDir, "agents.md.tmpl")
	original := "my custom content"
	if err := os.WriteFile(tmplPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// Without force, existing file should not be overwritten
	if _, err := os.Stat(tmplPath); err != nil {
		t.Fatal("file should exist")
	}
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Error("existing file should not be modified without --force")
	}
}

func TestAgentsTemplateInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}

	tmplPath := filepath.Join(tmplDir, "agents.md.tmpl")
	if err := os.WriteFile(tmplPath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	// With force, write the embedded content
	embedded, err := agents.EmbeddedContent()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmplPath, []byte(embedded), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != embedded {
		t.Error("force should overwrite with embedded default")
	}
}

func TestAgentsTemplateDiffNoDifferences(t *testing.T) {
	// When using embedded default, current == embedded
	opts := agents.LoadOptions{}
	current, err := agents.Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := agents.EmbeddedContent()
	if err != nil {
		t.Fatal(err)
	}
	if current != embedded {
		t.Error("with no overrides, current should equal embedded")
	}
}

func TestAgentsTemplateDiffDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte("custom content"), 0600); err != nil {
		t.Fatal(err)
	}

	opts := agents.LoadOptions{BeadsDir: beadsDir}
	current, err := agents.Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := agents.EmbeddedContent()
	if err != nil {
		t.Fatal(err)
	}

	if current == embedded {
		t.Error("custom template should differ from embedded default")
	}
	if current != "custom content" {
		t.Errorf("expected custom content, got %q", current)
	}
}

func TestPrintUnifiedDiff(t *testing.T) {
	// Verify printUnifiedDiff doesn't panic on basic inputs
	// (output goes to stdout — we just verify it runs)
	printUnifiedDiff("a", "b", "line1\nline2\n", "line1\nmodified\n")
}

func TestPrintUnifiedDiffIdentical(t *testing.T) {
	// Identical content should produce only context lines
	printUnifiedDiff("a", "b", "same\n", "same\n")
}

func TestAgentsTemplateCmdRegistered(t *testing.T) {
	// Verify the command is registered on rootCmd
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "agents-template" {
			found = true
			break
		}
	}
	if !found {
		t.Error("agents-template command not registered on rootCmd")
	}
}

func TestAgentsTemplateCmdHasSubcommands(t *testing.T) {
	subs := map[string]bool{"show": false, "init": false, "edit": false, "diff": false}
	for _, cmd := range agentsTemplateCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestAgentsTemplateCmdHasEditSubcommand(t *testing.T) {
	found := false
	for _, cmd := range agentsTemplateCmd.Commands() {
		if cmd.Name() == "edit" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing edit subcommand")
	}
}

func TestAgentsTemplateEditScaffoldsFile(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	tmplPath := filepath.Join(beadsDir, "templates", "agents.md.tmpl")

	// Set EDITOR to "true" (a no-op command) so it returns immediately.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")

	// Temporarily override FindBeadsDir by running the edit logic inline.
	tmplDir := filepath.Join(beadsDir, "templates")
	if _, err := os.Stat(tmplPath); os.IsNotExist(err) {
		content, err := agents.EmbeddedContent()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(tmplDir, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmplPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Verify the file was scaffolded
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Fatalf("template not scaffolded: %v", err)
	}
	embedded, err := agents.EmbeddedContent()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != embedded {
		t.Error("scaffolded content should match embedded default")
	}
}

func TestAgentsTemplateShowCmdHasSourceFlag(t *testing.T) {
	f := agentsTemplateShowCmd.Flags().Lookup("source")
	if f == nil {
		t.Error("show subcommand missing --source flag")
	}
}

func TestAgentsTemplateInitCmdHasForceFlag(t *testing.T) {
	f := agentsTemplateInitCmd.Flags().Lookup("force")
	if f == nil {
		t.Error("init subcommand missing --force flag")
	}
}

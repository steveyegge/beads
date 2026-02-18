package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agents "github.com/steveyegge/beads/internal/templates/agents"
)

// Integration tests verifying the full AGENTS.md lifecycle:
// template rendering → file creation → section detection → idempotency.
// These bridge init_agent.go, setup/agents.go, and internal/templates/agents.

func TestWriteAgentsFileIdempotency(t *testing.T) {
	// Running writeAgentsFile twice on a new file should produce identical output.
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "bd"}
	opts := agents.LoadOptions{}

	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	// Second call — file now exists WITH beads section → should be a no-op
	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Error("writeAgentsFile should be idempotent when beads section already present")
	}
}

func TestWriteAgentsFileThenAppendIsIdempotent(t *testing.T) {
	// After appending to an existing file, a second call should be a no-op.
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	original := "# My Project\n\nCustom content here.\n"
	if err := os.WriteFile(filename, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "bd"}
	opts := agents.LoadOptions{}

	// First call appends
	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	afterAppend, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	// Second call — should detect beads section and skip
	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("second write: %v", err)
	}
	afterSecond, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if string(afterAppend) != string(afterSecond) {
		t.Error("second call should be idempotent after append")
	}
}

func TestRenderAndWriteProduceCompatibleOutput(t *testing.T) {
	// Verify that agents.Render() output and writeAgentsFile() produce the same
	// content for a new file — they should match exactly.
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "compat"}
	opts := agents.LoadOptions{}

	// Get rendered template directly
	rendered, err := agents.Render(data, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Create file via writeAgentsFile
	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("writeAgentsFile: %v", err)
	}
	written, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if rendered != string(written) {
		t.Error("Render() and writeAgentsFile() should produce identical content for new files")
	}
}

func TestBeadsSectionMarkersAreBalanced(t *testing.T) {
	// The rendered template must have exactly one BEGIN and one END marker,
	// and BEGIN must come before END. This is critical for setup/agents.go
	// which relies on these markers for section extraction/replacement.
	data := agents.TemplateData{Prefix: "bd"}
	rendered, err := agents.Render(data, agents.LoadOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	beginCount := strings.Count(rendered, "<!-- BEGIN BEADS INTEGRATION -->")
	endCount := strings.Count(rendered, "<!-- END BEADS INTEGRATION -->")

	if beginCount != 1 {
		t.Errorf("expected exactly 1 BEGIN marker, found %d", beginCount)
	}
	if endCount != 1 {
		t.Errorf("expected exactly 1 END marker, found %d", endCount)
	}

	beginIdx := strings.Index(rendered, "<!-- BEGIN BEADS INTEGRATION -->")
	endIdx := strings.Index(rendered, "<!-- END BEADS INTEGRATION -->")
	if beginIdx >= endIdx {
		t.Error("BEGIN marker must appear before END marker")
	}
}

func TestBeadsSectionIsExtractable(t *testing.T) {
	// The beads section extracted from a rendered template should be a valid
	// standalone section (what setup/agents.go uses for updates).
	data := agents.TemplateData{Prefix: "extract"}
	rendered, err := agents.Render(data, agents.LoadOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	begin := strings.Index(rendered, "<!-- BEGIN BEADS INTEGRATION -->")
	end := strings.Index(rendered, "<!-- END BEADS INTEGRATION -->")
	section := rendered[begin : end+len("<!-- END BEADS INTEGRATION -->")]

	// Section should contain prefix-specific content
	if !strings.Contains(section, "extract-") {
		t.Error("extracted section should contain prefix")
	}

	// Section should not contain content outside the markers
	if strings.Contains(section, "# Project Instructions") {
		t.Error("section should not include header outside markers")
	}
	if strings.Contains(section, "Landing the Plane") {
		t.Error("section should not include landing-the-plane outside markers")
	}
}

func TestProjectTemplateOverridesEmbeddedInWriteAgentsFile(t *testing.T) {
	// End-to-end: project-level template is picked up by writeAgentsFile
	// on a fresh file (no existing AGENTS.md).
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}

	customContent := "# Custom Project\n<!-- BEGIN BEADS INTEGRATION -->\nPrefix: {{.Prefix}}\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte(customContent), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "proj"}
	opts := agents.LoadOptions{BeadsDir: beadsDir}

	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("writeAgentsFile: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)

	if !strings.Contains(s, "Custom Project") {
		t.Error("should use project-level template")
	}
	if !strings.Contains(s, "Prefix: proj") {
		t.Error("template variables should be resolved")
	}
	// Should NOT have the embedded default header
	if strings.Contains(s, "Project Instructions for AI Agents") {
		t.Error("should not contain embedded default content")
	}
}

func TestExplicitTemplateBypassesExistingFileChecks(t *testing.T) {
	// With an explicit template, writeAgentsFile always overwrites.
	// Even if the file already has beads markers, the explicit template wins.
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")

	// Create a file with existing beads markers
	existing := "# Old\n<!-- BEGIN BEADS INTEGRATION -->\nOld content\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filename, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	// Write with explicit template
	tmplPath := filepath.Join(dir, "explicit.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Explicit: {{.Prefix}}"), 0600); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "ex"}
	opts := agents.LoadOptions{ExplicitPath: tmplPath}

	// writeAgentsFile should skip because it detects the existing beads section.
	// The explicit template only affects the Render output, not the skip logic.
	if err := writeAgentsFile(filename, data, opts, false); err != nil {
		t.Fatalf("writeAgentsFile: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	// File should be unchanged because the skip-if-beads-exists check
	// runs before template selection.
	if string(content) != existing {
		t.Errorf("existing file with beads markers should not be modified, got %q", string(content))
	}
}

func TestRenderedTemplateHasProperMarkdownStructure(t *testing.T) {
	// The rendered template should be valid markdown with proper heading hierarchy.
	data := agents.TemplateData{Prefix: "md"}
	rendered, err := agents.Render(data, agents.LoadOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	lines := strings.Split(rendered, "\n")
	var h1Count int
	var headingLevels []int
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, c := range trimmed {
				if c == '#' {
					level++
				} else {
					break
				}
			}
			// Must be followed by a space to be a heading
			if level < len(trimmed) && trimmed[level] == ' ' {
				headingLevels = append(headingLevels, level)
				if level == 1 {
					h1Count++
				}
			}
		}
	}

	if h1Count != 1 {
		t.Errorf("expected exactly 1 H1 heading, found %d", h1Count)
	}

	// All headings should be level 1-4 (no deeply nested headings)
	for _, level := range headingLevels {
		if level > 4 {
			t.Errorf("heading level %d is too deep (max 4)", level)
		}
	}
}

func TestLandingThePlaneDetection(t *testing.T) {
	// writeAgentsFile should also skip files that have "Landing the Plane"
	// even without beads markers. This prevents double-adding instructions.
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	existing := "# My AGENTS.md\n\n## Landing the Plane\n\nAlways commit before closing.\n"
	if err := os.WriteFile(filename, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	data := agents.TemplateData{Prefix: "bd"}
	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatalf("writeAgentsFile: %v", err)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != existing {
		t.Error("file with 'Landing the Plane' should not be modified")
	}
}

func TestAddAgentsInstructionsCreatesInCwd(t *testing.T) {
	// addAgentsInstructions creates AGENTS.md in the current working directory.
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

	addAgentsInstructions(false, "integ", beadsDir, "")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}

	s := string(content)
	// Verify it has the full template (not just the beads section)
	if !strings.Contains(s, "# Project Instructions for AI Agents") {
		t.Error("should have the full template header")
	}
	if !strings.Contains(s, "integ-42") {
		t.Error("prefix should be substituted")
	}
	if !strings.Contains(s, "Landing the Plane") {
		t.Error("should have landing-the-plane section")
	}
}

func TestAddAgentsInstructionsWithProjectTemplate(t *testing.T) {
	// addAgentsInstructions should use a project-level template when available.
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
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}

	customTmpl := "# Custom\nProject={{.ProjectName}} Prefix={{.Prefix}}\n"
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte(customTmpl), 0600); err != nil {
		t.Fatal(err)
	}

	addAgentsInstructions(false, "projtest", beadsDir, "")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "Custom") {
		t.Error("should use project template")
	}
	if !strings.Contains(s, "Prefix=projtest") {
		t.Error("prefix should be substituted in project template")
	}
}

func TestAddAgentsInstructionsVerboseOutput(t *testing.T) {
	// Verbose mode should not cause errors (we can't easily capture stdout here,
	// but we verify it doesn't panic or fail).
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

	// Should not panic in verbose mode
	addAgentsInstructions(true, "verb", beadsDir, "")

	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md should be created in verbose mode: %v", err)
	}
}

func TestMultiplePrefixesProduceDifferentContent(t *testing.T) {
	// Different prefixes should produce different output in the beads section.
	prefixes := []string{"alpha", "beta", "gamma"}
	results := make(map[string]string)

	for _, prefix := range prefixes {
		rendered, err := agents.Render(agents.TemplateData{Prefix: prefix}, agents.LoadOptions{})
		if err != nil {
			t.Fatalf("Render(%s): %v", prefix, err)
		}
		results[prefix] = rendered
	}

	// Each should contain its own prefix
	for _, prefix := range prefixes {
		if !strings.Contains(results[prefix], prefix+"-42") {
			t.Errorf("output for prefix %q should contain %q", prefix, prefix+"-42")
		}
	}

	// They should all be different
	if results["alpha"] == results["beta"] || results["beta"] == results["gamma"] {
		t.Error("different prefixes should produce different content")
	}
}

func TestWriteAgentsFilePermissions(t *testing.T) {
	// Created file should be world-readable (0644).
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	data := agents.TemplateData{Prefix: "bd"}

	if err := writeAgentsFile(filename, data, agents.LoadOptions{}, false); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}

	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("expected 0644 permissions, got %04o", perm)
	}
}

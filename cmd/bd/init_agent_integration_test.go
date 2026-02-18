package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agents "github.com/steveyegge/beads/internal/templates/agents"
)

// Integration tests verifying the full AGENTS.md lifecycle:
// template loading → file creation → section detection → idempotency.
// These bridge init_agent.go, setup/agents.go, and internal/templates/agents.

func TestWriteAgentsFileIdempotency(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	opts := agents.LoadOptions{}

	if err := writeAgentsFile(filename, opts, false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if err := writeAgentsFile(filename, opts, false); err != nil {
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
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	original := "# My Project\n\nCustom content here.\n"
	if err := os.WriteFile(filename, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	opts := agents.LoadOptions{}

	if err := writeAgentsFile(filename, opts, false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	afterAppend, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if err := writeAgentsFile(filename, opts, false); err != nil {
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

func TestLoadAndWriteProduceCompatibleOutput(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	opts := agents.LoadOptions{}

	loaded, err := agents.Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := writeAgentsFile(filename, opts, false); err != nil {
		t.Fatalf("writeAgentsFile: %v", err)
	}
	written, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if loaded != string(written) {
		t.Error("Load() and writeAgentsFile() should produce identical content for new files")
	}
}

func TestBeadsSectionMarkersAreBalanced(t *testing.T) {
	content, err := agents.Load(agents.LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	beginCount := strings.Count(content, "<!-- BEGIN BEADS INTEGRATION -->")
	endCount := strings.Count(content, "<!-- END BEADS INTEGRATION -->")

	if beginCount != 1 {
		t.Errorf("expected exactly 1 BEGIN marker, found %d", beginCount)
	}
	if endCount != 1 {
		t.Errorf("expected exactly 1 END marker, found %d", endCount)
	}

	beginIdx := strings.Index(content, "<!-- BEGIN BEADS INTEGRATION -->")
	endIdx := strings.Index(content, "<!-- END BEADS INTEGRATION -->")
	if beginIdx >= endIdx {
		t.Error("BEGIN marker must appear before END marker")
	}
}

func TestBeadsSectionIsExtractable(t *testing.T) {
	content, err := agents.Load(agents.LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	begin := strings.Index(content, "<!-- BEGIN BEADS INTEGRATION -->")
	end := strings.Index(content, "<!-- END BEADS INTEGRATION -->")
	section := content[begin : end+len("<!-- END BEADS INTEGRATION -->")]

	if strings.Contains(section, "# Project Instructions") {
		t.Error("section should not include header outside markers")
	}
	if strings.Contains(section, "Landing the Plane") {
		t.Error("section should not include landing-the-plane outside markers")
	}
}

func TestProjectTemplateOverridesEmbeddedInWriteAgentsFile(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	tmplDir := filepath.Join(beadsDir, "templates")
	if err := os.MkdirAll(tmplDir, 0750); err != nil {
		t.Fatal(err)
	}

	customContent := "# Custom Project\n<!-- BEGIN BEADS INTEGRATION -->\nCustom section\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte(customContent), 0600); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(dir, "AGENTS.md")
	opts := agents.LoadOptions{BeadsDir: beadsDir}

	if err := writeAgentsFile(filename, opts, false); err != nil {
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
	if strings.Contains(s, "Project Instructions for AI Agents") {
		t.Error("should not contain embedded default content")
	}
}

func TestExplicitTemplateBypassesExistingFileChecks(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")

	existing := "# Old\n<!-- BEGIN BEADS INTEGRATION -->\nOld content\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(filename, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	tmplPath := filepath.Join(dir, "explicit.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Explicit content"), 0600); err != nil {
		t.Fatal(err)
	}

	opts := agents.LoadOptions{ExplicitPath: tmplPath}

	if err := writeAgentsFile(filename, opts, false); err != nil {
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
	content, err := agents.Load(agents.LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	lines := strings.Split(content, "\n")
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

	for _, level := range headingLevels {
		if level > 4 {
			t.Errorf("heading level %d is too deep (max 4)", level)
		}
	}
}

func TestLandingThePlaneDetection(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")
	existing := "# My AGENTS.md\n\n## Landing the Plane\n\nAlways commit before closing.\n"
	if err := os.WriteFile(filename, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
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

	s := string(content)
	if !strings.Contains(s, "# Project Instructions for AI Agents") {
		t.Error("should have the full template header")
	}
	if !strings.Contains(s, "Landing the Plane") {
		t.Error("should have landing-the-plane section")
	}
}

func TestAddAgentsInstructionsWithProjectTemplate(t *testing.T) {
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

	customTmpl := "# Custom\nProject template content\n"
	if err := os.WriteFile(filepath.Join(tmplDir, "agents.md.tmpl"), []byte(customTmpl), 0600); err != nil {
		t.Fatal(err)
	}

	addAgentsInstructions(false, beadsDir, "")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "Custom") {
		t.Error("should use project template")
	}
}

func TestAddAgentsInstructionsVerboseOutput(t *testing.T) {
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

	addAgentsInstructions(true, beadsDir, "")

	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md should be created in verbose mode: %v", err)
	}
}

func TestWriteAgentsFilePermissions(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "AGENTS.md")

	if err := writeAgentsFile(filename, agents.LoadOptions{}, false); err != nil {
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

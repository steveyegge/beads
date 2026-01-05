package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKiloCodeRulesTemplate(t *testing.T) {
	// Verify template contains required content
	requiredContent := []string{
		"bd prime",
		"bd ready",
		"bd create",
		"bd update",
		"bd close",
		"bd sync",
		"BEADS INTEGRATION",
	}

	for _, req := range requiredContent {
		if !strings.Contains(kilocodeRulesTemplate, req) {
			t.Errorf("kilocodeRulesTemplate missing required content: %q", req)
		}
	}
}

func TestKiloCodeTemplateFormatting(t *testing.T) {
	// Verify template is well-formed
	template := kilocodeRulesTemplate

	// Should have both markers
	if !strings.Contains(template, "BEGIN BEADS INTEGRATION") {
		t.Error("Missing BEGIN marker")
	}
	if !strings.Contains(template, "END BEADS INTEGRATION") {
		t.Error("Missing END marker")
	}

	// Should have workflow section
	if !strings.Contains(template, "## Workflow") {
		t.Error("Missing Workflow section")
	}

	// Should have context loading section
	if !strings.Contains(template, "## Context Loading") {
		t.Error("Missing Context Loading section")
	}
}

func TestInstallKiloCode_Project(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	err := installKiloCode(env, false)
	if err != nil {
		t.Fatalf("installKiloCode failed: %v", err)
	}

	// Verify file was created
	rulesPath := filepath.Join(tmpDir, ".kilocode", "rules", "bd.md")
	if !FileExists(rulesPath) {
		t.Fatal("Kilo Code rules file was not created")
	}

	// Verify content
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read rules file: %v", err)
	}

	if string(data) != kilocodeRulesTemplate {
		t.Error("Rules file content doesn't match template")
	}

	// Verify output
	if !strings.Contains(stdout.String(), "✓ Kilo Code integration installed") {
		t.Error("Expected success message in stdout")
	}
}

func TestInstallKiloCode_Global(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    homeDir,
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	err := installKiloCode(env, true)
	if err != nil {
		t.Fatalf("installKiloCode global failed: %v", err)
	}

	// Verify file was created in home directory
	rulesPath := filepath.Join(homeDir, ".kilocode", "rules", "bd.md")
	if !FileExists(rulesPath) {
		t.Fatal("Kilo Code global rules file was not created")
	}

	// Verify content
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read rules file: %v", err)
	}

	if string(data) != kilocodeRulesTemplate {
		t.Error("Rules file content doesn't match template")
	}

	// Verify output mentions global
	if !strings.Contains(stdout.String(), "globally") {
		t.Error("Expected 'globally' in stdout")
	}
}

func TestInstallKiloCode_ExistingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	// Pre-create the directory
	if err := os.MkdirAll(filepath.Join(tmpDir, ".kilocode", "rules"), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Should not fail
	err := installKiloCode(env, false)
	if err != nil {
		t.Fatalf("installKiloCode failed with existing directory: %v", err)
	}

	// Verify file was created
	rulesPath := filepath.Join(tmpDir, ".kilocode", "rules", "bd.md")
	if !FileExists(rulesPath) {
		t.Fatal("Kilo Code rules file was not created")
	}
}

func TestInstallKiloCode_OverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	// Create existing file with different content
	rulesPath := filepath.Join(tmpDir, ".kilocode", "rules", "bd.md")
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(rulesPath, []byte("old content"), 0644); err != nil {
		t.Fatalf("failed to create old file: %v", err)
	}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	err := installKiloCode(env, false)
	if err != nil {
		t.Fatalf("installKiloCode failed: %v", err)
	}

	// Verify content was overwritten
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read rules file: %v", err)
	}

	if string(data) == "old content" {
		t.Error("Old content was not overwritten")
	}
	if string(data) != kilocodeRulesTemplate {
		t.Error("Content doesn't match template")
	}
}

func TestInstallKiloCodeIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	rulesPath := filepath.Join(tmpDir, ".kilocode", "rules", "bd.md")

	// Run twice
	if err := installKiloCode(env, false); err != nil {
		t.Fatalf("first install failed: %v", err)
	}
	firstData, _ := os.ReadFile(rulesPath)

	if err := installKiloCode(env, false); err != nil {
		t.Fatalf("second install failed: %v", err)
	}
	secondData, _ := os.ReadFile(rulesPath)

	if string(firstData) != string(secondData) {
		t.Error("InstallKiloCode should be idempotent")
	}
}

func TestRemoveKiloCode_Project(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Install first
	if err := installKiloCode(env, false); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verify file exists
	rulesPath := filepath.Join(tmpDir, ".kilocode", "rules", "bd.md")
	if !FileExists(rulesPath) {
		t.Fatal("File should exist before removal")
	}

	// Reset output buffers
	stdout.Reset()
	stderr.Reset()

	// Remove
	err := removeKiloCode(env, false)
	if err != nil {
		t.Fatalf("removeKiloCode failed: %v", err)
	}

	// Verify file is gone
	if FileExists(rulesPath) {
		t.Error("File should have been removed")
	}

	// Verify output
	if !strings.Contains(stdout.String(), "✓ Removed Kilo Code integration") {
		t.Error("Expected removal success message in stdout")
	}
}

func TestRemoveKiloCode_Global(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    homeDir,
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Install first (global)
	if err := installKiloCode(env, true); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verify file exists
	rulesPath := filepath.Join(homeDir, ".kilocode", "rules", "bd.md")
	if !FileExists(rulesPath) {
		t.Fatal("File should exist before removal")
	}

	// Reset output buffers
	stdout.Reset()
	stderr.Reset()

	// Remove
	err := removeKiloCode(env, true)
	if err != nil {
		t.Fatalf("removeKiloCode global failed: %v", err)
	}

	// Verify file is gone
	if FileExists(rulesPath) {
		t.Error("File should have been removed")
	}
}

func TestRemoveKiloCode_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Should not fail when file doesn't exist
	err := removeKiloCode(env, false)
	if err != nil {
		t.Fatalf("removeKiloCode should not fail when file doesn't exist: %v", err)
	}

	// Should indicate no file found
	if !strings.Contains(stdout.String(), "No rules file found") {
		t.Error("Expected 'No rules file found' message")
	}
}

func TestCheckKiloCode_NotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	err := checkKiloCode(env)
	if err == nil {
		t.Error("checkKiloCode should return error when not installed")
	}

	// Should show not installed message
	if !strings.Contains(stdout.String(), "✗ Kilo Code integration not installed") {
		t.Error("Expected 'not installed' message")
	}
}

func TestCheckKiloCode_ProjectInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    filepath.Join(tmpDir, "home"),
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Install project rules
	if err := installKiloCode(env, false); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Reset output
	stdout.Reset()
	stderr.Reset()

	err := checkKiloCode(env)
	if err != nil {
		t.Errorf("checkKiloCode should not return error when installed: %v", err)
	}

	// Should show installed message
	if !strings.Contains(stdout.String(), "✓ Project rules installed") {
		t.Error("Expected 'Project rules installed' message")
	}
}

func TestCheckKiloCode_GlobalInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    homeDir,
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Install global rules
	if err := installKiloCode(env, true); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Reset output
	stdout.Reset()
	stderr.Reset()

	err := checkKiloCode(env)
	if err != nil {
		t.Errorf("checkKiloCode should not return error when installed: %v", err)
	}

	// Should show installed message
	if !strings.Contains(stdout.String(), "✓ Global rules installed") {
		t.Error("Expected 'Global rules installed' message")
	}
}

func TestCheckKiloCode_BothInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	env := kilocodeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    homeDir,
		projectDir: tmpDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return os.WriteFile(path, data, 0600)
		},
		removeFile: os.Remove,
		fileExists: FileExists,
	}

	// Install both
	if err := installKiloCode(env, true); err != nil {
		t.Fatalf("global install failed: %v", err)
	}
	if err := installKiloCode(env, false); err != nil {
		t.Fatalf("project install failed: %v", err)
	}

	// Reset output
	stdout.Reset()
	stderr.Reset()

	err := checkKiloCode(env)
	if err != nil {
		t.Errorf("checkKiloCode should not return error when installed: %v", err)
	}

	// Should show both
	output := stdout.String()
	if !strings.Contains(output, "✓ Global rules installed") {
		t.Error("Expected 'Global rules installed' message")
	}
	if !strings.Contains(output, "✓ Project rules installed") {
		t.Error("Expected 'Project rules installed' message")
	}
}

func TestKiloCodeRulesPath_Project(t *testing.T) {
	path := projectKiloCodeRulesPath("/project")
	expected := filepath.Join("/project", ".kilocode", "rules", "bd.md")
	if path != expected {
		t.Errorf("projectKiloCodeRulesPath = %q, want %q", path, expected)
	}
}

func TestKiloCodeRulesPath_Global(t *testing.T) {
	path := globalKiloCodeRulesPath("/home/user")
	expected := filepath.Join("/home/user", ".kilocode", "rules", "bd.md")
	if path != expected {
		t.Errorf("globalKiloCodeRulesPath = %q, want %q", path, expected)
	}
}

// TestKiloCodeVsCursorTemplates compares the Kilo Code and Cursor templates
// to ensure they have equivalent content
func TestKiloCodeVsCursorTemplates(t *testing.T) {
	// Both should have the same key content
	sharedContent := []string{
		"Beads Issue Tracking",
		"BEGIN BEADS INTEGRATION",
		"END BEADS INTEGRATION",
		"bd prime",
		"bd ready",
		"bd create",
		"bd update",
		"bd close",
		"bd sync",
		"## Core Rules",
		"## Quick Reference",
		"## Workflow",
		"## Context Loading",
	}

	for _, content := range sharedContent {
		if !strings.Contains(kilocodeRulesTemplate, content) {
			t.Errorf("kilocodeRulesTemplate missing: %q", content)
		}
		if !strings.Contains(cursorRulesTemplate, content) {
			t.Errorf("cursorRulesTemplate missing: %q", content)
		}
	}
}

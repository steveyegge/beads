package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

// TestBackendListText tests `bd backend list` text output.
func TestBackendListText(t *testing.T) {
	// Save original state
	origJsonOutput := jsonOutput
	defer func() { jsonOutput = origJsonOutput }()
	jsonOutput = false

	// Capture stdout
	output := captureBackendListOutput(t)

	// Verify output contains all backends
	if !strings.Contains(output, "sqlite") {
		t.Error("output should contain 'sqlite' backend")
	}
	if !strings.Contains(output, "dolt") {
		t.Error("output should contain 'dolt' backend")
	}
	if !strings.Contains(output, "jsonl") {
		t.Error("output should contain 'jsonl' backend")
	}

	// Verify it has header and footer
	if !strings.Contains(output, "Available backends") {
		t.Error("output should contain 'Available backends' header")
	}
	if !strings.Contains(output, "bd init --backend") {
		t.Error("output should contain usage hint")
	}
}

// TestBackendListJSON tests `bd backend list --json` output.
func TestBackendListJSON(t *testing.T) {
	// Save original state
	origJsonOutput := jsonOutput
	defer func() { jsonOutput = origJsonOutput }()
	jsonOutput = true

	// Capture stdout
	output := captureBackendListOutput(t)

	// Parse JSON
	var result struct {
		Backends []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"backends"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify we have 3 backends
	if len(result.Backends) != 3 {
		t.Errorf("expected 3 backends, got %d", len(result.Backends))
	}

	// Verify backend names
	names := make(map[string]bool)
	for _, b := range result.Backends {
		names[b.Name] = true
		if b.Description == "" {
			t.Errorf("backend %s has empty description", b.Name)
		}
	}

	for _, expected := range []string{"sqlite", "dolt", "jsonl"} {
		if !names[expected] {
			t.Errorf("missing backend: %s", expected)
		}
	}
}

// TestBackendShowSQLite tests `bd backend show` with SQLite backend.
func TestBackendShowSQLite(t *testing.T) {
	// Create temp directory with sqlite backend
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create metadata.json with sqlite backend
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendSQLite
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Change to temp dir
	oldCwd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	// Test text output
	t.Run("text", func(t *testing.T) {
		origJsonOutput := jsonOutput
		defer func() { jsonOutput = origJsonOutput }()
		jsonOutput = false

		output := captureBackendShowOutput(t)

		if !strings.Contains(output, "sqlite") {
			t.Errorf("output should contain 'sqlite', got: %s", output)
		}
		if !strings.Contains(output, beadsDir) {
			t.Errorf("output should contain beads dir, got: %s", output)
		}
	})

	// Test JSON output
	t.Run("json", func(t *testing.T) {
		origJsonOutput := jsonOutput
		defer func() { jsonOutput = origJsonOutput }()
		jsonOutput = true

		output := captureBackendShowOutput(t)

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, output)
		}

		if result["backend"] != "sqlite" {
			t.Errorf("expected backend 'sqlite', got %v", result["backend"])
		}
		if result["beads_dir"] == nil {
			t.Error("expected beads_dir in output")
		}
	})
}

// TestBackendShowJSONL tests `bd backend show` with JSONL-only mode.
func TestBackendShowJSONL(t *testing.T) {
	// Create temp directory with no database (jsonl only mode)
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create config.yaml with no-db: true (this is how JSONL-only mode is configured)
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("no-db: true\n"), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	// Change to temp dir
	oldCwd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	// Save and restore noDb flag
	origNoDb := noDb
	defer func() { noDb = origNoDb }()
	noDb = false // Let isNoDbModeConfigured detect from config

	// Test text output
	t.Run("text", func(t *testing.T) {
		origJsonOutput := jsonOutput
		defer func() { jsonOutput = origJsonOutput }()
		jsonOutput = false

		output := captureBackendShowOutput(t)

		if !strings.Contains(output, "jsonl") {
			t.Errorf("output should contain 'jsonl', got: %s", output)
		}
		if !strings.Contains(output, "JSONL only") {
			t.Errorf("output should mention 'JSONL only', got: %s", output)
		}
	})

	// Test JSON output
	t.Run("json", func(t *testing.T) {
		origJsonOutput := jsonOutput
		defer func() { jsonOutput = origJsonOutput }()
		jsonOutput = true

		output := captureBackendShowOutput(t)

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, output)
		}

		if result["backend"] != "jsonl" {
			t.Errorf("expected backend 'jsonl', got %v", result["backend"])
		}
	})
}

// TestAvailableBackendsStructure verifies the availableBackends slice is well-formed.
func TestAvailableBackendsStructure(t *testing.T) {
	if len(availableBackends) != 3 {
		t.Errorf("expected 3 backends, got %d", len(availableBackends))
	}

	for i, b := range availableBackends {
		if b.Name == "" {
			t.Errorf("availableBackends[%d] has empty Name", i)
		}
		if b.Description == "" {
			t.Errorf("availableBackends[%d] has empty Description", i)
		}
	}

	// Verify expected backends
	names := make(map[string]bool)
	for _, b := range availableBackends {
		names[b.Name] = true
	}

	for _, expected := range []string{"sqlite", "dolt", "jsonl"} {
		if !names[expected] {
			t.Errorf("missing backend: %s", expected)
		}
	}
}

// captureBackendListOutput runs the backend list command and captures stdout.
func captureBackendListOutput(t *testing.T) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	backendListCmd.Run(backendListCmd, []string{})

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	return buf.String()
}

// captureBackendShowOutput runs the backend show command and captures stdout.
func captureBackendShowOutput(t *testing.T) string {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// Need to catch os.Exit calls
	defer func() {
		if r := recover(); r != nil {
			// Ignore panics from os.Exit in tests
		}
	}()

	backendShowCmd.Run(backendShowCmd, []string{})

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	return buf.String()
}

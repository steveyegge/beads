package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShow_ExternalRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI test in short mode")
	}

	// Build bd binary
	tmpBin := filepath.Join(t.TempDir(), "bd")
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "./")
	buildCmd.Dir = "."
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build bd: %v\n%s", err, out)
	}

	// Create temp directory for test database
	tmpDir := t.TempDir()

	// Initialize beads
	initCmd := exec.Command(tmpBin, "init", "--prefix", "test", "--quiet")
	initCmd.Dir = tmpDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Disable auto-routing to prevent issues from being routed to ~/.beads-planning
	// The default routing.mode=auto routes contributor issues to a global planning repo
	configCmd := exec.Command(tmpBin, "config", "set", "routing.mode", "direct")
	configCmd.Dir = tmpDir
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("config set failed: %v\n%s", err, out)
	}

	// Create issue with external ref
	// Use --no-daemon for isolation (not --sandbox which disables auto-flush)
	// Use Output() (not CombinedOutput) to avoid stderr mixing with JSON
	// Use unique external_ref URL to avoid collision with any pre-existing issues
	uniqueRef := fmt.Sprintf("https://example.com/spec-%d.md", time.Now().UnixNano())
	createCmd := exec.Command(tmpBin, "--no-daemon", "create", "External ref test", "-p", "1",
		"--external-ref", uniqueRef, "--json")
	createCmd.Dir = tmpDir
	createOut, err := createCmd.Output()
	if err != nil {
		// Get stderr for debugging if command failed
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("create failed: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("create failed: %v", err)
	}

	var issue map[string]interface{}
	if err := json.Unmarshal(createOut, &issue); err != nil {
		t.Fatalf("failed to parse create output: %v, output: %s", err, createOut)
	}
	id := issue["id"].(string)
	t.Logf("Created issue with id: %s", id)

	// List issues to verify the issue was persisted
	listCmd := exec.Command(tmpBin, "--no-daemon", "list", "--json")
	listCmd.Dir = tmpDir
	listOut, listErr := listCmd.CombinedOutput()
	t.Logf("List output: %s, err: %v", listOut, listErr)

	// Show the issue and verify external ref is displayed
	showCmd := exec.Command(tmpBin, "--no-daemon", "show", id)
	showCmd.Dir = tmpDir
	showOut, err := showCmd.CombinedOutput() // Use CombinedOutput for show since we're not parsing JSON
	if err != nil {
		t.Fatalf("show failed: %v\n%s", err, showOut)
	}

	out := string(showOut)
	if !strings.Contains(out, "External:") {
		t.Errorf("expected 'External:' in output, got: %s", out)
	}
	if !strings.Contains(out, uniqueRef) {
		t.Errorf("expected external ref URL in output, got: %s", out)
	}
}

func TestShow_NoExternalRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI test in short mode")
	}

	// Build bd binary
	tmpBin := filepath.Join(t.TempDir(), "bd")
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "./")
	buildCmd.Dir = "."
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build bd: %v\n%s", err, out)
	}

	tmpDir := t.TempDir()

	// Initialize beads
	initCmd := exec.Command(tmpBin, "init", "--prefix", "test", "--quiet")
	initCmd.Dir = tmpDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Disable auto-routing to prevent issues from being routed to ~/.beads-planning
	configCmd := exec.Command(tmpBin, "config", "set", "routing.mode", "direct")
	configCmd.Dir = tmpDir
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("config set failed: %v\n%s", err, out)
	}

	// Create issue WITHOUT external ref
	// Use --no-daemon for isolation (not --sandbox which disables auto-flush)
	// Use Output() (not CombinedOutput) to avoid stderr mixing with JSON
	createCmd := exec.Command(tmpBin, "--no-daemon", "create", "No ref test", "-p", "1", "--json")
	createCmd.Dir = tmpDir
	createOut, err := createCmd.Output()
	if err != nil {
		// Get stderr for debugging if command failed
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("create failed: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("create failed: %v", err)
	}

	var issue map[string]interface{}
	if err := json.Unmarshal(createOut, &issue); err != nil {
		t.Fatalf("failed to parse create output: %v, output: %s", err, createOut)
	}
	id := issue["id"].(string)

	// Show the issue - should NOT contain External Ref line
	showCmd := exec.Command(tmpBin, "--no-daemon", "show", id)
	showCmd.Dir = tmpDir
	showOut, err := showCmd.CombinedOutput() // Use CombinedOutput for show since we're not parsing JSON
	if err != nil {
		t.Fatalf("show failed: %v\n%s", err, showOut)
	}

	out := string(showOut)
	if strings.Contains(out, "External:") {
		t.Errorf("expected no 'External:' line for issue without external ref, got: %s", out)
	}
}

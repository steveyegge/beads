package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cleanTestEnv returns os.Environ() with BD_DAEMON_HOST removed and BEADS_TEST_MODE=1 added.
// This prevents remote daemon interference in subprocess tests (bd-srr1).
func cleanTestEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "BD_DAEMON_HOST=") {
			env = append(env, e)
		}
	}
	return append(env, "BEADS_TEST_MODE=1")
}

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
	testEnv := cleanTestEnv()

	// Initialize beads
	initCmd := exec.Command(tmpBin, "init", "--prefix", "test", "--quiet")
	initCmd.Dir = tmpDir
	initCmd.Env = testEnv
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Disable auto-routing to prevent issues from being routed to ~/.beads-planning
	// The default routing.mode=auto routes contributor issues to a global planning repo
	configCmd := exec.Command(tmpBin, "config", "set", "routing.mode", "direct")
	configCmd.Dir = tmpDir
	configCmd.Env = testEnv
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("config set failed: %v\n%s", err, out)
	}

	// Create issue with external ref
	// Use --sandbox for isolation (forces direct mode, no daemon connection)
	// Use Output() (not CombinedOutput) to avoid stderr mixing with JSON
	// Use unique external_ref URL to avoid collision with any pre-existing issues
	uniqueRef := fmt.Sprintf("https://example.com/spec-%d.md", time.Now().UnixNano())
	createCmd := exec.Command(tmpBin, "--sandbox", "create", "External ref test", "-p", "1",
		"--external-ref", uniqueRef, "--json")
	createCmd.Dir = tmpDir
	createCmd.Env = testEnv
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
	listCmd := exec.Command(tmpBin, "--sandbox", "list", "--json")
	listCmd.Dir = tmpDir
	listCmd.Env = testEnv
	listOut, listErr := listCmd.CombinedOutput()
	t.Logf("List output: %s, err: %v", listOut, listErr)

	// Show the issue and verify external ref is displayed
	showCmd := exec.Command(tmpBin, "--sandbox", "show", id)
	showCmd.Dir = tmpDir
	showCmd.Env = testEnv
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
	testEnv := cleanTestEnv()

	// Initialize beads
	initCmd := exec.Command(tmpBin, "init", "--prefix", "test", "--quiet")
	initCmd.Dir = tmpDir
	initCmd.Env = testEnv
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Disable auto-routing to prevent issues from being routed to ~/.beads-planning
	configCmd := exec.Command(tmpBin, "config", "set", "routing.mode", "direct")
	configCmd.Dir = tmpDir
	configCmd.Env = testEnv
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("config set failed: %v\n%s", err, out)
	}

	// Create issue WITHOUT external ref
	// Use --sandbox for isolation (forces direct mode, no daemon connection)
	// Use Output() (not CombinedOutput) to avoid stderr mixing with JSON
	createCmd := exec.Command(tmpBin, "--sandbox", "create", "No ref test", "-p", "1", "--json")
	createCmd.Dir = tmpDir
	createCmd.Env = testEnv
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
	showCmd := exec.Command(tmpBin, "--sandbox", "show", id)
	showCmd.Dir = tmpDir
	showCmd.Env = testEnv
	showOut, err := showCmd.CombinedOutput() // Use CombinedOutput for show since we're not parsing JSON
	if err != nil {
		t.Fatalf("show failed: %v\n%s", err, showOut)
	}

	out := string(showOut)
	if strings.Contains(out, "External:") {
		t.Errorf("expected no 'External:' line for issue without external ref, got: %s", out)
	}
}

func TestShow_IDFlag(t *testing.T) {
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
	testEnv := cleanTestEnv()

	// Initialize beads
	initCmd := exec.Command(tmpBin, "init", "--prefix", "test", "--quiet")
	initCmd.Dir = tmpDir
	initCmd.Env = testEnv
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Create an issue (use --sandbox for isolation, forces direct mode)
	createCmd := exec.Command(tmpBin, "--sandbox", "create", "ID flag test", "-p", "1", "--json", "--repo", ".")
	createCmd.Dir = tmpDir
	createCmd.Env = testEnv
	createOut, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create failed: %v\n%s", err, createOut)
	}

	var issue map[string]interface{}
	if err := json.Unmarshal(createOut, &issue); err != nil {
		t.Fatalf("failed to parse create output: %v, output: %s", err, createOut)
	}
	id := issue["id"].(string)

	// Test 1: Using --id flag works
	showCmd := exec.Command(tmpBin, "--sandbox", "show", "--id="+id, "--short")
	showCmd.Dir = tmpDir
	showCmd.Env = testEnv
	showOut, err := showCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("show with --id flag failed: %v\n%s", err, showOut)
	}
	if !strings.Contains(string(showOut), id) {
		t.Errorf("expected issue ID in output, got: %s", showOut)
	}

	// Test 2: Multiple --id flags work
	showCmd2 := exec.Command(tmpBin, "--sandbox", "show", "--id="+id, "--id="+id, "--short")
	showCmd2.Dir = tmpDir
	showCmd2.Env = testEnv
	showOut2, err := showCmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("show with multiple --id flags failed: %v\n%s", err, showOut2)
	}
	// Should see the ID twice (one for each --id flag)
	if strings.Count(string(showOut2), id) != 2 {
		t.Errorf("expected issue ID twice in output, got: %s", showOut2)
	}

	// Test 3: Combining positional and --id flag
	showCmd3 := exec.Command(tmpBin, "--sandbox", "show", id, "--id="+id, "--short")
	showCmd3.Dir = tmpDir
	showCmd3.Env = testEnv
	showOut3, err := showCmd3.CombinedOutput()
	if err != nil {
		t.Fatalf("show with positional + --id failed: %v\n%s", err, showOut3)
	}
	// Should see the ID twice
	if strings.Count(string(showOut3), id) != 2 {
		t.Errorf("expected issue ID twice in output, got: %s", showOut3)
	}

	// Test 4: No args at all should fail
	showCmd4 := exec.Command(tmpBin, "--sandbox", "show")
	showCmd4.Dir = tmpDir
	showCmd4.Env = testEnv
	_, err = showCmd4.CombinedOutput()
	if err == nil {
		t.Error("expected error when no ID provided, but command succeeded")
	}
}

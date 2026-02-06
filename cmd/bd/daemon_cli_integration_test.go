//go:build integration
// +build integration

package main

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// setupDaemonCLITest creates a temp git repo, initializes beads with daemon
// support, and returns the directory and env vars for running CLI commands
// through the daemon (not direct mode).
func setupDaemonCLITest(t *testing.T) (string, []string) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("daemon CLI integration test not supported on windows")
	}

	tmpDir := createTempDirWithCleanup(t)

	// Set up a real git repo so daemon autostart is allowed.
	if err := runCommandInDir(tmpDir, "git", "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	_ = runCommandInDir(tmpDir, "git", "config", "user.email", "test@example.com")
	_ = runCommandInDir(tmpDir, "git", "config", "user.name", "Test User")

	socketPath := filepath.Join(tmpDir, ".beads", "bd.sock")
	env := []string{
		"BEADS_TEST_MODE=1",
		"BEADS_AUTO_START_DAEMON=true",
		"BEADS_NO_DAEMON=0",
		"BD_SOCKET=" + socketPath,
	}

	// Init with default (SQLite) backend.
	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--prefix", "test", "--quiet")
	if initErr != nil {
		t.Fatalf("bd init failed: %v\n%s", initErr, initOut)
	}

	// Always stop daemon on cleanup.
	t.Cleanup(func() {
		_, _ = runBDExecAllowErrorWithEnv(t, tmpDir, env, "daemon", "stop")
		time.Sleep(200 * time.Millisecond)
	})

	return tmpDir, env
}

// extractJSON finds and parses the first JSON value from output that may
// contain non-JSON prefixed lines (e.g., daemon startup messages).
// Handles both `{...}` objects and `[{...}]` arrays (returns first element).
func extractJSON(t *testing.T, output string) map[string]any {
	t.Helper()

	// Try array first (bd show/list --json returns arrays).
	if idx := strings.Index(output, "["); idx >= 0 {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(output[idx:]), &arr); err == nil && len(arr) > 0 {
			return arr[0]
		}
	}

	// Fall back to plain object.
	if idx := strings.Index(output, "{"); idx >= 0 {
		var m map[string]any
		if err := json.Unmarshal([]byte(output[idx:]), &m); err == nil {
			return m
		}
	}

	t.Fatalf("no parseable JSON found in output:\n%s", output)
	return nil
}

func TestDaemonCLI_CreateAndUpdate(t *testing.T) {
	tmpDir, env := setupDaemonCLITest(t)

	// Create an issue via CLI (goes through daemon).
	createOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "create", "daemon cli test issue", "--json")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, createOut)
	}

	created := extractJSON(t, createOut)
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected id in create output, got: %v", created)
	}

	// Update status to in_progress.
	updateOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "update", id, "--status", "in_progress")
	if err != nil {
		t.Fatalf("bd update failed: %v\n%s", err, updateOut)
	}

	// Verify the update via show --json.
	showOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "show", id, "--json")
	if err != nil {
		t.Fatalf("bd show failed: %v\n%s", err, showOut)
	}

	shown := extractJSON(t, showOut)
	if status, _ := shown["status"].(string); status != "in_progress" {
		t.Errorf("expected status 'in_progress', got %q\nfull output: %s", status, showOut)
	}
}

func TestDaemonCLI_CustomTypeCreate(t *testing.T) {
	tmpDir, env := setupDaemonCLITest(t)

	// Configure custom types via the CLI (writes to SQLite via daemon).
	configOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "config", "set", "types.custom", "spike,research")
	if err != nil {
		t.Fatalf("bd config set types.custom failed: %v\n%s", err, configOut)
	}

	// Create an issue with a custom type through the daemon.
	createOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "create", "test spike issue", "--type", "spike", "--json")
	if err != nil {
		t.Fatalf("bd create --type spike failed: %v\n%s", err, createOut)
	}

	created := extractJSON(t, createOut)
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected id in create output, got: %v", created)
	}

	// Verify the issue has the custom type.
	showOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "show", id, "--json")
	if err != nil {
		t.Fatalf("bd show failed: %v\n%s", err, showOut)
	}

	shown := extractJSON(t, showOut)
	if issueType, _ := shown["issue_type"].(string); issueType != "spike" {
		t.Errorf("expected type 'spike', got %q\nfull output: %s", issueType, showOut)
	}
}

func TestDaemonCLI_ListAndClose(t *testing.T) {
	tmpDir, env := setupDaemonCLITest(t)

	// Create two issues.
	out1, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "create", "first issue", "--json")
	if err != nil {
		t.Fatalf("create first failed: %v\n%s", err, out1)
	}
	created1 := extractJSON(t, out1)
	id1, _ := created1["id"].(string)

	out2, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "create", "second issue", "--json")
	if err != nil {
		t.Fatalf("create second failed: %v\n%s", err, out2)
	}
	created2 := extractJSON(t, out2)
	id2, _ := created2["id"].(string)

	// List should show both.
	listOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "list", "--json")
	if err != nil {
		t.Fatalf("bd list failed: %v\n%s", err, listOut)
	}
	if !strings.Contains(listOut, id1) || !strings.Contains(listOut, id2) {
		t.Errorf("expected both issue IDs in list output\nid1=%s id2=%s\noutput: %s", id1, id2, listOut)
	}

	// Close the first issue.
	closeOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "close", id1)
	if err != nil {
		t.Fatalf("bd close failed: %v\n%s", err, closeOut)
	}

	// Verify it's closed.
	showOut, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "show", id1, "--json")
	if err != nil {
		t.Fatalf("bd show failed: %v\n%s", err, showOut)
	}
	shown := extractJSON(t, showOut)
	if status, _ := shown["status"].(string); status != "closed" {
		t.Errorf("expected status 'closed', got %q", status)
	}
}

// exit_code_test.go - Tests that commands exit non-zero on partial failures.
//
// Uses exec.Command because the fix calls os.Exit(1) which cannot be
// captured in-process.

//go:build cgo && integration

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCLI_CloseBlockedExitCode tests that closing a blocked issue exits non-zero.
func TestCLI_CloseBlockedExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	// Create parent and child issues
	parentID := createExecTestIssue(t, tmpDir, "Parent issue")
	childID := createExecTestIssue(t, tmpDir, "Child blocker")

	// Add dependency: parent depends on child
	depCmd := exec.Command(testBD, "dep", "add", parentID, childID)
	depCmd.Dir = tmpDir
	depCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := depCmd.CombinedOutput(); err != nil {
		t.Fatalf("dep add failed: %v\n%s", err, out)
	}

	// Try to close parent (should fail — blocked by open child)
	closeCmd := exec.Command(testBD, "close", parentID)
	closeCmd.Dir = tmpDir
	closeCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := closeCmd.CombinedOutput()

	if err == nil {
		t.Errorf("Expected non-zero exit code when closing blocked issue, but got success. Output: %s", out)
	}
	if !strings.Contains(string(out), "blocked by open issues") {
		t.Errorf("Expected 'blocked by open issues' in stderr, got: %s", out)
	}
}

// TestCLI_CloseNonexistentExitCode tests that closing a nonexistent issue exits non-zero.
func TestCLI_CloseNonexistentExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	cmd := exec.Command(testBD, "close", "nonexistent-xyz")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	_, err := cmd.CombinedOutput()

	if err == nil {
		t.Errorf("Expected non-zero exit code when closing nonexistent issue")
	}
}

// TestCLI_UpdateNonexistentExitCode tests that updating a nonexistent issue exits non-zero.
func TestCLI_UpdateNonexistentExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	cmd := exec.Command(testBD, "update", "nonexistent-xyz", "--title", "Should fail")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	_, err := cmd.CombinedOutput()

	if err == nil {
		t.Errorf("Expected non-zero exit code when updating nonexistent issue")
	}
}

// TestCLI_ClosePartialFailureExitCode tests that closing multiple issues exits non-zero
// when some succeed and some fail.
func TestCLI_ClosePartialFailureExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	// Create two issues: one closeable, one blocked
	closeable := createExecTestIssue(t, tmpDir, "Closeable issue")
	blocked := createExecTestIssue(t, tmpDir, "Blocked issue")
	blocker := createExecTestIssue(t, tmpDir, "Blocker issue")

	// blocked depends on blocker
	depCmd := exec.Command(testBD, "dep", "add", blocked, blocker)
	depCmd.Dir = tmpDir
	depCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := depCmd.CombinedOutput(); err != nil {
		t.Fatalf("dep add failed: %v\n%s", err, out)
	}

	// Try to close both: closeable should succeed, blocked should fail
	closeCmd := exec.Command(testBD, "close", closeable, blocked)
	closeCmd.Dir = tmpDir
	closeCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := closeCmd.CombinedOutput()

	if err == nil {
		t.Errorf("Expected non-zero exit code for partial close failure, but got success. Output: %s", out)
	}

	// Verify the closeable one was actually closed
	showCmd := exec.Command(testBD, "show", closeable, "--json")
	showCmd.Dir = tmpDir
	showCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	showOut, err := showCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("show failed: %v\n%s", err, showOut)
	}
	var details []map[string]interface{}
	json.Unmarshal(showOut, &details)
	if len(details) > 0 && details[0]["status"] != "closed" {
		t.Errorf("Closeable issue should still be closed despite partial failure, got: %v", details[0]["status"])
	}
}

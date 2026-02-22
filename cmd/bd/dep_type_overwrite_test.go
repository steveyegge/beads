// dep_type_overwrite_test.go - Test that dep add rejects type change on existing pair.

//go:build cgo && integration

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCLI_DepAddRejectsTypeOverwrite tests that adding a dependency with a
// different type on an existing (issue_id, depends_on_id) pair returns an error
// instead of silently overwriting.
//
// This documents the decision: dep add errors on type mismatch; use
// dep remove + dep add to change types explicitly.
func TestCLI_DepAddRejectsTypeOverwrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	issueA := createExecTestIssue(t, tmpDir, "Issue A")
	issueB := createExecTestIssue(t, tmpDir, "Issue B")

	// Add blocks dependency
	cmd := exec.Command(testBD, "dep", "add", issueA, issueB)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial dep add failed: %v\n%s", err, out)
	}

	// Try to add caused-by on the same pair — should error
	cmd2 := exec.Command(testBD, "dep", "add", issueA, issueB, "--type", "caused-by")
	cmd2.Dir = tmpDir
	cmd2.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd2.CombinedOutput()
	if err == nil {
		t.Errorf("dep add with different type should fail, but succeeded. Output: %s", out)
	}
	if !strings.Contains(string(out), "already exists with type") {
		t.Errorf("expected 'already exists with type' error, got: %s", out)
	}
}

// TestCLI_DepAddIdempotentSameType tests that re-adding the same dependency
// with the same type is a no-op (idempotent).
func TestCLI_DepAddIdempotentSameType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	issueA := createExecTestIssue(t, tmpDir, "Issue A")
	issueB := createExecTestIssue(t, tmpDir, "Issue B")

	// Add blocks dependency twice — second should succeed silently
	for i := 0; i < 2; i++ {
		cmd := exec.Command(testBD, "dep", "add", issueA, issueB)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dep add (attempt %d) failed: %v\n%s", i+1, err, out)
		}
	}
}

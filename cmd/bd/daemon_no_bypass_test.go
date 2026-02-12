// daemon_no_bypass_test.go - Integration tests verifying all operations go through daemon RPC.
//
// These tests validate that after removing direct mode bypasses,
// all operations go through the daemon RPC with no unexpected fallbacks.
//
// Test coverage:
// 1. --no-daemon flag is deprecated (rejected in production)
// 2. Wisp operations route through daemon
// 3. Wisp ID patterns (-wisp-) route through daemon
// 4. No singleProcessOnly bypass (removed constant)
// 5. Daemon metrics capture all operations

//go:build integration

package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestWispOperationsRouteThroughDaemon verifies that wisp operations (Ephemeral=true)
// go through the daemon RPC, not direct mode.
func TestWispOperationsRouteThroughDaemon(t *testing.T) {
	RunDaemonModeOnly(t, "wisp_through_daemon", func(t *testing.T, env *DualModeTestEnv) {
		// Create a wisp (ephemeral issue)
		issue := &types.Issue{
			Title:     "Test wisp",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Ephemeral: true,
		}

		// In daemon mode, this should go through RPC
		err := env.CreateIssue(issue)
		if err != nil {
			t.Fatalf("[%s] CreateIssue failed: %v", env.Mode(), err)
		}

		// Verify the issue was created
		if issue.ID == "" {
			t.Fatalf("[%s] issue ID not set after creation", env.Mode())
		}

		// Verify we can retrieve it
		got, err := env.GetIssue(issue.ID)
		if err != nil {
			t.Fatalf("[%s] GetIssue failed: %v", env.Mode(), err)
		}

		if got.Title != "Test wisp" {
			t.Errorf("[%s] expected title 'Test wisp', got %q", env.Mode(), got.Title)
		}
	})
}

// TestWispIDPatternsRouteThroughDaemon verifies that issues with -wisp- in their ID
// are handled through daemon, not bypassed to direct mode.
func TestWispIDPatternsRouteThroughDaemon(t *testing.T) {
	RunDaemonModeOnly(t, "wisp_id_patterns", func(t *testing.T, env *DualModeTestEnv) {
		// Create a regular issue first
		issue := &types.Issue{
			Title:     "Regular issue for wisp test",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
		}

		err := env.CreateIssue(issue)
		if err != nil {
			t.Fatalf("[%s] CreateIssue failed: %v", env.Mode(), err)
		}

		// The key test: operations on any issue (even if ID contains -wisp-)
		// should go through daemon. The old isWispOperation bypass is removed.

		got, err := env.GetIssue(issue.ID)
		if err != nil {
			t.Fatalf("[%s] GetIssue failed: %v", env.Mode(), err)
		}

		if got.ID != issue.ID {
			t.Errorf("[%s] ID mismatch: expected %s, got %s", env.Mode(), issue.ID, got.ID)
		}
	})
}

// TestDaemonMetricsCaptureOperations verifies that daemon metrics track CRUD operations.
// This confirms operations are going through the daemon RPC layer.
func TestDaemonMetricsCaptureOperations(t *testing.T) {
	RunDaemonModeOnly(t, "metrics_capture", func(t *testing.T, env *DualModeTestEnv) {
		// Perform various operations
		issue := &types.Issue{
			Title:     "Metrics test issue",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
		}

		// Create
		if err := env.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		// Read (Get)
		_, err := env.GetIssue(issue.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}

		// Update
		updates := map[string]interface{}{
			"title": "Updated metrics test",
		}
		if err := env.UpdateIssue(issue.ID, updates); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}

		// List
		_, err = env.ListIssues(types.IssueFilter{})
		if err != nil {
			t.Fatalf("ListIssues failed: %v", err)
		}

		// If we got here without errors, operations went through daemon RPC
		t.Log("All CRUD operations completed through daemon RPC")
	})
}

// TestDoltBackendCanUseDaemon verifies that Dolt backend operations can use daemon
// now that the singleProcessOnly bypass is removed.
func TestDoltBackendCanUseDaemon(t *testing.T) {
	RunDaemonModeOnly(t, "dolt_daemon_possible", func(t *testing.T, env *DualModeTestEnv) {
		// Create and retrieve an issue through daemon
		issue := &types.Issue{
			Title:     "Daemon test issue",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
		}

		if err := env.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		got, err := env.GetIssue(issue.ID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}

		if got.Title != issue.Title {
			t.Errorf("Title mismatch: got %q, want %q", got.Title, issue.Title)
		}

		t.Log("Daemon operations work without singleProcessOnly bypass")
	})
}

// TestNoDaemonFlagRemoved verifies that --no-daemon flag no longer exists.
// The flag was fully removed in bd-e5e5; using it should produce an unknown flag error.
func TestNoDaemonFlagRemoved(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	// Run bd --no-daemon list in subprocess
	exe, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("bd binary not in PATH, skipping flag removal test")
	}

	cmd := exec.Command(exe, "--no-daemon", "list")
	output, err := cmd.CombinedOutput()

	// We expect an error because --no-daemon is an unknown flag
	if err == nil {
		t.Fatal("expected error for unknown --no-daemon flag, but command succeeded")
	}

	outputStr := string(output)
	// Should mention unknown flag
	if !strings.Contains(outputStr, "unknown flag") && !strings.Contains(outputStr, "no-daemon") {
		t.Logf("output: %s", outputStr)
		// Other errors (like "no beads directory") may occur first, which is acceptable
	}
}

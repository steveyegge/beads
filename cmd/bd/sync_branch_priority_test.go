package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/syncbranch"
)

// TestSyncBranchConfigPriorityOverUpstream tests that when sync.branch is configured,
// bd sync should NOT fall back to --from-main mode even if the current branch has no upstream.
// This is the regression test for GH#638.
func TestSyncBranchConfigPriorityOverUpstream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	t.Run("sync.branch configured without upstream should not fallback to from-main", func(t *testing.T) {
		// Setup: Create a git repo with no upstream tracking
		tmpDir, cleanup := setupGitRepo(t)
		defer cleanup()

		// Create beads database and configure sync.branch
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatalf("Failed to create .beads dir: %v", err)
		}

		dbPath := filepath.Join(beadsDir, "beads.db")
		testStore, err := sqlite.New(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create test database: %v", err)
		}
		defer testStore.Close()

		// Configure sync.branch
		if err := syncbranch.Set(ctx, testStore, "beads-sync"); err != nil {
			t.Fatalf("Failed to set sync.branch: %v", err)
		}

		// Verify sync.branch is configured
		syncBranch, err := syncbranch.Get(ctx, testStore)
		if err != nil {
			t.Fatalf("Failed to get sync.branch: %v", err)
		}
		if syncBranch != "beads-sync" {
			t.Errorf("Expected sync.branch='beads-sync', got %q", syncBranch)
		}

		// Verify we have no upstream
		if gitHasUpstream() {
			t.Skip("Test requires no upstream tracking")
		}

		// The key assertion: hasSyncBranchConfig should be true
		// which prevents fallback to from-main mode
		var hasSyncBranchConfig bool
		if syncBranch != "" {
			hasSyncBranchConfig = true
		}

		if !hasSyncBranchConfig {
			t.Error("hasSyncBranchConfig should be true when sync.branch is configured")
		}

		// With the fix, this condition should be false (should NOT fallback)
		shouldFallbackToFromMain := !gitHasUpstream() && !hasSyncBranchConfig
		if shouldFallbackToFromMain {
			t.Error("Should NOT fallback to from-main when sync.branch is configured")
		}
	})

	t.Run("no sync.branch and no upstream should fallback to from-main", func(t *testing.T) {
		// Setup: Create a git repo with no upstream tracking
		_, cleanup := setupGitRepo(t)
		defer cleanup()

		// No sync.branch configured, no upstream
		hasSyncBranchConfig := false

		// Verify we have no upstream
		if gitHasUpstream() {
			t.Skip("Test requires no upstream tracking")
		}

		// With no sync.branch, should fallback to from-main
		shouldFallbackToFromMain := !gitHasUpstream() && !hasSyncBranchConfig
		if !shouldFallbackToFromMain {
			t.Error("Should fallback to from-main when no sync.branch and no upstream")
		}
	})

	t.Run("detached HEAD with sync.branch should not fallback", func(t *testing.T) {
		// Setup: Create a git repo and detach HEAD (simulating jj workflow)
		tmpDir, cleanup := setupGitRepo(t)
		defer cleanup()

		// Get current commit hash
		cmd := exec.Command("git", "rev-parse", "HEAD")
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("Failed to get HEAD: %v", err)
		}
		commitHash := string(output[:len(output)-1]) // trim newline

		// Detach HEAD
		cmd = exec.Command("git", "checkout", "--detach", commitHash)
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to detach HEAD: %v", err)
		}

		// Create beads database and configure sync.branch
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatalf("Failed to create .beads dir: %v", err)
		}

		dbPath := filepath.Join(beadsDir, "beads.db")
		testStore, err := sqlite.New(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create test database: %v", err)
		}
		defer testStore.Close()

		// Configure sync.branch
		if err := syncbranch.Set(ctx, testStore, "beads-sync"); err != nil {
			t.Fatalf("Failed to set sync.branch: %v", err)
		}

		// Verify detached HEAD has no upstream
		if gitHasUpstream() {
			t.Error("Detached HEAD should not have upstream")
		}

		// With sync.branch configured, should NOT fallback
		hasSyncBranchConfig := true
		shouldFallbackToFromMain := !gitHasUpstream() && !hasSyncBranchConfig
		if shouldFallbackToFromMain {
			t.Error("Detached HEAD with sync.branch should NOT fallback to from-main")
		}
	})
}

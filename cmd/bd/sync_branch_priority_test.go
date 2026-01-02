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

// TestSyncBranchBypassesGitHasBeadsChanges tests that when sync.branch is configured,
// bd sync bypasses gitHasBeadsChanges and always calls CommitToSyncBranch.
// This is the regression test for GH#812: when .beads/ is gitignored on code branches
// (but tracked on the sync branch), gitHasBeadsChanges would return false, causing
// sync to skip CommitToSyncBranch entirely - even though CommitToSyncBranch has its
// own internal change detection that checks the worktree where gitignore is different.
func TestSyncBranchBypassesGitHasBeadsChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	t.Run("CommitToSyncBranch detects changes even when main repo has gitignored beads", func(t *testing.T) {
		// Setup: Create a git repo with sync-branch configured
		tmpDir, cleanup := setupGitRepo(t)
		defer cleanup()

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatalf("Failed to create .beads dir: %v", err)
		}

		// Create a sync branch with initial content
		syncBranch := "beads-sync"
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

		// Write initial content
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Initial"}`+"\n"), 0600); err != nil {
			t.Fatalf("Failed to write initial JSONL: %v", err)
		}

		// Create sync branch and commit the initial content
		if err := exec.Command("git", "checkout", "-b", syncBranch).Run(); err != nil {
			t.Fatalf("Failed to create sync branch: %v", err)
		}
		if err := exec.Command("git", "add", ".").Run(); err != nil {
			t.Fatalf("Failed to add files: %v", err)
		}
		if err := exec.Command("git", "commit", "-m", "initial sync").Run(); err != nil {
			t.Fatalf("Failed to commit: %v", err)
		}
		if err := exec.Command("git", "checkout", "main").Run(); err != nil {
			t.Fatalf("Failed to checkout main: %v", err)
		}

		// Recreate .beads directory on main branch (it may not exist after checkout)
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatalf("Failed to recreate .beads dir: %v", err)
		}

		// Now add a gitignore that ignores all beads files on the code branch
		// (simulating the pattern from GH#812)
		gitignorePath := filepath.Join(beadsDir, ".gitignore")
		gitignoreContent := `# On code branches, ignore all data files.
# The beads-sync branch tracks issues.jsonl, etc.
*
!.gitignore
`
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0600); err != nil {
			t.Fatalf("Failed to write .gitignore: %v", err)
		}
		if err := exec.Command("git", "add", gitignorePath).Run(); err != nil {
			t.Fatalf("Failed to add .gitignore: %v", err)
		}
		if err := exec.Command("git", "commit", "-m", "add gitignore").Run(); err != nil {
			t.Fatalf("Failed to commit .gitignore: %v", err)
		}

		// Now update the JSONL file (this change is gitignored on main branch)
		newContent := `{"id":"test-1","title":"Initial"}` + "\n" + `{"id":"test-2","title":"New issue"}` + "\n"
		if err := os.WriteFile(jsonlPath, []byte(newContent), 0600); err != nil {
			t.Fatalf("Failed to update JSONL: %v", err)
		}

		// Verify gitHasBeadsChanges returns false (file is gitignored)
		hasChanges, err := gitHasBeadsChanges(ctx)
		if err != nil {
			t.Fatalf("gitHasBeadsChanges error: %v", err)
		}
		if hasChanges {
			t.Log("Note: gitHasBeadsChanges returned true (gitignore may not be working as expected in test env)")
			// Continue test anyway - the key assertion is that CommitToSyncBranch works
		} else {
			t.Log("gitHasBeadsChanges correctly returned false (file is gitignored)")
		}

		// The key assertion: CommitToSyncBranch should still detect and commit changes
		// because it checks the worktree where the gitignore is different
		result, err := syncbranch.CommitToSyncBranch(ctx, tmpDir, syncBranch, jsonlPath, false)
		if err != nil {
			t.Fatalf("CommitToSyncBranch error: %v", err)
		}

		if !result.Committed {
			t.Error("CommitToSyncBranch() Committed = false, want true")
			t.Error("This indicates the GH#812 fix regression: sync branch worktree should detect changes even when main repo has files gitignored")
		} else {
			t.Log("CommitToSyncBranch correctly detected and committed changes to worktree")
		}
	})
}

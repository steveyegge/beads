package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/git"
)

// TestExternalBeadsDirE2E tests the full external BEADS_DIR flow.
// This is an end-to-end regression test for PR#918.
//
// When BEADS_DIR points to a separate git repository (external mode),
// sync operations should work correctly:
// 1. Changes are committed to the external beads repo (not the project repo)
// 2. Pulls from the external repo bring in remote changes
// 3. The merge algorithm works correctly across repo boundaries
func TestExternalBeadsDirE2E(t *testing.T) {
	ctx := context.Background()

	// Store original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original working directory: %v", err)
	}

	// Setup: Create the main project repo
	projectDir := t.TempDir()
	if err := setupGitRepoInDir(t, projectDir); err != nil {
		t.Fatalf("failed to setup project repo: %v", err)
	}

	// Setup: Create a separate external beads repo
	// Resolve symlinks to avoid macOS /var -> /private/var issues
	externalDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks failed: %v", err)
	}
	if err := setupGitRepoInDir(t, externalDir); err != nil {
		t.Fatalf("failed to setup external repo: %v", err)
	}

	// Create .beads directory in external repo
	externalBeadsDir := filepath.Join(externalDir, ".beads")
	if err := os.MkdirAll(externalBeadsDir, 0755); err != nil {
		t.Fatalf("failed to create external .beads dir: %v", err)
	}

	// Create issues.jsonl in external beads repo with initial issue
	jsonlPath := filepath.Join(externalBeadsDir, "issues.jsonl")
	issue1 := `{"id":"ext-1","title":"External Issue 1","status":"open","issue_type":"task","priority":2,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}`
	if err := os.WriteFile(jsonlPath, []byte(issue1+"\n"), 0644); err != nil {
		t.Fatalf("write external JSONL failed: %v", err)
	}

	// Commit initial beads files in external repo
	runGitInDir(t, externalDir, "add", ".beads")
	runGitInDir(t, externalDir, "commit", "-m", "initial beads setup")
	t.Log("✓ External beads repo initialized with issue ext-1")

	// Change to project directory (simulating user's project)
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir to project failed: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	// Reset git caches after directory change
	git.ResetCaches()

	// Test 1: isExternalBeadsDir should detect external repo
	if !isExternalBeadsDir(ctx, externalBeadsDir) {
		t.Error("isExternalBeadsDir should return true for external beads dir")
	}
	t.Log("✓ External beads dir correctly detected")

	// Test 2: getRepoRootFromPath should correctly identify external repo root
	repoRoot, err := getRepoRootFromPath(ctx, externalBeadsDir)
	if err != nil {
		t.Fatalf("getRepoRootFromPath failed: %v", err)
	}
	// Normalize paths for comparison
	resolvedExternal, _ := filepath.EvalSymlinks(externalDir)
	if repoRoot != resolvedExternal {
		t.Errorf("getRepoRootFromPath = %q, want %q", repoRoot, resolvedExternal)
	}
	t.Logf("✓ getRepoRootFromPath correctly identifies external repo: %s", repoRoot)

	// Test 3: pullFromExternalBeadsRepo should handle no-remote gracefully
	err = pullFromExternalBeadsRepo(ctx, externalBeadsDir)
	if err != nil {
		t.Errorf("pullFromExternalBeadsRepo should handle no-remote: %v", err)
	}
	t.Log("✓ Pull from external beads repo handled no-remote correctly")

	// Test 4: Create new issue and commit to external repo
	issue2 := `{"id":"ext-2","title":"External Issue 2","status":"open","issue_type":"task","priority":2,"created_at":"2025-01-02T00:00:00Z","updated_at":"2025-01-02T00:00:00Z"}`
	combinedContent := issue1 + "\n" + issue2 + "\n"
	if err := os.WriteFile(jsonlPath, []byte(combinedContent), 0644); err != nil {
		t.Fatalf("write updated JSONL failed: %v", err)
	}

	// Use commitToExternalBeadsRepo (don't push since no real remote)
	committed, err := commitToExternalBeadsRepo(ctx, externalBeadsDir, "add ext-2", false)
	if err != nil {
		t.Fatalf("commitToExternalBeadsRepo failed: %v", err)
	}
	if !committed {
		t.Error("expected commit to succeed for new issue")
	}
	t.Log("✓ Successfully committed issue ext-2 to external beads repo")

	// Test 5: Verify commit was made in external repo (not project repo)
	// Check external repo has the commit
	logOutput := getGitOutputInDir(t, externalDir, "log", "--oneline", "-1")
	if !strings.Contains(logOutput, "add ext-2") {
		t.Errorf("external repo should have commit, got: %s", logOutput)
	}
	t.Log("✓ Commit correctly made in external repo")

	// Test 6: Verify project repo is unchanged
	projectLogOutput := getGitOutputInDir(t, projectDir, "log", "--oneline", "-1")
	if strings.Contains(projectLogOutput, "add ext-2") {
		t.Error("project repo should not have beads commit")
	}
	t.Log("✓ Project repo correctly unchanged")

	// Test 7: Verify JSONL content is correct
	content, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to read JSONL: %v", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "ext-1") || !strings.Contains(contentStr, "ext-2") {
		t.Errorf("JSONL should contain both issues, got: %s", contentStr)
	}
	t.Log("✓ JSONL contains both issues")

	t.Log("✓ External BEADS_DIR E2E test completed")
}

// TestExternalBeadsDirDetection tests various edge cases for external beads dir detection.
func TestExternalBeadsDirDetection(t *testing.T) {
	ctx := context.Background()

	// Store original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original working directory: %v", err)
	}

	t.Run("same repo returns false", func(t *testing.T) {
		// Setup a single repo
		repoDir := t.TempDir()
		if err := setupGitRepoInDir(t, repoDir); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		beadsDir := filepath.Join(repoDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		// Change to repo dir
		if err := os.Chdir(repoDir); err != nil {
			t.Fatalf("chdir failed: %v", err)
		}
		defer func() { _ = os.Chdir(originalWd) }()
		git.ResetCaches()

		// Same repo should return false
		if isExternalBeadsDir(ctx, beadsDir) {
			t.Error("isExternalBeadsDir should return false for same repo")
		}
	})

	t.Run("different repo returns true", func(t *testing.T) {
		// Setup two separate repos
		projectDir := t.TempDir()
		if err := setupGitRepoInDir(t, projectDir); err != nil {
			t.Fatalf("setup project failed: %v", err)
		}

		externalDir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("eval symlinks failed: %v", err)
		}
		if err := setupGitRepoInDir(t, externalDir); err != nil {
			t.Fatalf("setup external failed: %v", err)
		}

		beadsDir := filepath.Join(externalDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		// Change to project dir
		if err := os.Chdir(projectDir); err != nil {
			t.Fatalf("chdir failed: %v", err)
		}
		defer func() { _ = os.Chdir(originalWd) }()
		git.ResetCaches()

		// Different repo should return true
		if !isExternalBeadsDir(ctx, beadsDir) {
			t.Error("isExternalBeadsDir should return true for different repo")
		}
	})

	t.Run("non-git directory returns false", func(t *testing.T) {
		// Setup a repo for cwd
		repoDir := t.TempDir()
		if err := setupGitRepoInDir(t, repoDir); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Non-git beads dir
		nonGitDir := t.TempDir()
		beadsDir := filepath.Join(nonGitDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		// Change to repo dir
		if err := os.Chdir(repoDir); err != nil {
			t.Fatalf("chdir failed: %v", err)
		}
		defer func() { _ = os.Chdir(originalWd) }()
		git.ResetCaches()

		// Non-git dir should return false (can't determine, assume local)
		if isExternalBeadsDir(ctx, beadsDir) {
			t.Error("isExternalBeadsDir should return false for non-git directory")
		}
	})
}

// TestCommitToExternalBeadsRepo tests the external repo commit function.
func TestCommitToExternalBeadsRepo(t *testing.T) {
	ctx := context.Background()

	t.Run("commits changes to external repo", func(t *testing.T) {
		// Setup external repo
		externalDir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("eval symlinks failed: %v", err)
		}
		if err := setupGitRepoInDir(t, externalDir); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		beadsDir := filepath.Join(externalDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		// Write initial JSONL
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		// Commit
		committed, err := commitToExternalBeadsRepo(ctx, beadsDir, "test commit", false)
		if err != nil {
			t.Fatalf("commit failed: %v", err)
		}
		if !committed {
			t.Error("expected commit to succeed")
		}

		// Verify commit exists
		logOutput := getGitOutputInDir(t, externalDir, "log", "--oneline", "-1")
		if !strings.Contains(logOutput, "test commit") {
			t.Errorf("commit not found in log: %s", logOutput)
		}
	})

	t.Run("returns false when no changes", func(t *testing.T) {
		// Setup external repo
		externalDir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("eval symlinks failed: %v", err)
		}
		if err := setupGitRepoInDir(t, externalDir); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		beadsDir := filepath.Join(externalDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		// Write and commit JSONL
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		runGitInDir(t, externalDir, "add", ".beads")
		runGitInDir(t, externalDir, "commit", "-m", "initial")

		// Try to commit again with no changes
		committed, err := commitToExternalBeadsRepo(ctx, beadsDir, "no changes", false)
		if err != nil {
			t.Fatalf("commit failed: %v", err)
		}
		if committed {
			t.Error("expected no commit when no changes")
		}
	})
}

// Helper: Setup git repo in a specific directory (doesn't change cwd)
func setupGitRepoInDir(t *testing.T, dir string) error {
	t.Helper()

	// Initialize git repo
	if err := exec.Command("git", "-C", dir, "init", "--initial-branch=main").Run(); err != nil {
		return err
	}

	// Configure git
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()

	// Create initial commit
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		return err
	}
	if err := exec.Command("git", "-C", dir, "add", ".").Run(); err != nil {
		return err
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", "initial commit").Run(); err != nil {
		return err
	}

	return nil
}

// Helper: Run git command in a specific directory
func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

// Helper: Get git output from a specific directory
func getGitOutputInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return string(output)
}

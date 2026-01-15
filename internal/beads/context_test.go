package beads

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/git"
)

// TestGetRepoContextForWorkspace_NormalRepo tests context resolution for a normal git repository
func TestGetRepoContextForWorkspace_NormalRepo(t *testing.T) {
	// Create a temporary git repo
	tmpDir := t.TempDir()
	if err := initGitRepo(tmpDir); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Create .beads directory with required files
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	// Create a database file (required for hasBeadsProjectFiles)
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0644); err != nil {
		t.Fatalf("failed to create beads.db: %v", err)
	}

	// Reset caches before test
	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	// Get context for the workspace
	rc, err := GetRepoContextForWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("GetRepoContextForWorkspace failed: %v", err)
	}

	// Verify context fields
	if rc.RepoRoot != resolveSymlinks(tmpDir) {
		t.Errorf("RepoRoot mismatch: expected %s, got %s", resolveSymlinks(tmpDir), rc.RepoRoot)
	}
	if rc.BeadsDir != resolveSymlinks(beadsDir) {
		t.Errorf("BeadsDir mismatch: expected %s, got %s", resolveSymlinks(beadsDir), rc.BeadsDir)
	}
	if rc.IsRedirected {
		t.Error("IsRedirected should be false for workspace-specific context")
	}
	if rc.IsWorktree {
		t.Error("IsWorktree should be false for main repo")
	}
}

// TestGetRepoContextForWorkspace_IgnoresBEADS_DIR verifies that workspace-specific
// context resolution ignores the BEADS_DIR environment variable (DMN-001)
func TestGetRepoContextForWorkspace_IgnoresBEADS_DIR(t *testing.T) {
	// Save original env var
	originalBeadsDir := os.Getenv("BEADS_DIR")
	t.Cleanup(func() {
		if originalBeadsDir != "" {
			os.Setenv("BEADS_DIR", originalBeadsDir)
		} else {
			os.Unsetenv("BEADS_DIR")
		}
		ResetCaches()
		git.ResetCaches()
	})

	// Create two separate repos: repo1 and repo2
	tmpDir := t.TempDir()
	repo1 := filepath.Join(tmpDir, "repo1")
	repo2 := filepath.Join(tmpDir, "repo2")

	for _, repo := range []string{repo1, repo2} {
		if err := os.MkdirAll(repo, 0750); err != nil {
			t.Fatalf("failed to create repo dir: %v", err)
		}
		if err := initGitRepo(repo); err != nil {
			t.Fatalf("failed to init git repo in %s: %v", repo, err)
		}
		beadsDir := filepath.Join(repo, ".beads")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatalf("failed to create .beads in %s: %v", repo, err)
		}
		if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}
	}

	// Set BEADS_DIR to repo2's .beads
	os.Setenv("BEADS_DIR", filepath.Join(repo2, ".beads"))

	// Get context for repo1 - should find repo1's .beads, NOT repo2's
	rc, err := GetRepoContextForWorkspace(repo1)
	if err != nil {
		t.Fatalf("GetRepoContextForWorkspace failed: %v", err)
	}

	// Verify we got repo1, not repo2
	expectedBeadsDir := resolveSymlinks(filepath.Join(repo1, ".beads"))
	if rc.BeadsDir != expectedBeadsDir {
		t.Errorf("BEADS_DIR was not ignored: expected %s, got %s", expectedBeadsDir, rc.BeadsDir)
	}
	expectedRepoRoot := resolveSymlinks(repo1)
	if rc.RepoRoot != expectedRepoRoot {
		t.Errorf("RepoRoot mismatch: expected %s, got %s", expectedRepoRoot, rc.RepoRoot)
	}
}

// TestGetRepoContextForWorkspace_NonexistentPath tests handling of invalid workspace paths
func TestGetRepoContextForWorkspace_NonexistentPath(t *testing.T) {
	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	_, err := GetRepoContextForWorkspace("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent workspace path")
	}
}

// TestGetRepoContextForWorkspace_NonGitDirectory tests handling of non-git directories
func TestGetRepoContextForWorkspace_NonGitDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	// Don't initialize git - just a plain directory
	_, err := GetRepoContextForWorkspace(tmpDir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

// TestGetRepoContextForWorkspace_MissingBeadsDir tests error when .beads doesn't exist
func TestGetRepoContextForWorkspace_MissingBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	if err := initGitRepo(tmpDir); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	// No .beads directory created
	_, err := GetRepoContextForWorkspace(tmpDir)
	if err == nil {
		t.Error("expected error when .beads directory is missing")
	}
}

// TestRepoContext_Validate tests the Validate method for detecting stale contexts
func TestRepoContext_Validate(t *testing.T) {
	tmpDir := t.TempDir()
	if err := initGitRepo(tmpDir); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0644); err != nil {
		t.Fatalf("failed to create beads.db: %v", err)
	}

	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	// Get initial context
	rc, err := GetRepoContextForWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("GetRepoContextForWorkspace failed: %v", err)
	}

	// Validate should pass initially
	if err := rc.Validate(); err != nil {
		t.Errorf("Validate should pass for fresh context: %v", err)
	}

	// Remove the .beads directory to make context stale
	if err := os.RemoveAll(beadsDir); err != nil {
		t.Fatalf("failed to remove .beads: %v", err)
	}

	// Validate should now fail (stale context)
	if err := rc.Validate(); err == nil {
		t.Error("Validate should fail when BeadsDir no longer exists")
	}
}

// TestRepoContext_Validate_RepoRootRemoved tests Validate when repo root is removed
func TestRepoContext_Validate_RepoRootRemoved(t *testing.T) {
	// Create repo inside a removable parent
	parentDir := t.TempDir()
	repoDir := filepath.Join(parentDir, "removable-repo")
	if err := os.MkdirAll(repoDir, 0750); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	if err := initGitRepo(repoDir); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	beadsDir := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0644); err != nil {
		t.Fatalf("failed to create beads.db: %v", err)
	}

	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	// Get context
	rc, err := GetRepoContextForWorkspace(repoDir)
	if err != nil {
		t.Fatalf("GetRepoContextForWorkspace failed: %v", err)
	}

	// Validate should pass
	if err := rc.Validate(); err != nil {
		t.Errorf("Validate should pass for fresh context: %v", err)
	}

	// Remove the entire repo
	if err := os.RemoveAll(repoDir); err != nil {
		t.Fatalf("failed to remove repo: %v", err)
	}

	// Validate should now fail (both BeadsDir and RepoRoot are gone)
	if err := rc.Validate(); err == nil {
		t.Error("Validate should fail when RepoRoot no longer exists")
	}
}

// TestGetRepoContextForWorkspace_CacheReset verifies that multiple calls return fresh contexts
func TestGetRepoContextForWorkspace_CacheReset(t *testing.T) {
	tmpDir := t.TempDir()
	if err := initGitRepo(tmpDir); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0644); err != nil {
		t.Fatalf("failed to create beads.db: %v", err)
	}

	t.Cleanup(func() {
		ResetCaches()
		git.ResetCaches()
	})

	// First call
	rc1, err := GetRepoContextForWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("first GetRepoContextForWorkspace failed: %v", err)
	}

	// Second call - should still work (fresh resolution)
	rc2, err := GetRepoContextForWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("second GetRepoContextForWorkspace failed: %v", err)
	}

	// Both should return valid contexts
	if rc1.BeadsDir != rc2.BeadsDir {
		t.Errorf("BeadsDir mismatch between calls: %s vs %s", rc1.BeadsDir, rc2.BeadsDir)
	}
}

// TestGetRepoContextForWorkspace_RelativePath tests handling of relative workspace paths
func TestGetRepoContextForWorkspace_RelativePath(t *testing.T) {
	// Get original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		os.Chdir(originalWd)
		ResetCaches()
		git.ResetCaches()
	})

	tmpDir := t.TempDir()
	if err := initGitRepo(tmpDir); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0644); err != nil {
		t.Fatalf("failed to create beads.db: %v", err)
	}

	// Change to parent directory
	parentDir := filepath.Dir(tmpDir)
	os.Chdir(parentDir)

	// Use relative path
	relPath := filepath.Base(tmpDir)
	rc, err := GetRepoContextForWorkspace(relPath)
	if err != nil {
		t.Fatalf("GetRepoContextForWorkspace with relative path failed: %v", err)
	}

	// Verify we got the correct absolute path
	expectedBeadsDir := resolveSymlinks(beadsDir)
	if rc.BeadsDir != expectedBeadsDir {
		t.Errorf("BeadsDir mismatch: expected %s, got %s", expectedBeadsDir, rc.BeadsDir)
	}
}

// initGitRepo initializes a git repository in the given directory
func initGitRepo(dir string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	// Suppress git output
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// resolveSymlinks resolves symlinks and returns the canonical path
// This handles macOS temp directory symlinks (/var -> /private/var)
func resolveSymlinks(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

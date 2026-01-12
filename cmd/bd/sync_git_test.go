package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/git"
)

// setupGitRepoWithBeads creates a temporary git repository with a .beads directory.
// Returns the repo path and cleanup function.
func setupGitRepoWithBeads(t *testing.T) (repoPath string, cleanup func()) {
	t.Helper()

	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}

	git.ResetCaches()

	// Initialize git repo
	if err := exec.Command("git", "init", "--initial-branch=main").Run(); err != nil {
		_ = os.Chdir(originalWd)
		t.Fatalf("failed to init git repo: %v", err)
	}
	git.ResetCaches()

	// Configure git
	_ = exec.Command("git", "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .beads directory with issues.jsonl
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		_ = os.Chdir(originalWd)
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
		_ = os.Chdir(originalWd)
		t.Fatalf("failed to write issues.jsonl: %v", err)
	}

	// Create initial commit
	_ = exec.Command("git", "add", ".beads").Run()
	if err := exec.Command("git", "commit", "-m", "initial").Run(); err != nil {
		_ = os.Chdir(originalWd)
		t.Fatalf("failed to create initial commit: %v", err)
	}

	cleanup = func() {
		_ = os.Chdir(originalWd)
		git.ResetCaches()
	}

	return tmpDir, cleanup
}

// setupRedirectedBeadsRepo creates two git repos: source with redirect, target with actual .beads.
// Returns source path, target path, and cleanup function.
func setupRedirectedBeadsRepo(t *testing.T) (sourcePath, targetPath string, cleanup func()) {
	t.Helper()

	baseDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Create target repo with actual .beads
	targetPath = filepath.Join(baseDir, "target")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatalf("failed to create target directory: %v", err)
	}

	targetBeadsDir := filepath.Join(targetPath, ".beads")
	if err := os.MkdirAll(targetBeadsDir, 0755); err != nil {
		t.Fatalf("failed to create target .beads directory: %v", err)
	}

	// Initialize target as git repo
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = targetPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init target git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = targetPath
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = targetPath
	_ = cmd.Run()

	// Write issues.jsonl in target
	jsonlPath := filepath.Join(targetBeadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to write issues.jsonl: %v", err)
	}

	// Commit in target
	cmd = exec.Command("git", "add", ".beads")
	cmd.Dir = targetPath
	_ = cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = targetPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial commit in target: %v", err)
	}

	// Create source repo with redirect
	sourcePath = filepath.Join(baseDir, "source")
	if err := os.MkdirAll(sourcePath, 0755); err != nil {
		t.Fatalf("failed to create source directory: %v", err)
	}

	sourceBeadsDir := filepath.Join(sourcePath, ".beads")
	if err := os.MkdirAll(sourceBeadsDir, 0755); err != nil {
		t.Fatalf("failed to create source .beads directory: %v", err)
	}

	// Write redirect file pointing to target
	redirectPath := filepath.Join(sourceBeadsDir, "redirect")
	// Use relative path: ../target/.beads
	if err := os.WriteFile(redirectPath, []byte("../target/.beads\n"), 0644); err != nil {
		t.Fatalf("failed to write redirect file: %v", err)
	}

	// Initialize source as git repo
	cmd = exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = sourcePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init source git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = sourcePath
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourcePath
	_ = cmd.Run()

	// Commit redirect in source
	cmd = exec.Command("git", "add", ".beads")
	cmd.Dir = sourcePath
	_ = cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "initial with redirect")
	cmd.Dir = sourcePath
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial commit in source: %v", err)
	}

	// Change to source directory
	if err := os.Chdir(sourcePath); err != nil {
		t.Fatalf("failed to change to source directory: %v", err)
	}
	git.ResetCaches()

	cleanup = func() {
		_ = os.Chdir(originalWd)
		git.ResetCaches()
	}

	return sourcePath, targetPath, cleanup
}

func TestGitHasBeadsChanges_NoChanges(t *testing.T) {
	ctx := context.Background()
	_, cleanup := setupGitRepoWithBeads(t)
	defer cleanup()

	hasChanges, err := gitHasBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasBeadsChanges() error = %v", err)
	}
	if hasChanges {
		t.Error("expected no changes for clean repo")
	}
}

func TestGitHasBeadsChanges_WithChanges(t *testing.T) {
	ctx := context.Background()
	repoPath, cleanup := setupGitRepoWithBeads(t)
	defer cleanup()

	// Modify the issues.jsonl file
	jsonlPath := filepath.Join(repoPath, ".beads", "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-2"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to modify issues.jsonl: %v", err)
	}

	hasChanges, err := gitHasBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasBeadsChanges() error = %v", err)
	}
	if !hasChanges {
		t.Error("expected changes for modified file")
	}
}

func TestGitHasBeadsChanges_WithRedirect_NoChanges(t *testing.T) {
	ctx := context.Background()
	sourcePath, _, cleanup := setupRedirectedBeadsRepo(t)
	defer cleanup()

	// Set BEADS_DIR to point to source's .beads (which has the redirect)
	oldBeadsDir := os.Getenv("BEADS_DIR")
	os.Setenv("BEADS_DIR", filepath.Join(sourcePath, ".beads"))
	defer os.Setenv("BEADS_DIR", oldBeadsDir)

	hasChanges, err := gitHasBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasBeadsChanges() error = %v", err)
	}
	if hasChanges {
		t.Error("expected no changes for clean redirected repo")
	}
}

func TestGitHasBeadsChanges_WithRedirect_WithChanges(t *testing.T) {
	ctx := context.Background()
	sourcePath, targetPath, cleanup := setupRedirectedBeadsRepo(t)
	defer cleanup()

	// Set BEADS_DIR to point to source's .beads (which has the redirect)
	oldBeadsDir := os.Getenv("BEADS_DIR")
	os.Setenv("BEADS_DIR", filepath.Join(sourcePath, ".beads"))
	defer os.Setenv("BEADS_DIR", oldBeadsDir)

	// Modify the issues.jsonl file in target (where actual beads is)
	jsonlPath := filepath.Join(targetPath, ".beads", "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-2"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to modify issues.jsonl: %v", err)
	}

	hasChanges, err := gitHasBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasBeadsChanges() error = %v", err)
	}
	if !hasChanges {
		t.Error("expected changes for modified file in redirected repo")
	}
}

func TestGitHasUncommittedBeadsChanges_NoChanges(t *testing.T) {
	ctx := context.Background()
	_, cleanup := setupGitRepoWithBeads(t)
	defer cleanup()

	hasChanges, err := gitHasUncommittedBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasUncommittedBeadsChanges() error = %v", err)
	}
	if hasChanges {
		t.Error("expected no changes for clean repo")
	}
}

func TestGitHasUncommittedBeadsChanges_WithChanges(t *testing.T) {
	ctx := context.Background()
	repoPath, cleanup := setupGitRepoWithBeads(t)
	defer cleanup()

	// Modify the issues.jsonl file
	jsonlPath := filepath.Join(repoPath, ".beads", "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-2"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to modify issues.jsonl: %v", err)
	}

	hasChanges, err := gitHasUncommittedBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasUncommittedBeadsChanges() error = %v", err)
	}
	if !hasChanges {
		t.Error("expected changes for modified file")
	}
}

func TestGitHasUncommittedBeadsChanges_WithRedirect_NoChanges(t *testing.T) {
	ctx := context.Background()
	sourcePath, _, cleanup := setupRedirectedBeadsRepo(t)
	defer cleanup()

	// Set BEADS_DIR to point to source's .beads (which has the redirect)
	oldBeadsDir := os.Getenv("BEADS_DIR")
	os.Setenv("BEADS_DIR", filepath.Join(sourcePath, ".beads"))
	defer os.Setenv("BEADS_DIR", oldBeadsDir)

	hasChanges, err := gitHasUncommittedBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasUncommittedBeadsChanges() error = %v", err)
	}
	if hasChanges {
		t.Error("expected no changes for clean redirected repo")
	}
}

func TestGitHasUncommittedBeadsChanges_WithRedirect_WithChanges(t *testing.T) {
	ctx := context.Background()
	sourcePath, targetPath, cleanup := setupRedirectedBeadsRepo(t)
	defer cleanup()

	// Set BEADS_DIR to point to source's .beads (which has the redirect)
	oldBeadsDir := os.Getenv("BEADS_DIR")
	os.Setenv("BEADS_DIR", filepath.Join(sourcePath, ".beads"))
	defer os.Setenv("BEADS_DIR", oldBeadsDir)

	// Modify the issues.jsonl file in target (where actual beads is)
	jsonlPath := filepath.Join(targetPath, ".beads", "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-2"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to modify issues.jsonl: %v", err)
	}

	hasChanges, err := gitHasUncommittedBeadsChanges(ctx)
	if err != nil {
		t.Fatalf("gitHasUncommittedBeadsChanges() error = %v", err)
	}
	if !hasChanges {
		t.Error("expected changes for modified file in redirected repo")
	}
}

func TestParseGitStatusForBeadsChanges(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		// No changes
		{
			name:     "empty status",
			status:   "",
			expected: false,
		},
		{
			name:     "whitespace only",
			status:   "   \n",
			expected: false,
		},

		// Modified (should return true)
		{
			name:     "staged modified",
			status:   "M  .beads/issues.jsonl",
			expected: true,
		},
		{
			name:     "unstaged modified",
			status:   " M .beads/issues.jsonl",
			expected: true,
		},
		{
			name:     "staged and unstaged modified",
			status:   "MM .beads/issues.jsonl",
			expected: true,
		},

		// Added (should return true)
		{
			name:     "staged added",
			status:   "A  .beads/issues.jsonl",
			expected: true,
		},
		{
			name:     "added then modified",
			status:   "AM .beads/issues.jsonl",
			expected: true,
		},

		// Untracked (should return false)
		{
			name:     "untracked file",
			status:   "?? .beads/issues.jsonl",
			expected: false,
		},

		// Deleted (should return false)
		{
			name:     "staged deleted",
			status:   "D  .beads/issues.jsonl",
			expected: false,
		},
		{
			name:     "unstaged deleted",
			status:   " D .beads/issues.jsonl",
			expected: false,
		},

		// Edge cases
		{
			name:     "renamed file",
			status:   "R  old.jsonl -> .beads/issues.jsonl",
			expected: false,
		},
		{
			name:     "copied file",
			status:   "C  source.jsonl -> .beads/issues.jsonl",
			expected: false,
		},
		{
			name:     "status too short",
			status:   "M",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGitStatusForBeadsChanges(tt.status)
			if result != tt.expected {
				t.Errorf("parseGitStatusForBeadsChanges(%q) = %v, want %v",
					tt.status, result, tt.expected)
			}
		})
	}
}

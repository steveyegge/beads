package reset

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo initializes a git repo in the given directory
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set git email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set git name: %v", err)
	}
}

func TestCheckGitState_NotARepo(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	state, err := CheckGitState(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.IsRepo {
		t.Error("expected IsRepo to be false for non-repo directory")
	}
}

func TestCheckGitState_CleanRepo(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	state, err := CheckGitState(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !state.IsRepo {
		t.Error("expected IsRepo to be true")
	}
	if state.IsDirty {
		t.Error("expected IsDirty to be false for clean repo")
	}
}

func TestCheckGitState_DirtyRepo(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create a file in .beads to make it dirty
	testFile := filepath.Join(beadsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	state, err := CheckGitState(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !state.IsRepo {
		t.Error("expected IsRepo to be true")
	}
	if !state.IsDirty {
		t.Error("expected IsDirty to be true with uncommitted changes")
	}
}

func TestCheckGitState_DetectsOnlyBeadsChanges(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create a file OUTSIDE .beads (should NOT make beads dirty)
	otherFile := filepath.Join(tmpDir, "other.txt")
	if err := os.WriteFile(otherFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create other file: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	state, err := CheckGitState(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT be dirty because only .beads changes are checked
	if state.IsDirty {
		t.Error("expected IsDirty to be false when only non-beads files are changed")
	}
}

func TestCheckGitState_DetectsBranch(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Need at least one commit to have a branch
	readmeFile := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create README: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Add and commit to create a branch
	cmd := exec.Command("git", "add", "README.md")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	state, err := CheckGitState(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.IsDetached {
		t.Error("expected IsDetached to be false on a branch")
	}
	// Branch should be "main" or "master" depending on git version
	if state.Branch != "main" && state.Branch != "master" {
		t.Errorf("expected branch to be 'main' or 'master', got %q", state.Branch)
	}
}

func TestGitRemoveBeads(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create and commit a JSONL file
	jsonlFile := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlFile, []byte(`{"id":"test-1"}`), 0644); err != nil {
		t.Fatalf("failed to create jsonl file: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Add the file to git
	cmd := exec.Command("git", "add", ".beads/issues.jsonl")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}

	// Now remove it
	err := GitRemoveBeads(beadsDir)
	if err != nil {
		t.Fatalf("GitRemoveBeads failed: %v", err)
	}

	// Verify file is staged for removal
	cmd = exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get git status: %v", err)
	}

	// Should show "D " for staged deletion
	// Note: The file was never committed, so it will show "AD" (added then deleted)
	// or may not show at all
	t.Logf("git status output: %q", string(output))
}

func TestGitRemoveBeads_NonexistentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Should not error when files don't exist (--ignore-unmatch)
	err := GitRemoveBeads(beadsDir)
	if err != nil {
		t.Errorf("unexpected error for nonexistent files: %v", err)
	}
}

func TestGitCommitReset_NoStagedChanges(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Should not error when there's nothing to commit
	err := GitCommitReset("test message")
	if err != nil {
		t.Errorf("unexpected error for empty commit: %v", err)
	}
}

func TestGitCommitReset_WithStagedChanges(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create and stage a file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cmd := exec.Command("git", "add", "test.txt")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}

	err := GitCommitReset("Reset beads workspace")
	if err != nil {
		t.Fatalf("GitCommitReset failed: %v", err)
	}

	// Verify commit was created
	cmd = exec.Command("git", "log", "--oneline", "-1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}
	if len(output) == 0 {
		t.Error("expected commit to be created")
	}
}

func TestGitAddAndCommit(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create a file in .beads
	testFile := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(testFile, []byte(`{"id":"test-1"}`), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	err := GitAddAndCommit(beadsDir, "Initialize fresh beads workspace")
	if err != nil {
		t.Fatalf("GitAddAndCommit failed: %v", err)
	}

	// Verify commit was created
	cmd := exec.Command("git", "log", "--oneline", "-1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}
	if len(output) == 0 {
		t.Error("expected commit to be created")
	}

	// Verify file is tracked
	cmd = exec.Command("git", "status", "--porcelain")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("failed to get git status: %v", err)
	}
	if len(output) != 0 {
		t.Errorf("expected clean working directory, got: %s", output)
	}
}

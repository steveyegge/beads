package fix

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestWorkspace creates a temporary directory with a .beads directory
func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}
	return dir
}

// setupTestGitRepo creates a temporary git repository with a .beads directory
func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := setupTestWorkspace(t)

	// Initialize git repo from cached template
	initGitTemplate()
	if gitTemplateErr != nil {
		t.Fatalf("git template init failed: %v", gitTemplateErr)
	}
	if err := copyGitDir(gitTemplateDir, dir); err != nil {
		t.Fatalf("failed to copy git template: %v", err)
	}

	return dir
}

// runGit runs a git command and returns output
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("git %v: %s", args, output)
	}
	return string(output)
}

// TestValidateBeadsWorkspace tests the workspace validation function
func TestValidateBeadsWorkspace(t *testing.T) {
	t.Run("invalid path", func(t *testing.T) {
		err := validateBeadsWorkspace("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Error("expected error for nonexistent path")
		}
	})
}

// TestGitHooks_Validation tests GitHooks validation
func TestGitHooks_Validation(t *testing.T) {
	t.Run("not a git repository", func(t *testing.T) {
		dir := setupTestWorkspace(t)
		err := GitHooks(dir)
		if err == nil {
			t.Error("expected error for non-git repository")
		}
		if err.Error() != "not a git repository" {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestUntrackedJSONL_Validation tests UntrackedJSONL validation
func TestUntrackedJSONL_Validation(t *testing.T) {
	t.Run("not a git repository", func(t *testing.T) {
		dir := setupTestWorkspace(t)
		err := UntrackedJSONL(dir)
		if err == nil {
			t.Error("expected error for non-git repository")
		}
	})

	t.Run("no untracked files", func(t *testing.T) {
		dir := setupTestGitRepo(t)
		err := UntrackedJSONL(dir)
		// Should succeed with no untracked files
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})
}

// TestFindJSONLPath tests the findJSONLPath helper
func TestFindJSONLPath(t *testing.T) {
	t.Run("returns empty for no JSONL", func(t *testing.T) {
		dir := t.TempDir()
		path := findJSONLPath(dir)
		if path != "" {
			t.Errorf("expected empty path, got %s", path)
		}
	})

	t.Run("finds issues.jsonl", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte("{}"), 0600); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		path := findJSONLPath(dir)
		if path != jsonlPath {
			t.Errorf("expected %s, got %s", jsonlPath, path)
		}
	})

	t.Run("finds beads.jsonl as fallback", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "beads.jsonl")
		if err := os.WriteFile(jsonlPath, []byte("{}"), 0600); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		path := findJSONLPath(dir)
		if path != jsonlPath {
			t.Errorf("expected %s, got %s", jsonlPath, path)
		}
	})

	t.Run("prefers issues.jsonl over beads.jsonl", func(t *testing.T) {
		dir := t.TempDir()
		issuesPath := filepath.Join(dir, "issues.jsonl")
		beadsPath := filepath.Join(dir, "beads.jsonl")
		if err := os.WriteFile(issuesPath, []byte("{}"), 0600); err != nil {
			t.Fatalf("failed to create issues.jsonl: %v", err)
		}
		if err := os.WriteFile(beadsPath, []byte("{}"), 0600); err != nil {
			t.Fatalf("failed to create beads.jsonl: %v", err)
		}

		path := findJSONLPath(dir)
		if path != issuesPath {
			t.Errorf("expected %s, got %s", issuesPath, path)
		}
	})
}

// TestIsWithinWorkspace tests the isWithinWorkspace helper
func TestIsWithinWorkspace(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{
			name:      "same directory",
			candidate: root,
			want:      true,
		},
		{
			name:      "subdirectory",
			candidate: filepath.Join(root, "subdir"),
			want:      true,
		},
		{
			name:      "nested subdirectory",
			candidate: filepath.Join(root, "sub", "dir", "nested"),
			want:      true,
		},
		{
			name:      "parent directory",
			candidate: filepath.Dir(root),
			want:      false,
		},
		{
			name:      "sibling directory",
			candidate: filepath.Join(filepath.Dir(root), "sibling"),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWithinWorkspace(root, tt.candidate)
			if got != tt.want {
				t.Errorf("isWithinWorkspace(%q, %q) = %v, want %v", root, tt.candidate, got, tt.want)
			}
		})
	}
}

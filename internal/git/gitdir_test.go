package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetGitHooksDirTildeExpansion(t *testing.T) {
	repoPath, cleanup := setupTestRepo(t)
	defer cleanup()

	// Use an explicit temporary HOME so tilde expansion is deterministic
	// regardless of the environment (CI, containers, overridden HOME, etc.).
	fakeHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", fakeHome)
	t.Cleanup(func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	})

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("skipping: cannot determine home directory: %v", err)
	}

	tests := []struct {
		name      string
		hooksPath string
		wantDir   string
	}{
		{
			name:      "tilde with forward slash",
			hooksPath: "~/.githooks",
			wantDir:   filepath.Join(homeDir, ".githooks"),
		},
		{
			name:      "tilde with backslash",
			hooksPath: `~\.githooks`,
			wantDir:   filepath.Join(homeDir, ".githooks"),
		},
		{
			name:      "bare tilde",
			hooksPath: "~",
			wantDir:   homeDir,
		},
		{
			name:      "relative path without tilde",
			hooksPath: ".beads/hooks",
			wantDir:   filepath.Join(repoPath, ".beads", "hooks"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetCaches()

			cmd := exec.Command("git", "config", "core.hooksPath", tt.hooksPath)
			cmd.Dir = repoPath
			if err := cmd.Run(); err != nil {
				t.Skipf("git config rejected core.hooksPath %q: %v", tt.hooksPath, err)
			}

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get working directory: %v", err)
			}
			if err := os.Chdir(repoPath); err != nil {
				t.Fatalf("Failed to chdir to test repo: %v", err)
			}
			t.Cleanup(func() { os.Chdir(originalDir) })

			gotDir, err := GetGitHooksDir()
			if err != nil {
				t.Fatalf("GetGitHooksDir() returned error: %v", err)
			}

			if gotDir != tt.wantDir {
				t.Errorf("GetGitHooksDir() = %q, want %q", gotDir, tt.wantDir)
			}
		})
	}
}

package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetGitHooksDirTildeExpansion(t *testing.T) {
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
		// wantDir is either an absolute path or "REPO_RELATIVE:" prefix
		// meaning the expected path is relative to the subtest's repo root.
		wantDir string
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
			wantDir:   "REPO_RELATIVE:.beads/hooks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each subtest gets its own repo to avoid git config corruption.
			// Setting core.hooksPath to a backslash-tilde path (e.g. ~\.githooks)
			// causes all subsequent git commands to fail with "failed to expand
			// user dir", and even `git config --unset` cannot recover.
			subRepoPath, subCleanup := setupTestRepo(t)
			defer subCleanup()
			ResetCaches()

			cmd := exec.Command("git", "config", "core.hooksPath", tt.hooksPath)
			cmd.Dir = subRepoPath
			if err := cmd.Run(); err != nil {
				t.Skipf("git config rejected core.hooksPath %q: %v", tt.hooksPath, err)
			}

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get working directory: %v", err)
			}
			if err := os.Chdir(subRepoPath); err != nil {
				t.Fatalf("Failed to chdir to test repo: %v", err)
			}
			t.Cleanup(func() { os.Chdir(originalDir) })

			gotDir, err := GetGitHooksDir()
			if err != nil {
				t.Fatalf("GetGitHooksDir() returned error: %v", err)
			}

			wantDir := tt.wantDir
			const repoRelPrefix = "REPO_RELATIVE:"
			if len(wantDir) > len(repoRelPrefix) && wantDir[:len(repoRelPrefix)] == repoRelPrefix {
				wantDir = filepath.Join(subRepoPath, wantDir[len(repoRelPrefix):])
			}

			// On macOS, /var is a symlink to /private/var, so we need to resolve
			// symlinks before comparing paths for equality.
			gotDirResolved, _ := filepath.EvalSymlinks(gotDir)
			wantDirResolved, _ := filepath.EvalSymlinks(wantDir)
			if gotDirResolved != wantDirResolved {
				t.Errorf("GetGitHooksDir() = %q (resolved: %q), want %q (resolved: %q)",
					gotDir, gotDirResolved, wantDir, wantDirResolved)
			}
		})
	}
}

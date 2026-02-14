package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitRepo creates a temporary git repository for testing
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create .beads directory
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	// Initialize git repo with 'main' as default branch (modern git convention)
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	_ = cmd.Run()

	return dir
}

func TestCheckGitHooks(t *testing.T) {
	// This test needs to run in a git repository
	// We test the basic case where hooks are not installed
	t.Run("not in git repo returns N/A", func(t *testing.T) {
		tmpDir := t.TempDir()
		runInDir(t, tmpDir, func() {
			check := CheckGitHooks()

			if check.Status != StatusOK {
				t.Errorf("expected status %q, got %q", StatusOK, check.Status)
			}
			if check.Message != "N/A (not a git repository)" {
				t.Errorf("unexpected message: %s", check.Message)
			}
		})
	})
}

func TestCheckMergeDriver(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectMessage  string
	}{
		{
			name: "not a git repo",
			setup: func(t *testing.T, dir string) {
				// Just create .beads directory, no git
				beadsDir := filepath.Join(dir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: "ok", // path-local detection: returns N/A when not in git repo
			expectMessage:  "",   // Message varies
		},
		{
			name: "merge driver not configured",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
			},
			expectedStatus: "warning",
			expectMessage:  "Git merge driver not configured",
		},
		{
			name: "correct config",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.driver", "bd merge %A %O %A %B")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			expectedStatus: "ok",
			expectMessage:  "Correctly configured",
		},
		{
			name: "incorrect config with old placeholders",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.driver", "bd merge %L %O %A %R")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			expectedStatus: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckMergeDriver(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectMessage != "" && check.Message != tt.expectMessage {
				t.Errorf("expected message %q, got %q", tt.expectMessage, check.Message)
			}
		})
	}
}

func TestCheckSyncBranchConfig(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
	}{
		{
			name: "no .beads directory",
			setup: func(t *testing.T, dir string) {
				// Empty directory, no .beads
			},
			expectedStatus: "ok",
		},
		{
			name: "not a git repo",
			setup: func(t *testing.T, dir string) {
				beadsDir := filepath.Join(dir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: "ok",
		},
		{
			name: "no remote configured",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
			},
			expectedStatus: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckSyncBranchConfig(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
		})
	}
}

func TestCheckSyncBranchHealth(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectMessage  string
	}{
		{
			name: "no sync branch configured (not a git repo)",
			setup: func(t *testing.T, dir string) {
				// No git init — path-local detection returns "not a git repository"
				beadsDir := filepath.Join(dir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: "ok",
			expectMessage:  "N/A (not a git repository)",
		},
		{
			name: "no sync branch configured with git",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
			},
			expectedStatus: "ok",
			expectMessage:  "N/A (no sync branch configured)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckSyncBranchHealth(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectMessage != "" && check.Message != tt.expectMessage {
				t.Errorf("expected message %q, got %q", tt.expectMessage, check.Message)
			}
		})
	}
}

func TestCheckSyncBranchHookCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
	}{
		{
			name: "sync-branch not configured",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
			},
			expectedStatus: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckSyncBranchHookCompatibility(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q", tt.expectedStatus, check.Status)
			}
		})
	}
}

// setupGitRepoInDir initializes a git repo in the given directory with .beads
func setupGitRepoInDir(t *testing.T, dir string) {
	t.Helper()

	// Create .beads directory
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	// Initialize git repo with 'main' as default branch (modern git convention)
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	_ = cmd.Run()
}

// Edge case tests for CheckGitHooks

func TestCheckGitHooks_CorruptedHookFiles(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectInMsg    string
	}{
		{
			name: "pre-commit hook is directory instead of file",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				// create pre-commit as directory instead of file
				os.MkdirAll(filepath.Join(hooksDir, "pre-commit"), 0755)
				// create valid post-merge and pre-push hooks
				os.WriteFile(filepath.Join(hooksDir, "post-merge"), []byte("#!/bin/sh\nbd sync\n"), 0755)
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte("#!/bin/sh\nbd sync\n"), 0755)
			},
			// os.Stat reports directories as existing, so CheckGitHooks sees it as installed
			expectedStatus: "ok",
			expectInMsg:    "All recommended hooks installed",
		},
		{
			name: "hook file with no execute permissions",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				// create hooks but with no execute permissions
				os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte("#!/bin/sh\nbd sync\n"), 0644)
				os.WriteFile(filepath.Join(hooksDir, "post-merge"), []byte("#!/bin/sh\nbd sync\n"), 0644)
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte("#!/bin/sh\nbd sync\n"), 0644)
			},
			expectedStatus: "ok",
			expectInMsg:    "All recommended hooks installed",
		},
		{
			name: "empty hook file",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				// create empty hook files
				os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte(""), 0755)
				os.WriteFile(filepath.Join(hooksDir, "post-merge"), []byte(""), 0755)
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(""), 0755)
			},
			expectedStatus: "ok",
			expectInMsg:    "All recommended hooks installed",
		},
		{
			name: "hook file with binary content",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				// create hooks with binary content
				binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
				os.WriteFile(filepath.Join(hooksDir, "pre-commit"), binaryContent, 0755)
				os.WriteFile(filepath.Join(hooksDir, "post-merge"), binaryContent, 0755)
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), binaryContent, 0755)
			},
			expectedStatus: "ok",
			expectInMsg:    "All recommended hooks installed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			runInDir(t, tmpDir, func() {
				check := CheckGitHooks()

				if check.Status != tt.expectedStatus {
					t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
				}
				if tt.expectInMsg != "" && !strings.Contains(check.Message, tt.expectInMsg) {
					t.Errorf("expected message to contain %q, got %q", tt.expectInMsg, check.Message)
				}
			})
		})
	}
}

// Edge case tests for CheckMergeDriver

func TestCheckMergeDriver_PartiallyConfigured(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectInMsg    string
	}{
		{
			name: "only merge.beads.name configured",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.name", "Beads merge driver")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			expectedStatus: "warning",
			expectInMsg:    "not configured",
		},
		{
			name: "empty merge driver config",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.driver", "")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			// git config trims to empty string, which is non-standard
			expectedStatus: "warning",
			expectInMsg:    "Non-standard",
		},
		{
			name: "merge driver with extra spaces",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.driver", "  bd merge %A %O %A %B  ")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			// git config stores the value with spaces, but the code trims it
			expectedStatus: "ok",
			expectInMsg:    "Correctly configured",
		},
		{
			name: "merge driver with wrong bd path",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.driver", "/usr/local/bin/bd merge %A %O %A %B")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			expectedStatus: "warning",
			expectInMsg:    "Non-standard",
		},
		{
			name: "merge driver with only two placeholders",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				cmd := exec.Command("git", "config", "merge.beads.driver", "bd merge %A %O")
				cmd.Dir = dir
				if err := cmd.Run(); err != nil {
					t.Fatalf("failed to set git config: %v", err)
				}
			},
			expectedStatus: "warning",
			expectInMsg:    "Non-standard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckMergeDriver(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectInMsg != "" && !strings.Contains(check.Message, tt.expectInMsg) {
				t.Errorf("expected message to contain %q, got %q", tt.expectInMsg, check.Message)
			}
		})
	}
}

// Edge case tests for CheckSyncBranchConfig

func TestCheckSyncBranchConfig_MultipleRemotes(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectInMsg    string
	}{
		{
			name: "multiple remotes without sync-branch",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// add multiple remotes
				cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/user/repo.git")
				cmd.Dir = dir
				_ = cmd.Run()
				cmd = exec.Command("git", "remote", "add", "upstream", "https://github.com/upstream/repo.git")
				cmd.Dir = dir
				_ = cmd.Run()
			},
			expectedStatus: "warning",
			expectInMsg:    "not configured",
		},
		{
			name: "multiple remotes with sync-branch configured via env",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// add multiple remotes
				cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/user/repo.git")
				cmd.Dir = dir
				_ = cmd.Run()
				cmd = exec.Command("git", "remote", "add", "upstream", "https://github.com/upstream/repo.git")
				cmd.Dir = dir
				_ = cmd.Run()
				// use env var to configure sync-branch since config package reads from cwd
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "ok",
			expectInMsg:    "Configured",
		},
		{
			name: "no remotes at all",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
			},
			expectedStatus: "ok",
			expectInMsg:    "no remote configured",
		},
		{
			name: "on sync branch itself via env (error case)",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// create and checkout sync branch
				cmd := exec.Command("git", "checkout", "-b", "beads-sync")
				cmd.Dir = dir
				_ = cmd.Run()
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "error",
			expectInMsg:    "On sync branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckSyncBranchConfig(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectInMsg != "" && !strings.Contains(check.Message, tt.expectInMsg) {
				t.Errorf("expected message to contain %q, got %q", tt.expectInMsg, check.Message)
			}
		})
	}
}

// Edge case tests for CheckSyncBranchHealth

func TestCheckSyncBranchHealth_DetachedHEAD(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectInMsg    string
	}{
		{
			name: "detached HEAD without sync-branch",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// create initial commit
				testFile := filepath.Join(dir, "test.txt")
				os.WriteFile(testFile, []byte("test"), 0644)
				cmd := exec.Command("git", "add", "test.txt")
				cmd.Dir = dir
				_ = cmd.Run()
				cmd = exec.Command("git", "commit", "-m", "initial commit")
				cmd.Dir = dir
				_ = cmd.Run()
				// detach HEAD
				cmd = exec.Command("git", "checkout", "HEAD^0")
				cmd.Dir = dir
				_ = cmd.Run()
			},
			expectedStatus: "ok",
			expectInMsg:    "no sync branch configured",
		},
		{
			name: "detached HEAD with sync-branch configured via env",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// create initial commit
				testFile := filepath.Join(dir, "test.txt")
				os.WriteFile(testFile, []byte("test"), 0644)
				cmd := exec.Command("git", "add", "test.txt")
				cmd.Dir = dir
				_ = cmd.Run()
				cmd = exec.Command("git", "commit", "-m", "initial commit")
				cmd.Dir = dir
				_ = cmd.Run()
				// create sync branch
				cmd = exec.Command("git", "branch", "beads-sync")
				cmd.Dir = dir
				_ = cmd.Run()
				// detach HEAD
				cmd = exec.Command("git", "checkout", "HEAD^0")
				cmd.Dir = dir
				_ = cmd.Run()
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "ok",
			expectInMsg:    "remote", // no remote configured, so returns "N/A (remote ... not found)"
		},
		{
			name: "sync branch exists but remote doesn't",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// create initial commit
				testFile := filepath.Join(dir, "test.txt")
				os.WriteFile(testFile, []byte("test"), 0644)
				cmd := exec.Command("git", "add", "test.txt")
				cmd.Dir = dir
				_ = cmd.Run()
				cmd = exec.Command("git", "commit", "-m", "initial commit")
				cmd.Dir = dir
				_ = cmd.Run()
				// create sync branch
				cmd = exec.Command("git", "branch", "beads-sync")
				cmd.Dir = dir
				_ = cmd.Run()
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "ok",
			expectInMsg:    "remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckSyncBranchHealth(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectInMsg != "" && !strings.Contains(check.Message, tt.expectInMsg) {
				t.Errorf("expected message to contain %q, got %q", tt.expectInMsg, check.Message)
			}
		})
	}
}

// Edge case tests for CheckSyncBranchHookCompatibility

func TestCheckSyncBranchHookCompatibility_OldHookFormat(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectInMsg    string
	}{
		{
			name: "old hook without version marker",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// create old-style pre-push hook without version
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				hookContent := "#!/bin/sh\n# Old hook without version\nbd sync\n"
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(hookContent), 0755)
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "warning",
			expectInMsg:    "not a bd hook",
		},
		{
			name: "hook with version 0.28.0 (old format)",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				hookContent := "#!/bin/sh\n# bd-hooks-version: 0.28.0\nbd sync\n"
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(hookContent), 0755)
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "error",
			expectInMsg:    "incompatible",
		},
		{
			name: "hook with version 0.29.0 (compatible)",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				hookContent := "#!/bin/sh\n# bd-hooks-version: 0.29.0\nbd sync\n"
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(hookContent), 0755)
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "ok",
			expectInMsg:    "compatible",
		},
		{
			name: "hook with malformed version",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				hookContent := "#!/bin/sh\n# bd-hooks-version: invalid\nbd sync\n"
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(hookContent), 0755)
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "error",
			expectInMsg:    "incompatible",
		},
		{
			name: "hook with version marker but no value",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				hookContent := "#!/bin/sh\n# bd-hooks-version:\nbd sync\n"
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(hookContent), 0755)
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "warning",
			expectInMsg:    "Could not determine",
		},
		// Note: core.hooksPath is NOT respected by this check (or CheckGitHooks)
		// Both functions use .git/hooks/ for consistency. This is a known limitation.
		// A future fix could make both respect core.hooksPath.
		{
			name: "hook in standard location with core.hooksPath set elsewhere",
			setup: func(t *testing.T, dir string) {
				setupGitRepoInDir(t, dir)
				// Put hook in standard .git/hooks location
				gitDir := filepath.Join(dir, ".git")
				hooksDir := filepath.Join(gitDir, "hooks")
				os.MkdirAll(hooksDir, 0755)
				hookContent := "#!/bin/sh\n# bd-hooks-version: 0.29.0\nbd sync\n"
				os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(hookContent), 0755)
				// configure core.hooksPath (ignored by this check)
				cmd := exec.Command("git", "config", "core.hooksPath", ".git-hooks")
				cmd.Dir = dir
				_ = cmd.Run()
				// use env var to configure sync-branch
				os.Setenv("BEADS_SYNC_BRANCH", "beads-sync")
				t.Cleanup(func() { os.Unsetenv("BEADS_SYNC_BRANCH") })
			},
			expectedStatus: "ok",
			expectInMsg:    "compatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			check := CheckSyncBranchHookCompatibility(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectInMsg != "" && !strings.Contains(check.Message, tt.expectInMsg) {
				t.Errorf("expected message to contain %q, got %q", tt.expectInMsg, check.Message)
			}
		})
	}
}

// Tests for CheckOrphanedIssues

func TestCheckOrphanedIssues_NoGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	// No git init - just a plain directory

	check := CheckOrphanedIssues(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("expected status %q, got %q", StatusOK, check.Status)
	}
	if !strings.Contains(check.Message, "not a git repository") {
		t.Errorf("expected message about not a git repository, got %q", check.Message)
	}
}

func TestCheckOrphanedIssues_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo WITHOUT creating .beads
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	check := CheckOrphanedIssues(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("expected status %q, got %q", StatusOK, check.Status)
	}
	if !strings.Contains(check.Message, "no .beads directory") {
		t.Errorf("expected message about no .beads directory, got %q", check.Message)
	}
}

func TestCheckOrphanedIssues_NoDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	setupGitRepoInDir(t, tmpDir)

	// Create .beads directory but no database
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := CheckOrphanedIssues(tmpDir)

	// Without a database, FindOrphanedIssuesFromPath returns empty and
	// openDoctorDB also fails, so we get "No open issues" or similar OK message.
	if check.Status != StatusOK {
		t.Errorf("expected status %q, got %q (message: %s)", StatusOK, check.Status, check.Message)
	}
}

// TestCheckOrphanedIssues_NoOpenIssues verifies behavior when database has no open issues.
// Skipped: requires SQLite which has been removed. Orphan detection with Dolt is
// tested via integration tests and orphans_provider_test.go.
func TestCheckOrphanedIssues_NoOpenIssues(t *testing.T) {
	t.Skip("Requires SQLite — orphan detection with Dolt is tested via integration tests")
}

// TestCheckOrphanedIssues_OpenIssueNotInCommits verifies open issue not in commits is OK.
// Skipped: requires SQLite which has been removed. Orphan detection with Dolt is
// tested via integration tests and orphans_provider_test.go.
func TestCheckOrphanedIssues_OpenIssueNotInCommits(t *testing.T) {
	t.Skip("Requires SQLite — orphan detection with Dolt is tested via integration tests")
}

// TestCheckOrphanedIssues_OpenIssueInCommit verifies open issue referenced in commit triggers warning.
// Skipped: requires SQLite which has been removed. Orphan detection with Dolt is
// tested via integration tests and orphans_provider_test.go.
func TestCheckOrphanedIssues_OpenIssueInCommit(t *testing.T) {
	t.Skip("Requires SQLite — orphan detection with Dolt is tested via integration tests")
}

// TestCheckOrphanedIssues_ClosedIssueInCommit verifies closed issue in commit is OK.
// Skipped: requires SQLite which has been removed. Orphan detection with Dolt is
// tested via integration tests and orphans_provider_test.go.
func TestCheckOrphanedIssues_ClosedIssueInCommit(t *testing.T) {
	t.Skip("Requires SQLite — orphan detection with Dolt is tested via integration tests")
}

// TestCheckOrphanedIssues_HierarchicalIssueID verifies hierarchical issue IDs are detected.
// Skipped: requires SQLite which has been removed. Orphan detection with Dolt is
// tested via integration tests and orphans_provider_test.go (see UT-09).
func TestCheckOrphanedIssues_HierarchicalIssueID(t *testing.T) {
	t.Skip("Requires SQLite — orphan detection with Dolt is tested via integration tests")
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/steveyegge/beads/internal/git"
)

func TestGetEmbeddedHooks(t *testing.T) {
	hooks, err := getEmbeddedHooks()
	if err != nil {
		t.Fatalf("getEmbeddedHooks() failed: %v", err)
	}

	expectedHooks := []string{"pre-commit", "post-merge", "pre-push", "post-checkout"}
	for _, hookName := range expectedHooks {
		content, ok := hooks[hookName]
		if !ok {
			t.Errorf("Missing hook: %s", hookName)
			continue
		}
		if len(content) == 0 {
			t.Errorf("Hook %s has empty content", hookName)
		}
		// Verify it's a shell script
		if content[:2] != "#!" {
			t.Errorf("Hook %s doesn't start with shebang: %s", hookName, content[:50])
		}
	}
}

func TestInstallHooks(t *testing.T) {
	tmpDir := t.TempDir()
	runInDir(t, tmpDir, func() {
		if err := exec.Command("git", "init").Run(); err != nil {
			t.Skipf("Skipping test: git init failed: %v", err)
		}

		gitDirPath, err := git.GetGitDir()
		if err != nil {
			t.Fatalf("git.GetGitDir() failed: %v", err)
		}
		gitDir := filepath.Join(gitDirPath, "hooks")

		hooks, err := getEmbeddedHooks()
		if err != nil {
			t.Fatalf("getEmbeddedHooks() failed: %v", err)
		}

		if err := installHooks(hooks, false, false); err != nil {
			t.Fatalf("installHooks() failed: %v", err)
		}

		for hookName := range hooks {
			hookPath := filepath.Join(gitDir, hookName)
			if _, err := os.Stat(hookPath); os.IsNotExist(err) {
				t.Errorf("Hook %s was not installed", hookName)
			}
			if runtime.GOOS == "windows" {
				continue
			}

			info, err := os.Stat(hookPath)
			if err != nil {
				t.Errorf("Failed to stat %s: %v", hookName, err)
				continue
			}
			if info.Mode()&0111 == 0 {
				t.Errorf("Hook %s is not executable", hookName)
			}
		}
	})
}

func TestInstallHooksBackup(t *testing.T) {
	tmpDir := t.TempDir()
	runInDir(t, tmpDir, func() {
		if err := exec.Command("git", "init").Run(); err != nil {
			t.Skipf("Skipping test: git init failed: %v", err)
		}

		gitDirPath, err := git.GetGitDir()
		if err != nil {
			t.Fatalf("git.GetGitDir() failed: %v", err)
		}
		gitDir := filepath.Join(gitDirPath, "hooks")
		if err := os.MkdirAll(gitDir, 0750); err != nil {
			t.Fatalf("Failed to create hooks directory: %v", err)
		}

		existingHook := filepath.Join(gitDir, "pre-commit")
		existingContent := "#!/bin/sh\necho old hook\n"
		if err := os.WriteFile(existingHook, []byte(existingContent), 0755); err != nil {
			t.Fatalf("Failed to create existing hook: %v", err)
		}

		hooks, err := getEmbeddedHooks()
		if err != nil {
			t.Fatalf("getEmbeddedHooks() failed: %v", err)
		}

		if err := installHooks(hooks, false, false); err != nil {
			t.Fatalf("installHooks() failed: %v", err)
		}

		backupPath := existingHook + ".backup"
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Errorf("Backup was not created")
		}

		backupContent, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("Failed to read backup: %v", err)
		}
		if string(backupContent) != existingContent {
			t.Errorf("Backup content mismatch: got %q, want %q", string(backupContent), existingContent)
		}
	})
}

func TestInstallHooksForce(t *testing.T) {
	tmpDir := t.TempDir()
	runInDir(t, tmpDir, func() {
		if err := exec.Command("git", "init").Run(); err != nil {
			t.Skipf("Skipping test: git init failed: %v", err)
		}

		gitDirPath, err := git.GetGitDir()
		if err != nil {
			t.Fatalf("git.GetGitDir() failed: %v", err)
		}
		gitDir := filepath.Join(gitDirPath, "hooks")
		if err := os.MkdirAll(gitDir, 0750); err != nil {
			t.Fatalf("Failed to create hooks directory: %v", err)
		}

		existingHook := filepath.Join(gitDir, "pre-commit")
		if err := os.WriteFile(existingHook, []byte("old"), 0755); err != nil {
			t.Fatalf("Failed to create existing hook: %v", err)
		}

		hooks, err := getEmbeddedHooks()
		if err != nil {
			t.Fatalf("getEmbeddedHooks() failed: %v", err)
		}

		if err := installHooks(hooks, true, false); err != nil {
			t.Fatalf("installHooks() failed: %v", err)
		}

		backupPath := existingHook + ".backup"
		if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
			t.Errorf("Backup should not have been created with --force")
		}
	})
}

func TestUninstallHooks(t *testing.T) {
	tmpDir := t.TempDir()
	runInDir(t, tmpDir, func() {
		if err := exec.Command("git", "init").Run(); err != nil {
			t.Skipf("Skipping test: git init failed: %v", err)
		}

		gitDirPath, err := git.GetGitDir()
		if err != nil {
			t.Fatalf("git.GetGitDir() failed: %v", err)
		}
		gitDir := filepath.Join(gitDirPath, "hooks")

		hooks, err := getEmbeddedHooks()
		if err != nil {
			t.Fatalf("getEmbeddedHooks() failed: %v", err)
		}
		if err := installHooks(hooks, false, false); err != nil {
			t.Fatalf("installHooks() failed: %v", err)
		}

		if err := uninstallHooks(); err != nil {
			t.Fatalf("uninstallHooks() failed: %v", err)
		}

		for hookName := range hooks {
			hookPath := filepath.Join(gitDir, hookName)
			if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
				t.Errorf("Hook %s was not removed", hookName)
			}
		}
	})
}

func TestHooksCheckGitHooks(t *testing.T) {
	tmpDir := t.TempDir()
	runInDir(t, tmpDir, func() {
		if err := exec.Command("git", "init").Run(); err != nil {
			t.Skipf("Skipping test: git init failed: %v", err)
		}

		statuses := CheckGitHooks()
		for _, status := range statuses {
			if status.Installed {
				t.Errorf("Hook %s should not be installed initially", status.Name)
			}
		}

		hooks, err := getEmbeddedHooks()
		if err != nil {
			t.Fatalf("getEmbeddedHooks() failed: %v", err)
		}
		if err := installHooks(hooks, false, false); err != nil {
			t.Fatalf("installHooks() failed: %v", err)
		}

		statuses = CheckGitHooks()
		for _, status := range statuses {
			if !status.Installed {
				t.Errorf("Hook %s should be installed", status.Name)
			}
			if !status.IsShim {
				t.Errorf("Hook %s should be a thin shim", status.Name)
			}
			if status.Version != "v1" {
				t.Errorf("Hook %s shim version mismatch: got %s, want v1", status.Name, status.Version)
			}
			if status.Outdated {
				t.Errorf("Hook %s should not be outdated", status.Name)
			}
		}
	})
}

func TestInstallHooksShared(t *testing.T) {
	tmpDir := t.TempDir()
	runInDir(t, tmpDir, func() {
		if err := exec.Command("git", "init").Run(); err != nil {
			t.Skipf("Skipping test: git init failed (git may not be available): %v", err)
		}

		hooks, err := getEmbeddedHooks()
		if err != nil {
			t.Fatalf("getEmbeddedHooks() failed: %v", err)
		}

		if err := installHooks(hooks, false, true); err != nil {
			t.Fatalf("installHooks() with shared=true failed: %v", err)
		}

		sharedHooksDir := ".beads-hooks"
		for hookName := range hooks {
			hookPath := filepath.Join(sharedHooksDir, hookName)
			if _, err := os.Stat(hookPath); os.IsNotExist(err) {
				t.Errorf("Hook %s was not installed to .beads-hooks/", hookName)
			}
			if runtime.GOOS == "windows" {
				continue
			}

			info, err := os.Stat(hookPath)
			if err != nil {
				t.Errorf("Failed to stat %s: %v", hookName, err)
				continue
			}
			if info.Mode()&0111 == 0 {
				t.Errorf("Hook %s is not executable", hookName)
			}
		}

		gitDirPath, err := git.GetGitDir()
		if err != nil {
			t.Fatalf("git.GetGitDir() failed: %v", err)
		}
		standardHooksDir := filepath.Join(gitDirPath, "hooks")
		for hookName := range hooks {
			hookPath := filepath.Join(standardHooksDir, hookName)
			if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
				t.Errorf("Hook %s should not be in .git/hooks/ when using --shared", hookName)
			}
		}
	})
}

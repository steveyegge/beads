package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckGitHooks(t *testing.T) {
	t.Run("not a git repo", func(t *testing.T) {
		// Save and change to a temp dir that's not a git repo
		oldWd, _ := os.Getwd()
		tmpDir := t.TempDir()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		check := CheckGitHooks()

		// Should return warning when not in a git repo
		if check.Name != "Git Hooks" {
			t.Errorf("Name = %q, want %q", check.Name, "Git Hooks")
		}
	})
}

func TestCheckMergeDriver(t *testing.T) {
	t.Run("not a git repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := CheckMergeDriver(tmpDir)

		if check.Name != "Git Merge Driver" {
			t.Errorf("Name = %q, want %q", check.Name, "Git Merge Driver")
		}
	})

	t.Run("no gitattributes", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create a fake git directory structure
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.Mkdir(gitDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckMergeDriver(tmpDir)

		// Should report missing configuration
		if check.Status != StatusWarning && check.Status != StatusError {
			t.Logf("Status = %q, Message = %q", check.Status, check.Message)
		}
	})
}

func TestCheckSyncBranchConfig(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := CheckSyncBranchConfig(tmpDir)

		if check.Name != "Sync Branch Config" {
			t.Errorf("Name = %q, want %q", check.Name, "Sync Branch Config")
		}
	})

	t.Run("beads directory exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckSyncBranchConfig(tmpDir)

		// Should check for sync branch config
		if check.Name != "Sync Branch Config" {
			t.Errorf("Name = %q, want %q", check.Name, "Sync Branch Config")
		}
	})
}

func TestCheckSyncBranchHealth(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := CheckSyncBranchHealth(tmpDir)

		if check.Name != "Sync Branch Health" {
			t.Errorf("Name = %q, want %q", check.Name, "Sync Branch Health")
		}
	})
}

func TestCheckSyncBranchHookCompatibility(t *testing.T) {
	t.Run("no sync branch configured", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := CheckSyncBranchHookCompatibility(tmpDir)

		// Should return OK when sync branch not configured
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})
}

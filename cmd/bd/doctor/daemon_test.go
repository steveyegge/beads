package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/git"
)

func TestCheckDaemonStatus(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := CheckDaemonStatus(tmpDir, "1.0.0")

		// Should return OK when no .beads directory (daemon not needed)
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})

	t.Run("beads directory exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckDaemonStatus(tmpDir, "1.0.0")

		// Should check daemon status - may be OK or warning depending on daemon state
		if check.Name != "Daemon Health" {
			t.Errorf("Name = %q, want %q", check.Name, "Daemon Health")
		}
	})
}

func TestCheckGitSyncSetup(t *testing.T) {
	t.Run("not in git repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		oldDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = os.Chdir(oldDir)
			git.ResetCaches()
		}()

		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		git.ResetCaches()

		check := CheckGitSyncSetup(tmpDir)

		if check.Status != StatusWarning {
			t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
		}
		if check.Name != "Git Sync Setup" {
			t.Errorf("Name = %q, want %q", check.Name, "Git Sync Setup")
		}
		if check.Fix == "" {
			t.Error("Expected Fix to contain instructions")
		}
	})

	t.Run("in git repository without sync-branch", func(t *testing.T) {
		tmpDir := t.TempDir()
		oldDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = os.Chdir(oldDir)
			git.ResetCaches()
		}()

		// Initialize git repo
		cmd := exec.Command("git", "init")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		git.ResetCaches()

		check := CheckGitSyncSetup(tmpDir)

		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
		if check.Name != "Git Sync Setup" {
			t.Errorf("Name = %q, want %q", check.Name, "Git Sync Setup")
		}
		// Should mention sync-branch not configured
		if check.Detail == "" {
			t.Error("Expected Detail to contain sync-branch hint")
		}
	})
}

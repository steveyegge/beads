package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckInstallation(t *testing.T) {
	t.Run("missing beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckInstallation(tmpDir)

		if check.Status != StatusError {
			t.Errorf("expected StatusError, got %s", check.Status)
		}
		if check.Name != "Installation" {
			t.Errorf("expected name 'Installation', got %s", check.Name)
		}
	})
}

func TestCheckPermissions(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckPermissions(tmpDir)

		// Should return error when .beads dir doesn't exist (can't write to it)
		if check.Status != StatusError {
			t.Errorf("expected StatusError for missing dir, got %s", check.Status)
		}
	})

	t.Run("writable directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckPermissions(tmpDir)

		if check.Status != StatusOK {
			t.Errorf("expected StatusOK for writable dir, got %s", check.Status)
		}
	})
}

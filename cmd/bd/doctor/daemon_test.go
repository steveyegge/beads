package doctor

import (
	"os"
	"path/filepath"
	"testing"
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

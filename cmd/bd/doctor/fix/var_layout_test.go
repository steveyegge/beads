package fix

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStrayVolatileFiles(t *testing.T) {
	t.Run("not a beads workspace returns error", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-fix-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		_, err = StrayVolatileFiles(tmpDir)
		if err == nil {
			t.Error("expected error for non-beads workspace")
		}
	})

	t.Run("not using var layout does nothing", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-fix-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		// Create file at root (legacy layout - no var/)
		if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}

		moved, err := StrayVolatileFiles(tmpDir)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if moved != 0 {
			t.Errorf("expected 0 files moved, got %d", moved)
		}

		// File should still be at root
		if _, err := os.Stat(filepath.Join(beadsDir, "beads.db")); os.IsNotExist(err) {
			t.Error("beads.db should still exist at root")
		}
	})

	t.Run("moves stray files from root to var", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-fix-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create stray file at root (should be in var/)
		if err := os.WriteFile(filepath.Join(beadsDir, "daemon.pid"), []byte("123"), 0644); err != nil {
			t.Fatalf("failed to create daemon.pid: %v", err)
		}

		moved, err := StrayVolatileFiles(tmpDir)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if moved != 1 {
			t.Errorf("expected 1 file moved, got %d", moved)
		}

		// File should now be in var/
		if _, err := os.Stat(filepath.Join(varDir, "daemon.pid")); os.IsNotExist(err) {
			t.Error("daemon.pid should now be in var/")
		}

		// File should not be at root anymore
		if _, err := os.Stat(filepath.Join(beadsDir, "daemon.pid")); !os.IsNotExist(err) {
			t.Error("daemon.pid should not exist at root anymore")
		}
	})

	t.Run("removes duplicate when file exists in both locations", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-fix-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create file in both locations
		if err := os.WriteFile(filepath.Join(beadsDir, "daemon.pid"), []byte("old"), 0644); err != nil {
			t.Fatalf("failed to create daemon.pid at root: %v", err)
		}
		if err := os.WriteFile(filepath.Join(varDir, "daemon.pid"), []byte("new"), 0644); err != nil {
			t.Fatalf("failed to create daemon.pid in var/: %v", err)
		}

		moved, err := StrayVolatileFiles(tmpDir)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if moved != 1 {
			t.Errorf("expected 1 file 'moved' (duplicate removed), got %d", moved)
		}

		// var/ copy should remain
		data, err := os.ReadFile(filepath.Join(varDir, "daemon.pid"))
		if err != nil {
			t.Fatalf("failed to read daemon.pid from var/: %v", err)
		}
		if string(data) != "new" {
			t.Errorf("var/ copy should have 'new' content, got %q", string(data))
		}

		// Root copy should be gone
		if _, err := os.Stat(filepath.Join(beadsDir, "daemon.pid")); !os.IsNotExist(err) {
			t.Error("daemon.pid should not exist at root anymore")
		}
	})

	t.Run("no stray files does nothing", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-fix-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create file in var/ (correct location)
		if err := os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db in var/: %v", err)
		}

		moved, err := StrayVolatileFiles(tmpDir)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if moved != 0 {
			t.Errorf("expected 0 files moved, got %d", moved)
		}
	})
}

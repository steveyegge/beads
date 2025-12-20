package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckIDFormat(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckIDFormat(tmpDir)

		// Should handle missing .beads gracefully
		if check.Name != "Issue IDs" {
			t.Errorf("Name = %q, want %q", check.Name, "Issue IDs")
		}
	})

	t.Run("no database file", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckIDFormat(tmpDir)

		// Should report "will use hash-based IDs" for new install
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})
}

func TestCheckDependencyCycles(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckDependencyCycles(tmpDir)

		// Should handle missing directory gracefully
		if check.Name != "Dependency Cycles" {
			t.Errorf("Name = %q, want %q", check.Name, "Dependency Cycles")
		}
	})

	t.Run("no database", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckDependencyCycles(tmpDir)

		// Should return OK when no database (nothing to check)
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})
}

func TestCheckTombstones(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckTombstones(tmpDir)

		// Should handle missing directory
		if check.Name != "Tombstones" {
			t.Errorf("Name = %q, want %q", check.Name, "Tombstones")
		}
	})

	t.Run("empty beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckTombstones(tmpDir)

		// Should return OK when no tombstones file
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})
}

func TestCheckDeletionsManifest(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckDeletionsManifest(tmpDir)

		if check.Name != "Deletions Manifest" {
			t.Errorf("Name = %q, want %q", check.Name, "Deletions Manifest")
		}
	})

	t.Run("no deletions file", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckDeletionsManifest(tmpDir)

		// Should return OK when no deletions.jsonl (nothing to migrate)
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})

	t.Run("has deletions file", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		// Create a deletions.jsonl file
		deletionsPath := filepath.Join(beadsDir, "deletions.jsonl")
		if err := os.WriteFile(deletionsPath, []byte(`{"id":"test-1"}`), 0644); err != nil {
			t.Fatal(err)
		}

		check := CheckDeletionsManifest(tmpDir)

		// Should warn about legacy deletions file
		if check.Status != StatusWarning {
			t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
		}
	})
}

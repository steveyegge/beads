package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDatabaseVersion(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := CheckDatabaseVersion(tmpDir, "1.0.0")

		if check.Name != "Database" {
			t.Errorf("Name = %q, want %q", check.Name, "Database")
		}
		// Should report no database found
		if check.Status != StatusError {
			t.Errorf("Status = %q, want %q", check.Status, StatusError)
		}
	})

	t.Run("jsonl only mode", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		// Create issues.jsonl file
		if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		// Create config.yaml with no-db mode
		configContent := `database: ""`
		if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(configContent), 0644); err != nil {
			t.Fatal(err)
		}

		check := CheckDatabaseVersion(tmpDir, "1.0.0")

		// Fresh clone detection should warn about needing to import
		if check.Status == StatusError {
			t.Logf("Got error status with message: %s", check.Message)
		}
	})
}

func TestCheckSchemaCompatibility(t *testing.T) {
	t.Run("no database", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckSchemaCompatibility(tmpDir)

		// Should return OK when no database
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q for no database", check.Status, StatusOK)
		}
	})
}

func TestCheckDatabaseIntegrity(t *testing.T) {
	t.Run("no database", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckDatabaseIntegrity(tmpDir)

		// Should return OK when no database
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q for no database", check.Status, StatusOK)
		}
	})
}

func TestCheckDatabaseJSONLSync(t *testing.T) {
	t.Run("no beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := CheckDatabaseJSONLSync(tmpDir)

		// Should return OK when no .beads directory
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})

	t.Run("empty beads directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		check := CheckDatabaseJSONLSync(tmpDir)

		// Should return OK when nothing to sync
		if check.Status != StatusOK {
			t.Errorf("Status = %q, want %q", check.Status, StatusOK)
		}
	})
}

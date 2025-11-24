package fix

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

// TestDatabaseConfigFix_JSONLMismatch tests that DatabaseConfig fixes JSONL mismatches.
// bd-afd: Verify auto-fix for metadata.json jsonl_export mismatch
func TestDatabaseConfigFix_JSONLMismatch(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create beads.jsonl file (actual JSONL)
	jsonlPath := filepath.Join(beadsDir, "beads.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-123"}`), 0644); err != nil {
		t.Fatalf("Failed to create beads.jsonl: %v", err)
	}

	// Create metadata.json with wrong JSONL filename (issues.jsonl)
	cfg := &configfile.Config{
		Database:    "beads.db",
		JSONLExport: "issues.jsonl", // Wrong - should be beads.jsonl
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Run the fix
	if err := DatabaseConfig(tmpDir); err != nil {
		t.Fatalf("DatabaseConfig failed: %v", err)
	}

	// Verify the config was updated
	updatedCfg, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("Failed to load updated config: %v", err)
	}

	if updatedCfg.JSONLExport != "beads.jsonl" {
		t.Errorf("Expected JSONLExport to be 'beads.jsonl', got %q", updatedCfg.JSONLExport)
	}
}

// TestDatabaseConfigFix_PrefersBeadsJSONL tests that DatabaseConfig prefers beads.jsonl over issues.jsonl.
func TestDatabaseConfigFix_PrefersBeadsJSONL(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create both beads.jsonl and issues.jsonl
	beadsJSONL := filepath.Join(beadsDir, "beads.jsonl")
	if err := os.WriteFile(beadsJSONL, []byte(`{"id":"test-123"}`), 0644); err != nil {
		t.Fatalf("Failed to create beads.jsonl: %v", err)
	}

	issuesJSONL := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesJSONL, []byte(`{"id":"test-456"}`), 0644); err != nil {
		t.Fatalf("Failed to create issues.jsonl: %v", err)
	}

	// Create metadata.json with wrong JSONL filename (old.jsonl)
	cfg := &configfile.Config{
		Database:    "beads.db",
		JSONLExport: "old.jsonl", // Wrong - should prefer beads.jsonl
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Run the fix
	if err := DatabaseConfig(tmpDir); err != nil {
		t.Fatalf("DatabaseConfig failed: %v", err)
	}

	// Verify the config was updated to beads.jsonl (not issues.jsonl)
	updatedCfg, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("Failed to load updated config: %v", err)
	}

	if updatedCfg.JSONLExport != "beads.jsonl" {
		t.Errorf("Expected JSONLExport to be 'beads.jsonl', got %q", updatedCfg.JSONLExport)
	}
}

// TestFindActualJSONLFile_SkipsBackups tests that backup files are skipped.
func TestFindActualJSONLFile_SkipsBackups(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create beads.jsonl and various backup files
	files := []string{
		"beads.jsonl",
		"beads.jsonl.backup",
		"backup_beads.jsonl",
		"beads.jsonl.orig",
		"beads.jsonl.bak",
		"beads.jsonl~",
	}

	for _, name := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(`{"id":"test"}`), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	// findActualJSONLFile should return beads.jsonl (not backups)
	result := findActualJSONLFile(tmpDir)
	if result != "beads.jsonl" {
		t.Errorf("Expected 'beads.jsonl', got %q", result)
	}
}

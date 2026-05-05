//go:build cgo

package doltdriver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

// TestMigrateHyphenatedDB_PersistsToMetadata verifies that migrateHyphenatedDB
// updates metadata.json with the sanitized database name.
func TestMigrateHyphenatedDB_PersistsToMetadata(t *testing.T) {
	beadsDir := t.TempDir()

	cfg := &configfile.Config{
		Database:     "dolt",
		DoltDatabase: "my-project",
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if err := migrateHyphenatedDB(beadsDir, cfg, "my-project", "my_project"); err != nil {
		t.Fatalf("migrateHyphenatedDB failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("failed to read metadata.json: %v", err)
	}

	var saved configfile.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("failed to parse metadata.json: %v", err)
	}
	if saved.DoltDatabase != "my_project" {
		t.Errorf("expected dolt_database %q in metadata.json, got %q", "my_project", saved.DoltDatabase)
	}
}

// TestMigrateHyphenatedDB_RenamesDirectory verifies that migrateHyphenatedDB
// renames the old hyphenated database directory to the sanitized name.
func TestMigrateHyphenatedDB_RenamesDirectory(t *testing.T) {
	beadsDir := t.TempDir()

	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	oldDir := filepath.Join(dataDir, "my-project")
	newDir := filepath.Join(dataDir, "my_project")

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("failed to create old dir: %v", err)
	}
	sentinel := filepath.Join(oldDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to write sentinel: %v", err)
	}

	cfg := &configfile.Config{DoltDatabase: "my-project"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if err := migrateHyphenatedDB(beadsDir, cfg, "my-project", "my_project"); err != nil {
		t.Fatalf("migrateHyphenatedDB failed: %v", err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("old directory should no longer exist after rename")
	}
	if _, err := os.Stat(filepath.Join(newDir, "sentinel.txt")); err != nil {
		t.Error("sentinel file should exist in renamed directory")
	}
}

// TestMigrateHyphenatedDB_CollisionError verifies that migrateHyphenatedDB
// returns an error when both old and new directories exist (GH#3231).
func TestMigrateHyphenatedDB_CollisionError(t *testing.T) {
	beadsDir := t.TempDir()

	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	oldDir := filepath.Join(dataDir, "my-project")
	newDir := filepath.Join(dataDir, "my_project")

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("failed to create old dir: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("failed to create new dir: %v", err)
	}

	cfg := &configfile.Config{DoltDatabase: "my-project"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	err := migrateHyphenatedDB(beadsDir, cfg, "my-project", "my_project")
	if err == nil {
		t.Fatal("expected error when both directories exist, got nil")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("expected collision error message, got: %v", err)
	}
}

// TestMigrateHyphenatedDB_NoOldDir verifies that migrateHyphenatedDB still
// updates metadata.json even when the old directory doesn't exist (e.g., fresh
// project where only metadata.json has the bad name).
func TestMigrateHyphenatedDB_NoOldDir(t *testing.T) {
	beadsDir := t.TempDir()

	cfg := &configfile.Config{DoltDatabase: "my-project"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if err := migrateHyphenatedDB(beadsDir, cfg, "my-project", "my_project"); err != nil {
		t.Fatalf("migrateHyphenatedDB failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("failed to read metadata.json: %v", err)
	}
	var saved configfile.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("failed to parse metadata.json: %v", err)
	}
	if saved.DoltDatabase != "my_project" {
		t.Errorf("expected %q, got %q", "my_project", saved.DoltDatabase)
	}
}

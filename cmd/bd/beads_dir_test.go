package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/utils"
)

func TestGetBeadsDirPrefersBeadsDiscoveryOverCustomDoltDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeServer
	cfg.DoltDatabase = "hq"
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	customDataDir := filepath.Join(tmpDir, "shared-dolt-data")
	if err := os.MkdirAll(customDataDir, 0o755); err != nil {
		t.Fatalf("failed to create custom dolt data dir: %v", err)
	}
	t.Setenv("BEADS_DOLT_DATA_DIR", customDataDir)

	oldDBPath := dbPath
	oldCwd, _ := os.Getwd()
	dbPath = customDataDir
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() {
		dbPath = oldDBPath
		_ = os.Chdir(oldCwd)
	}()

	if got := getBeadsDir(); got != utils.CanonicalizePath(beadsDir) {
		t.Fatalf("getBeadsDir() = %q, want %q", got, utils.CanonicalizePath(beadsDir))
	}
}

func TestGetBeadsDirFallsBackToDBPathParent(t *testing.T) {
	oldDBPath := dbPath
	dbPath = "/tmp/example/.beads/dolt"
	defer func() { dbPath = oldDBPath }()

	// Ensure discovery path is unavailable.
	t.Setenv("BEADS_DIR", "")
	oldCwd, _ := os.Getwd()
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	if got := getBeadsDir(); got != "/tmp/example/.beads" {
		t.Fatalf("getBeadsDir() fallback = %q, want %q", got, "/tmp/example/.beads")
	}
}

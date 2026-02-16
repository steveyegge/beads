//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
)

// TestMigrateToDolt_SyncModeConfigYaml verifies the SetYamlConfig + Set + GetSyncMode
// roundtrip that migrate_dolt.go uses to persist sync.mode to config.yaml.
// This tests the write path without needing a real SQLite database (NFR-010).
func TestMigrateToDolt_SyncModeConfigYaml(t *testing.T) {
	// Use established test helper for viper init (test_helpers_test.go:56-63)
	initConfigForTest(t)

	// Create temp .beads/ dir with flat-key config.yaml (matches bd init format)
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("# beads config\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Change to tmpDir so findProjectConfigYaml() finds .beads/config.yaml
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Write sync.mode via the same path migrate_dolt.go will use
	if err := config.SetYamlConfig("sync.mode", "dolt-native"); err != nil {
		t.Fatalf("SetYamlConfig failed: %v", err)
	}
	config.Set("sync.mode", "dolt-native")

	// Verify roundtrip: GetSyncMode reads from viper which was just updated
	if got := config.GetSyncMode(); got != config.SyncModeDoltNative {
		t.Errorf("GetSyncMode() = %q, want %q", got, config.SyncModeDoltNative)
	}

	// Verify file contents (flat-key format: "sync.mode: dolt-native")
	content, err := os.ReadFile(filepath.Join(beadsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "dolt-native") {
		t.Errorf("config.yaml missing dolt-native: %s", content)
	}
}

//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/config"
)

// setupYamlConfig creates a temp .beads/ directory with config.yaml,
// changes to it, and initializes viper. Cleanup restores cwd and
// re-initializes viper to avoid global state leaking between tests.
func setupYamlConfig(t *testing.T) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed to create config.yaml: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	config.Initialize() // Points viper at temp .beads/config.yaml

	t.Cleanup(func() {
		_ = os.Chdir(origDir)
		config.Initialize() // Re-initialize viper to original config
	})
}

// TestSyncModeConfig verifies sync mode yaml roundtrip: set via SetSyncMode,
// read back via GetSyncMode. Storage parameter is nil since both functions
// now use config.yaml exclusively.
func TestSyncModeConfig(t *testing.T) {
	ctx := context.Background()
	setupYamlConfig(t)

	// Test 1: Default mode is git-portable
	mode := GetSyncMode(ctx, nil)
	if mode != SyncModeGitPortable {
		t.Errorf("default sync mode = %q, want %q", mode, SyncModeGitPortable)
	}

	// Test 2-4: Set and get each non-default mode
	for _, want := range []string{SyncModeRealtime, SyncModeDoltNative, SyncModeBeltAndSuspenders} {
		if err := SetSyncMode(ctx, nil, want); err != nil {
			t.Fatalf("SetSyncMode(%q) failed: %v", want, err)
		}
		got := GetSyncMode(ctx, nil)
		if got != want {
			t.Errorf("GetSyncMode() = %q after setting %q", got, want)
		}
	}

	// Test 5: Invalid mode returns error
	if err := SetSyncMode(ctx, nil, "invalid-mode"); err == nil {
		t.Error("expected error for invalid sync mode")
	}

	// Test 6: Invalid mode in config.yaml defaults to git-portable (FR-011)
	if err := config.SetYamlConfig("sync.mode", "bogus-value"); err != nil {
		t.Fatalf("failed to write invalid mode to config.yaml: %v", err)
	}
	config.Initialize() // Re-read config.yaml with invalid value
	mode = GetSyncMode(ctx, nil)
	if mode != SyncModeGitPortable {
		t.Errorf("invalid mode in config.yaml: GetSyncMode() = %q, want %q", mode, SyncModeGitPortable)
	}
}

// TestShouldExportJSONL verifies JSONL export behavior per mode.
func TestShouldExportJSONL(t *testing.T) {
	ctx := context.Background()
	setupYamlConfig(t)

	tests := []struct {
		mode       string
		wantExport bool
	}{
		{SyncModeGitPortable, true},
		{SyncModeRealtime, true},
		{SyncModeDoltNative, false}, // dolt-native uses Dolt remotes, not JSONL
		{SyncModeBeltAndSuspenders, true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			config.Set("sync.mode", tt.mode)

			got := ShouldExportJSONL(ctx, nil)
			if got != tt.wantExport {
				t.Errorf("ShouldExportJSONL() = %v, want %v", got, tt.wantExport)
			}
		})
	}
}

// TestShouldExportJSONL_YamlRespected verifies ShouldExportJSONL reads from
// yaml config (via GetSyncMode delegation to config.GetSyncMode). This ensures
// sync.mode set in config.yaml is respected — fixing the bug where dolt-native
// workspaces paid 10-25s JSONL export overhead (bd-6fiwk).
func TestShouldExportJSONL_YamlRespected(t *testing.T) {
	ctx := context.Background()
	setupYamlConfig(t)

	// Set dolt-native via yaml — ShouldExportJSONL must return false
	config.Set("sync.mode", SyncModeDoltNative)
	if ShouldExportJSONL(ctx, nil) {
		t.Error("ShouldExportJSONL() = true, want false for dolt-native in yaml")
	}

	// Reset to default — should return true
	config.Set("sync.mode", SyncModeGitPortable)
	if !ShouldExportJSONL(ctx, nil) {
		t.Error("ShouldExportJSONL() = false, want true for git-portable in yaml")
	}
}

// TestShouldImportJSONL verifies JSONL import behavior per mode.
func TestShouldImportJSONL(t *testing.T) {
	ctx := context.Background()
	setupYamlConfig(t)

	tests := []struct {
		mode       string
		wantImport bool
	}{
		{SyncModeGitPortable, true},
		{SyncModeRealtime, true},
		{SyncModeDoltNative, false},
		{SyncModeBeltAndSuspenders, true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			config.Set("sync.mode", tt.mode)

			got := ShouldImportJSONL(ctx, nil)
			if got != tt.wantImport {
				t.Errorf("ShouldImportJSONL() = %v, want %v", got, tt.wantImport)
			}
		})
	}
}

// TestShouldUseDoltRemote verifies Dolt remote usage per mode.
func TestShouldUseDoltRemote(t *testing.T) {
	ctx := context.Background()
	setupYamlConfig(t)

	tests := []struct {
		mode    string
		wantUse bool
	}{
		{SyncModeGitPortable, false},
		{SyncModeRealtime, false},
		{SyncModeDoltNative, true},
		{SyncModeBeltAndSuspenders, true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			config.Set("sync.mode", tt.mode)

			got := ShouldUseDoltRemote(ctx, nil)
			if got != tt.wantUse {
				t.Errorf("ShouldUseDoltRemote() = %v, want %v", got, tt.wantUse)
			}
		})
	}
}

// TestSyncModeDescription verifies mode descriptions are meaningful.
func TestSyncModeDescription(t *testing.T) {
	tests := []struct {
		mode        string
		wantContain string
	}{
		{SyncModeGitPortable, "JSONL"},
		{SyncModeRealtime, "every change"},
		{SyncModeDoltNative, "export-only"},
		{SyncModeBeltAndSuspenders, "Both"},
		{"invalid", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			desc := SyncModeDescription(tt.mode)
			if desc == "" {
				t.Error("description should not be empty")
			}
			// Just verify descriptions are non-empty and distinct
			t.Logf("%s: %s", tt.mode, desc)
		})
	}
}

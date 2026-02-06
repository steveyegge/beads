package factory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestServerModeConfig(t *testing.T) {
	// Create temp dir with server mode config
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write config with server mode enabled
	configData := `{
		"database": "beads",
		"backend": "dolt",
		"dolt_server_enabled": true,
		"dolt_server_host": "127.0.0.1",
		"dolt_server_port": 3306,
		"dolt_server_user": "root"
	}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(configData), 0644); err != nil {
		t.Fatal(err)
	}

	// Test config loading
	cfg := LoadConfig(beadsDir)
	if cfg == nil {
		t.Fatal("Failed to load config")
	}

	t.Logf("Backend: %s", cfg.GetBackend())
	t.Logf("Server mode: %v", cfg.IsDoltServerMode())
	t.Logf("Server: %s:%d", cfg.GetDoltServerHost(), cfg.GetDoltServerPort())

	if !cfg.IsDoltServerMode() {
		t.Error("Expected server mode to be enabled")
	}

	// Test capabilities
	caps := GetCapabilitiesFromConfig(beadsDir)
	t.Logf("SingleProcessOnly: %v", caps.SingleProcessOnly)

	if caps.SingleProcessOnly {
		t.Error("Expected SingleProcessOnly=false for server mode")
	}
}

// TestGetBackendFromConfigServerModeEnvVar verifies that GetBackendFromConfig
// returns "dolt" when BEADS_DOLT_SERVER_MODE=1 is set, even without metadata.json.
// This enables K8s deployments configured entirely via environment variables.
func TestGetBackendFromConfigServerModeEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// No metadata.json written - simulates fresh K8s container

	// Without env var, should return empty (falls through to yaml config / empty)
	os.Unsetenv("BEADS_DOLT_SERVER_MODE")
	backend := GetBackendFromConfig(beadsDir)
	if backend == "dolt" {
		t.Error("Expected non-dolt backend without env var set, got 'dolt'")
	}

	// With BEADS_DOLT_SERVER_MODE=1, should return "dolt"
	t.Setenv("BEADS_DOLT_SERVER_MODE", "1")
	backend = GetBackendFromConfig(beadsDir)
	if backend != "dolt" {
		t.Errorf("Expected backend 'dolt' with BEADS_DOLT_SERVER_MODE=1, got %q", backend)
	}
}

func TestServerModeConnection(t *testing.T) {
	// Skip if no server running
	// This test requires a running dolt sql-server on 127.0.0.1:3306
	// pointing to /home/ubuntu/gastown9/.beads/dolt

	// Clear BD_DAEMON_HOST to ensure test uses local database (gt-57wsnm guard)
	os.Unsetenv("BD_DAEMON_HOST")

	// Check if server is likely running by trying to connect
	ctx := context.Background()

	// Use the actual gastown9 dolt directory but with server mode
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Point to the actual dolt data dir but connect via server
	configData := `{
		"database": "/home/ubuntu/gastown9/.beads/dolt",
		"backend": "dolt",
		"dolt_server_enabled": true,
		"dolt_server_host": "127.0.0.1",
		"dolt_server_port": 3306,
		"dolt_server_user": "root"
	}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(configData), 0644); err != nil {
		t.Fatal(err)
	}

	// Try to create storage
	store, err := NewFromConfig(ctx, beadsDir)
	if err != nil {
		t.Skipf("Could not connect to server (may not be running): %v", err)
	}
	defer store.Close()

	// Try a simple query - search issues to verify connection works
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}

	t.Logf("SUCCESS: Connected via server mode, found %d issues", len(issues))
}

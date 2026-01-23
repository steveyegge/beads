package factory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

func TestServerModeConnection(t *testing.T) {
	// Skip if no server running
	// This test requires a running dolt sql-server on 127.0.0.1:3306
	// pointing to /home/ubuntu/gastown9/.beads/dolt

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

	// Try a simple query - get a known issue
	issue, err := store.GetIssue(ctx, "hq-f37cb5")
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	t.Logf("SUCCESS: Connected via server mode, got issue: %s - %s", issue.ID, issue.Title)
}

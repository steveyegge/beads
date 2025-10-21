package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	sqlitestorage "github.com/steveyegge/beads/internal/storage/sqlite"
)

func TestVersionCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		serverVersion string
		clientVersion string
		shouldWork    bool
		errorContains string
	}{
		{
			name:          "Exact version match",
			serverVersion: "1.0.0",
			clientVersion: "1.0.0",
			shouldWork:    true,
		},
		{
			name:          "Client older, same major version (backward compatible)",
			serverVersion: "1.2.0",
			clientVersion: "1.1.0",
			shouldWork:    true,
		},
		{
			name:          "Client newer, same major version (not supported)",
			serverVersion: "1.1.0",
			clientVersion: "1.2.0",
			shouldWork:    false,
			errorContains: "daemon 1.1.0 is older than client 1.2.0",
		},
		{
			name:          "Different major versions - client newer",
			serverVersion: "1.0.0",
			clientVersion: "2.0.0",
			shouldWork:    false,
			errorContains: "incompatible major versions",
		},
		{
			name:          "Different major versions - daemon newer",
			serverVersion: "2.0.0",
			clientVersion: "1.0.0",
			shouldWork:    false,
			errorContains: "incompatible major versions",
		},
		{
			name:          "Empty client version (legacy client)",
			serverVersion: "1.0.0",
			clientVersion: "",
			shouldWork:    true,
		},
		{
			name:          "Invalid semver formats (dev builds)",
			serverVersion: "dev-build",
			clientVersion: "local-test",
			shouldWork:    true, // Allow dev builds
		},
		{
			name:          "Version without v prefix",
			serverVersion: "1.0.0",
			clientVersion: "1.0.0",
			shouldWork:    true,
		},
		{
			name:          "Patch version differences (compatible)",
			serverVersion: "1.0.5",
			clientVersion: "1.0.3",
			shouldWork:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temp environment
			tmpDir, err := os.MkdirTemp("", "bd-version-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			dbPath := filepath.Join(tmpDir, "test.db")
			socketPath := filepath.Join(tmpDir, "bd.sock")

			store, err := sqlitestorage.New(dbPath)
			if err != nil {
				t.Fatalf("Failed to create store: %v", err)
			}
			defer store.Close()

			// Override server version
			originalServerVersion := ServerVersion
			ServerVersion = tt.serverVersion
			defer func() { ServerVersion = originalServerVersion }()

			server := NewServer(socketPath, store)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				server.Start(ctx)
			}()

			// Wait for server to be ready
			time.Sleep(100 * time.Millisecond)

			// Override client version for this test
			originalClientVersion := ClientVersion
			ClientVersion = tt.clientVersion
			defer func() { ClientVersion = originalClientVersion }()

			client, err := TryConnect(socketPath)
			if err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}
			if client == nil {
				t.Fatal("Client is nil after successful connection")
			}
			defer client.Close()

			// Try to create an issue (this triggers version check)
			args := &CreateArgs{
				Title:     "Version test issue",
				IssueType: "task",
				Priority:  2,
			}

			resp, err := client.Create(args)

			if tt.shouldWork {
				if err != nil {
					t.Errorf("Expected operation to succeed, but got error: %v", err)
				}
				if resp != nil && !resp.Success {
					t.Errorf("Expected success, but got error: %s", resp.Error)
				}
			} else {
				// Should fail
				if err == nil && (resp == nil || resp.Success) {
					t.Errorf("Expected operation to fail due to version mismatch, but it succeeded")
				}
				if err != nil && tt.errorContains != "" {
					if !contains(err.Error(), tt.errorContains) {
						t.Errorf("Expected error to contain '%s', got: %s", tt.errorContains, err.Error())
					}
				}
				if resp != nil && !resp.Success && tt.errorContains != "" {
					if !contains(resp.Error, tt.errorContains) {
						t.Errorf("Expected error to contain '%s', got: %s", tt.errorContains, resp.Error)
					}
				}
			}

			server.Stop()
		})
	}
}

func TestHealthCheckIncludesVersionInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bd-health-version-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	socketPath := filepath.Join(tmpDir, "bd.sock")

	store, err := sqlitestorage.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set explicit versions
	ServerVersion = "1.0.0"
	ClientVersion = "1.0.0"

	server := NewServer(socketPath, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	if health.Version != ServerVersion {
		t.Errorf("Expected server version %s, got %s", ServerVersion, health.Version)
	}

	if health.ClientVersion != ClientVersion {
		t.Errorf("Expected client version %s, got %s", ClientVersion, health.ClientVersion)
	}

	if !health.Compatible {
		t.Error("Expected versions to be compatible")
	}

	server.Stop()
}

func TestIncompatibleVersionInHealth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bd-health-incompatible-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	socketPath := filepath.Join(tmpDir, "bd.sock")

	store, err := sqlitestorage.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set incompatible versions
	ServerVersion = "1.0.0"
	ClientVersion = "2.0.0"

	server := NewServer(socketPath, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Health check should succeed but report incompatible
	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	if health.Compatible {
		t.Error("Expected versions to be incompatible")
	}

	server.Stop()
}

func TestVersionCheckMessage(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name          string
		serverVersion string
		clientVersion string
		expectError   bool
		errorContains string
	}{
		{
			name:          "Major mismatch - daemon older",
			serverVersion: "1.0.0",
			clientVersion: "2.0.0",
			expectError:   true,
			errorContains: "Daemon is older; upgrade and restart daemon",
		},
		{
			name:          "Major mismatch - client older",
			serverVersion: "2.0.0",
			clientVersion: "1.0.0",
			expectError:   true,
			errorContains: "Client is older; upgrade the bd CLI",
		},
		{
			name:          "Minor mismatch - daemon older",
			serverVersion: "1.0.0",
			clientVersion: "1.1.0",
			expectError:   true,
			errorContains: "daemon 1.0.0 is older than client 1.1.0",
		},
		{
			name:          "Compatible versions",
			serverVersion: "1.1.0",
			clientVersion: "1.0.0",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Override versions
			origServer := ServerVersion
			ServerVersion = tt.serverVersion
			defer func() { ServerVersion = origServer }()

			err := server.checkVersionCompatibility(tt.clientVersion)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestPingAndHealthBypassVersionCheck(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bd-bypass-version-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	socketPath := filepath.Join(tmpDir, "bd.sock")

	store, err := sqlitestorage.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set incompatible versions
	ServerVersion = "1.0.0"
	ClientVersion = "2.0.0"

	server := NewServer(socketPath, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Ping should work despite version mismatch
	if err := client.Ping(); err != nil {
		t.Errorf("Ping should work despite version mismatch, got: %v", err)
	}

	// Health should work despite version mismatch
	health, err := client.Health()
	if err != nil {
		t.Errorf("Health should work despite version mismatch, got: %v", err)
	}

	// Health should report incompatible
	if health.Compatible {
		t.Error("Health should report versions as incompatible")
	}

	// But Create should fail
	args := &CreateArgs{
		Title:     "Test",
		IssueType: "task",
		Priority:  2,
	}

	resp, err := client.Create(args)
	if err == nil && (resp == nil || resp.Success) {
		t.Error("Create should fail due to version mismatch")
	}

	server.Stop()
}

func TestMetricsOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bd-metrics-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	socketPath := filepath.Join(tmpDir, "bd.sock")

	store, err := sqlitestorage.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ServerVersion = "1.0.0"
	ClientVersion = "1.0.0"

	server := NewServer(socketPath, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	metrics, err := client.Metrics()
	if err != nil {
		t.Fatalf("Metrics call failed: %v", err)
	}

	if metrics == nil {
		t.Fatal("Metrics response is nil")
	}

	// Verify we have some basic metrics structure
	var metricsMap map[string]interface{}
	data, _ := json.Marshal(metrics)
	json.Unmarshal(data, &metricsMap)

	if len(metricsMap) == 0 {
		t.Error("Expected non-empty metrics map")
	}

	server.Stop()
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

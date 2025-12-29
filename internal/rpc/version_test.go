//go:build integration
// +build integration

package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sqlitestorage "github.com/steveyegge/beads/internal/storage/sqlite"
)

const testVersion100 = "1.0.0"

func TestVersionCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow RPC test in short mode")
	}
	tests := []struct {
		name          string
		serverVersion string
		clientVersion string
		shouldWork    bool
		errorContains string
	}{
		{
			name:          "Exact version match",
			serverVersion: testVersion100,
			clientVersion: testVersion100,
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
			errorContains: "daemon upgrade",
		},
		{
			name:          "Different major versions - client newer",
			serverVersion: testVersion100,
			clientVersion: "2.0.0",
			shouldWork:    false,
			errorContains: "incompatible major versions",
		},
		{
			name:          "Different major versions - daemon newer",
			serverVersion: "2.0.0",
			clientVersion: testVersion100,
			shouldWork:    false,
			errorContains: "incompatible major versions",
		},
		{
			name:          "Empty client version (legacy client)",
			serverVersion: testVersion100,
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
			serverVersion: testVersion100,
			clientVersion: testVersion100,
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
			// Setup isolated test environment
			tmpDir, _, dbPath, socketPath, cleanup := setupTestServerIsolated(t)
			defer cleanup()

			store := newTestStore(t, dbPath)
			defer store.Close()

			// Override server version
			originalServerVersion := ServerVersion
			ServerVersion = tt.serverVersion
			defer func() { ServerVersion = originalServerVersion }()

			server := NewServer(socketPath, store, tmpDir, dbPath)

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

			// Change to tmpDir so client's os.Getwd() finds the test database
			t.Chdir(tmpDir)

			client, err := TryConnect(socketPath)
			if err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}
			if client == nil {
				t.Fatal("Client is nil after successful connection")
			}
			defer client.Close()

			// Set dbPath so client validates it's connected to the right daemon
			client.dbPath = dbPath

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
	tmpDir, _, dbPath, socketPath, cleanup := setupTestServerIsolated(t)
	defer cleanup()

	store, err := sqlitestorage.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set explicit versions
	ServerVersion = testVersion100
	ClientVersion = testVersion100

	server := NewServer(socketPath, store, tmpDir, dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Change to tmpDir so client's os.Getwd() finds the test database
	t.Chdir(tmpDir)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Set dbPath so client validates it's connected to the right daemon
	client.dbPath = dbPath

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
	tmpDir, _, dbPath, socketPath, cleanup := setupTestServerIsolated(t)
	defer cleanup()

	store, err := sqlitestorage.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set incompatible versions
	ServerVersion = testVersion100
	ClientVersion = "2.0.0"

	server := NewServer(socketPath, store, tmpDir, dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Change to tmpDir so client's os.Getwd() finds the test database
	t.Chdir(tmpDir)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Set dbPath so client validates it's connected to the right daemon
	client.dbPath = dbPath

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
			serverVersion: testVersion100,
			clientVersion: "2.0.0",
			expectError:   true,
			errorContains: "Daemon is older; upgrade and restart daemon",
		},
		{
			name:          "Major mismatch - client older",
			serverVersion: "2.0.0",
			clientVersion: testVersion100,
			expectError:   true,
			errorContains: "Client is older; upgrade the bd CLI",
		},
		{
			name:          "Minor mismatch - daemon older",
			serverVersion: testVersion100,
			clientVersion: "1.1.0",
			expectError:   true,
			errorContains: "client v1.1.0 requires daemon upgrade",
		},
		{
			name:          "Compatible versions",
			serverVersion: "1.1.0",
			clientVersion: testVersion100,
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
	tmpDir, _, dbPath, socketPath, cleanup := setupTestServerIsolated(t)
	defer cleanup()

	store, err := sqlitestorage.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set incompatible versions
	ServerVersion = testVersion100
	ClientVersion = "2.0.0"

	server := NewServer(socketPath, store, tmpDir, dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Change to tmpDir so client's os.Getwd() finds the test database
	t.Chdir(tmpDir)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Set dbPath so client validates it's connected to the right daemon
	client.dbPath = dbPath

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
	tmpDir, _, dbPath, socketPath, cleanup := setupTestServerIsolated(t)
	defer cleanup()

	store, err := sqlitestorage.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ServerVersion = testVersion100
	ClientVersion = testVersion100

	server := NewServer(socketPath, store, tmpDir, dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Change to tmpDir so client's os.Getwd() finds the test database
	t.Chdir(tmpDir)

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Set dbPath so client validates it's connected to the right daemon
	client.dbPath = dbPath

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

// TestExtractSemver tests the semver extraction helper function (GH#797)
func TestExtractSemver(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain semver",
			input:    "0.40.0",
			expected: "0.40.0",
		},
		{
			name:     "semver with dev suffix",
			input:    "0.40.0 (dev: main@abc123)",
			expected: "0.40.0",
		},
		{
			name:     "semver with release suffix",
			input:    "0.40.0 (release)",
			expected: "0.40.0",
		},
		{
			name:     "semver with parenthesis no space",
			input:    "1.2.3(test)",
			expected: "1.2.3",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "v-prefix with suffix",
			input:    "v1.0.0 (dev)",
			expected: "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSemver(tt.input)
			if result != tt.expected {
				t.Errorf("extractSemver(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestDevBuildVersionMismatch tests detection of dev builds with same semver but different commits (GH#797)
func TestDevBuildVersionMismatch(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name          string
		serverVersion string
		clientVersion string
		expectError   bool
		errorContains string
	}{
		{
			name:          "exact match with dev suffix",
			serverVersion: "0.40.0 (dev: main@abc123)",
			clientVersion: "0.40.0 (dev: main@abc123)",
			expectError:   false,
		},
		{
			name:          "same semver different commits",
			serverVersion: "0.40.0 (dev: main@abc123)",
			clientVersion: "0.40.0 (dev: main@def456)",
			expectError:   true,
			errorContains: "same semver",
		},
		{
			name:          "same semver different branches",
			serverVersion: "0.40.0 (dev: main@abc123)",
			clientVersion: "0.40.0 (dev: feature@abc123)",
			expectError:   true,
			errorContains: "different builds",
		},
		{
			name:          "plain semver vs dev build",
			serverVersion: "0.40.0",
			clientVersion: "0.40.0 (dev: main@abc123)",
			expectError:   true,
			errorContains: "same semver",
		},
		{
			name:          "dev vs release suffix",
			serverVersion: "0.40.0 (release)",
			clientVersion: "0.40.0 (dev: main@abc123)",
			expectError:   true,
			errorContains: "different builds",
		},
		{
			name:          "plain semver exact match",
			serverVersion: "0.40.0",
			clientVersion: "0.40.0",
			expectError:   false,
		},
		{
			name:          "different semver with dev suffix (client newer)",
			serverVersion: "0.39.0 (dev: main@abc123)",
			clientVersion: "0.40.0 (dev: main@def456)",
			expectError:   true,
			errorContains: "daemon upgrade",
		},
		{
			name:          "different semver with dev suffix (daemon newer)",
			serverVersion: "0.40.0 (dev: main@abc123)",
			clientVersion: "0.39.0 (dev: main@def456)",
			expectError:   false, // Client older is allowed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Override server version
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

// TestFullVersionStringCompatibility tests that full version strings work correctly for release builds (GH#797)
func TestFullVersionStringCompatibility(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name          string
		serverVersion string
		clientVersion string
		expectError   bool
		description   string
	}{
		{
			name:          "goreleaser release builds",
			serverVersion: "0.40.0 (release)",
			clientVersion: "0.40.0 (release)",
			expectError:   false,
			description:   "Release builds with same semver should be compatible",
		},
		{
			name:          "goreleaser vs plain semver",
			serverVersion: "0.40.0 (release)",
			clientVersion: "0.40.0",
			expectError:   true,
			description:   "Release suffix vs no suffix = different builds",
		},
		{
			name:          "semver comparison with full version (compatible)",
			serverVersion: "0.41.0 (release)",
			clientVersion: "0.40.0 (release)",
			expectError:   false,
			description:   "Newer daemon, older client = allowed",
		},
		{
			name:          "semver comparison with full version (incompatible)",
			serverVersion: "0.40.0 (release)",
			clientVersion: "0.41.0 (release)",
			expectError:   true,
			description:   "Older daemon, newer client = error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origServer := ServerVersion
			ServerVersion = tt.serverVersion
			defer func() { ServerVersion = origServer }()

			err := server.checkVersionCompatibility(tt.clientVersion)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got nil", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: expected no error but got: %v", tt.description, err)
			}
		})
	}
}

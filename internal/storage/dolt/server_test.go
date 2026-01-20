//go:build cgo

// Package dolt provides server mode tests for dolt sql-server integration (bd-f4f78a).
package dolt

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// getTestServerPort returns an available port for test servers.
// Uses a dynamic port to avoid conflicts with production servers on 3306.
func getTestServerPort(t *testing.T) int {
	t.Helper()
	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// TestIsServerRunning tests the server running detection.
func TestIsServerRunning(t *testing.T) {
	// Test with a port that's definitely not running
	if IsServerRunning("127.0.0.1", 59999) {
		t.Error("expected IsServerRunning to return false for unused port")
	}

	// Start a listener to simulate a server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	if !IsServerRunning("127.0.0.1", port) {
		t.Error("expected IsServerRunning to return true for listening port")
	}
}

// TestServerConfigFromStoreConfig tests config conversion.
func TestServerConfigFromStoreConfig(t *testing.T) {
	storeCfg := &Config{
		Path:       "/tmp/test-dolt",
		ServerHost: "192.168.1.1",
		ServerPort: 3307,
		ServerUser: "testuser",
		ServerPass: "testpass",
	}

	serverCfg := ServerConfigFromStoreConfig(storeCfg)

	if serverCfg.DataDir != storeCfg.Path {
		t.Errorf("expected DataDir %q, got %q", storeCfg.Path, serverCfg.DataDir)
	}
	if serverCfg.Host != storeCfg.ServerHost {
		t.Errorf("expected Host %q, got %q", storeCfg.ServerHost, serverCfg.Host)
	}
	if serverCfg.Port != storeCfg.ServerPort {
		t.Errorf("expected Port %d, got %d", storeCfg.ServerPort, serverCfg.Port)
	}
	if serverCfg.User != storeCfg.ServerUser {
		t.Errorf("expected User %q, got %q", storeCfg.ServerUser, serverCfg.User)
	}
	if serverCfg.Password != storeCfg.ServerPass {
		t.Errorf("expected Password %q, got %q", storeCfg.ServerPass, serverCfg.Password)
	}
}

// TestDefaultServerConfig tests default config values.
func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig("/tmp/test-dolt")

	if cfg.DataDir != "/tmp/test-dolt" {
		t.Errorf("expected DataDir /tmp/test-dolt, got %q", cfg.DataDir)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("expected Host 127.0.0.1, got %q", cfg.Host)
	}
	if cfg.Port != 3306 {
		t.Errorf("expected Port 3306, got %d", cfg.Port)
	}
	if cfg.User != "root" {
		t.Errorf("expected User root, got %q", cfg.User)
	}
	if cfg.Password != "" {
		t.Errorf("expected empty Password, got %q", cfg.Password)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel info, got %q", cfg.LogLevel)
	}
}

// TestServerStartStop tests starting and stopping a dolt sql-server.
// This test requires dolt to be installed.
func TestServerStartStop(t *testing.T) {
	skipIfNoDolt(t)

	// Create temp directory for the test
	tmpDir, err := os.MkdirTemp("", "dolt-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get an available port
	port := getTestServerPort(t)

	cfg := &ServerConfig{
		DataDir:  tmpDir,
		Host:     "127.0.0.1",
		Port:     port,
		User:     "root",
		Password: "",
		LogLevel: "warning", // Reduce log noise in tests
	}

	// Initialize a dolt database in a subdirectory
	dbDir := filepath.Join(tmpDir, "testdb")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}

	// Initialize dolt in the database directory
	cmd := exec.CommandContext(ctx, "dolt", "init")
	cmd.Dir = dbDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init dolt: %v, output: %s", err, output)
	}

	// Start the server
	pid, err := StartServer(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if pid <= 0 {
		t.Error("expected valid PID")
	}

	// Verify server is running
	if !IsServerRunning(cfg.Host, cfg.Port) {
		t.Error("server should be running after start")
	}

	// Check PID file was created
	pidFile := filepath.Join(tmpDir, "sql-server.pid")
	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		t.Error("PID file should exist after server start")
	}

	// Read PID file and verify
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	filePID, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatalf("invalid PID in file: %v", err)
	}
	if filePID != pid {
		t.Errorf("PID file contains %d, expected %d", filePID, pid)
	}

	// Get server status
	status, err := GetServerStatus(tmpDir, cfg.Host, cfg.Port)
	if err != nil {
		t.Fatalf("failed to get server status: %v", err)
	}
	if !status.Running {
		t.Error("status should show server running")
	}
	if status.PID != pid {
		t.Errorf("status PID %d, expected %d", status.PID, pid)
	}

	// Stop the server
	if err := StopServer(tmpDir); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	// Wait a bit for server to fully stop
	time.Sleep(500 * time.Millisecond)

	// Verify server is stopped
	if IsServerRunning(cfg.Host, cfg.Port) {
		t.Error("server should not be running after stop")
	}

	// Verify PID file was cleaned up
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("PID file should be removed after server stop")
	}
}

// TestEnsureServerRunning tests idempotent server start.
func TestEnsureServerRunning(t *testing.T) {
	skipIfNoDolt(t)

	tmpDir, err := os.MkdirTemp("", "dolt-ensure-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := getTestServerPort(t)

	// Initialize dolt database
	dbDir := filepath.Join(tmpDir, "testdb")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}
	cmd := exec.CommandContext(ctx, "dolt", "init")
	cmd.Dir = dbDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init dolt: %v, output: %s", err, output)
	}

	cfg := &ServerConfig{
		DataDir:  tmpDir,
		Host:     "127.0.0.1",
		Port:     port,
		User:     "root",
		Password: "",
		LogLevel: "warning",
	}

	// First call should start the server
	if err := EnsureServerRunning(ctx, cfg); err != nil {
		t.Fatalf("first EnsureServerRunning failed: %v", err)
	}

	if !IsServerRunning(cfg.Host, cfg.Port) {
		t.Error("server should be running after first EnsureServerRunning")
	}

	// Second call should be idempotent (no error, server still running)
	if err := EnsureServerRunning(ctx, cfg); err != nil {
		t.Fatalf("second EnsureServerRunning failed: %v", err)
	}

	if !IsServerRunning(cfg.Host, cfg.Port) {
		t.Error("server should still be running after second EnsureServerRunning")
	}

	// Clean up
	if err := StopServer(tmpDir); err != nil {
		t.Logf("warning: failed to stop server during cleanup: %v", err)
	}
}

// TestServerModeStoreCreation tests creating a DoltStore in server mode.
func TestServerModeStoreCreation(t *testing.T) {
	skipIfNoDolt(t)

	tmpDir, err := os.MkdirTemp("", "dolt-store-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := getTestServerPort(t)

	// Initialize dolt database structure
	// The server mode expects databases to be in subdirectories
	dbDir := filepath.Join(tmpDir, "beads")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}
	cmd := exec.CommandContext(ctx, "dolt", "init")
	cmd.Dir = dbDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init dolt: %v, output: %s", err, output)
	}

	// Start a server for the test
	serverCfg := &ServerConfig{
		DataDir:  tmpDir,
		Host:     "127.0.0.1",
		Port:     port,
		User:     "root",
		Password: "",
		LogLevel: "warning",
	}

	pid, err := StartServer(ctx, serverCfg)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		_ = StopServer(tmpDir)
	}()

	t.Logf("Test server started with PID %d on port %d", pid, port)

	// Create store in server mode
	storeCfg := &Config{
		Path:           tmpDir,
		Database:       "beads",
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		ServerMode:     true,
		ServerHost:     "127.0.0.1",
		ServerPort:     port,
		ServerUser:     "root",
		ServerPass:     "",
	}

	store, err := New(ctx, storeCfg)
	if err != nil {
		t.Fatalf("failed to create server mode store: %v", err)
	}
	defer store.Close()

	// Verify store is using server mode
	if !store.IsServerMode() {
		t.Error("store should be in server mode")
	}

	// Verify we can perform operations
	if err := store.SetConfig(ctx, "test_key", "test_value"); err != nil {
		t.Fatalf("failed to set config via server mode: %v", err)
	}

	value, err := store.GetConfig(ctx, "test_key")
	if err != nil {
		t.Fatalf("failed to get config via server mode: %v", err)
	}
	if value != "test_value" {
		t.Errorf("expected 'test_value', got %q", value)
	}
}

// TestServerModeReadOnly tests read-only operations in server mode.
func TestServerModeReadOnly(t *testing.T) {
	skipIfNoDolt(t)

	tmpDir, err := os.MkdirTemp("", "dolt-readonly-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := getTestServerPort(t)

	// Initialize dolt database
	dbDir := filepath.Join(tmpDir, "beads")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}
	cmd := exec.CommandContext(ctx, "dolt", "init")
	cmd.Dir = dbDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init dolt: %v, output: %s", err, output)
	}

	// Start server
	serverCfg := &ServerConfig{
		DataDir:  tmpDir,
		Host:     "127.0.0.1",
		Port:     port,
		User:     "root",
		Password: "",
		LogLevel: "warning",
	}

	pid, err := StartServer(ctx, serverCfg)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		_ = StopServer(tmpDir)
	}()

	t.Logf("Test server started with PID %d on port %d", pid, port)

	// Create store in server mode with read-only flag
	storeCfg := &Config{
		Path:           tmpDir,
		Database:       "beads",
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		ReadOnly:       true,
		ServerMode:     true,
		ServerHost:     "127.0.0.1",
		ServerPort:     port,
		ServerUser:     "root",
		ServerPass:     "",
	}

	store, err := New(ctx, storeCfg)
	if err != nil {
		t.Fatalf("failed to create read-only server mode store: %v", err)
	}
	defer store.Close()

	// Verify store is using server mode
	if !store.IsServerMode() {
		t.Error("store should be in server mode")
	}
}

// TestServerModeAutoStart tests that server is auto-started when not running.
func TestServerModeAutoStart(t *testing.T) {
	skipIfNoDolt(t)

	tmpDir, err := os.MkdirTemp("", "dolt-autostart-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	port := getTestServerPort(t)

	// Initialize dolt database
	dbDir := filepath.Join(tmpDir, "beads")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}
	cmd := exec.CommandContext(ctx, "dolt", "init")
	cmd.Dir = dbDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init dolt: %v, output: %s", err, output)
	}

	// Verify server is NOT running before we create the store
	if IsServerRunning("127.0.0.1", port) {
		t.Skip("port already in use, skipping auto-start test")
	}

	// Create store in server mode - should auto-start the server
	storeCfg := &Config{
		Path:           tmpDir,
		Database:       "beads",
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		ServerMode:     true,
		ServerHost:     "127.0.0.1",
		ServerPort:     port,
		ServerUser:     "root",
		ServerPass:     "",
	}

	store, err := New(ctx, storeCfg)
	if err != nil {
		t.Fatalf("failed to create server mode store with auto-start: %v", err)
	}
	defer store.Close()

	// Verify server was started
	if !IsServerRunning("127.0.0.1", port) {
		t.Error("server should have been auto-started")
	}

	// Clean up - stop the auto-started server
	if err := StopServer(tmpDir); err != nil {
		t.Logf("warning: failed to stop auto-started server: %v", err)
	}
}

// TestConcurrentServerModeAccess tests multiple clients accessing server mode concurrently.
func TestConcurrentServerModeAccess(t *testing.T) {
	skipIfNoDolt(t)

	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "dolt-concurrent-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	port := getTestServerPort(t)

	// Initialize dolt database
	dbDir := filepath.Join(tmpDir, "beads")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}
	cmd := exec.CommandContext(ctx, "dolt", "init")
	cmd.Dir = dbDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init dolt: %v, output: %s", err, output)
	}

	// Start server
	serverCfg := &ServerConfig{
		DataDir:  tmpDir,
		Host:     "127.0.0.1",
		Port:     port,
		User:     "root",
		Password: "",
		LogLevel: "warning",
	}

	pid, err := StartServer(ctx, serverCfg)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		_ = StopServer(tmpDir)
	}()

	t.Logf("Test server started with PID %d on port %d", pid, port)

	// Create initial store to set up schema
	initialCfg := &Config{
		Path:           tmpDir,
		Database:       "beads",
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		ServerMode:     true,
		ServerHost:     "127.0.0.1",
		ServerPort:     port,
		ServerUser:     "root",
		ServerPass:     "",
	}

	initialStore, err := New(ctx, initialCfg)
	if err != nil {
		t.Fatalf("failed to create initial store: %v", err)
	}

	// Set up issue prefix
	if err := initialStore.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		initialStore.Close()
		t.Fatalf("failed to set prefix: %v", err)
	}
	initialStore.Close()

	// Now create multiple concurrent clients
	const numClients = 5
	const opsPerClient = 10

	type result struct {
		clientID int
		success  int
		errors   int
	}

	results := make(chan result, numClients)

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			r := result{clientID: clientID}

			// Each client creates its own store connection
			cfg := &Config{
				Path:           tmpDir,
				Database:       "beads",
				CommitterName:  fmt.Sprintf("client-%d", clientID),
				CommitterEmail: fmt.Sprintf("client%d@example.com", clientID),
				ServerMode:     true,
				ServerHost:     "127.0.0.1",
				ServerPort:     port,
				ServerUser:     "root",
				ServerPass:     "",
			}

			store, err := New(ctx, cfg)
			if err != nil {
				r.errors = opsPerClient
				results <- r
				return
			}
			defer store.Close()

			// Perform operations
			for j := 0; j < opsPerClient; j++ {
				key := fmt.Sprintf("client_%d_key_%d", clientID, j)
				value := fmt.Sprintf("value_%d_%d", clientID, j)

				if err := store.SetConfig(ctx, key, value); err != nil {
					r.errors++
					continue
				}

				retrieved, err := store.GetConfig(ctx, key)
				if err != nil {
					r.errors++
					continue
				}

				if retrieved != value {
					r.errors++
					continue
				}

				r.success++
			}

			results <- r
		}(i)
	}

	// Collect results
	totalSuccess := 0
	totalErrors := 0
	for i := 0; i < numClients; i++ {
		r := <-results
		t.Logf("Client %d: %d success, %d errors", r.clientID, r.success, r.errors)
		totalSuccess += r.success
		totalErrors += r.errors
	}

	t.Logf("Total: %d success, %d errors", totalSuccess, totalErrors)

	// All operations should succeed in server mode
	if totalErrors > 0 {
		t.Errorf("expected 0 errors in server mode, got %d", totalErrors)
	}

	expectedOps := numClients * opsPerClient
	if totalSuccess != expectedOps {
		t.Errorf("expected %d successful operations, got %d", expectedOps, totalSuccess)
	}
}

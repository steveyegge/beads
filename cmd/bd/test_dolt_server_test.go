//go:build cgo

package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// testDoltServerPort is the port of the shared test Dolt server.
// Set by startSharedTestDoltServer before any tests run.
// When non-zero, newTestStore connects to this server instead of the default.
var testDoltServerPort int

func init() {
	beforeTestsHook = startSharedTestDoltServer
}

// startSharedTestDoltServer ensures a Dolt server is available for the cmd/bd test suite.
// Prefers an already-running server on the default port (3307). If none is found,
// starts a dedicated test server. Each test gets its own database via uniqueTestDBName.
// Returns a cleanup function.
func startSharedTestDoltServer() func() {
	// Skip if dolt is not available
	if _, err := exec.LookPath("dolt"); err != nil {
		fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: dolt not installed, CGO tests requiring Dolt will fail\n")
		return func() {}
	}

	// Always start a dedicated test server for isolation.
	// Reusing the rig's Dolt server on 3307 is fragile: the shared server may be
	// overloaded by other processes, and 100+ tests creating concurrent databases
	// can overwhelm it. A dedicated server with --max-connections ensures reliability.
	return startDedicatedTestServer()
}

// tryExistingServer checks if a Dolt server is fully operational on the given port.
// A simple Ping is insufficient — the server may accept connections but fail on actual
// operations (e.g., broken/overloaded server). We verify by creating and dropping a database.
func tryExistingServer(port int) int {
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/?parseTime=true&timeout=5s", port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return 0
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return 0
	}

	// Verify server can actually perform DDL (not just accept connections)
	testDB := "beads_health_check_probe"
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", testDB)); err != nil {
		return 0
	}
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", testDB))
	return port
}

// startDedicatedTestServer starts a new Dolt server for tests on a free port.
// Uses exec.Command directly (instead of dolt.NewServer) to control all server flags
// including --max-connections which dolt.ServerConfig doesn't expose.
func startDedicatedTestServer() func() {
	tmpDir, err := os.MkdirTemp("", "beads-test-dolt-server-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: failed to create temp dir: %v\n", err)
		return func() {}
	}

	// Set up dolt identity (HOME is a temp dir in tests)
	for _, args := range [][]string{
		{"config", "--global", "--add", "user.name", "Test User"},
		{"config", "--global", "--add", "user.email", "test@test.com"},
	} {
		cmd := exec.Command("dolt", args...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: dolt %v failed: %v, output: %s\n", args, err, out)
			os.RemoveAll(tmpDir)
			return func() {}
		}
	}

	// Initialize dolt repo
	cmd := exec.Command("dolt", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: dolt init failed: %v, output: %s\n", err, out)
		os.RemoveAll(tmpDir)
		return func() {}
	}

	// Find a free port
	port := findFreePort()
	if port == 0 {
		port = 13310
	}

	// Start dolt sql-server
	serverLog := fmt.Sprintf("%s/server.log", tmpDir)
	serverArgs := []string{
		"sql-server",
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--max-connections", "200",
	}
	serverCmd := exec.Command("dolt", serverArgs...)
	serverCmd.Dir = tmpDir

	logFile, err := os.OpenFile(serverLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: failed to open log: %v\n", err)
		os.RemoveAll(tmpDir)
		return func() {}
	}
	serverCmd.Stdout = logFile
	serverCmd.Stderr = logFile

	if err := serverCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: failed to start dolt: %v\n", err)
		logFile.Close()
		os.RemoveAll(tmpDir)
		return func() {}
	}

	// Wait for MySQL protocol readiness (not just TCP)
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer readyCancel()
	if err := waitForMySQL(readyCtx, port); err != nil {
		fmt.Fprintf(os.Stderr, "test-dolt-server: WARNING: MySQL not ready: %v\n", err)
		_ = serverCmd.Process.Kill()
		logFile.Close()
		os.RemoveAll(tmpDir)
		return func() {}
	}

	testDoltServerPort = port
	// Set env vars so code paths use the test server:
	// - BEADS_DOLT_PORT: read by applyConfigDefaults (direct dolt.New calls)
	// - BEADS_DOLT_SERVER_PORT: read by configfile.GetDoltServerPort (NewFromConfig calls
	//   when metadata.json sets dolt_mode=server)
	// Note: do NOT set BEADS_DOLT_SERVER_MODE — that would change bd init behavior globally.
	// Instead, metadata.json written by writeTestMetadata sets dolt_mode=server per-store.
	os.Setenv("BEADS_DOLT_PORT", strconv.Itoa(port))
	os.Setenv("BEADS_DOLT_SERVER_PORT", strconv.Itoa(port))
	fmt.Fprintf(os.Stderr, "test-dolt-server: dedicated server ready on port %d (pid %d)\n", port, serverCmd.Process.Pid)

	return func() {
		os.Unsetenv("BEADS_DOLT_PORT")
		os.Unsetenv("BEADS_DOLT_SERVER_PORT")
		fmt.Fprintf(os.Stderr, "test-dolt-server: stopping server on port %d\n", port)
		_ = serverCmd.Process.Kill()
		_, _ = serverCmd.Process.Wait()
		logFile.Close()
		os.RemoveAll(tmpDir)
	}
}

// waitForMySQL waits until the Dolt server accepts MySQL protocol connections.
func waitForMySQL(ctx context.Context, port int) error {
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/?parseTime=true&timeout=2s", port)
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		err = db.PingContext(ctx)
		_ = db.Close()
		if err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for MySQL protocol on port %d", port)
}

// findFreePort finds a free TCP port by briefly listening on :0.
func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// uniqueTestDBName generates a unique database name for test isolation.
// Each test gets its own database on the shared test server.
func uniqueTestDBName(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("failed to generate random bytes: %v", err)
	}
	return "testdb_" + hex.EncodeToString(buf)
}

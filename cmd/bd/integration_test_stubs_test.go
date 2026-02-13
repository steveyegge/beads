//go:build integration
// +build integration

package main

// Stubs for missing test helper functions that prevent integration tests from compiling.
// These functions were referenced by tests but their definitions were removed/moved.
// Filed as pre-existing issue: these stubs allow the integration test suite to compile
// while the proper implementations are restored.
//
// Pre-existing build failure: the following functions are called but never defined:
//   - makeSocketTempDir (delete_rpc_test.go, dual_mode_test.go)
//   - initTestGitRepo (delete_rpc_test.go, dual_mode_test.go)
//   - startRPCServer (delete_rpc_test.go, dual_mode_test.go)
//   - newTestLogger (export_mtime_test.go)
//   - createTestLogger (periodic_remote_sync_test.go)

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

// DefaultRemoteSyncInterval is a stub constant for the removed periodic_remote_sync module.
const DefaultRemoteSyncInterval = 30 * time.Second

// makeSocketTempDir creates a temporary directory for socket files.
// STUB: This is a placeholder for a removed function definition.
func makeSocketTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bd-socket-test-*")
	if err != nil {
		t.Fatalf("makeSocketTempDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// initTestGitRepo initializes a git repository in the given directory.
// STUB: This is a placeholder for a removed function definition.
func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initTestGitRepo: git init failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	cmd.Run()
}

// startRPCServer starts an RPC server for testing.
// STUB: This is a placeholder for a removed function definition.
// Tests using this will fail at runtime until the proper implementation is restored.
func startRPCServer(
	_ context.Context,
	_ string,
	_ *sqlite.SQLiteStorage,
	_ string,
	_ string,
	_ *slog.Logger,
) (*rpc.Server, <-chan error, error) {
	return nil, nil, fmt.Errorf("startRPCServer: STUB - proper implementation was removed, tests using this will fail")
}

// newTestLogger returns a logger that discards output.
// STUB: This is a placeholder for a removed function definition.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// createTestLogger returns a logger that discards output.
// STUB: This is a placeholder for a removed function definition.
func createTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// getRemoteSyncInterval is a stub for the removed periodic remote sync function.
// STUB: This is a placeholder for a removed function definition.
func getRemoteSyncInterval(_ *slog.Logger) time.Duration {
	return DefaultRemoteSyncInterval
}

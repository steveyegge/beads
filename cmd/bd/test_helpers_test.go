package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/testutil/teststore"
)

const windowsOS = "windows"

func newTestStore(t *testing.T, _ string) storage.Storage {
	t.Helper()
	return teststore.New(t)
}

// newTestStoreWithPrefix creates a Dolt-backed storage.Storage at a specific directory
// with the given issue_prefix. The dbPath parameter is treated as a legacy SQLite path;
// the Dolt store is created in a "dolt" subdirectory under dbPath's parent directory.
func newTestStoreWithPrefix(t *testing.T, dbPath string, prefix string) storage.Storage {
	t.Helper()

	// Use the parent directory of the old .db file path as the base.
	beadsDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create database directory: %v", err)
	}

	storeDir := filepath.Join(beadsDir, "dolt")
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("Failed to create dolt store directory: %v", err)
	}

	ctx := context.Background()

	cfg := &dolt.Config{
		Path:              storeDir,
		CommitterName:     "test",
		CommitterEmail:    "test@example.com",
		Database:          "testdb",
		SkipDirtyTracking: true,
	}

	s, err := dolt.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create Dolt store: %v", err)
	}

	if err := s.SetConfig(ctx, "issue_prefix", prefix); err != nil {
		s.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	// Register standard Gas Town custom issue types.
	if err := s.SetConfig(ctx, "types.custom", "gate,molecule,convoy,merge-request,slot,agent,role,rig,event,message,advice,wisp"); err != nil {
		s.Close()
		t.Fatalf("Failed to set custom types: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s
}

// ensureTestMode sets BEADS_TEST_MODE environment variable to prevent production pollution.
func ensureTestMode(t *testing.T) {
	t.Helper()
	os.Setenv("BEADS_TEST_MODE", "1")
	t.Cleanup(func() {
		os.Unsetenv("BEADS_TEST_MODE")
	})
}

// ensureCleanGlobalState resets global state that may have been modified by other tests.
// Call this at the start of tests that manipulate globals directly.
func ensureCleanGlobalState(t *testing.T) {
	t.Helper()
	// Reset CommandContext so accessor functions fall back to globals
	resetCommandContext()
}

// runCommandInDir runs a command in the specified directory.
func runCommandInDir(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}

// runCommandInDirWithOutput runs a command in the specified directory and returns its output.
func runCommandInDirWithOutput(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// openExistingTestDB opens a Dolt store at the given path (for tests that need to reopen a store).
// The dbPath is treated as a legacy SQLite path; the Dolt store directory is resolved from it.
func openExistingTestDB(t *testing.T, dbPath string) (storage.Storage, error) {
	t.Helper()

	beadsDir := filepath.Dir(dbPath)
	storeDir := filepath.Join(beadsDir, "dolt")

	ctx := context.Background()
	cfg := &dolt.Config{
		Path:              storeDir,
		CommitterName:     "test",
		CommitterEmail:    "test@example.com",
		Database:          "testdb",
		SkipDirtyTracking: true,
	}

	s, err := dolt.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s, nil
}

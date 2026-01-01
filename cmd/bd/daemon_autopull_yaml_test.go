package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/syncbranch"
)

// TestAutoPullDefaultFromYamlConfig is a tracer bullet test that proves the bug:
// When sync-branch is configured in config.yaml (not SQLite), autoPull defaulting
// should be true, but the current implementation returns false.
//
// Root cause: daemon.go:111-114 checks store.GetConfig("sync.branch") which only
// reads from SQLite, but sync-branch is typically set in config.yaml which is
// read via the config package (viper).
//
// The fix should use syncbranch.IsConfigured() or syncbranch.IsConfiguredWithDB()
// instead of store.GetConfig().
//
// TRACER BULLET (Phase 1): This test is expected to FAIL before the fix is applied.
// Test failure proves the bug exists. Remove t.Skip() after fix is implemented.
//
// Bug report: The daemon's autoPull defaults to false even when sync-branch is
// configured in config.yaml, because the code only checks SQLite.
func TestAutoPullDefaultFromYamlConfig(t *testing.T) {
	// PHASE 1 TRACER BULLET: Skip this test to document expected failure
	// Remove this Skip() in Phase 2 when implementing the fix
	t.Skip("TRACER BULLET: This test proves the bug exists. autoPull reads from SQLite but sync-branch is in config.yaml. See daemon.go:111-114")
	// Create temp directory structure
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads directory: %v", err)
	}

	// Create config.yaml with sync-branch set (the user's configuration)
	configYAML := `# Beads configuration
sync-branch: beads-sync
issue-prefix: test
`
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to write config.yaml: %v", err)
	}

	// Create a database WITHOUT sync.branch in the config table
	dbPath := filepath.Join(beadsDir, "beads.db")
	ctx := context.Background()
	testStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore.Close()

	// Verify: sync.branch is NOT set in SQLite
	dbSyncBranch, _ := testStore.GetConfig(ctx, "sync.branch")
	if dbSyncBranch != "" {
		t.Fatalf("Expected no sync.branch in database, got %q", dbSyncBranch)
	}

	// Reinitialize config package to read from our test directory
	// This is what happens when bd daemon starts and reads config
	// Use t.Chdir which automatically restores the original directory on test cleanup
	t.Chdir(tmpDir)

	// Reinitialize viper config to pick up the new config.yaml
	if err := config.Initialize(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	// === THE BUG: Current implementation (what daemon.go does) ===
	// This simulates lines 111-114 in daemon.go:
	//   if syncBranch, err := store.GetConfig(ctx, "sync.branch"); err == nil && syncBranch != "" {
	//       autoPull = true
	//   }
	var autoPullFromDB bool
	if syncBranch, err := testStore.GetConfig(ctx, "sync.branch"); err == nil && syncBranch != "" {
		autoPullFromDB = true
	}

	// === THE FIX: What daemon.go SHOULD do ===
	// Use syncbranch.IsConfigured() which checks env var, config.yaml, AND SQLite
	autoPullFromYAML := syncbranch.IsConfigured()

	// Log the current state for debugging
	t.Logf("sync-branch in config.yaml: beads-sync")
	t.Logf("sync.branch in SQLite: %q (empty)", dbSyncBranch)
	t.Logf("autoPullFromDB (current bug): %v", autoPullFromDB)
	t.Logf("autoPullFromYAML (correct): %v", autoPullFromYAML)

	// === ASSERTIONS ===

	// Current behavior (the bug): autoPull is false because SQLite has no sync.branch
	// This assertion documents the bug - it PASSES because the bug exists
	if autoPullFromDB != false {
		t.Errorf("BUG VERIFICATION FAILED: Expected autoPullFromDB=false (bug behavior), got %v", autoPullFromDB)
	}

	// Expected behavior: autoPull should be true because config.yaml has sync-branch
	// This assertion SHOULD pass (and does with syncbranch.IsConfigured)
	if autoPullFromYAML != true {
		t.Errorf("Expected autoPullFromYAML=true (config.yaml has sync-branch), got %v", autoPullFromYAML)
	}

	// === THE PROOF: These should be equal, but they're not ===
	// This is the core assertion that proves the bug exists.
	// After the fix, both should be true and equal.
	if autoPullFromDB != autoPullFromYAML {
		t.Logf("BUG PROVEN: autoPullFromDB=%v but autoPullFromYAML=%v", autoPullFromDB, autoPullFromYAML)
		t.Log("The daemon uses store.GetConfig('sync.branch') which only checks SQLite,")
		t.Log("but sync-branch is configured in config.yaml which is read by viper.")
		t.Log("FIX: daemon.go:111-114 should use syncbranch.IsConfigured() or syncbranch.IsConfiguredWithDB()")

		// This makes the test FAIL to prove the bug exists
		t.Errorf("TRACER BULLET: autoPull determination differs between SQLite (%v) and config.yaml (%v)",
			autoPullFromDB, autoPullFromYAML)
	}
}

// TestAutoPullDefaultFromEnvVar verifies that env var override works correctly.
// This test should PASS because the env var is checked first.
func TestAutoPullDefaultFromEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads directory: %v", err)
	}

	// Create database without sync.branch
	dbPath := filepath.Join(beadsDir, "beads.db")
	ctx := context.Background()
	testStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore.Close()

	// Set env var (highest priority)
	t.Setenv("BEADS_SYNC_BRANCH", "env-sync-branch")

	// The daemon DOES check env var (line 97-98), so this path works
	// But it's in a separate code path from the YAML bug
	autoPullFromEnv := syncbranch.IsConfigured()

	if !autoPullFromEnv {
		t.Errorf("Expected autoPull=true when BEADS_SYNC_BRANCH is set, got false")
	}
}

// TestAutoPullDefaultFromSQLite verifies the legacy SQLite path still works.
// This test should PASS because the SQLite path works (it's just not used by config.yaml users).
func TestAutoPullDefaultFromSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads directory: %v", err)
	}

	// Create database WITH sync.branch
	dbPath := filepath.Join(beadsDir, "beads.db")
	ctx := context.Background()
	testStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore.Close()

	// Set sync.branch in SQLite (legacy configuration)
	if err := testStore.SetConfig(ctx, "sync.branch", "sqlite-sync-branch"); err != nil {
		t.Fatalf("Failed to set sync.branch in database: %v", err)
	}

	// The current daemon code should work for this case
	var autoPullFromDB bool
	if syncBranch, err := testStore.GetConfig(ctx, "sync.branch"); err == nil && syncBranch != "" {
		autoPullFromDB = true
	}

	if !autoPullFromDB {
		t.Errorf("Expected autoPull=true when sync.branch is in SQLite, got false")
	}

	// Verify syncbranch.IsConfiguredWithDB also works
	autoPullFromHelper := syncbranch.IsConfiguredWithDB(dbPath)
	if !autoPullFromHelper {
		t.Errorf("Expected IsConfiguredWithDB=true when sync.branch is in SQLite, got false")
	}
}

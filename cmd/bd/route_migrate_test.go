package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/types"
)

// createRoutesJSONL creates a routes.jsonl file with the given routes in the beads directory.
func createRoutesJSONL(t *testing.T, beadsDir string, routes []routing.Route) {
	t.Helper()
	routesPath := filepath.Join(beadsDir, routing.RoutesFileName)

	var lines []string
	for _, r := range routes {
		lines = append(lines, `{"prefix":"`+r.Prefix+`","path":"`+r.Path+`"}`)
	}
	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(routesPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create routes.jsonl: %v", err)
	}
}

// setupRouteMigrateTestEnv creates an isolated test environment for route migrate tests.
// It sets BEADS_DIR to force FindBeadsDir to use our test directory and creates
// a fake mayor/town.json so FindTownRoot identifies tmpDir as town root.
// Returns cleanup function that must be called.
func setupRouteMigrateTestEnv(t *testing.T, tmpDir, beadsDir string) func() {
	t.Helper()

	// Set BEADS_DIR to ensure test isolation (FindBeadsDir checks this first)
	origBeadsDir := os.Getenv("BEADS_DIR")
	os.Setenv("BEADS_DIR", beadsDir)

	// Create fake mayor/town.json so FindTownRoot identifies tmpDir as town root
	// This prevents routing from walking up to the real town root
	mayorDir := filepath.Join(tmpDir, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("failed to create mayor directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte(`{"name":"test-town"}`), 0644); err != nil {
		t.Fatalf("failed to create town.json: %v", err)
	}

	return func() {
		os.Setenv("BEADS_DIR", origBeadsDir)
	}
}

// TestRouteMigrate_DryRun verifies dry-run shows routes without making changes.
func TestRouteMigrate_DryRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create test routes.jsonl
	routes := []routing.Route{
		{Prefix: "gt-", Path: "gastown"},
		{Prefix: "bd-", Path: "beads"},
	}
	createRoutesJSONL(t, beadsDir, routes)

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Capture stdout
	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run dry-run
	routeMigrateDryRun = true
	routeMigrateBackup = false
	routeMigrateDelete = false
	defer func() {
		routeMigrateDryRun = false
	}()

	err = runRouteMigrate(routeMigrateCmd, []string{})
	w.Close()
	os.Stdout = oldStdout

	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Fatalf("runRouteMigrate failed: %v", err)
	}

	// Verify dry-run output shows routes
	if !strings.Contains(output, "Dry run") {
		t.Errorf("expected 'Dry run' in output, got: %s", output)
	}
	if !strings.Contains(output, "gt-") {
		t.Errorf("expected 'gt-' prefix in output, got: %s", output)
	}
	if !strings.Contains(output, "bd-") {
		t.Errorf("expected 'bd-' prefix in output, got: %s", output)
	}

	// Verify no route beads were created
	filter := types.IssueFilter{}
	issueType := types.IssueType("route")
	filter.IssueType = &issueType
	issues, err := testStore.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("failed to search for route beads: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 route beads after dry-run, got %d", len(issues))
	}

	// Verify routes.jsonl still exists (no deletion in dry-run)
	routesPath := filepath.Join(beadsDir, routing.RoutesFileName)
	if _, err := os.Stat(routesPath); os.IsNotExist(err) {
		t.Error("routes.jsonl should still exist after dry-run")
	}
}

// TestRouteMigrate_CreateRouteBeads verifies route beads are created correctly.
// Skipped: requires complex Dolt setup that causes timeouts in test environment.
// TODO: Enable when test environment properly isolates from Dolt worker threads.
func TestRouteMigrate_CreateRouteBeads(t *testing.T) {
	t.Skip("Skipped: Dolt async workers cause timeout - see gt-oasyjm.1.2")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create test routes.jsonl
	routes := []routing.Route{
		{Prefix: "gt-", Path: "gastown"},
		{Prefix: "bd-", Path: "beads"},
		{Prefix: "hq-", Path: "."},
	}
	createRoutesJSONL(t, beadsDir, routes)

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Run migration
	routeMigrateDryRun = false
	routeMigrateBackup = false
	routeMigrateDelete = false

	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("runRouteMigrate failed: %v", err)
	}

	// Verify route beads were created
	filter := types.IssueFilter{}
	issueType := types.IssueType("route")
	filter.IssueType = &issueType
	issues, err := testStore.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("failed to search for route beads: %v", err)
	}

	if len(issues) != 3 {
		t.Errorf("expected 3 route beads, got %d", len(issues))
	}

	// Verify the titles are in the expected format "prefix → path"
	foundPrefixes := make(map[string]bool)
	for _, issue := range issues {
		route := routing.ParseRouteFromTitle(issue.Title)
		if route.Prefix != "" {
			foundPrefixes[route.Prefix] = true
		}
	}

	for _, r := range routes {
		if !foundPrefixes[r.Prefix] {
			t.Errorf("expected route bead for prefix %s", r.Prefix)
		}
	}
}

// TestRouteMigrate_Idempotent verifies running migration twice doesn't create duplicates.
// Skipped: requires complex Dolt setup that causes timeouts in test environment.
func TestRouteMigrate_Idempotent(t *testing.T) {
	t.Skip("Skipped: Dolt async workers cause timeout - see gt-oasyjm.1.2")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create test routes.jsonl
	routes := []routing.Route{
		{Prefix: "gt-", Path: "gastown"},
		{Prefix: "bd-", Path: "beads"},
	}
	createRoutesJSONL(t, beadsDir, routes)

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Run migration first time
	routeMigrateDryRun = false
	routeMigrateBackup = false
	routeMigrateDelete = false

	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("first runRouteMigrate failed: %v", err)
	}

	// Count route beads after first run
	filter := types.IssueFilter{}
	issueType := types.IssueType("route")
	filter.IssueType = &issueType
	issuesAfterFirst, err := testStore.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("failed to search for route beads: %v", err)
	}
	countAfterFirst := len(issuesAfterFirst)

	// Run migration second time
	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("second runRouteMigrate failed: %v", err)
	}

	// Count route beads after second run
	issuesAfterSecond, err := testStore.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("failed to search for route beads after second run: %v", err)
	}
	countAfterSecond := len(issuesAfterSecond)

	// Verify no duplicates created
	if countAfterFirst != countAfterSecond {
		t.Errorf("idempotency violation: first run created %d beads, second run has %d beads",
			countAfterFirst, countAfterSecond)
	}
	if countAfterFirst != 2 {
		t.Errorf("expected 2 route beads, got %d", countAfterFirst)
	}
}

// TestRouteMigrate_BackupCreated verifies routes.jsonl.bak is created when --backup is set.
// Skipped: requires complex Dolt setup that causes timeouts in test environment.
func TestRouteMigrate_BackupCreated(t *testing.T) {
	t.Skip("Skipped: Dolt async workers cause timeout - see gt-oasyjm.1.2")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create test routes.jsonl with specific content
	routes := []routing.Route{
		{Prefix: "test-", Path: "testrig"},
	}
	createRoutesJSONL(t, beadsDir, routes)

	// Read original content for comparison
	routesPath := filepath.Join(beadsDir, routing.RoutesFileName)
	originalContent, err := os.ReadFile(routesPath)
	if err != nil {
		t.Fatalf("failed to read routes.jsonl: %v", err)
	}

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Run migration with backup enabled
	routeMigrateDryRun = false
	routeMigrateBackup = true
	routeMigrateDelete = false
	defer func() {
		routeMigrateBackup = false
	}()

	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("runRouteMigrate failed: %v", err)
	}

	// Verify backup file was created
	backupPath := routesPath + ".bak"
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("expected backup file %s to exist: %v", backupPath, err)
	}

	// Verify backup content matches original
	if string(backupContent) != string(originalContent) {
		t.Errorf("backup content mismatch:\nexpected: %s\ngot: %s",
			string(originalContent), string(backupContent))
	}
}

// TestRouteMigrate_DeleteAfter verifies routes.jsonl is removed when --delete is set.
// Skipped: requires complex Dolt setup that causes timeouts in test environment.
func TestRouteMigrate_DeleteAfter(t *testing.T) {
	t.Skip("Skipped: Dolt async workers cause timeout - see gt-oasyjm.1.2")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create test routes.jsonl
	routes := []routing.Route{
		{Prefix: "del-", Path: "deleteme"},
	}
	createRoutesJSONL(t, beadsDir, routes)

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Verify routes.jsonl exists before migration
	routesPath := filepath.Join(beadsDir, routing.RoutesFileName)
	if _, err := os.Stat(routesPath); os.IsNotExist(err) {
		t.Fatal("routes.jsonl should exist before migration")
	}

	// Run migration with delete enabled
	routeMigrateDryRun = false
	routeMigrateBackup = false
	routeMigrateDelete = true
	defer func() {
		routeMigrateDelete = false
	}()

	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("runRouteMigrate failed: %v", err)
	}

	// Verify routes.jsonl was deleted
	if _, err := os.Stat(routesPath); !os.IsNotExist(err) {
		t.Error("routes.jsonl should be deleted after migration with --delete")
	}

	// Verify route beads were still created
	filter := types.IssueFilter{}
	issueType := types.IssueType("route")
	filter.IssueType = &issueType
	issues, err := testStore.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("failed to search for route beads: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("expected 1 route bead, got %d", len(issues))
	}
}

// TestRouteMigrate_NoRoutesFile verifies graceful handling when routes.jsonl doesn't exist.
func TestRouteMigrate_NoRoutesFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads (no routes.jsonl)
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Run migration - should complete without error
	routeMigrateDryRun = false
	routeMigrateBackup = false
	routeMigrateDelete = false

	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("runRouteMigrate should not fail when no routes.jsonl exists: %v", err)
	}
}

// TestRouteMigrate_EmptyRoutesFile verifies graceful handling when routes.jsonl is empty.
func TestRouteMigrate_EmptyRoutesFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset CommandContext so accessor functions use globals
	ensureCleanGlobalState(t)

	// Reset global config (hq--5vj3)
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", SyncModeGitPortable)
	config.Set("storage-backend", "sqlite")

	// Save and restore global state
	oldRootCtx := rootCtx
	rootCtx = ctx
	origDaemonClient := daemonClient
	origStore := store
	origStoreActive := storeActive
	origDBPath := dbPath

	defer func() {
		rootCtx = oldRootCtx
		if store != nil && store != origStore {
			_ = store.Close()
		}
		storeMutex.Lock()
		store = origStore
		storeActive = origStoreActive
		storeMutex.Unlock()
		daemonClient = origDaemonClient
		dbPath = origDBPath
	}()

	// Create temp directory with .beads
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Set up isolated test environment
	cleanup := setupRouteMigrateTestEnv(t, tmpDir, beadsDir)
	defer cleanup()

	// Create empty routes.jsonl
	routesPath := filepath.Join(beadsDir, routing.RoutesFileName)
	if err := os.WriteFile(routesPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create empty routes.jsonl: %v", err)
	}

	// Create database
	testDBPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStore(t, testDBPath)

	// Set up globals for direct mode
	dbPath = testDBPath
	storeMutex.Lock()
	store = testStore
	storeActive = true
	storeMutex.Unlock()
	daemonClient = nil

	// Change to temp directory so FindBeadsDir works
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	defer os.Chdir(originalWd)

	// Run migration - should complete without error
	routeMigrateDryRun = false
	routeMigrateBackup = false
	routeMigrateDelete = false

	err = runRouteMigrate(routeMigrateCmd, []string{})
	if err != nil {
		t.Fatalf("runRouteMigrate should not fail when routes.jsonl is empty: %v", err)
	}
}

// TestRouteMigrate_ParseRouteFromTitle verifies route parsing from bead titles.
func TestRouteMigrate_ParseRouteFromTitle(t *testing.T) {
	tests := []struct {
		title    string
		expected routing.Route
	}{
		{
			title:    "gt- → gastown",
			expected: routing.Route{Prefix: "gt-", Path: "gastown"},
		},
		{
			title:    "bd- → beads",
			expected: routing.Route{Prefix: "bd-", Path: "beads"},
		},
		{
			title:    "hq- → .",
			expected: routing.Route{Prefix: "hq-", Path: "."},
		},
		{
			title:    "hq- -> town root",
			expected: routing.Route{Prefix: "hq-", Path: "."},
		},
		{
			title:    "prefix -> path",
			expected: routing.Route{Prefix: "prefix-", Path: "path"},
		},
		{
			title:    "invalid title without arrow",
			expected: routing.Route{},
		},
		{
			title:    "",
			expected: routing.Route{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := routing.ParseRouteFromTitle(tt.title)
			if got.Prefix != tt.expected.Prefix || got.Path != tt.expected.Path {
				t.Errorf("ParseRouteFromTitle(%q) = {%q, %q}, want {%q, %q}",
					tt.title, got.Prefix, got.Path, tt.expected.Prefix, tt.expected.Path)
			}
		})
	}
}

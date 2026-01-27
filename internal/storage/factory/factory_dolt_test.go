//go:build cgo

// Package factory tests for Dolt backend selection and integration.
package factory

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/types"
)

// skipIfNoDolt skips the test if Dolt is not installed
func skipIfNoDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("Dolt not installed, skipping test")
	}
}

// TestDoltBackendRegistration verifies that the Dolt backend is registered.
func TestDoltBackendRegistration(t *testing.T) {
	// The Dolt backend registers itself via init() in factory_dolt.go
	// We can verify it by checking if we can create a Dolt store
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	doltPath := filepath.Join(tmpDir, "dolt")

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// If Dolt backend is registered, NewWithOptions should work
	store, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{ReadOnly: false})
	if err != nil {
		t.Fatalf("Dolt backend should be registered, but got error: %v", err)
	}
	defer store.Close()
}

// TestNewDoltStore tests creating a Dolt store via the factory.
func TestNewDoltStore(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	doltPath := filepath.Join(tmpDir, "dolt")

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// Create store via factory
	store, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{
		ReadOnly: false,
	})
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Verify store is functional
	if err := store.SetConfig(ctx, "test_key", "test_value"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	value, err := store.GetConfig(ctx, "test_key")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if value != "test_value" {
		t.Errorf("expected 'test_value', got %q", value)
	}
}

// TestDoltBootstrapFromJSONL tests that JSONL is bootstrapped on first open.
func TestDoltBootstrapFromJSONL(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	doltPath := filepath.Join(beadsDir, "dolt")

	// Create beads directory structure
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	// Create JSONL file with test issues
	issues := []types.Issue{
		{
			ID:          "test-001",
			Title:       "First issue",
			Description: "Test description 1",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			CreatedAt:   time.Now().Add(-time.Hour),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "test-002",
			Title:       "Second issue",
			Description: "Test description 2",
			Status:      types.StatusInProgress,
			Priority:    1,
			IssueType:   types.TypeBug,
			CreatedAt:   time.Now().Add(-2 * time.Hour),
			UpdatedAt:   time.Now().Add(-30 * time.Minute),
		},
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	var content strings.Builder
	for _, issue := range issues {
		data, err := json.Marshal(issue)
		if err != nil {
			t.Fatalf("failed to marshal issue: %v", err)
		}
		content.Write(data)
		content.WriteString("\n")
	}
	if err := os.WriteFile(jsonlPath, []byte(content.String()), 0644); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	// Set environment to disable server mode for predictable testing
	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// Create store via factory - should trigger bootstrap
	store, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{
		ReadOnly: false,
	})
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Verify issues were imported
	issue1, err := store.GetIssue(ctx, "test-001")
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue1 == nil {
		t.Fatal("expected issue test-001 to exist")
	}
	if issue1.Title != "First issue" {
		t.Errorf("expected title 'First issue', got %q", issue1.Title)
	}

	issue2, err := store.GetIssue(ctx, "test-002")
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue2 == nil {
		t.Fatal("expected issue test-002 to exist")
	}
}

// TestDoltReadOnlyMode tests opening Dolt in read-only mode.
func TestDoltReadOnlyMode(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	doltPath := filepath.Join(tmpDir, "dolt")

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// First create the database in write mode
	writeStore, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{
		ReadOnly: false,
	})
	if err != nil {
		t.Fatalf("failed to create Dolt store for writing: %v", err)
	}

	// Create some data
	if err := writeStore.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	issue := &types.Issue{
		ID:          "readonly-test",
		Title:       "Read-Only Test Issue",
		Description: "For testing read-only mode",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := writeStore.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	writeStore.Close()

	// Now open in read-only mode
	readStore, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("failed to create Dolt store in read-only mode: %v", err)
	}
	defer readStore.Close()

	// Verify we can read
	retrieved, err := readStore.GetIssue(ctx, "readonly-test")
	if err != nil {
		t.Fatalf("failed to get issue in read-only mode: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to find issue in read-only mode")
	}
	if retrieved.Title != "Read-Only Test Issue" {
		t.Errorf("expected 'Read-Only Test Issue', got %q", retrieved.Title)
	}
}

// TestDoltFactoryOptions tests various factory options.
func TestDoltFactoryOptions(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	doltPath := filepath.Join(tmpDir, "dolt")

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// Test with idle timeout option
	store, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{
		ReadOnly:    false,
		IdleTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create Dolt store with options: %v", err)
	}
	defer store.Close()

	// Verify store is functional
	if err := store.SetConfig(ctx, "options_test", "value"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}
}

// TestDoltNoBootstrapWhenExists tests that bootstrap is skipped when Dolt already exists.
func TestDoltNoBootstrapWhenExists(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	doltPath := filepath.Join(beadsDir, "dolt")

	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// First open creates the database
	store1, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{ReadOnly: false})
	if err != nil {
		t.Fatalf("failed to create Dolt store first time: %v", err)
	}
	if err := store1.SetConfig(ctx, "issue_prefix", "existing"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}
	store1.Close()

	// Create a JSONL file with different data
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	issue := types.Issue{
		ID:     "new-001",
		Title:  "New issue that should NOT be imported",
		Status: types.StatusOpen,
	}
	data, _ := json.Marshal(issue)
	if err := os.WriteFile(jsonlPath, append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	// Second open should NOT bootstrap (Dolt already exists)
	store2, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{ReadOnly: false})
	if err != nil {
		t.Fatalf("failed to create Dolt store second time: %v", err)
	}
	defer store2.Close()

	// Verify original prefix preserved (bootstrap was skipped)
	prefix, err := store2.GetConfig(ctx, "issue_prefix")
	if err != nil {
		t.Fatalf("failed to get prefix: %v", err)
	}
	if prefix != "existing" {
		t.Errorf("expected prefix 'existing', got %q (bootstrap may have run when it shouldn't)", prefix)
	}

	// Verify the new issue was NOT imported
	newIssue, err := store2.GetIssue(ctx, "new-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newIssue != nil {
		t.Error("new issue should NOT exist (bootstrap should have been skipped)")
	}
}

// TestDoltServerModeEnvVar tests the BEADS_DOLT_SERVER_MODE environment variable.
func TestDoltServerModeEnvVar(t *testing.T) {
	skipIfNoDolt(t)

	// Test with server mode disabled
	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")

	ctx := context.Background()
	tmpDir := t.TempDir()
	doltPath := filepath.Join(tmpDir, "dolt")

	store, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{ReadOnly: false})
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	store.Close()

	os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// Note: Testing with server mode enabled would require an actual server
	// which is covered in server_test.go
}

// TestDoltLockTimeout tests the lock timeout option.
func TestDoltLockTimeout(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	doltPath := filepath.Join(tmpDir, "dolt")

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// Create with lock timeout
	store, err := NewWithOptions(ctx, configfile.BackendDolt, doltPath, Options{
		ReadOnly:    false,
		LockTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create Dolt store with lock timeout: %v", err)
	}
	defer store.Close()

	// Store should be functional
	if err := store.SetConfig(ctx, "lock_test", "value"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}
}

// TestUnknownBackendError tests that unknown backends return an error.
func TestUnknownBackendError(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	_, err := NewWithOptions(ctx, "unknown-backend", tmpDir, Options{})
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

// TestNewFromConfigDolt tests creating a Dolt store from config.
func TestNewFromConfigDolt(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")

	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	// Create metadata.json with dolt backend
	metadata := `{"backend": "dolt"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	os.Setenv("BEADS_DOLT_SERVER_MODE", "0")
	defer os.Unsetenv("BEADS_DOLT_SERVER_MODE")

	// Create store from config
	store, err := NewFromConfigWithOptions(ctx, beadsDir, Options{ReadOnly: false})
	if err != nil {
		t.Fatalf("failed to create store from config: %v", err)
	}
	defer store.Close()

	// Verify store works
	if err := store.SetConfig(ctx, "from_config", "test"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}
}

// TestGetBackendFromConfigDolt tests retrieving backend type from config.
func TestGetBackendFromConfigDolt(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")

	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	// Create metadata.json with dolt backend
	metadata := `{"backend": "dolt"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	backend := GetBackendFromConfig(beadsDir)
	if backend != configfile.BackendDolt {
		t.Errorf("expected backend 'dolt', got %q", backend)
	}
}

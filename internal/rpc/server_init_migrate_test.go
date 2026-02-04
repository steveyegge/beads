package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupInitMigrateTestServer creates a test server with SQLite storage for init/migrate tests
func setupInitMigrateTestServer(t *testing.T) (*Server, storage.Storage) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Initialize database with required config
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Set bd_version metadata for inspect tests
	if err := store.SetMetadata(ctx, "bd_version", "1.0.0"); err != nil {
		t.Fatalf("failed to set bd_version: %v", err)
	}

	server := NewServer("/tmp/test.sock", store, tmpDir, dbPath)
	return server, store
}

// TestHandleInit_ReturnsNotSupported verifies that init returns appropriate "run locally" message
func TestHandleInit_ReturnsNotSupported(t *testing.T) {
	server, _ := setupInitMigrateTestServer(t)

	req := &Request{
		Operation: OpInit,
		Args:      []byte(`{}`),
		Actor:     "test",
	}

	resp := server.handleInit(req)

	// Init via RPC should return an error indicating it's not supported
	if resp.Success {
		t.Error("expected init via RPC to fail (not supported)")
	}

	// Should contain guidance to run locally
	if resp.Error == "" {
		t.Error("expected error message")
	}
	expectedMsg := "init via RPC is not supported; run 'bd init' locally instead"
	if resp.Error != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, resp.Error)
	}
}

// TestHandleMigrate_ReturnsNotSupported verifies that migrate returns appropriate message
func TestHandleMigrate_ReturnsNotSupported(t *testing.T) {
	server, _ := setupInitMigrateTestServer(t)

	req := &Request{
		Operation: OpMigrate,
		Args:      []byte(`{}`),
		Actor:     "test",
	}

	resp := server.handleMigrate(req)

	// Migrate via RPC (without --inspect) should return an error
	if resp.Success {
		t.Error("expected migrate via RPC to fail (not supported)")
	}

	// Should contain guidance to run locally
	if resp.Error == "" {
		t.Error("expected error message")
	}
	expectedMsg := "migrate via RPC is not supported; run 'bd migrate' locally instead"
	if resp.Error != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, resp.Error)
	}
}

// TestHandleMigrateInspect_ReturnsDBInfo verifies that inspect mode returns version, issue count, prefix
func TestHandleMigrateInspect_ReturnsDBInfo(t *testing.T) {
	server, store := setupInitMigrateTestServer(t)
	ctx := context.Background()

	// Create some test issues to verify issue count
	now := time.Now()
	issue1 := &types.Issue{
		ID:        "test-001",
		Title:     "Test Issue 1",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		CreatedAt: now,
		UpdatedAt: now,
	}
	issue2 := &types.Issue{
		ID:        "test-002",
		Title:     "Test Issue 2",
		Status:    types.StatusOpen,
		IssueType: types.TypeBug,
		Priority:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Create issues in storage
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("failed to create test issue 1: %v", err)
	}
	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("failed to create test issue 2: %v", err)
	}

	// Call migrate with inspect flag
	args := MigrateArgs{
		Inspect: true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMigrate,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMigrate(req)

	// Inspect should succeed
	if !resp.Success {
		t.Fatalf("expected inspect to succeed, got error: %s", resp.Error)
	}

	// Parse the result
	var result MigrateResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify status
	if result.Status != "success" {
		t.Errorf("expected status 'success', got %q", result.Status)
	}

	// Verify version
	if result.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", result.Version)
	}

	// Verify database path is included
	if result.CurrentDatabase == "" {
		t.Error("expected current_database to be set")
	}

	// Verify message contains expected information
	if result.Message == "" {
		t.Error("expected message to be set")
	}

	// Message should contain version, issue count, and prefix
	if !strings.Contains(result.Message, "1.0.0") {
		t.Errorf("expected message to contain version '1.0.0', got %q", result.Message)
	}
	if !strings.Contains(result.Message, "2 issues") {
		t.Errorf("expected message to contain '2 issues', got %q", result.Message)
	}
	if !strings.Contains(result.Message, "test") {
		t.Errorf("expected message to contain prefix 'test', got %q", result.Message)
	}
}

// TestHandleInit_ArgsValidation tests init with various args
func TestHandleInit_ArgsValidation(t *testing.T) {
	server, _ := setupInitMigrateTestServer(t)

	tests := []struct {
		name     string
		args     interface{}
		wantErr  bool
		errMatch string
	}{
		{
			name:     "empty args",
			args:     InitArgs{},
			wantErr:  true,
			errMatch: "init via RPC is not supported",
		},
		{
			name: "with prefix",
			args: InitArgs{
				Prefix: "my-proj",
			},
			wantErr:  true,
			errMatch: "init via RPC is not supported",
		},
		{
			name: "with backend",
			args: InitArgs{
				Backend: "sqlite",
			},
			wantErr:  true,
			errMatch: "init via RPC is not supported",
		},
		{
			name: "with force flag",
			args: InitArgs{
				Force: true,
			},
			wantErr:  true,
			errMatch: "init via RPC is not supported",
		},
		{
			name: "with all options",
			args: InitArgs{
				Prefix:    "test",
				Backend:   "dolt",
				Branch:    "main",
				Force:     true,
				FromJSONL: true,
				Quiet:     true,
			},
			wantErr:  true,
			errMatch: "init via RPC is not supported",
		},
		{
			name:     "invalid JSON",
			args:     nil, // Will be set to invalid JSON below
			wantErr:  true,
			errMatch: "invalid init arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var argsJSON []byte
			if tt.name == "invalid JSON" {
				argsJSON = []byte(`{"invalid json`)
			} else {
				argsJSON, _ = json.Marshal(tt.args)
			}

			req := &Request{
				Operation: OpInit,
				Args:      argsJSON,
				Actor:     "test",
			}

			resp := server.handleInit(req)

			if tt.wantErr {
				if resp.Success {
					t.Error("expected error, got success")
				}
				if resp.Error == "" {
					t.Error("expected error message")
				}
				// Check error message contains expected text
				if !strings.Contains(resp.Error, tt.errMatch) {
					t.Errorf("expected error to contain %q, got %q", tt.errMatch, resp.Error)
				}
			} else {
				if !resp.Success {
					t.Errorf("expected success, got error: %s", resp.Error)
				}
			}
		})
	}
}

// TestHandleMigrate_InspectFlag verifies that --inspect flag triggers inspect handler
func TestHandleMigrate_InspectFlag(t *testing.T) {
	server, _ := setupInitMigrateTestServer(t)

	tests := []struct {
		name        string
		args        MigrateArgs
		wantSuccess bool
		wantStatus  string
	}{
		{
			name:        "no flags - not supported",
			args:        MigrateArgs{},
			wantSuccess: false,
		},
		{
			name: "inspect flag - supported",
			args: MigrateArgs{
				Inspect: true,
			},
			wantSuccess: true,
			wantStatus:  "success",
		},
		{
			name: "dry_run without inspect - not supported",
			args: MigrateArgs{
				DryRun: true,
			},
			wantSuccess: false,
		},
		{
			name: "cleanup without inspect - not supported",
			args: MigrateArgs{
				Cleanup: true,
			},
			wantSuccess: false,
		},
		{
			name: "to_dolt without inspect - not supported",
			args: MigrateArgs{
				ToDolt: true,
			},
			wantSuccess: false,
		},
		{
			name: "to_sqlite without inspect - not supported",
			args: MigrateArgs{
				ToSQLite: true,
			},
			wantSuccess: false,
		},
		{
			name: "inspect with other flags - inspect takes precedence",
			args: MigrateArgs{
				Inspect: true,
				DryRun:  true,
				Cleanup: true,
			},
			wantSuccess: true,
			wantStatus:  "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsJSON, _ := json.Marshal(tt.args)

			req := &Request{
				Operation: OpMigrate,
				Args:      argsJSON,
				Actor:     "test",
			}

			resp := server.handleMigrate(req)

			if tt.wantSuccess {
				if !resp.Success {
					t.Fatalf("expected success, got error: %s", resp.Error)
				}

				var result MigrateResult
				if err := json.Unmarshal(resp.Data, &result); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				if result.Status != tt.wantStatus {
					t.Errorf("expected status %q, got %q", tt.wantStatus, result.Status)
				}
			} else {
				if resp.Success {
					t.Error("expected failure, got success")
				}
				if resp.Error == "" {
					t.Error("expected error message")
				}
			}
		})
	}
}

// TestHandleMigrateInspect_NoStorage verifies error when storage is nil
func TestHandleMigrateInspect_NoStorage(t *testing.T) {
	// Create a server without storage
	server := NewServer("/tmp/test.sock", nil, t.TempDir(), "")

	args := MigrateArgs{
		Inspect: true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMigrate,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMigrate(req)

	if resp.Success {
		t.Error("expected failure when storage is nil")
	}
	if resp.Error != "storage not available" {
		t.Errorf("expected error 'storage not available', got %q", resp.Error)
	}
}

// TestHandleMigrateInspect_MissingVersion verifies behavior when bd_version is missing
func TestHandleMigrateInspect_MissingVersion(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Set prefix but NOT version
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	server := NewServer("/tmp/test.sock", store, tmpDir, dbPath)

	args := MigrateArgs{
		Inspect: true,
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpMigrate,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleMigrate(req)

	// Should still succeed
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result MigrateResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// When metadata key doesn't exist, GetMetadata returns empty string (not error)
	// So version will be empty, not "unknown" (unknown is only set on error)
	if result.Version != "" {
		t.Errorf("expected version '' when metadata missing (no error), got %q", result.Version)
	}

	// Verify the response is still valid
	if result.Status != "success" {
		t.Errorf("expected status 'success', got %q", result.Status)
	}
}

// TestHandleInit_EmptyArgs verifies init with no args provided
func TestHandleInit_EmptyArgs(t *testing.T) {
	server, _ := setupInitMigrateTestServer(t)

	req := &Request{
		Operation: OpInit,
		Args:      nil, // No args at all
		Actor:     "test",
	}

	resp := server.handleInit(req)

	// Should return not supported error
	if resp.Success {
		t.Error("expected init via RPC to fail (not supported)")
	}
	if resp.Error != "init via RPC is not supported; run 'bd init' locally instead" {
		t.Errorf("unexpected error message: %s", resp.Error)
	}
}

// TestHandleMigrate_InvalidJSON verifies migrate with invalid JSON args
func TestHandleMigrate_InvalidJSON(t *testing.T) {
	server, _ := setupInitMigrateTestServer(t)

	req := &Request{
		Operation: OpMigrate,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleMigrate(req)

	if resp.Success {
		t.Error("expected failure for invalid JSON")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
	// Should mention invalid arguments
	if !strings.Contains(resp.Error, "invalid migrate arguments") {
		t.Errorf("expected error to contain 'invalid migrate arguments', got %q", resp.Error)
	}
}

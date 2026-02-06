//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// setupDaemonTestEnvForEdit creates a daemon test environment for edit command tests.
func setupDaemonTestEnvForEdit(t *testing.T) (context.Context, context.CancelFunc, *rpc.Client, *sqlite.SQLiteStorage, func()) {
	t.Helper()

	tmpDir := makeSocketTempDir(t)
	initTestGitRepo(t, tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}

	socketPath := filepath.Join(beadsDir, "bd.sock")
	testDBPath := filepath.Join(beadsDir, "beads.db")

	testStore := newTestStore(t, testDBPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log := daemonLogger{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, testDBPath, "", "", "", "", "", log)
	if err != nil {
		cancel()
		t.Fatalf("Failed to start RPC server: %v", err)
	}

	select {
	case <-server.WaitReady():
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Server did not become ready")
	}

	client, err := rpc.TryConnect(socketPath)
	if err != nil || client == nil {
		cancel()
		t.Fatalf("Failed to connect RPC client: %v", err)
	}

	cleanup := func() {
		if client != nil {
			client.Close()
		}
		if server != nil {
			server.Stop()
		}
		testStore.Close()
	}

	return ctx, cancel, client, testStore, cleanup
}

// TestEditViaDaemon_ShowAndUpdate tests the RPC path used by bd edit:
// 1. Show (fetch current field value)
// 2. Update (save edited value)
// This is the core flow that bd edit uses when daemonClient is non-nil.
func TestEditViaDaemon_ShowAndUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForEdit(t)
	defer cleanup()
	defer cancel()

	// Create a test issue with a description
	issue := &types.Issue{
		Title:       "Edit Test Issue",
		Description: "Original description",
		IssueType:   "task",
		Status:      types.StatusOpen,
		Priority:    2,
	}
	if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Step 1: Show via RPC (same as edit.go line 82-91)
	showArgs := &rpc.ShowArgs{ID: issue.ID}
	resp, err := client.Show(showArgs)
	if err != nil {
		t.Fatalf("Show RPC failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Show RPC returned error: %s", resp.Error)
	}

	var fetched types.Issue
	if err := json.Unmarshal(resp.Data, &fetched); err != nil {
		t.Fatalf("Failed to unmarshal show response: %v", err)
	}
	if fetched.Description != "Original description" {
		t.Fatalf("Expected 'Original description', got %q", fetched.Description)
	}

	// Step 2: Update via RPC (same as edit.go line 172-190)
	newDesc := "Edited description via RPC"
	updateArgs := &rpc.UpdateArgs{
		ID:          issue.ID,
		Description: &newDesc,
	}
	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Update RPC failed: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update RPC returned error: %s", updateResp.Error)
	}

	// Verify the update persisted
	verifyResp, err := client.Show(showArgs)
	if err != nil {
		t.Fatalf("Verify Show RPC failed: %v", err)
	}

	var verified types.Issue
	if err := json.Unmarshal(verifyResp.Data, &verified); err != nil {
		t.Fatalf("Failed to unmarshal verify response: %v", err)
	}
	if verified.Description != newDesc {
		t.Fatalf("Expected %q, got %q", newDesc, verified.Description)
	}
}

// TestEditViaDaemon_AllFields tests editing each field that bd edit supports.
func TestEditViaDaemon_AllFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForEdit(t)
	defer cleanup()
	defer cancel()

	issue := &types.Issue{
		Title:              "Multi-field Edit Test",
		Description:        "Original desc",
		Design:             "Original design",
		Notes:              "Original notes",
		AcceptanceCriteria: "Original acceptance",
		IssueType:          "task",
		Status:             types.StatusOpen,
		Priority:           2,
	}
	if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	tests := []struct {
		name     string
		field    string
		makeArgs func(val string) *rpc.UpdateArgs
		getField func(i *types.Issue) string
	}{
		{
			name:  "title",
			field: "title",
			makeArgs: func(val string) *rpc.UpdateArgs {
				return &rpc.UpdateArgs{ID: issue.ID, Title: &val}
			},
			getField: func(i *types.Issue) string { return i.Title },
		},
		{
			name:  "description",
			field: "description",
			makeArgs: func(val string) *rpc.UpdateArgs {
				return &rpc.UpdateArgs{ID: issue.ID, Description: &val}
			},
			getField: func(i *types.Issue) string { return i.Description },
		},
		{
			name:  "design",
			field: "design",
			makeArgs: func(val string) *rpc.UpdateArgs {
				return &rpc.UpdateArgs{ID: issue.ID, Design: &val}
			},
			getField: func(i *types.Issue) string { return i.Design },
		},
		{
			name:  "notes",
			field: "notes",
			makeArgs: func(val string) *rpc.UpdateArgs {
				return &rpc.UpdateArgs{ID: issue.ID, Notes: &val}
			},
			getField: func(i *types.Issue) string { return i.Notes },
		},
		{
			name:  "acceptance_criteria",
			field: "acceptance_criteria",
			makeArgs: func(val string) *rpc.UpdateArgs {
				return &rpc.UpdateArgs{ID: issue.ID, AcceptanceCriteria: &val}
			},
			getField: func(i *types.Issue) string { return i.AcceptanceCriteria },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newVal := "Edited " + tt.name + " via RPC"

			// Update field via RPC
			updateResp, err := client.Update(tt.makeArgs(newVal))
			if err != nil {
				t.Fatalf("Update RPC failed for %s: %v", tt.name, err)
			}
			if !updateResp.Success {
				t.Fatalf("Update RPC returned error for %s: %s", tt.name, updateResp.Error)
			}

			// Verify via Show RPC
			showResp, err := client.Show(&rpc.ShowArgs{ID: issue.ID})
			if err != nil {
				t.Fatalf("Show RPC failed: %v", err)
			}

			var updated types.Issue
			if err := json.Unmarshal(showResp.Data, &updated); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}
			if got := tt.getField(&updated); got != newVal {
				t.Errorf("Expected %q, got %q", newVal, got)
			}
		})
	}
}

// TestEditViaDaemon_WithDaemonHostSet verifies the edit RPC flow works
// when BD_DAEMON_HOST is set (the scenario from bd-bdbt).
// This confirms that the forceDirectMode bypass in main.go allows
// the edit command to reach the daemon RPC code path.
func TestEditViaDaemon_WithDaemonHostSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel, client, testStore, cleanup := setupDaemonTestEnvForEdit(t)
	defer cleanup()
	defer cancel()

	// Simulate BD_DAEMON_HOST being set (remote daemon mode)
	t.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")

	// Save and restore global state
	oldDaemonClient := daemonClient
	defer func() { daemonClient = oldDaemonClient }()
	daemonClient = client

	// Create test issue
	issue := &types.Issue{
		Title:       "BD_DAEMON_HOST Edit Test",
		Description: "Before edit",
		IssueType:   "task",
		Status:      types.StatusOpen,
		Priority:    2,
	}
	if err := testStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Verify Show works via daemon (edit.go lines 80-91)
	showArgs := &rpc.ShowArgs{ID: issue.ID}
	resp, err := daemonClient.Show(showArgs)
	if err != nil {
		t.Fatalf("Show via daemon failed with BD_DAEMON_HOST set: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Show via daemon returned error: %s", resp.Error)
	}

	var fetched types.Issue
	if err := json.Unmarshal(resp.Data, &fetched); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if fetched.Description != "Before edit" {
		t.Fatalf("Expected 'Before edit', got %q", fetched.Description)
	}

	// Verify Update works via daemon (edit.go lines 170-190)
	newDesc := "After edit via remote daemon"
	updateArgs := &rpc.UpdateArgs{
		ID:          issue.ID,
		Description: &newDesc,
	}
	updateResp, err := daemonClient.Update(updateArgs)
	if err != nil {
		t.Fatalf("Update via daemon failed with BD_DAEMON_HOST set: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update via daemon returned error: %s", updateResp.Error)
	}

	// Verify the update
	verifyResp, err := daemonClient.Show(showArgs)
	if err != nil {
		t.Fatalf("Verify Show failed: %v", err)
	}
	var verified types.Issue
	if err := json.Unmarshal(verifyResp.Data, &verified); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if verified.Description != newDesc {
		t.Fatalf("Expected %q, got %q", newDesc, verified.Description)
	}
}

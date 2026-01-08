package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestUpdatesFromArgs_DueAt verifies that DueAt is extracted from UpdateArgs
// and included in the updates map for the storage layer.
//
// This test is a TRACER BULLET for GH#952 Issue 1: Daemon ignoring --due flag.
// Gap 1: updatesFromArgs() handles 19 fields but DueAt/DeferUntil are MISSING.
//
// Expected behavior: When UpdateArgs.DueAt contains an RFC3339 date string,
// it should be parsed and added to the updates map as a time.Time value.
func TestUpdatesFromArgs_DueAt(t *testing.T) {
	tests := map[string]struct {
		input    string // ISO date or RFC3339 format
		wantKey  string
		wantTime bool // if true, expect time.Time value; if false, expect nil
	}{
		"RFC3339 with timezone": {
			input:    "2026-01-15T10:00:00Z",
			wantKey:  "due_at",
			wantTime: true,
		},
		"ISO date only": {
			input:    "2026-01-15",
			wantKey:  "due_at",
			wantTime: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			args := UpdateArgs{
				ID:    "test-issue",
				DueAt: &tt.input,
			}

			updates := updatesFromArgs(args)

			val, exists := updates[tt.wantKey]
			if !exists {
				t.Fatalf("updatesFromArgs did not include %q key; got keys: %v", tt.wantKey, mapKeys(updates))
			}

			if tt.wantTime {
				if _, ok := val.(time.Time); !ok {
					t.Errorf("expected time.Time value for %q, got %T: %v", tt.wantKey, val, val)
				}
			}
		})
	}
}

// TestUpdatesFromArgs_DeferUntil verifies that DeferUntil is extracted from UpdateArgs
// and included in the updates map for the storage layer.
//
// This test is a TRACER BULLET for GH#952 Issue 1: Daemon ignoring --defer flag.
// Gap 1: updatesFromArgs() handles 19 fields but DueAt/DeferUntil are MISSING.
//
// Expected behavior: When UpdateArgs.DeferUntil contains an RFC3339 date string,
// it should be parsed and added to the updates map as a time.Time value.
func TestUpdatesFromArgs_DeferUntil(t *testing.T) {
	tests := map[string]struct {
		input    string // ISO date or RFC3339 format
		wantKey  string
		wantTime bool
	}{
		"RFC3339 with timezone": {
			input:    "2026-01-20T14:30:00Z",
			wantKey:  "defer_until",
			wantTime: true,
		},
		"ISO date only": {
			input:    "2026-01-20",
			wantKey:  "defer_until",
			wantTime: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			args := UpdateArgs{
				ID:         "test-issue",
				DeferUntil: &tt.input,
			}

			updates := updatesFromArgs(args)

			val, exists := updates[tt.wantKey]
			if !exists {
				t.Fatalf("updatesFromArgs did not include %q key; got keys: %v", tt.wantKey, mapKeys(updates))
			}

			if tt.wantTime {
				if _, ok := val.(time.Time); !ok {
					t.Errorf("expected time.Time value for %q, got %T: %v", tt.wantKey, val, val)
				}
			}
		})
	}
}

// TestUpdatesFromArgs_ClearFields verifies that empty strings clear date fields.
//
// This test is a TRACER BULLET for GH#952: verifying that undefer works.
// When an empty string is passed for DueAt or DeferUntil, it should result in
// a nil value in the updates map, which will clear the field in the database.
//
// Expected behavior: Empty string input should set the field to nil in updates map.
func TestUpdatesFromArgs_ClearFields(t *testing.T) {
	tests := map[string]struct {
		setupArgs func() UpdateArgs
		wantKey   string
	}{
		"clear due_at with empty string": {
			setupArgs: func() UpdateArgs {
				empty := ""
				return UpdateArgs{
					ID:    "test-issue",
					DueAt: &empty,
				}
			},
			wantKey: "due_at",
		},
		"clear defer_until with empty string": {
			setupArgs: func() UpdateArgs {
				empty := ""
				return UpdateArgs{
					ID:         "test-issue",
					DeferUntil: &empty,
				}
			},
			wantKey: "defer_until",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			args := tt.setupArgs()

			updates := updatesFromArgs(args)

			val, exists := updates[tt.wantKey]
			if !exists {
				t.Fatalf("updatesFromArgs did not include %q key for clearing; got keys: %v", tt.wantKey, mapKeys(updates))
			}

			// When clearing, value should be nil (not an empty string)
			if val != nil {
				t.Errorf("expected nil value for clearing %q, got %T: %v", tt.wantKey, val, val)
			}
		})
	}
}

// TestHandleCreate_DeferUntil verifies that DeferUntil is parsed and set in handleCreate.
//
// This test is a TRACER BULLET for GH#952 Issue 1: Daemon ignoring --defer in create.
// Gap 2: handleCreate() parses DueAt (lines 224-239) but NOT DeferUntil.
//
// Expected behavior: When CreateArgs.DeferUntil contains an ISO date or RFC3339 string,
// it should be parsed and set on the created issue's DeferUntil field.
func TestHandleCreate_DeferUntil(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	tests := map[string]struct {
		deferUntil string
		wantSet    bool // true if DeferUntil should be set on the issue
	}{
		"RFC3339 format": {
			deferUntil: "2026-01-20T14:30:00Z",
			wantSet:    true,
		},
		"ISO date format": {
			deferUntil: "2026-01-20",
			wantSet:    true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			createArgs := &CreateArgs{
				Title:      "Test issue with defer - " + name,
				IssueType:  "task",
				Priority:   1,
				DeferUntil: tt.deferUntil,
			}

			resp, err := client.Create(createArgs)
			if err != nil {
				t.Fatalf("Create failed: %v", err)
			}
			if !resp.Success {
				t.Fatalf("Create returned error: %s", resp.Error)
			}

			var issue types.Issue
			if err := json.Unmarshal(resp.Data, &issue); err != nil {
				t.Fatalf("Failed to unmarshal issue: %v", err)
			}

			if tt.wantSet {
				if issue.DeferUntil == nil {
					t.Error("expected DeferUntil to be set, got nil")
				}
			}
		})
	}
}

// TestUpdateViaDaemon_DueAt tests end-to-end update of DueAt through the daemon RPC.
//
// This test verifies that `bd update --due` works via daemon mode.
// It creates an issue, updates it with a due date via RPC, and verifies
// the due date was actually persisted.
func TestUpdateViaDaemon_DueAt(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue without due date
	createArgs := &CreateArgs{
		Title:     "Issue for due date update test",
		IssueType: "task",
		Priority:  1,
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Update with due date via daemon RPC
	dueDate := "2026-01-25"
	updateArgs := &UpdateArgs{
		ID:    issue.ID,
		DueAt: &dueDate,
	}

	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update returned error: %s", updateResp.Error)
	}

	// Verify directly from storage
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	if retrieved.DueAt == nil {
		t.Fatal("expected DueAt to be set after update, got nil")
	}

	// Verify the date is correct (just check the date part)
	expectedDate := time.Date(2026, 1, 25, 0, 0, 0, 0, time.Local)
	if retrieved.DueAt.Year() != expectedDate.Year() ||
		retrieved.DueAt.Month() != expectedDate.Month() ||
		retrieved.DueAt.Day() != expectedDate.Day() {
		t.Errorf("DueAt date mismatch: got %v, want date 2026-01-25", retrieved.DueAt)
	}
}

// TestUpdateViaDaemon_DeferUntil tests end-to-end update of DeferUntil through the daemon RPC.
//
// This test verifies that `bd update --defer` and `bd defer --until` work via daemon mode.
func TestUpdateViaDaemon_DeferUntil(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue without defer_until
	createArgs := &CreateArgs{
		Title:     "Issue for defer update test",
		IssueType: "task",
		Priority:  1,
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Update with defer_until via daemon RPC
	deferDate := "2026-01-30"
	updateArgs := &UpdateArgs{
		ID:         issue.ID,
		DeferUntil: &deferDate,
	}

	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update returned error: %s", updateResp.Error)
	}

	// Verify directly from storage
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	if retrieved.DeferUntil == nil {
		t.Fatal("expected DeferUntil to be set after update, got nil")
	}

	// Verify the date is correct
	expectedDate := time.Date(2026, 1, 30, 0, 0, 0, 0, time.Local)
	if retrieved.DeferUntil.Year() != expectedDate.Year() ||
		retrieved.DeferUntil.Month() != expectedDate.Month() ||
		retrieved.DeferUntil.Day() != expectedDate.Day() {
		t.Errorf("DeferUntil date mismatch: got %v, want date 2026-01-30", retrieved.DeferUntil)
	}
}

// TestUndefer_ClearsDeferUntil tests that undefer clears the defer_until field via daemon.
//
// This verifies SC-005: `bd undefer` clears defer_until via daemon.
func TestUndefer_ClearsDeferUntil(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue with defer_until set
	createArgs := &CreateArgs{
		Title:      "Issue to undefer",
		IssueType:  "task",
		Priority:   1,
		DeferUntil: "2026-02-15",
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	// Verify defer_until was set on create
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if retrieved.DeferUntil == nil {
		t.Log("WARNING: DeferUntil not set on create - Gap 2 not yet fixed")
		// Set it directly for this test
		deferTime := time.Date(2026, 2, 15, 0, 0, 0, 0, time.Local)
		updates := map[string]interface{}{"defer_until": deferTime}
		if err := store.UpdateIssue(ctx, issue.ID, updates, "test"); err != nil {
			t.Fatalf("Failed to set defer_until directly: %v", err)
		}
	}

	// Now clear defer_until via RPC update with empty string
	empty := ""
	updateArgs := &UpdateArgs{
		ID:         issue.ID,
		DeferUntil: &empty,
	}

	updateResp, err := client.Update(updateArgs)
	if err != nil {
		t.Fatalf("Update (undefer) failed: %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("Update (undefer) returned error: %s", updateResp.Error)
	}

	// Verify defer_until was cleared
	retrieved, err = store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue after undefer: %v", err)
	}

	if retrieved.DeferUntil != nil {
		t.Errorf("expected DeferUntil to be nil after undefer, got %v", retrieved.DeferUntil)
	}
}

// TestCreateWithRelativeDate tests that relative date formats like "+1d" work via daemon create.
//
// This test is a TRACER BULLET for GH#952 Issue 3: DateTime format too restrictive.
// Gap 3: create.go sends raw strings like "+1d" instead of RFC3339 formatted times.
//
// The daemon's parseTimeRPC only handles RFC3339 and ISO date formats, not relative dates.
// This test verifies that the client properly formats relative dates before sending to daemon.
//
// Expected behavior: Relative date formats should result in correctly set DueAt/DeferUntil.
func TestCreateWithRelativeDate(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	tests := map[string]struct {
		dueAt      string
		deferUntil string
		wantDue    bool
		wantDefer  bool
	}{
		"relative +1d for due": {
			dueAt:   "+1d",
			wantDue: true,
		},
		"relative tomorrow for defer": {
			deferUntil: "tomorrow",
			wantDefer:  true,
		},
		"both relative dates": {
			dueAt:      "+2d",
			deferUntil: "+1d",
			wantDue:    true,
			wantDefer:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			createArgs := &CreateArgs{
				Title:      "Issue with relative date - " + name,
				IssueType:  "task",
				Priority:   1,
				DueAt:      tt.dueAt,
				DeferUntil: tt.deferUntil,
			}

			resp, err := client.Create(createArgs)
			if err != nil {
				t.Fatalf("Create failed: %v", err)
			}
			if !resp.Success {
				// This is expected to fail currently because the daemon doesn't parse relative dates
				t.Logf("Create returned error (expected with current bug): %s", resp.Error)
				t.Fatalf("Create failed with relative date: %s", resp.Error)
			}

			var issue types.Issue
			if err := json.Unmarshal(resp.Data, &issue); err != nil {
				t.Fatalf("Failed to unmarshal issue: %v", err)
			}

			// Verify from storage to ensure persistence
			retrieved, err := store.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get issue: %v", err)
			}

			if tt.wantDue {
				if retrieved.DueAt == nil {
					t.Error("expected DueAt to be set from relative date, got nil")
				} else {
					// Verify it's in the future
					if retrieved.DueAt.Before(time.Now()) {
						t.Errorf("expected DueAt to be in the future, got %v", retrieved.DueAt)
					}
				}
			}

			if tt.wantDefer {
				if retrieved.DeferUntil == nil {
					t.Error("expected DeferUntil to be set from relative date, got nil")
				} else {
					// Verify it's in the future
					if retrieved.DeferUntil.Before(time.Now()) {
						t.Errorf("expected DeferUntil to be in the future, got %v", retrieved.DeferUntil)
					}
				}
			}
		})
	}
}

// mapKeys returns the keys of a map for debugging
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

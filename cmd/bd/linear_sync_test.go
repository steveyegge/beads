package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// TestRunLinearSync_PullOnly tests pull-only sync with Linear tracker.
func TestRunLinearSync_PullOnly(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "linear")

	// Add issues to mock tracker
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:          "uuid-1",
			Identifier:  "LINEAR-1",
			URL:         "https://linear.app/team/issue/LINEAR-1",
			Title:       "Linear Issue 1",
			Description: "Description 1",
			Priority:    1,
			State:       "started",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "uuid-2",
			Identifier:  "LINEAR-2",
			URL:         "https://linear.app/team/issue/LINEAR-2",
			Title:       "Linear Issue 2",
			Description: "Description 2",
			Priority:    2,
			State:       "unstarted",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	opts := tracker.SyncOptions{
		Pull: true,
		Push: false,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if !mock.fetchCalled {
		t.Error("Expected FetchIssues to be called")
	}

	if mock.createCalled {
		t.Error("CreateIssue should not be called during pull-only")
	}

	if result.Stats.Created != 2 {
		t.Errorf("Expected 2 created issues, got %d", result.Stats.Created)
	}
}

// TestRunLinearSync_PushOnly tests push-only sync with Linear tracker.
func TestRunLinearSync_PushOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "linear")

	// Create local issue without external_ref
	issue := &types.Issue{
		Title:       "Local Linear Issue",
		Description: "Created locally",
		Priority:    2,
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	opts := tracker.SyncOptions{
		Pull:       false,
		Push:       true,
		UpdateRefs: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if mock.fetchCalled {
		t.Error("FetchIssues should not be called during push-only")
	}

	if !mock.createCalled {
		t.Error("Expected CreateIssue to be called")
	}

	if len(mock.createdIssues) != 1 {
		t.Errorf("Expected 1 created issue, got %d", len(mock.createdIssues))
	}

	if result.Stats.Created != 1 {
		t.Errorf("Expected 1 created issue in stats, got %d", result.Stats.Created)
	}
}

// TestRunLinearSync_PullAndPush tests bidirectional sync with Linear.
func TestRunLinearSync_PullAndPush(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "linear")

	// Add remote issue
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "uuid-1",
			Identifier: "LINEAR-1",
			URL:        "https://linear.app/team/issue/LINEAR-1",
			Title:      "Remote Linear Issue",
			Priority:   1,
			State:      "started",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create local issue
	localIssue := &types.Issue{
		Title:     "Local Linear Issue",
		Priority:  2,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	opts := tracker.SyncOptions{
		Pull:       true,
		Push:       true,
		UpdateRefs: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if !mock.fetchCalled {
		t.Error("Expected FetchIssues to be called")
	}

	if !mock.createCalled {
		t.Error("Expected CreateIssue to be called")
	}

	// Should have pulled 1 and pushed 1
	if result.Stats.Pulled != 1 {
		t.Errorf("Expected 1 pulled issue, got %d", result.Stats.Pulled)
	}

	if result.Stats.Pushed != 1 {
		t.Errorf("Expected 1 pushed issue, got %d", result.Stats.Pushed)
	}
}

// TestRunLinearSync_DryRun tests dry run mode.
func TestRunLinearSync_DryRun(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "linear")

	// Add remote issue
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "uuid-1",
			Identifier: "LINEAR-1",
			URL:        "https://linear.app/team/issue/LINEAR-1",
			Title:      "Remote Issue",
			Priority:   1,
			State:      "open",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create local issue
	localIssue := &types.Issue{
		Title:     "Local Issue",
		Priority:  2,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	opts := tracker.SyncOptions{
		Pull:   true,
		Push:   true,
		DryRun: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// No actual issues should be created
	if len(mock.createdIssues) != 0 {
		t.Errorf("Expected 0 created issues in dry run, got %d", len(mock.createdIssues))
	}

	if len(mock.updatedIssues) != 0 {
		t.Errorf("Expected 0 updated issues in dry run, got %d", len(mock.updatedIssues))
	}
}

// TestRunLinearSync_ConflictResolution tests conflict resolution modes.
func TestRunLinearSync_ConflictResolution(t *testing.T) {
	tests := []struct {
		name       string
		resolution tracker.ConflictResolution
	}{
		{"prefer-local", tracker.ConflictResolutionLocal},
		{"prefer-linear", tracker.ConflictResolutionExternal},
		{"timestamp", tracker.ConflictResolutionTimestamp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "linear")

			// Create issue with external_ref
			now := time.Now()
			externalRef := "https://linear.app/team/issue/LINEAR-1"
			localIssue := &types.Issue{
				Title:       "Local Version",
				Priority:    1,
				Status:      types.StatusOpen,
				IssueType:   types.TypeTask,
				ExternalRef: &externalRef,
				CreatedAt:   now.Add(-2 * time.Hour),
				UpdatedAt:   now.Add(-1 * time.Hour),
			}
			if err := syncStore.CreateIssue(ctx, localIssue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Set last_sync before both updates
			lastSync := now.Add(-3 * time.Hour).Format(time.RFC3339)
			if err := syncStore.SetConfig(ctx, "linear.last_sync", lastSync); err != nil {
				t.Fatalf("Failed to set last_sync: %v", err)
			}

			// Add remote issue
			mock.issues = []tracker.TrackerIssue{
				{
					ID:         "uuid-1",
					Identifier: "LINEAR-1",
					URL:        "https://linear.app/team/issue/LINEAR-1",
					Title:      "Remote Version",
					Priority:   2,
					State:      "open",
					CreatedAt:  now.Add(-2 * time.Hour),
					UpdatedAt:  now,
				},
			}

			opts := tracker.SyncOptions{
				Pull:               true,
				Push:               true,
				ConflictResolution: tt.resolution,
			}

			result, err := engine.Sync(ctx, opts)
			if err != nil {
				t.Fatalf("Sync failed: %v", err)
			}

			if !result.Success {
				t.Errorf("Sync should succeed, got error: %s", result.Error)
			}
		})
	}
}

// TestRunLinearSync_IncrementalSync tests incremental sync using last_sync timestamp.
func TestRunLinearSync_IncrementalSync(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "linear")

	// Set last_sync
	lastSync := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := syncStore.SetConfig(ctx, "linear.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	// Add issues to mock
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "uuid-1",
			Identifier: "LINEAR-1",
			URL:        "https://linear.app/team/issue/LINEAR-1",
			Title:      "Updated Issue",
			Priority:   1,
			State:      "open",
			CreatedAt:  now.Add(-2 * time.Hour),
			UpdatedAt:  now, // Updated after last_sync
		},
	}

	opts := tracker.SyncOptions{
		Pull: true,
		Push: false,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Should have fetched issues
	if !mock.fetchCalled {
		t.Error("Expected FetchIssues to be called")
	}
}

// TestRunLinearSync_StateFilter tests state filtering (open, closed, all).
func TestRunLinearSync_StateFilter(t *testing.T) {
	states := []string{"open", "closed", "all"}

	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			mock, engine, _, ctx := setupTrackerSyncTest(t, "linear")

			now := time.Now()
			mock.issues = []tracker.TrackerIssue{
				{
					ID:         "uuid-1",
					Identifier: "LINEAR-1",
					URL:        "https://linear.app/team/issue/LINEAR-1",
					Title:      "Test Issue",
					Priority:   1,
					State:      "open",
					CreatedAt:  now,
					UpdatedAt:  now,
				},
			}

			opts := tracker.SyncOptions{
				Pull:  true,
				Push:  false,
				State: state,
			}

			result, err := engine.Sync(ctx, opts)
			if err != nil {
				t.Fatalf("Sync failed: %v", err)
			}

			if !result.Success {
				t.Errorf("Sync should succeed with state=%s, got error: %s", state, result.Error)
			}
		})
	}
}

// TestRunLinearSync_UpdateRefs tests that external_ref is updated after creating issues.
func TestRunLinearSync_UpdateRefs(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "linear")

	// Create local issue
	issue := &types.Issue{
		Title:     "Local Issue",
		Priority:  2,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Push with update-refs enabled
	opts := tracker.SyncOptions{
		Pull:       false,
		Push:       true,
		UpdateRefs: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if !mock.createCalled {
		t.Error("Expected CreateIssue to be called")
	}

	// Verify created issue has URL
	if len(mock.createdIssues) != 1 {
		t.Fatalf("Expected 1 created issue, got %d", len(mock.createdIssues))
	}

	if mock.createdIssues[0].URL == "" {
		t.Error("Expected URL to be set on created issue")
	}
}

// TestLinearSyncOptions tests that SyncOptions are correctly applied.
func TestLinearSyncOptions(t *testing.T) {
	// Test default behavior (both pull and push if neither specified)
	opts := tracker.SyncOptions{}

	// When both are false, Sync() defaults to both true
	if opts.Pull && opts.Push {
		t.Error("Initial options should have Pull=false and Push=false")
	}

	// Test explicit options
	opts2 := tracker.SyncOptions{
		Pull:               true,
		Push:               true,
		DryRun:             true,
		CreateOnly:         true,
		UpdateRefs:         true,
		State:              "open",
		ConflictResolution: tracker.ConflictResolutionLocal,
	}

	if !opts2.Pull {
		t.Error("Pull should be true")
	}
	if !opts2.Push {
		t.Error("Push should be true")
	}
	if !opts2.DryRun {
		t.Error("DryRun should be true")
	}
	if !opts2.CreateOnly {
		t.Error("CreateOnly should be true")
	}
	if !opts2.UpdateRefs {
		t.Error("UpdateRefs should be true")
	}
	if opts2.State != "open" {
		t.Errorf("State should be 'open', got %s", opts2.State)
	}
	if opts2.ConflictResolution != tracker.ConflictResolutionLocal {
		t.Errorf("ConflictResolution should be 'local', got %s", opts2.ConflictResolution)
	}
}

// TestLinearTrackerMockImplementation verifies the mock tracker implements the interface correctly.
func TestLinearTrackerMockImplementation(t *testing.T) {
	mock := newMockTracker("linear")

	// Verify interface methods exist
	if mock.Name() != "linear" {
		t.Errorf("Name() = %s, want 'linear'", mock.Name())
	}

	if mock.DisplayName() != "linear" {
		t.Errorf("DisplayName() = %s, want 'linear'", mock.DisplayName())
	}

	if mock.ConfigPrefix() != "linear" {
		t.Errorf("ConfigPrefix() = %s, want 'linear'", mock.ConfigPrefix())
	}

	// Verify Init doesn't error
	ctx := context.Background()
	tmpDir := t.TempDir()
	testStore := newTestStore(t, filepath.Join(tmpDir, "test.db"))
	cfg := tracker.NewConfig(ctx, "linear", newConfigStoreAdapter(testStore))

	if err := mock.Init(ctx, cfg); err != nil {
		t.Errorf("Init() failed: %v", err)
	}

	if err := mock.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}

	if err := mock.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}

	// Verify FieldMapper returns something
	if mock.FieldMapper() == nil {
		t.Error("FieldMapper() returned nil")
	}
}

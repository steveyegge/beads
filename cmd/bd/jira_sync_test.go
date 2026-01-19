package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// TestRunJiraSyncNative_PullOnly tests pull-only sync with Jira tracker.
func TestRunJiraSyncNative_PullOnly(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "jira")

	// Add issues to mock tracker
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:          "10001",
			Identifier:  "PROJ-1",
			URL:         "https://jira.example.com/browse/PROJ-1",
			Title:       "Jira Issue 1",
			Description: "Description 1",
			Priority:    1,
			State:       "In Progress",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "10002",
			Identifier:  "PROJ-2",
			URL:         "https://jira.example.com/browse/PROJ-2",
			Title:       "Jira Issue 2",
			Description: "Description 2",
			Priority:    2,
			State:       "Open",
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

// TestRunJiraSyncNative_PushOnly tests push-only sync with Jira tracker.
func TestRunJiraSyncNative_PushOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

	// Create local issue without external_ref
	issue := &types.Issue{
		Title:       "Local Jira Issue",
		Description: "Created locally",
		Priority:    2,
		Status:      types.StatusOpen,
		IssueType:   types.TypeBug,
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

// TestRunJiraSyncNative_DryRun tests dry run mode with Jira.
func TestRunJiraSyncNative_DryRun(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

	// Add remote issue
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "10001",
			Identifier: "PROJ-1",
			URL:        "https://jira.example.com/browse/PROJ-1",
			Title:      "Remote Jira Issue",
			Priority:   1,
			State:      "Open",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create local issue
	localIssue := &types.Issue{
		Title:     "Local Jira Issue",
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

// TestRunJiraSyncNative_ConflictResolution tests conflict resolution modes.
func TestRunJiraSyncNative_ConflictResolution(t *testing.T) {
	tests := []struct {
		name       string
		resolution tracker.ConflictResolution
	}{
		{"prefer-local", tracker.ConflictResolutionLocal},
		{"prefer-jira", tracker.ConflictResolutionExternal},
		{"timestamp", tracker.ConflictResolutionTimestamp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

			// Create issue with external_ref
			now := time.Now()
			externalRef := "https://jira.example.com/browse/PROJ-1"
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
			if err := syncStore.SetConfig(ctx, "jira.last_sync", lastSync); err != nil {
				t.Fatalf("Failed to set last_sync: %v", err)
			}

			// Add remote issue with different data
			mock.issues = []tracker.TrackerIssue{
				{
					ID:         "10001",
					Identifier: "PROJ-1",
					URL:        "https://jira.example.com/browse/PROJ-1",
					Title:      "Remote Version",
					Priority:   2,
					State:      "Open",
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

// TestRunJiraSyncNative_BidirectionalSync tests pull and push together.
func TestRunJiraSyncNative_BidirectionalSync(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

	// Add remote issue
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "10001",
			Identifier: "PROJ-1",
			URL:        "https://jira.example.com/browse/PROJ-1",
			Title:      "Remote Jira Issue",
			Priority:   1,
			State:      "In Progress",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create local issue
	localIssue := &types.Issue{
		Title:     "Local Jira Issue",
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

// TestRunJiraSyncNative_StateFilter tests state filtering.
func TestRunJiraSyncNative_StateFilter(t *testing.T) {
	states := []string{"open", "closed", "all"}

	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			mock, engine, _, ctx := setupTrackerSyncTest(t, "jira")

			now := time.Now()
			mock.issues = []tracker.TrackerIssue{
				{
					ID:         "10001",
					Identifier: "PROJ-1",
					URL:        "https://jira.example.com/browse/PROJ-1",
					Title:      "Test Issue",
					Priority:   1,
					State:      "Open",
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

// TestRunJiraSyncNative_CreateOnly tests create-only mode.
func TestRunJiraSyncNative_CreateOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

	// Create issue with external_ref (would normally be updated)
	externalRef := "https://jira.example.com/browse/PROJ-1"
	existingIssue := &types.Issue{
		Title:       "Existing Issue",
		Priority:    1,
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		ExternalRef: &externalRef,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, existingIssue, "test"); err != nil {
		t.Fatalf("Failed to create existing issue: %v", err)
	}

	// Create new issue without external_ref
	newIssue := &types.Issue{
		Title:     "New Issue",
		Priority:  2,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, newIssue, "test"); err != nil {
		t.Fatalf("Failed to create new issue: %v", err)
	}

	opts := tracker.SyncOptions{
		Pull:       false,
		Push:       true,
		CreateOnly: true,
		UpdateRefs: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Only the new issue should be created
	if len(mock.createdIssues) != 1 {
		t.Errorf("Expected 1 created issue, got %d", len(mock.createdIssues))
	}

	// No updates in create-only mode
	if len(mock.updatedIssues) != 0 {
		t.Errorf("Expected 0 updated issues in create-only mode, got %d", len(mock.updatedIssues))
	}
}

// TestRunJiraSyncNative_UpdateRefs tests external_ref updates.
func TestRunJiraSyncNative_UpdateRefs(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

	// Create local issue
	issue := &types.Issue{
		Title:     "Local Issue",
		Priority:  2,
		Status:    types.StatusOpen,
		IssueType: types.TypeBug,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
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

	if len(mock.createdIssues) != 1 {
		t.Fatalf("Expected 1 created issue, got %d", len(mock.createdIssues))
	}

	if mock.createdIssues[0].URL == "" {
		t.Error("Expected URL to be set on created issue")
	}
}

// TestJiraSyncOptions tests Jira-specific sync options.
func TestJiraSyncOptions(t *testing.T) {
	opts := tracker.SyncOptions{
		Pull:               true,
		Push:               true,
		DryRun:             false,
		CreateOnly:         false,
		UpdateRefs:         true,
		State:              "all",
		ConflictResolution: tracker.ConflictResolutionExternal,
	}

	if !opts.Pull {
		t.Error("Pull should be true")
	}
	if !opts.Push {
		t.Error("Push should be true")
	}
	if opts.DryRun {
		t.Error("DryRun should be false")
	}
	if opts.CreateOnly {
		t.Error("CreateOnly should be false")
	}
	if !opts.UpdateRefs {
		t.Error("UpdateRefs should be true")
	}
	if opts.State != "all" {
		t.Errorf("State should be 'all', got %s", opts.State)
	}
	if opts.ConflictResolution != tracker.ConflictResolutionExternal {
		t.Errorf("ConflictResolution should be 'external', got %s", opts.ConflictResolution)
	}
}

// TestJiraTrackerMockImplementation verifies the mock tracker works for Jira.
func TestJiraTrackerMockImplementation(t *testing.T) {
	mock := newMockTracker("jira")

	if mock.Name() != "jira" {
		t.Errorf("Name() = %s, want 'jira'", mock.Name())
	}

	if mock.DisplayName() != "jira" {
		t.Errorf("DisplayName() = %s, want 'jira'", mock.DisplayName())
	}

	if mock.ConfigPrefix() != "jira" {
		t.Errorf("ConfigPrefix() = %s, want 'jira'", mock.ConfigPrefix())
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	testStore := newTestStore(t, filepath.Join(tmpDir, "test.db"))
	cfg := tracker.NewConfig(ctx, "jira", newConfigStoreAdapter(testStore))

	if err := mock.Init(ctx, cfg); err != nil {
		t.Errorf("Init() failed: %v", err)
	}

	if err := mock.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

// TestJiraSyncNative_IncrementalSync tests incremental sync.
func TestJiraSyncNative_IncrementalSync(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "jira")

	// Set last_sync
	lastSync := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := syncStore.SetConfig(ctx, "jira.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "10001",
			Identifier: "PROJ-1",
			URL:        "https://jira.example.com/browse/PROJ-1",
			Title:      "Updated Issue",
			Priority:   1,
			State:      "In Progress",
			CreatedAt:  now.Add(-2 * time.Hour),
			UpdatedAt:  now,
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
}

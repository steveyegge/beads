package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// TestRunAzureDevOpsSync_PullOnly tests pull-only sync with Azure DevOps tracker.
func TestRunAzureDevOpsSync_PullOnly(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Add issues to mock tracker
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:          "1",
			Identifier:  "1",
			URL:         "https://dev.azure.com/org/project/_workitems/edit/1",
			Title:       "Azure DevOps Work Item 1",
			Description: "Description 1",
			Priority:    1,
			State:       "Active",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "2",
			Identifier:  "2",
			URL:         "https://dev.azure.com/org/project/_workitems/edit/2",
			Title:       "Azure DevOps Work Item 2",
			Description: "Description 2",
			Priority:    2,
			State:       "New",
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

// TestRunAzureDevOpsSync_PushOnly tests push-only sync with Azure DevOps tracker.
func TestRunAzureDevOpsSync_PushOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Create local issue without external_ref
	issue := &types.Issue{
		Title:       "Local ADO Work Item",
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

// TestRunAzureDevOpsSync_DryRun tests dry run mode with Azure DevOps.
func TestRunAzureDevOpsSync_DryRun(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Add remote issue
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "1",
			Identifier: "1",
			URL:        "https://dev.azure.com/org/project/_workitems/edit/1",
			Title:      "Remote ADO Issue",
			Priority:   1,
			State:      "New",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create local issue
	localIssue := &types.Issue{
		Title:     "Local ADO Issue",
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

// TestRunAzureDevOpsSync_ConflictResolution tests conflict resolution modes.
func TestRunAzureDevOpsSync_ConflictResolution(t *testing.T) {
	tests := []struct {
		name       string
		resolution tracker.ConflictResolution
	}{
		{"prefer-local", tracker.ConflictResolutionLocal},
		{"prefer-ado", tracker.ConflictResolutionExternal},
		{"timestamp", tracker.ConflictResolutionTimestamp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

			// Create issue with external_ref
			now := time.Now()
			externalRef := "https://dev.azure.com/org/project/_workitems/edit/1"
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
			if err := syncStore.SetConfig(ctx, "azuredevops.last_sync", lastSync); err != nil {
				t.Fatalf("Failed to set last_sync: %v", err)
			}

			// Add remote issue with different data
			mock.issues = []tracker.TrackerIssue{
				{
					ID:         "1",
					Identifier: "1",
					URL:        "https://dev.azure.com/org/project/_workitems/edit/1",
					Title:      "Remote Version",
					Priority:   2,
					State:      "Active",
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

// TestRunAzureDevOpsSync_BidirectionalSync tests pull and push together.
func TestRunAzureDevOpsSync_BidirectionalSync(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Add remote issue
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "1",
			Identifier: "1",
			URL:        "https://dev.azure.com/org/project/_workitems/edit/1",
			Title:      "Remote ADO Work Item",
			Priority:   1,
			State:      "Active",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create local issue
	localIssue := &types.Issue{
		Title:     "Local ADO Work Item",
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

// TestRunAzureDevOpsSync_StateFilter tests state filtering.
func TestRunAzureDevOpsSync_StateFilter(t *testing.T) {
	states := []string{"open", "closed", "all"}

	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			mock, engine, _, ctx := setupTrackerSyncTest(t, "azuredevops")

			now := time.Now()
			mock.issues = []tracker.TrackerIssue{
				{
					ID:         "1",
					Identifier: "1",
					URL:        "https://dev.azure.com/org/project/_workitems/edit/1",
					Title:      "Test Work Item",
					Priority:   1,
					State:      "New",
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

// TestRunAzureDevOpsSync_CreateOnly tests create-only mode.
func TestRunAzureDevOpsSync_CreateOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Create issue with external_ref (would normally be updated)
	externalRef := "https://dev.azure.com/org/project/_workitems/edit/1"
	existingIssue := &types.Issue{
		Title:       "Existing Work Item",
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
		Title:     "New Work Item",
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

// TestRunAzureDevOpsSync_UpdateRefs tests external_ref updates.
func TestRunAzureDevOpsSync_UpdateRefs(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Create local issue
	issue := &types.Issue{
		Title:     "Local Work Item",
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

// TestAzureDevOpsSyncOptions tests Azure DevOps-specific sync options.
func TestAzureDevOpsSyncOptions(t *testing.T) {
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

// TestAzureDevOpsTrackerMockImplementation verifies the mock tracker works for Azure DevOps.
func TestAzureDevOpsTrackerMockImplementation(t *testing.T) {
	mock := newMockTracker("azuredevops")

	if mock.Name() != "azuredevops" {
		t.Errorf("Name() = %s, want 'azuredevops'", mock.Name())
	}

	if mock.DisplayName() != "azuredevops" {
		t.Errorf("DisplayName() = %s, want 'azuredevops'", mock.DisplayName())
	}

	if mock.ConfigPrefix() != "azuredevops" {
		t.Errorf("ConfigPrefix() = %s, want 'azuredevops'", mock.ConfigPrefix())
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	testStore := newTestStore(t, filepath.Join(tmpDir, "test.db"))
	cfg := tracker.NewConfig(ctx, "azuredevops", newConfigStoreAdapter(testStore))

	if err := mock.Init(ctx, cfg); err != nil {
		t.Errorf("Init() failed: %v", err)
	}

	if err := mock.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

// TestAzureDevOpsSync_IncrementalSync tests incremental sync.
func TestAzureDevOpsSync_IncrementalSync(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Set last_sync
	lastSync := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := syncStore.SetConfig(ctx, "azuredevops.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "1",
			Identifier: "1",
			URL:        "https://dev.azure.com/org/project/_workitems/edit/1",
			Title:      "Updated Work Item",
			Priority:   1,
			State:      "Active",
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

// TestAzureDevOpsSync_ErrorHandling tests error handling.
func TestAzureDevOpsSync_ErrorHandling(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "azuredevops")

	// Set up mock to return an error
	mock.fetchErr = &tracker.ErrNotInitialized{Tracker: "azuredevops"}

	opts := tracker.SyncOptions{
		Pull: true,
		Push: false,
	}

	result, err := engine.Sync(ctx, opts)

	// Should return error
	if err == nil {
		t.Error("Expected error from sync")
	}

	// Result should indicate failure
	if result.Success {
		t.Error("Expected Success to be false")
	}

	if result.Error == "" {
		t.Error("Expected Error message to be set")
	}
}

// TestAzureDevOpsSync_WorkItemTypes tests handling of different work item types.
func TestAzureDevOpsSync_WorkItemTypes(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "azuredevops")

	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "1",
			Identifier: "1",
			URL:        "https://dev.azure.com/org/project/_workitems/edit/1",
			Title:      "Bug Work Item",
			Priority:   1,
			State:      "New",
			Labels:     []string{"Bug"},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "2",
			Identifier: "2",
			URL:        "https://dev.azure.com/org/project/_workitems/edit/2",
			Title:      "Feature Work Item",
			Priority:   2,
			State:      "Active",
			Labels:     []string{"Feature"},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "3",
			Identifier: "3",
			URL:        "https://dev.azure.com/org/project/_workitems/edit/3",
			Title:      "Task Work Item",
			Priority:   3,
			State:      "New",
			Labels:     []string{"Task"},
			CreatedAt:  now,
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

	if result.Stats.Created != 3 {
		t.Errorf("Expected 3 created issues, got %d", result.Stats.Created)
	}
}

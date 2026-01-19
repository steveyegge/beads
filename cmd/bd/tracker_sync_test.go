package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// mockFieldMapper implements tracker.FieldMapper for testing.
type mockFieldMapper struct{}

func (m *mockFieldMapper) PriorityToBeads(trackerPriority interface{}) int {
	if p, ok := trackerPriority.(int); ok {
		return p
	}
	return 2 // Default medium
}

func (m *mockFieldMapper) PriorityToTracker(beadsPriority int) interface{} {
	return beadsPriority
}

func (m *mockFieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if s, ok := trackerState.(string); ok {
		switch s {
		case "open", "todo", "backlog":
			return types.StatusOpen
		case "in_progress", "started", "active":
			return types.StatusInProgress
		case "closed", "done", "completed":
			return types.StatusClosed
		}
	}
	return types.StatusOpen
}

func (m *mockFieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	switch beadsStatus {
	case types.StatusOpen:
		return "todo"
	case types.StatusInProgress:
		return "in_progress"
	case types.StatusClosed:
		return "done"
	default:
		return "todo"
	}
}

func (m *mockFieldMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	if t, ok := trackerType.(string); ok {
		switch t {
		case "bug":
			return types.TypeBug
		case "feature":
			return types.TypeFeature
		case "epic":
			return types.TypeEpic
		default:
			return types.TypeTask
		}
	}
	return types.TypeTask
}

func (m *mockFieldMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	return string(beadsType)
}

func (m *mockFieldMapper) IssueToBeads(trackerIssue *tracker.TrackerIssue) *tracker.IssueConversion {
	issue := &types.Issue{
		Title:       trackerIssue.Title,
		Description: trackerIssue.Description,
		Priority:    trackerIssue.Priority,
		Status:      m.StatusToBeads(trackerIssue.State),
		IssueType:   types.TypeTask,
		Labels:      trackerIssue.Labels,
		Assignee:    trackerIssue.Assignee,
		CreatedAt:   trackerIssue.CreatedAt,
		UpdatedAt:   trackerIssue.UpdatedAt,
	}

	// Set external_ref
	if trackerIssue.URL != "" {
		issue.ExternalRef = &trackerIssue.URL
	}

	// Handle closed issues
	if trackerIssue.CompletedAt != nil {
		issue.ClosedAt = trackerIssue.CompletedAt
	}

	return &tracker.IssueConversion{
		Issue:        issue,
		Dependencies: nil,
	}
}

func (m *mockFieldMapper) LoadConfig(cfg tracker.ConfigLoader) {}

// mockTracker implements tracker.IssueTracker for testing.
type mockTracker struct {
	name        string
	displayName string
	prefix      string
	issues      []tracker.TrackerIssue
	fetchErr    error
	createErr   error
	updateErr   error

	// Track method calls for assertions
	fetchCalled  bool
	createCalled bool
	updateCalled bool

	// Created issues (for tracking what was pushed)
	createdIssues []tracker.TrackerIssue

	// Updated issues (for tracking what was pushed)
	updatedIssues []tracker.TrackerIssue

	// Config reference for validation
	cfg *tracker.Config

	// Field mapper for conversions
	mapper *mockFieldMapper
}

func newMockTracker(name string) *mockTracker {
	return &mockTracker{
		name:          name,
		displayName:   name,
		prefix:        name,
		issues:        []tracker.TrackerIssue{},
		createdIssues: []tracker.TrackerIssue{},
		updatedIssues: []tracker.TrackerIssue{},
		mapper:        &mockFieldMapper{},
	}
}

func (m *mockTracker) Name() string         { return m.name }
func (m *mockTracker) DisplayName() string  { return m.displayName }
func (m *mockTracker) ConfigPrefix() string { return m.prefix }

func (m *mockTracker) Init(ctx context.Context, cfg *tracker.Config) error {
	m.cfg = cfg
	return nil
}

func (m *mockTracker) Validate() error { return nil }
func (m *mockTracker) Close() error    { return nil }

func (m *mockTracker) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	m.fetchCalled = true
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.issues, nil
}

func (m *mockTracker) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	m.fetchCalled = true
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	for _, issue := range m.issues {
		if issue.Identifier == identifier {
			return &issue, nil
		}
	}
	return nil, nil
}

func (m *mockTracker) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	m.createCalled = true
	if m.createErr != nil {
		return nil, m.createErr
	}

	// Create a tracker issue from the beads issue
	now := time.Now()
	trackerIssue := tracker.TrackerIssue{
		ID:          "ext-" + issue.ID,
		Identifier:  m.prefix + "-" + issue.ID,
		URL:         "https://" + m.name + ".example.com/issue/" + issue.ID,
		Title:       issue.Title,
		Description: issue.Description,
		Priority:    issue.Priority,
		State:       string(issue.Status),
		Labels:      issue.Labels,
		Assignee:    issue.Assignee,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	m.createdIssues = append(m.createdIssues, trackerIssue)
	return &trackerIssue, nil
}

func (m *mockTracker) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	m.updateCalled = true
	if m.updateErr != nil {
		return nil, m.updateErr
	}

	// Find and update the issue
	for i, ti := range m.issues {
		if ti.ID == externalID {
			m.issues[i].Title = issue.Title
			m.issues[i].Description = issue.Description
			m.issues[i].Priority = issue.Priority
			m.issues[i].State = string(issue.Status)
			m.issues[i].UpdatedAt = time.Now()
			m.updatedIssues = append(m.updatedIssues, m.issues[i])
			return &m.issues[i], nil
		}
	}

	// Issue not found, create a new tracker issue
	trackerIssue := tracker.TrackerIssue{
		ID:          externalID,
		Identifier:  m.prefix + "-" + externalID,
		URL:         "https://" + m.name + ".example.com/issue/" + externalID,
		Title:       issue.Title,
		Description: issue.Description,
		Priority:    issue.Priority,
		State:       string(issue.Status),
		UpdatedAt:   time.Now(),
	}
	m.updatedIssues = append(m.updatedIssues, trackerIssue)
	return &trackerIssue, nil
}

func (m *mockTracker) FieldMapper() tracker.FieldMapper {
	return m.mapper
}

func (m *mockTracker) IsExternalRef(ref string) bool {
	prefix := "https://" + m.name
	return ref != "" && len(ref) > len(prefix) && ref[:len(prefix)] == prefix
}

func (m *mockTracker) ExtractIdentifier(ref string) string {
	// Simple extraction: last part of the URL
	if ref == "" {
		return ""
	}
	return filepath.Base(ref)
}

func (m *mockTracker) BuildExternalRef(issue *tracker.TrackerIssue) string {
	return issue.URL
}

func (m *mockTracker) CanonicalizeRef(ref string) string {
	return ref
}

// setupTrackerSyncTest creates a test environment with mock tracker and SyncEngine.
func setupTrackerSyncTest(t *testing.T, trackerName string) (*mockTracker, *tracker.SyncEngine, *syncStoreAdapter, context.Context) {
	t.Helper()

	// Create temp directory for test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create test store
	testStore := newTestStore(t, dbPath)

	// Create mock tracker
	mock := newMockTracker(trackerName)

	// Create config
	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, trackerName, newConfigStoreAdapter(testStore))

	// Initialize mock tracker
	if err := mock.Init(ctx, cfg); err != nil {
		t.Fatalf("Failed to init mock tracker: %v", err)
	}

	// Create sync store adapter
	syncStore := newSyncStoreAdapter(testStore)

	// Create sync engine
	engine := tracker.NewSyncEngine(mock, cfg, syncStore, "test-actor")

	// Capture messages for debugging (optional)
	engine.OnMessage = func(msg string) {}
	engine.OnWarning = func(msg string) {}

	return mock, engine, syncStore, ctx
}

// TestSyncEngine_PullOnly tests that pull-only sync only fetches issues.
func TestSyncEngine_PullOnly(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "test")

	// Add some issues to the mock tracker
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:          "ext-1",
			Identifier:  "TEST-1",
			URL:         "https://test.example.com/issue/TEST-1",
			Title:       "Test Issue 1",
			Description: "First test issue",
			Priority:    2,
			State:       "open",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "ext-2",
			Identifier:  "TEST-2",
			URL:         "https://test.example.com/issue/TEST-2",
			Title:       "Test Issue 2",
			Description: "Second test issue",
			Priority:    1,
			State:       "in_progress",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	// Execute pull-only sync
	opts := tracker.SyncOptions{
		Pull: true,
		Push: false,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if !mock.fetchCalled {
		t.Error("Expected FetchIssues to be called")
	}

	if mock.createCalled {
		t.Error("CreateIssue should not be called during pull-only")
	}

	// Verify stats
	if result.Stats.Created != 2 {
		t.Errorf("Expected 2 created issues, got %d", result.Stats.Created)
	}
}

// TestSyncEngine_PushOnly tests that push-only sync only creates issues.
func TestSyncEngine_PushOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Create some local issues without external_ref
	issue1 := &types.Issue{
		Title:       "Local Issue 1",
		Description: "First local issue",
		Priority:    2,
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Execute push-only sync
	opts := tracker.SyncOptions{
		Pull:       false,
		Push:       true,
		UpdateRefs: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if mock.fetchCalled {
		t.Error("FetchIssues should not be called during push-only")
	}

	if !mock.createCalled {
		t.Error("Expected CreateIssue to be called")
	}

	// Verify created issues
	if len(mock.createdIssues) != 1 {
		t.Errorf("Expected 1 created issue in tracker, got %d", len(mock.createdIssues))
	}

	// Verify stats
	if result.Stats.Created != 1 {
		t.Errorf("Expected 1 created issue, got %d", result.Stats.Created)
	}
}

// TestSyncEngine_DryRun tests that dry run doesn't make changes.
func TestSyncEngine_DryRun(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Add issues to mock tracker
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "ext-1",
			Identifier: "TEST-1",
			URL:        "https://test.example.com/issue/TEST-1",
			Title:      "Test Issue 1",
			Priority:   2,
			State:      "open",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create a local issue to push
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

	// Execute dry run sync
	opts := tracker.SyncOptions{
		Pull:   true,
		Push:   true,
		DryRun: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Fetch was called (to get issues for dry run preview)
	if !mock.fetchCalled {
		t.Error("Expected FetchIssues to be called even in dry run")
	}

	// But no actual creation should happen
	if len(mock.createdIssues) != 0 {
		t.Errorf("Expected 0 created issues in dry run, got %d", len(mock.createdIssues))
	}
}

// TestSyncEngine_BidirectionalSync tests pull and push together.
func TestSyncEngine_BidirectionalSync(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Add an issue to the mock tracker for pulling
	now := time.Now()
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "ext-1",
			Identifier: "TEST-1",
			URL:        "https://test.example.com/issue/TEST-1",
			Title:      "Remote Issue",
			Priority:   1,
			State:      "open",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	// Create a local issue without external_ref for pushing
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

	// Execute bidirectional sync
	opts := tracker.SyncOptions{
		Pull:       true,
		Push:       true,
		UpdateRefs: true,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Both should be called
	if !mock.fetchCalled {
		t.Error("Expected FetchIssues to be called")
	}

	if !mock.createCalled {
		t.Error("Expected CreateIssue to be called")
	}
}

// TestSyncEngine_CreateOnly tests that create-only doesn't update existing issues.
func TestSyncEngine_CreateOnly(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Create a local issue with external_ref (would normally be updated)
	externalRef := "https://test.example.com/issue/TEST-1"
	localIssue := &types.Issue{
		Title:       "Modified Local Issue",
		Priority:    1,
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		ExternalRef: &externalRef,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Add another local issue without external_ref (should be created)
	newIssue := &types.Issue{
		Title:     "New Local Issue",
		Priority:  2,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := syncStore.CreateIssue(ctx, newIssue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Execute push with create-only
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

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Only one issue should be created (the one without external_ref)
	if len(mock.createdIssues) != 1 {
		t.Errorf("Expected 1 created issue, got %d", len(mock.createdIssues))
	}

	// No updates should happen in create-only mode
	if len(mock.updatedIssues) != 0 {
		t.Errorf("Expected 0 updated issues in create-only mode, got %d", len(mock.updatedIssues))
	}
}

// TestSyncEngine_LastSyncTimestamp tests that last_sync is recorded.
func TestSyncEngine_LastSyncTimestamp(t *testing.T) {
	_, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Execute sync
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

	// Verify last_sync was set
	if result.LastSync == "" {
		t.Error("Expected LastSync to be set")
	}

	// Verify it's stored in config
	stored, err := syncStore.GetConfig(ctx, "test.last_sync")
	if err != nil {
		t.Fatalf("Failed to get last_sync: %v", err)
	}

	if stored == "" {
		t.Error("Expected last_sync to be stored in config")
	}
}

// TestSyncEngine_ErrorHandling tests error handling during sync.
func TestSyncEngine_ErrorHandling(t *testing.T) {
	mock, engine, _, ctx := setupTrackerSyncTest(t, "test")

	// Set up mock to return an error
	mock.fetchErr = &tracker.ErrNotInitialized{Tracker: "test"}

	// Execute sync
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

// TestSyncEngine_ConflictResolutionLocal tests prefer-local conflict resolution.
func TestSyncEngine_ConflictResolutionLocal(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Create a local issue with external_ref
	now := time.Now()
	localUpdated := now.Add(-1 * time.Hour)
	externalRef := "https://test.example.com/issue/TEST-1"
	localIssue := &types.Issue{
		Title:       "Local Version",
		Priority:    1,
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		ExternalRef: &externalRef,
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   localUpdated,
	}
	if err := syncStore.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Set last_sync to before both updates
	lastSync := now.Add(-3 * time.Hour).Format(time.RFC3339)
	if err := syncStore.SetConfig(ctx, "test.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	// Add corresponding issue to mock tracker with different data
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "ext-1",
			Identifier: "TEST-1",
			URL:        "https://test.example.com/issue/TEST-1",
			Title:      "Remote Version",
			Priority:   2,
			State:      "open",
			CreatedAt:  now.Add(-2 * time.Hour),
			UpdatedAt:  now, // Remote is newer
		},
	}

	// Execute sync with prefer-local
	opts := tracker.SyncOptions{
		Pull:               true,
		Push:               true,
		ConflictResolution: tracker.ConflictResolutionLocal,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}
}

// TestSyncEngine_ConflictResolutionExternal tests prefer-external conflict resolution.
func TestSyncEngine_ConflictResolutionExternal(t *testing.T) {
	mock, engine, syncStore, ctx := setupTrackerSyncTest(t, "test")

	// Create a local issue with external_ref
	now := time.Now()
	localUpdated := now.Add(-1 * time.Hour)
	externalRef := "https://test.example.com/issue/TEST-1"
	localIssue := &types.Issue{
		Title:       "Local Version",
		Priority:    1,
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		ExternalRef: &externalRef,
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   localUpdated,
	}
	if err := syncStore.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Set last_sync to before both updates
	lastSync := now.Add(-3 * time.Hour).Format(time.RFC3339)
	if err := syncStore.SetConfig(ctx, "test.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	// Add corresponding issue to mock tracker with different data
	mock.issues = []tracker.TrackerIssue{
		{
			ID:         "ext-1",
			Identifier: "TEST-1",
			URL:        "https://test.example.com/issue/TEST-1",
			Title:      "Remote Version",
			Priority:   2,
			State:      "open",
			CreatedAt:  now.Add(-2 * time.Hour),
			UpdatedAt:  now, // Remote is newer
		},
	}

	// Execute sync with prefer-external
	opts := tracker.SyncOptions{
		Pull:               true,
		Push:               true,
		ConflictResolution: tracker.ConflictResolutionExternal,
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify results
	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}
}

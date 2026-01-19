//go:build integration

package tracker_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/tracker/azuredevops"
	"github.com/steveyegge/beads/internal/tracker/jira"
	"github.com/steveyegge/beads/internal/tracker/testutil"
	"github.com/steveyegge/beads/internal/types"
)

// syncTestStore is an in-memory implementation of tracker.SyncStore for testing.
type syncTestStore struct {
	mu         sync.RWMutex
	issues     map[string]*types.Issue
	config     map[string]string
	deps       map[string][]*types.Dependency
	nextID     int
}

func newSyncTestStore(t *testing.T) (*syncTestStore, func()) {
	t.Helper()
	return &syncTestStore{
		issues:     make(map[string]*types.Issue),
		config:     make(map[string]string),
		deps:       make(map[string][]*types.Dependency),
		nextID:     1,
	}, func() {}
}

func (s *syncTestStore) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	issue, ok := s.issues[id]
	if !ok {
		return nil, nil
	}
	return issue, nil
}

func (s *syncTestStore) GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, issue := range s.issues {
		if issue.ExternalRef != nil && *issue.ExternalRef == externalRef {
			return issue, nil
		}
	}
	return nil, nil
}

func (s *syncTestStore) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*types.Issue
	for _, issue := range s.issues {
		result = append(result, issue)
	}
	return result, nil
}

func (s *syncTestStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if issue.ID == "" {
		issue.ID = stringFromID(s.nextID)
		s.nextID++
	}
	s.issues[issue.ID] = issue
	return nil
}

func (s *syncTestStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	issue, ok := s.issues[id]
	if !ok {
		return nil
	}
	// Apply updates
	if title, ok := updates["title"].(string); ok {
		issue.Title = title
	}
	if desc, ok := updates["description"].(string); ok {
		issue.Description = desc
	}
	if status, ok := updates["status"].(string); ok {
		issue.Status = types.Status(status)
	}
	if priority, ok := updates["priority"].(int); ok {
		issue.Priority = priority
	}
	if ref, ok := updates["external_ref"].(string); ok {
		issue.ExternalRef = &ref
	}
	issue.UpdatedAt = time.Now()
	return nil
}

func (s *syncTestStore) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deps[dep.IssueID] = append(s.deps[dep.IssueID], dep)
	return nil
}

func (s *syncTestStore) GetConfig(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config[key], nil
}

func (s *syncTestStore) SetConfig(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config[key] = value
	return nil
}

func (s *syncTestStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range s.config {
		result[k] = v
	}
	return result, nil
}

// TestE2E_Sync_JiraPullEmpty tests pulling from Jira when no issues exist.
func TestE2E_Sync_JiraPullEmpty(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	// Create Jira plugin tracker
	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if result.Stats.Created != 0 {
		t.Errorf("Expected 0 created, got %d", result.Stats.Created)
	}
}

// TestE2E_Sync_JiraPullWithData tests pulling issues from Jira.
func TestE2E_Sync_JiraPullWithData(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "First Issue", "To Do"),
		testutil.MakeJiraIssue("PROJ-2", "Second Issue", "In Progress"),
	})

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if result.Stats.Created != 2 {
		t.Errorf("Expected 2 created, got %d", result.Stats.Created)
	}

	// Verify issues were created in local store
	issues, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	if len(issues) != 2 {
		t.Errorf("Expected 2 issues in store, got %d", len(issues))
	}
}

// TestE2E_Sync_AzureDevOpsPullEmpty tests pulling from Azure DevOps when no work items exist.
func TestE2E_Sync_AzureDevOpsPullEmpty(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	adoClient := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	adoTracker := &adoTrackerAdapter{client: adoClient}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "azuredevops", store)

	engine := tracker.NewSyncEngine(adoTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if result.Stats.Created != 0 {
		t.Errorf("Expected 0 created, got %d", result.Stats.Created)
	}
}

// TestE2E_Sync_AzureDevOpsPullWithData tests pulling work items from Azure DevOps.
func TestE2E_Sync_AzureDevOpsPullWithData(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "First Work Item", "New"),
		testutil.MakeADOWorkItem(2, "Second Work Item", "Active"),
	})

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	adoClient := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	adoTracker := &adoTrackerAdapter{client: adoClient, baseURL: mock.URL()}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "azuredevops", store)

	engine := tracker.NewSyncEngine(adoTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if result.Stats.Created != 2 {
		t.Errorf("Expected 2 created, got %d", result.Stats.Created)
	}

	// Verify issues were created in local store
	issues, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	if len(issues) != 2 {
		t.Errorf("Expected 2 issues in store, got %d", len(issues))
	}
}

// TestE2E_Sync_PushNew tests pushing new local issues to tracker.
func TestE2E_Sync_PushNew(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Configure create response
	createdIssue := testutil.MakeJiraIssue("PROJ-100", "Pushed Issue", "To Do")
	mock.SetCreateIssueResponse(&createdIssue)
	mock.AddIssue(createdIssue)

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	// Create a local issue without external_ref
	ctx := context.Background()
	localIssue := &types.Issue{
		Title:     "Local Issue",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create local issue: %v", err)
	}

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: false, Push: true, UpdateRefs: true}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	if result.Stats.Created != 1 {
		t.Errorf("Expected 1 created, got %d", result.Stats.Created)
	}
}

// TestE2E_Sync_BidirectionalFull tests full bidirectional sync.
func TestE2E_Sync_BidirectionalFull(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	// Set up remote issues to pull
	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "Remote Issue 1", "To Do"),
	})

	// Configure create response for push
	createdIssue := testutil.MakeJiraIssue("PROJ-100", "Pushed Issue", "To Do")
	mock.SetCreateIssueResponse(&createdIssue)
	// Also add to issues list so FetchIssue can find it after creation
	mock.AddIssue(createdIssue)

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	// Create a local issue to push
	ctx := context.Background()
	localIssue := &types.Issue{
		Title:     "Local Issue",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create local issue: %v", err)
	}

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: true, UpdateRefs: true}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Should have pulled (1 original + 1 pre-added for CreateIssue FetchIssue) and pushed 1
	if result.Stats.Pulled != 2 {
		t.Errorf("Expected 2 pulled, got %d", result.Stats.Pulled)
	}

	if result.Stats.Pushed != 1 {
		t.Errorf("Expected 1 pushed, got %d", result.Stats.Pushed)
	}
}

// TestE2E_Sync_DryRun tests dry run mode.
func TestE2E_Sync_DryRun(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetIssues([]jira.Issue{
		testutil.MakeJiraIssue("PROJ-1", "Remote Issue", "To Do"),
	})

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false, DryRun: true}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}

	// Verify no issues were actually created
	issues, _ := store.SearchIssues(ctx, "", types.IssueFilter{})
	if len(issues) != 0 {
		t.Errorf("Expected 0 issues in store during dry run, got %d", len(issues))
	}
}

// TestE2E_Sync_AuthError tests sync failure with auth error.
func TestE2E_Sync_AuthError(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetAuthError(true)

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "invalid-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false}
	result, err := engine.Sync(ctx, opts)

	if err == nil {
		t.Fatal("Expected sync to fail with auth error")
	}

	if result.Success {
		t.Error("Expected result.Success to be false")
	}
}

// TestE2E_Sync_LastSyncTimestamp tests that last_sync is recorded.
func TestE2E_Sync_LastSyncTimestamp(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	mock.SetIssues([]jira.Issue{})

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	ctx := context.Background()
	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{Pull: true, Push: false}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if result.LastSync == "" {
		t.Error("Expected LastSync to be set")
	}

	// Verify it's stored in config
	stored, _ := store.GetConfig(ctx, "jira.last_sync")
	if stored == "" {
		t.Error("Expected last_sync to be stored")
	}
}

// TestE2E_Sync_ConflictResolutionLocal tests prefer-local conflict resolution.
func TestE2E_Sync_ConflictResolutionLocal(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a local issue with external_ref
	now := time.Now()
	localUpdated := now.Add(-1 * time.Hour)
	externalRef := mock.URL() + "/browse/PROJ-1"
	localIssue := &types.Issue{
		Title:       "Local Version",
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		Priority:    1,
		ExternalRef: &externalRef,
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   localUpdated,
	}
	if err := store.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create local issue: %v", err)
	}

	// Set last_sync to before both updates
	lastSync := now.Add(-3 * time.Hour).Format(time.RFC3339)
	if err := store.SetConfig(ctx, "jira.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	// Set up remote issue with different data (newer)
	remoteIssue := testutil.MakeJiraIssue("PROJ-1", "Remote Version", "To Do")
	mock.SetIssues([]jira.Issue{remoteIssue})

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{
		Pull:               true,
		Push:               true,
		ConflictResolution: tracker.ConflictResolutionLocal,
	}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}
}

// TestE2E_Sync_ConflictResolutionExternal tests prefer-external conflict resolution.
func TestE2E_Sync_ConflictResolutionExternal(t *testing.T) {
	mock := testutil.NewJiraMockServer()
	defer mock.Close()

	store, cleanup := newSyncTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a local issue with external_ref
	now := time.Now()
	localUpdated := now.Add(-1 * time.Hour)
	externalRef := mock.URL() + "/browse/PROJ-1"
	localIssue := &types.Issue{
		Title:       "Local Version",
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		Priority:    1,
		ExternalRef: &externalRef,
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   localUpdated,
	}
	if err := store.CreateIssue(ctx, localIssue, "test"); err != nil {
		t.Fatalf("Failed to create local issue: %v", err)
	}

	// Set last_sync to before both updates
	lastSync := now.Add(-3 * time.Hour).Format(time.RFC3339)
	if err := store.SetConfig(ctx, "jira.last_sync", lastSync); err != nil {
		t.Fatalf("Failed to set last_sync: %v", err)
	}

	// Set up remote issue with different data
	remoteIssue := testutil.MakeJiraIssue("PROJ-1", "Remote Version", "In Progress")
	mock.SetIssues([]jira.Issue{remoteIssue})

	jiraClient := jira.NewClient("https://example.atlassian.net", "PROJ", "user@example.com", "test-token").
		WithEndpoint(mock.URL())

	jiraTracker := &jiraTrackerAdapter{client: jiraClient, baseURL: mock.URL()}

	cfg := tracker.NewConfig(ctx, "jira", store)

	engine := tracker.NewSyncEngine(jiraTracker, cfg, store, "test-actor")

	opts := tracker.SyncOptions{
		Pull:               true,
		Push:               true,
		ConflictResolution: tracker.ConflictResolutionExternal,
	}
	result, err := engine.Sync(ctx, opts)

	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Sync should succeed, got error: %s", result.Error)
	}
}

// jiraTrackerAdapter adapts the Jira client to the IssueTracker interface.
type jiraTrackerAdapter struct {
	client  *jira.Client
	baseURL string
}

func (a *jiraTrackerAdapter) Name() string        { return "jira" }
func (a *jiraTrackerAdapter) DisplayName() string { return "Jira" }
func (a *jiraTrackerAdapter) ConfigPrefix() string { return "jira" }

func (a *jiraTrackerAdapter) Init(ctx context.Context, cfg *tracker.Config) error {
	return nil
}

func (a *jiraTrackerAdapter) Validate() error { return nil }
func (a *jiraTrackerAdapter) Close() error    { return nil }

func (a *jiraTrackerAdapter) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	jiraIssues, err := a.client.FetchIssues(ctx, opts.State, opts.Since)
	if err != nil {
		return nil, err
	}

	result := make([]tracker.TrackerIssue, len(jiraIssues))
	for i, ji := range jiraIssues {
		result[i] = a.toTrackerIssue(&ji)
	}
	return result, nil
}

func (a *jiraTrackerAdapter) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	ji, err := a.client.FetchIssue(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if ji == nil {
		return nil, nil
	}
	ti := a.toTrackerIssue(ji)
	return &ti, nil
}

func (a *jiraTrackerAdapter) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	ji, err := a.client.CreateIssue(ctx, issue.Title, issue.Description, "Task", "", issue.Labels)
	if err != nil {
		return nil, err
	}
	if ji == nil {
		return nil, nil
	}
	ti := a.toTrackerIssue(ji)
	return &ti, nil
}

func (a *jiraTrackerAdapter) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	updates := map[string]interface{}{
		"summary":     issue.Title,
		"description": issue.Description,
	}
	if err := a.client.UpdateIssue(ctx, externalID, updates); err != nil {
		return nil, err
	}
	// Fetch updated issue
	return a.FetchIssue(ctx, externalID)
}

func (a *jiraTrackerAdapter) FieldMapper() tracker.FieldMapper {
	return &jiraFieldMapper{}
}

func (a *jiraTrackerAdapter) IsExternalRef(ref string) bool {
	return ref != "" && (len(ref) > len(a.baseURL) && ref[:len(a.baseURL)] == a.baseURL)
}

func (a *jiraTrackerAdapter) ExtractIdentifier(ref string) string {
	// Simple extraction: assume format is baseURL/browse/KEY
	if len(ref) > len(a.baseURL)+8 {
		return ref[len(a.baseURL)+8:] // "/browse/"
	}
	return ""
}

func (a *jiraTrackerAdapter) BuildExternalRef(issue *tracker.TrackerIssue) string {
	return issue.URL
}

func (a *jiraTrackerAdapter) CanonicalizeRef(ref string) string {
	return ref
}

func (a *jiraTrackerAdapter) toTrackerIssue(ji *jira.Issue) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:         ji.ID,
		Identifier: ji.Key,
		URL:        a.baseURL + "/browse/" + ji.Key,
		Title:      ji.Fields.Summary,
		Labels:     ji.Fields.Labels,
	}

	if ji.Fields.Description != nil {
		if s, ok := ji.Fields.Description.(string); ok {
			ti.Description = s
		}
	}

	if ji.Fields.Status != nil {
		ti.State = ji.Fields.Status.Name
	}

	if ji.Fields.Priority != nil {
		ti.Priority = 2 // Default medium
	}

	if ji.Fields.Assignee != nil {
		ti.Assignee = ji.Fields.Assignee.EmailAddress
	}

	// Parse times
	if ji.Fields.Created != "" {
		if t, err := time.Parse("2006-01-02T15:04:05.000-0700", ji.Fields.Created); err == nil {
			ti.CreatedAt = t
		}
	}
	if ji.Fields.Updated != "" {
		if t, err := time.Parse("2006-01-02T15:04:05.000-0700", ji.Fields.Updated); err == nil {
			ti.UpdatedAt = t
		}
	}

	return ti
}

// jiraFieldMapper implements FieldMapper for Jira.
type jiraFieldMapper struct{}

func (m *jiraFieldMapper) PriorityToBeads(trackerPriority interface{}) int { return 2 }
func (m *jiraFieldMapper) PriorityToTracker(beadsPriority int) interface{} { return "Medium" }

func (m *jiraFieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if s, ok := trackerState.(string); ok {
		switch s {
		case "To Do", "Open", "Backlog":
			return types.StatusOpen
		case "In Progress":
			return types.StatusInProgress
		case "Done", "Closed":
			return types.StatusClosed
		}
	}
	return types.StatusOpen
}

func (m *jiraFieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	return string(beadsStatus)
}

func (m *jiraFieldMapper) TypeToBeads(trackerType interface{}) types.IssueType { return types.TypeTask }
func (m *jiraFieldMapper) TypeToTracker(beadsType types.IssueType) interface{} { return string(beadsType) }

func (m *jiraFieldMapper) IssueToBeads(trackerIssue *tracker.TrackerIssue) *tracker.IssueConversion {
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

	if trackerIssue.URL != "" {
		issue.ExternalRef = &trackerIssue.URL
	}

	return &tracker.IssueConversion{
		Issue:        issue,
		Dependencies: nil,
	}
}

func (m *jiraFieldMapper) LoadConfig(cfg tracker.ConfigLoader) {}

// adoTrackerAdapter adapts the Azure DevOps client to the IssueTracker interface.
type adoTrackerAdapter struct {
	client  *azuredevops.Client
	baseURL string
}

func (a *adoTrackerAdapter) Name() string        { return "azuredevops" }
func (a *adoTrackerAdapter) DisplayName() string { return "Azure DevOps" }
func (a *adoTrackerAdapter) ConfigPrefix() string { return "azuredevops" }

func (a *adoTrackerAdapter) Init(ctx context.Context, cfg *tracker.Config) error {
	return nil
}

func (a *adoTrackerAdapter) Validate() error { return nil }
func (a *adoTrackerAdapter) Close() error    { return nil }

func (a *adoTrackerAdapter) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	adoItems, err := a.client.FetchWorkItems(ctx, opts.State, opts.Since)
	if err != nil {
		return nil, err
	}

	result := make([]tracker.TrackerIssue, len(adoItems))
	for i, wi := range adoItems {
		result[i] = a.toTrackerIssue(&wi)
	}
	return result, nil
}

func (a *adoTrackerAdapter) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	var id int
	_, _ = idFromString(identifier, &id)
	wi, err := a.client.FetchWorkItem(ctx, id)
	if err != nil {
		return nil, err
	}
	if wi == nil {
		return nil, nil
	}
	ti := a.toTrackerIssue(wi)
	return &ti, nil
}

func (a *adoTrackerAdapter) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	wi, err := a.client.CreateWorkItem(ctx, "Task", issue.Title, issue.Description, issue.Priority, issue.Labels)
	if err != nil {
		return nil, err
	}
	ti := a.toTrackerIssue(wi)
	return &ti, nil
}

func (a *adoTrackerAdapter) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	var id int
	_, _ = idFromString(externalID, &id)
	ops := []azuredevops.PatchOperation{
		{Op: "replace", Path: "/fields/System.Title", Value: issue.Title},
	}
	wi, err := a.client.UpdateWorkItem(ctx, id, ops)
	if err != nil {
		return nil, err
	}
	ti := a.toTrackerIssue(wi)
	return &ti, nil
}

func (a *adoTrackerAdapter) FieldMapper() tracker.FieldMapper {
	return &adoFieldMapper{}
}

func (a *adoTrackerAdapter) IsExternalRef(ref string) bool {
	return ref != "" && (len(ref) > len(a.baseURL) && ref[:len(a.baseURL)] == a.baseURL)
}

func (a *adoTrackerAdapter) ExtractIdentifier(ref string) string {
	// Simple extraction
	if id, ok := azuredevops.ParseWorkItemID(ref); ok {
		return stringFromID(id)
	}
	return ""
}

func (a *adoTrackerAdapter) BuildExternalRef(issue *tracker.TrackerIssue) string {
	return issue.URL
}

func (a *adoTrackerAdapter) CanonicalizeRef(ref string) string {
	return ref
}

func (a *adoTrackerAdapter) toTrackerIssue(wi *azuredevops.WorkItem) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:          stringFromID(wi.ID),
		Identifier:  stringFromID(wi.ID),
		URL:         a.baseURL + "/testproj/_workitems/edit/" + stringFromID(wi.ID),
		Title:       wi.Fields.Title,
		Description: wi.Fields.Description,
		State:       wi.Fields.State,
		Priority:    wi.Fields.Priority,
	}

	if wi.Fields.AssignedTo != nil {
		ti.Assignee = wi.Fields.AssignedTo.UniqueName
	}

	// Parse times
	if wi.Fields.CreatedDate != "" {
		if t, err := time.Parse(time.RFC3339, wi.Fields.CreatedDate); err == nil {
			ti.CreatedAt = t
		}
	}
	if wi.Fields.ChangedDate != "" {
		if t, err := time.Parse(time.RFC3339, wi.Fields.ChangedDate); err == nil {
			ti.UpdatedAt = t
		}
	}

	return ti
}

// adoFieldMapper implements FieldMapper for Azure DevOps.
type adoFieldMapper struct{}

func (m *adoFieldMapper) PriorityToBeads(trackerPriority interface{}) int { return 2 }
func (m *adoFieldMapper) PriorityToTracker(beadsPriority int) interface{} { return 2 }

func (m *adoFieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if s, ok := trackerState.(string); ok {
		switch s {
		case "New", "To Do":
			return types.StatusOpen
		case "Active", "In Progress":
			return types.StatusInProgress
		case "Closed", "Done":
			return types.StatusClosed
		}
	}
	return types.StatusOpen
}

func (m *adoFieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	return string(beadsStatus)
}

func (m *adoFieldMapper) TypeToBeads(trackerType interface{}) types.IssueType { return types.TypeTask }
func (m *adoFieldMapper) TypeToTracker(beadsType types.IssueType) interface{} { return string(beadsType) }

func (m *adoFieldMapper) IssueToBeads(trackerIssue *tracker.TrackerIssue) *tracker.IssueConversion {
	issue := &types.Issue{
		Title:       trackerIssue.Title,
		Description: trackerIssue.Description,
		Priority:    trackerIssue.Priority,
		Status:      m.StatusToBeads(trackerIssue.State),
		IssueType:   types.TypeTask,
		Assignee:    trackerIssue.Assignee,
		CreatedAt:   trackerIssue.CreatedAt,
		UpdatedAt:   trackerIssue.UpdatedAt,
	}

	if trackerIssue.URL != "" {
		issue.ExternalRef = &trackerIssue.URL
	}

	return &tracker.IssueConversion{
		Issue:        issue,
		Dependencies: nil,
	}
}

func (m *adoFieldMapper) LoadConfig(cfg tracker.ConfigLoader) {}

// Helper functions

func idFromString(s string, id *int) (int, error) {
	var n int
	_, err := stringToInt(s, &n)
	if err == nil {
		*id = n
	}
	return n, err
}

func stringToInt(s string, id *int) (int, error) {
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
	}
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	*id = n
	return n, nil
}

func stringFromID(id int) string {
	if id == 0 {
		return "0"
	}
	s := ""
	for id > 0 {
		s = string(rune('0'+id%10)) + s
		id /= 10
	}
	return s
}

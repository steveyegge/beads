package tracker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

// mockTracker implements IssueTracker for testing.
type mockTracker struct {
	name        string
	issues      []TrackerIssue
	created     []*types.Issue
	updated     map[string]*types.Issue
	fetchErr    error
	createErr   error
	updateErr   error
	fieldMapper FieldMapper
}

func newMockTracker(name string) *mockTracker {
	return &mockTracker{
		name:        name,
		updated:     make(map[string]*types.Issue),
		fieldMapper: &mockMapper{},
	}
}

func (m *mockTracker) Name() string                                        { return m.name }
func (m *mockTracker) DisplayName() string                                 { return m.name }
func (m *mockTracker) ConfigPrefix() string                                { return m.name }
func (m *mockTracker) Init(_ context.Context, _ storage.Storage) error     { return nil }
func (m *mockTracker) Validate() error                                     { return nil }
func (m *mockTracker) Close() error                                        { return nil }
func (m *mockTracker) FieldMapper() FieldMapper                       { return m.fieldMapper }
func (m *mockTracker) IsExternalRef(ref string) bool                  { return len(ref) > 0 }
func (m *mockTracker) ExtractIdentifier(ref string) string {
	// Extract "EXT-1" from "https://test.test/EXT-1"
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}
func (m *mockTracker) BuildExternalRef(issue *TrackerIssue) string {
	return fmt.Sprintf("https://%s.test/%s", m.name, issue.Identifier)
}

func (m *mockTracker) FetchIssues(_ context.Context, _ FetchOptions) ([]TrackerIssue, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.issues, nil
}

func (m *mockTracker) FetchIssue(_ context.Context, identifier string) (*TrackerIssue, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	for i := range m.issues {
		if m.issues[i].Identifier == identifier {
			return &m.issues[i], nil
		}
	}
	return nil, nil
}

func (m *mockTracker) CreateIssue(_ context.Context, issue *types.Issue) (*TrackerIssue, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.created = append(m.created, issue)
	return &TrackerIssue{
		ID:         "ext-" + issue.ID,
		Identifier: "EXT-" + issue.ID,
		URL:        fmt.Sprintf("https://%s.test/EXT-%s", m.name, issue.ID),
		Title:      issue.Title,
	}, nil
}

func (m *mockTracker) UpdateIssue(_ context.Context, externalID string, issue *types.Issue) (*TrackerIssue, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	m.updated[externalID] = issue
	return &TrackerIssue{
		ID:         externalID,
		Identifier: externalID,
		Title:      issue.Title,
	}, nil
}

// mockMapper implements FieldMapper for testing.
type mockMapper struct{}

func (m *mockMapper) PriorityToBeads(p interface{}) int {
	if v, ok := p.(int); ok {
		return v
	}
	return 2
}
func (m *mockMapper) PriorityToTracker(p int) interface{}             { return p }
func (m *mockMapper) StatusToBeads(_ interface{}) types.Status        { return types.StatusOpen }
func (m *mockMapper) StatusToTracker(s types.Status) interface{}      { return string(s) }
func (m *mockMapper) TypeToBeads(_ interface{}) types.IssueType       { return types.TypeTask }
func (m *mockMapper) TypeToTracker(t types.IssueType) interface{}     { return string(t) }
func (m *mockMapper) IssueToTracker(issue *types.Issue) map[string]interface{} {
	return map[string]interface{}{
		"title":       issue.Title,
		"description": issue.Description,
	}
}

func (m *mockMapper) IssueToBeads(ti *TrackerIssue) *IssueConversion {
	return &IssueConversion{
		Issue: &types.Issue{
			Title:       ti.Title,
			Description: ti.Description,
			Priority:    2,
			Status:      types.StatusOpen,
			IssueType:   types.TypeTask,
		},
	}
}

func TestEnginePullOnly(t *testing.T) {
	ctx := context.Background()
	store := memory.New("")
	defer store.Close()

	tracker := newMockTracker("test")
	tracker.issues = []TrackerIssue{
		{ID: "1", Identifier: "TEST-1", Title: "First issue", Description: "Desc 1", UpdatedAt: time.Now()},
		{ID: "2", Identifier: "TEST-2", Title: "Second issue", Description: "Desc 2", UpdatedAt: time.Now()},
	}

	engine := NewEngine(tracker, store, "test-actor")

	result, err := engine.Sync(ctx, SyncOptions{Pull: true})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if !result.Success {
		t.Errorf("Sync() not successful: %s", result.Error)
	}
	if result.Stats.Created != 2 {
		t.Errorf("Stats.Created = %d, want 2", result.Stats.Created)
	}

	// Verify issues were stored
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues() error: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("stored %d issues, want 2", len(issues))
	}
}

func TestEnginePushOnly(t *testing.T) {
	ctx := context.Background()
	store := memory.New("")
	defer store.Close()

	// Create a local issue
	issue := &types.Issue{
		ID:        "bd-test1",
		Title:     "Local issue",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue() error: %v", err)
	}

	tracker := newMockTracker("test")
	engine := NewEngine(tracker, store, "test-actor")

	result, err := engine.Sync(ctx, SyncOptions{Push: true})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if !result.Success {
		t.Errorf("Sync() not successful: %s", result.Error)
	}
	if result.Stats.Created != 1 {
		t.Errorf("Stats.Created = %d, want 1", result.Stats.Created)
	}
	if len(tracker.created) != 1 {
		t.Errorf("tracker.created = %d, want 1", len(tracker.created))
	}
}

func TestEngineDryRun(t *testing.T) {
	ctx := context.Background()
	store := memory.New("")
	defer store.Close()

	tracker := newMockTracker("test")
	tracker.issues = []TrackerIssue{
		{ID: "1", Identifier: "TEST-1", Title: "Issue", UpdatedAt: time.Now()},
	}

	engine := NewEngine(tracker, store, "test-actor")

	result, err := engine.Sync(ctx, SyncOptions{Pull: true, DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if !result.Success {
		t.Errorf("Sync() not successful: %s", result.Error)
	}
	if result.Stats.Created != 1 {
		t.Errorf("Stats.Created = %d, want 1 (dry-run counted)", result.Stats.Created)
	}

	// Verify nothing was actually stored
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues() error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("stored %d issues in dry-run, want 0", len(issues))
	}
}

func TestEngineExcludeTypes(t *testing.T) {
	ctx := context.Background()
	store := memory.New("")
	defer store.Close()

	// Create issues of different types
	for _, tc := range []struct {
		id  string
		typ types.IssueType
	}{
		{"bd-task1", types.TypeTask},
		{"bd-bug1", types.TypeBug},
		{"bd-feat1", types.TypeFeature},
	} {
		issue := &types.Issue{
			ID:        tc.id,
			Title:     "Issue " + tc.id,
			Status:    types.StatusOpen,
			IssueType: tc.typ,
			Priority:  2,
		}
		if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
			t.Fatalf("CreateIssue(%s) error: %v", tc.id, err)
		}
	}

	tracker := newMockTracker("test")
	engine := NewEngine(tracker, store, "test-actor")

	// Push excluding bugs
	result, err := engine.Sync(ctx, SyncOptions{Push: true, ExcludeTypes: []types.IssueType{types.TypeBug}})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if !result.Success {
		t.Errorf("Sync() not successful: %s", result.Error)
	}
	if len(tracker.created) != 2 {
		t.Errorf("created %d issues (excluding bugs), want 2", len(tracker.created))
	}
}

func TestEngineConflictResolution(t *testing.T) {
	ctx := context.Background()
	store := memory.New("")
	defer store.Close()

	// Set up last_sync
	lastSync := time.Now().Add(-1 * time.Hour)
	if err := store.SetConfig(ctx, "test.last_sync", lastSync.Format(time.RFC3339)); err != nil {
		t.Fatalf("SetConfig() error: %v", err)
	}

	// Create a local issue that was modified after last_sync
	issue := &types.Issue{
		ID:          "bd-conflict1",
		Title:       "Local version",
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		Priority:    2,
		ExternalRef: strPtr("https://test.test/EXT-1"),
		UpdatedAt:   time.Now().Add(-30 * time.Minute), // Modified 30 min ago
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue() error: %v", err)
	}

	// Set up tracker with an external issue also modified after last_sync
	tracker := newMockTracker("test")
	tracker.issues = []TrackerIssue{
		{
			ID:         "EXT-1",
			Identifier: "EXT-1",
			Title:      "External version",
			UpdatedAt:  time.Now().Add(-15 * time.Minute), // Modified 15 min ago (newer)
		},
	}

	engine := NewEngine(tracker, store, "test-actor")

	// Detect conflicts
	conflicts, err := engine.DetectConflicts(ctx)
	if err != nil {
		t.Fatalf("DetectConflicts() error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("detected %d conflicts, want 1", len(conflicts))
	}
	if conflicts[0].IssueID != "bd-conflict1" {
		t.Errorf("conflict issue ID = %q, want %q", conflicts[0].IssueID, "bd-conflict1")
	}
}

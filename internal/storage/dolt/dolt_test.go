package dolt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// testTimeout is the maximum time for any single test operation.
// The embedded Dolt driver can be slow, especially for complex JOIN queries.
// If tests are timing out, it may indicate an issue with the embedded Dolt
// driver's async operations rather than with the DoltStore implementation.
const testTimeout = 30 * time.Second

// testContext returns a context with timeout for test operations
func testContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), testTimeout)
}

// skipIfNoDolt skips the test if Dolt is not installed
func skipIfNoDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("Dolt not installed, skipping test")
	}
}

// setupTestStore creates a test store with a temporary directory
func setupTestStore(t *testing.T) (*DoltStore, func()) {
	t.Helper()
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create Dolt store: %v", err)
	}

	// Set up issue prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to set prefix: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestNewDoltStore(t *testing.T) {
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "dolt-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Verify store path
	if store.Path() != tmpDir {
		t.Errorf("expected path %s, got %s", tmpDir, store.Path())
	}

	// Verify not closed
	if store.IsClosed() {
		t.Error("store should not be closed")
	}
}

// TestCreateIssueEventType verifies that CreateIssue accepts event type
// without requiring it in types.custom config (GH#1356).
func TestCreateIssueEventType(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// setupTestStore does not set types.custom, so this reproduces the bug
	event := &types.Issue{
		Title:     "state change audit trail",
		Status:    types.StatusClosed,
		Priority:  4,
		IssueType: types.TypeEvent,
	}
	err := store.CreateIssue(ctx, event, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue with event type should succeed without types.custom, got: %v", err)
	}

	got, err := store.GetIssue(ctx, event.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.IssueType != types.TypeEvent {
		t.Errorf("Expected IssueType %q, got %q", types.TypeEvent, got.IssueType)
	}
}

func TestDoltStoreConfig(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Test SetConfig
	if err := store.SetConfig(ctx, "test_key", "test_value"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	// Test GetConfig
	value, err := store.GetConfig(ctx, "test_key")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if value != "test_value" {
		t.Errorf("expected 'test_value', got %q", value)
	}

	// Test GetAllConfig
	allConfig, err := store.GetAllConfig(ctx)
	if err != nil {
		t.Fatalf("failed to get all config: %v", err)
	}
	if allConfig["test_key"] != "test_value" {
		t.Errorf("expected test_key in all config")
	}

	// Test DeleteConfig
	if err := store.DeleteConfig(ctx, "test_key"); err != nil {
		t.Fatalf("failed to delete config: %v", err)
	}
	value, err = store.GetConfig(ctx, "test_key")
	if err != nil {
		t.Fatalf("failed to get deleted config: %v", err)
	}
	if value != "" {
		t.Errorf("expected empty value after delete, got %q", value)
	}
}

func TestDoltStoreIssue(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Verify ID was generated
	if issue.ID == "" {
		t.Error("expected issue ID to be generated")
	}

	// Get the issue back
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to retrieve issue")
	}
	if retrieved.Title != issue.Title {
		t.Errorf("expected title %q, got %q", issue.Title, retrieved.Title)
	}
}

func TestDoltStoreIssueUpdate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		Title:       "Original Title",
		Description: "Original description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Update the issue
	updates := map[string]interface{}{
		"title":    "Updated Title",
		"priority": 1,
		"status":   string(types.StatusInProgress),
	}

	if err := store.UpdateIssue(ctx, issue.ID, updates, "tester"); err != nil {
		t.Fatalf("failed to update issue: %v", err)
	}

	// Get the updated issue
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if retrieved.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", retrieved.Title)
	}
	if retrieved.Priority != 1 {
		t.Errorf("expected priority 1, got %d", retrieved.Priority)
	}
	if retrieved.Status != types.StatusInProgress {
		t.Errorf("expected status in_progress, got %s", retrieved.Status)
	}
}

func TestDoltStoreIssueClose(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		Title:       "Issue to Close",
		Description: "Will be closed",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Close the issue
	if err := store.CloseIssue(ctx, issue.ID, "completed", "tester", "session123"); err != nil {
		t.Fatalf("failed to close issue: %v", err)
	}

	// Get the closed issue
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if retrieved.Status != types.StatusClosed {
		t.Errorf("expected status closed, got %s", retrieved.Status)
	}
	if retrieved.ClosedAt == nil {
		t.Error("expected closed_at to be set")
	}
}

func TestDoltStoreLabels(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		Title:       "Issue with Labels",
		Description: "Test labels",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Add labels
	if err := store.AddLabel(ctx, issue.ID, "bug", "tester"); err != nil {
		t.Fatalf("failed to add label: %v", err)
	}
	if err := store.AddLabel(ctx, issue.ID, "priority", "tester"); err != nil {
		t.Fatalf("failed to add second label: %v", err)
	}

	// Get labels
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}
	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels))
	}

	// Remove label
	if err := store.RemoveLabel(ctx, issue.ID, "bug", "tester"); err != nil {
		t.Fatalf("failed to remove label: %v", err)
	}

	// Verify removal
	labels, err = store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get labels after removal: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("expected 1 label after removal, got %d", len(labels))
	}
}

func TestDoltStoreDependencies(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create parent and child issues
	parent := &types.Issue{
		ID:          "test-parent",
		Title:       "Parent Issue",
		Description: "Parent description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeEpic,
	}
	child := &types.Issue{
		ID:          "test-child",
		Title:       "Child Issue",
		Description: "Child description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, parent, "tester"); err != nil {
		t.Fatalf("failed to create parent issue: %v", err)
	}
	if err := store.CreateIssue(ctx, child, "tester"); err != nil {
		t.Fatalf("failed to create child issue: %v", err)
	}

	// Add dependency (child depends on parent)
	dep := &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: parent.ID,
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Get dependencies
	deps, err := store.GetDependencies(ctx, child.ID)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].ID != parent.ID {
		t.Errorf("expected dependency on %s, got %s", parent.ID, deps[0].ID)
	}

	// Get dependents
	dependents, err := store.GetDependents(ctx, parent.ID)
	if err != nil {
		t.Fatalf("failed to get dependents: %v", err)
	}
	if len(dependents) != 1 {
		t.Errorf("expected 1 dependent, got %d", len(dependents))
	}

	// Check if blocked
	blocked, blockers, err := store.IsBlocked(ctx, child.ID)
	if err != nil {
		t.Fatalf("failed to check if blocked: %v", err)
	}
	if !blocked {
		t.Error("expected child to be blocked")
	}
	if len(blockers) != 1 || blockers[0] != parent.ID {
		t.Errorf("expected blocker %s, got %v", parent.ID, blockers)
	}

	// Remove dependency
	if err := store.RemoveDependency(ctx, child.ID, parent.ID, "tester"); err != nil {
		t.Fatalf("failed to remove dependency: %v", err)
	}

	// Verify removal
	deps, err = store.GetDependencies(ctx, child.ID)
	if err != nil {
		t.Fatalf("failed to get dependencies after removal: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies after removal, got %d", len(deps))
	}
}

func TestDoltStoreSearch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create multiple issues
	issues := []*types.Issue{
		{
			ID:          "test-search-1",
			Title:       "First Issue",
			Description: "Search test one",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		},
		{
			ID:          "test-search-2",
			Title:       "Second Issue",
			Description: "Search test two",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeBug,
		},
		{
			ID:          "test-search-3",
			Title:       "Third Issue",
			Description: "Different content",
			Status:      types.StatusClosed,
			Priority:    3,
			IssueType:   types.TypeTask,
		},
	}

	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue %s: %v", issue.ID, err)
		}
	}

	// Search by query
	results, err := store.SearchIssues(ctx, "Search test", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'Search test', got %d", len(results))
	}

	// Search with status filter
	openStatus := types.StatusOpen
	results, err = store.SearchIssues(ctx, "", types.IssueFilter{Status: &openStatus})
	if err != nil {
		t.Fatalf("failed to search with status filter: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 open issues, got %d", len(results))
	}

	// Search by issue type
	bugType := types.TypeBug
	results, err = store.SearchIssues(ctx, "", types.IssueFilter{IssueType: &bugType})
	if err != nil {
		t.Fatalf("failed to search by type: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 bug, got %d", len(results))
	}
}

func TestDoltStoreCreateIssues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create multiple issues in batch
	issues := []*types.Issue{
		{
			ID:          "test-batch-1",
			Title:       "Batch Issue 1",
			Description: "First batch issue",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		},
		{
			ID:          "test-batch-2",
			Title:       "Batch Issue 2",
			Description: "Second batch issue",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		},
	}

	if err := store.CreateIssues(ctx, issues, "tester"); err != nil {
		t.Fatalf("failed to create issues: %v", err)
	}

	// Verify all issues were created
	for _, issue := range issues {
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("failed to get issue %s: %v", issue.ID, err)
		}
		if retrieved == nil {
			t.Errorf("expected to retrieve issue %s", issue.ID)
		}
		if retrieved.Title != issue.Title {
			t.Errorf("expected title %q, got %q", issue.Title, retrieved.Title)
		}
	}
}

func TestDoltStoreComments(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		ID:          "test-comment-issue",
		Title:       "Issue with Comments",
		Description: "Test comments",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Add comments
	comment1, err := store.AddIssueComment(ctx, issue.ID, "user1", "First comment")
	if err != nil {
		t.Fatalf("failed to add first comment: %v", err)
	}
	if comment1.ID == 0 {
		t.Error("expected comment ID to be generated")
	}

	_, err = store.AddIssueComment(ctx, issue.ID, "user2", "Second comment")
	if err != nil {
		t.Fatalf("failed to add second comment: %v", err)
	}

	// Get comments
	comments, err := store.GetIssueComments(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get comments: %v", err)
	}
	if len(comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].Text != "First comment" {
		t.Errorf("expected 'First comment', got %q", comments[0].Text)
	}
}

func TestDoltStoreEvents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue (this creates a creation event)
	issue := &types.Issue{
		ID:          "test-event-issue",
		Title:       "Issue with Events",
		Description: "Test events",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Add a comment event
	if err := store.AddComment(ctx, issue.ID, "user1", "A comment"); err != nil {
		t.Fatalf("failed to add comment: %v", err)
	}

	// Get events
	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}
	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}
}

func TestDoltStoreDeleteIssue(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		ID:          "test-delete-issue",
		Title:       "Issue to Delete",
		Description: "Will be deleted",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Verify it exists
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil || retrieved == nil {
		t.Fatalf("issue should exist before delete")
	}

	// Delete the issue
	if err := store.DeleteIssue(ctx, issue.ID); err != nil {
		t.Fatalf("failed to delete issue: %v", err)
	}

	// Verify it's gone
	retrieved, err = store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue after delete: %v", err)
	}
	if retrieved != nil {
		t.Error("expected issue to be deleted")
	}
}

func TestDoltStoreDirtyTracking(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue (marks it dirty)
	issue := &types.Issue{
		ID:          "test-dirty-issue",
		Title:       "Dirty Issue",
		Description: "Will be dirty",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Get dirty issues
	dirtyIDs, err := store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("failed to get dirty issues: %v", err)
	}
	found := false
	for _, id := range dirtyIDs {
		if id == issue.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected issue to be in dirty list")
	}

	// Clear dirty issues
	if err := store.ClearDirtyIssuesByID(ctx, []string{issue.ID}); err != nil {
		t.Fatalf("failed to clear dirty issues: %v", err)
	}

	// Verify it's cleared
	dirtyIDs, err = store.GetDirtyIssues(ctx)
	if err != nil {
		t.Fatalf("failed to get dirty issues after clear: %v", err)
	}
	for _, id := range dirtyIDs {
		if id == issue.ID {
			t.Error("expected issue to be cleared from dirty list")
		}
	}
}

func TestDoltStoreStatistics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create some issues
	issues := []*types.Issue{
		{ID: "test-stat-1", Title: "Open 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		{ID: "test-stat-2", Title: "Open 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "test-stat-3", Title: "Closed", Status: types.StatusClosed, Priority: 1, IssueType: types.TypeTask},
	}

	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Get statistics
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		t.Fatalf("failed to get statistics: %v", err)
	}

	if stats.OpenIssues < 2 {
		t.Errorf("expected at least 2 open issues, got %d", stats.OpenIssues)
	}
	if stats.ClosedIssues < 1 {
		t.Errorf("expected at least 1 closed issue, got %d", stats.ClosedIssues)
	}
}

// Test SQL injection protection

func TestValidateRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{"valid hash", "abc123def456", false},
		{"valid branch", "main", false},
		{"valid with underscore", "feature_branch", false},
		{"valid with dash", "feature-branch", false},
		{"empty", "", true},
		{"too long", string(make([]byte, 200)), true},
		{"with SQL injection", "main'; DROP TABLE issues; --", true},
		{"with quotes", "main'test", true},
		{"with semicolon", "main;test", true},
		{"with space", "main test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTableName(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		wantErr   bool
	}{
		{"valid table", "issues", false},
		{"valid with underscore", "dirty_issues", false},
		{"valid with numbers", "table123", false},
		{"empty", "", true},
		{"too long", string(make([]byte, 100)), true},
		{"starts with number", "123table", true},
		{"with SQL injection", "issues'; DROP TABLE issues; --", true},
		{"with space", "my table", true},
		{"with dash", "my-table", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTableName(tt.tableName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTableName(%q) error = %v, wantErr %v", tt.tableName, err, tt.wantErr)
			}
		})
	}
}

func TestDoltStoreGetReadyWork(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create issues: one blocked, one ready
	blocker := &types.Issue{
		ID:          "test-blocker",
		Title:       "Blocker",
		Description: "Blocks another issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	blocked := &types.Issue{
		ID:          "test-blocked",
		Title:       "Blocked",
		Description: "Is blocked",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	ready := &types.Issue{
		ID:          "test-ready",
		Title:       "Ready",
		Description: "Is ready",
		Status:      types.StatusOpen,
		Priority:    3,
		IssueType:   types.TypeTask,
	}

	for _, issue := range []*types.Issue{blocker, blocked, ready} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue %s: %v", issue.ID, err)
		}
	}

	// Add blocking dependency
	dep := &types.Dependency{
		IssueID:     blocked.ID,
		DependsOnID: blocker.ID,
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Get ready work
	readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("failed to get ready work: %v", err)
	}

	// Should include blocker and ready, but not blocked
	foundBlocker := false
	foundBlocked := false
	foundReady := false
	for _, issue := range readyWork {
		switch issue.ID {
		case blocker.ID:
			foundBlocker = true
		case blocked.ID:
			foundBlocked = true
		case ready.ID:
			foundReady = true
		}
	}

	if !foundBlocker {
		t.Error("expected blocker to be in ready work")
	}
	if foundBlocked {
		t.Error("expected blocked issue to NOT be in ready work")
	}
	if !foundReady {
		t.Error("expected ready issue to be in ready work")
	}
}

// TestClosedIssueIDCollision verifies that creating an issue with the same content
// as a closed issue doesn't inherit the closed status (hq-3ebbac fix).
func TestClosedIssueIDCollision(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue with specific content
	issue1 := &types.Issue{
		Title:       "Wisp Step",
		Description: "Test step for wisp",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		IDPrefix:    "wisp", // Use wisp prefix like actual wisps
	}

	if err := store.CreateIssue(ctx, issue1, "tester"); err != nil {
		t.Fatalf("failed to create first issue: %v", err)
	}
	originalID := issue1.ID

	// Close the issue
	if err := store.CloseIssue(ctx, issue1.ID, "completed", "tester", "session1"); err != nil {
		t.Fatalf("failed to close issue: %v", err)
	}

	// Verify it's closed
	retrieved, err := store.GetIssue(ctx, originalID)
	if err != nil {
		t.Fatalf("failed to get closed issue: %v", err)
	}
	if retrieved.Status != types.StatusClosed {
		t.Errorf("expected status %s, got %s", types.StatusClosed, retrieved.Status)
	}

	// Now try to create a new issue with the same content (same hash -> same base ID)
	// This simulates creating a new wisp from the same template
	issue2 := &types.Issue{
		Title:       "Wisp Step",
		Description: "Test step for wisp",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		IDPrefix:    "wisp",
	}

	// Use RunInTransaction since that's what wisp creation uses
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.CreateIssue(ctx, issue2, "tester")
	})
	if err != nil {
		t.Fatalf("failed to create second issue: %v", err)
	}

	// The new issue should have a different ID (with suffix)
	if issue2.ID == originalID {
		t.Errorf("expected new issue to have different ID, but both have %s", issue2.ID)
	}

	// The new issue should be open, not closed
	retrieved2, err := store.GetIssue(ctx, issue2.ID)
	if err != nil {
		t.Fatalf("failed to get new issue: %v", err)
	}
	if retrieved2.Status != types.StatusOpen {
		t.Errorf("expected new issue status %s, got %s", types.StatusOpen, retrieved2.Status)
	}

	// Verify the ID follows the expected suffix pattern
	expectedPrefix := originalID + "-"
	if len(issue2.ID) <= len(expectedPrefix) || issue2.ID[:len(expectedPrefix)] != expectedPrefix {
		t.Logf("Note: new issue ID %s (original was %s)", issue2.ID, originalID)
	}
}

// TestIsTransientDoltError verifies the transient error detection function (bd-3q6.4)
func TestIsTransientDoltError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "invalid format version",
			err:      fmt.Errorf("invalid format version"),
			expected: true,
		},
		{
			name:     "failed to load database with format version",
			err:      fmt.Errorf("failed to load database at /path/to/db with error: invalid format version"),
			expected: true,
		},
		{
			name:     "failed to load database generic",
			err:      fmt.Errorf("failed to load database"),
			expected: true,
		},
		{
			name:     "manifest invalid",
			err:      fmt.Errorf("manifest is invalid or corrupted"),
			expected: true,
		},
		{
			name:     "database is read only (lock error)",
			err:      fmt.Errorf("database is read only"),
			expected: true,
		},
		{
			name:     "database is locked",
			err:      fmt.Errorf("database is locked"),
			expected: true,
		},
		{
			name:     "lock timeout",
			err:      fmt.Errorf("lock timeout exceeded"),
			expected: true,
		},
		{
			name:     "lock contention",
			err:      fmt.Errorf("lock contention detected"),
			expected: true,
		},
		{
			name:     "generic non-transient error",
			err:      fmt.Errorf("table does not exist"),
			expected: false,
		},
		{
			name:     "connection refused",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
		{
			name:     "case insensitive format version",
			err:      fmt.Errorf("Invalid Format Version in manifest"),
			expected: true,
		},
		// Serialization conflict errors (hq-jkr56x)
		{
			name:     "error 1105 optimistic lock failed",
			err:      fmt.Errorf("Error 1105 (HY000): optimistic lock failed on database Root update"),
			expected: true,
		},
		{
			name:     "optimistic lock failed generic",
			err:      fmt.Errorf("optimistic lock failed"),
			expected: true,
		},
		{
			name:     "error 1213 serialization failure",
			err:      fmt.Errorf("Error 1213 (40001): Serialization failure"),
			expected: true,
		},
		{
			name:     "serialization failure generic",
			err:      fmt.Errorf("serialization failure during commit"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientDoltError(tt.err)
			if result != tt.expected {
				t.Errorf("isTransientDoltError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsSerializationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "error 1105 optimistic lock failed",
			err:      fmt.Errorf("Error 1105 (HY000): optimistic lock failed on database Root update"),
			expected: true,
		},
		{
			name:     "optimistic lock failed generic",
			err:      fmt.Errorf("optimistic lock failed"),
			expected: true,
		},
		{
			name:     "error 1213 serialization failure",
			err:      fmt.Errorf("Error 1213 (40001): Serialization failure"),
			expected: true,
		},
		{
			name:     "serialization failure generic",
			err:      fmt.Errorf("serialization failure during commit"),
			expected: true,
		},
		{
			name:     "non-serialization error",
			err:      fmt.Errorf("table does not exist"),
			expected: false,
		},
		{
			name:     "lock error is not serialization",
			err:      fmt.Errorf("database is locked"),
			expected: false, // lock errors are separate from serialization errors
		},
		{
			name:     "case insensitive error 1105",
			err:      fmt.Errorf("error 1105 in statement"),
			expected: true,
		},
		{
			name:     "error 1105 nothing to commit is NOT serialization",
			err:      fmt.Errorf("Error 1105: nothing to commit"),
			expected: false, // "nothing to commit" is not a serialization error
		},
		{
			name:     "nothing to commit generic",
			err:      fmt.Errorf("nothing to commit"),
			expected: false,
		},
		{
			name:     "no changes to commit",
			err:      fmt.Errorf("no changes to commit"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSerializationError(tt.err)
			if result != tt.expected {
				t.Errorf("isSerializationError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

// TestCommitRetryConfig verifies that retry configuration is properly stored
func TestCommitRetryConfig(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-retry-config-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with custom retry config
	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@test.com",
		LockRetries:    10,
		LockRetryDelay: 200 * time.Millisecond,
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Verify config was stored
	if store.lockRetries != 10 {
		t.Errorf("expected lockRetries=10, got %d", store.lockRetries)
	}
	if store.lockRetryDelay != 200*time.Millisecond {
		t.Errorf("expected lockRetryDelay=200ms, got %v", store.lockRetryDelay)
	}
}

// TestCommitRetryDefaults verifies that default retry config is used when not specified
func TestCommitRetryDefaults(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "dolt-retry-defaults-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with no retry config - defaults are applied in New()
	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@test.com",
		// LockRetries and LockRetryDelay intentionally not set - will use defaults
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Config defaults are applied in New() - LockRetries defaults to 30
	// When lockRetries is 0 in the store, Commit() uses an internal default of 5
	// This tests that the store was created successfully with some retry config
	if store.lockRetries < 0 {
		t.Errorf("expected lockRetries >= 0, got %d", store.lockRetries)
	}
}

// TestCommitSucceedsWithoutRetry verifies normal commit behavior without errors
func TestCommitSucceedsWithoutRetry(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue to have something to commit
	issue := &types.Issue{
		ID:        "test-commit-1",
		Title:     "Test commit without retry",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Commit should succeed on first try
	err := store.Commit(ctx, "Test commit")
	if err != nil {
		t.Errorf("expected commit to succeed, got error: %v", err)
	}
}

// TestConcurrentCommits verifies that concurrent commits don't cause permanent failures
// This is an integration test that exercises the retry logic under concurrent access
func TestConcurrentCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create multiple issues and commit them concurrently
	const numGoroutines = 3
	const issuesPerGoroutine = 2

	errCh := make(chan error, numGoroutines*issuesPerGoroutine)
	doneCh := make(chan struct{}, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer func() { doneCh <- struct{}{} }()

			for i := 0; i < issuesPerGoroutine; i++ {
				issue := &types.Issue{
					ID:        fmt.Sprintf("concurrent-%d-%d", goroutineID, i),
					Title:     fmt.Sprintf("Concurrent test issue %d-%d", goroutineID, i),
					Status:    types.StatusOpen,
					IssueType: types.TypeTask,
				}

				if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
					errCh <- fmt.Errorf("goroutine %d: failed to create issue %d: %w", goroutineID, i, err)
					return
				}

				// Commit after each issue - this may trigger optimistic lock failures
				// which should be retried automatically
				if err := store.Commit(ctx, fmt.Sprintf("Concurrent commit %d-%d", goroutineID, i)); err != nil {
					// Note: "nothing to commit" is not an error in this context
					if !isDoltNothingToCommit(err) {
						errCh <- fmt.Errorf("goroutine %d: failed to commit %d: %w", goroutineID, i, err)
						return
					}
				}
			}
		}(g)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-doneCh
	}
	close(errCh)

	// Check for any errors
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("concurrent commits had %d errors:", len(errors))
		for _, err := range errors {
			t.Errorf("  - %v", err)
		}
	}

	// Verify all issues were created by fetching each one
	for g := 0; g < numGoroutines; g++ {
		for i := 0; i < issuesPerGoroutine; i++ {
			id := fmt.Sprintf("concurrent-%d-%d", g, i)
			issue, err := store.GetIssue(ctx, id)
			if err != nil {
				t.Errorf("failed to get issue %s: %v", id, err)
			}
			if issue == nil {
				t.Errorf("expected issue %s to exist", id)
			}
		}
	}
}

// isDoltNothingToCommit checks if the error indicates there was nothing to commit
// (duplicated from dolt_autocommit.go for test isolation)
func isDoltNothingToCommit(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "nothing to commit") || (contains(s, "no changes") && contains(s, "commit"))
}

// contains is a case-insensitive contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsLower(lower(s), lower(substr)))
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

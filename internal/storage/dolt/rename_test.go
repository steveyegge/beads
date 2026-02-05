package dolt

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestUpdateIssueIDWithComments verifies that renaming an issue with comments
// does not fail with FK constraint violations (bd-wj80.1).
func TestUpdateIssueIDWithComments(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		ID:          "test-old1",
		Title:       "Test issue",
		Description: "Some description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add a structured comment to the comments table (this triggers the FK cascade bug)
	if _, err := store.AddIssueComment(ctx, "test-old1", "test-user", "This is a comment"); err != nil {
		t.Fatalf("AddIssueComment failed: %v", err)
	}

	// Add a label
	if err := store.AddLabel(ctx, "test-old1", "bug", "test-user"); err != nil {
		t.Fatalf("AddLabel failed: %v", err)
	}

	// Rename the issue - this was failing with FK violation before the fix
	issue.ID = "test-new1"
	if err := store.UpdateIssueID(ctx, "test-old1", "test-new1", issue, "test-user"); err != nil {
		t.Fatalf("UpdateIssueID failed: %v", err)
	}

	// Verify old ID no longer exists
	old, err := store.GetIssue(ctx, "test-old1")
	if err != nil {
		t.Fatalf("GetIssue(old) failed: %v", err)
	}
	if old != nil {
		t.Error("old issue should not exist after rename")
	}

	// Verify new ID exists
	got, err := store.GetIssue(ctx, "test-new1")
	if err != nil {
		t.Fatalf("GetIssue(new) failed: %v", err)
	}
	if got == nil {
		t.Fatal("renamed issue should exist")
	}
	if got.Title != "Test issue" {
		t.Errorf("expected title %q, got %q", "Test issue", got.Title)
	}

	// Verify comments were cascaded to new ID
	comments, err := store.GetCommentsForIssues(ctx, []string{"test-new1"})
	if err != nil {
		t.Fatalf("GetCommentsForIssues failed: %v", err)
	}
	if len(comments["test-new1"]) != 1 {
		t.Errorf("expected 1 comment on new ID, got %d", len(comments["test-new1"]))
	}

	// Verify labels were cascaded to new ID
	labels, err := store.GetLabels(ctx, "test-new1")
	if err != nil {
		t.Fatalf("GetLabels failed: %v", err)
	}
	found := false
	for _, l := range labels {
		if l == "bug" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected label 'bug' on renamed issue, got %v", labels)
	}
}

// TestUpdateIssueIDWithDependencies verifies that dependencies are properly
// cascaded when renaming an issue.
func TestUpdateIssueIDWithDependencies(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create two issues
	parent := &types.Issue{
		ID:        "test-parent",
		Title:     "Parent issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	child := &types.Issue{
		ID:        "test-child",
		Title:     "Child issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, parent, "test-user"); err != nil {
		t.Fatalf("CreateIssue(parent) failed: %v", err)
	}
	if err := store.CreateIssue(ctx, child, "test-user"); err != nil {
		t.Fatalf("CreateIssue(child) failed: %v", err)
	}

	// Add dependency: parent depends on child (child blocks parent)
	dep := &types.Dependency{
		IssueID:     "test-parent",
		DependsOnID: "test-child",
		Type:        "blocks",
	}
	if err := store.AddDependency(ctx, dep, "test-user"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Rename the parent
	parent.ID = "test-renamed-parent"
	if err := store.UpdateIssueID(ctx, "test-parent", "test-renamed-parent", parent, "test-user"); err != nil {
		t.Fatalf("UpdateIssueID failed: %v", err)
	}

	// Verify dependencies point to new ID (GetDependencies returns the blockers)
	deps, err := store.GetDependencies(ctx, "test-renamed-parent")
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].ID != "test-child" {
		t.Errorf("expected dependency on %q, got %q", "test-child", deps[0].ID)
	}
}

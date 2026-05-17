package dolt

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestUpdateIssueIDUpdatesWispDependencyTargets verifies that renaming a
// regular issue also updates wisp_dependencies rows that target it. Wisp
// auxiliary table source rows always belong to wisps and are covered by
// TestUpdateIssueIDRenamesWisp.
func TestUpdateIssueIDUpdatesWispDependencyTargets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a permanent issue that we'll rename
	issue := &types.Issue{
		ID:        "test-old1",
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Create a wisp that references the issue via wisp_dependencies
	wisp := &types.Issue{
		ID:        "test-wisp-abc",
		Title:     "Test wisp",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.CreateIssue(ctx, wisp, "test"); err != nil {
		t.Fatalf("failed to create wisp: %v", err)
	}

	// Add wisp dependency: wisp depends on the permanent issue
	dep := &types.Dependency{
		IssueID:     wisp.ID,
		DependsOnID: "test-old1",
		Type:        types.DepBlocks,
	}
	if err := store.addWispDependency(ctx, dep, "test", false); err != nil {
		t.Fatalf("failed to add wisp dependency: %v", err)
	}

	// Now rename the issue
	newID := "test-new1"
	if err := store.UpdateIssueID(ctx, "test-old1", newID, issue, "test"); err != nil {
		t.Fatalf("UpdateIssueID failed: %v", err)
	}

	// Verify wisp_dependencies.depends_on_id was updated
	var depCount int
	err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wisp_dependencies WHERE depends_on_id = ?`, newID).Scan(&depCount)
	if err != nil {
		t.Fatalf("failed to query wisp_dependencies depends_on_id: %v", err)
	}
	if depCount != 1 {
		t.Errorf("expected 1 wisp_dependencies row with depends_on_id=%q, got %d", newID, depCount)
	}

	// Verify wisp_dependencies.issue_id still points at the source wisp.
	err = store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wisp_dependencies WHERE issue_id = ?`, wisp.ID).Scan(&depCount)
	if err != nil {
		t.Fatalf("failed to query wisp_dependencies issue_id: %v", err)
	}
	if depCount != 1 {
		t.Errorf("expected 1 wisp_dependencies row with issue_id=%q, got %d", wisp.ID, depCount)
	}

	// Verify old ID is gone from wisp_dependencies
	var oldCount int
	err = store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wisp_dependencies WHERE depends_on_id = ?`, "test-old1").Scan(&oldCount)
	if err != nil {
		t.Fatalf("failed to query old wisp_dependencies: %v", err)
	}
	if oldCount != 0 {
		t.Errorf("expected 0 wisp_dependencies rows with old ID, got %d", oldCount)
	}
}

// TestUpdateIssueIDRenamesWisp verifies that UpdateIssueID correctly renames
// wisps in the wisps table and all wisp_* auxiliary tables when the target
// is itself a wisp (not a regular issue). (bd-8ykk)
func TestUpdateIssueIDRenamesWisp(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create a wisp (goes to wisps + wisp_* tables)
	wisp := &types.Issue{
		Title:     "Wisp to rename",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.CreateIssue(ctx, wisp, "tester"); err != nil {
		t.Fatalf("CreateIssue (wisp) failed: %v", err)
	}
	oldID := wisp.ID
	if oldID == "" {
		t.Fatal("wisp got empty ID")
	}

	// Verify wisp is in the wisps table
	if !store.isActiveWisp(ctx, oldID) {
		t.Fatalf("expected %q to be an active wisp", oldID)
	}

	// Verify creation event was recorded in wisp_events
	var eventCount int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wisp_events WHERE issue_id = ?`, oldID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("failed to count wisp_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 wisp_events row for old ID, got %d", eventCount)
	}

	// Add a label to the wisp
	if err := store.AddLabel(ctx, oldID, "test-label", "tester"); err != nil {
		t.Fatalf("failed to add wisp label: %v", err)
	}

	// Rename the wisp
	newID := "test-renamed-wisp"
	wisp.ID = newID
	if err := store.UpdateIssueID(ctx, oldID, newID, wisp, "tester"); err != nil {
		t.Fatalf("UpdateIssueID failed: %v", err)
	}

	// Verify the old ID no longer exists in wisps table
	if store.isActiveWisp(ctx, oldID) {
		t.Fatal("old wisp ID should not exist after rename")
	}

	// Verify the new ID exists in wisps table
	if !store.isActiveWisp(ctx, newID) {
		t.Fatal("new wisp ID should exist in wisps table after rename")
	}

	// Verify wisp_events were updated (1 creation + 1 label_added + 1 rename = 3)
	var newEventCount int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wisp_events WHERE issue_id = ?`, newID).Scan(&newEventCount)
	if err != nil {
		t.Fatalf("failed to count wisp_events for new ID: %v", err)
	}
	if newEventCount != 3 {
		t.Fatalf("expected 3 wisp_events rows for new ID (creation + label_added + rename), got %d", newEventCount)
	}

	// Verify no events left under old ID
	var oldEventCount int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wisp_events WHERE issue_id = ?`, oldID).Scan(&oldEventCount)
	if err != nil {
		t.Fatalf("failed to count wisp_events for old ID: %v", err)
	}
	if oldEventCount != 0 {
		t.Fatalf("expected 0 wisp_events rows for old ID, got %d", oldEventCount)
	}

	// Verify wisp_labels were updated
	var labelCount int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wisp_labels WHERE issue_id = ?`, newID).Scan(&labelCount)
	if err != nil {
		t.Fatalf("failed to count wisp_labels for new ID: %v", err)
	}
	if labelCount != 1 {
		t.Fatalf("expected 1 wisp_labels row for new ID, got %d", labelCount)
	}

	// Verify no labels left under old ID
	var oldLabelCount int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wisp_labels WHERE issue_id = ?`, oldID).Scan(&oldLabelCount)
	if err != nil {
		t.Fatalf("failed to count wisp_labels for old ID: %v", err)
	}
	if oldLabelCount != 0 {
		t.Fatalf("expected 0 wisp_labels rows for old ID, got %d", oldLabelCount)
	}
}

// TestUpdateIssueIDStillWorksForRegularIssues verifies that the refactored
// UpdateIssueID still correctly handles regular (non-wisp) issues.
func TestUpdateIssueIDStillWorksForRegularIssues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create a regular issue
	issue := &types.Issue{
		ID:        "test-regular-1",
		Title:     "Regular issue to rename",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify it's not a wisp
	if store.isActiveWisp(ctx, issue.ID) {
		t.Fatal("regular issue should not be an active wisp")
	}

	// Rename it
	newID := "test-regular-renamed"
	issue.ID = newID
	if err := store.UpdateIssueID(ctx, "test-regular-1", newID, issue, "tester"); err != nil {
		t.Fatalf("UpdateIssueID failed: %v", err)
	}

	// Verify the old ID is gone and new ID exists
	got, err := store.GetIssue(ctx, newID)
	if err != nil {
		t.Fatalf("GetIssue failed for renamed issue: %v", err)
	}
	if got.Title != "Regular issue to rename" {
		t.Fatalf("expected original title, got %q", got.Title)
	}

	// Verify rename event in events table
	var eventCount int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE issue_id = ? AND event_type = 'renamed'`, newID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("failed to count rename events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 rename event for new ID, got %d", eventCount)
	}
}

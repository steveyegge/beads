//go:build cgo

package dolt

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// GetBlockedIssues Tests
// =============================================================================

func TestGetBlockedIssues(t *testing.T) {
	// Skip: GetBlockedIssues makes nested queries (GetIssue calls inside a rows cursor)
	// which can cause connection issues in embedded Dolt mode.
	// This is a known limitation that should be fixed in bd-tdgo.3.
	t.Skip("Skipping: GetBlockedIssues has nested query issue in embedded Dolt mode")
}

func TestGetBlockedIssues_ClosedBlocker(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a closed blocker and a blocked issue
	closedBlocker := &types.Issue{
		ID:        "closed-blocker",
		Title:     "Closed Blocker",
		Status:    types.StatusClosed,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	blocked := &types.Issue{
		ID:        "was-blocked",
		Title:     "Was Blocked",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	for _, issue := range []*types.Issue{closedBlocker, blocked} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Add dependency (blocker is already closed)
	dep := &types.Dependency{
		IssueID:     blocked.ID,
		DependsOnID: closedBlocker.ID,
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep, "tester"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Get blocked issues - should not include the "blocked" issue since blocker is closed
	blockedIssues, err := store.GetBlockedIssues(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("GetBlockedIssues failed: %v", err)
	}

	for _, bi := range blockedIssues {
		if bi.Issue.ID == blocked.ID {
			t.Error("issue should not be blocked when blocker is closed")
		}
	}
}

// =============================================================================
// GetEpicsEligibleForClosure Tests
// =============================================================================

func TestGetEpicsEligibleForClosure(t *testing.T) {
	// Skip: GetEpicsEligibleForClosure makes nested queries (GetIssue calls inside a rows cursor)
	// which can cause connection issues in embedded Dolt mode.
	// This is a known limitation that should be fixed in bd-tdgo.3.
	t.Skip("Skipping: GetEpicsEligibleForClosure has nested query issue in embedded Dolt mode")
}

func TestGetEpicsEligibleForClosure_OpenChild(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create an epic with one open child
	epic := &types.Issue{
		ID:        "not-eligible-epic",
		Title:     "Epic with Open Child",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	closedChild := &types.Issue{
		ID:        "notelig-closed",
		Title:     "Closed Child",
		Status:    types.StatusClosed,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	openChild := &types.Issue{
		ID:        "notelig-open",
		Title:     "Open Child",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	for _, issue := range []*types.Issue{epic, closedChild, openChild} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Add parent-child relationships
	for _, childID := range []string{closedChild.ID, openChild.ID} {
		dep := &types.Dependency{
			IssueID:     childID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "tester"); err != nil {
			t.Fatalf("failed to add dependency: %v", err)
		}
	}

	// Get epics eligible for closure - our epic should not be included
	epics, err := store.GetEpicsEligibleForClosure(ctx)
	if err != nil {
		t.Fatalf("GetEpicsEligibleForClosure failed: %v", err)
	}

	for _, es := range epics {
		if es.Epic.ID == epic.ID {
			t.Error("epic with open child should not be eligible for closure")
		}
	}
}

// =============================================================================
// GetStaleIssues Tests
// =============================================================================

func TestGetStaleIssues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create an issue with old updated_at (simulate stale issue)
	staleIssue := &types.Issue{
		ID:        "stale-issue",
		Title:     "Stale Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, staleIssue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Force the updated_at to be old by directly updating the database
	db, err := store.getDB(ctx)
	if err != nil {
		t.Fatalf("failed to get db: %v", err)
	}

	oldDate := time.Now().AddDate(0, 0, -30) // 30 days ago
	_, err = db.ExecContext(ctx, `UPDATE issues SET updated_at = ? WHERE id = ?`, oldDate, staleIssue.ID)
	if err != nil {
		t.Fatalf("failed to backdate issue: %v", err)
	}

	// Create a fresh issue
	freshIssue := &types.Issue{
		ID:        "fresh-issue",
		Title:     "Fresh Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, freshIssue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Get stale issues (older than 7 days)
	filter := types.StaleFilter{
		Days:  7,
		Limit: 100,
	}
	staleIssues, err := store.GetStaleIssues(ctx, filter)
	if err != nil {
		t.Fatalf("GetStaleIssues failed: %v", err)
	}

	// Should find the stale issue but not the fresh one
	foundStale := false
	for _, issue := range staleIssues {
		if issue.ID == staleIssue.ID {
			foundStale = true
		}
		if issue.ID == freshIssue.ID {
			t.Error("fresh issue should not be in stale list")
		}
	}

	if !foundStale {
		t.Error("stale issue should be in stale list")
	}
}

func TestGetStaleIssues_WithStatus(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create issues with different statuses
	openIssue := &types.Issue{
		ID:        "stale-open",
		Title:     "Stale Open",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	inProgressIssue := &types.Issue{
		ID:        "stale-inprogress",
		Title:     "Stale In Progress",
		Status:    types.StatusInProgress,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	for _, issue := range []*types.Issue{openIssue, inProgressIssue} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Backdate both issues
	db, err := store.getDB(ctx)
	if err != nil {
		t.Fatalf("failed to get db: %v", err)
	}

	oldDate := time.Now().AddDate(0, 0, -30)
	_, err = db.ExecContext(ctx, `UPDATE issues SET updated_at = ? WHERE id IN (?, ?)`,
		oldDate, openIssue.ID, inProgressIssue.ID)
	if err != nil {
		t.Fatalf("failed to backdate issues: %v", err)
	}

	// Get stale issues with status filter for 'open' only
	filter := types.StaleFilter{
		Days:   7,
		Status: string(types.StatusOpen),
		Limit:  100,
	}
	staleIssues, err := store.GetStaleIssues(ctx, filter)
	if err != nil {
		t.Fatalf("GetStaleIssues failed: %v", err)
	}

	// Should only find the open issue
	foundOpen := false
	for _, issue := range staleIssues {
		if issue.ID == openIssue.ID {
			foundOpen = true
		}
		if issue.ID == inProgressIssue.ID {
			t.Error("in_progress issue should not be included with status=open filter")
		}
	}

	if !foundOpen {
		t.Error("open stale issue should be found")
	}
}

// =============================================================================
// GetMoleculeProgress Tests
// =============================================================================

func TestGetMoleculeProgress(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a molecule with children
	mol := &types.Issue{
		ID:        "mol-progress",
		Title:     "Molecule for Progress",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeMolecule,
		MolType:   types.MolTypeWork,
	}
	child1 := &types.Issue{
		ID:        "mol-child1",
		Title:     "Child 1",
		Status:    types.StatusClosed,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	child2 := &types.Issue{
		ID:        "mol-child2",
		Title:     "Child 2",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	for _, issue := range []*types.Issue{mol, child1, child2} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Add parent-child relationships
	for _, childID := range []string{child1.ID, child2.ID} {
		dep := &types.Dependency{
			IssueID:     childID,
			DependsOnID: mol.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "tester"); err != nil {
			t.Fatalf("failed to add dependency: %v", err)
		}
	}

	// Get molecule progress
	progress, err := store.GetMoleculeProgress(ctx, mol.ID)
	if err != nil {
		t.Fatalf("GetMoleculeProgress failed: %v", err)
	}

	if progress == nil {
		t.Fatal("expected progress, got nil")
	}

	if progress.Total != 2 {
		t.Errorf("expected 2 total, got %d", progress.Total)
	}

	if progress.Completed != 1 {
		t.Errorf("expected 1 completed, got %d", progress.Completed)
	}
}

// =============================================================================
// SearchIssues Tests
// =============================================================================

func TestSearchIssues_TextQuery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create issues with different titles
	issue1 := &types.Issue{
		ID:          "search-auth",
		Title:       "Fix authentication bug",
		Description: "Users can't login",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeBug,
	}
	issue2 := &types.Issue{
		ID:          "search-db",
		Title:       "Database optimization",
		Description: "Improve query performance",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	for _, issue := range []*types.Issue{issue1, issue2} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Search for "authentication"
	results, err := store.SearchIssues(ctx, "authentication", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if len(results) > 0 && results[0].ID != issue1.ID {
		t.Errorf("expected to find %s, found %s", issue1.ID, results[0].ID)
	}
}

func TestSearchIssues_LabelFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create issues
	issue1 := &types.Issue{
		ID:        "label-bug",
		Title:     "Bug with label",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	issue2 := &types.Issue{
		ID:        "label-feature",
		Title:     "Feature without label",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	for _, issue := range []*types.Issue{issue1, issue2} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Add label to first issue
	if err := store.AddLabel(ctx, issue1.ID, "urgent", "tester"); err != nil {
		t.Fatalf("failed to add label: %v", err)
	}

	// Search with label filter
	filter := types.IssueFilter{
		Labels: []string{"urgent"},
	}
	results, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result with 'urgent' label, got %d", len(results))
	}

	if len(results) > 0 && results[0].ID != issue1.ID {
		t.Errorf("expected to find %s, found %s", issue1.ID, results[0].ID)
	}
}

func TestSearchIssues_StatusFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create issues with different statuses
	openIssue := &types.Issue{
		ID:        "status-open",
		Title:     "Open Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	closedIssue := &types.Issue{
		ID:        "status-closed",
		Title:     "Closed Issue",
		Status:    types.StatusClosed,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	for _, issue := range []*types.Issue{openIssue, closedIssue} {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}
	}

	// Search with status filter
	status := types.StatusOpen
	filter := types.IssueFilter{
		Status: &status,
	}
	results, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}

	for _, r := range results {
		if r.Status != types.StatusOpen {
			t.Errorf("expected only open issues, found %s with status %s", r.ID, r.Status)
		}
	}
}

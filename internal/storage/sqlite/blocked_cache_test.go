package sqlite

import (
	"context"
	"strconv"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// getCachedBlockedIssues returns the set of issue IDs currently in blocked_issues_cache
func getCachedBlockedIssues(t *testing.T, store *SQLiteStorage) map[string]bool {
	t.Helper()
	ctx := context.Background()

	rows, err := store.db.QueryContext(ctx, "SELECT issue_id FROM blocked_issues_cache")
	if err != nil {
		t.Fatalf("Failed to query blocked_issues_cache: %v", err)
	}
	defer rows.Close()

	cached := make(map[string]bool)
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			t.Fatalf("Failed to scan issue_id: %v", err)
		}
		cached[issueID] = true
	}

	return cached
}

// TestCacheInvalidationOnDependencyAdd tests that adding a blocking dependency updates the cache
func TestCacheInvalidationOnDependencyAdd(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create two issues: blocker and blocked
	blocker := &types.Issue{Title: "Blocker", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocked := &types.Issue{Title: "Blocked", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, blocker, "test-user")
	store.CreateIssue(ctx, blocked, "test-user")

	// Initially, cache should be empty (no blocked issues)
	cached := getCachedBlockedIssues(t, store)
	if len(cached) != 0 {
		t.Errorf("Expected empty cache initially, got %d issues", len(cached))
	}

	// Add blocking dependency
	dep := &types.Dependency{IssueID: blocked.ID, DependsOnID: blocker.ID, Type: types.DepBlocks}
	if err := store.AddDependency(ctx, dep, "test-user"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Verify blocked issue appears in cache
	cached = getCachedBlockedIssues(t, store)
	if !cached[blocked.ID] {
		t.Errorf("Expected %s to be in cache after adding blocking dependency", blocked.ID)
	}
	if cached[blocker.ID] {
		t.Errorf("Expected %s NOT to be in cache (it's the blocker, not blocked)", blocker.ID)
	}
}

// TestCacheInvalidationOnDependencyRemove tests that removing a blocking dependency updates the cache
func TestCacheInvalidationOnDependencyRemove(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create blocker → blocked relationship
	blocker := &types.Issue{Title: "Blocker", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocked := &types.Issue{Title: "Blocked", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, blocker, "test-user")
	store.CreateIssue(ctx, blocked, "test-user")

	dep := &types.Dependency{IssueID: blocked.ID, DependsOnID: blocker.ID, Type: types.DepBlocks}
	store.AddDependency(ctx, dep, "test-user")

	// Verify blocked issue is in cache
	cached := getCachedBlockedIssues(t, store)
	if !cached[blocked.ID] {
		t.Fatalf("Setup failed: expected %s in cache before removal", blocked.ID)
	}

	// Remove the blocking dependency
	if err := store.RemoveDependency(ctx, blocked.ID, blocker.ID, "test-user"); err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	// Verify blocked issue removed from cache
	cached = getCachedBlockedIssues(t, store)
	if cached[blocked.ID] {
		t.Errorf("Expected %s to be removed from cache after removing blocking dependency", blocked.ID)
	}
}

// TestCacheInvalidationOnStatusChange tests cache updates when blocker status changes
func TestCacheInvalidationOnStatusChange(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create blocker → blocked relationship
	blocker := &types.Issue{Title: "Blocker", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocked := &types.Issue{Title: "Blocked", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, blocker, "test-user")
	store.CreateIssue(ctx, blocked, "test-user")

	dep := &types.Dependency{IssueID: blocked.ID, DependsOnID: blocker.ID, Type: types.DepBlocks}
	store.AddDependency(ctx, dep, "test-user")

	// Initially blocked issue should be in cache
	cached := getCachedBlockedIssues(t, store)
	if !cached[blocked.ID] {
		t.Fatalf("Setup failed: expected %s in cache", blocked.ID)
	}

	// Close the blocker
	if err := store.CloseIssue(ctx, blocker.ID, "Done", "test-user", ""); err != nil {
		t.Fatalf("CloseIssue failed: %v", err)
	}

	// Verify blocked issue removed from cache (blocker is closed)
	cached = getCachedBlockedIssues(t, store)
	if cached[blocked.ID] {
		t.Errorf("Expected %s to be removed from cache after blocker closed", blocked.ID)
	}

	// Reopen the blocker
	updates := map[string]interface{}{"status": string(types.StatusOpen)}
	if err := store.UpdateIssue(ctx, blocker.ID, updates, "test-user"); err != nil {
		t.Fatalf("UpdateIssue (reopen) failed: %v", err)
	}

	// Verify blocked issue added back to cache
	cached = getCachedBlockedIssues(t, store)
	if !cached[blocked.ID] {
		t.Errorf("Expected %s to be added back to cache after blocker reopened", blocked.ID)
	}
}

// TestCacheConsistencyAcrossOperations tests cache stays consistent through multiple changes
func TestCacheConsistencyAcrossOperations(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple issues: blocker1, blocker2, blocked1, blocked2
	blocker1 := &types.Issue{Title: "Blocker 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocker2 := &types.Issue{Title: "Blocker 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocked1 := &types.Issue{Title: "Blocked 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocked2 := &types.Issue{Title: "Blocked 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, blocker1, "test-user")
	store.CreateIssue(ctx, blocker2, "test-user")
	store.CreateIssue(ctx, blocked1, "test-user")
	store.CreateIssue(ctx, blocked2, "test-user")

	// Operation 1: Add blocker1 → blocked1
	dep1 := &types.Dependency{IssueID: blocked1.ID, DependsOnID: blocker1.ID, Type: types.DepBlocks}
	store.AddDependency(ctx, dep1, "test-user")

	cached := getCachedBlockedIssues(t, store)
	if !cached[blocked1.ID] || cached[blocked2.ID] {
		t.Errorf("After op1: expected only blocked1 in cache, got: %v", cached)
	}

	// Operation 2: Add blocker2 → blocked2
	dep2 := &types.Dependency{IssueID: blocked2.ID, DependsOnID: blocker2.ID, Type: types.DepBlocks}
	store.AddDependency(ctx, dep2, "test-user")

	cached = getCachedBlockedIssues(t, store)
	if !cached[blocked1.ID] || !cached[blocked2.ID] {
		t.Errorf("After op2: expected both blocked1 and blocked2 in cache, got: %v", cached)
	}

	// Operation 3: Close blocker1
	store.CloseIssue(ctx, blocker1.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if cached[blocked1.ID] || !cached[blocked2.ID] {
		t.Errorf("After op3: expected only blocked2 in cache, got: %v", cached)
	}

	// Operation 4: Remove blocker2 → blocked2 dependency
	store.RemoveDependency(ctx, blocked2.ID, blocker2.ID, "test-user")

	cached = getCachedBlockedIssues(t, store)
	if len(cached) != 0 {
		t.Errorf("After op4: expected empty cache, got: %v", cached)
	}
}

// TestParentChildTransitiveBlocking tests that children of blocked parents appear in cache
func TestParentChildTransitiveBlocking(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create: blocker → epic → task1, task2
	blocker := &types.Issue{Title: "Blocker", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	epic := &types.Issue{Title: "Epic", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic}
	task1 := &types.Issue{Title: "Task 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	task2 := &types.Issue{Title: "Task 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, blocker, "test-user")
	store.CreateIssue(ctx, epic, "test-user")
	store.CreateIssue(ctx, task1, "test-user")
	store.CreateIssue(ctx, task2, "test-user")

	// Add blocking dependency: epic blocked by blocker
	depBlock := &types.Dependency{IssueID: epic.ID, DependsOnID: blocker.ID, Type: types.DepBlocks}
	store.AddDependency(ctx, depBlock, "test-user")

	// Add parent-child relationships: task1 and task2 are children of epic
	depChild1 := &types.Dependency{IssueID: task1.ID, DependsOnID: epic.ID, Type: types.DepParentChild}
	depChild2 := &types.Dependency{IssueID: task2.ID, DependsOnID: epic.ID, Type: types.DepParentChild}
	store.AddDependency(ctx, depChild1, "test-user")
	store.AddDependency(ctx, depChild2, "test-user")

	// Verify all children appear in cache (transitive blocking)
	cached := getCachedBlockedIssues(t, store)

	if !cached[epic.ID] {
		t.Errorf("Expected epic to be in cache (directly blocked)")
	}
	if !cached[task1.ID] {
		t.Errorf("Expected task1 to be in cache (parent is blocked)")
	}
	if !cached[task2.ID] {
		t.Errorf("Expected task2 to be in cache (parent is blocked)")
	}
	if cached[blocker.ID] {
		t.Errorf("Expected blocker NOT to be in cache (it's the blocker)")
	}

	// Expected: exactly 3 blocked issues (epic, task1, task2)
	if len(cached) != 3 {
		t.Errorf("Expected 3 blocked issues in cache, got %d: %v", len(cached), cached)
	}
}

// TestRelatedDepsDoNotAffectCache tests that 'related' dependencies don't cause cache entries
func TestRelatedDepsDoNotAffectCache(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	issue1 := &types.Issue{Title: "Issue 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Issue 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Add 'related' dependency (should NOT cause blocking)
	dep := &types.Dependency{IssueID: issue2.ID, DependsOnID: issue1.ID, Type: types.DepRelated}
	store.AddDependency(ctx, dep, "test-user")

	// Cache should be empty (related deps don't block)
	cached := getCachedBlockedIssues(t, store)
	if len(cached) != 0 {
		t.Errorf("Expected empty cache (related deps don't block), got: %v", cached)
	}
}

// TestDeepHierarchyCacheCorrectness tests cache handles deep parent-child hierarchies
func TestDeepHierarchyCacheCorrectness(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create blocker → level0 → level1 → level2 → level3
	blocker := &types.Issue{Title: "Blocker", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	store.CreateIssue(ctx, blocker, "test-user")

	var issues []*types.Issue
	for i := 0; i < 4; i++ {
		issue := &types.Issue{
			Title:     "Level " + strconv.Itoa(i),
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeEpic,
		}
		store.CreateIssue(ctx, issue, "test-user")
		issues = append(issues, issue)

		if i == 0 {
			// First level: blocked by blocker
			dep := &types.Dependency{IssueID: issue.ID, DependsOnID: blocker.ID, Type: types.DepBlocks}
			store.AddDependency(ctx, dep, "test-user")
		} else {
			// Each subsequent level: child of previous level
			dep := &types.Dependency{IssueID: issue.ID, DependsOnID: issues[i-1].ID, Type: types.DepParentChild}
			store.AddDependency(ctx, dep, "test-user")
		}
	}

	// Verify all 4 levels are in cache
	cached := getCachedBlockedIssues(t, store)
	if len(cached) != 4 {
		t.Errorf("Expected 4 blocked issues in cache, got %d", len(cached))
	}

	for i, issue := range issues {
		if !cached[issue.ID] {
			t.Errorf("Expected level %d (issue %s) to be in cache", i, issue.ID)
		}
	}

	// Close the blocker and verify all become unblocked
	store.CloseIssue(ctx, blocker.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if len(cached) != 0 {
		t.Errorf("Expected empty cache after closing blocker, got %d issues: %v", len(cached), cached)
	}
}

// TestMultipleBlockersInCache tests issue blocked by multiple blockers
func TestMultipleBlockersInCache(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create blocker1 → blocked ← blocker2
	blocker1 := &types.Issue{Title: "Blocker 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocker2 := &types.Issue{Title: "Blocker 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	blocked := &types.Issue{Title: "Blocked", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, blocker1, "test-user")
	store.CreateIssue(ctx, blocker2, "test-user")
	store.CreateIssue(ctx, blocked, "test-user")

	// Add both blocking dependencies
	dep1 := &types.Dependency{IssueID: blocked.ID, DependsOnID: blocker1.ID, Type: types.DepBlocks}
	dep2 := &types.Dependency{IssueID: blocked.ID, DependsOnID: blocker2.ID, Type: types.DepBlocks}
	store.AddDependency(ctx, dep1, "test-user")
	store.AddDependency(ctx, dep2, "test-user")

	// Verify blocked issue appears in cache
	cached := getCachedBlockedIssues(t, store)
	if !cached[blocked.ID] {
		t.Errorf("Expected %s to be in cache (blocked by 2 issues)", blocked.ID)
	}

	// Close one blocker - should still be blocked
	store.CloseIssue(ctx, blocker1.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if !cached[blocked.ID] {
		t.Errorf("Expected %s to still be in cache (still blocked by blocker2)", blocked.ID)
	}

	// Close the second blocker - should be unblocked
	store.CloseIssue(ctx, blocker2.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if cached[blocked.ID] {
		t.Errorf("Expected %s to be removed from cache (both blockers closed)", blocked.ID)
	}
}

// TestConditionalBlocksCache tests the conditional-blocks dependency type (bd-kzda)
// B runs only if A fails. B is blocked until A is closed with a failure close reason.
func TestConditionalBlocksCache(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create A (potential failure) -> B (conditional on A's failure)
	issueA := &types.Issue{Title: "Issue A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issueB := &types.Issue{Title: "Issue B (runs if A fails)", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issueA, "test-user")
	store.CreateIssue(ctx, issueB, "test-user")

	// Add conditional-blocks dependency: B depends on A failing
	dep := &types.Dependency{IssueID: issueB.ID, DependsOnID: issueA.ID, Type: types.DepConditionalBlocks}
	store.AddDependency(ctx, dep, "test-user")

	// Initially: A is open, so B should be blocked
	cached := getCachedBlockedIssues(t, store)
	if !cached[issueB.ID] {
		t.Errorf("Expected %s to be blocked (A is still open)", issueB.ID)
	}

	// Close A with SUCCESS (no failure keywords) - B should STILL be blocked
	store.CloseIssue(ctx, issueA.ID, "Completed successfully", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if !cached[issueB.ID] {
		t.Errorf("Expected %s to be blocked (A succeeded, condition not met)", issueB.ID)
	}

	// Reopen A
	store.UpdateIssue(ctx, issueA.ID, map[string]interface{}{"status": types.StatusOpen}, "test-user")

	// Close A with FAILURE - B should now be UNBLOCKED
	store.CloseIssue(ctx, issueA.ID, "Task failed due to timeout", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if cached[issueB.ID] {
		t.Errorf("Expected %s to be unblocked (A failed, condition met)", issueB.ID)
	}
}

// TestConditionalBlocksVariousFailureKeywords tests that various failure keywords unlock B
func TestConditionalBlocksVariousFailureKeywords(t *testing.T) {
	failureReasons := []string{
		"failed",
		"rejected",
		"wontfix",
		"won't fix",
		"cancelled",
		"canceled",
		"abandoned",
		"blocked",
		"error",
		"timeout",
		"aborted",
	}

	for _, reason := range failureReasons {
		t.Run(reason, func(t *testing.T) {
			store, cleanup := setupTestDB(t)
			defer cleanup()
			ctx := context.Background()

			issueA := &types.Issue{Title: "Issue A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
			issueB := &types.Issue{Title: "Issue B", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

			store.CreateIssue(ctx, issueA, "test-user")
			store.CreateIssue(ctx, issueB, "test-user")

			dep := &types.Dependency{IssueID: issueB.ID, DependsOnID: issueA.ID, Type: types.DepConditionalBlocks}
			store.AddDependency(ctx, dep, "test-user")

			// Close A with failure reason
			store.CloseIssue(ctx, issueA.ID, "Closed: "+reason, "test-user", "")

			cached := getCachedBlockedIssues(t, store)
			if cached[issueB.ID] {
				t.Errorf("Expected B to be unblocked after A closed with '%s'", reason)
			}
		})
	}
}

// TestWaitsForAllChildren tests the waits-for dependency with all-children gate (bd-xo1o.2)
// B waits for spawner A's children. B is blocked until ALL children of A are closed.
func TestWaitsForAllChildren(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create spawner (A) with two children (C1, C2), and waiter (B) that waits for A's children
	spawner := &types.Issue{Title: "Spawner", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	child1 := &types.Issue{Title: "Child 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	child2 := &types.Issue{Title: "Child 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	waiter := &types.Issue{Title: "Waiter", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, spawner, "test-user")
	store.CreateIssue(ctx, child1, "test-user")
	store.CreateIssue(ctx, child2, "test-user")
	store.CreateIssue(ctx, waiter, "test-user")

	// Add parent-child relationships: C1, C2 are children of spawner
	depChild1 := &types.Dependency{IssueID: child1.ID, DependsOnID: spawner.ID, Type: types.DepParentChild}
	depChild2 := &types.Dependency{IssueID: child2.ID, DependsOnID: spawner.ID, Type: types.DepParentChild}
	store.AddDependency(ctx, depChild1, "test-user")
	store.AddDependency(ctx, depChild2, "test-user")

	// Add waits-for dependency: waiter waits for spawner's children (default: all-children gate)
	depWaits := &types.Dependency{
		IssueID:     waiter.ID,
		DependsOnID: spawner.ID,
		Type:        types.DepWaitsFor,
		Metadata:    `{"gate":"all-children"}`,
	}
	store.AddDependency(ctx, depWaits, "test-user")

	// Initially: both children open, waiter should be blocked
	cached := getCachedBlockedIssues(t, store)
	if !cached[waiter.ID] {
		t.Errorf("Expected waiter to be blocked (children still open)")
	}

	// Close first child - waiter should still be blocked (second child still open)
	store.CloseIssue(ctx, child1.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if !cached[waiter.ID] {
		t.Errorf("Expected waiter to still be blocked (one child still open)")
	}

	// Close second child - waiter should now be unblocked
	store.CloseIssue(ctx, child2.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if cached[waiter.ID] {
		t.Errorf("Expected waiter to be unblocked (all children closed)")
	}
}

// TestWaitsForAnyChildren tests the waits-for dependency with any-children gate
// B waits for spawner A's children. B is blocked until ANY child of A is closed.
func TestWaitsForAnyChildren(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create spawner with two children, and waiter that waits for any child to close
	spawner := &types.Issue{Title: "Spawner", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	child1 := &types.Issue{Title: "Child 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	child2 := &types.Issue{Title: "Child 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	waiter := &types.Issue{Title: "Waiter", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, spawner, "test-user")
	store.CreateIssue(ctx, child1, "test-user")
	store.CreateIssue(ctx, child2, "test-user")
	store.CreateIssue(ctx, waiter, "test-user")

	// Add parent-child relationships
	depChild1 := &types.Dependency{IssueID: child1.ID, DependsOnID: spawner.ID, Type: types.DepParentChild}
	depChild2 := &types.Dependency{IssueID: child2.ID, DependsOnID: spawner.ID, Type: types.DepParentChild}
	store.AddDependency(ctx, depChild1, "test-user")
	store.AddDependency(ctx, depChild2, "test-user")

	// Add waits-for dependency with any-children gate
	depWaits := &types.Dependency{
		IssueID:     waiter.ID,
		DependsOnID: spawner.ID,
		Type:        types.DepWaitsFor,
		Metadata:    `{"gate":"any-children"}`,
	}
	store.AddDependency(ctx, depWaits, "test-user")

	// Initially: both children open, waiter should be blocked
	cached := getCachedBlockedIssues(t, store)
	if !cached[waiter.ID] {
		t.Errorf("Expected waiter to be blocked (no children closed yet)")
	}

	// Close first child - waiter should now be unblocked (any-children gate satisfied)
	store.CloseIssue(ctx, child1.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if cached[waiter.ID] {
		t.Errorf("Expected waiter to be unblocked (any-children gate: one child closed)")
	}
}

// TestWaitsForNoChildren tests waits-for when spawner has no children
// Should be unblocked immediately (vacuous truth: all 0 children are closed)
func TestWaitsForNoChildren(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create spawner with no children
	spawner := &types.Issue{Title: "Spawner", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	waiter := &types.Issue{Title: "Waiter", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, spawner, "test-user")
	store.CreateIssue(ctx, waiter, "test-user")

	// Add waits-for dependency (no children to wait for)
	depWaits := &types.Dependency{
		IssueID:     waiter.ID,
		DependsOnID: spawner.ID,
		Type:        types.DepWaitsFor,
		Metadata:    `{"gate":"all-children"}`,
	}
	store.AddDependency(ctx, depWaits, "test-user")

	// Waiter should NOT be blocked (no children means condition is satisfied)
	cached := getCachedBlockedIssues(t, store)
	if cached[waiter.ID] {
		t.Errorf("Expected waiter to NOT be blocked (spawner has no children)")
	}
}

// TestWaitsForDynamicChildrenAdded tests waits-for when children are added dynamically
func TestWaitsForDynamicChildrenAdded(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create spawner with no children initially
	spawner := &types.Issue{Title: "Spawner", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	waiter := &types.Issue{Title: "Waiter", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, spawner, "test-user")
	store.CreateIssue(ctx, waiter, "test-user")

	// Add waits-for dependency
	depWaits := &types.Dependency{
		IssueID:     waiter.ID,
		DependsOnID: spawner.ID,
		Type:        types.DepWaitsFor,
		Metadata:    `{"gate":"all-children"}`,
	}
	store.AddDependency(ctx, depWaits, "test-user")

	// Initially: no children, waiter should be unblocked
	cached := getCachedBlockedIssues(t, store)
	if cached[waiter.ID] {
		t.Errorf("Expected waiter to be unblocked (no children yet)")
	}

	// Dynamically add a child
	child := &types.Issue{Title: "Dynamic Child", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	store.CreateIssue(ctx, child, "test-user")
	depChild := &types.Dependency{IssueID: child.ID, DependsOnID: spawner.ID, Type: types.DepParentChild}
	store.AddDependency(ctx, depChild, "test-user")

	// Now waiter should be blocked (child is open)
	cached = getCachedBlockedIssues(t, store)
	if !cached[waiter.ID] {
		t.Errorf("Expected waiter to be blocked (dynamic child added)")
	}

	// Close the child - waiter should be unblocked again
	store.CloseIssue(ctx, child.ID, "Done", "test-user", "")

	cached = getCachedBlockedIssues(t, store)
	if cached[waiter.ID] {
		t.Errorf("Expected waiter to be unblocked (dynamic child closed)")
	}
}

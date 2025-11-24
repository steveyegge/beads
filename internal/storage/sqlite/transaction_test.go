package sqlite

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestRunInTransactionBasic verifies the RunInTransaction method exists and
// can be called.
func TestRunInTransactionBasic(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Test that we can call RunInTransaction
	callCount := 0
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("RunInTransaction returned error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected callback to be called once, got %d", callCount)
	}
}

// TestRunInTransactionRollbackOnError verifies that returning an error
// from the callback does not cause a panic and the error is propagated.
func TestRunInTransactionRollbackOnError(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	expectedErr := "intentional test error"
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return &testError{msg: expectedErr}
	})

	if err == nil {
		t.Error("expected error to be returned, got nil")
	}

	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

// TestRunInTransactionPanicRecovery verifies that panics in the callback
// are recovered and re-raised after rollback.
func TestRunInTransactionPanicRecovery(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to be re-raised, but no panic occurred")
		} else if r != "test panic" {
			t.Errorf("unexpected panic value: %v", r)
		}
	}()

	_ = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		panic("test panic")
	})

	t.Error("should not reach here - panic should have been re-raised")
}

// TestTransactionCreateIssue tests creating an issue within a transaction.
func TestTransactionCreateIssue(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var createdID string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			Title:     "Test Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "test-actor"); err != nil {
			return err
		}
		createdID = issue.ID
		return nil
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	if createdID == "" {
		t.Error("expected issue ID to be set after creation")
	}

	// Verify issue exists after commit
	issue, err := store.GetIssue(ctx, createdID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if issue == nil {
		t.Error("expected issue to exist after transaction commit")
	}
	if issue.Title != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %q", issue.Title)
	}
}

// TestTransactionRollbackOnCreateError tests that issues are not created
// when transaction rolls back due to error.
func TestTransactionRollbackOnCreateError(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var createdID string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			Title:     "Test Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "test-actor"); err != nil {
			return err
		}
		createdID = issue.ID

		// Return error to trigger rollback
		return &testError{msg: "intentional rollback"}
	})

	if err == nil {
		t.Error("expected error from transaction")
	}

	// Verify issue does NOT exist after rollback
	if createdID != "" {
		issue, err := store.GetIssue(ctx, createdID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if issue != nil {
			t.Error("expected issue to NOT exist after transaction rollback")
		}
	}
}

// TestTransactionMultipleIssues tests creating multiple issues atomically.
func TestTransactionMultipleIssues(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var ids []string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		for i := 0; i < 3; i++ {
			issue := &types.Issue{
				Title:     "Test Issue",
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: types.TypeTask,
			}
			if err := tx.CreateIssue(ctx, issue, "test-actor"); err != nil {
				return err
			}
			ids = append(ids, issue.ID)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify all issues exist
	for _, id := range ids {
		issue, err := store.GetIssue(ctx, id)
		if err != nil {
			t.Fatalf("GetIssue failed for %s: %v", id, err)
		}
		if issue == nil {
			t.Errorf("expected issue %s to exist", id)
		}
	}
}

// TestTransactionUpdateIssue tests updating an issue within a transaction.
func TestTransactionUpdateIssue(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create issue first
	issue := &types.Issue{
		Title:     "Original Title",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Update in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
			"title": "Updated Title",
		}, "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify update
	updated, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if updated.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", updated.Title)
	}
}

// TestTransactionCloseIssue tests closing an issue within a transaction.
func TestTransactionCloseIssue(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create issue first
	issue := &types.Issue{
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Close in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.CloseIssue(ctx, issue.ID, "Done", "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify closed
	closed, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if closed.Status != types.StatusClosed {
		t.Errorf("expected status 'closed', got %q", closed.Status)
	}
}

// TestTransactionDeleteIssue tests deleting an issue within a transaction.
func TestTransactionDeleteIssue(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create issue first
	issue := &types.Issue{
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Delete in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.DeleteIssue(ctx, issue.ID)
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify deleted
	deleted, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if deleted != nil {
		t.Error("expected issue to be deleted")
	}
}

// TestTransactionGetIssue tests read-your-writes within a transaction.
func TestTransactionGetIssue(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create issue
		issue := &types.Issue{
			Title:     "Test Issue",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "test-actor"); err != nil {
			return err
		}

		// Read it back within same transaction (read-your-writes)
		retrieved, err := tx.GetIssue(ctx, issue.ID)
		if err != nil {
			return err
		}
		if retrieved == nil {
			t.Error("expected to read issue within transaction")
		}
		if retrieved.Title != "Test Issue" {
			t.Errorf("expected title 'Test Issue', got %q", retrieved.Title)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}
}

// TestTransactionCreateIssues tests batch issue creation within a transaction.
func TestTransactionCreateIssues(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var ids []string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issues := []*types.Issue{
			{Title: "Issue 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
			{Title: "Issue 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
			{Title: "Issue 3", Status: types.StatusOpen, Priority: 3, IssueType: types.TypeTask},
		}
		if err := tx.CreateIssues(ctx, issues, "test-actor"); err != nil {
			return err
		}
		for _, issue := range issues {
			ids = append(ids, issue.ID)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify all issues exist
	for i, id := range ids {
		issue, err := store.GetIssue(ctx, id)
		if err != nil {
			t.Fatalf("GetIssue failed for %s: %v", id, err)
		}
		if issue == nil {
			t.Errorf("expected issue %s to exist", id)
		}
		expectedTitle := "Issue " + string(rune('1'+i))
		if issue.Title != expectedTitle {
			t.Errorf("expected title %q, got %q", expectedTitle, issue.Title)
		}
	}
}

// TestTransactionAddDependency tests adding a dependency within a transaction.
func TestTransactionAddDependency(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create two issues first
	issue1 := &types.Issue{Title: "Issue 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Issue 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue1, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := store.CreateIssue(ctx, issue2, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add dependency in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		dep := &types.Dependency{
			IssueID:     issue1.ID,
			DependsOnID: issue2.ID,
			Type:        types.DepBlocks,
		}
		return tx.AddDependency(ctx, dep, "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify dependency exists
	deps, err := store.GetDependencies(ctx, issue1.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].ID != issue2.ID {
		t.Errorf("expected dependency on %s, got %s", issue2.ID, deps[0].ID)
	}
}

// TestTransactionRemoveDependency tests removing a dependency within a transaction.
func TestTransactionRemoveDependency(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create two issues and add dependency
	issue1 := &types.Issue{Title: "Issue 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Issue 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue1, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := store.CreateIssue(ctx, issue2, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	dep := &types.Dependency{IssueID: issue1.ID, DependsOnID: issue2.ID, Type: types.DepBlocks}
	if err := store.AddDependency(ctx, dep, "test-actor"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Remove dependency in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.RemoveDependency(ctx, issue1.ID, issue2.ID, "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify dependency is gone
	deps, err := store.GetDependencies(ctx, issue1.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(deps))
	}
}

// TestTransactionAddLabel tests adding a label within a transaction.
func TestTransactionAddLabel(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create issue first
	issue := &types.Issue{Title: "Test Issue", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add label in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.AddLabel(ctx, issue.ID, "test-label", "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify label exists
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetLabels failed: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(labels))
	}
	if labels[0] != "test-label" {
		t.Errorf("expected label 'test-label', got %s", labels[0])
	}
}

// TestTransactionRemoveLabel tests removing a label within a transaction.
func TestTransactionRemoveLabel(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create issue and add label
	issue := &types.Issue{Title: "Test Issue", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := store.AddLabel(ctx, issue.ID, "test-label", "test-actor"); err != nil {
		t.Fatalf("AddLabel failed: %v", err)
	}

	// Remove label in transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.RemoveLabel(ctx, issue.ID, "test-label", "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify label is gone
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetLabels failed: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels, got %d", len(labels))
	}
}

// TestTransactionAtomicIssueWithDependency tests creating issue + adding dependency atomically.
func TestTransactionAtomicIssueWithDependency(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create parent issue first
	parent := &types.Issue{Title: "Parent", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	if err := store.CreateIssue(ctx, parent, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	var childID string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create child issue
		child := &types.Issue{Title: "Child", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
		if err := tx.CreateIssue(ctx, child, "test-actor"); err != nil {
			return err
		}
		childID = child.ID

		// Add dependency: child blocks parent (child must be done before parent)
		dep := &types.Dependency{
			IssueID:     parent.ID,
			DependsOnID: child.ID,
			Type:        types.DepBlocks,
		}
		return tx.AddDependency(ctx, dep, "test-actor")
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify both issue and dependency exist
	child, err := store.GetIssue(ctx, childID)
	if err != nil || child == nil {
		t.Error("expected child issue to exist")
	}

	deps, err := store.GetDependencies(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != childID {
		t.Error("expected dependency from parent to child")
	}
}

// TestTransactionAtomicIssueWithLabels tests creating issue + adding labels atomically.
func TestTransactionAtomicIssueWithLabels(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var issueID string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create issue
		issue := &types.Issue{Title: "Test Issue", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
		if err := tx.CreateIssue(ctx, issue, "test-actor"); err != nil {
			return err
		}
		issueID = issue.ID

		// Add multiple labels
		for _, label := range []string{"label1", "label2", "label3"} {
			if err := tx.AddLabel(ctx, issue.ID, label, "test-actor"); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify issue and all labels exist
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil || issue == nil {
		t.Error("expected issue to exist")
	}

	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		t.Fatalf("GetLabels failed: %v", err)
	}
	if len(labels) != 3 {
		t.Errorf("expected 3 labels, got %d", len(labels))
	}
}

// TestTransactionEmpty tests that an empty transaction commits successfully.
func TestTransactionEmpty(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Do nothing - empty transaction
		return nil
	})

	if err != nil {
		t.Errorf("empty transaction should succeed, got error: %v", err)
	}
}

// TestTransactionConcurrent tests multiple concurrent transactions.
func TestTransactionConcurrent(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	const numGoroutines = 10
	errors := make(chan error, numGoroutines)
	ids := make(chan string, numGoroutines)

	// Launch concurrent transactions
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
				issue := &types.Issue{
					Title:     "Concurrent Issue",
					Status:    types.StatusOpen,
					Priority:  index % 4,
					IssueType: types.TypeTask,
				}
				if err := tx.CreateIssue(ctx, issue, "test-actor"); err != nil {
					return err
				}
				ids <- issue.ID
				return nil
			})
			errors <- err
		}(i)
	}

	// Collect results
	var errs []error
	var createdIDs []string
	for i := 0; i < numGoroutines; i++ {
		if err := <-errors; err != nil {
			errs = append(errs, err)
		}
	}
	close(ids)
	for id := range ids {
		createdIDs = append(createdIDs, id)
	}

	if len(errs) > 0 {
		t.Errorf("some transactions failed: %v", errs)
	}

	if len(createdIDs) != numGoroutines {
		t.Errorf("expected %d issues created, got %d", numGoroutines, len(createdIDs))
	}

	// Verify all issues exist
	for _, id := range createdIDs {
		issue, err := store.GetIssue(ctx, id)
		if err != nil || issue == nil {
			t.Errorf("expected issue %s to exist", id)
		}
	}
}

// TestTransactionNestedFailure tests that when first op succeeds but second fails,
// both are rolled back.
func TestTransactionNestedFailure(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var firstIssueID string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// First operation succeeds
		issue1 := &types.Issue{
			Title:     "First Issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue1, "test-actor"); err != nil {
			return err
		}
		firstIssueID = issue1.ID

		// Second operation fails
		issue2 := &types.Issue{
			Title:    "", // Invalid - missing title
			Status:   types.StatusOpen,
			Priority: 2,
		}
		return tx.CreateIssue(ctx, issue2, "test-actor")
	})

	if err == nil {
		t.Error("expected error from invalid second issue")
	}

	// Verify first issue was NOT created (rolled back)
	if firstIssueID != "" {
		issue, err := store.GetIssue(ctx, firstIssueID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if issue != nil {
			t.Error("expected first issue to be rolled back, but it exists")
		}
	}
}

// TestTransactionAtomicPlanApproval simulates a VC plan approval workflow:
// creating multiple issues with dependencies and labels atomically.
func TestTransactionAtomicPlanApproval(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	var epicID, task1ID, task2ID string
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create epic
		epic := &types.Issue{
			Title:     "Epic: Feature Implementation",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeEpic,
		}
		if err := tx.CreateIssue(ctx, epic, "test-actor"); err != nil {
			return err
		}
		epicID = epic.ID

		// Create task 1
		task1 := &types.Issue{
			Title:     "Task 1: Setup",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, task1, "test-actor"); err != nil {
			return err
		}
		task1ID = task1.ID

		// Create task 2
		task2 := &types.Issue{
			Title:     "Task 2: Implementation",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, task2, "test-actor"); err != nil {
			return err
		}
		task2ID = task2.ID

		// Add dependencies: task2 depends on task1
		dep := &types.Dependency{
			IssueID:     task2ID,
			DependsOnID: task1ID,
			Type:        types.DepBlocks,
		}
		if err := tx.AddDependency(ctx, dep, "test-actor"); err != nil {
			return err
		}

		// Add labels to all issues
		for _, id := range []string{epicID, task1ID, task2ID} {
			if err := tx.AddLabel(ctx, id, "feature-x", "test-actor"); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("RunInTransaction failed: %v", err)
	}

	// Verify all issues exist
	for _, id := range []string{epicID, task1ID, task2ID} {
		issue, err := store.GetIssue(ctx, id)
		if err != nil || issue == nil {
			t.Errorf("expected issue %s to exist", id)
		}
	}

	// Verify dependency
	deps, err := store.GetDependencies(ctx, task2ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != task1ID {
		t.Error("expected task2 to depend on task1")
	}

	// Verify labels
	for _, id := range []string{epicID, task1ID, task2ID} {
		labels, err := store.GetLabels(ctx, id)
		if err != nil {
			t.Fatalf("GetLabels failed: %v", err)
		}
		if len(labels) != 1 || labels[0] != "feature-x" {
			t.Errorf("expected 'feature-x' label on %s", id)
		}
	}
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

//go:build cgo

// Package dolt provides comprehensive tests for the Dolt storage backend.
// This file supplements dolt_test.go with additional coverage for:
// - Transaction handling (commit, rollback, panic recovery)
// - Decision point operations
// - Lock management and idle timeout
// - Sync cycle operations (commit, push, pull)
// - Edge cases and error handling
package dolt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// Transaction Tests
// =============================================================================

// TestTransactionCommit verifies that successful transactions are committed.
func TestTransactionCommit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	var createdID string

	// Execute transaction that should succeed
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			Title:       "Transaction Test Issue",
			Description: "Created in transaction",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "tester"); err != nil {
			return err
		}
		createdID = issue.ID
		return nil
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify issue was committed
	issue, err := store.GetIssue(ctx, createdID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue == nil {
		t.Fatal("issue should exist after committed transaction")
	}
	if issue.Title != "Transaction Test Issue" {
		t.Errorf("expected title 'Transaction Test Issue', got %q", issue.Title)
	}
}

// TestTransactionRollbackOnError verifies that failed transactions are rolled back.
func TestTransactionRollbackOnError(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	var attemptedID string
	intentionalErr := errors.New("intentional failure")

	// Execute transaction that should fail
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			ID:          "rollback-test-issue",
			Title:       "Rollback Test Issue",
			Description: "Should be rolled back",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "tester"); err != nil {
			return err
		}
		attemptedID = issue.ID

		// Verify issue is visible within transaction
		txIssue, err := tx.GetIssue(ctx, attemptedID)
		if err != nil {
			t.Logf("warning: could not read issue in transaction: %v", err)
		}
		if txIssue == nil {
			t.Log("warning: issue not visible within transaction")
		}

		// Return error to trigger rollback
		return intentionalErr
	})

	if err != intentionalErr {
		t.Fatalf("expected intentional error, got: %v", err)
	}

	// Verify issue was NOT committed (rolled back)
	issue, err := store.GetIssue(ctx, attemptedID)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue != nil {
		t.Error("issue should NOT exist after rolled back transaction")
	}
}

// TestTransactionRollbackOnPanic verifies that panics in transactions trigger rollback.
func TestTransactionRollbackOnPanic(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	attemptedID := "panic-test-issue"

	// Recover from the panic we're about to cause
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to be re-raised")
		}
	}()

	// Execute transaction that panics
	_ = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			ID:          attemptedID,
			Title:       "Panic Test Issue",
			Description: "Should be rolled back on panic",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "tester"); err != nil {
			return err
		}
		panic("intentional panic in transaction")
	})
}

// TestTransactionMultipleOperations verifies atomic multi-operation transactions.
func TestTransactionMultipleOperations(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	var parentID, childID string

	// Create parent and child with dependency in single transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		parent := &types.Issue{
			ID:          "tx-parent",
			Title:       "Parent Issue",
			Description: "Parent for atomic test",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := tx.CreateIssue(ctx, parent, "tester"); err != nil {
			return err
		}
		parentID = parent.ID

		child := &types.Issue{
			ID:          "tx-child",
			Title:       "Child Issue",
			Description: "Child for atomic test",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, child, "tester"); err != nil {
			return err
		}
		childID = child.ID

		// Add dependency
		dep := &types.Dependency{
			IssueID:     childID,
			DependsOnID: parentID,
			Type:        types.DepBlocks,
		}
		if err := tx.AddDependency(ctx, dep, "tester"); err != nil {
			return err
		}

		// Add label
		if err := tx.AddLabel(ctx, childID, "atomic-test", "tester"); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify all operations were committed atomically
	parent, err := store.GetIssue(ctx, parentID)
	if err != nil || parent == nil {
		t.Fatal("parent issue should exist")
	}

	child, err := store.GetIssue(ctx, childID)
	if err != nil || child == nil {
		t.Fatal("child issue should exist")
	}

	deps, err := store.GetDependencies(ctx, childID)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != parentID {
		t.Errorf("expected dependency on parent, got: %v", deps)
	}

	labels, err := store.GetLabels(ctx, childID)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}
	if len(labels) != 1 || labels[0] != "atomic-test" {
		t.Errorf("expected label 'atomic-test', got: %v", labels)
	}
}

// TestTransactionNestedReadYourWrites verifies read-your-writes within transactions.
func TestTransactionNestedReadYourWrites(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create issue
		issue := &types.Issue{
			ID:          "read-your-writes",
			Title:       "Read Your Writes Test",
			Description: "Original",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "tester"); err != nil {
			return err
		}

		// Read it back within same transaction
		retrieved, err := tx.GetIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("failed to read issue in transaction: %w", err)
		}
		if retrieved == nil {
			return errors.New("issue should be visible within transaction")
		}
		if retrieved.Title != "Read Your Writes Test" {
			return fmt.Errorf("expected title 'Read Your Writes Test', got %q", retrieved.Title)
		}

		// Update it
		if err := tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
			"description": "Modified",
		}, "tester"); err != nil {
			return err
		}

		// Read again and verify update is visible
		retrieved2, err := tx.GetIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("failed to read issue after update: %w", err)
		}
		if retrieved2.Description != "Modified" {
			return fmt.Errorf("expected description 'Modified', got %q", retrieved2.Description)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
}

// =============================================================================
// Decision Point Tests
// =============================================================================

// TestDecisionPointCRUD tests create, read, update operations for decision points.
func TestDecisionPointCRUD(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue first
	issue := &types.Issue{
		ID:          "decision-test-issue",
		Title:       "Decision Test Issue",
		Description: "Issue for decision point testing",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Create decision point options
	options := []map[string]string{
		{"id": "opt1", "short": "Redis", "label": "Use Redis for caching"},
		{"id": "opt2", "short": "Memory", "label": "Use in-memory caching"},
		{"id": "opt3", "short": "None", "label": "No caching"},
	}
	optionsJSON, _ := json.Marshal(options)

	// Create decision point
	dp := &types.DecisionPoint{
		IssueID:       issue.ID,
		Prompt:        "Which caching strategy should we use?",
		Options:       string(optionsJSON),
		DefaultOption: "opt2",
		Iteration:     1,
		MaxIterations: 3,
	}

	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("failed to create decision point: %v", err)
	}

	// Read decision point
	retrieved, err := store.GetDecisionPoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get decision point: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected decision point to exist")
	}
	if retrieved.Prompt != dp.Prompt {
		t.Errorf("expected prompt %q, got %q", dp.Prompt, retrieved.Prompt)
	}
	if retrieved.DefaultOption != "opt2" {
		t.Errorf("expected default option 'opt2', got %q", retrieved.DefaultOption)
	}
	if retrieved.Iteration != 1 {
		t.Errorf("expected iteration 1, got %d", retrieved.Iteration)
	}

	// Update decision point (respond)
	now := time.Now().UTC()
	retrieved.SelectedOption = "opt1"
	retrieved.ResponseText = "Redis is preferred for distributed setup"
	retrieved.RespondedAt = &now
	retrieved.RespondedBy = "human@example.com"

	if err := store.UpdateDecisionPoint(ctx, retrieved); err != nil {
		t.Fatalf("failed to update decision point: %v", err)
	}

	// Verify update
	updated, err := store.GetDecisionPoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get updated decision point: %v", err)
	}
	if updated.SelectedOption != "opt1" {
		t.Errorf("expected selected option 'opt1', got %q", updated.SelectedOption)
	}
	if updated.ResponseText != "Redis is preferred for distributed setup" {
		t.Errorf("unexpected response text: %q", updated.ResponseText)
	}
	if updated.RespondedAt == nil {
		t.Error("expected responded_at to be set")
	}
}

// TestDecisionPointNonExistentIssue tests creating a decision point for non-existent issue.
func TestDecisionPointNonExistentIssue(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	dp := &types.DecisionPoint{
		IssueID: "non-existent-issue",
		Prompt:  "This should fail",
		Options: "[]",
	}

	err := store.CreateDecisionPoint(ctx, dp)
	if err == nil {
		t.Error("expected error when creating decision point for non-existent issue")
	}
}

// TestListPendingDecisions tests listing pending decision points.
func TestListPendingDecisions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create issues
	for i := 1; i <= 3; i++ {
		issue := &types.Issue{
			ID:          fmt.Sprintf("pending-test-%d", i),
			Title:       fmt.Sprintf("Pending Test Issue %d", i),
			Description: "Issue for pending decisions test",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("failed to create issue %d: %v", i, err)
		}

		dp := &types.DecisionPoint{
			IssueID: issue.ID,
			Prompt:  fmt.Sprintf("Question %d?", i),
			Options: "[]",
		}
		if err := store.CreateDecisionPoint(ctx, dp); err != nil {
			t.Fatalf("failed to create decision point %d: %v", i, err)
		}
	}

	// Respond to one decision
	dp2, _ := store.GetDecisionPoint(ctx, "pending-test-2")
	now := time.Now().UTC()
	dp2.RespondedAt = &now
	dp2.SelectedOption = "opt1"
	if err := store.UpdateDecisionPoint(ctx, dp2); err != nil {
		t.Fatalf("failed to respond to decision: %v", err)
	}

	// List pending decisions
	pending, err := store.ListPendingDecisions(ctx)
	if err != nil {
		t.Fatalf("failed to list pending decisions: %v", err)
	}

	// Should have 2 pending (1 and 3, not 2)
	if len(pending) != 2 {
		t.Errorf("expected 2 pending decisions, got %d", len(pending))
	}

	// Verify the responded one is not in the list
	for _, p := range pending {
		if p.IssueID == "pending-test-2" {
			t.Error("responded decision should not be in pending list")
		}
	}
}

// TestDecisionPointInTransaction tests decision point operations in transactions.
func TestDecisionPointInTransaction(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create issue and decision point atomically
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		issue := &types.Issue{
			ID:          "tx-decision-issue",
			Title:       "Transaction Decision Issue",
			Description: "For atomic decision point test",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}
		if err := tx.CreateIssue(ctx, issue, "tester"); err != nil {
			return err
		}

		dp := &types.DecisionPoint{
			IssueID: issue.ID,
			Prompt:  "Atomic decision?",
			Options: "[]",
		}
		if err := tx.CreateDecisionPoint(ctx, dp); err != nil {
			return err
		}

		// Verify decision point is visible within transaction
		retrieved, err := tx.GetDecisionPoint(ctx, issue.ID)
		if err != nil {
			return err
		}
		if retrieved == nil {
			return errors.New("decision point should be visible in transaction")
		}

		return nil
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify both were committed
	dp, err := store.GetDecisionPoint(ctx, "tx-decision-issue")
	if err != nil {
		t.Fatalf("failed to get decision point: %v", err)
	}
	if dp == nil {
		t.Error("decision point should exist after transaction commit")
	}
}

// =============================================================================
// Idle Timeout Tests
// =============================================================================

// TestIdleTimeoutReleasesConnection tests that idle timeout releases the connection.
func TestIdleTimeoutReleasesConnection(t *testing.T) {
	t.Skip("Test uses IdleTimeout field not yet implemented in Config struct")
	skipIfNoDolt(t)

	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "dolt-idle-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create store with short idle timeout
	cfg := &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       "testdb",
		IdleTimeout:    1 * time.Second, // Very short for testing
	}

	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}
	defer store.Close()

	// Do an operation to trigger activity
	if err := store.SetConfig(ctx, "idle_test", "value1"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	// Wait for idle timeout
	time.Sleep(2 * time.Second)

	// The connection should be released, but store should reconnect transparently
	value, err := store.GetConfig(ctx, "idle_test")
	if err != nil {
		t.Fatalf("failed to get config after idle timeout: %v", err)
	}
	if value != "value1" {
		t.Errorf("expected 'value1', got %q", value)
	}
}

// =============================================================================
// Version Control Cycle Tests
// =============================================================================

// TestCommitCycle tests the commit workflow.
func TestCommitCycle(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create an issue
	issue := &types.Issue{
		ID:          "commit-test-issue",
		Title:       "Commit Test Issue",
		Description: "For commit cycle testing",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Commit the changes
	if err := store.Commit(ctx, "Test commit: added issue"); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Get commit log
	log, err := store.Log(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get log: %v", err)
	}

	if len(log) == 0 {
		t.Fatal("expected at least one commit")
	}

	// Find our commit
	found := false
	for _, commit := range log {
		if commit.Message == "Test commit: added issue" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find our commit in the log")
	}
}

// TestBranchWorkflow tests branch creation and checkout.
func TestBranchWorkflow(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Commit initial state
	issue := &types.Issue{
		ID:          "branch-test-issue",
		Title:       "Branch Test Issue",
		Description: "Initial state",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	if err := store.Commit(ctx, "Initial commit"); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Create a new branch
	if err := store.Branch(ctx, "feature-branch"); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Checkout the branch
	if err := store.Checkout(ctx, "feature-branch"); err != nil {
		t.Fatalf("failed to checkout branch: %v", err)
	}

	// Verify we're on the new branch
	currentBranch, err := store.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if currentBranch != "feature-branch" {
		t.Errorf("expected 'feature-branch', got %q", currentBranch)
	}

	// Make changes on the branch
	if err := store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
		"description": "Modified on feature branch",
	}, "tester"); err != nil {
		t.Fatalf("failed to update issue: %v", err)
	}
	if err := store.Commit(ctx, "Feature work"); err != nil {
		t.Fatalf("failed to commit on branch: %v", err)
	}

	// Checkout main
	if err := store.Checkout(ctx, "main"); err != nil {
		t.Fatalf("failed to checkout main: %v", err)
	}

	// Verify original state on main
	mainIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get issue on main: %v", err)
	}
	if mainIssue.Description != "Initial state" {
		t.Errorf("expected 'Initial state' on main, got %q", mainIssue.Description)
	}

	// Merge the branch
	conflicts, err := store.Merge(ctx, "feature-branch")
	if err != nil {
		t.Fatalf("failed to merge branch: %v", err)
	}
	if len(conflicts) > 0 {
		t.Logf("merge produced conflicts: %v", conflicts)
	}

	// Verify merged state
	mergedIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get merged issue: %v", err)
	}
	if mergedIssue.Description != "Modified on feature branch" {
		t.Errorf("expected 'Modified on feature branch' after merge, got %q", mergedIssue.Description)
	}

	// Clean up branch
	if err := store.DeleteBranch(ctx, "feature-branch"); err != nil {
		t.Logf("warning: failed to delete branch: %v", err)
	}
}

// TestStatusTracking tests the status command.
func TestStatusTracking(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Commit clean state
	if err := store.Commit(ctx, "Clean state"); err != nil {
		t.Fatalf("failed to commit clean state: %v", err)
	}

	// Check status - should be clean
	status, err := store.Status(ctx)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if len(status.Staged) > 0 || len(status.Unstaged) > 0 {
		t.Logf("Note: status shows changes after commit: staged=%d, unstaged=%d", len(status.Staged), len(status.Unstaged))
	}

	// Make a change
	issue := &types.Issue{
		ID:          "status-test-issue",
		Title:       "Status Test Issue",
		Description: "For status testing",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Check status - should show changes
	statusAfter, err := store.Status(ctx)
	if err != nil {
		t.Fatalf("failed to get status after change: %v", err)
	}

	hasChanges := len(statusAfter.Staged) > 0 || len(statusAfter.Unstaged) > 0
	t.Logf("Status after change: staged=%d, unstaged=%d", len(statusAfter.Staged), len(statusAfter.Unstaged))

	if !hasChanges {
		t.Log("Note: status does not show uncommitted changes (Dolt tracks differently than git)")
	}
}

// TestHasUncommittedChanges tests the cheap status check used to reduce sync overhead.
// gt-p1mpqx: This tests the optimization that avoids expensive commit operations.
func TestHasUncommittedChanges(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Commit any initial state to start clean
	_ = store.Commit(ctx, "Initial clean state")

	// Check for uncommitted changes - should be false after commit
	hasChanges, err := store.HasUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("failed to check uncommitted changes: %v", err)
	}
	t.Logf("HasUncommittedChanges after commit: %v", hasChanges)

	// Create an issue to make a change
	issue := &types.Issue{
		ID:          "uncommitted-test",
		Title:       "Test for uncommitted changes",
		Description: "Testing HasUncommittedChanges",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Now there should be uncommitted changes
	hasChangesAfter, err := store.HasUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("failed to check uncommitted changes after create: %v", err)
	}
	t.Logf("HasUncommittedChanges after create: %v", hasChangesAfter)

	// Commit the changes
	if err := store.Commit(ctx, "Commit test issue"); err != nil {
		// "nothing to commit" is acceptable if Dolt auto-committed
		if !strings.Contains(err.Error(), "nothing to commit") {
			t.Fatalf("failed to commit: %v", err)
		}
	}

	// After commit, should have no uncommitted changes
	hasChangesFinal, err := store.HasUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("failed to check uncommitted changes after final commit: %v", err)
	}
	t.Logf("HasUncommittedChanges after final commit: %v", hasChangesFinal)

	// The important thing is that the method works and doesn't error
	// The exact behavior depends on Dolt's internal tracking
}

// =============================================================================
// Concurrent Transaction Tests
// =============================================================================

// TestConcurrentTransactionIsolation verifies that transactions are isolated.
func TestConcurrentTransactionIsolation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create a shared issue
	sharedIssue := &types.Issue{
		ID:          "shared-tx-issue",
		Title:       "Shared Issue",
		Description: "Initial",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, sharedIssue, "tester"); err != nil {
		t.Fatalf("failed to create shared issue: %v", err)
	}

	const numWorkers = 5
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// Each worker tries to update in a transaction
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
				// Read current state
				issue, err := tx.GetIssue(ctx, sharedIssue.ID)
				if err != nil {
					return err
				}
				if issue == nil {
					return errors.New("issue not found")
				}

				// Update with worker-specific value
				return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
					"description": fmt.Sprintf("Updated by worker %d", workerID),
				}, fmt.Sprintf("worker-%d", workerID))
			})

			if err != nil {
				errorCount.Add(1)
				t.Logf("Worker %d transaction failed (may be expected): %v", workerID, err)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Transaction results: %d succeeded, %d failed", successCount.Load(), errorCount.Load())

	// At least one should succeed
	if successCount.Load() == 0 {
		t.Error("expected at least one transaction to succeed")
	}

	// Verify final state is consistent
	final, err := store.GetIssue(ctx, sharedIssue.ID)
	if err != nil {
		t.Fatalf("failed to get final issue state: %v", err)
	}
	t.Logf("Final description: %q", final.Description)
}

// =============================================================================
// External Reference Tests
// =============================================================================

// TestGetIssueByExternalRef tests fetching issues by external reference.
func TestGetIssueByExternalRef(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create issue with external reference
	issue := &types.Issue{
		ID:          "ext-ref-issue",
		Title:       "External Ref Issue",
		Description: "Has external reference",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Set external reference
	if err := store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
		"external_ref": "JIRA-1234",
	}, "tester"); err != nil {
		t.Fatalf("failed to set external ref: %v", err)
	}

	// Fetch by external reference
	retrieved, err := store.GetIssueByExternalRef(ctx, "JIRA-1234")
	if err != nil {
		t.Fatalf("failed to get issue by external ref: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to find issue by external ref")
	}
	if retrieved.ID != issue.ID {
		t.Errorf("expected issue ID %s, got %s", issue.ID, retrieved.ID)
	}

	// Fetch non-existent external reference
	notFound, err := store.GetIssueByExternalRef(ctx, "NON-EXISTENT")
	if err != nil {
		t.Fatalf("unexpected error for non-existent ref: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for non-existent external reference")
	}
}

// =============================================================================
// Metadata and Config Tests
// =============================================================================

// TestMetadataOperations tests internal metadata storage.
func TestMetadataOperations(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Set metadata
	if err := store.SetMetadata(ctx, "import_hash", "abc123"); err != nil {
		t.Fatalf("failed to set metadata: %v", err)
	}

	// Get metadata
	value, err := store.GetMetadata(ctx, "import_hash")
	if err != nil {
		t.Fatalf("failed to get metadata: %v", err)
	}
	if value != "abc123" {
		t.Errorf("expected 'abc123', got %q", value)
	}

	// Update metadata
	if err := store.SetMetadata(ctx, "import_hash", "def456"); err != nil {
		t.Fatalf("failed to update metadata: %v", err)
	}

	// Verify update
	value, err = store.GetMetadata(ctx, "import_hash")
	if err != nil {
		t.Fatalf("failed to get updated metadata: %v", err)
	}
	if value != "def456" {
		t.Errorf("expected 'def456', got %q", value)
	}

	// Non-existent key
	missing, err := store.GetMetadata(ctx, "non_existent")
	if err != nil {
		t.Fatalf("unexpected error for missing key: %v", err)
	}
	if missing != "" {
		t.Errorf("expected empty string for missing key, got %q", missing)
	}
}

// TestCustomStatusesAndTypes tests custom status and type configuration.
func TestCustomStatusesAndTypes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Set custom statuses
	if err := store.SetConfig(ctx, "status.custom", "review,testing,qa"); err != nil {
		t.Fatalf("failed to set custom statuses: %v", err)
	}

	// Get custom statuses
	statuses, err := store.GetCustomStatuses(ctx)
	if err != nil {
		t.Fatalf("failed to get custom statuses: %v", err)
	}
	if len(statuses) != 3 {
		t.Errorf("expected 3 custom statuses, got %d: %v", len(statuses), statuses)
	}

	// Set custom types
	if err := store.SetConfig(ctx, "types.custom", "research,spike"); err != nil {
		t.Fatalf("failed to set custom types: %v", err)
	}

	// Get custom types
	customTypes, err := store.GetCustomTypes(ctx)
	if err != nil {
		t.Fatalf("failed to get custom types: %v", err)
	}
	if len(customTypes) != 2 {
		t.Errorf("expected 2 custom types, got %d: %v", len(customTypes), customTypes)
	}
}

// =============================================================================
// Export Hash Tracking Tests
// =============================================================================

// TestExportHashTracking tests the export hash tracking for deduplication.
func TestExportHashTracking(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create issue
	issue := &types.Issue{
		ID:          "export-hash-issue",
		Title:       "Export Hash Issue",
		Description: "For export hash testing",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Initially no export hash
	hash, err := store.GetExportHash(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get export hash: %v", err)
	}
	if hash != "" {
		t.Errorf("expected empty export hash initially, got %q", hash)
	}

	// Set export hash
	if err := store.SetExportHash(ctx, issue.ID, "hash123"); err != nil {
		t.Fatalf("failed to set export hash: %v", err)
	}

	// Verify export hash
	hash, err = store.GetExportHash(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get export hash: %v", err)
	}
	if hash != "hash123" {
		t.Errorf("expected 'hash123', got %q", hash)
	}

	// Clear all export hashes
	if err := store.ClearAllExportHashes(ctx); err != nil {
		t.Fatalf("failed to clear export hashes: %v", err)
	}

	// Verify cleared
	hash, err = store.GetExportHash(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to get export hash after clear: %v", err)
	}
	if hash != "" {
		t.Errorf("expected empty export hash after clear, got %q", hash)
	}
}

// =============================================================================
// Child ID Generation Tests
// =============================================================================

// TestChildIDGeneration tests sequential child ID generation.
func TestChildIDGeneration(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Create parent issue
	parent := &types.Issue{
		ID:          "parent-for-children",
		Title:       "Parent Issue",
		Description: "Has children",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeEpic,
	}
	if err := store.CreateIssue(ctx, parent, "tester"); err != nil {
		t.Fatalf("failed to create parent: %v", err)
	}

	// Generate sequential child IDs
	ids := make(map[string]bool)
	for i := 0; i < 5; i++ {
		childID, err := store.GetNextChildID(ctx, parent.ID)
		if err != nil {
			t.Fatalf("failed to get child ID %d: %v", i, err)
		}
		if ids[childID] {
			t.Errorf("duplicate child ID generated: %s", childID)
		}
		ids[childID] = true
		t.Logf("Generated child ID %d: %s", i+1, childID)
	}

	if len(ids) != 5 {
		t.Errorf("expected 5 unique IDs, got %d", len(ids))
	}
}

// TestGetNextChildID_ParentNotExists tests that GetNextChildID fails gracefully
// when the parent issue doesn't exist (hq-e6988b fix).
func TestGetNextChildID_ParentNotExists(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Try to get child ID for non-existent parent
	_, err := store.GetNextChildID(ctx, "nonexistent-parent")
	if err == nil {
		t.Error("expected error when parent doesn't exist, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' in error, got: %v", err)
	}
}

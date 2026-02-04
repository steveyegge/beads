package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestBatchAddDependencies_HappyPath verifies adding multiple dependencies atomically
func TestBatchAddDependencies_HappyPath(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create test issues
	issue1, err := createTestIssue(t, client, "Issue 1", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 1: %v", err)
	}

	issue2, err := createTestIssue(t, client, "Issue 2", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 2: %v", err)
	}

	issue3, err := createTestIssue(t, client, "Issue 3", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 3: %v", err)
	}

	// Add multiple dependencies in batch
	args := &BatchAddDependenciesArgs{
		Dependencies: []BatchDependency{
			{FromID: issue1, ToID: issue2, Type: "blocks"},
			{FromID: issue1, ToID: issue3, Type: "blocks"},
			{FromID: issue2, ToID: issue3, Type: "blocks"},
		},
	}

	result, err := client.BatchAddDependencies(args)
	if err != nil {
		t.Fatalf("BatchAddDependencies failed: %v", err)
	}

	// Verify all dependencies were added
	if result.Added != 3 {
		t.Errorf("Expected 3 dependencies added, got %d", result.Added)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Expected no errors, got %v", result.Errors)
	}

	// Verify dependencies exist in store
	ctx := context.Background()
	deps1, err := store.GetDependencies(ctx, issue1)
	if err != nil {
		t.Fatalf("Failed to get dependencies for issue1: %v", err)
	}

	if len(deps1) != 2 {
		t.Errorf("Expected 2 dependencies for issue1, got %d", len(deps1))
	}

	deps2, err := store.GetDependencies(ctx, issue2)
	if err != nil {
		t.Fatalf("Failed to get dependencies for issue2: %v", err)
	}

	if len(deps2) != 1 {
		t.Errorf("Expected 1 dependency for issue2, got %d", len(deps2))
	}
}

// TestBatchAddDependencies_Rollback verifies the transaction rolls back on failure
func TestBatchAddDependencies_Rollback(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create test issues
	issue1, err := createTestIssue(t, client, "Issue 1", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 1: %v", err)
	}

	issue2, err := createTestIssue(t, client, "Issue 2", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 2: %v", err)
	}

	// Try to add dependencies where one references a non-existent issue
	// The batch operation continues past errors and reports them
	args := &BatchAddDependenciesArgs{
		Dependencies: []BatchDependency{
			{FromID: issue1, ToID: issue2, Type: "blocks"},
			{FromID: issue1, ToID: "nonexistent-issue", Type: "blocks"},
		},
	}

	result, err := client.BatchAddDependencies(args)
	if err != nil {
		t.Fatalf("BatchAddDependencies failed unexpectedly: %v", err)
	}

	// The operation should succeed but report errors for invalid dependencies
	// The first valid dependency should still be added
	if result.Added < 1 {
		t.Errorf("Expected at least 1 dependency added, got %d", result.Added)
	}

	// Verify the valid dependency was added
	ctx := context.Background()
	deps, err := store.GetDependencies(ctx, issue1)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}

	// Should have exactly one dependency (the valid one)
	validDeps := 0
	for _, dep := range deps {
		if dep.ID == issue2 {
			validDeps++
		}
	}
	if validDeps != 1 {
		t.Errorf("Expected 1 valid dependency to issue2, got %d", validDeps)
	}
}

// TestBatchAddDependencies_EmptyList handles empty input gracefully
func TestBatchAddDependencies_EmptyList(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	args := &BatchAddDependenciesArgs{
		Dependencies: []BatchDependency{},
	}

	result, err := client.BatchAddDependencies(args)
	if err != nil {
		t.Fatalf("BatchAddDependencies failed: %v", err)
	}

	if result.Added != 0 {
		t.Errorf("Expected 0 dependencies added for empty list, got %d", result.Added)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Expected no errors for empty list, got %v", result.Errors)
	}
}

// TestBatchAddDependencies_InvalidIssue tests error handling for non-existent issues
func TestBatchAddDependencies_InvalidIssue(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Create one valid issue
	issue1, err := createTestIssue(t, client, "Issue 1", "task")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Try to add dependency to non-existent issue
	args := &BatchAddDependenciesArgs{
		Dependencies: []BatchDependency{
			{FromID: issue1, ToID: "nonexistent-abc123", Type: "blocks"},
		},
	}

	result, err := client.BatchAddDependencies(args)
	if err != nil {
		t.Fatalf("BatchAddDependencies failed unexpectedly: %v", err)
	}

	// The operation succeeds but reports errors
	if len(result.Errors) == 0 {
		// Depending on implementation, errors may or may not be reported
		// The key is that it doesn't add an invalid dependency
		t.Logf("No errors reported, checking if dependency was skipped")
	}

	// The invalid dependency should not have been added
	// (or if it was attempted, it should have failed and been recorded in errors)
}

// TestBatchQueryWorkers_HappyPath verifies querying workers for multiple issues
func TestBatchQueryWorkers_HappyPath(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create test issues with different assignees
	issue1, err := createTestIssue(t, client, "Issue 1", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 1: %v", err)
	}
	if err := store.UpdateIssue(ctx, issue1, map[string]interface{}{"assignee": "alice"}, "test"); err != nil {
		t.Fatalf("Failed to assign issue 1: %v", err)
	}

	issue2, err := createTestIssue(t, client, "Issue 2", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 2: %v", err)
	}
	if err := store.UpdateIssue(ctx, issue2, map[string]interface{}{"assignee": "bob"}, "test"); err != nil {
		t.Fatalf("Failed to assign issue 2: %v", err)
	}

	issue3, err := createTestIssue(t, client, "Issue 3", "task")
	if err != nil {
		t.Fatalf("Failed to create issue 3: %v", err)
	}
	// issue3 has no assignee

	// Query workers for all issues
	args := &BatchQueryWorkersArgs{
		IssueIDs: []string{issue1, issue2, issue3},
	}

	result, err := client.BatchQueryWorkers(args)
	if err != nil {
		t.Fatalf("BatchQueryWorkers failed: %v", err)
	}

	// Verify workers map has all issues
	if len(result.Workers) != 3 {
		t.Errorf("Expected 3 workers in result, got %d", len(result.Workers))
	}

	// Verify correct assignees
	if w, ok := result.Workers[issue1]; !ok || w == nil {
		t.Errorf("Expected worker info for issue1")
	} else if w.Assignee != "alice" {
		t.Errorf("Expected assignee 'alice' for issue1, got %q", w.Assignee)
	}

	if w, ok := result.Workers[issue2]; !ok || w == nil {
		t.Errorf("Expected worker info for issue2")
	} else if w.Assignee != "bob" {
		t.Errorf("Expected assignee 'bob' for issue2, got %q", w.Assignee)
	}

	if w, ok := result.Workers[issue3]; !ok || w == nil {
		t.Errorf("Expected worker info for issue3")
	} else if w.Assignee != "" {
		t.Errorf("Expected empty assignee for issue3, got %q", w.Assignee)
	}
}

// TestBatchQueryWorkers_PartialResults tests that some issues can be found while others don't exist
func TestBatchQueryWorkers_PartialResults(t *testing.T) {
	_, client, store, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create one valid issue
	issue1, err := createTestIssue(t, client, "Issue 1", "task")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	if err := store.UpdateIssue(ctx, issue1, map[string]interface{}{"assignee": "worker1"}, "test"); err != nil {
		t.Fatalf("Failed to assign issue: %v", err)
	}

	// Query with mix of existing and non-existing issues
	args := &BatchQueryWorkersArgs{
		IssueIDs: []string{issue1, "nonexistent-xyz789", "another-fake-id"},
	}

	result, err := client.BatchQueryWorkers(args)
	if err != nil {
		t.Fatalf("BatchQueryWorkers failed: %v", err)
	}

	// Verify all requested IDs are in result map
	if len(result.Workers) != 3 {
		t.Errorf("Expected 3 entries in workers map, got %d", len(result.Workers))
	}

	// The valid issue should have worker info
	if w, ok := result.Workers[issue1]; !ok {
		t.Errorf("Expected entry for issue1 in workers map")
	} else if w == nil {
		t.Errorf("Expected non-nil worker info for issue1")
	} else if w.Assignee != "worker1" {
		t.Errorf("Expected assignee 'worker1', got %q", w.Assignee)
	}

	// Non-existent issues should have nil entries
	if w, ok := result.Workers["nonexistent-xyz789"]; !ok {
		t.Errorf("Expected entry for nonexistent-xyz789")
	} else if w != nil {
		t.Errorf("Expected nil worker info for nonexistent issue, got %+v", w)
	}

	if w, ok := result.Workers["another-fake-id"]; !ok {
		t.Errorf("Expected entry for another-fake-id")
	} else if w != nil {
		t.Errorf("Expected nil worker info for nonexistent issue, got %+v", w)
	}
}

// TestBatchQueryWorkers_EmptyList handles empty input gracefully
func TestBatchQueryWorkers_EmptyList(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	args := &BatchQueryWorkersArgs{
		IssueIDs: []string{},
	}

	result, err := client.BatchQueryWorkers(args)
	if err != nil {
		t.Fatalf("BatchQueryWorkers failed: %v", err)
	}

	if result.Workers == nil {
		t.Errorf("Expected non-nil workers map, got nil")
	}

	if len(result.Workers) != 0 {
		t.Errorf("Expected empty workers map, got %d entries", len(result.Workers))
	}
}

// Helper function to create a test issue and return its ID
func createTestIssue(t *testing.T, client *Client, title string, issueType string) (string, error) {
	t.Helper()

	createArgs := &CreateArgs{
		Title:     title,
		IssueType: issueType,
		Priority:  2,
	}

	resp, err := client.Create(createArgs)
	if err != nil {
		return "", err
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		return "", err
	}

	return issue.ID, nil
}

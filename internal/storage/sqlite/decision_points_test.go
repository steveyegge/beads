package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestCreateDecisionPoint tests basic decision point creation
func TestCreateDecisionPoint(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create options JSON
	options := []types.DecisionOption{
		{ID: "a", Short: "Redis", Label: "Use Redis for caching"},
		{ID: "b", Short: "In-memory", Label: "Use in-memory cache"},
	}
	optionsJSON, _ := json.Marshal(options)

	// Create decision point
	dp := &types.DecisionPoint{
		IssueID:       issue.ID,
		Prompt:        "Which caching solution should we use?",
		Options:       string(optionsJSON),
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
	}

	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Verify by retrieving
	retrieved, err := store.GetDecisionPoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected decision point, got nil")
	}

	if retrieved.IssueID != issue.ID {
		t.Errorf("Expected IssueID %s, got %s", issue.ID, retrieved.IssueID)
	}
	if retrieved.Prompt != dp.Prompt {
		t.Errorf("Expected Prompt %s, got %s", dp.Prompt, retrieved.Prompt)
	}
	if retrieved.Options != dp.Options {
		t.Errorf("Expected Options %s, got %s", dp.Options, retrieved.Options)
	}
	if retrieved.DefaultOption != dp.DefaultOption {
		t.Errorf("Expected DefaultOption %s, got %s", dp.DefaultOption, retrieved.DefaultOption)
	}
	if retrieved.Iteration != dp.Iteration {
		t.Errorf("Expected Iteration %d, got %d", dp.Iteration, retrieved.Iteration)
	}
	if retrieved.MaxIterations != dp.MaxIterations {
		t.Errorf("Expected MaxIterations %d, got %d", dp.MaxIterations, retrieved.MaxIterations)
	}
	if retrieved.CreatedAt.IsZero() {
		t.Error("Expected non-zero CreatedAt timestamp")
	}
}

// TestCreateDecisionPointNonexistentIssue tests creating decision point for non-existent issue
func TestCreateDecisionPointNonexistentIssue(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	dp := &types.DecisionPoint{
		IssueID: "nonexistent-id",
		Prompt:  "Test prompt",
		Options: "[]",
	}

	err := store.CreateDecisionPoint(ctx, dp)
	if err == nil {
		t.Fatal("Expected error when creating decision point for non-existent issue, got nil")
	}
}

// TestGetDecisionPointNotFound tests retrieving non-existent decision point
func TestGetDecisionPointNotFound(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue but no decision point
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Try to get decision point
	dp, err := store.GetDecisionPoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}
	if dp != nil {
		t.Errorf("Expected nil for non-existent decision point, got %+v", dp)
	}
}

// TestUpdateDecisionPoint tests updating a decision point
func TestUpdateDecisionPoint(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create decision point
	dp := &types.DecisionPoint{
		IssueID:       issue.ID,
		Prompt:        "Original prompt",
		Options:       "[]",
		Iteration:     1,
		MaxIterations: 3,
	}

	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Update with response
	now := time.Now()
	dp.SelectedOption = "a"
	dp.ResponseText = "Choosing Redis for better scalability"
	dp.RespondedAt = &now
	dp.RespondedBy = "alice@example.com"

	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("UpdateDecisionPoint failed: %v", err)
	}

	// Verify update
	retrieved, err := store.GetDecisionPoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}

	if retrieved.SelectedOption != "a" {
		t.Errorf("Expected SelectedOption 'a', got '%s'", retrieved.SelectedOption)
	}
	if retrieved.ResponseText != "Choosing Redis for better scalability" {
		t.Errorf("Expected ResponseText '%s', got '%s'", dp.ResponseText, retrieved.ResponseText)
	}
	if retrieved.RespondedBy != "alice@example.com" {
		t.Errorf("Expected RespondedBy 'alice@example.com', got '%s'", retrieved.RespondedBy)
	}
	if retrieved.RespondedAt == nil {
		t.Error("Expected non-nil RespondedAt")
	}
}

// TestUpdateDecisionPointNotFound tests updating non-existent decision point
func TestUpdateDecisionPointNotFound(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an issue but no decision point
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:        issue.ID,
		Prompt:         "Test",
		Options:        "[]",
		SelectedOption: "a",
	}

	err := store.UpdateDecisionPoint(ctx, dp)
	if err == nil {
		t.Fatal("Expected error when updating non-existent decision point, got nil")
	}
}

// TestListPendingDecisions tests listing pending decisions
func TestListPendingDecisions(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issues with decision points
	for i := 0; i < 3; i++ {
		issue := &types.Issue{
			Title:     "Test issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		dp := &types.DecisionPoint{
			IssueID:       issue.ID,
			Prompt:        "Pending decision",
			Options:       "[]",
			Iteration:     1,
			MaxIterations: 3,
		}
		if err := store.CreateDecisionPoint(ctx, dp); err != nil {
			t.Fatalf("CreateDecisionPoint failed: %v", err)
		}

		// Mark first one as responded
		if i == 0 {
			now := time.Now()
			dp.RespondedAt = &now
			dp.SelectedOption = "a"
			if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
				t.Fatalf("UpdateDecisionPoint failed: %v", err)
			}
		}
	}

	// List pending decisions
	pending, err := store.ListPendingDecisions(ctx)
	if err != nil {
		t.Fatalf("ListPendingDecisions failed: %v", err)
	}

	// Should have 2 pending (3 total - 1 responded)
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending decisions, got %d", len(pending))
	}

	// All should have nil RespondedAt
	for _, dp := range pending {
		if dp.RespondedAt != nil {
			t.Errorf("Pending decision should have nil RespondedAt, got %v", dp.RespondedAt)
		}
	}
}

// TestListPendingDecisionsEmpty tests listing when no pending decisions exist
func TestListPendingDecisionsEmpty(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	pending, err := store.ListPendingDecisions(ctx)
	if err != nil {
		t.Fatalf("ListPendingDecisions failed: %v", err)
	}

	if len(pending) != 0 {
		t.Errorf("Expected 0 pending decisions, got %d", len(pending))
	}
}

// TestDecisionPointIteration tests iteration fields
func TestDecisionPointIteration(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issue
	issue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create first iteration
	dp1 := &types.DecisionPoint{
		IssueID:       issue.ID,
		Prompt:        "Iteration 1 prompt",
		Options:       "[]",
		Iteration:     1,
		MaxIterations: 3,
	}
	if err := store.CreateDecisionPoint(ctx, dp1); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Update with guidance for next iteration
	dp1.ResponseText = "Need more options"
	dp1.Guidance = "Consider cloud options"
	dp1.Iteration = 2
	if err := store.UpdateDecisionPoint(ctx, dp1); err != nil {
		t.Fatalf("UpdateDecisionPoint failed: %v", err)
	}

	// Verify
	retrieved, err := store.GetDecisionPoint(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}

	if retrieved.Iteration != 2 {
		t.Errorf("Expected Iteration 2, got %d", retrieved.Iteration)
	}
	if retrieved.Guidance != "Consider cloud options" {
		t.Errorf("Expected Guidance 'Consider cloud options', got '%s'", retrieved.Guidance)
	}
}

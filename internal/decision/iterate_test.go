package decision

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

func createTestDecisionPoint(t *testing.T, store *memory.MemoryStorage, id string, iteration, maxIter int) (*types.Issue, *types.DecisionPoint) {
	t.Helper()
	ctx := context.Background()

	issue := &types.Issue{
		ID:        id,
		Title:     "Test Decision",
		IssueType: types.IssueType("gate"),
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
		Timeout:   24 * time.Hour,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:       id,
		Prompt:        "Which option?",
		Options:       `[{"id":"a","short":"A","label":"Option A"},{"id":"b","short":"B","label":"Option B"}]`,
		DefaultOption: "a",
		Iteration:     iteration,
		MaxIterations: maxIter,
		CreatedAt:     time.Now(),
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("failed to create decision point: %v", err)
	}

	return issue, dp
}

func TestCreateNextIteration(t *testing.T) {
	store := memory.New("")
	ctx := context.Background()

	issue, dp := createTestDecisionPoint(t, store, "mol.decision-1", 1, 3)

	result, err := CreateNextIteration(ctx, store, dp, issue, "Try a hybrid approach", "user@example.com", "test")
	if err != nil {
		t.Fatalf("CreateNextIteration failed: %v", err)
	}

	// Check result
	if result.MaxReached {
		t.Error("MaxReached should be false for iteration 2/3")
	}
	if result.NewDecisionID != "mol.decision-1.r2" {
		t.Errorf("NewDecisionID = %q, want %q", result.NewDecisionID, "mol.decision-1.r2")
	}

	// Check new decision point
	if result.DecisionPoint.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", result.DecisionPoint.Iteration)
	}
	if result.DecisionPoint.PriorID != "mol.decision-1" {
		t.Errorf("PriorID = %q, want %q", result.DecisionPoint.PriorID, "mol.decision-1")
	}
	if result.DecisionPoint.Guidance != "Try a hybrid approach" {
		t.Errorf("Guidance = %q, want %q", result.DecisionPoint.Guidance, "Try a hybrid approach")
	}

	// Verify new issue exists in store
	newIssue, err := store.GetIssue(ctx, result.NewDecisionID)
	if err != nil {
		t.Fatalf("failed to get new issue: %v", err)
	}
	if newIssue == nil {
		t.Fatal("new issue not found in store")
	}
	if newIssue.IssueType != types.IssueType("gate") {
		t.Errorf("IssueType = %q, want %q", newIssue.IssueType, types.IssueType("gate"))
	}

	// Verify new decision point exists
	newDP, err := store.GetDecisionPoint(ctx, result.NewDecisionID)
	if err != nil {
		t.Fatalf("failed to get new decision point: %v", err)
	}
	if newDP == nil {
		t.Fatal("new decision point not found in store")
	}
}

func TestCreateNextIteration_MaxReached(t *testing.T) {
	store := memory.New("")
	ctx := context.Background()

	// Create at max iteration
	issue, dp := createTestDecisionPoint(t, store, "mol.decision-1", 3, 3)

	result, err := CreateNextIteration(ctx, store, dp, issue, "More guidance", "user@example.com", "test")
	if err != nil {
		t.Fatalf("CreateNextIteration failed: %v", err)
	}

	// Should indicate max reached
	if !result.MaxReached {
		t.Error("MaxReached should be true when at max iterations")
	}
	if result.NewDecisionID != "" {
		t.Errorf("NewDecisionID should be empty when max reached, got %q", result.NewDecisionID)
	}
}

func TestCreateNextIteration_ChainedIterations(t *testing.T) {
	store := memory.New("")
	ctx := context.Background()

	// Create initial decision
	issue, dp := createTestDecisionPoint(t, store, "mol.decision-1", 1, 5)

	// Create iteration 2
	result2, err := CreateNextIteration(ctx, store, dp, issue, "Guidance 1", "user@example.com", "test")
	if err != nil {
		t.Fatalf("iteration 2 failed: %v", err)
	}
	if result2.NewDecisionID != "mol.decision-1.r2" {
		t.Errorf("iter 2 ID = %q, want %q", result2.NewDecisionID, "mol.decision-1.r2")
	}

	// Create iteration 3 from iteration 2
	result3, err := CreateNextIteration(ctx, store, result2.DecisionPoint, result2.Issue, "Guidance 2", "user@example.com", "test")
	if err != nil {
		t.Fatalf("iteration 3 failed: %v", err)
	}
	if result3.NewDecisionID != "mol.decision-1.r3" {
		t.Errorf("iter 3 ID = %q, want %q", result3.NewDecisionID, "mol.decision-1.r3")
	}

	// Verify chain
	if result3.DecisionPoint.PriorID != "mol.decision-1.r2" {
		t.Errorf("iter 3 PriorID = %q, want %q", result3.DecisionPoint.PriorID, "mol.decision-1.r2")
	}
}

func TestGenerateIterationID(t *testing.T) {
	testCases := []struct {
		baseID    string
		iteration int
		want      string
	}{
		{"mol.decision-1", 2, "mol.decision-1.r2"},
		{"mol.decision-1", 3, "mol.decision-1.r3"},
		{"mol.decision-1.r2", 3, "mol.decision-1.r3"}, // Strip existing suffix
		{"mol.decision-1.r99", 100, "mol.decision-1.r100"},
		{"simple-id", 2, "simple-id.r2"},
		{"id.with.dots", 2, "id.with.dots.r2"},
		{"id.r-notanumber", 2, "id.r-notanumber.r2"}, // .r- is not iteration suffix
	}

	for _, tc := range testCases {
		t.Run(tc.baseID, func(t *testing.T) {
			got := generateIterationID(tc.baseID, tc.iteration)
			if got != tc.want {
				t.Errorf("generateIterationID(%q, %d) = %q, want %q",
					tc.baseID, tc.iteration, got, tc.want)
			}
		})
	}
}

func TestIsMaxIteration(t *testing.T) {
	testCases := []struct {
		iteration int
		maxIter   int
		want      bool
	}{
		{1, 3, false},
		{2, 3, false},
		{3, 3, true},
		{4, 3, true}, // Over max
		{1, 1, true}, // Single iteration
	}

	for _, tc := range testCases {
		dp := &types.DecisionPoint{
			Iteration:     tc.iteration,
			MaxIterations: tc.maxIter,
		}
		got := IsMaxIteration(dp)
		if got != tc.want {
			t.Errorf("IsMaxIteration(iter=%d, max=%d) = %v, want %v",
				tc.iteration, tc.maxIter, got, tc.want)
		}
	}
}

func TestGetIterationSuffix(t *testing.T) {
	testCases := []struct {
		iteration int
		maxIter   int
		want      string
	}{
		{1, 3, ""},              // Default, don't show
		{2, 3, " [iter 2/3]"},
		{3, 3, " [iter 3/3]"},
		{1, 5, " [iter 1/5]"},  // Non-default max, show
		{1, 1, " [iter 1/1]"},
	}

	for _, tc := range testCases {
		dp := &types.DecisionPoint{
			Iteration:     tc.iteration,
			MaxIterations: tc.maxIter,
		}
		got := GetIterationSuffix(dp)
		if got != tc.want {
			t.Errorf("GetIterationSuffix(iter=%d, max=%d) = %q, want %q",
				tc.iteration, tc.maxIter, got, tc.want)
		}
	}
}

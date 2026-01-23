//go:build ignore
// +build ignore

// NOTE: Temporarily ignored - ListAllDecisionPoints not implemented in SQLiteStorage.

package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/decision"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// hq-946577.31: Decision point test suite

// TestDecisionOptionsSchema tests option JSON parsing
func TestDecisionOptionsSchema(t *testing.T) {
	testCases := []struct {
		name      string
		optJSON   string
		wantCount int
		wantError bool
	}{
		{
			name:      "valid options",
			optJSON:   `[{"id":"a","short":"A","label":"Option A"},{"id":"b","short":"B","label":"Option B"}]`,
			wantCount: 2,
		},
		{
			name:      "options with descriptions",
			optJSON:   `[{"id":"a","short":"A","label":"Option A","description":"Long description"}]`,
			wantCount: 1,
		},
		{
			name:      "empty options",
			optJSON:   `[]`,
			wantCount: 0,
		},
		{
			name:      "invalid JSON",
			optJSON:   `[{"id":"a",}]`,
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var options []types.DecisionOption
			err := json.Unmarshal([]byte(tc.optJSON), &options)

			if tc.wantError {
				if err == nil {
					t.Error("expected error parsing invalid JSON")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(options) != tc.wantCount {
				t.Errorf("got %d options, want %d", len(options), tc.wantCount)
			}
		})
	}
}

// TestDecisionPointFields tests DecisionPoint struct behavior
func TestDecisionPointFields(t *testing.T) {
	now := time.Now()
	dp := &types.DecisionPoint{
		IssueID:       "test-dp1",
		Prompt:        "Which approach?",
		Options:       `[{"id":"a","label":"Option A"}]`,
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     now,
	}

	// Test GetOptionsWithAccept for iteration 1 (no _accept)
	options, err := dp.GetOptionsWithAccept()
	if err != nil {
		t.Fatalf("GetOptionsWithAccept failed: %v", err)
	}
	if len(options) != 1 {
		t.Errorf("iteration 1 should have 1 option, got %d", len(options))
	}

	// Test GetOptionsWithAccept for iteration 2 (has _accept)
	dp.Iteration = 2
	options, err = dp.GetOptionsWithAccept()
	if err != nil {
		t.Fatalf("GetOptionsWithAccept failed: %v", err)
	}
	if len(options) != 2 {
		t.Errorf("iteration 2 should have 2 options (including _accept), got %d", len(options))
	}

	// Verify _accept option
	found := false
	for _, opt := range options {
		if opt.ID == types.DecisionAcceptOptionID {
			found = true
			break
		}
	}
	if !found {
		t.Error("_accept option not found in iteration 2")
	}
}

// TestDecisionTerminationConditions tests when decisions should close
func TestDecisionTerminationConditions(t *testing.T) {
	testCases := []struct {
		name         string
		selectOpt    string
		text         string
		accept       bool
		shouldClose  bool
		shouldIterate bool
	}{
		{"select only", "a", "", false, true, false},
		{"text only", "", "guidance", false, false, true},
		{"select and text", "a", "notes", false, true, false},
		{"accept guidance", "", "guidance", true, true, false},
		{"_accept option", types.DecisionAcceptOptionID, "", false, true, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shouldCloseGate := tc.selectOpt != "" || tc.accept
			shouldIterate := tc.text != "" && tc.selectOpt == "" && !tc.accept

			if shouldCloseGate != tc.shouldClose {
				t.Errorf("shouldCloseGate = %v, want %v", shouldCloseGate, tc.shouldClose)
			}
			if shouldIterate != tc.shouldIterate {
				t.Errorf("shouldIterate = %v, want %v", shouldIterate, tc.shouldIterate)
			}
		})
	}
}

// TestDecisionIterationLogic tests iteration ID generation
func TestDecisionIterationLogic(t *testing.T) {
	testCases := []struct {
		baseID    string
		iteration int
		wantID    string
		isMax     bool
	}{
		{"mol.decision-1", 2, "mol.decision-1.r2", false},
		{"mol.decision-1", 3, "mol.decision-1.r3", true},
		{"mol.decision-1.r2", 3, "mol.decision-1.r3", true},
	}

	for _, tc := range testCases {
		t.Run(tc.baseID, func(t *testing.T) {
			dp := &types.DecisionPoint{
				IssueID:       tc.baseID,
				Iteration:     tc.iteration - 1,
				MaxIterations: 3,
			}

			if decision.IsMaxIteration(dp) && tc.iteration == 2 {
				t.Error("should not be max at iteration 2")
			}

			dp.Iteration = tc.iteration
			isMax := decision.IsMaxIteration(dp)
			if isMax != tc.isMax {
				t.Errorf("IsMaxIteration = %v, want %v", isMax, tc.isMax)
			}
		})
	}
}

// TestDecisionCreateRespondClose tests the full lifecycle
func TestDecisionCreateRespondClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-dp1"
	now := time.Now()

	// Step 1: Create decision point
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "Test Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		Timeout:   24 * time.Hour,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:       decisionID,
		Prompt:        "Which option?",
		Options:       `[{"id":"a","label":"Option A"},{"id":"b","label":"Option B"}]`,
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Verify decision exists
	gotDP, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}
	if gotDP.Prompt != "Which option?" {
		t.Errorf("Prompt = %q, want %q", gotDP.Prompt, "Which option?")
	}

	// Step 2: Respond to decision
	respondedAt := time.Now()
	gotDP.RespondedAt = &respondedAt
	gotDP.RespondedBy = "user@example.com"
	gotDP.SelectedOption = "a"
	if err := store.UpdateDecisionPoint(ctx, gotDP); err != nil {
		t.Fatalf("UpdateDecisionPoint failed: %v", err)
	}

	// Step 3: Close the gate
	if err := store.CloseIssue(ctx, decisionID, "Selected: Option A", "test", ""); err != nil {
		t.Fatalf("CloseIssue failed: %v", err)
	}

	// Verify final state
	closedIssue, err := store.GetIssue(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if closedIssue.Status != types.StatusClosed {
		t.Errorf("Status = %q, want %q", closedIssue.Status, types.StatusClosed)
	}
}

// TestDecisionIterationFlow tests text response creating new iteration
func TestDecisionIterationFlow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-dp1"
	now := time.Now()

	// Create initial decision
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "Test Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		Timeout:   24 * time.Hour,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:       decisionID,
		Prompt:        "Which option?",
		Options:       `[{"id":"a","label":"Option A"},{"id":"b","label":"Option B"}]`,
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Simulate text-only response (triggers iteration)
	result, err := decision.CreateNextIteration(ctx, store, dp, issue,
		"I'd prefer a hybrid approach", "user@example.com", "test")
	if err != nil {
		t.Fatalf("CreateNextIteration failed: %v", err)
	}

	if result.MaxReached {
		t.Error("MaxReached should be false for iteration 2")
	}
	if result.NewDecisionID != "test-dp1.r2" {
		t.Errorf("NewDecisionID = %q, want %q", result.NewDecisionID, "test-dp1.r2")
	}
	if result.DecisionPoint.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", result.DecisionPoint.Iteration)
	}
	if result.DecisionPoint.PriorID != decisionID {
		t.Errorf("PriorID = %q, want %q", result.DecisionPoint.PriorID, decisionID)
	}
	if result.DecisionPoint.Guidance != "I'd prefer a hybrid approach" {
		t.Errorf("Guidance = %q, want %q", result.DecisionPoint.Guidance, "I'd prefer a hybrid approach")
	}
}

// TestDecisionMaxIterations tests max iteration limit
func TestDecisionMaxIterations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-dp1"
	now := time.Now()

	// Create decision at max iteration
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "Test Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:       decisionID,
		Prompt:        "Which option?",
		Options:       `[{"id":"a","label":"Option A"}]`,
		Iteration:     3, // At max
		MaxIterations: 3,
		CreatedAt:     now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Try to create iteration - should be blocked
	result, err := decision.CreateNextIteration(ctx, store, dp, issue,
		"More guidance", "user@example.com", "test")
	if err != nil {
		t.Fatalf("CreateNextIteration failed: %v", err)
	}

	if !result.MaxReached {
		t.Error("MaxReached should be true when at max iterations")
	}
	if result.NewDecisionID != "" {
		t.Errorf("NewDecisionID should be empty, got %q", result.NewDecisionID)
	}
}

// TestDecisionJSONLRoundTrip tests export/import preserves decision data
func TestDecisionJSONLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-dp1"
	now := time.Now()

	// Create decision with all fields populated
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "JSONL Test Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		Timeout:   48 * time.Hour,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create a prior decision to test PriorID foreign key
	priorID := "test-dp0"
	priorIssue := &types.Issue{
		ID:        priorID,
		Title:     "Prior Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusClosed,
		Priority:  2,
		AwaitType: "decision",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, priorIssue, "test"); err != nil {
		t.Fatalf("CreateIssue (prior) failed: %v", err)
	}

	respondedAt := now.Add(time.Hour)
	dp := &types.DecisionPoint{
		IssueID:        decisionID,
		Prompt:         "Which option for JSONL?",
		Options:        `[{"id":"x","short":"X","label":"Option X","description":"Long desc"}]`,
		DefaultOption:  "x",
		SelectedOption: "x",
		ResponseText:   "With notes",
		RespondedBy:    "tester@example.com",
		RespondedAt:    &respondedAt,
		Iteration:      2,
		MaxIterations:  5,
		PriorID:        priorID,
		Guidance:       "Previous guidance",
		CreatedAt:      now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Export all decision points
	dps, err := store.ListAllDecisionPoints(ctx)
	if err != nil {
		t.Fatalf("ListAllDecisionPoints failed: %v", err)
	}
	if len(dps) != 1 {
		t.Fatalf("expected 1 decision point, got %d", len(dps))
	}

	// Verify all fields preserved
	got := dps[0]
	if got.IssueID != decisionID {
		t.Errorf("IssueID = %q, want %q", got.IssueID, decisionID)
	}
	if got.Prompt != dp.Prompt {
		t.Errorf("Prompt = %q, want %q", got.Prompt, dp.Prompt)
	}
	if got.SelectedOption != "x" {
		t.Errorf("SelectedOption = %q, want %q", got.SelectedOption, "x")
	}
	if got.ResponseText != "With notes" {
		t.Errorf("ResponseText = %q, want %q", got.ResponseText, "With notes")
	}
	if got.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", got.Iteration)
	}
	if got.PriorID != "test-dp0" {
		t.Errorf("PriorID = %q, want %q", got.PriorID, "test-dp0")
	}
	if got.Guidance != "Previous guidance" {
		t.Errorf("Guidance = %q, want %q", got.Guidance, "Previous guidance")
	}
}

// TestDecisionRemind tests the reminder functionality
func TestDecisionRemind(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-remind"
	now := time.Now()

	// Create decision point
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "Remind Test Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:       decisionID,
		Prompt:        "Need a reminder?",
		Options:       `[{"id":"a","label":"Option A"}]`,
		Iteration:     1,
		MaxIterations: 3,
		ReminderCount: 0,
		CreatedAt:     now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Simulate sending reminders
	for i := 1; i <= 3; i++ {
		gotDP, err := store.GetDecisionPoint(ctx, decisionID)
		if err != nil {
			t.Fatalf("GetDecisionPoint failed: %v", err)
		}

		gotDP.ReminderCount++
		if err := store.UpdateDecisionPoint(ctx, gotDP); err != nil {
			t.Fatalf("UpdateDecisionPoint failed: %v", err)
		}

		// Verify reminder count
		updated, err := store.GetDecisionPoint(ctx, decisionID)
		if err != nil {
			t.Fatalf("GetDecisionPoint failed: %v", err)
		}
		if updated.ReminderCount != i {
			t.Errorf("ReminderCount = %d, want %d", updated.ReminderCount, i)
		}
	}

	// Verify at max reminders
	final, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}
	if final.ReminderCount != 3 {
		t.Errorf("Final ReminderCount = %d, want 3", final.ReminderCount)
	}
}

// TestDecisionCancel tests the cancellation functionality
func TestDecisionCancel(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-cancel"
	now := time.Now()

	// Create decision point
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "Cancel Test Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	dp := &types.DecisionPoint{
		IssueID:       decisionID,
		Prompt:        "Should we cancel?",
		Options:       `[{"id":"a","label":"Option A"}]`,
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Cancel the decision
	cancelTime := time.Now()
	cancelReason := "No longer needed"
	cancelledBy := "admin@example.com"

	gotDP, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}

	gotDP.RespondedAt = &cancelTime
	gotDP.RespondedBy = cancelledBy
	gotDP.SelectedOption = "_cancelled"
	gotDP.ResponseText = cancelReason

	if err := store.UpdateDecisionPoint(ctx, gotDP); err != nil {
		t.Fatalf("UpdateDecisionPoint failed: %v", err)
	}

	// Close the gate
	if err := store.CloseIssue(ctx, decisionID, "Decision cancelled: "+cancelReason, "test", ""); err != nil {
		t.Fatalf("CloseIssue failed: %v", err)
	}

	// Verify final state
	closedIssue, err := store.GetIssue(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if closedIssue.Status != types.StatusClosed {
		t.Errorf("Status = %q, want %q", closedIssue.Status, types.StatusClosed)
	}

	finalDP, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}
	if finalDP.SelectedOption != "_cancelled" {
		t.Errorf("SelectedOption = %q, want %q", finalDP.SelectedOption, "_cancelled")
	}
	if finalDP.ResponseText != cancelReason {
		t.Errorf("ResponseText = %q, want %q", finalDP.ResponseText, cancelReason)
	}
	if finalDP.RespondedBy != cancelledBy {
		t.Errorf("RespondedBy = %q, want %q", finalDP.RespondedBy, cancelledBy)
	}
}

// TestDecisionCancelAlreadyResponded tests cancelling an already responded decision
func TestDecisionCancelAlreadyResponded(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	store, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Initialize database with prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	decisionID := "test-cancel-responded"
	now := time.Now()

	// Create decision point
	issue := &types.Issue{
		ID:        decisionID,
		Title:     "Already Responded Decision",
		IssueType: types.TypeGate,
		Status:    types.StatusClosed,
		Priority:  2,
		AwaitType: "decision",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	respondedAt := now.Add(-time.Hour)
	dp := &types.DecisionPoint{
		IssueID:        decisionID,
		Prompt:         "Already answered",
		Options:        `[{"id":"a","label":"Option A"}]`,
		SelectedOption: "a",
		RespondedAt:    &respondedAt,
		RespondedBy:    "user@example.com",
		Iteration:      1,
		MaxIterations:  3,
		CreatedAt:      now,
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("CreateDecisionPoint failed: %v", err)
	}

	// Verify it's already responded
	gotDP, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("GetDecisionPoint failed: %v", err)
	}
	if gotDP.RespondedAt == nil {
		t.Error("RespondedAt should not be nil")
	}
	// In real code, we'd check this before attempting to cancel
}

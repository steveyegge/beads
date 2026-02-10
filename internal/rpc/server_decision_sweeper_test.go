package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	sqlitestorage "github.com/steveyegge/beads/internal/storage/sqlite"
)

// setupDecisionStore creates a test server with custom types configured for gate issues.
func setupDecisionStore(t *testing.T) (*Server, *Client, *sqlitestorage.SQLiteStorage, func()) {
	t.Helper()
	// Initialize config so decision defaults (max_reminders=3, timeout=24h) are available.
	_ = config.Initialize()

	server, client, store, cleanup := setupTestServerWithStore(t)

	// Enable "gate" as a custom issue type so DecisionCreate can create gate issues.
	ctx := context.Background()
	if err := store.SetConfig(ctx, "types.custom", "gate"); err != nil {
		cleanup()
		t.Fatalf("failed to set types.custom: %v", err)
	}
	return server, client, store, cleanup
}

// createDecisionForSweep is a helper that creates a standalone gate + decision point
// via RPC and returns the gate issue ID.
func createDecisionForSweep(t *testing.T, client *Client, prompt string) string {
	t.Helper()
	// Don't pass IssueID — let DecisionCreate create its own gate issue.
	result, err := client.DecisionCreate(&DecisionCreateArgs{
		Prompt:      prompt,
		Options:     StringOptions("Yes", "No"),
		RequestedBy: "test-agent",
	})
	if err != nil {
		t.Fatalf("DecisionCreate failed: %v", err)
	}
	return result.Decision.IssueID
}

// TestSweepExpiredDecisions verifies that the sweeper expires decisions past their timeout.
func TestSweepExpiredDecisions(t *testing.T) {
	_, client, store, cleanup := setupDecisionStore(t)
	defer cleanup()

	ctx := context.Background()

	decisionID := createDecisionForSweep(t, client, "Should we proceed?")

	// Verify it's pending
	pending, err := store.ListPendingDecisions(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.IssueID == decisionID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected decision in pending list")
	}

	// Use a very short timeout (1ms) so the just-created decision is already expired.
	server := &Server{
		storage:         store,
		shutdownChan:    make(chan struct{}),
		decisionTimeout: 1 * time.Millisecond,
	}
	// Wait a moment to ensure the decision is past the 1ms timeout
	time.Sleep(5 * time.Millisecond)
	server.sweepExpiredDecisions()

	// Decision should now be expired
	updated, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("get decision after sweep: %v", err)
	}
	if updated.RespondedAt == nil {
		t.Fatal("expected decision to be expired (RespondedAt set)")
	}
	if updated.RespondedBy != "system:timeout" {
		t.Errorf("expected RespondedBy=system:timeout, got %s", updated.RespondedBy)
	}
	if updated.SelectedOption != "_expired" {
		t.Errorf("expected SelectedOption=_expired, got %s", updated.SelectedOption)
	}

	// Should no longer be in pending list
	pending, err = store.ListPendingDecisions(ctx)
	if err != nil {
		t.Fatalf("list pending after sweep: %v", err)
	}
	for _, p := range pending {
		if p.IssueID == decisionID {
			t.Error("expired decision should not be in pending list")
		}
	}
}

// TestSweepExpiredDecisions_NotExpired verifies that recent decisions are NOT expired.
func TestSweepExpiredDecisions_NotExpired(t *testing.T) {
	_, client, store, cleanup := setupDecisionStore(t)
	defer cleanup()

	ctx := context.Background()

	decisionID := createDecisionForSweep(t, client, "Recent decision?")

	server := &Server{
		storage:         store,
		shutdownChan:    make(chan struct{}),
		decisionTimeout: 24 * time.Hour,
	}
	server.sweepExpiredDecisions()

	// Should still be pending (just created, well within 24h timeout)
	updated, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if updated.RespondedAt != nil {
		t.Error("recent decision should NOT have been expired")
	}
}

// TestSweepExpiredDecisions_DefaultOption verifies that when a decision has a
// DefaultOption set, the sweeper uses it instead of "_expired".
func TestSweepExpiredDecisions_DefaultOption(t *testing.T) {
	_, client, store, cleanup := setupDecisionStore(t)
	defer cleanup()

	ctx := context.Background()

	result, err := client.DecisionCreate(&DecisionCreateArgs{
		Prompt:        "Continue?",
		Options:       StringOptions("Yes", "No"),
		DefaultOption: "Yes",
		RequestedBy:   "test-agent",
	})
	if err != nil {
		t.Fatalf("DecisionCreate: %v", err)
	}
	decisionID := result.Decision.IssueID

	// Use 1ms timeout so just-created decision is immediately expired
	server := &Server{
		storage:         store,
		shutdownChan:    make(chan struct{}),
		decisionTimeout: 1 * time.Millisecond,
	}
	time.Sleep(5 * time.Millisecond)
	server.sweepExpiredDecisions()

	updated, err := store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if updated.SelectedOption != "Yes" {
		t.Errorf("expected SelectedOption=Yes (default), got %s", updated.SelectedOption)
	}
}

// TestDecisionRemind_HitsMax verifies that the daemon's handleDecisionRemind
// succeeds when reminder count reaches max (the escalation event is emitted
// internally — this test verifies the path doesn't error).
func TestDecisionRemind_HitsMax(t *testing.T) {
	_, client, _, cleanup := setupDecisionStore(t)
	defer cleanup()

	decisionID := createDecisionForSweep(t, client, "Approve deployment?")

	// Send 3 reminders to reach max (default max_reminders=3)
	for i := 1; i <= 3; i++ {
		result, err := client.DecisionRemind(&DecisionRemindArgs{
			IssueID: decisionID,
		})
		if err != nil {
			t.Fatalf("remind #%d: %v", i, err)
		}
		if result.ReminderCount != i {
			t.Errorf("remind #%d: expected count %d, got %d", i, i, result.ReminderCount)
		}
	}

	// 4th remind (without force) should fail — at max
	_, err := client.DecisionRemind(&DecisionRemindArgs{
		IssueID: decisionID,
	})
	if err == nil {
		t.Error("expected error when reminding past max, got nil")
	}
}

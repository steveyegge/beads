package dolt

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestGetAllEventsSince_UnionBothTables verifies that GetAllEventsSince returns
// events from both the events table (permanent issues) and wisp_events table
// (ephemeral/wisp issues), ordered by created_at.
func TestGetAllEventsSince_UnionBothTables(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	since := time.Now().UTC().Add(-1 * time.Second)

	// Create a permanent issue (events go to 'events' table)
	perm := &types.Issue{
		ID:        "test-ev-perm",
		Title:     "Permanent Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, perm, "tester"); err != nil {
		t.Fatalf("failed to create permanent issue: %v", err)
	}

	// Create an ephemeral issue (events go to 'wisp_events' table)
	wisp := &types.Issue{
		ID:        "test-ev-wisp",
		Title:     "Wisp Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.CreateIssue(ctx, wisp, "tester"); err != nil {
		t.Fatalf("failed to create wisp issue: %v", err)
	}

	// Query events since before both were created
	events, err := store.GetAllEventsSince(ctx, since)
	if err != nil {
		t.Fatalf("GetAllEventsSince failed: %v", err)
	}

	// Should have events from both tables (at least one 'created' event each)
	permFound, wispFound := false, false
	for _, e := range events {
		if e.IssueID == perm.ID {
			permFound = true
		}
		if e.IssueID == wisp.ID {
			wispFound = true
		}
	}
	if !permFound {
		t.Error("expected event from permanent issue (events table), not found")
	}
	if !wispFound {
		t.Error("expected event from wisp issue (wisp_events table), not found")
	}

	// Verify chronological ordering
	for i := 1; i < len(events); i++ {
		if events[i].CreatedAt.Before(events[i-1].CreatedAt) {
			t.Errorf("events not in chronological order: [%d] %v > [%d] %v",
				i-1, events[i-1].CreatedAt, i, events[i].CreatedAt)
		}
	}
}

// TestGetAllEventsSince_EmptyStore verifies that GetAllEventsSince returns an
// empty slice (not an error) when no events exist after the given time.
func TestGetAllEventsSince_EmptyStore(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	events, err := store.GetAllEventsSince(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty store, got %d", len(events))
	}
}

// TestCloseIssue_EmitsEvent verifies that CloseIssue writes an event row so
// audit consumers see the close operation. Regression guard for the parity gap
// where the dolt path did a raw UPDATE without calling CloseIssueInTx.
func TestCloseIssue_EmitsEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "ev-close-1",
		Title:     "Issue to close",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := store.CloseIssue(ctx, issue.ID, "all done", "tester", "sess-abc"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}

	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.EventType == types.EventClosed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'closed' event after CloseIssue, got %d events: %v", len(events), events)
	}
}

// TestAddLabel_EmitsEvent verifies that AddLabel writes a label_added event.
// Regression guard for the dolt path that used raw INSERT IGNORE with no event emission.
func TestAddLabel_EmitsEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "ev-label-add-1",
		Title:     "Issue for label event test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := store.AddLabel(ctx, issue.ID, "bug", "tester"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.EventType == types.EventLabelAdded {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'label_added' event after AddLabel, got %d events: %v", len(events), events)
	}
}

// TestRemoveLabel_EmitsEvent verifies that RemoveLabel writes a label_removed event.
// Regression guard for the dolt path that used raw DELETE with no event emission.
func TestRemoveLabel_EmitsEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "ev-label-rm-1",
		Title:     "Issue for label remove event test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := store.AddLabel(ctx, issue.ID, "urgent", "tester"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	if err := store.RemoveLabel(ctx, issue.ID, "urgent", "tester"); err != nil {
		t.Fatalf("RemoveLabel: %v", err)
	}

	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.EventType == types.EventLabelRemoved {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'label_removed' event after RemoveLabel, got %d events: %v", len(events), events)
	}
}

// TestCloseIssueInTransaction_EmitsEvent verifies that closing an issue via the
// transaction path (RunInTransaction → tx.CloseIssue) writes an event row.
// This exercises the actually-broken path: callers like bd batch (cmd/bd/batch.go:332)
// and bd mol squash hit doltTransaction.CloseIssue, which on pre-fix main did a
// raw UPDATE with no event insert. The Storage.CloseIssue path was already correct,
// so the non-transactional test above does not catch this regression.
func TestCloseIssueInTransaction_EmitsEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "ev-tx-close-1",
		Title:     "Issue to close in tx",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	err := store.RunInTransaction(ctx, "test: close in tx", func(tx storage.Transaction) error {
		return tx.CloseIssue(ctx, issue.ID, "all done", "tester", "sess-abc")
	})
	if err != nil {
		t.Fatalf("RunInTransaction/CloseIssue: %v", err)
	}

	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.EventType == types.EventClosed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'closed' event after tx.CloseIssue, got %d events: %v", len(events), events)
	}
}

// TestAddLabelInTransaction_EmitsEvent verifies that adding a label via the
// transaction path (RunInTransaction → tx.AddLabel) writes a label_added event.
// Callers include bd label add (cmd/bd/label.go:106), bd cook (cook.go:869),
// bd graph apply (graph_apply.go:240), bd mol bond (mol_bond.go:313), and the
// tracker engine's external-tracker pull (engine.go:732). On pre-fix main the
// dolt path used raw INSERT IGNORE with no event emission.
func TestAddLabelInTransaction_EmitsEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "ev-tx-label-add-1",
		Title:     "Issue for tx label add event test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	err := store.RunInTransaction(ctx, "test: add label in tx", func(tx storage.Transaction) error {
		return tx.AddLabel(ctx, issue.ID, "bug", "tester")
	})
	if err != nil {
		t.Fatalf("RunInTransaction/AddLabel: %v", err)
	}

	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.EventType == types.EventLabelAdded {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'label_added' event after tx.AddLabel, got %d events: %v", len(events), events)
	}
}

// TestRemoveLabelInTransaction_EmitsEvent verifies that removing a label via the
// transaction path (RunInTransaction → tx.RemoveLabel) writes a label_removed event.
// Same caller surface as AddLabel above (bd label remove, tracker engine pull, etc).
// On pre-fix main the dolt path used raw DELETE with no event emission.
func TestRemoveLabelInTransaction_EmitsEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "ev-tx-label-rm-1",
		Title:     "Issue for tx label remove event test",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := store.AddLabel(ctx, issue.ID, "urgent", "tester"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	err := store.RunInTransaction(ctx, "test: remove label in tx", func(tx storage.Transaction) error {
		return tx.RemoveLabel(ctx, issue.ID, "urgent", "tester")
	})
	if err != nil {
		t.Fatalf("RunInTransaction/RemoveLabel: %v", err)
	}

	events, err := store.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.EventType == types.EventLabelRemoved {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'label_removed' event after tx.RemoveLabel, got %d events: %v", len(events), events)
	}
}

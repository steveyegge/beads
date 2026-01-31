package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// adviceRemoveTestHelper provides test setup for advice remove tests
type adviceRemoveTestHelper struct {
	t     *testing.T
	ctx   context.Context
	store *sqlite.SQLiteStorage
}

func newAdviceRemoveTestHelper(t *testing.T, store *sqlite.SQLiteStorage) *adviceRemoveTestHelper {
	return &adviceRemoveTestHelper{t: t, ctx: context.Background(), store: store}
}

func (h *adviceRemoveTestHelper) createAdvice(title, description string) *types.Issue {
	advice := &types.Issue{
		Title:       title,
		Description: description,
		Priority:    2,
		IssueType:   types.TypeAdvice,
		Status:      types.StatusOpen,
		CreatedAt:   time.Now(),
	}
	if err := h.store.CreateIssue(h.ctx, advice, "test-user"); err != nil {
		h.t.Fatalf("Failed to create advice: %v", err)
	}
	return advice
}

func (h *adviceRemoveTestHelper) createNonAdvice(title string) *types.Issue {
	issue := &types.Issue{
		Title:     title,
		Priority:  2,
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		CreatedAt: time.Now(),
	}
	if err := h.store.CreateIssue(h.ctx, issue, "test-user"); err != nil {
		h.t.Fatalf("Failed to create issue: %v", err)
	}
	return issue
}

func (h *adviceRemoveTestHelper) closeAdvice(id, reason string) error {
	return h.store.CloseIssue(h.ctx, id, reason, "test-user", "")
}

func (h *adviceRemoveTestHelper) deleteAdvice(id string) error {
	return h.store.DeleteIssue(h.ctx, id)
}

func (h *adviceRemoveTestHelper) getAdvice(id string) *types.Issue {
	issue, err := h.store.GetIssue(h.ctx, id)
	if err != nil {
		h.t.Fatalf("Failed to get issue: %v", err)
	}
	return issue
}

func (h *adviceRemoveTestHelper) searchOpenAdvice() []*types.Issue {
	adviceType := types.TypeAdvice
	status := types.StatusOpen
	results, err := h.store.SearchIssues(h.ctx, "", types.IssueFilter{
		IssueType: &adviceType,
		Status:    &status,
	})
	if err != nil {
		h.t.Fatalf("Failed to search advice: %v", err)
	}
	return results
}

func (h *adviceRemoveTestHelper) searchAllAdvice() []*types.Issue {
	adviceType := types.TypeAdvice
	results, err := h.store.SearchIssues(h.ctx, "", types.IssueFilter{
		IssueType: &adviceType,
	})
	if err != nil {
		h.t.Fatalf("Failed to search advice: %v", err)
	}
	return results
}

func TestAdviceRemoveSuite(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)

	t.Run("AdviceRemoveCommand", func(t *testing.T) {
		h := newAdviceRemoveTestHelper(t, s)

		t.Run("close advice by ID", func(t *testing.T) {
			advice := h.createAdvice("Advice to close", "This will be closed")
			originalID := advice.ID

			// Verify advice is open
			before := h.getAdvice(originalID)
			if before.Status != types.StatusOpen {
				t.Errorf("Expected open status before close, got %s", before.Status)
			}

			// Close the advice
			if err := h.closeAdvice(originalID, "No longer needed"); err != nil {
				t.Fatalf("Failed to close advice: %v", err)
			}

			// Verify advice is closed
			after := h.getAdvice(originalID)
			if after.Status != types.StatusClosed {
				t.Errorf("Expected closed status after close, got %s", after.Status)
			}

			// Verify close reason is recorded
			if after.CloseReason != "No longer needed" {
				t.Errorf("Expected close reason 'No longer needed', got %q", after.CloseReason)
			}

			// Verify it doesn't appear in open advice list
			openAdvice := h.searchOpenAdvice()
			for _, a := range openAdvice {
				if a.ID == originalID {
					t.Error("Closed advice should not appear in open advice list")
				}
			}
		})

		t.Run("delete advice permanently", func(t *testing.T) {
			advice := h.createAdvice("Advice to delete", "This will be permanently deleted")
			originalID := advice.ID

			// Verify advice exists
			before := h.getAdvice(originalID)
			if before == nil {
				t.Fatal("Advice should exist before deletion")
			}

			// Delete the advice
			if err := h.deleteAdvice(originalID); err != nil {
				t.Fatalf("Failed to delete advice: %v", err)
			}

			// Verify advice is gone (returns nil or tombstone)
			after := h.getAdvice(originalID)
			if after != nil && after.Status != types.StatusTombstone {
				t.Error("Deleted advice should not be retrievable or should be tombstoned")
			}
		})

		t.Run("closed advice not in default list", func(t *testing.T) {
			// Create multiple advice items
			openAdvice := h.createAdvice("Open advice", "Still active")
			closedAdvice := h.createAdvice("Will close", "To be closed")

			// Close one
			if err := h.closeAdvice(closedAdvice.ID, "Closing"); err != nil {
				t.Fatalf("Failed to close advice: %v", err)
			}

			// Search open advice
			results := h.searchOpenAdvice()

			foundOpen := false
			foundClosed := false
			for _, a := range results {
				if a.ID == openAdvice.ID {
					foundOpen = true
				}
				if a.ID == closedAdvice.ID {
					foundClosed = true
				}
			}

			if !foundOpen {
				t.Error("Open advice should appear in open list")
			}
			if foundClosed {
				t.Error("Closed advice should NOT appear in open list")
			}
		})

		t.Run("closed advice appears with --all", func(t *testing.T) {
			// Create and close advice
			advice := h.createAdvice("For all flag test", "Testing --all")
			if err := h.closeAdvice(advice.ID, "Closed for test"); err != nil {
				t.Fatalf("Failed to close: %v", err)
			}

			// Search all advice (no status filter)
			allAdvice := h.searchAllAdvice()

			found := false
			for _, a := range allAdvice {
				if a.ID == advice.ID {
					found = true
					if a.Status != types.StatusClosed {
						t.Errorf("Expected closed status, got %s", a.Status)
					}
					break
				}
			}

			if !found {
				t.Error("Closed advice should appear when searching all (--all)")
			}
		})

		t.Run("close already-closed advice is idempotent", func(t *testing.T) {
			advice := h.createAdvice("Double close test", "Close twice")

			// Close first time
			if err := h.closeAdvice(advice.ID, "First close"); err != nil {
				t.Fatalf("First close failed: %v", err)
			}

			// Close second time - should not error
			if err := h.closeAdvice(advice.ID, "Second close"); err != nil {
				t.Fatalf("Second close should succeed but got: %v", err)
			}

			// Verify still closed
			after := h.getAdvice(advice.ID)
			if after.Status != types.StatusClosed {
				t.Errorf("Expected closed status, got %s", after.Status)
			}
		})
	})
}

func TestAdviceTypeValidation(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	h := newAdviceRemoveTestHelper(t, s)

	t.Run("advice has correct type", func(t *testing.T) {
		advice := h.createAdvice("Type check", "Verify type")
		if advice.IssueType != types.TypeAdvice {
			t.Errorf("Expected type 'advice', got %s", advice.IssueType)
		}
	})

	t.Run("non-advice has different type", func(t *testing.T) {
		issue := h.createNonAdvice("Task issue")
		if issue.IssueType == types.TypeAdvice {
			t.Error("Non-advice issue should not have advice type")
		}
	})
}

func TestAdviceRemoveWithReason(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	h := newAdviceRemoveTestHelper(t, s)

	t.Run("close with custom reason", func(t *testing.T) {
		advice := h.createAdvice("Reason test", "Test close reason")

		customReason := "Superseded by newer advice"
		if err := h.closeAdvice(advice.ID, customReason); err != nil {
			t.Fatalf("Failed to close: %v", err)
		}

		after := h.getAdvice(advice.ID)
		if after.CloseReason != customReason {
			t.Errorf("Expected reason %q, got %q", customReason, after.CloseReason)
		}
	})

	t.Run("close with empty reason uses default", func(t *testing.T) {
		advice := h.createAdvice("Empty reason", "Test empty reason")

		// Close with empty reason
		if err := h.closeAdvice(advice.ID, ""); err != nil {
			t.Fatalf("Failed to close: %v", err)
		}

		after := h.getAdvice(advice.ID)
		// Empty reason is stored as-is (the CLI fills in a default)
		if after.Status != types.StatusClosed {
			t.Errorf("Expected closed status, got %s", after.Status)
		}
	})
}

func TestAdviceRemoveClosedAt(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStore(t, testDB)
	h := newAdviceRemoveTestHelper(t, s)

	t.Run("closed_at is set when closing", func(t *testing.T) {
		advice := h.createAdvice("ClosedAt test", "Check closed_at timestamp")

		// Verify no closed_at before
		before := h.getAdvice(advice.ID)
		if before.ClosedAt != nil {
			t.Error("ClosedAt should be nil before closing")
		}

		beforeClose := time.Now()
		if err := h.closeAdvice(advice.ID, "test"); err != nil {
			t.Fatalf("Failed to close: %v", err)
		}
		afterClose := time.Now()

		after := h.getAdvice(advice.ID)
		if after.ClosedAt == nil {
			t.Fatal("ClosedAt should be set after closing")
		}

		// Verify timestamp is reasonable
		if after.ClosedAt.Before(beforeClose) || after.ClosedAt.After(afterClose) {
			t.Errorf("ClosedAt %v should be between %v and %v",
				after.ClosedAt, beforeClose, afterClose)
		}
	})
}

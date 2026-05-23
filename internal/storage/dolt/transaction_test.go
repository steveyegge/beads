package dolt

import (
	"context"
	"database/sql"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

func TestRunInTransactionIgnoredWritesStayOnActiveBranch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	branch, err := store.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}

	wispID := "test-wisp-branch-local"
	wisp := &types.Issue{
		ID:        wispID,
		Title:     "branch-local ignored tx wisp",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.RunInTransaction(ctx, "test: create branch-local wisp", func(tx storage.Transaction) error {
		return tx.CreateIssue(ctx, wisp, "tester")
	}); err != nil {
		t.Fatalf("RunInTransaction create wisp: %v", err)
	}

	assertWispCount(ctx, t, store.db, wispID, 1)

	if err := store.Checkout(ctx, "main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	assertWispCount(ctx, t, store.db, wispID, 0)

	if err := store.Checkout(ctx, branch); err != nil {
		t.Fatalf("checkout %s: %v", branch, err)
	}
	assertWispCount(ctx, t, store.db, wispID, 1)
}

func assertWispCount(ctx context.Context, t *testing.T, db *sql.DB, id string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM wisps WHERE id = ?", id).Scan(&got); err != nil {
		t.Fatalf("query wisp count for %s: %v", id, err)
	}
	if got != want {
		t.Fatalf("wisp count for %s = %d, want %d", id, got, want)
	}
}

package dolt

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestCreateIssueCommitsInitialRelationalData(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createdAt := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	issue := &types.Issue{
		ID:          "create-relational-data",
		Title:       "Create with relational data",
		Description: "labels and comments should live in the create commit",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		Labels:      []string{"gc:wisp", "status:pending"},
		Comments: []*types.Comment{
			{
				Author:    "tester",
				Text:      "seed comment",
				CreatedAt: createdAt,
			},
		},
	}

	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	var labelCount int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM labels AS OF 'HEAD' WHERE issue_id = ?",
		issue.ID,
	).Scan(&labelCount); err != nil {
		t.Fatalf("count committed labels: %v", err)
	}
	if labelCount != 2 {
		t.Fatalf("committed label count = %d, want 2", labelCount)
	}

	var commentCount int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM comments AS OF 'HEAD' WHERE issue_id = ?",
		issue.ID,
	).Scan(&commentCount); err != nil {
		t.Fatalf("count committed comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("committed comment count = %d, want 1", commentCount)
	}

	var labelEventCount int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events AS OF 'HEAD' WHERE issue_id = ? AND event_type = ?",
		issue.ID, types.EventLabelAdded,
	).Scan(&labelEventCount); err != nil {
		t.Fatalf("count committed label events: %v", err)
	}
	if labelEventCount != 2 {
		t.Fatalf("committed label_added event count = %d, want 2", labelEventCount)
	}

	var dirtyRelationalTables int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dolt_status
		WHERE table_name IN ('labels', 'comments', 'events')
	`).Scan(&dirtyRelationalTables); err != nil {
		t.Fatalf("count dirty relational tables: %v", err)
	}
	if dirtyRelationalTables != 0 {
		t.Fatalf("dirty relational table count = %d, want 0", dirtyRelationalTables)
	}
}

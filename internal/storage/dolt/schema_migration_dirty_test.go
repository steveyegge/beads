package dolt

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestSchemaMigrationDoesNotCommitPreExistingDirtyData(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	issue := &types.Issue{
		ID:        "schema-dirty-label",
		Title:     "schema migration dirty label",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := store.SetConfig(ctx, "status.custom", "review:wip"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if err := store.CommitWithConfig(ctx, "test: configure custom status"); err != nil {
		t.Fatalf("CommitWithConfig: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, "DELETE FROM custom_statuses"); err != nil {
		t.Fatalf("clear custom_statuses: %v", err)
	}
	if err := store.doltAddAndCommit(ctx, []string{"custom_statuses"}, "test: simulate missing custom status backfill"); err != nil {
		t.Fatalf("commit cleared custom_statuses: %v", err)
	}

	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO labels (issue_id, label) VALUES (?, ?)",
		issue.ID, "dirty-before-schema",
	); err != nil {
		t.Fatalf("insert dirty label: %v", err)
	}

	if err := initSchemaOnDB(ctx, store.db); err != nil {
		t.Fatalf("initSchemaOnDB: %v", err)
	}

	var committedLabelCount int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM labels AS OF 'HEAD' WHERE issue_id = ? AND label = ?",
		issue.ID, "dirty-before-schema",
	).Scan(&committedLabelCount); err != nil {
		t.Fatalf("count committed dirty label: %v", err)
	}
	if committedLabelCount != 0 {
		t.Fatalf("dirty label was committed by schema migration, count = %d", committedLabelCount)
	}

	var workingLabelCount int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM labels WHERE issue_id = ? AND label = ?",
		issue.ID, "dirty-before-schema",
	).Scan(&workingLabelCount); err != nil {
		t.Fatalf("count working dirty label: %v", err)
	}
	if workingLabelCount != 1 {
		t.Fatalf("working label count = %d, want 1", workingLabelCount)
	}

	var dirtyLabelTables int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dolt_status WHERE table_name = 'labels'",
	).Scan(&dirtyLabelTables); err != nil {
		t.Fatalf("count dirty label tables: %v", err)
	}
	if dirtyLabelTables != 1 {
		t.Fatalf("dirty labels table count = %d, want 1", dirtyLabelTables)
	}

	var committedCustomStatuses int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM custom_statuses AS OF 'HEAD' WHERE name = ?",
		"review",
	).Scan(&committedCustomStatuses); err != nil {
		t.Fatalf("count committed custom statuses: %v", err)
	}
	if committedCustomStatuses != 1 {
		t.Fatalf("committed custom status count = %d, want 1", committedCustomStatuses)
	}
}

package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

const (
	activeIssuesQuery = `(?s)SELECT id FROM issues\s+WHERE status NOT IN \('closed', 'pinned'\)`
	blockingDepsQuery = `(?s)SELECT issue_id, COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) AS depends_on_id, type, metadata FROM dependencies\s+WHERE issue_id IN \(.+\)\s+AND type IN \('blocks', 'waits-for', 'conditional-blocks'\)`
	childrenQuery     = `(?s)SELECT issue_id, COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) AS depends_on_id FROM dependencies\s+WHERE type = 'parent-child' AND COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) IN \(.+\)`
	activeChildQuery  = `(?s)SELECT id FROM issues\s+WHERE id IN \(.+\)\s+AND status NOT IN \('closed', 'pinned'\)`
	closedChildQuery  = `(?s)SELECT id FROM issues\s+WHERE status = 'closed' AND id IN \(.+\)`
)

func TestComputeBlockedIDsInTxDependencySemantics(t *testing.T) {
	ctx := context.Background()
	db, mock, tx := newBlockedMockTx(t, ctx)
	defer closeBlockedMockTx(t, db, mock, tx)

	mock.ExpectQuery(activeIssuesQuery).WillReturnRows(sqlmock.NewRows([]string{"id"}).
		AddRow("issue-blocked").
		AddRow("blocker-active").
		AddRow("issue-closed-blocker").
		AddRow("issue-conditional").
		AddRow("conditional-blocker").
		AddRow("wait-default-active-child").
		AddRow("spawner-default").
		AddRow("child-default-active").
		AddRow("wait-default-closed-child").
		AddRow("spawner-default-closed").
		AddRow("wait-any-active-only").
		AddRow("spawner-any-active").
		AddRow("child-any-active").
		AddRow("wait-any-with-closed").
		AddRow("spawner-any-closed").
		AddRow("child-any-active2"))

	mock.ExpectQuery(blockingDepsQuery).WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type", "metadata"}).
		AddRow("issue-blocked", "blocker-active", "blocks", nil).
		AddRow("issue-closed-blocker", "blocker-closed", "blocks", nil).
		AddRow("issue-conditional", "conditional-blocker", "conditional-blocks", nil).
		AddRow("wait-default-active-child", "spawner-default", "waits-for", nil).
		AddRow("wait-default-closed-child", "spawner-default-closed", "waits-for", nil).
		AddRow("wait-any-active-only", "spawner-any-active", "waits-for", `{"gate":"any-children"}`).
		AddRow("wait-any-with-closed", "spawner-any-closed", "waits-for", `{"gate":"any-children"}`))

	mock.ExpectQuery(childrenQuery).WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id"}).
		AddRow("child-default-active", "spawner-default").
		AddRow("child-default-closed", "spawner-default-closed").
		AddRow("child-any-active", "spawner-any-active").
		AddRow("child-any-active2", "spawner-any-closed").
		AddRow("child-any-closed", "spawner-any-closed"))

	mock.ExpectQuery(activeChildQuery).WillReturnRows(sqlmock.NewRows([]string{"id"}).
		AddRow("child-default-active").
		AddRow("child-any-active").
		AddRow("child-any-active2"))

	mock.ExpectQuery(closedChildQuery).WillReturnRows(sqlmock.NewRows([]string{"id"}).
		AddRow("child-default-closed").
		AddRow("child-any-closed"))

	got, activeIDs, err := ComputeBlockedIDsInTx(ctx, tx, false)
	if err != nil {
		t.Fatalf("ComputeBlockedIDsInTx: %v", err)
	}
	assertStringSet(t, got, []string{
		"issue-blocked",
		"issue-conditional",
		"wait-default-active-child",
		"wait-any-active-only",
	})

	for _, id := range []string{
		"issue-closed-blocker",
		"wait-default-closed-child",
		"wait-any-with-closed",
	} {
		if containsString(got, id) {
			t.Fatalf("expected %s not to be blocked; got %v", id, got)
		}
	}

	if !activeIDs["child-default-active"] || activeIDs["child-default-closed"] {
		t.Fatalf("active child lookup = %v, want active child only", activeIDs)
	}
}

func TestComputeBlockedIDsInTxEmptyActiveSetSkipsDependencyLoad(t *testing.T) {
	ctx := context.Background()
	db, mock, tx := newBlockedMockTx(t, ctx)
	defer closeBlockedMockTx(t, db, mock, tx)

	mock.ExpectQuery(activeIssuesQuery).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, activeIDs, err := ComputeBlockedIDsInTx(ctx, tx, false)
	if err != nil {
		t.Fatalf("ComputeBlockedIDsInTx: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("blocked IDs = %v, want empty", got)
	}
	if len(activeIDs) != 0 {
		t.Fatalf("active IDs = %v, want empty", activeIDs)
	}
}

func TestComputeBlockedIDsInTxBatchesActiveDependencyScan(t *testing.T) {
	ctx := context.Background()
	db, mock, tx := newBlockedMockTx(t, ctx)
	defer closeBlockedMockTx(t, db, mock, tx)

	activeRows := sqlmock.NewRows([]string{"id"}).
		AddRow("issue-batch").
		AddRow("blocker-batch")
	for i := 0; i < queryBatchSize-1; i++ {
		activeRows.AddRow(fmt.Sprintf("filler-%03d", i))
	}
	mock.ExpectQuery(activeIssuesQuery).WillReturnRows(activeRows)

	mock.ExpectQuery(blockingDepsQuery).WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type", "metadata"}))
	mock.ExpectQuery(blockingDepsQuery).WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type", "metadata"}).
		AddRow("issue-batch", "blocker-batch", "blocks", nil))

	got, _, err := ComputeBlockedIDsInTx(ctx, tx, false)
	if err != nil {
		t.Fatalf("ComputeBlockedIDsInTx: %v", err)
	}
	assertStringSet(t, got, []string{"issue-batch"})
}

func newBlockedMockTx(t *testing.T, ctx context.Context) (*sql.DB, sqlmock.Sqlmock, *sql.Tx) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	mock.ExpectBegin()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	return db, mock, tx
}

func closeBlockedMockTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock, tx *sql.Tx) {
	t.Helper()

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func assertStringSet(t *testing.T, got []string, want []string) {
	t.Helper()

	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

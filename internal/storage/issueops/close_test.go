package issueops

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/types"
)

// expectAffectedByStatusChangeNone sets up the AffectedByStatusChangeInTx query
// chain for a non-wisp issue with no blocking dependers, waiters, or children.
func expectAffectedByStatusChangeNone(mock sqlmock.Sqlmock, id string) {
	// IsActiveWispInTx: not a wisp.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM wisps WHERE id = ?")).
		WithArgs(id).
		WillReturnError(sql.ErrNoRows)
	// loadBlockingDependersInTx: no dependers in either table.
	mock.ExpectQuery("SELECT issue_id FROM dependencies").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}))
	mock.ExpectQuery("SELECT issue_id FROM wisp_dependencies").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}))
	// loadWaitersWhoseSpawnerIsParentOfInTx: no waiters.
	mock.ExpectQuery("SELECT depends_on_issue_id, depends_on_wisp_id").
		WillReturnRows(sqlmock.NewRows([]string{"depends_on_issue_id", "depends_on_wisp_id"}))
	// expandByParentChildDescendantsInTx: no children in either table.
	mock.ExpectQuery("SELECT issue_id FROM dependencies\\s+WHERE type = 'parent-child'").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}))
	mock.ExpectQuery("SELECT issue_id FROM wisp_dependencies\\s+WHERE type = 'parent-child'").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}))
}

// TestCloseIssueInTx_AlreadyClosedIsIdempotent verifies that re-closing an
// issue that is already closed returns success rather than "issue not found".
// The UPDATE affects 0 rows (status unchanged), so the code falls back to a
// status probe; finding StatusClosed, it returns a CloseResult without error.
func TestCloseIssueInTx_AlreadyClosedIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const id = "bd-1"

	mock.ExpectBegin()
	expectAffectedByStatusChangeNone(mock, id)
	// UPDATE affects 0 rows (already closed, status unchanged).
	mock.ExpectExec("UPDATE issues SET status").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Status probe finds the issue already closed.
	mock.ExpectQuery("SELECT status FROM issues WHERE id = ?").
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(string(types.StatusClosed)))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	res, err := CloseIssueInTx(context.Background(), tx, id, "done", "tester", "sess-1")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("CloseIssueInTx on already-closed issue: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil CloseResult")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// TestCloseIssueInTx_MissingIssueStillErrors verifies that the idempotent
// re-close path does not mask a genuinely missing issue: when the UPDATE
// affects 0 rows and the status probe finds no row, the call still errors.
func TestCloseIssueInTx_MissingIssueStillErrors(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const id = "bd-missing"

	mock.ExpectBegin()
	expectAffectedByStatusChangeNone(mock, id)
	mock.ExpectExec("UPDATE issues SET status").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Status probe finds no such issue.
	mock.ExpectQuery("SELECT status FROM issues WHERE id = ?").
		WithArgs(id).
		WillReturnError(sql.ErrNoRows)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = CloseIssueInTx(context.Background(), tx, id, "done", "tester", "sess-1")
	_ = tx.Rollback()
	if err == nil {
		t.Fatalf("expected error closing missing issue, got nil")
	}
	if !strings.Contains(err.Error(), "issue not found") {
		t.Fatalf("error = %q, want it to mention 'issue not found'", err)
	}
}

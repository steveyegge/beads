package versioncontrolops

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// The replayed commits must keep their original committer dates: a tracker's
// last_sync stored before the compaction is compared with dolt_history's
// commit_date, and a history re-dated to the compaction time leaves only the
// initial commit before it. The committer goes in through session variables,
// so the cherry-pick keeps each commit's own author, author date and message.
func TestCompactRestoresCommitMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT commit_hash, committer, email, DATE_FORMAT(date, '%Y-%m-%dT%H:%i:%s.%fZ') FROM dolt_log")).
		WillReturnRows(sqlmock.NewRows([]string{"commit_hash", "committer", "email", "date"}).
			AddRow("r2hash", "Ann", "ann@example.com", "2026-10-02T09:00:00.250000Z").
			AddRow("r1hash", "Jane (work)", "jane@example.com", "2026-10-01T08:00:00.125000Z").
			AddRow("bndhash", "Ann", "ann@example.com", "2026-09-01T07:00:00.000000Z").
			AddRow("inithash", "Ann", "ann@example.com", "2026-01-01T00:00:00.000000Z"))

	exec := func(query string, args ...any) {
		expected := make([]driver.Value, len(args))
		for i, a := range args {
			expected[i] = a
		}
		mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(expected...).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	exec("CALL DOLT_BRANCH('compact-tmp', ?)", "bndhash")
	exec("CALL DOLT_CHECKOUT('compact-tmp')")
	exec("CALL DOLT_RESET('--soft', ?)", "inithash")
	exec("CALL DOLT_COMMIT('-Am', ?, '--date', ?)", "compact: squash 2 commits into base snapshot", "2026-09-01T07:00:00.000000Z")
	setCommitter := "SET @@dolt_committer_name = ?, @@dolt_committer_email = ?, @@dolt_committer_date = ?"
	exec(setCommitter, "Jane (work)", "jane@example.com", "2026-10-01T08:00:00.125000Z")
	exec("CALL DOLT_CHERRY_PICK('--allow-empty', ?)", "r1hash")
	exec(setCommitter, "Ann", "ann@example.com", "2026-10-02T09:00:00.250000Z")
	exec("CALL DOLT_CHERRY_PICK('--allow-empty', ?)", "r2hash")
	exec("CALL DOLT_CHECKOUT('main')")
	exec("CALL DOLT_RESET('--hard', 'compact-tmp')")
	exec("CALL DOLT_BRANCH('-D', 'compact-tmp')")
	exec("SET @@dolt_committer_name = '', @@dolt_committer_email = '', @@dolt_committer_date = ''")

	if err := Compact(context.Background(), db, "inithash", "bndhash", 2, []string{"r1hash", "r2hash"}); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A hash missing from the log must stop the compaction before the temp branch
// exists, rather than replaying a commit it cannot re-date.
func TestCompactRefusesUnknownRecentCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT commit_hash").
		WillReturnRows(sqlmock.NewRows([]string{"commit_hash", "committer", "email", "date"}).
			AddRow("bndhash", "Ann", "ann@example.com", "2026-09-01T07:00:00.000000Z"))

	err = Compact(context.Background(), db, "inithash", "bndhash", 1, []string{"missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Compact error = %v, want one naming the missing commit", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A replay that fails part-way must still clear the committer session
// variables, or every later commit on the caller's connection is attributed to
// the last replayed commit's committer and dated at its time.
func TestCompactClearsCommitterAfterFailedReplay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT commit_hash").
		WillReturnRows(sqlmock.NewRows([]string{"commit_hash", "committer", "email", "date"}).
			AddRow("r1hash", "Bob", "bob@example.com", "2026-10-01T08:00:00.125000Z").
			AddRow("bndhash", "Ann", "ann@example.com", "2026-09-01T07:00:00.000000Z"))
	ok := sqlmock.NewResult(0, 0)
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_BRANCH('compact-tmp', ?)")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_CHECKOUT('compact-tmp')")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_RESET('--soft', ?)")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_COMMIT('-Am', ?, '--date', ?)")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("SET @@dolt_committer_name = ?")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_CHERRY_PICK('--allow-empty', ?)")).WillReturnError(errors.New("conflict"))
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_CHECKOUT('main')")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_BRANCH('-D', 'compact-tmp')")).WillReturnResult(ok)
	mock.ExpectExec(regexp.QuoteMeta("SET @@dolt_committer_name = '', @@dolt_committer_email = '', @@dolt_committer_date = ''")).WillReturnResult(ok)

	if err := Compact(context.Background(), db, "inithash", "bndhash", 1, []string{"r1hash"}); err == nil {
		t.Fatal("Compact succeeded, want the cherry-pick error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

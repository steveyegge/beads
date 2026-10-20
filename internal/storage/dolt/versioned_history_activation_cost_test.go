package dolt

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync/atomic"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/storage/sqlcount"
	"github.com/steveyegge/beads/internal/versionedhistory"
)

// What versioned-history activation costs a server-mode store whose history is
// off, counted at the driver.
//
// Activation runs on every store a factory opens, reads included. It used to
// read the switch's row right there: BEGIN, SELECT value FROM config, ROLLBACK,
// three round trips on every open of a store that, for most commands, never
// writes. It now hands the store the read instead, and the store makes it
// inside its first write transaction: one SELECT in a transaction that is open
// anyway, and none at all for a store that only reads.
//
// The mock is reached through sqlcount, so a statement sqlmock refused (and
// the caller swallowed, as StoreSetting swallows every error) still counts.

var vhConfigRead = regexp.QuoteMeta("SELECT value FROM config WHERE `key` = ?")

var vhMockSeq atomic.Int64

// vhCountedStore is a DoltStore over a fresh sqlmock connection that is
// reached through sqlcount.
func vhCountedStore(t *testing.T) (*DoltStore, sqlmock.Sqlmock, *sqlcount.Counts) {
	t.Helper()
	dsn := fmt.Sprintf("dolt_vh_activation_%d", vhMockSeq.Add(1))
	mockDB, mock, err := sqlmock.NewWithDSN(dsn)
	if err != nil {
		t.Fatalf("sqlmock.NewWithDSN: %v", err)
	}
	counts := &sqlcount.Counts{}
	db := sql.OpenDB(sqlcount.WrapConnector(vhMockConnector{dsn: dsn, driver: mockDB.Driver()}, counts))
	t.Cleanup(func() {
		_ = db.Close()
		_ = mockDB.Close()
	})
	return &DoltStore{db: db}, mock, counts
}

type vhMockConnector struct {
	dsn    string
	driver driver.Driver
}

func (c vhMockConnector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c vhMockConnector) Driver() driver.Driver                        { return c.driver }

func vhRow(value string, present bool) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"value"})
	if present {
		rows.AddRow(value)
	}
	return rows
}

// vhWrite runs one write transaction through the store's own write funnel and
// reports whether it was scoped for minting.
func vhWrite(t *testing.T, store *DoltStore, mock sqlmock.Sqlmock, readRow *sqlmock.Rows) bool {
	t.Helper()
	mock.ExpectBegin()
	if readRow != nil {
		mock.ExpectQuery(vhConfigRead).WithArgs(versionedhistory.ConfigKey).WillReturnRows(readRow)
	}
	mock.ExpectExec(regexp.QuoteMeta("UPDATE vh_probe SET n = n + 1")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	var scoped bool
	if err := store.withWriteTx(context.Background(), func(tx *sql.Tx) error {
		scoped = issueops.VersionedHistoryEnabledFor(tx)
		_, err := tx.ExecContext(context.Background(), "UPDATE vh_probe SET n = n + 1")
		return err
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	return scoped
}

// TestActivateStoreSendsNoStatementsToAHistoryOffServerStore is the pin: the
// activation itself sends nothing, and the switch's row is read once, inside
// the first write transaction, before that transaction's body runs.
func TestActivateStoreSendsNoStatementsToAHistoryOffServerStore(t *testing.T) {
	for _, tc := range []struct {
		name    string
		row     string
		present bool
		want    bool
	}{
		{name: "no row", want: false},
		{name: "row false", row: "false", present: true, want: false},
		{name: "row true", row: "true", present: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, mock, counts := vhCountedStore(t)
			if _, err := versionedhistory.ActivateStore(context.Background(), store, nil); err != nil {
				t.Fatal(err)
			}
			if got := counts.Snapshot(); got != (sqlcount.Snapshot{}) {
				t.Fatalf("activation sent %+v to the store; want nothing: the switch's row is the first write's to read", got)
			}

			// A read is not a write: it never reads the row.
			mock.ExpectBegin()
			mock.ExpectQuery(vhConfigRead).WithArgs("issue_prefix").WillReturnRows(vhRow("vh", true))
			mock.ExpectRollback()
			if _, err := store.GetConfig(context.Background(), "issue_prefix"); err != nil {
				t.Fatal(err)
			}

			if got := vhWrite(t, store, mock, vhRow(tc.row, tc.present)); got != tc.want {
				t.Errorf("first write scoped %v, want %v", got, tc.want)
			}
			if got := vhWrite(t, store, mock, nil); got != tc.want {
				t.Errorf("second write scoped %v, want %v without reading the row again", got, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if got := counts.Snapshot().Total(); got != 1+3 {
				t.Errorf("read + two writes sent %d statements, want 4: one read, one switch read, two updates", got)
			}
		})
	}
}

// TestDeferredSwitchReadErrorIsRetriedByTheNextWrite: a write whose switch read
// fails is scoped off, and the next write reads again.
func TestDeferredSwitchReadErrorIsRetriedByTheNextWrite(t *testing.T) {
	store, mock, _ := vhCountedStore(t)
	if _, err := versionedhistory.ActivateStore(context.Background(), store, nil); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(vhConfigRead).WithArgs(versionedhistory.ConfigKey).WillReturnError(errors.New("connection reset"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE vh_probe SET n = n + 1")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := store.withWriteTx(context.Background(), func(tx *sql.Tx) error {
		if issueops.VersionedHistoryEnabledFor(tx) {
			t.Error("a write whose switch read failed was scoped on")
		}
		_, err := tx.ExecContext(context.Background(), "UPDATE vh_probe SET n = n + 1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !vhWrite(t, store, mock, vhRow("true", true)) {
		t.Error("the write after a failed switch read did not read the row again")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestStagingAnswersADeferredSwitch: a write that ran in a transaction this
// store did not scope (AddComment's) reaches its staging list first, and the
// list still carries the version tables when the row says on.
func TestStagingAnswersADeferredSwitch(t *testing.T) {
	store, mock, _ := vhCountedStore(t)
	if _, err := versionedhistory.ActivateStore(context.Background(), store, nil); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(vhConfigRead).WithArgs(versionedhistory.ConfigKey).WillReturnRows(vhRow("true", true))
	got := store.withVersionedHistoryTables(context.Background(), store.db, []string{"events"})
	for _, table := range issueops.VersionedHistoryStagedTables() {
		if !slices.Contains(got, table) {
			t.Errorf("staging list %v is missing %s with the row on", got, table)
		}
	}
	if again := store.withVersionedHistoryTables(context.Background(), store.db, []string{"events"}); !slices.Equal(again, got) {
		t.Errorf("second staging list %v != first %v", again, got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

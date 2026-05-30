package schema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/testutil"
)

// TestEnsureContentHashColumnAddsWhenMissing verifies the idempotent upgrade adds
// the content_hash column to an existing cursor table that lacks it.
func TestEnsureContentHashColumnAddsWhenMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA\.COLUMNS`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`ALTER TABLE schema_migrations ADD COLUMN content_hash CHAR\(64\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := mainSource.ensureContentHashColumn(context.Background(), db); err != nil {
		t.Fatalf("ensureContentHashColumn: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestEnsureContentHashColumnNoOpWhenPresent verifies it issues no ALTER when the
// column already exists.
func TestEnsureContentHashColumnNoOpWhenPresent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA\.COLUMNS`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// No ExpectExec: an ALTER here would be an unexpected call.

	if err := mainSource.ensureContentHashColumn(context.Background(), db); err != nil {
		t.Fatalf("ensureContentHashColumn: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestAllMigrationsSQLRecordsContentHashes applies the full migration bundle
// through the dolt CLI and verifies every recorded migration carries the SHA-256
// of its migration file content (gastownhall/beads#4259 reporter fix No.2).
func TestAllMigrationsSQLRecordsContentHashes(t *testing.T) {
	testutil.RequireDoltBinary(t)

	dir := filepath.Join(t.TempDir(), "hash-bundle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create bundle dir: %v", err)
	}
	runDoltCommand(t, dir, "init", "--name", "test", "--email", "test@example.com")
	runDoltSQL(t, dir, AllMigrationsSQL())

	got := map[string]string{}
	for _, r := range queryDoltCSV(t, dir, `SELECT version, content_hash FROM schema_migrations`) {
		got[r["version"]] = r["content_hash"]
	}

	for _, mf := range mainSource.list() {
		data, err := mainSource.files.ReadFile(mainSource.dir + "/" + mf.name)
		if err != nil {
			t.Fatalf("read migration %s: %v", mf.name, err)
		}
		sum := sha256.Sum256(data)
		want := hex.EncodeToString(sum[:])
		if h := got[strconv.Itoa(mf.version)]; h != want {
			t.Errorf("version %d content_hash = %q, want %q (sha256 of %s)", mf.version, h, want, mf.name)
		}
	}
}

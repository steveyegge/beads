package schema

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const maxVersionQuery = `SELECT COALESCE\(MAX\(version\), 0\) FROM schema_migrations`

// expectGateCurrentVersion mocks the MAX(version) read that both CurrentVersion
// and PendingVersions issue.
func expectGateCurrentVersion(mock sqlmock.Sqlmock, version int) {
	mock.ExpectQuery(maxVersionQuery).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(version))
}

func TestCheckRemoteMigrateGate(t *testing.T) {
	latest := LatestVersion()

	t.Run("escape hatch env var allows migration without any query", func(t *testing.T) {
		t.Setenv(AllowRemoteMigrateEnv, "1")
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		if err := CheckRemoteMigrateGate(context.Background(), db); err != nil {
			t.Fatalf("expected nil with escape hatch set, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unexpected queries with escape hatch: %v", err)
		}
	})

	t.Run("fresh database (version 0) is allowed", func(t *testing.T) {
		t.Setenv(AllowRemoteMigrateEnv, "0")
		db, mock, _ := sqlmock.New()
		defer db.Close()
		expectGateCurrentVersion(mock, 0)
		if err := CheckRemoteMigrateGate(context.Background(), db); err != nil {
			t.Fatalf("fresh DB should be allowed, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("already at latest (no pending) is allowed", func(t *testing.T) {
		t.Setenv(AllowRemoteMigrateEnv, "0")
		db, mock, _ := sqlmock.New()
		defer db.Close()
		expectGateCurrentVersion(mock, latest) // CurrentVersion
		expectGateCurrentVersion(mock, latest) // PendingVersions -> no pending
		if err := CheckRemoteMigrateGate(context.Background(), db); err != nil {
			t.Fatalf("at-latest DB should be allowed, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("pending migrations but no remote is allowed", func(t *testing.T) {
		t.Setenv(AllowRemoteMigrateEnv, "0")
		db, mock, _ := sqlmock.New()
		defer db.Close()
		expectGateCurrentVersion(mock, 1) // CurrentVersion
		expectGateCurrentVersion(mock, 1) // PendingVersions -> pending exists
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM dolt_remotes`).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		if err := CheckRemoteMigrateGate(context.Background(), db); err != nil {
			t.Fatalf("no-remote DB should be allowed, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("pending migrations with a remote is blocked", func(t *testing.T) {
		t.Setenv(AllowRemoteMigrateEnv, "0")
		db, mock, _ := sqlmock.New()
		defer db.Close()
		expectGateCurrentVersion(mock, 1) // CurrentVersion
		expectGateCurrentVersion(mock, 1) // PendingVersions -> pending exists
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM dolt_remotes`).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		err := CheckRemoteMigrateGate(context.Background(), db)
		var gateErr *RemoteMigrateGateError
		if !errors.As(err, &gateErr) {
			t.Fatalf("expected *RemoteMigrateGateError, got %v", err)
		}
		if gateErr.CurrentVersion != 1 {
			t.Errorf("CurrentVersion = %d, want 1", gateErr.CurrentVersion)
		}
		if gateErr.LatestVersion != latest {
			t.Errorf("LatestVersion = %d, want %d", gateErr.LatestVersion, latest)
		}
		if gateErr.Pending <= 0 {
			t.Errorf("Pending = %d, want > 0", gateErr.Pending)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("missing dolt_remotes table is treated as no remote", func(t *testing.T) {
		t.Setenv(AllowRemoteMigrateEnv, "0")
		db, mock, _ := sqlmock.New()
		defer db.Close()
		expectGateCurrentVersion(mock, 1)
		expectGateCurrentVersion(mock, 1)
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM dolt_remotes`).
			WillReturnError(errors.New("Error 1146: Table 'beads.dolt_remotes' doesn't exist"))
		if err := CheckRemoteMigrateGate(context.Background(), db); err != nil {
			t.Fatalf("missing dolt_remotes should be treated as no remote (allow), got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
}

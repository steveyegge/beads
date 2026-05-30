package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCheckMigrationContentSkew_NoDatabase(t *testing.T) {
	got := CheckMigrationContentSkew(&SharedStore{}) // Store() == nil
	if got.Status != StatusOK {
		t.Errorf("status = %q, want %q", got.Status, StatusOK)
	}
}

func expectRemoteAndBranch(mock sqlmock.Sqlmock, remote, branch string) {
	mock.ExpectQuery(`SELECT name FROM dolt_remotes`).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow(remote))
	mock.ExpectQuery(`SELECT active_branch\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"b"}).AddRow(branch))
}

func TestCheckMigrationContentSkew_NoRemote(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// dolt_remotes returns no rows -> Scan yields ErrNoRows -> skip.
	mock.ExpectQuery(`SELECT name FROM dolt_remotes`).
		WillReturnRows(sqlmock.NewRows([]string{"name"}))

	got := checkMigrationContentSkew(context.Background(), db)
	if got.Status != StatusOK {
		t.Errorf("status = %q, want %q (%s)", got.Status, StatusOK, got.Message)
	}
}

func TestCheckMigrationContentSkew_NoCachedRemoteRef(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectRemoteAndBranch(mock, "origin", "main")
	mock.ExpectQuery(`SELECT version, content_hash FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "content_hash"}).AddRow(1, "a"))
	mock.ExpectQuery(`AS OF CONCAT`).
		WillReturnError(errors.New("branch not found: remotes/origin/main"))

	got := checkMigrationContentSkew(context.Background(), db)
	if got.Status != StatusOK {
		t.Errorf("status = %q, want %q (%s)", got.Status, StatusOK, got.Message)
	}
}

func TestCheckMigrationContentSkew_Matches(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectRemoteAndBranch(mock, "origin", "main")
	mock.ExpectQuery(`SELECT version, content_hash FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "content_hash"}).AddRow(1, "a").AddRow(2, "b"))
	mock.ExpectQuery(`AS OF CONCAT`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "content_hash"}).AddRow(1, "a").AddRow(2, "b"))

	got := checkMigrationContentSkew(context.Background(), db)
	if got.Status != StatusOK {
		t.Errorf("status = %q, want %q (%s)", got.Status, StatusOK, got.Message)
	}
}

func TestCheckMigrationContentSkew_Diverges(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectRemoteAndBranch(mock, "origin", "main")
	mock.ExpectQuery(`SELECT version, content_hash FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "content_hash"}).AddRow(1, "a").AddRow(2, "b"))
	mock.ExpectQuery(`AS OF CONCAT`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "content_hash"}).AddRow(1, "a").AddRow(2, "DIFFERENT"))

	got := checkMigrationContentSkew(context.Background(), db)
	if got.Status != StatusWarning {
		t.Fatalf("status = %q, want %q (%s)", got.Status, StatusWarning, got.Message)
	}
	if !strings.Contains(got.Message, "0002") {
		t.Errorf("message = %q, want it to name migration 0002", got.Message)
	}
}

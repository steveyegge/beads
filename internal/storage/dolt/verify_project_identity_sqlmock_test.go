package dolt

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// TestVerifyProjectIdentityReturnsTypedMismatch (gascity mc-3gbed follow-up,
// PR #7561 review round 2). openWithEnvEndpointFallback only retries a
// refusal isProjectIdentityMismatch recognizes — verifyProjectIdentity's
// return has to stay the typed *projectIdentityMismatch wrap, not an untyped
// fmt.Errorf a caller's errors.As can no longer see, or the whole fallback
// silently stops firing on every mismatched project, with every call site
// still believing it works.
//
// This does not need a live Dolt server: verifyProjectIdentity's one query
// (metadata.json's local project_id against the database's, read through
// GetMetadata -> withReadTx -> issueops.GetMetadataInTx) is a single
// `SELECT value FROM metadata` inside a read transaction, which sqlmock
// stands in for directly.
func TestVerifyProjectIdentityReturnsTypedMismatch(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM metadata")).
		WithArgs("_project_id").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("db-project"))
	mock.ExpectRollback()

	dir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"project_id":"local-project"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &DoltStore{db: db, database: "beads"}
	got := s.verifyProjectIdentity(context.Background(), dir, false)
	if !isProjectIdentityMismatch(got) {
		t.Fatalf("verifyProjectIdentity returned %v; want a typed *projectIdentityMismatch openWithEnvEndpointFallback's errors.As can see", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

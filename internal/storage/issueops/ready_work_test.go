package issueops

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/types"
)

func deferredParentProbeRegex(issueTable string) string {
	return `SELECT 1 FROM ` + issueTable + `\s+WHERE defer_until IS NOT NULL\s+AND defer_until > UTC_TIMESTAMP\(\)\s+LIMIT 1`
}

func deferredChildrenQueryRegex(depTable, issueTable string) string {
	targetCol := "depends_on_issue_id"
	if issueTable == "wisps" {
		targetCol = "depends_on_wisp_id"
	}
	return `SELECT dep\.issue_id\s+FROM ` + depTable + ` dep\s+JOIN ` + issueTable + ` parent ON parent\.id = dep\.` + targetCol + `\s+WHERE dep\.type = 'parent-child'\s+AND parent\.defer_until IS NOT NULL\s+AND parent\.defer_until > UTC_TIMESTAMP\(\)`
}

func childrenOfIssuesQueryRegex(depTable string) string {
	return `SELECT issue_id FROM ` + depTable + `\s+WHERE type = 'parent-child' AND COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) IN \(\?\)`
}

func beginMockTx(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *sql.Tx) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	return db, mock, tx
}

func TestBuildSQLInClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		ids              []string
		wantPlaceholders string
		wantArgs         []interface{}
	}{
		{
			name:             "single ID",
			ids:              []string{"42"},
			wantPlaceholders: "?",
			wantArgs:         []interface{}{"42"},
		},
		{
			name:             "multiple IDs",
			ids:              []string{"1", "2", "3"},
			wantPlaceholders: "?,?,?",
			wantArgs:         []interface{}{"1", "2", "3"},
		},
		{
			name:             "empty slice",
			ids:              []string{},
			wantPlaceholders: "",
			wantArgs:         []interface{}{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPlaceholders, gotArgs := buildSQLInClause(tt.ids)

			if gotPlaceholders != tt.wantPlaceholders {
				t.Errorf("placeholders = %q, want %q", gotPlaceholders, tt.wantPlaceholders)
			}

			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("args length = %d, want %d", len(gotArgs), len(tt.wantArgs))
			}

			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %v, want %v", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestGetReadyWorkInTx_UnboundedPropagatesBlockedComputationError(t *testing.T) {
	t.Parallel()

	blockedErr := errors.New("blocked graph unavailable")
	_, err := GetReadyWorkInTx(
		context.Background(),
		nil,
		types.WorkFilter{IncludeDeferred: true},
		func(context.Context, *sql.Tx, bool) ([]string, error) {
			return nil, blockedErr
		},
	)
	if err == nil {
		t.Fatal("expected blocked computation error")
	}
	if !errors.Is(err, blockedErr) {
		t.Fatalf("expected wrapped blocked computation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "compute blocked IDs") {
		t.Fatalf("expected compute blocked IDs context, got %v", err)
	}
}

func TestGetReadyWorkInTx_UnboundedComputesBlockersAcrossWispsByDefault(t *testing.T) {
	t.Parallel()

	stopErr := errors.New("stop after blocker computation")
	var gotIncludeWisps bool
	_, err := GetReadyWorkInTx(
		context.Background(),
		nil,
		types.WorkFilter{IncludeDeferred: true},
		func(_ context.Context, _ *sql.Tx, includeWisps bool) ([]string, error) {
			gotIncludeWisps = includeWisps
			return nil, stopErr
		},
	)
	if !errors.Is(err, stopErr) {
		t.Fatalf("error = %v, want stop error", err)
	}
	if !gotIncludeWisps {
		t.Fatal("computeBlockedFn includeWisps = false, want true")
	}
}

func TestGetReadyWorkInTx_PropagatesDeferredParentChildError(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	childErr := errors.New("dolt transient dependency read failure")
	mock.ExpectQuery(deferredParentProbeRegex("issues")).WillReturnError(childErr)

	_, err := GetReadyWorkInTx(
		context.Background(),
		tx,
		types.WorkFilter{},
		func(context.Context, *sql.Tx, bool) ([]string, error) {
			t.Fatal("blocked computation should not run after deferred parent child failure")
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("expected deferred parent child error")
	}
	if !errors.Is(err, childErr) {
		t.Fatalf("expected wrapped deferred parent child error, got %v", err)
	}
	if !strings.Contains(err.Error(), "compute deferred parent children") {
		t.Fatalf("expected deferred parent child context, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestIsBlockedInTxErrorsOnIssueWispCollision(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(`(?s)SELECT COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) AS depends_on_id, type FROM dependencies\s+WHERE issue_id = \? AND type IN`).
		WithArgs("blocked-id").
		WillReturnRows(sqlmock.NewRows([]string{"depends_on_id", "type"}).AddRow("dup-id", "blocks"))
	mock.ExpectQuery(`(?s)SELECT COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) AS depends_on_id, type FROM wisp_dependencies\s+WHERE issue_id = \? AND type IN`).
		WithArgs("blocked-id").
		WillReturnRows(sqlmock.NewRows([]string{"depends_on_id", "type"}))
	mock.ExpectQuery("SELECT id, status FROM issues").
		WithArgs("dup-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("dup-id", types.StatusOpen))
	mock.ExpectQuery("SELECT id, status FROM wisps").
		WithArgs("dup-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("dup-id", types.StatusOpen))

	_, _, err := IsBlockedInTx(context.Background(), tx, "blocked-id")
	if err == nil {
		t.Fatal("expected duplicate issue/wisp status error")
	}
	if !strings.Contains(err.Error(), `id "dup-id" exists in both issues and wisps`) {
		t.Fatalf("error = %v, want duplicate issue/wisp context", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGetNewlyUnblockedByCloseInTxChecksRemainingBlockersInBatches(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(`(?s)SELECT issue_id FROM dependencies\s+WHERE COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) = \? AND type = 'blocks'`).
		WithArgs("closed-blocker").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).
			AddRow("candidate-a").
			AddRow("candidate-b"))
	mock.ExpectQuery(`(?s)SELECT issue_id FROM wisp_dependencies\s+WHERE COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) = \? AND type = 'blocks'`).
		WithArgs("closed-blocker").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}))
	mock.ExpectQuery(`SELECT id, status FROM issues WHERE id IN \(\?,\?\)`).
		WithArgs("candidate-a", "candidate-b").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).
			AddRow("candidate-a", types.StatusOpen).
			AddRow("candidate-b", types.StatusOpen))
	mock.ExpectQuery(`SELECT id, status FROM wisps WHERE id IN \(\?,\?\)`).
		WithArgs("candidate-a", "candidate-b").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}))
	mock.ExpectQuery(`(?s)SELECT issue_id, COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) AS depends_on_id FROM dependencies\s+WHERE issue_id IN \(\?,\?\) AND type = 'blocks' AND COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) != \?`).
		WithArgs("candidate-a", "candidate-b", "closed-blocker").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id"}).
			AddRow("candidate-a", "other-blocker").
			AddRow("candidate-b", "other-blocker"))
	mock.ExpectQuery(`(?s)SELECT issue_id, COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) AS depends_on_id FROM wisp_dependencies\s+WHERE issue_id IN \(\?,\?\) AND type = 'blocks' AND COALESCE\(depends_on_issue_id, depends_on_wisp_id, depends_on_external\) != \?`).
		WithArgs("candidate-a", "candidate-b", "closed-blocker").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id"}))
	mock.ExpectQuery(`SELECT id, status FROM issues WHERE id IN \(\?\)`).
		WithArgs("other-blocker").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("other-blocker", types.StatusOpen))
	mock.ExpectQuery(`SELECT id, status FROM wisps WHERE id IN \(\?\)`).
		WithArgs("other-blocker").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}))

	got, err := GetNewlyUnblockedByCloseInTx(context.Background(), tx, "closed-blocker")
	if err != nil {
		t.Fatalf("GetNewlyUnblockedByCloseInTx: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unblocked = %v, want none because both candidates still have an open blocker", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestLoadStatusByIDInTxErrorsOnIssueWispCollision(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery("SELECT id, status FROM issues").
		WithArgs("dup-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("dup-id", types.StatusOpen))
	mock.ExpectQuery("SELECT id, status FROM wisps").
		WithArgs("dup-id").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("dup-id", types.StatusClosed))

	_, err := loadStatusByIDInTx(context.Background(), tx, []string{"dup-id"})
	if err == nil {
		t.Fatal("expected duplicate issue/wisp status error")
	}
	if !strings.Contains(err.Error(), `id "dup-id" exists in both issues and wisps`) {
		t.Fatalf("error = %v, want duplicate issue/wisp context", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestMergeReadyWispsErrorsOnIssueWispCollision(t *testing.T) {
	t.Parallel()

	_, err := mergeReadyWisps(
		[]*types.Issue{{ID: "dup-id", Status: types.StatusOpen}},
		[]*types.Issue{{ID: "dup-id", Status: types.StatusClosed}},
		types.WorkFilter{},
	)
	if err == nil {
		t.Fatal("expected duplicate issue/wisp ready-work error")
	}
	if !strings.Contains(err.Error(), `id "dup-id" exists in both issues and wisps`) {
		t.Fatalf("error = %v, want duplicate issue/wisp context", err)
	}
}

func TestGetChildrenOfDeferredParentsInTx_ReturnsChildrenFromBothDependencyTables(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(deferredParentProbeRegex("issues")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery(deferredChildrenQueryRegex("dependencies", "issues")).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).AddRow("child-from-dependencies-issues"))
	mock.ExpectQuery(deferredChildrenQueryRegex("dependencies", "wisps")).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).AddRow("child-from-dependencies-wisps"))
	mock.ExpectQuery(deferredChildrenQueryRegex("wisp_dependencies", "issues")).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).AddRow("child-from-wisp-dependencies-issues"))
	mock.ExpectQuery(deferredChildrenQueryRegex("wisp_dependencies", "wisps")).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).AddRow("child-from-wisp-dependencies-wisps"))

	got, err := getChildrenOfDeferredParentsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("getChildrenOfDeferredParentsInTx: %v", err)
	}
	want := []string{
		"child-from-dependencies-issues",
		"child-from-dependencies-wisps",
		"child-from-wisp-dependencies-issues",
		"child-from-wisp-dependencies-wisps",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGetChildrenOfDeferredParentsInTx_NoDeferredParentsExitsAfterProbe(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(deferredParentProbeRegex("issues")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}))
	mock.ExpectQuery(deferredParentProbeRegex("wisps")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}))

	got, err := getChildrenOfDeferredParentsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("getChildrenOfDeferredParentsInTx: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("children = %v, want empty", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGetChildrenOfDeferredParentsInTx_IgnoresMissingWispDependenciesTable(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(deferredParentProbeRegex("issues")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery(deferredChildrenQueryRegex("dependencies", "issues")).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).AddRow("child-from-dependencies-issues"))
	mock.ExpectQuery(deferredChildrenQueryRegex("dependencies", "wisps")).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}).AddRow("child-from-dependencies-wisps"))
	mock.ExpectQuery(deferredChildrenQueryRegex("wisp_dependencies", "issues")).
		WillReturnError(errors.New("table wisp_dependencies does not exist"))

	got, err := getChildrenOfDeferredParentsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("getChildrenOfDeferredParentsInTx: %v", err)
	}
	want := []string{"child-from-dependencies-issues", "child-from-dependencies-wisps"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGetChildrenOfIssuesInTxPropagatesWispDependencyReadError(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	readErr := errors.New("permission denied reading wisp_dependencies")
	mock.ExpectQuery(childrenOfIssuesQueryRegex("dependencies")).
		WithArgs("parent-id").
		WillReturnRows(sqlmock.NewRows([]string{"issue_id"}))
	mock.ExpectQuery(childrenOfIssuesQueryRegex("wisp_dependencies")).
		WithArgs("parent-id").
		WillReturnError(readErr)

	_, err := getChildrenOfIssuesInTx(context.Background(), tx, []string{"parent-id"})
	if err == nil {
		t.Fatal("expected wisp_dependencies read error")
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped read error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

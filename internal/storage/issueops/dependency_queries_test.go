package issueops

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/types"
)

func allDependencyRecordsQueryRegex(table string) string {
	col := DepTargetColumnForTable(table)
	return `(?s)SELECT source_id, ` + regexp.QuoteMeta(col) + ` AS depends_on_id, type, created_at, created_by, metadata, thread_id\s+FROM ` +
		regexp.QuoteMeta(table) + `\s+ORDER BY source_id`
}

func dependencyRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"source_id",
		"depends_on_id",
		"type",
		"created_at",
		"created_by",
		"metadata",
		"thread_id",
	})
}

func TestGetAllDependencyRecordsInTxReadsPermanentAndWispDependencies(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	now := time.Now()
	// AllDepTables order: issue_issue, issue_wisp, issue_external,
	// wisp_issue, wisp_wisp, wisp_external. Return a perm row from the first
	// (issue-source) table and a wisp row from the fourth (wisp-source) table.
	for i, table := range AllDepTables() {
		rows := dependencyRows()
		switch i {
		case 0:
			rows.AddRow("perm-source", "perm-target", types.DepBlocks, now, "tester", "{}", "thread-perm")
		case 3:
			rows.AddRow("wisp-source", "wisp-target", types.DepParentChild, now, "tester", "{}", "thread-wisp")
		}
		mock.ExpectQuery(allDependencyRecordsQueryRegex(table)).WillReturnRows(rows)
	}

	got, err := GetAllDependencyRecordsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("GetAllDependencyRecordsInTx: %v", err)
	}
	if dep := onlyDependency(t, got, "perm-source"); dep.DependsOnID != "perm-target" {
		t.Fatalf("perm dependency target = %q, want perm-target", dep.DependsOnID)
	}
	if dep := onlyDependency(t, got, "wisp-source"); dep.DependsOnID != "wisp-target" {
		t.Fatalf("wisp dependency target = %q, want wisp-target", dep.DependsOnID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGetAllDependencyRecordsInTxToleratesMissingWispDependencyTable(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	for _, table := range AllDepTables() {
		isWisp := strings.HasPrefix(table, "wisp_")
		if isWisp {
			mock.ExpectQuery(allDependencyRecordsQueryRegex(table)).
				WillReturnError(errors.New("Error 1146: Table 'db." + table + "' doesn't exist"))
			continue
		}
		rows := dependencyRows()
		if table == "issue_issue_dependencies" {
			rows.AddRow("perm-source", "perm-target", types.DepBlocks, time.Now(), "tester", "{}", "")
		}
		mock.ExpectQuery(allDependencyRecordsQueryRegex(table)).WillReturnRows(rows)
	}

	got, err := GetAllDependencyRecordsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("GetAllDependencyRecordsInTx: %v", err)
	}
	if dep := onlyDependency(t, got, "perm-source"); dep.DependsOnID != "perm-target" {
		t.Fatalf("perm dependency target = %q, want perm-target", dep.DependsOnID)
	}
	if _, ok := got["wisp-source"]; ok {
		t.Fatal("unexpected wisp dependency records from missing table")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func onlyDependency(t *testing.T, deps map[string][]*types.Dependency, issueID string) *types.Dependency {
	t.Helper()

	got := deps[issueID]
	if len(got) != 1 {
		t.Fatalf("deps[%q] length = %d, want 1: %+v", issueID, len(got), got)
	}
	return got[0]
}

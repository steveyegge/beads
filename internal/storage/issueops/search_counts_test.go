package issueops

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/types"
)

func TestSearchIssuesWithCountsAppliesLimitToEachSourceQuery(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(`SELECT 1 FROM wisp_dependencies LIMIT 1`).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM issues i.*ORDER BY i\.priority ASC, i\.created_at DESC, i\.id ASC\s+LIMIT 3`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT 1 FROM wisps LIMIT 1`).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM wisps i.*ORDER BY i\.priority ASC, i\.created_at DESC, i\.id ASC\s+LIMIT 3`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := SearchIssuesWithCountsInTx(context.Background(), tx, "", types.IssueFilter{Limit: 3})
	if err != nil {
		t.Fatalf("SearchIssuesWithCountsInTx: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchIssuesWithCountsInTx returned %d rows, want none", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

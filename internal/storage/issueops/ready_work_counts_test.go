package issueops

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/types"
)

func TestGetReadyWorkWithCountsAppliesLimitToEachSourceQuery(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	mock.ExpectQuery(`SELECT 1 FROM wisp_dependencies LIMIT 1`).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM issues i.*WHERE status IN \('open', 'in_progress'\).*ORDER BY priority ASC, created_at DESC, id ASC\s+LIMIT 3`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT 1 FROM wisps LIMIT 1`).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM wisps i.*WHERE status IN \('open', 'in_progress'\).*ORDER BY priority ASC, created_at DESC, id ASC\s+LIMIT 3`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := GetReadyWorkWithCountsInTx(context.Background(), tx, types.WorkFilter{
		Limit:           3,
		IncludeDeferred: true,
		SortPolicy:      types.SortPolicyPriority,
	})
	if err != nil {
		t.Fatalf("GetReadyWorkWithCountsInTx: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetReadyWorkWithCountsInTx returned %d rows, want none", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

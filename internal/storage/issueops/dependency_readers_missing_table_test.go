package issueops

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/storage/sqlbuild"
	"github.com/steveyegge/beads/internal/types"
)

// bd-cif6y4 (#3445): the anchor-keyed edge readers merge `dependencies` and
// `wisp_dependencies`, a dolt_ignore'd plane a server-mode working set can
// drop. These tests pin the two halves of the merge contract on every reader:
// a missing wisp plane is an empty plane, never a failed read, and an edge
// stored in BOTH tables (a pull that lands another clone's promotion next to
// the still-local wisp copy) is returned exactly once, as its durable row.

// edgeRow is one (far-end id, type) pair a primed dependency table returns.
type edgeRow struct {
	farID, depType string
}

func edgeRows(rows []edgeRow) *sqlmock.Rows {
	out := sqlmock.NewRows([]string{"id", "type"})
	for _, r := range rows {
		out.AddRow(r.farID, r.depType)
	}
	return out
}

// primeEdgeTable installs the expectation for one dependency table's leg of
// an anchorEdgesInTx read: the per-direction SQL the four readers use, bound
// to the anchor id.
func primeEdgeTable(mock sqlmock.Sqlmock, dir domain.DepDirection, table string, anchor string, rows []edgeRow) {
	var q string
	if dir == domain.DepDirectionIn {
		q = "SELECT issue_id, type FROM " + table + " WHERE " + DepTargetExpr + " = ?"
	} else {
		q = "SELECT " + DepTargetExpr + " AS depends_on_id, type FROM " + table + " WHERE issue_id = ?"
	}
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(anchor).
		WillReturnRows(edgeRows(rows))
}

// primeEdgeTableErr installs a failing expectation for one dependency
// table's leg (e.g. tableNotFound, or an unrelated failure).
func primeEdgeTableErr(mock sqlmock.Sqlmock, dir domain.DepDirection, table, anchor string, err error) {
	var q string
	if dir == domain.DepDirectionIn {
		q = "SELECT issue_id, type FROM " + table + " WHERE " + DepTargetExpr + " = ?"
	} else {
		q = "SELECT " + DepTargetExpr + " AS depends_on_id, type FROM " + table + " WHERE issue_id = ?"
	}
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(anchor).
		WillReturnError(err)
}

// primeHydration installs the expectations GetIssuesByIDsInTx runs for ids:
// the wisps probe (answered empty, so every id hydrates from `issues`), the
// batched issue rows, and the label rows.
func primeHydration(mock sqlmock.Sqlmock, ids []string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM wisps LIMIT 1")).
		WillReturnError(sql.ErrNoRows)

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]driver.Value, len(ids))
	rows := issueRows()
	for i, id := range ids {
		args[i] = id
		rows.AddRow(issueRowValues(id, id)...)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + IssueSelectColumns + " FROM issues " + sqlbuild.LeaseJoin("issues") + " WHERE id IN (" + placeholders + ")")).
		WithArgs(args...).
		WillReturnRows(rows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT issue_id, label FROM labels WHERE issue_id IN (" + placeholders + ") ORDER BY issue_id, label")).
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "label"}))
}

// dependencyReaderEntryPoint is one anchor-keyed read whose both-table merge
// the #3445 fix pins. render answers "id:type" per row so a duplicate or a
// wrong-type survivor is visible, not just the row count.
type dependencyReaderEntryPoint struct {
	name string
	// anchor is the fixed anchor issue every primed leg binds to.
	anchor string
	dir    domain.DepDirection
	// run drives the reader inside the primed transaction.
	run func(ctx context.Context, tx *sql.Tx, anchor string) (string, error)
}

var dependencyReaderEntryPoints = []dependencyReaderEntryPoint{
	{
		name:   "GetDependentsWithMetadataInTx",
		anchor: "epic-1",
		dir:    domain.DepDirectionIn,
		run: func(ctx context.Context, tx *sql.Tx, anchor string) (string, error) {
			out, err := GetDependentsWithMetadataInTx(ctx, tx, anchor)
			return renderDepMetaRows(out), err
		},
	},
}

func renderDepMetaRows(out []*types.IssueWithDependencyMetadata) string {
	parts := make([]string, 0, len(out))
	for _, d := range out {
		parts = append(parts, d.ID+":"+string(d.DependencyType))
	}
	return strings.Join(parts, ",")
}

// mirroredEdges returns N inbound edges for anchor as the durable table's
// rows plus the wisp table's copies of the SAME edges, with first wisp copy
// carrying a different type: the cross-table duplicate #3445 reports, with the
// type divergence that pins which copy wins.
func mirroredEdges(n int) (durable, wisp []edgeRow) {
	for i := 0; i < n; i++ {
		farID := "child-" + string(rune('a'+i))
		depType := string(types.DepParentChild)
		durable = append(durable, edgeRow{farID: farID, depType: depType})
		wispType := depType
		if i == 0 {
			wispType = string(types.DepRelated)
		}
		wisp = append(wisp, edgeRow{farID: farID, depType: wispType})
	}
	return durable, wisp
}

func durableEdgeIDs(rows []edgeRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.farID
	}
	return ids
}

// TestDependencyReadersCrossTableDuplicateReturnedOnce pins the de-duplicated
// half of the merge: an edge stored in both tables is ONE edge, carrying the
// durable row's type, and the hydration below it receives each far end once.
// Before the fix every reader returned the edge twice (2N rows, and `bd show`
// rendered 24 children for 12).
func TestDependencyReadersCrossTableDuplicateReturnedOnce(t *testing.T) {
	for _, tc := range dependencyReaderEntryPoints {
		t.Run(tc.name, func(t *testing.T) {
			_, mock, tx := beginMockTx(t)
			durable, wisp := mirroredEdges(2)
			primeEdgeTable(mock, tc.dir, "dependencies", tc.anchor, durable)
			primeEdgeTable(mock, tc.dir, "wisp_dependencies", tc.anchor, wisp)
			primeHydration(mock, durableEdgeIDs(durable))

			got, err := tc.run(context.Background(), tx, tc.anchor)
			if err != nil {
				t.Fatalf("%s over a mirrored working set: %v", tc.name, err)
			}
			want := "child-a:" + string(types.DepParentChild) + ",child-b:" + string(types.DepParentChild)
			if got != want {
				t.Fatalf("%s = %q, want %q (each far end once, durable type wins)", tc.name, got, want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: unmet sql expectations (a duplicated far end would hydrate as a 4-id IN batch): %v", tc.name, err)
			}
		})
	}
}

// TestDependencyReadersMissingWispPlaneIsNotAnError pins the tolerance half
// of the merge: a database with no wisp_dependencies table answers from the
// durable plane alone instead of failing the read (the state where `bd show`
// rendered 0 children for 12).
func TestDependencyReadersMissingWispPlaneIsNotAnError(t *testing.T) {
	for _, tc := range dependencyReaderEntryPoints {
		t.Run(tc.name, func(t *testing.T) {
			_, mock, tx := beginMockTx(t)
			durable := []edgeRow{
				{farID: "child-a", depType: string(types.DepParentChild)},
				{farID: "child-b", depType: string(types.DepParentChild)},
			}
			primeEdgeTable(mock, tc.dir, "dependencies", tc.anchor, durable)
			primeEdgeTableErr(mock, tc.dir, "wisp_dependencies", tc.anchor, tableNotFound("wisp_dependencies"))
			primeHydration(mock, durableEdgeIDs(durable))

			got, err := tc.run(context.Background(), tx, tc.anchor)
			if err != nil {
				t.Fatalf("%s over a database with no wisp_dependencies: %v (want the plane treated as empty)", tc.name, err)
			}
			want := "child-a:" + string(types.DepParentChild) + ",child-b:" + string(types.DepParentChild)
			if got != want {
				t.Fatalf("%s = %q, want %q", tc.name, got, want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: unmet sql expectations: %v", tc.name, err)
			}
		})
	}
}
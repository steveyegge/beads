package issueops

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/steveyegge/beads/internal/storage/dberrors"
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
// the #3445 fix pins. plain readers answer far-end ids only; the metadata
// readers answer "id:type" per row so a duplicate or a wrong-type survivor is
// visible, not just the row count.
type dependencyReaderEntryPoint struct {
	name string
	// anchor is the fixed anchor issue every primed leg binds to.
	anchor string
	dir    domain.DepDirection
	plain   bool
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
	{
		name:   "GetDependenciesWithMetadataInTx",
		anchor: "issue-1",
		dir:    domain.DepDirectionOut,
		run: func(ctx context.Context, tx *sql.Tx, anchor string) (string, error) {
			out, err := GetDependenciesWithMetadataInTx(ctx, tx, anchor)
			return renderDepMetaRows(out), err
		},
	},
	{
		name:   "GetDependentsInTx",
		anchor: "epic-2",
		dir:    domain.DepDirectionIn,
		plain:  true,
		run: func(ctx context.Context, tx *sql.Tx, anchor string) (string, error) {
			out, err := GetDependentsInTx(ctx, tx, anchor)
			return renderIssueIDs(out), err
		},
	},
	{
		name:   "GetDependenciesInTx",
		anchor: "issue-2",
		dir:    domain.DepDirectionOut,
		plain:  true,
		run: func(ctx context.Context, tx *sql.Tx, anchor string) (string, error) {
			out, err := GetDependenciesInTx(ctx, tx, anchor)
			return renderIssueIDs(out), err
		},
	},
}

// wantReaderRows renders the expected answer for durable rows: far-end ids in
// first-seen order, each once, with the metadata readers also carrying the
// durable row's type.
func wantReaderRows(durable []edgeRow, plain bool) string {
	parts := make([]string, 0, len(durable))
	for _, r := range durable {
		if plain {
			parts = append(parts, r.farID)
		} else {
			parts = append(parts, r.farID+":"+r.depType)
		}
	}
	return strings.Join(parts, ",")
}

func renderDepMetaRows(out []*types.IssueWithDependencyMetadata) string {
	parts := make([]string, 0, len(out))
	for _, d := range out {
		parts = append(parts, d.ID+":"+string(d.DependencyType))
	}
	return strings.Join(parts, ",")
}

func renderIssueIDs(out []*types.Issue) string {
	parts := make([]string, 0, len(out))
	for _, iss := range out {
		parts = append(parts, iss.ID)
	}
	return strings.Join(parts, ",")
}

// dependencyCountEntryPoint is one direction of the distinct-edge count whose
// agreement with the listings AC-4 pins: CountDependencyEdgesInTx, the body
// behind `bd show`'s dependent_count/dependency_count on the proxied leg and
// (delegated) the server and embedded legs.
type dependencyCountEntryPoint struct {
	name   string
	anchor string
	dir    domain.DepDirection
}

var dependencyCountEntryPoints = []dependencyCountEntryPoint{
	{name: "CountDependencyEdgesInTx/in", anchor: "epic-3", dir: domain.DepDirectionIn},
	{name: "CountDependencyEdgesInTx/out", anchor: "issue-3", dir: domain.DepDirectionOut},
}

// testCountAnchorExpr mirrors countAnchorExpr: the column a direction's
// counted edges hang off the anchor by.
func testCountAnchorExpr(dir domain.DepDirection) string {
	if dir == domain.DepDirectionOut {
		return "issue_id"
	}
	return DepTargetExpr
}

func primeDurableDepCount(mock sqlmock.Sqlmock, dir domain.DepDirection, anchor string, n int) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM dependencies WHERE "+testCountAnchorExpr(dir)+" = ?")).
		WithArgs(anchor).
		WillReturnRows(scalarCountRows(n))
}

// primeWispExtraDepCount installs the expectation for the wisp-only edge
// count: the NOT-IN-complement query whose shape (far-end key, untyped
// subquery, outbound NULL guard) is exactly what AC-4 pins.
func primeWispExtraDepCount(mock sqlmock.Sqlmock, dir domain.DepDirection, anchor string, result any) {
	farExpr := "issue_id"
	nullGuard := ""
	if dir == domain.DepDirectionOut {
		farExpr = DepTargetExpr
		nullGuard = " AND " + DepTargetExpr + " IS NOT NULL"
	}
	q := "SELECT COUNT(*) FROM wisp_dependencies WHERE " + testCountAnchorExpr(dir) + " = ? AND " + farExpr +
		" NOT IN (SELECT " + farExpr + " FROM dependencies WHERE " + testCountAnchorExpr(dir) + " = ?" + nullGuard + ")"
	expectation := mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(anchor, anchor)
	if err, ok := result.(error); ok {
		expectation.WillReturnError(err)
	} else {
		expectation.WillReturnRows(scalarCountRows(result.(int)))
	}
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
			want := wantReaderRows(durable, tc.plain)
			if got != want {
				t.Fatalf("%s = %q, want %q (each far end once, durable type wins)", tc.name, got, want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: unmet sql expectations (a duplicated far end would hydrate as a 4-id IN batch): %v", tc.name, err)
			}
		})
	}
	for _, tc := range dependencyCountEntryPoints {
		t.Run(tc.name, func(t *testing.T) {
			_, mock, tx := beginMockTx(t)
			// Both tables hold the same N edges, so the distinct-edge count
			// is N: the durable rows plus a wisp-only complement of zero.
			primeDurableDepCount(mock, tc.dir, tc.anchor, 2)
			primeWispExtraDepCount(mock, tc.dir, tc.anchor, 0)

			got, err := CountDependencyEdgesInTx(context.Background(), tx, tc.anchor, tc.dir, nil)
			if err != nil {
				t.Fatalf("%s over a mirrored working set: %v", tc.name, err)
			}
			if got != 2 {
				t.Fatalf("%s = %d, want 2 (the both-table duplicates count once)", tc.name, got)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: unmet sql expectations: %v", tc.name, err)
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
			want := wantReaderRows(durable, tc.plain)
			if got != want {
				t.Fatalf("%s = %q, want %q", tc.name, got, want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: unmet sql expectations: %v", tc.name, err)
			}
		})
	}
	for _, tc := range dependencyCountEntryPoints {
		t.Run(tc.name, func(t *testing.T) {
			_, mock, tx := beginMockTx(t)
			primeDurableDepCount(mock, tc.dir, tc.anchor, 2)
			primeWispExtraDepCount(mock, tc.dir, tc.anchor, tableNotFound("wisp_dependencies"))

			got, err := CountDependencyEdgesInTx(context.Background(), tx, tc.anchor, tc.dir, nil)
			if err != nil {
				t.Fatalf("%s over a database with no wisp_dependencies: %v (want the plane treated as empty)", tc.name, err)
			}
			if got != 2 {
				t.Fatalf("%s = %d, want the durable count 2", tc.name, got)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: unmet sql expectations: %v", tc.name, err)
			}
		})
	}
}

// TestDependencyCountTypeFilteredDuplicateAgreesWithListing pins why the
// wisp-only count's de-duplication subquery carries no type filter: an edge
// stored in both tables with diverging types is the durable row as far as the
// de-duplicated listing is concerned, so a type filter that matches only the
// wisp copy's type must answer zero on BOTH sides — the listing drops the
// durable row after the de-duplication, and the count's untyped subquery
// keeps the wisp copy out of the wisp-only complement. A typed subquery would
// count 1 where the listing shows 0.
func TestDependencyCountTypeFilteredDuplicateAgreesWithListing(t *testing.T) {
	const anchor = "epic-4"
	_, mock, tx := beginMockTx(t)

	// The listing: the durable parent-child edge and its related-typed wisp
	// copy. The de-duplication keeps the durable row.
	primeEdgeTable(mock, domain.DepDirectionIn, "dependencies", anchor,
		[]edgeRow{{farID: "child-a", depType: string(types.DepParentChild)}})
	primeEdgeTable(mock, domain.DepDirectionIn, "wisp_dependencies", anchor,
		[]edgeRow{{farID: "child-a", depType: string(types.DepRelated)}})
	primeHydration(mock, []string{"child-a"})

	listed, err := GetDependentsWithMetadataInTx(context.Background(), tx, anchor)
	if err != nil {
		t.Fatalf("GetDependentsWithMetadataInTx: %v", err)
	}
	if len(listed) != 1 || listed[0].DependencyType != types.DepParentChild {
		t.Fatalf("listing = %+v, want the one durable row carrying parent-child", listed)
	}
	filtered := 0
	for _, d := range listed {
		if d.DependencyType == types.DepRelated {
			filtered++
		}
	}

	// The type-filtered count of the same state, as the proxied leg computes
	// it (CountByID passes the filter to CountDependencyEdgesInTx).
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM dependencies WHERE "+DepTargetExpr+" = ? AND type IN (?)")).
		WithArgs(anchor, string(types.DepRelated)).
		WillReturnRows(scalarCountRows(0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM wisp_dependencies WHERE "+DepTargetExpr+" = ? AND type IN (?) AND issue_id NOT IN (SELECT issue_id FROM dependencies WHERE "+DepTargetExpr+" = ?)")).
		WithArgs(anchor, string(types.DepRelated), anchor).
		WillReturnRows(scalarCountRows(0))

	counted, err := CountDependencyEdgesInTx(context.Background(), tx, anchor, domain.DepDirectionIn,
		[]types.DependencyType{types.DepRelated})
	if err != nil {
		t.Fatalf("CountDependencyEdgesInTx with a type filter: %v", err)
	}
	if filtered != 0 || counted != 0 {
		t.Fatalf("type-filtered listing = %d rows, count = %d; want 0 on both (the durable row wins the de-duplication, so a filter matching only the wisp copy's type matches nothing)", filtered, counted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// primeDurableDepCountErr installs a failing expectation for the durable
// count leg.
func primeDurableDepCountErr(mock sqlmock.Sqlmock, dir domain.DepDirection, anchor string, err error) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM dependencies WHERE "+testCountAnchorExpr(dir)+" = ?")).
		WithArgs(anchor).
		WillReturnError(err)
}

// TestDependencyReadersBrokenPlaneIsAnError pins the other half of the named
// tolerance the merge contract introduced: the guard admits exactly a missing
// OPTIONAL wisp table (wisps, wisp_dependencies). A missing durable table, a
// missing core table under the hydration (leases in the issues join,
// wisp_labels in the wisp-side label join), or an unrelated database error is
// a broken database and must surface, never silently empty. Every one of
// these failures propagated before the fix too — the base failed all reads
// loudly — so this is a preservation contract: it holds the fix's tolerance
// to the two names that may legitimately be absent (#3445).
func TestDependencyReadersBrokenPlaneIsAnError(t *testing.T) {
	gone := errors.New("connection refused")

	// injections are the failures primed per entry point; each also says
	// whether the count runs it (the count reads no issues and no labels, so
	// the hydration-plane failures do not apply to it).
	injections := []struct {
		name   string
		counts bool
		// wantErr asserts the read surfaced the injected failure, never a
		// silent empty answer.
		wantErr func(t *testing.T, err error)
		// prime installs the expectations for one reader entry point's read.
		prime func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyReaderEntryPoint)
		// primeCount installs the expectations for one count entry point.
		primeCount func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyCountEntryPoint)
	}{
		{
			name:   "missing dependencies table",
			counts: true,
			wantErr: func(t *testing.T, err error) {
				if !dberrors.IsMissingTable(err, "dependencies") {
					t.Fatalf("error = %v, want it to name the missing dependencies table", err)
				}
			},
			prime: func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyReaderEntryPoint) {
				primeEdgeTableErr(mock, tc.dir, "dependencies", tc.anchor, tableNotFound("dependencies"))
			},
			primeCount: func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyCountEntryPoint) {
				primeDurableDepCountErr(mock, tc.dir, tc.anchor, tableNotFound("dependencies"))
			},
		},
		{
			name:   "unrelated database error",
			counts: true,
			wantErr: func(t *testing.T, err error) {
				if !errors.Is(err, gone) {
					t.Fatalf("error = %v, want the injected connection failure", err)
				}
			},
			prime: func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyReaderEntryPoint) {
				primeEdgeTableErr(mock, tc.dir, "dependencies", tc.anchor, gone)
			},
			primeCount: func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyCountEntryPoint) {
				primeDurableDepCountErr(mock, tc.dir, tc.anchor, gone)
			},
		},
		{
			name:  "missing leases table",
			counts: false,
			wantErr: func(t *testing.T, err error) {
				if !dberrors.IsMissingTable(err, "leases") {
					t.Fatalf("error = %v, want it to name the missing leases table", err)
				}
			},
			prime: func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyReaderEntryPoint) {
				durable := []edgeRow{{farID: "child-a", depType: string(types.DepParentChild)}}
				primeEdgeTable(mock, tc.dir, "dependencies", tc.anchor, durable)
				primeEdgeTable(mock, tc.dir, "wisp_dependencies", tc.anchor, nil)
				// The wisps probe answers empty, so the batch hydrates from
				// `issues` — whose lease overlay is missing.
				mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM wisps LIMIT 1")).
					WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT "+IssueSelectColumns+" FROM issues "+sqlbuild.LeaseJoin("issues")+" WHERE id IN (?)")).
					WithArgs("child-a").
					WillReturnError(tableNotFound("leases"))
			},
		},
		{
			name:  "missing wisp_labels table",
			counts: false,
			wantErr: func(t *testing.T, err error) {
				if !dberrors.IsMissingTable(err, "wisp_labels") {
					t.Fatalf("error = %v, want it to name the missing wisp_labels table", err)
				}
			},
			prime: func(t *testing.T, mock sqlmock.Sqlmock, tc dependencyReaderEntryPoint) {
				// One wisp-source dependent, so the hydration routes through
				// the wisps row and its label join.
				const wispChild = "wisp-child-a"
				primeEdgeTable(mock, tc.dir, "dependencies", tc.anchor,
					[]edgeRow{{farID: wispChild, depType: string(types.DepParentChild)}})
				primeEdgeTable(mock, tc.dir, "wisp_dependencies", tc.anchor, nil)
				// The wisps probe answers non-empty ...
				mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM wisps LIMIT 1")).
					WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
				// ... the batch's ids resolve to the wisp ...
				mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM wisps WHERE id IN (?)")).
					WithArgs(wispChild).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(wispChild))
				// ... whose row hydrates ...
				wispRows := issueRows()
				wispRows.AddRow(issueRowValues(wispChild, wispChild)...)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT "+IssueSelectColumns+" FROM wisps "+sqlbuild.LeaseJoin("wisps")+" WHERE id IN (?)")).
					WithArgs(wispChild).
					WillReturnRows(wispRows)
				// ... and whose labels live in a table this database lost.
				mock.ExpectQuery(regexp.QuoteMeta("SELECT issue_id, label FROM wisp_labels WHERE issue_id IN (?) ORDER BY issue_id, label")).
					WithArgs(wispChild).
					WillReturnError(tableNotFound("wisp_labels"))
			},
		},
	}

	for _, tc := range dependencyReaderEntryPoints {
		for _, inj := range injections {
			t.Run(tc.name+"/"+inj.name, func(t *testing.T) {
				_, mock, tx := beginMockTx(t)
				inj.prime(t, mock, tc)

				_, err := tc.run(context.Background(), tx, tc.anchor)
				if err == nil {
					t.Fatalf("%s over a broken plane answered, want the failure surfaced", tc.name)
				}
				inj.wantErr(t, err)
			})
		}
	}
	for _, tc := range dependencyCountEntryPoints {
		for _, inj := range injections {
			if !inj.counts {
				continue
			}
			t.Run(tc.name+"/"+inj.name, func(t *testing.T) {
				_, mock, tx := beginMockTx(t)
				inj.primeCount(t, mock, tc)

				_, err := CountDependencyEdgesInTx(context.Background(), tx, tc.anchor, tc.dir, nil)
				if err == nil {
					t.Fatalf("%s over a broken plane answered, want the failure surfaced", tc.name)
				}
				inj.wantErr(t, err)
			})
		}
	}
}
package issueops

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/storage/depid"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

// TestCheckDependencyCycleInTxSelfDependencyIsWrapPreserving proves the
// self-dependency guard now returns a typed sentinel (errors.Is-able) while its
// user-facing message text stays byte-identical to the pre-taxonomy string.
func TestCheckDependencyCycleInTxSelfDependencyIsWrapPreserving(t *testing.T) {
	t.Parallel()

	_, _, tx := beginMockTx(t)
	dep := &types.Dependency{IssueID: "dep-a", DependsOnID: "dep-a", Type: types.DepBlocks}

	err := CheckDependencyCycleInTx(context.Background(), tx, dep, nil)
	if err == nil {
		t.Fatal("CheckDependencyCycleInTx(self-dep) = nil, want error")
	}
	if !errors.Is(err, domain.ErrSelfDependency) {
		t.Errorf("errors.Is(err, domain.ErrSelfDependency) = false, want true; err = %v", err)
	}
	// Byte-identical to the pre-taxonomy message: the sentinel is the static
	// prefix rendered by %w, the rest is unchanged.
	const want = "cannot add self-dependency: dep-a cannot depend on itself"
	if err.Error() != want {
		t.Errorf("message = %q, want byte-identical %q", err.Error(), want)
	}
}

// TestCheckDependencyCycleInTxCycleIsWrapPreserving proves the cycle guard now
// returns a typed sentinel while its user-facing message text stays
// byte-identical to the pre-taxonomy string.
func TestCheckDependencyCycleInTxCycleIsWrapPreserving(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)
	dep := &types.Dependency{IssueID: "dep-a", DependsOnID: "dep-b", Type: types.DepBlocks}

	// WouldCreateSchedulingCycleInTx queries reachability with (dependsOnID, issueID).
	mock.ExpectQuery("WITH RECURSIVE reachable").
		WithArgs("dep-b", "dep-a").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := CheckDependencyCycleInTx(context.Background(), tx, dep, nil)
	if err == nil {
		t.Fatal("CheckDependencyCycleInTx(cycle) = nil, want error")
	}
	if !errors.Is(err, domain.ErrDependencyCycle) {
		t.Errorf("errors.Is(err, domain.ErrDependencyCycle) = false, want true; err = %v", err)
	}
	// Byte-identical to the pre-taxonomy message (the bare sentinel text).
	const want = "adding dependency would create a cycle"
	if err.Error() != want {
		t.Errorf("message = %q, want byte-identical %q", err.Error(), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestReplaceDependencyTargetNormalizesTargetColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		targetCol    string
		rowIssue     any
		rowWisp      any
		wantIssue    any
		wantWisp     any
		wantExternal any
	}{
		{
			name:         "issue target clears stale wisp target",
			targetCol:    "depends_on_issue_id",
			rowIssue:     nil,
			rowWisp:      "old-target",
			wantIssue:    "new-target",
			wantWisp:     nil,
			wantExternal: nil,
		},
		{
			name:         "wisp target clears stale issue target",
			targetCol:    "depends_on_wisp_id",
			rowIssue:     "old-target",
			rowWisp:      nil,
			wantIssue:    nil,
			wantWisp:     "new-target",
			wantExternal: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM dependencies a")).
				WithArgs("new-target", "new-target", "new-target").
				WillReturnRows(sqlmock.NewRows([]string{"found"}))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external, type, created_at, created_by, metadata, thread_id")).
				WithArgs("old-target", "old-target").
				WillReturnRows(sqlmock.NewRows([]string{
					"issue_id",
					"depends_on_issue_id",
					"depends_on_wisp_id",
					"depends_on_external",
					"type",
					"created_at",
					"created_by",
					"metadata",
					"thread_id",
				}).AddRow("source", tt.rowIssue, tt.rowWisp, nil, "blocks", nil, "tester", "{}", "thread-1"))
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM dependencies")).
				WithArgs("old-target", "old-target").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(regexp.QuoteMeta("INSERT INTO dependencies (id, issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external, type, created_at, created_by, metadata, thread_id)")).
				WithArgs(depid.New("source", "new-target"), "source", tt.wantIssue, tt.wantWisp, tt.wantExternal, "blocks", nil, "tester", "{}", "thread-1").
				WillReturnResult(sqlmock.NewResult(0, 1))
			// The post-cascade sweep: rows whose typed column already carries
			// newID because fk_dep_*_target's ON UPDATE CASCADE beat the rewrite
			// here get their stale id re-derived. Nothing stale in this fixture.
			mock.ExpectQuery(regexp.QuoteMeta("SELECT id, issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external")).
				WithArgs("new-target").
				WillReturnRows(sqlmock.NewRows([]string{"id", "issue_id", "depends_on_issue_id", "depends_on_wisp_id", "depends_on_external"}))
			mock.ExpectCommit()

			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}
			if err := replaceDependencyTargetInTx(context.Background(), tx, "dependencies", tt.targetCol, "old-target", "new-target"); err != nil {
				_ = tx.Rollback()
				t.Fatalf("replaceDependencyTargetInTx: %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestCycleDetectionTablesUseBothTablesByDefault(t *testing.T) {
	got := cycleDetectionTables()
	want := []string{"dependencies", "wisp_dependencies"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCycleReachabilityQuerySingleTableJoinsDirectly(t *testing.T) {
	query := cycleReachabilityQuery([]string{"wisp_dependencies"})
	if !strings.Contains(query, "JOIN wisp_dependencies d ON d.issue_id = r.node") {
		t.Fatalf("query does not join wisp_dependencies directly:\n%s", query)
	}
	if strings.Contains(query, "JOIN (SELECT") {
		t.Fatalf("single-table cycle query should not materialize a derived dependency table:\n%s", query)
	}
	if !strings.Contains(query, "d.type IN ('blocks', 'conditional-blocks', 'parent-child')") {
		t.Fatalf("query does not filter scheduling-relevant dependency types:\n%s", query)
	}
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
}

func TestCycleReachabilityQueryMultipleTablesTraversesUniqueNodes(t *testing.T) {
	query := cycleReachabilityQuery([]string{"dependencies", "wisp_dependencies"})
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("multi-table cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
	// One recursive member per table, each joined directly on issue_id: a
	// join against a UNION of the tables cannot use an index, and Dolt then
	// rescans every edge per recursion step.
	for _, table := range []string{"dependencies", "wisp_dependencies"} {
		if !strings.Contains(query, "JOIN "+table+" d ON d.issue_id = r.node") {
			t.Fatalf("query does not join %s directly on issue_id:\n%s", table, query)
		}
	}
	if strings.Contains(query, "JOIN (SELECT") {
		t.Fatalf("multi-table cycle query should not join a derived union of the dependency tables:\n%s", query)
	}
	if strings.Count(query, "JOIN_ORDER(r, d) LOOKUP_JOIN(r, d)") != 2 {
		t.Fatalf("every recursive member should pin the lookup join:\n%s", query)
	}
	if !strings.Contains(query, depTargetExpr("d")) {
		t.Fatalf("query does not resolve depends_on_id via the typed target columns:\n%s", query)
	}
}

func TestAncestorQueryJoinsEachTableOnIssueID(t *testing.T) {
	query := reachabilityQuery("ancestors", []string{"dependencies", "wisp_dependencies"}, "d.type = 'parent-child'")
	for _, table := range []string{"dependencies", "wisp_dependencies"} {
		// issue_id is the join's only predicate: a type equality there could
		// be planned as a lookup into a type-leading index instead.
		if !strings.Contains(query, "JOIN "+table+" d ON d.issue_id = r.node\n") {
			t.Fatalf("ancestor query does not join %s on issue_id alone:\n%s", table, query)
		}
	}
	if strings.Count(query, "CASE WHEN d.type = 'parent-child' THEN") != 2 {
		t.Fatalf("ancestor query does not filter parent-child rows in each member's projection:\n%s", query)
	}
	if strings.Count(query, "JOIN_ORDER(r, d) LOOKUP_JOIN(r, d)") != 2 {
		t.Fatalf("every recursive member should pin the lookup join:\n%s", query)
	}
}

// TestReplaceDependencyTargetRekeysCascadedRows pins the half of a rename the
// rewrite above cannot see. fk_dep_issue_target carries ON UPDATE CASCADE and
// updateIssueIDInTx renames the issues row first, so depends_on_issue_id already
// reads newID by the time replaceDependencyTargetInTx runs: its
// `WHERE ... = oldID` matches nothing and the row keeps id = depid(issue, oldID).
// That stale primary key re-forks across clones (#4259) and, once a later rename
// hands oldID to another issue, becomes the migration-time re-key chain of
// gastownhall/beads#5268.
func TestReplaceDependencyTargetRekeysCascadedRows(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM dependencies a")).
		WithArgs("new-target", "new-target", "new-target").
		WillReturnRows(sqlmock.NewRows([]string{"found"}))
	// The cascade already moved the row, so the oldID rewrite finds nothing.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external, type, created_at, created_by, metadata, thread_id")).
		WithArgs("old-target", "old-target").
		WillReturnRows(sqlmock.NewRows([]string{
			"issue_id", "depends_on_issue_id", "depends_on_wisp_id", "depends_on_external",
			"type", "created_at", "created_by", "metadata", "thread_id",
		}))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM dependencies")).
		WithArgs("old-target", "old-target").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// The sweep finds the cascaded row: right target, id still derived from the
	// pre-rename name.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external")).
		WithArgs("new-target").
		WillReturnRows(sqlmock.NewRows([]string{"id", "issue_id", "depends_on_issue_id", "depends_on_wisp_id", "depends_on_external"}).
			AddRow(depid.New("source", "old-target"), "source", "new-target", nil, nil).
			// An already-correct sibling must not be touched.
			AddRow(depid.New("other", "new-target"), "other", "new-target", nil, nil))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE dependencies SET id = ? WHERE id = ?")).
		WithArgs(depid.New("source", "new-target"), depid.New("source", "old-target")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := replaceDependencyTargetInTx(context.Background(), tx, "dependencies", "depends_on_issue_id", "old-target", "new-target"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replaceDependencyTargetInTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// TestDependencyMetadataEqualComparesValuesNotBytes pins the change-free
// re-add gate's comparison rule. The gate decides between "nothing happened"
// and "a real mutation of the source issue" by comparing metadata the CALLER
// supplied against metadata read back out of a native JSON column, and a
// native JSON column is free to re-canonicalize what it persists — Dolt's
// inserts a space after ':', sorts object keys, and narrows a bare integral
// float to an int. A byte compare therefore reports a change on values that
// never changed. Each same-value row here is a spelling difference a byte
// compare fails and a JCS (RFC 8785) compare survives (#6650).
func TestDependencyMetadataEqualComparesValuesNotBytes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "IdenticalBytes", a: `{"gate":"any-children"}`, b: `{"gate":"any-children"}`, want: true},
		{name: "EmptyObjects", a: `{}`, b: `{}`, want: true},
		{name: "StorageInsertedSpacing", a: `{"gate":"any-children"}`, b: `{"gate": "any-children"}`, want: true},
		{name: "StorageSortedKeys", a: `{"spawner_id":"src-1","gate":"any-children"}`, b: `{"gate":"any-children","spawner_id":"src-1"}`, want: true},
		{name: "StorageNarrowedIntegralFloat", a: `{"weight":1.0}`, b: `{"weight":1}`, want: true},
		{name: "DifferentValue", a: `{"note":"v1"}`, b: `{"note":"v2"}`, want: false},
		{name: "DifferentKey", a: `{"gate":"any-children"}`, b: `{"mode":"any-children"}`, want: false},
		{name: "EmptyVersusPopulated", a: `{}`, b: `{"gate":"any-children"}`, want: false},
		// Neither side parses, so the rule falls back to the byte compare it
		// replaced rather than calling two unparseable strings equal.
		{name: "UnparseableFallsBackToBytesEqual", a: `not json`, b: `not json`, want: true},
		{name: "UnparseableFallsBackToBytesUnequal", a: `not json`, b: `also not json`, want: false},
		{name: "OneSideUnparseableFallsBackToBytes", a: `{"gate":"any-children"}`, b: `{"gate":`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DependencyMetadataEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("DependencyMetadataEqual(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			// The rule is symmetric: which side came from the column and
			// which from the caller must not change the answer.
			if got := DependencyMetadataEqual(tc.b, tc.a); got != tc.want {
				t.Errorf("DependencyMetadataEqual(%q, %q) = %v, want %v (asymmetric)", tc.b, tc.a, got, tc.want)
			}
		})
	}
}

// TestReAddedDependencyThreadKeepsTheStoredThreadUnlessANewOneIsNamed pins the
// thread half of the same-type re-add gate. A re-add naming a thread the row
// does not carry is a change that must reach the row; a re-add naming none
// says nothing about threads, so it must leave the stored one — NULL, empty
// or named — exactly as it is, or every plain `bd dep add` of a threaded edge
// would erase its thread.
func TestReAddedDependencyThreadKeepsTheStoredThreadUnlessANewOneIsNamed(t *testing.T) {
	t.Parallel()

	null := sql.NullString{}
	empty := sql.NullString{Valid: true}
	named := sql.NullString{String: "thread-1", Valid: true}
	for _, tc := range []struct {
		name        string
		stored      sql.NullString
		requested   string
		want        sql.NullString
		wantChanged bool
	}{
		{name: "NoneNamedKeepsNull", stored: null, requested: "", want: null},
		{name: "NoneNamedKeepsEmpty", stored: empty, requested: "", want: empty},
		{name: "NoneNamedKeepsTheStoredThread", stored: named, requested: "", want: named},
		{name: "SameThreadIsNoChange", stored: named, requested: "thread-1", want: named},
		{name: "NewThreadOverNull", stored: null, requested: "thread-1", want: named, wantChanged: true},
		{name: "NewThreadOverEmpty", stored: empty, requested: "thread-1", want: named, wantChanged: true},
		{name: "DifferentThreadReplacesIt", stored: named, requested: "thread-2",
			want: sql.NullString{String: "thread-2", Valid: true}, wantChanged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, changed := ReAddedDependencyThread(tc.stored, tc.requested)
			if got != tc.want || changed != tc.wantChanged {
				t.Errorf("ReAddedDependencyThread(%+v, %q) = (%+v, %v), want (%+v, %v)",
					tc.stored, tc.requested, got, changed, tc.want, tc.wantChanged)
			}
		})
	}
}

// TestAddDependencyInTxReadsNullStoredMetadataAsEmptyObject pins the widened
// SELECT's handling of a SQL-NULL metadata column. dependencies.metadata is
// nullable (`JSON DEFAULT (JSON_OBJECT())`, no NOT NULL), so a row written out
// of band — bd sql, an external tool, hand SQL — can hold NULL. Before the
// re-add gate read metadata at all this path scanned only `type` and was
// idempotently happy; scanning NULL into a plain string would hard-fail it
// with "converting NULL to string is unsupported", turning every subsequent
// re-add of that edge into an error. NULL is the absent-metadata state, so it
// reads as `{}` and the change-free re-add of a metadata-free edge stays the
// no-op it was.
//
// thread_id is nullable the same way (a VARCHAR with no NOT NULL), and the gate
// reads it too, so the row here holds NULL in both columns: a re-add naming no
// thread keeps whatever thread the row stores, NULL included, so this re-add
// is still change-free.
func TestAddDependencyInTxReadsNullStoredMetadataAsEmptyObject(t *testing.T) {
	t.Parallel()

	_, mock, tx := beginMockTx(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT issue_type FROM issues WHERE id = ?")).
		WithArgs("dep-a").
		WillReturnRows(sqlmock.NewRows([]string{"issue_type"}).AddRow("task"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT type, metadata, thread_id FROM dependencies")).
		WithArgs("dep-a", "dep-b").
		WillReturnRows(sqlmock.NewRows([]string{"type", "metadata", "thread_id"}).AddRow(string(types.DepRelated), nil, nil))

	kind := DepTargetIssue
	dep := &types.Dependency{IssueID: "dep-a", DependsOnID: "dep-b", Type: types.DepRelated}
	created, err := AddDependencyInTx(context.Background(), tx, dep, "writer", AddDependencyOpts{
		SourceTable:      "issues",
		TargetTable:      "issues",
		WriteTable:       "dependencies",
		SkipCycleCheck:   true,
		TargetKind:       &kind,
		PrecheckedTarget: &DepTargetPrecheck{IssueType: "task"},
	})
	if err != nil {
		t.Fatalf("AddDependencyInTx(existing edge with NULL metadata) = %v, want nil: a change-free re-add is a no-op, not an error", err)
	}
	if created {
		t.Error("AddDependencyInTx reported the edge as created; the edge already existed with the requested type")
	}
	// No UPDATE and no journal write are expected: scripting only the two
	// reads means any write this path attempted would fail the run.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// edgeEndQueryRegex matches one arm of listDepEdgeEndsInTx: the far-end select
// over table in the given direction.
func edgeEndQueryRegex(table string, inbound bool) string {
	if inbound {
		return regexp.QuoteMeta("SELECT issue_id, type FROM " + table + " WHERE " + DepTargetExpr + " = ?")
	}
	return regexp.QuoteMeta("SELECT " + DepTargetExpr + " AS depends_on_id, type FROM " + table + " WHERE issue_id = ?")
}

// errEdgeRead stands in for a read failure other than a missing table, which
// the edge reads and counts must return rather than read as empty.
var errEdgeRead = errors.New("connection reset")

// edgeEndArm scripts one table's answer: far-end (id, type) rows, or an error.
type edgeEndArm struct {
	rows []depEdgeEnd
	err  error
}

func (a edgeEndArm) expect(mock sqlmock.Sqlmock, table string, inbound bool) {
	q := mock.ExpectQuery(edgeEndQueryRegex(table, inbound)).WithArgs("near")
	if a.err != nil {
		q.WillReturnError(a.err)
		return
	}
	rows := sqlmock.NewRows([]string{"id", "type"})
	for _, r := range a.rows {
		rows.AddRow(r.id, r.depType)
	}
	q.WillReturnRows(rows)
}

// TestListDepEdgeEndsInTx pins the shared both-table edge read behind the four
// dependency readers and their counts, in both directions: the SQL of each
// arm, a missing wisp_dependencies reading as empty (GH#3445), a far end
// stored in both tables read once with its durable type, and the order durable
// ends then wisp ends.
func TestListDepEdgeEndsInTx(t *testing.T) {
	t.Parallel()

	durable := []depEdgeEnd{{"d1", "parent-child"}, {"d2", "blocks"}}
	cases := []struct {
		name string
		// wisp is nil when the dependencies arm fails and wisp_dependencies is
		// never read.
		durable edgeEndArm
		wisp    *edgeEndArm
		want    []depEdgeEnd
		wantErr error
	}{
		{
			name:    "missing wisp table reads as empty",
			durable: edgeEndArm{rows: durable},
			wisp:    &edgeEndArm{err: missingTableError("wisp_dependencies")},
			want:    durable,
		},
		{
			name:    "missing wisp table in MySQL wording reads as empty",
			durable: edgeEndArm{rows: durable},
			wisp:    &edgeEndArm{err: missingTableErrorMySQLText("beads", "wisp_dependencies")},
			want:    durable,
		},
		{
			name:    "mirrored far end reads once with the durable type",
			durable: edgeEndArm{rows: durable},
			wisp:    &edgeEndArm{rows: []depEdgeEnd{{"d1", "blocks"}, {"d2", "blocks"}}},
			want:    durable,
		},
		{
			name:    "wisp-only far end reads after the durable ends",
			durable: edgeEndArm{rows: durable},
			wisp:    &edgeEndArm{rows: []depEdgeEnd{{"w1", "blocks"}, {"d1", "blocks"}, {"w2", "parent-child"}}},
			want:    append(append([]depEdgeEnd(nil), durable...), depEdgeEnd{"w1", "blocks"}, depEdgeEnd{"w2", "parent-child"}),
		},
		{
			name:    "empty wisp table",
			durable: edgeEndArm{rows: durable},
			wisp:    &edgeEndArm{},
			want:    durable,
		},
		{
			name:    "dependencies error propagates",
			durable: edgeEndArm{err: errEdgeRead},
			wantErr: errEdgeRead,
		},
		{
			name:    "wisp_dependencies error other than a missing table propagates",
			durable: edgeEndArm{rows: durable},
			wisp:    &edgeEndArm{err: errEdgeRead},
			wantErr: errEdgeRead,
		},
	}

	for _, inbound := range []bool{true, false} {
		for _, tc := range cases {
			name := tc.name + "/outbound"
			if inbound {
				name = tc.name + "/inbound"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, mock, tx := beginMockTx(t)
				tc.durable.expect(mock, "dependencies", inbound)
				if tc.wisp != nil {
					tc.wisp.expect(mock, "wisp_dependencies", inbound)
				}

				got, err := listDepEdgeEndsInTx(context.Background(), tx, "near", inbound)
				if tc.wantErr != nil {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("listDepEdgeEndsInTx error = %v, want it to wrap %v", err, tc.wantErr)
					}
					if got != nil {
						t.Fatalf("listDepEdgeEndsInTx = %v alongside an error, want nil", got)
					}
				} else {
					if err != nil {
						t.Fatalf("listDepEdgeEndsInTx: %v", err)
					}
					if !reflect.DeepEqual(got, tc.want) {
						t.Fatalf("listDepEdgeEndsInTx = %v, want %v", got, tc.want)
					}
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatalf("unmet SQL expectations: %v", err)
				}
			})
		}
	}
}

// countArm scripts one table read of CountDependencyEdgesInTx.
type countArm struct {
	table   string
	inbound bool
	arm     edgeEndArm
}

// TestCountDependencyEdgesInTxDistinct pins that the per-issue edge counts
// behind `bd show` count the same far-end set listDepEdgeEndsInTx lists, so
// count - len(listing) only measures far ends with no row (GH#3445).
func TestCountDependencyEdgesInTxDistinct(t *testing.T) {
	t.Parallel()

	children := []depEdgeEnd{{"c1", "parent-child"}, {"c2", "parent-child"}}
	cases := []struct {
		name    string
		dir     domain.DepDirection
		filter  []types.DependencyType
		arms    []countArm
		want    int64
		wantErr error
	}{
		{
			name: "mirrored dependents count once",
			dir:  domain.DepDirectionIn,
			arms: []countArm{
				{"dependencies", true, edgeEndArm{rows: children}},
				{"wisp_dependencies", true, edgeEndArm{rows: []depEdgeEnd{{"c1", "blocks"}, {"c2", "parent-child"}}}},
			},
			want: 2,
		},
		{
			name: "mirrored dependency counts once",
			dir:  domain.DepDirectionOut,
			arms: []countArm{
				{"dependencies", false, edgeEndArm{rows: []depEdgeEnd{{"epic", "parent-child"}}}},
				{"wisp_dependencies", false, edgeEndArm{rows: []depEdgeEnd{{"epic", "parent-child"}}}},
			},
			want: 1,
		},
		{
			name: "both directions sum their distinct counts",
			dir:  domain.DepDirectionBoth,
			arms: []countArm{
				{"dependencies", false, edgeEndArm{rows: []depEdgeEnd{{"epic", "parent-child"}}}},
				{"wisp_dependencies", false, edgeEndArm{rows: []depEdgeEnd{{"epic", "parent-child"}}}},
				{"dependencies", true, edgeEndArm{rows: children}},
				{"wisp_dependencies", true, edgeEndArm{rows: []depEdgeEnd{{"c1", "parent-child"}, {"w1", "blocks"}}}},
			},
			want: 4,
		},
		{
			name: "missing wisp table counts the durable edges",
			dir:  domain.DepDirectionBoth,
			arms: []countArm{
				{"dependencies", false, edgeEndArm{rows: []depEdgeEnd{{"epic", "parent-child"}}}},
				{"wisp_dependencies", false, edgeEndArm{err: missingTableError("wisp_dependencies")}},
				{"dependencies", true, edgeEndArm{rows: children}},
				{"wisp_dependencies", true, edgeEndArm{err: missingTableError("wisp_dependencies")}},
			},
			want: 3,
		},
		{
			name:   "type filter applies after de-dup",
			dir:    domain.DepDirectionIn,
			filter: []types.DependencyType{types.DepBlocks},
			arms: []countArm{
				{"dependencies", true, edgeEndArm{rows: children}},
				{"wisp_dependencies", true, edgeEndArm{rows: []depEdgeEnd{{"c1", "blocks"}, {"w1", "blocks"}}}},
			},
			// c1's conflicting wisp mirror is not an edge of its own; w1 is.
			want: 1,
		},
		{
			name:    "dependencies error propagates",
			dir:     domain.DepDirectionIn,
			arms:    []countArm{{"dependencies", true, edgeEndArm{err: errEdgeRead}}},
			wantErr: errEdgeRead,
		},
		{
			name: "wisp_dependencies error other than a missing table propagates",
			dir:  domain.DepDirectionOut,
			arms: []countArm{
				{"dependencies", false, edgeEndArm{rows: []depEdgeEnd{{"epic", "parent-child"}}}},
				{"wisp_dependencies", false, edgeEndArm{err: errEdgeRead}},
			},
			wantErr: errEdgeRead,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, mock, tx := beginMockTx(t)
			for _, a := range tc.arms {
				a.arm.expect(mock, a.table, a.inbound)
			}

			got, err := CountDependencyEdgesInTx(context.Background(), tx, "near", tc.dir, tc.filter)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("CountDependencyEdgesInTx error = %v, want it to wrap %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("CountDependencyEdgesInTx: %v", err)
			} else if got != tc.want {
				t.Fatalf("CountDependencyEdgesInTx = %d, want %d", got, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

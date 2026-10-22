//go:build cgo

// bd-cif6y4 (#3445): the embedded leg of the dependents-views agreement the
// dolt-server test TestDependentsAcrossWispPlaneStates pins. The embedded
// store's dependents path runs the same issueops readers and counts the
// server leg does, so the same wisp-plane states — empty, mirrored (another
// clone's promotion landed beside the still-local wisp copy), and the table
// dropped from the working set — must leave every view of the epic's children
// in agreement here too.
package embeddeddolt_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"

	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/workapi"
)

// withEmbeddedFixtureConn opens one short-lived raw SQL connection to the
// fixture's database and runs fn on it. One connection for the whole fn is
// what makes session-scoped statements (FOREIGN_KEY_CHECKS) carry across the
// statements that need them.
func withEmbeddedFixtureConn(t *testing.T, ctx context.Context, fixture *pristineEmbeddedDoltFixture, fn func(conn *sql.Conn)) {
	t.Helper()
	db, cleanup, err := embeddeddolt.OpenSQL(ctx, fixture.dataDir, fixture.database, "main")
	if err != nil {
		t.Fatalf("OpenSQL(%s): %v", fixture.dataDir, err)
	}
	defer cleanup()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire raw conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	fn(conn)
}

// reopenEmbeddedFixture reopens the fixture's store after a raw-SQL mutation.
// DDL from a second connection is what the migrate tests do with the store
// closed, so the drop and the copy below follow the same discipline.
func reopenEmbeddedFixture(t *testing.T, ctx context.Context, fixture *pristineEmbeddedDoltFixture) *embeddeddolt.EmbeddedDoltStore {
	t.Helper()
	store, err := embeddeddolt.Open(ctx, fixture.beadsDir, fixture.database, "main")
	if err != nil {
		t.Fatalf("reopen embedded store: %v", err)
	}
	return store
}

func TestEmbeddedDependentsAcrossWispPlaneStates(t *testing.T) {
	ctx := context.Background()
	fixture := newPristineEmbeddedDoltFixture(t, "dep_plane")
	store := fixture.store
	defer func() { closeEmbeddedDoltStore(t, store) }()

	const epicID = "plane-embedded-epic"
	if err := store.CreateIssue(ctx, &types.Issue{
		ID:        epicID,
		Title:     "plane epic " + epicID,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeEpic,
	}, "tester"); err != nil {
		t.Fatalf("create epic %s: %v", epicID, err)
	}
	childIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("%s-child-%c", epicID, 'a'+i)
		if err := store.CreateIssue(ctx, &types.Issue{
			ID:        id,
			Title:     "plane child " + id,
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}, "tester"); err != nil {
			t.Fatalf("create child %s: %v", id, err)
		}
		if err := store.AddDependency(ctx, &types.Dependency{
			IssueID:     id,
			DependsOnID: epicID,
			Type:        types.DepParentChild,
		}, "tester"); err != nil {
			t.Fatalf("add dependency %s -> %s: %v", id, epicID, err)
		}
		childIDs = append(childIDs, id)
	}

	assert := func(state string) {
		t.Helper()
		listed, err := store.GetDependentsWithMetadata(ctx, epicID)
		if err != nil {
			t.Fatalf("%s: GetDependentsWithMetadata: %v", state, err)
		}
		listedIDs := make([]string, 0, len(listed))
		for _, dep := range listed {
			listedIDs = append(listedIDs, dep.ID)
		}
		count, err := store.CountDependents(ctx, epicID)
		if err != nil {
			t.Fatalf("%s: CountDependents: %v", state, err)
		}
		iter, err := store.IterDependentsWithMetadata(ctx, epicID)
		if err != nil {
			t.Fatalf("%s: IterDependentsWithMetadata: %v", state, err)
		}
		streamedIDs := make([]string, 0, len(childIDs))
		for iter.Next(ctx) {
			if item := iter.Value(); item != nil {
				streamedIDs = append(streamedIDs, item.ID)
			}
		}
		if err := iter.Err(); err != nil {
			t.Fatalf("%s: IterDependentsWithMetadata stream: %v", state, err)
		}
		_ = iter.Close()

		src := workapi.NewStoreDetailSource(store)
		issue, _, err := workapi.GetIssueOrWisp(ctx, src, epicID)
		if err != nil {
			t.Fatalf("%s: GetIssueOrWisp %s: %v", state, epicID, err)
		}
		details, err := workapi.BuildIssueDetails(ctx, src, issue, false, workapi.DetailOptions{IncludeDependents: true})
		if err != nil {
			t.Fatalf("%s: BuildIssueDetails %s: %v", state, epicID, err)
		}
		detailIDs := make([]string, 0, len(details.Dependents))
		for _, dep := range details.Dependents {
			detailIDs = append(detailIDs, dep.ID)
		}

		want := sortedCopyOf(childIDs)
		for _, view := range []struct {
			name string
			got  []string
		}{
			{"GetDependentsWithMetadata", listedIDs},
			{"IterDependentsWithMetadata", streamedIDs},
			{"detail dependents", detailIDs},
		} {
			if !sameStringSet(sortedCopyOf(view.got), want) {
				t.Errorf("%s: %s = %v, want %v", state, view.name, view.got, childIDs)
			}
		}
		if count != int64(len(childIDs)) {
			t.Errorf("%s: CountDependents = %d, want %d", state, count, len(childIDs))
		}
		if details.DependentCount == nil || *details.DependentCount != int64(len(childIDs)) {
			t.Errorf("%s: detail dependent_count = %v, want %d", state, details.DependentCount, len(childIDs))
		}
		if details.UnresolvableDependents != nil {
			t.Errorf("%s: detail unresolvable_dependents = %d, want none (the count and the listing agree)", state, *details.UnresolvableDependents)
		}
		if details.EpicTotalChildren == nil || *details.EpicTotalChildren != len(childIDs) {
			t.Errorf("%s: detail epic children total = %v, want %d", state, details.EpicTotalChildren, len(childIDs))
		}
	}

	assert("control wisp plane present and empty")

	// Mirrored: copy the epic's inbound durable rows into wisp_dependencies
	// verbatim, with the same explicit column list migration 0053 uses for its
	// own cross-table move and a fresh UUID for the row id (the current
	// schema's `id` column has no default). FK checks are disabled around the
	// copy on the one connection because a durable row's source is a
	// permanent issue, which wisp_dependencies' own FKs reject.
	closeEmbeddedDoltStore(t, store)
	withEmbeddedFixtureConn(t, ctx, fixture, func(conn *sql.Conn) {
		mustExecOn(t, ctx, conn, "SET FOREIGN_KEY_CHECKS=0")
		mustExecOn(t, ctx, conn, `
			INSERT INTO wisp_dependencies
				(id, issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external,
				 type, created_at, created_by, metadata, thread_id)
			SELECT UUID(), issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external,
			       type, created_at, created_by, metadata, thread_id
			FROM dependencies
			WHERE COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) = ?
		`, epicID)
		mustExecOn(t, ctx, conn, "SET FOREIGN_KEY_CHECKS=1")
	})
	store = reopenEmbeddedFixture(t, ctx, fixture)
	assert("mirrored durable edges duplicated into wisp plane")

	// Missing: wisp_dependencies dropped from the working set — the state a
	// server-mode push or working-set reset leaves behind (#3445).
	closeEmbeddedDoltStore(t, store)
	withEmbeddedFixtureConn(t, ctx, fixture, func(conn *sql.Conn) {
		mustExecOn(t, ctx, conn, "DROP TABLE IF EXISTS wisp_dependencies")
	})
	store = reopenEmbeddedFixture(t, ctx, fixture)
	assert("missing wisp_dependencies")
}

func sortedCopyOf(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
// bd-cif6y4 (#3445): `bd show <epic>`'s CHILDREN block and dependent_count,
// and the server's detail JSON behind them, come from four different reads of
// the same anchor — the dependents listing, the dependents count, the
// streaming iterator, and workapi's detail assembly. All of them merge the
// durable plane with the wisp plane, which is dolt_ignore'd: a server-mode
// working set can legitimately hold the wisp tables empty, hold copies of the
// same edges next to the durable rows (another clone's promotion landed beside
// the still-local wisp copy), or lack the tables entirely. This test drives
// one epic through all of those states and asserts every view still agrees on
// the same children.
package dolt

import (
	"context"
	"sort"
	"testing"

	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/workapi"
)

// seedEpicWithChildren creates an epic and n open task children, each holding
// a parent-child edge pointing at the epic, and returns the epic's id and the
// children's ids.
func seedEpicWithChildren(t *testing.T, ctx context.Context, store *DoltStore, epicID string, n int) []string {
	t.Helper()
	if err := store.CreateIssue(ctx, &types.Issue{
		ID:        epicID,
		Title:     "plane epic " + epicID,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeEpic,
	}, "tester"); err != nil {
		t.Fatalf("create epic %s: %v", epicID, err)
	}
	childIDs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := epicID + "-child-" + string(rune('a'+i))
		createPerm(t, ctx, store, id)
		addDependency(t, ctx, store, id, epicID, types.DepParentChild)
		childIDs = append(childIDs, id)
	}
	return childIDs
}

// mirrorEpicDependentsIntoWispPlane copies the epic's inbound durable rows
// into wisp_dependencies verbatim — the cross-table duplicate #3445 reports:
// a pull that lands another clone's promotion next to the still-local wisp
// copy. The copy carries the same explicit column list migration 0053 uses
// for its own cross-table move, with a fresh UUID for the row id (the
// current schema's `id` column has no default). FOREIGN_KEY_CHECKS is
// disabled around the copy because a durable row's source is a permanent
// issue, which wisp_dependencies' own FKs reject.
func mirrorEpicDependentsIntoWispPlane(t *testing.T, ctx context.Context, store *DoltStore, epicID string) {
	t.Helper()
	if _, err := store.db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("disable FK checks: %v", err)
	}
	defer func() {
		if _, err := store.db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
			t.Fatalf("re-enable FK checks: %v", err)
		}
	}()
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO wisp_dependencies
			(id, issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external,
			 type, created_at, created_by, metadata, thread_id)
		SELECT UUID(), issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external,
		       type, created_at, created_by, metadata, thread_id
		FROM dependencies
		WHERE COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) = ?
	`, epicID); err != nil {
		t.Fatalf("mirror %s dependents into wisp_dependencies: %v", epicID, err)
	}
}

// dropWispTable drops one wisp-plane table that no other table's FK points
// at (wisp_dependencies only references other tables itself). Dropping wisps
// alone is not possible — wisp_child_counters holds an FK to it that Dolt
// refuses to orphan — so the whole-plane state below uses dropWispTables,
// which drops in FK-safe order.
func dropWispTable(t *testing.T, ctx context.Context, store *DoltStore, table string) {
	t.Helper()
	//nolint:gosec // table is a fixed wisp-plane schema table from the callers below.
	if _, err := store.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatalf("drop %s: %v", table, err)
	}
}

// sortedCopy returns a sorted copy of ids so two views of the same children
// can be compared without depending on either view's row order.
func sortedCopy(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSorted, wantSorted := sortedCopy(got), sortedCopy(want)
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			return false
		}
	}
	return true
}

// assertDependentViewsAgree asserts that every view of epicID's dependents —
// the listing `bd show` renders CHILDREN from, the count shown beside it, the
// stream the server's detail JSON walks, and workapi's detail assembly —
// answers the same childIDs in the store's CURRENT wisp-plane state.
func assertDependentViewsAgree(t *testing.T, ctx context.Context, store *DoltStore, epicID string, childIDs []string) {
	t.Helper()

	listed, err := store.GetDependentsWithMetadata(ctx, epicID)
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata: %v", err)
	}
	listedIDs := make([]string, 0, len(listed))
	for _, dep := range listed {
		listedIDs = append(listedIDs, dep.ID)
		if dep.DependencyType != types.DepParentChild {
			t.Errorf("dependent %s carried type %q, want parent-child", dep.ID, dep.DependencyType)
		}
	}
	if !sameIDs(listedIDs, childIDs) {
		t.Errorf("GetDependentsWithMetadata = %v, want %v", listedIDs, childIDs)
	}

	count, err := store.CountDependents(ctx, epicID)
	if err != nil {
		t.Fatalf("CountDependents: %v", err)
	}
	if count != int64(len(childIDs)) {
		t.Errorf("CountDependents = %d, want %d", count, len(childIDs))
	}

	iter, err := store.IterDependentsWithMetadata(ctx, epicID)
	if err != nil {
		t.Fatalf("IterDependentsWithMetadata: %v", err)
	}
	streamedIDs := make([]string, 0, len(childIDs))
	for iter.Next(ctx) {
		if item := iter.Value(); item != nil {
			streamedIDs = append(streamedIDs, item.ID)
		}
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("IterDependentsWithMetadata stream: %v", err)
	}
	_ = iter.Close()
	if !sameIDs(streamedIDs, childIDs) {
		t.Errorf("IterDependentsWithMetadata streamed %v, want %v", streamedIDs, childIDs)
	}

	src := workapi.NewStoreDetailSource(store)
	issue, _, err := workapi.GetIssueOrWisp(ctx, src, epicID)
	if err != nil {
		t.Fatalf("GetIssueOrWisp %s: %v", epicID, err)
	}
	details, err := workapi.BuildIssueDetails(ctx, src, issue, false, workapi.DetailOptions{IncludeDependents: true})
	if err != nil {
		t.Fatalf("BuildIssueDetails %s: %v", epicID, err)
	}
	if details.DependentCount == nil || *details.DependentCount != int64(len(childIDs)) {
		got := "nil"
		if details.DependentCount != nil {
			got = "non-nil"
		}
		t.Errorf("detail dependent_count = %s, want %d", got, len(childIDs))
	}
	if details.UnresolvableDependents != nil {
		t.Errorf("detail unresolvable_dependents = %d, want none (the count and the listing agree)", *details.UnresolvableDependents)
	}
	if !sameIDs(dependentIDs(details.Dependents), childIDs) {
		t.Errorf("detail dependents = %v, want %v", dependentIDs(details.Dependents), childIDs)
	}
	if details.EpicTotalChildren == nil || *details.EpicTotalChildren != len(childIDs) {
		t.Errorf("detail epic children total = %v, want %d", details.EpicTotalChildren, len(childIDs))
	}
	if details.EpicClosedChildren == nil || *details.EpicClosedChildren != 0 {
		t.Errorf("detail epic children closed = %v, want 0", details.EpicClosedChildren)
	}
}

func dependentIDs(deps []*types.IssueWithDependencyMetadata) []string {
	ids := make([]string, 0, len(deps))
	for _, dep := range deps {
		ids = append(ids, dep.ID)
	}
	return ids
}

// TestDependentsAcrossWispPlaneStates drives one epic through the wisp-plane
// states a server-mode working set can hold and asserts the four dependents
// views agree in every one. Before the fix the missing-plane states failed
// the streaming leg outright — the iterator's UNION ALL names a wisp table a
// dropped working set no longer has — which took BuildIssueDetails (and the
// server's detail JSON) down with it; the mirrored state is the agreement
// contract the earlier readers' de-duplication already pins at unit level.
func TestDependentsAcrossWispPlaneStates(t *testing.T) {
	const epicID = "plane-epic"
	states := []struct {
		name    string
		arrange func(t *testing.T, ctx context.Context, store *DoltStore)
	}{
		{
			name: "control wisp plane present and empty",
		},
		{
			name: "mirrored durable edges duplicated into wisp plane",
			arrange: func(t *testing.T, ctx context.Context, store *DoltStore) {
				mirrorEpicDependentsIntoWispPlane(t, ctx, store, epicID)
			},
		},
		{
			name: "missing wisp_dependencies",
			arrange: func(t *testing.T, ctx context.Context, store *DoltStore) {
				dropWispTable(t, ctx, store, "wisp_dependencies")
			},
		},
		{
			name: "missing wisp plane entirely",
			arrange: func(t *testing.T, ctx context.Context, store *DoltStore) {
				dropWispTables(t, ctx, store)
			},
		},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			store, cleanup := setupTestStore(t)
			defer cleanup()
			ctx, cancel := testContext(t)
			defer cancel()

			childIDs := seedEpicWithChildren(t, ctx, store, epicID, 3)
			if state.arrange != nil {
				state.arrange(t, ctx, store)
			}
			assertDependentViewsAgree(t, ctx, store, epicID, childIDs)
		})
	}
}
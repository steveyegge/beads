package dolt

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func TestWispCycleDetectionTablesUseBothTables(t *testing.T) {
	got := wispCycleDetectionTables()
	want := issueops.AllDepTables()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWispCycleReachabilityQuerySingleTableJoinsDirectly(t *testing.T) {
	query := wispCycleReachabilityQuery([]string{"wisp_issue_dependencies"})
	if !strings.Contains(query, "JOIN wisp_issue_dependencies d ON d.source_id = r.node") {
		t.Fatalf("query does not join wisp_issue_dependencies directly:\n%s", query)
	}
	if !strings.Contains(query, "SELECT d.depends_on_issue_id") {
		t.Fatalf("single-table wisp cycle query should project the typed target column:\n%s", query)
	}
	if strings.Contains(query, "JOIN (SELECT") {
		t.Fatalf("single-table wisp cycle query should not materialize a derived dependency table:\n%s", query)
	}
	if !strings.Contains(query, "d.type = 'blocks'") {
		t.Fatalf("query does not filter blocks at the direct join:\n%s", query)
	}
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("wisp cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
}

func TestWispCycleReachabilityQueryMultipleTablesTraversesUniqueNodes(t *testing.T) {
	query := wispCycleReachabilityQuery(issueops.AllDepTables())
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("multi-table wisp cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
	for _, table := range issueops.AllDepTables() {
		if !strings.Contains(query, "FROM "+table) {
			t.Fatalf("query does not include %s table:\n%s", table, query)
		}
		col := issueops.DepTargetColumnForTable(table)
		if !strings.Contains(query, col+" AS depends_on_id FROM "+table) {
			t.Fatalf("query does not project typed column %s from %s:\n%s", col, table, query)
		}
	}
}

func TestAddDependencyRejectsPermanentEndpointCycleThroughWisp(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	const (
		permA = "cycle-perm-a"
		permX = "cycle-perm-x"
		wispW = "cycle-wisp-w"
	)
	createPerm(t, ctx, store, permA)
	createPerm(t, ctx, store, permX)
	createWisp(t, ctx, store, wispW)

	mustAddBlockingDependency(t, ctx, store, permX, wispW)
	mustAddBlockingDependency(t, ctx, store, wispW, permA)

	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     permA,
		DependsOnID: permX,
		Type:        types.DepBlocks,
	}, "tester")
	assertCycleError(t, err)
}

func TestAddDependencyRejectsWispEndpointCycleThroughPermanent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	const (
		wispA = "cycle-wisp-a"
		wispX = "cycle-wisp-x"
		permB = "cycle-perm-b"
	)
	createWisp(t, ctx, store, wispA)
	createWisp(t, ctx, store, wispX)
	createPerm(t, ctx, store, permB)

	mustAddBlockingDependency(t, ctx, store, wispX, permB)
	mustAddBlockingDependency(t, ctx, store, permB, wispA)

	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     wispA,
		DependsOnID: wispX,
		Type:        types.DepBlocks,
	}, "tester")
	assertCycleError(t, err)
}

func mustAddBlockingDependency(t *testing.T, ctx context.Context, store *DoltStore, issueID, dependsOnID string) {
	t.Helper()
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issueID,
		DependsOnID: dependsOnID,
		Type:        types.DepBlocks,
	}, "tester"); err != nil {
		t.Fatalf("AddDependency %s->%s: %v", issueID, dependsOnID, err)
	}
}

func assertCycleError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected AddDependency to reject mixed-table cycle, but it succeeded")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

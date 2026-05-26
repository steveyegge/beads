package dolt

import (
	"context"
	"fmt"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func assertReadyAPIsAgree(t *testing.T, ctx context.Context, store *DoltStore, filter types.WorkFilter) []string {
	t.Helper()

	plain, err := store.GetReadyWork(ctx, filter)
	if err != nil {
		t.Fatalf("GetReadyWork: %v", err)
	}
	counted, err := store.GetReadyWorkWithCounts(ctx, filter)
	if err != nil {
		t.Fatalf("GetReadyWorkWithCounts: %v", err)
	}

	plainIDs := issueIDs(plain)
	countedIDs := issueWithCountsIDs(counted)
	if fmt.Sprint(plainIDs) != fmt.Sprint(countedIDs) {
		t.Fatalf("ready API mismatch:\nplain:   %v\ncounted: %v", plainIDs, countedIDs)
	}
	return plainIDs
}

func assertReadyContains(t *testing.T, ids []string, want ...string) {
	t.Helper()
	set := readyIDSet(ids)
	for _, id := range want {
		if !set[id] {
			t.Fatalf("ready IDs missing %s: %v", id, ids)
		}
	}
}

func assertReadyExcludes(t *testing.T, ids []string, blocked ...string) {
	t.Helper()
	set := readyIDSet(ids)
	for _, id := range blocked {
		if set[id] {
			t.Fatalf("ready IDs included blocked %s: %v", id, ids)
		}
	}
}

func TestReadyWorkAPIsAgreeOnMixedIssueWispBlockedGraph(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	for _, id := range []string{
		"ready-mixed-issue-blocker",
		"ready-mixed-issue-blocked-by-issue",
		"ready-mixed-issue-blocked-by-wisp",
		"ready-mixed-issue-parent-blocked-by-wisp",
		"ready-mixed-issue-child-of-wisp",
		"ready-mixed-ready-control",
	} {
		createPerm(t, ctx, store, id)
	}
	for _, id := range []string{
		"ready-mixed-wisp-blocker",
		"ready-mixed-wisp-blocked-by-issue",
		"ready-mixed-wisp-blocked-by-wisp",
		"ready-mixed-wisp-parent-blocked-by-issue",
		"ready-mixed-wisp-child-of-issue",
		"ready-mixed-wisp-ready-control",
	} {
		createNoHistoryWisp(t, ctx, store, id)
	}

	addDependency(t, ctx, store, "ready-mixed-issue-blocked-by-issue", "ready-mixed-issue-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "ready-mixed-issue-blocked-by-wisp", "ready-mixed-wisp-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "ready-mixed-wisp-blocked-by-issue", "ready-mixed-issue-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "ready-mixed-wisp-blocked-by-wisp", "ready-mixed-wisp-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "ready-mixed-issue-parent-blocked-by-wisp", "ready-mixed-wisp-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "ready-mixed-issue-child-of-wisp", "ready-mixed-issue-parent-blocked-by-wisp", types.DepParentChild)
	addDependency(t, ctx, store, "ready-mixed-wisp-parent-blocked-by-issue", "ready-mixed-issue-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "ready-mixed-wisp-child-of-issue", "ready-mixed-wisp-parent-blocked-by-issue", types.DepParentChild)

	defaultIDs := assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{SortPolicy: types.SortPolicyPriority})
	assertReadyContains(t, defaultIDs, "ready-mixed-ready-control", "ready-mixed-wisp-ready-control")
	assertReadyExcludes(t, defaultIDs,
		"ready-mixed-issue-blocked-by-issue",
		"ready-mixed-issue-blocked-by-wisp",
		"ready-mixed-wisp-blocked-by-issue",
		"ready-mixed-wisp-blocked-by-wisp",
		"ready-mixed-issue-child-of-wisp",
		"ready-mixed-wisp-child-of-issue",
	)
}

func TestReadyWorkAPIsAgreeOnMixedIssueWispWaitsForGraph(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "ready-wf-issue-waiter")
	createPerm(t, ctx, store, "ready-wf-issue-spawner")
	createNoHistoryWisp(t, ctx, store, "ready-wf-wisp-child")
	addDependency(t, ctx, store, "ready-wf-wisp-child", "ready-wf-issue-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "ready-wf-issue-waiter", "ready-wf-issue-spawner", types.DepWaitsFor)

	createNoHistoryWisp(t, ctx, store, "ready-wf-wisp-waiter")
	createNoHistoryWisp(t, ctx, store, "ready-wf-wisp-spawner")
	createPerm(t, ctx, store, "ready-wf-issue-child")
	addDependency(t, ctx, store, "ready-wf-issue-child", "ready-wf-wisp-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "ready-wf-wisp-waiter", "ready-wf-wisp-spawner", types.DepWaitsFor)

	blockedIDs := assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{IncludeEphemeral: true})
	assertReadyExcludes(t, blockedIDs, "ready-wf-issue-waiter", "ready-wf-wisp-waiter")

	if err := store.CloseIssue(ctx, "ready-wf-wisp-child", "done", "tester", ""); err != nil {
		t.Fatalf("CloseIssue wisp child: %v", err)
	}
	if err := store.CloseIssue(ctx, "ready-wf-issue-child", "done", "tester", ""); err != nil {
		t.Fatalf("CloseIssue issue child: %v", err)
	}
	readyIDs := assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{IncludeEphemeral: true})
	assertReadyContains(t, readyIDs, "ready-wf-issue-waiter", "ready-wf-wisp-waiter")
}

func TestReadyWorkAlreadyClosedChildAndDeleteWaiterMutations(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "ready-mut-any-waiter")
	createPerm(t, ctx, store, "ready-mut-any-spawner")
	createPerm(t, ctx, store, "ready-mut-any-active-child")
	createPerm(t, ctx, store, "ready-mut-any-closed-child")
	addDependency(t, ctx, store, "ready-mut-any-active-child", "ready-mut-any-spawner", types.DepParentChild)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     "ready-mut-any-waiter",
		DependsOnID: "ready-mut-any-spawner",
		Type:        types.DepWaitsFor,
		Metadata:    `{"gate":"any-children"}`,
	}, "tester"); err != nil {
		t.Fatalf("add any-children waiter: %v", err)
	}
	assertReadyExcludes(t, assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{}), "ready-mut-any-waiter")
	if err := store.CloseIssue(ctx, "ready-mut-any-closed-child", "done", "tester", ""); err != nil {
		t.Fatalf("CloseIssue closed child: %v", err)
	}
	addDependency(t, ctx, store, "ready-mut-any-closed-child", "ready-mut-any-spawner", types.DepParentChild)
	assertReadyContains(t, assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{}), "ready-mut-any-waiter")

	createPerm(t, ctx, store, "ready-mut-del-waiter")
	createPerm(t, ctx, store, "ready-mut-del-spawner")
	createPerm(t, ctx, store, "ready-mut-del-child")
	addDependency(t, ctx, store, "ready-mut-del-child", "ready-mut-del-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "ready-mut-del-waiter", "ready-mut-del-spawner", types.DepWaitsFor)
	assertReadyExcludes(t, assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{}), "ready-mut-del-waiter")
	if err := store.DeleteIssue(ctx, "ready-mut-del-child"); err != nil {
		t.Fatalf("DeleteIssue child: %v", err)
	}
	assertReadyContains(t, assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{}), "ready-mut-del-waiter")
}

func TestReadyWorkWithCountsToleratesMissingWispTables(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "ready-missing-wisps-issue")
	dropWispTables(t, ctx, store)

	ids := assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{Limit: 10})
	if fmt.Sprint(ids) != fmt.Sprint([]string{"ready-missing-wisps-issue"}) {
		t.Fatalf("ready IDs with missing wisp tables = %v, want permanent issue only", ids)
	}
}

func TestReadyWorkMigrationRecomputesReadyVisibleState(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "ready-mig-issue-blocked-by-wisp")
	createNoHistoryWisp(t, ctx, store, "ready-mig-wisp-blocker")
	addDependency(t, ctx, store, "ready-mig-issue-blocked-by-wisp", "ready-mig-wisp-blocker", types.DepBlocks)

	createNoHistoryWisp(t, ctx, store, "ready-mig-wisp-blocked-by-issue")
	createPerm(t, ctx, store, "ready-mig-issue-blocker")
	addDependency(t, ctx, store, "ready-mig-wisp-blocked-by-issue", "ready-mig-issue-blocker", types.DepBlocks)

	createPerm(t, ctx, store, "ready-mig-waiter")
	createPerm(t, ctx, store, "ready-mig-spawner")
	createPerm(t, ctx, store, "ready-mig-active-child")
	addDependency(t, ctx, store, "ready-mig-active-child", "ready-mig-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "ready-mig-waiter", "ready-mig-spawner", types.DepWaitsFor)

	if _, err := store.db.ExecContext(ctx, `
		UPDATE issues SET is_blocked = 0
		WHERE id IN ('ready-mig-issue-blocked-by-wisp', 'ready-mig-waiter')
	`); err != nil {
		t.Fatalf("seed stale issue projections: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE wisps SET is_blocked = 0
		WHERE id = 'ready-mig-wisp-blocked-by-issue'
	`); err != nil {
		t.Fatalf("seed stale wisp projections: %v", err)
	}

	runMigrationSQL(t, ctx, store, "../schema/migrations/0047_recompute_mixed_is_blocked.up.sql")

	ids := assertReadyAPIsAgree(t, ctx, store, types.WorkFilter{})
	assertReadyExcludes(t, ids,
		"ready-mig-issue-blocked-by-wisp",
		"ready-mig-wisp-blocked-by-issue",
		"ready-mig-waiter",
	)
}

package dolt

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestPR4107WispIsBlockedMigrationBackfillsBlockedWisps(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-wisp-mig-blocker")
	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-mig-nohist")
	createEphemeralWisp(t, ctx, store, "pr4107-wisp-mig-ephemeral")
	addDependency(t, ctx, store, "pr4107-wisp-mig-nohist", "pr4107-wisp-mig-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-wisp-mig-ephemeral", "pr4107-wisp-mig-blocker", types.DepBlocks)

	dropWispIsBlockedProjection(t, ctx, store)
	runMigrationSQLFilesFrom(t, ctx, store, "../schema/migrations/ignored", 6)

	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-mig-nohist") {
		t.Errorf("no-history wisp blocked by an open issue was not backfilled as blocked")
	}
	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-mig-ephemeral") {
		t.Errorf("ephemeral wisp blocked by an open issue was not backfilled as blocked")
	}

	defaultReady, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("GetReadyWork default: %v", err)
	}
	defaultIDs := readyIDSet(issueIDs(defaultReady))
	if defaultIDs["pr4107-wisp-mig-nohist"] {
		t.Errorf("default ready work returned blocked no-history wisp after migration: %v", issueIDs(defaultReady))
	}

	ephemeralReady, err := store.GetReadyWork(ctx, types.WorkFilter{IncludeEphemeral: true})
	if err != nil {
		t.Fatalf("GetReadyWork include ephemeral: %v", err)
	}
	ephemeralIDs := readyIDSet(issueIDs(ephemeralReady))
	if ephemeralIDs["pr4107-wisp-mig-ephemeral"] {
		t.Errorf("include-ephemeral ready work returned blocked ephemeral wisp after migration: %v", issueIDs(ephemeralReady))
	}
}

func TestPR4107IssueIsBlockedMigrationMatchesRuntimeMixedGraphSemantics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-issue-mig-blocked-by-wisp")
	createEphemeralWisp(t, ctx, store, "pr4107-issue-mig-wisp-blocker")
	addDependency(t, ctx, store, "pr4107-issue-mig-blocked-by-wisp", "pr4107-issue-mig-wisp-blocker", types.DepBlocks)

	createPerm(t, ctx, store, "pr4107-issue-mig-waiter")
	createPerm(t, ctx, store, "pr4107-issue-mig-spawner")
	addDependency(t, ctx, store, "pr4107-issue-mig-waiter", "pr4107-issue-mig-spawner", types.DepWaitsFor)

	runMigrationSQLFilesFrom(t, ctx, store, "../schema/migrations", 46)

	if !getIsBlocked(t, ctx, store, "issues", "pr4107-issue-mig-blocked-by-wisp") {
		t.Errorf("issue blocked by an open wisp was not backfilled as blocked")
	}
	if getIsBlocked(t, ctx, store, "issues", "pr4107-issue-mig-waiter") {
		t.Errorf("waits-for issue with no active children was backfilled as blocked")
	}
}

func TestPR4107WispMigrationBackfillsMixedParentChildPropagation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-wisp-parent-blocker")
	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-blocked-parent")
	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-blocked-child")
	addDependency(t, ctx, store, "pr4107-wisp-blocked-parent", "pr4107-wisp-parent-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-wisp-blocked-child", "pr4107-wisp-blocked-parent", types.DepParentChild)

	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-blocker")
	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-blocked-by-wisp")
	addDependency(t, ctx, store, "pr4107-wisp-blocked-by-wisp", "pr4107-wisp-blocker", types.DepBlocks)

	dropWispIsBlockedProjection(t, ctx, store)
	runMigrationSQLFilesFrom(t, ctx, store, "../schema/migrations/ignored", 6)

	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-blocked-parent") {
		t.Errorf("wisp directly blocked by an issue was not backfilled as blocked")
	}
	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-blocked-child") {
		t.Errorf("child wisp did not inherit blocked parent state during migration backfill")
	}
	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-blocked-by-wisp") {
		t.Errorf("wisp blocked by an open wisp was not backfilled as blocked")
	}
}

func TestPR4107WispMigrationBackfillsWaitsForSemantics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-waits-for-active")
	createPerm(t, ctx, store, "pr4107-wisp-waits-for-spawner")
	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-waits-for-child")
	addDependency(t, ctx, store, "pr4107-wisp-waits-for-child", "pr4107-wisp-waits-for-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "pr4107-wisp-waits-for-active", "pr4107-wisp-waits-for-spawner", types.DepWaitsFor)

	createNoHistoryWisp(t, ctx, store, "pr4107-wisp-waits-for-no-children")
	createPerm(t, ctx, store, "pr4107-wisp-waits-for-empty-spawner")
	addDependency(t, ctx, store, "pr4107-wisp-waits-for-no-children", "pr4107-wisp-waits-for-empty-spawner", types.DepWaitsFor)

	dropWispIsBlockedProjection(t, ctx, store)
	runMigrationSQLFilesFrom(t, ctx, store, "../schema/migrations/ignored", 6)

	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-waits-for-active") {
		t.Errorf("wisp waits-for gate with an active child was not backfilled as blocked")
	}
	if getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-waits-for-no-children") {
		t.Errorf("wisp waits-for gate with no children was backfilled as blocked")
	}
}

func TestPR4107RuntimeIsBlockedMaintainsMixedIssueWispGraph(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-runtime-issue-blocked-by-wisp")
	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wisp-blocker-a")
	addDependency(t, ctx, store, "pr4107-runtime-issue-blocked-by-wisp", "pr4107-runtime-wisp-blocker-a", types.DepBlocks)

	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wisp-blocked-by-issue")
	createPerm(t, ctx, store, "pr4107-runtime-issue-blocker-a")
	addDependency(t, ctx, store, "pr4107-runtime-wisp-blocked-by-issue", "pr4107-runtime-issue-blocker-a", types.DepBlocks)

	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-blocked-wisp-parent")
	createPerm(t, ctx, store, "pr4107-runtime-issue-blocker-b")
	createPerm(t, ctx, store, "pr4107-runtime-issue-child-of-wisp")
	addDependency(t, ctx, store, "pr4107-runtime-blocked-wisp-parent", "pr4107-runtime-issue-blocker-b", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-runtime-issue-child-of-wisp", "pr4107-runtime-blocked-wisp-parent", types.DepParentChild)

	createPerm(t, ctx, store, "pr4107-runtime-blocked-issue-parent")
	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wisp-blocker-b")
	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wisp-child-of-issue")
	addDependency(t, ctx, store, "pr4107-runtime-blocked-issue-parent", "pr4107-runtime-wisp-blocker-b", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-runtime-wisp-child-of-issue", "pr4107-runtime-blocked-issue-parent", types.DepParentChild)

	assertIsBlocked(t, ctx, store, "issues", "pr4107-runtime-issue-blocked-by-wisp", true)
	assertIsBlocked(t, ctx, store, "wisps", "pr4107-runtime-wisp-blocked-by-issue", true)
	assertIsBlocked(t, ctx, store, "issues", "pr4107-runtime-issue-child-of-wisp", true)
	assertIsBlocked(t, ctx, store, "wisps", "pr4107-runtime-wisp-child-of-issue", true)
}

func TestPR4107RuntimeWaitsForHandlesMixedIssueWispChildren(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-runtime-wf-issue-waiter")
	createPerm(t, ctx, store, "pr4107-runtime-wf-issue-spawner")
	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wf-wisp-child")
	addDependency(t, ctx, store, "pr4107-runtime-wf-wisp-child", "pr4107-runtime-wf-issue-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "pr4107-runtime-wf-issue-waiter", "pr4107-runtime-wf-issue-spawner", types.DepWaitsFor)

	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wf-wisp-waiter")
	createNoHistoryWisp(t, ctx, store, "pr4107-runtime-wf-wisp-spawner")
	createPerm(t, ctx, store, "pr4107-runtime-wf-issue-child")
	addDependency(t, ctx, store, "pr4107-runtime-wf-issue-child", "pr4107-runtime-wf-wisp-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "pr4107-runtime-wf-wisp-waiter", "pr4107-runtime-wf-wisp-spawner", types.DepWaitsFor)

	assertIsBlocked(t, ctx, store, "issues", "pr4107-runtime-wf-issue-waiter", true)
	assertIsBlocked(t, ctx, store, "wisps", "pr4107-runtime-wf-wisp-waiter", true)

	if err := store.CloseIssue(ctx, "pr4107-runtime-wf-wisp-child", "done", "tester", ""); err != nil {
		t.Fatalf("CloseIssue wisp child: %v", err)
	}
	if err := store.CloseIssue(ctx, "pr4107-runtime-wf-issue-child", "done", "tester", ""); err != nil {
		t.Fatalf("CloseIssue issue child: %v", err)
	}
	assertIsBlocked(t, ctx, store, "issues", "pr4107-runtime-wf-issue-waiter", false)
	assertIsBlocked(t, ctx, store, "wisps", "pr4107-runtime-wf-wisp-waiter", false)
}

func TestPR4107DeleteRecomputesWaitersWhenSpawnerChildIsDeleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-delete-waiter")
	createPerm(t, ctx, store, "pr4107-delete-spawner")
	createPerm(t, ctx, store, "pr4107-delete-child")
	addDependency(t, ctx, store, "pr4107-delete-child", "pr4107-delete-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "pr4107-delete-waiter", "pr4107-delete-spawner", types.DepWaitsFor)

	if !getIsBlocked(t, ctx, store, "issues", "pr4107-delete-waiter") {
		t.Fatal("waiter should start blocked while the spawner has an active child")
	}

	if err := store.DeleteIssue(ctx, "pr4107-delete-child"); err != nil {
		t.Fatalf("DeleteIssue child: %v", err)
	}
	if getIsBlocked(t, ctx, store, "issues", "pr4107-delete-waiter") {
		t.Fatalf("waiter remained blocked after deleting the spawner's only active child")
	}
}

func TestPR4107DeleteRecomputesWaitersWhenSpawnerIsDeleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-delete-spawner-waiter")
	createPerm(t, ctx, store, "pr4107-delete-spawner-target")
	createPerm(t, ctx, store, "pr4107-delete-spawner-child")
	addDependency(t, ctx, store, "pr4107-delete-spawner-child", "pr4107-delete-spawner-target", types.DepParentChild)
	addDependency(t, ctx, store, "pr4107-delete-spawner-waiter", "pr4107-delete-spawner-target", types.DepWaitsFor)

	assertIsBlocked(t, ctx, store, "issues", "pr4107-delete-spawner-waiter", true)

	if err := store.DeleteIssue(ctx, "pr4107-delete-spawner-target"); err != nil {
		t.Fatalf("DeleteIssue spawner: %v", err)
	}
	assertIsBlocked(t, ctx, store, "issues", "pr4107-delete-spawner-waiter", false)
}

func TestPR4107DeleteRecomputesWispWaitersWhenSpawnerChildIsDeleted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createNoHistoryWisp(t, ctx, store, "pr4107-delete-wisp-waiter")
	createPerm(t, ctx, store, "pr4107-delete-wisp-waiter-spawner")
	createNoHistoryWisp(t, ctx, store, "pr4107-delete-wisp-waiter-child")
	addDependency(t, ctx, store, "pr4107-delete-wisp-waiter-child", "pr4107-delete-wisp-waiter-spawner", types.DepParentChild)
	addDependency(t, ctx, store, "pr4107-delete-wisp-waiter", "pr4107-delete-wisp-waiter-spawner", types.DepWaitsFor)

	assertIsBlocked(t, ctx, store, "wisps", "pr4107-delete-wisp-waiter", true)

	if err := store.DeleteIssue(ctx, "pr4107-delete-wisp-waiter-child"); err != nil {
		t.Fatalf("DeleteIssue wisp child: %v", err)
	}
	assertIsBlocked(t, ctx, store, "wisps", "pr4107-delete-wisp-waiter", false)
}

func TestPR4107JSONCountReadPathsTolerateMissingWispTables(t *testing.T) {
	t.Run("query_counts", func(t *testing.T) {
		store, cleanup := setupTestStore(t)
		defer cleanup()

		ctx, cancel := testContext(t)
		defer cancel()

		createPerm(t, ctx, store, "pr4107-missing-wisps-query")
		dropWispTables(t, ctx, store)

		results, err := store.SearchIssuesWithCounts(ctx, "", types.IssueFilter{Limit: 10})
		if err != nil {
			t.Fatalf("SearchIssuesWithCounts should tolerate missing wisp tables: %v", err)
		}
		if len(results) != 1 || results[0].Issue.ID != "pr4107-missing-wisps-query" {
			t.Fatalf("SearchIssuesWithCounts returned %+v, want the permanent issue only", results)
		}
	})

	t.Run("ready_counts", func(t *testing.T) {
		store, cleanup := setupTestStore(t)
		defer cleanup()

		ctx, cancel := testContext(t)
		defer cancel()

		createPerm(t, ctx, store, "pr4107-missing-wisps-ready")
		dropWispTables(t, ctx, store)

		results, err := store.GetReadyWorkWithCounts(ctx, types.WorkFilter{Limit: 10})
		if err != nil {
			t.Fatalf("GetReadyWorkWithCounts should tolerate missing wisp tables: %v", err)
		}
		if len(results) != 1 || results[0].Issue.ID != "pr4107-missing-wisps-ready" {
			t.Fatalf("GetReadyWorkWithCounts returned %+v, want the permanent issue only", results)
		}
	})
}

func TestPR4107SearchIssuesWithCountsLimitIsGlobalAcrossIssuesAndWisps(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-limit-issue")
	createEphemeralWisp(t, ctx, store, "pr4107-limit-wisp")

	results, err := store.SearchIssuesWithCounts(ctx, "", types.IssueFilter{Limit: 1})
	if err != nil {
		t.Fatalf("SearchIssuesWithCounts limit=1: %v", err)
	}
	if len(results) > 1 {
		t.Fatalf("SearchIssuesWithCounts limit=1 returned %d rows; want at most 1", len(results))
	}
}

func TestPR4107SearchIssuesWithCountsLimitUsesGlobalSortOrderAcrossIssuesAndWisps(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	if err := store.CreateIssue(ctx, &types.Issue{
		ID:        "pr4107-limit-low-priority-issue",
		Title:     "low priority issue",
		Status:    types.StatusOpen,
		Priority:  4,
		IssueType: types.TypeTask,
	}, "tester"); err != nil {
		t.Fatalf("create low-priority issue: %v", err)
	}
	if err := store.CreateIssue(ctx, &types.Issue{
		ID:        "pr4107-limit-high-priority-wisp",
		Title:     "high priority wisp",
		Status:    types.StatusOpen,
		Priority:  0,
		IssueType: types.TypeTask,
		NoHistory: true,
	}, "tester"); err != nil {
		t.Fatalf("create high-priority wisp: %v", err)
	}

	results, err := store.SearchIssuesWithCounts(ctx, "", types.IssueFilter{Limit: 1})
	if err != nil {
		t.Fatalf("SearchIssuesWithCounts limit=1: %v", err)
	}
	if got := issueWithCountsIDs(results); len(got) != 1 || got[0] != "pr4107-limit-high-priority-wisp" {
		t.Fatalf("SearchIssuesWithCounts limit=1 IDs = %v, want [pr4107-limit-high-priority-wisp]", got)
	}
}

func TestPR4107ReadyWorkWithCountsLimitUsesGlobalSortOrderAcrossIssuesAndWisps(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	if err := store.CreateIssue(ctx, &types.Issue{
		ID:        "pr4107-ready-limit-low-priority-issue",
		Title:     "low priority ready issue",
		Status:    types.StatusOpen,
		Priority:  4,
		IssueType: types.TypeTask,
	}, "tester"); err != nil {
		t.Fatalf("create low-priority issue: %v", err)
	}
	if err := store.CreateIssue(ctx, &types.Issue{
		ID:        "pr4107-ready-limit-high-priority-wisp",
		Title:     "high priority ready wisp",
		Status:    types.StatusOpen,
		Priority:  0,
		IssueType: types.TypeTask,
		NoHistory: true,
	}, "tester"); err != nil {
		t.Fatalf("create high-priority no-history wisp: %v", err)
	}

	results, err := store.GetReadyWorkWithCounts(ctx, types.WorkFilter{Limit: 1, SortPolicy: types.SortPolicyPriority})
	if err != nil {
		t.Fatalf("GetReadyWorkWithCounts limit=1: %v", err)
	}
	if got := issueWithCountsIDs(results); len(got) != 1 || got[0] != "pr4107-ready-limit-high-priority-wisp" {
		t.Fatalf("GetReadyWorkWithCounts limit=1 IDs = %v, want [pr4107-ready-limit-high-priority-wisp]", got)
	}
}

func TestPR4107SearchIssuesWithCountsMatchesMixedDependencyCounts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-counts-target")
	createPerm(t, ctx, store, "pr4107-counts-issue-dependent")
	createNoHistoryWisp(t, ctx, store, "pr4107-counts-wisp-dependent")
	createNoHistoryWisp(t, ctx, store, "pr4107-counts-wisp-source")

	addDependency(t, ctx, store, "pr4107-counts-issue-dependent", "pr4107-counts-target", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-counts-wisp-dependent", "pr4107-counts-target", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-counts-wisp-source", "pr4107-counts-target", types.DepBlocks)

	results, err := store.SearchIssuesWithCounts(ctx, "", types.IssueFilter{Limit: 0})
	if err != nil {
		t.Fatalf("SearchIssuesWithCounts: %v", err)
	}
	byID := issueWithCountsByID(results)

	target := byID["pr4107-counts-target"]
	if target == nil {
		t.Fatalf("target issue missing from SearchIssuesWithCounts: %v", issueWithCountsIDs(results))
	}
	if target.DependentCount != 3 {
		t.Fatalf("target dependent count = %d, want 3 mixed issue+wisp dependents", target.DependentCount)
	}

	wispSource := byID["pr4107-counts-wisp-source"]
	if wispSource == nil {
		t.Fatalf("wisp source missing from SearchIssuesWithCounts: %v", issueWithCountsIDs(results))
	}
	if wispSource.DependencyCount != 1 {
		t.Fatalf("wisp source dependency count = %d, want 1", wispSource.DependencyCount)
	}
}

func TestPR4107ReadyWorkWithCountsExcludesBlockedWisps(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-ready-counts-blocker")
	createNoHistoryWisp(t, ctx, store, "pr4107-ready-counts-nohistory-blocked")
	createEphemeralWisp(t, ctx, store, "pr4107-ready-counts-ephemeral-blocked")
	addDependency(t, ctx, store, "pr4107-ready-counts-nohistory-blocked", "pr4107-ready-counts-blocker", types.DepBlocks)
	addDependency(t, ctx, store, "pr4107-ready-counts-ephemeral-blocked", "pr4107-ready-counts-blocker", types.DepBlocks)

	defaultReady, err := store.GetReadyWorkWithCounts(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("GetReadyWorkWithCounts default: %v", err)
	}
	defaultIDs := readyIDSet(issueWithCountsIDs(defaultReady))
	if defaultIDs["pr4107-ready-counts-nohistory-blocked"] {
		t.Fatalf("default ready counts returned blocked no-history wisp: %v", issueWithCountsIDs(defaultReady))
	}

	ephemeralReady, err := store.GetReadyWorkWithCounts(ctx, types.WorkFilter{IncludeEphemeral: true})
	if err != nil {
		t.Fatalf("GetReadyWorkWithCounts include ephemeral: %v", err)
	}
	ephemeralIDs := readyIDSet(issueWithCountsIDs(ephemeralReady))
	if ephemeralIDs["pr4107-ready-counts-ephemeral-blocked"] {
		t.Fatalf("include-ephemeral ready counts returned blocked ephemeral wisp: %v", issueWithCountsIDs(ephemeralReady))
	}
}

func TestPR4107SearchIssuesWithCountsEphemeralFalseIncludesNoHistoryOnly(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	createPerm(t, ctx, store, "pr4107-eph-false-issue")
	createNoHistoryWisp(t, ctx, store, "pr4107-eph-false-nohistory")
	createEphemeralWisp(t, ctx, store, "pr4107-eph-false-ephemeral")

	ephemeral := false
	results, err := store.SearchIssuesWithCounts(ctx, "", types.IssueFilter{Ephemeral: &ephemeral})
	if err != nil {
		t.Fatalf("SearchIssuesWithCounts ephemeral=false: %v", err)
	}
	ids := readyIDSet(issueWithCountsIDs(results))
	if !ids["pr4107-eph-false-issue"] {
		t.Fatalf("permanent issue missing from ephemeral=false results: %v", issueWithCountsIDs(results))
	}
	if !ids["pr4107-eph-false-nohistory"] {
		t.Fatalf("no-history wisp missing from ephemeral=false results: %v", issueWithCountsIDs(results))
	}
	if ids["pr4107-eph-false-ephemeral"] {
		t.Fatalf("true ephemeral wisp leaked into ephemeral=false results: %v", issueWithCountsIDs(results))
	}
}

func createNoHistoryWisp(t *testing.T, ctx context.Context, store *DoltStore, id string) {
	t.Helper()
	issue := &types.Issue{
		ID:        id,
		Title:     "no-history " + id,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		NoHistory: true,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("create no-history wisp %s: %v", id, err)
	}
}

func createEphemeralWisp(t *testing.T, ctx context.Context, store *DoltStore, id string) {
	t.Helper()
	issue := &types.Issue{
		ID:        id,
		Title:     "ephemeral " + id,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Ephemeral: true,
	}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("create ephemeral wisp %s: %v", id, err)
	}
}

func addDependency(t *testing.T, ctx context.Context, store *DoltStore, source, target string, depType types.DependencyType) {
	t.Helper()
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     source,
		DependsOnID: target,
		Type:        depType,
	}, "tester"); err != nil {
		t.Fatalf("AddDependency %s -> %s (%s): %v", source, target, depType, err)
	}
}

func assertIsBlocked(t *testing.T, ctx context.Context, store *DoltStore, table, id string, want bool) {
	t.Helper()
	if got := getIsBlocked(t, ctx, store, table, id); got != want {
		t.Fatalf("%s.%s is_blocked = %v, want %v", table, id, got, want)
	}
}

func dropWispIsBlockedProjection(t *testing.T, ctx context.Context, store *DoltStore) {
	t.Helper()
	if _, err := store.db.ExecContext(ctx, "DROP INDEX idx_wisps_is_blocked ON wisps"); err != nil {
		t.Fatalf("drop idx_wisps_is_blocked: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "ALTER TABLE wisps DROP COLUMN is_blocked"); err != nil {
		t.Fatalf("drop wisps.is_blocked: %v", err)
	}
}

func runMigrationSQL(t *testing.T, ctx context.Context, store *DoltStore, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if _, err := store.db.ExecContext(ctx, string(data)); err != nil {
		t.Fatalf("run migration %s: %v", path, err)
	}
}

func runMigrationSQLFilesFrom(t *testing.T, ctx context.Context, store *DoltStore, dir string, minVersion int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migration dir %s: %v", dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Fatalf("migration filename %s has no version prefix", name)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("parse migration version from %s: %v", name, err)
		}
		if version < minVersion {
			continue
		}
		runMigrationSQL(t, ctx, store, filepath.Join(dir, name))
	}
}

func issueWithCountsIDs(items []*types.IssueWithCounts) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || item.Issue == nil {
			continue
		}
		ids = append(ids, item.Issue.ID)
	}
	return ids
}

func issueWithCountsByID(items []*types.IssueWithCounts) map[string]*types.IssueWithCounts {
	byID := make(map[string]*types.IssueWithCounts, len(items))
	for _, item := range items {
		if item == nil || item.Issue == nil {
			continue
		}
		byID[item.Issue.ID] = item
	}
	return byID
}

func dropWispTables(t *testing.T, ctx context.Context, store *DoltStore) {
	t.Helper()
	for _, table := range []string{
		"wisp_dependencies",
		"wisp_labels",
		"wisp_events",
		"wisp_comments",
		"wisp_child_counters",
		"wisps",
	} {
		//nolint:gosec // table names are fixed test schema tables from the list above.
		if _, err := store.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
}

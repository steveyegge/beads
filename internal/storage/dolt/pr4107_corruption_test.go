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

	dropWispIsBlockedProjection(t, ctx, store)
	runMigrationSQLFilesFrom(t, ctx, store, "../schema/migrations/ignored", 6)

	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-blocked-parent") {
		t.Errorf("wisp directly blocked by an issue was not backfilled as blocked")
	}
	if !getIsBlocked(t, ctx, store, "wisps", "pr4107-wisp-blocked-child") {
		t.Errorf("child wisp did not inherit blocked parent state during migration backfill")
	}
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

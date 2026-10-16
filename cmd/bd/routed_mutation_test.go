//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

// TestMaintainerAutoRouteMutationWritesThrough covers the widened
// openAutoRoutedStore behavior for write-intent callers beyond mol bond:
// resolveAndGetIssueForMutation is the resolver bd update uses, and a
// maintainer auto-routed store must open writable so the mutation actually
// commits in the routed database. (Contributor auto-routes stay read-only and
// mutation-forbidden; that side is covered by
// TestMolBondAutoRoutedReadOnlyFailsFast.)
func TestMaintainerAutoRouteMutationWritesThrough(t *testing.T) {
	initConfigForTest(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	planningDir := filepath.Join(tmpDir, "planning")

	runCmd(t, tmpDir, "git", "init", repoDir)
	// Force repo-local hooks so tests ignore any global hooksPath override.
	runCmd(t, repoDir, "git", "config", "core.hooksPath", ".git/hooks")
	runCmd(t, repoDir, "git", "config", "beads.role", "maintainer")

	primaryStore := newTestStoreIsolatedDB(t, filepath.Join(repoDir, ".beads", "beads.db"), "shared")
	if err := primaryStore.SetConfig(ctx, "routing.mode", "auto"); err != nil {
		t.Fatalf("set routing.mode: %v", err)
	}
	if err := primaryStore.SetConfig(ctx, "routing.maintainer", planningDir); err != nil {
		t.Fatalf("set routing.maintainer: %v", err)
	}

	planningStore := newTestStoreIsolatedDB(t, filepath.Join(planningDir, ".beads", "beads.db"), "shared")
	seedIssue(t, ctx, planningStore, "shared-hub")
	if err := planningStore.Close(); err != nil {
		t.Fatalf("close planning store: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repoDir: %v", err)
	}
	// Role detection reads the process-wide RepoContext cache, which an
	// earlier test in this binary may have built for its own, now-deleted
	// repo. Rebuild it from repoDir.
	resetRepoContextCachesForTest(t)

	rr, err := resolveAndGetIssueForMutation(ctx, primaryStore, "shared-hub")
	if err != nil {
		t.Fatalf("resolveAndGetIssueForMutation: %v", err)
	}
	if !rr.Routed || rr.MutationForbidden {
		rr.Close()
		t.Fatalf("routed = %v, mutationForbidden = %v; want a permitted maintainer auto-route", rr.Routed, rr.MutationForbidden)
	}
	if err := rr.Store.UpdateIssue(ctx, rr.ResolvedID, map[string]interface{}{"priority": 0}, "test"); err != nil {
		rr.Close()
		t.Fatalf("update through the maintainer auto-route: %v", err)
	}
	rr.Close()

	// Verify through a fresh routed read that the update committed in the
	// planning database, not in a doomed in-memory handle.
	reread, err := resolveAndGetIssueWithRouting(ctx, primaryStore, "shared-hub")
	if err != nil {
		t.Fatalf("re-reading the routed issue: %v", err)
	}
	defer reread.Close()
	if reread.Issue.Priority != 0 {
		t.Fatalf("priority = %d after routed update, want 0", reread.Issue.Priority)
	}
}

// TestMaintainerAutoRouteExactMutationWritesThrough is the same pin for
// resolveAndGetIssueForMutationExact, the exact-match resolver bd comment and
// bd comments add use (#5393): the maintainer auto-route must open writable,
// as any other write-intent resolution does, and a comment written through it
// must commit in the routed database. The read-only state is checked on the
// store itself because a server-mode read-only open does not refuse the write.
func TestMaintainerAutoRouteExactMutationWritesThrough(t *testing.T) {
	initConfigForTest(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	planningDir := filepath.Join(tmpDir, "planning")

	runCmd(t, tmpDir, "git", "init", repoDir)
	// Force repo-local hooks so tests ignore any global hooksPath override.
	runCmd(t, repoDir, "git", "config", "core.hooksPath", ".git/hooks")
	runCmd(t, repoDir, "git", "config", "beads.role", "maintainer")

	primaryStore := newTestStoreIsolatedDB(t, filepath.Join(repoDir, ".beads", "beads.db"), "shared")
	if err := primaryStore.SetConfig(ctx, "routing.mode", "auto"); err != nil {
		t.Fatalf("set routing.mode: %v", err)
	}
	if err := primaryStore.SetConfig(ctx, "routing.maintainer", planningDir); err != nil {
		t.Fatalf("set routing.maintainer: %v", err)
	}

	planningStore := newTestStoreIsolatedDB(t, filepath.Join(planningDir, ".beads", "beads.db"), "shared")
	seedIssue(t, ctx, planningStore, "shared-hub")
	if err := planningStore.Close(); err != nil {
		t.Fatalf("close planning store: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repoDir: %v", err)
	}
	// Role detection reads the process-wide RepoContext cache, which an
	// earlier test in this binary may have built for its own, now-deleted
	// repo. Rebuild it from repoDir.
	resetRepoContextCachesForTest(t)

	rr, err := resolveAndGetIssueForMutationExact(ctx, primaryStore, "shared-hub")
	if err != nil {
		t.Fatalf("resolveAndGetIssueForMutationExact: %v", err)
	}
	if !rr.Routed || rr.MutationForbidden {
		rr.Close()
		t.Fatalf("routed = %v, mutationForbidden = %v; want a permitted maintainer auto-route", rr.Routed, rr.MutationForbidden)
	}
	routedStore, ok := storage.UnwrapStore(rr.Store).(interface{ IsReadOnly() bool })
	if !ok {
		rr.Close()
		t.Fatalf("routed store %T does not expose read-only state", rr.Store)
	}
	if routedStore.IsReadOnly() {
		rr.Close()
		t.Fatalf("maintainer auto-route reopened read-only for an exact-match write")
	}
	if _, err := addCommentDirect(ctx, rr.Store, rr.ResolvedID, "test", "routed comment"); err != nil {
		rr.Close()
		t.Fatalf("comment through the maintainer auto-route: %v", err)
	}
	rr.Close()

	reread, err := resolveAndGetIssueWithRouting(ctx, primaryStore, "shared-hub")
	if err != nil {
		t.Fatalf("re-reading the routed issue: %v", err)
	}
	defer reread.Close()
	comments, err := reread.Store.GetIssueComments(ctx, reread.ResolvedID)
	if err != nil {
		t.Fatalf("reading routed comments: %v", err)
	}
	if len(comments) != 1 || comments[0].Text != "routed comment" {
		t.Fatalf("comments = %+v after the routed comment write, want the one written", comments)
	}
}

//go:build cgo

package embeddeddolt_test

import (
	"testing"

	"github.com/steveyegge/beads/internal/storage/depid"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/types"
)

// TestCountsIncludeWispDependencies is the regression guard for the PR #4010
// review (be-xbl83). The Count* methods that feed `bd show --json`
// (DependentCount/DependencyCount) must include `wisp_dependencies` edges,
// matching the slice-path relationship APIs (GetDependentsWithMetadata) that
// the human-readable `bd show` already uses.
//
// Before the fix, CountDependents/CountDependencies/CountDependentsByStatus
// queried only the `dependencies` table, so a permanent issue with a wisp
// dependent reported a too-small dependent_count — contradicting the
// dependents the same tool returns elsewhere.
func TestCountsIncludeWispDependencies(t *testing.T) {
	skipUnlessEmbeddedDolt(t)

	te := newTestEnv(t, "ec")
	ctx := t.Context()

	// ec-perm-dep --blocks--> ec-target   (edge in `dependencies`)
	// ec-wisp-dep --blocks--> ec-target   (edge in `wisp_dependencies`)
	for _, issue := range []*types.Issue{
		{ID: "ec-target", Title: "target", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "ec-perm-dep", Title: "perm dependent", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "ec-wisp-dep", Title: "wisp dependent", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: true},
	} {
		if err := te.store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("CreateIssue %s: %v", issue.ID, err)
		}
	}
	for _, dep := range []*types.Dependency{
		{IssueID: "ec-perm-dep", DependsOnID: "ec-target", Type: types.DepBlocks},
		{IssueID: "ec-wisp-dep", DependsOnID: "ec-target", Type: types.DepBlocks},
	} {
		if err := te.store.AddDependency(ctx, dep, "tester"); err != nil {
			t.Fatalf("AddDependency %s->%s: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}

	// Precondition: the slice path bd show's human output uses counts both.
	deps, err := te.store.GetDependentsWithMetadata(ctx, "ec-target")
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("precondition: GetDependentsWithMetadata(ec-target) = %d, want 2", len(deps))
	}

	if n, err := te.store.CountDependents(ctx, "ec-target"); err != nil {
		t.Fatalf("CountDependents: %v", err)
	} else if n != 2 {
		t.Errorf("CountDependents(ec-target) = %d, want 2 (1 perm + 1 wisp); wisp_dependencies edge dropped", n)
	}

	if n, err := te.store.CountDependentsByStatus(ctx, "ec-target", types.StatusOpen); err != nil {
		t.Fatalf("CountDependentsByStatus: %v", err)
	} else if n != 2 {
		t.Errorf("CountDependentsByStatus(ec-target, open) = %d, want 2", n)
	}

	if n, err := te.store.CountDependencies(ctx, "ec-wisp-dep"); err != nil {
		t.Fatalf("CountDependencies: %v", err)
	} else if n != 1 {
		t.Errorf("CountDependencies(ec-wisp-dep) = %d, want 1 (edge lives in wisp_dependencies)", n)
	}
}

// seedTwoChildEpic creates epic <p>-epic with durable parent-child children
// <p>-c01 and <p>-c02, so both edges land in `dependencies`.
func seedTwoChildEpic(t *testing.T, te *testEnv, p string) {
	t.Helper()
	ctx := t.Context()
	for _, issue := range []*types.Issue{
		{ID: p + "-epic", Title: "epic", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeEpic},
		{ID: p + "-c01", Title: "child 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: p + "-c02", Title: "child 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	} {
		if err := te.store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("CreateIssue %s: %v", issue.ID, err)
		}
	}
	for _, child := range []string{p + "-c01", p + "-c02"} {
		if err := te.store.AddDependency(ctx, &types.Dependency{IssueID: child, DependsOnID: p + "-epic", Type: types.DepParentChild}, "tester"); err != nil {
			t.Fatalf("AddDependency %s: %v", child, err)
		}
	}
}

// requireEpicEdgeCounts asserts CountDependents(<p>-epic) == 2 and
// CountDependencies(<p>-c01) == 1 with no error.
func requireEpicEdgeCounts(t *testing.T, te *testEnv, p string) {
	t.Helper()
	ctx := t.Context()
	if n, err := te.store.CountDependents(ctx, p+"-epic"); err != nil {
		t.Fatalf("CountDependents: %v", err)
	} else if n != 2 {
		t.Errorf("CountDependents(%s-epic) = %d, want 2 (each child once)", p, n)
	}
	if n, err := te.store.CountDependencies(ctx, p+"-c01"); err != nil {
		t.Fatalf("CountDependencies: %v", err)
	} else if n != 1 {
		t.Errorf("CountDependencies(%s-c01) = %d, want 1 (its epic once)", p, n)
	}
}

// TestCountDependentsDistinctOnMirroredWispEdge pins GH#3445's mirrored state
// on the embedded store: an edge stored in both dependency tables under one
// depid (a promoted wisp, two merged clones) counts once, matching the
// de-duplicated listing `bd show` subtracts it from.
func TestCountDependentsDistinctOnMirroredWispEdge(t *testing.T) {
	skipUnlessEmbeddedDolt(t)

	te := newTestEnv(t, "mr")
	ctx := t.Context()
	seedTwoChildEpic(t, te, "mr")

	// FOREIGN_KEY_CHECKS is session state, so the mirror rows go in on one
	// connection; the children are not wisps. The raw handle is released
	// before the store reads, which it would otherwise block.
	func() {
		db, cleanup, err := embeddeddolt.OpenSQL(ctx, te.dataDir, te.database, "main")
		if err != nil {
			t.Fatalf("OpenSQL: %v", err)
		}
		defer cleanup()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
			t.Fatalf("disable FK checks: %v", err)
		}
		for _, child := range []string{"mr-c01", "mr-c02"} {
			if _, err := conn.ExecContext(ctx,
				"INSERT INTO wisp_dependencies (id, issue_id, depends_on_issue_id, type, created_by) VALUES (?, ?, ?, 'parent-child', 'tester')",
				depid.New(child, "mr-epic"), child, "mr-epic"); err != nil {
				t.Fatalf("mirror %s: %v", child, err)
			}
		}
	}()

	requireEpicEdgeCounts(t, te, "mr")
}

// TestCountDependentsWithoutWispDependenciesTable pins GH#3445's missing-table
// state on the embedded store: the dolt-ignored wisp_dependencies can be absent
// from the working set, and the dependency counts read it as empty rather than
// fail.
func TestCountDependentsWithoutWispDependenciesTable(t *testing.T) {
	skipUnlessEmbeddedDolt(t)

	te := newTestEnv(t, "mt")
	seedTwoChildEpic(t, te, "mt")
	te.exec(t, t.Context(), "RENAME TABLE wisp_dependencies TO wisp_dependencies_renamed_away")

	requireEpicEdgeCounts(t, te, "mt")
}

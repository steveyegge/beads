package protocol

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Import / relational data tests
// ---------------------------------------------------------------------------

// TestProtocol_ImportPreservesRelationalData asserts that relational data
// (labels, dependencies, comments) set via CLI commands survives and is
// queryable via bd show --json.
//
// Invariant: create → add labels/deps/comments → show --json returns all data.
func TestProtocol_ImportPreservesRelationalData(t *testing.T) {
	w := newWorkspace(t)
	id1 := w.create("--title", "Feature with data", "--type", "feature", "--priority", "1")
	id2 := w.create("--title", "Dependency target", "--type", "task", "--priority", "2")

	w.run("label", "add", id1, "important")
	w.run("label", "add", id1, "v2")
	w.run("label", "add", id2, "backend")

	w.run("dep", "add", id1, id2) // feature depends on dep-target

	w.run("comment", id1, "Design notes for the feature")
	w.run("comment", id1, "Review feedback from team")

	// Verify via bd show --json
	featShow := w.showJSON(id1)
	depTargetShow := w.showJSON(id2)

	t.Run("labels", func(t *testing.T) {
		requireStringSetEqual(t, getStringSlice(featShow, "labels"),
			[]string{"important", "v2"}, "feature labels via show --json")

		requireStringSetEqual(t, getStringSlice(depTargetShow, "labels"),
			[]string{"backend"}, "dep-target labels via show --json")
	})

	t.Run("dependencies", func(t *testing.T) {
		wantEdges := []depEdge{{issueID: id1, dependsOnID: id2}}
		requireDepEdgesEqual(t, getObjectSlice(featShow, "dependencies"),
			wantEdges, "feature deps via show --json")
	})

	t.Run("comments", func(t *testing.T) {
		wantTexts := []string{
			"Design notes for the feature",
			"Review feedback from team",
		}
		requireCommentTextsEqual(t, getObjectSlice(featShow, "comments"),
			wantTexts, "feature comments via show --json")
	})
}

// TestProtocol_ClosedBlockerNotShownAsBlocking asserts that when all of an
// issue's blockers are closed, bd list must NOT display "(blocked by: ...)"
// for that issue.
//
// Pins down the behavior that GH#1858 reports: bd list shows resolved blockers
// as still blocking even though bd ready and bd show correctly identify them
// as resolved.
func TestProtocol_ClosedBlockerNotShownAsBlocking(t *testing.T) {
	w := newWorkspace(t)
	blocker := w.create("--title", "Blocker task", "--type", "task", "--priority", "1")
	blocked := w.create("--title", "Blocked task", "--type", "task", "--priority", "2")

	w.run("dep", "add", blocked, blocker)

	// Before closing: blocked issue should show as blocked
	t.Run("blocked_before_close", func(t *testing.T) {
		out := w.run("list", "--json")
		items := parseJSONOutput(t, out)
		blockedItem := findByID(items, blocked)
		if blockedItem == nil {
			t.Fatalf("blocked issue %s not found in list --json", blocked)
		}
		// Verify the dependency exists
		deps := getObjectSlice(blockedItem, "dependencies")
		if len(deps) == 0 {
			t.Errorf("blocked issue should have dependencies before close")
		}
	})

	// Close the blocker
	w.run("close", blocker)

	// After closing: bd ready should show the previously-blocked issue
	t.Run("ready_after_close", func(t *testing.T) {
		out := w.run("ready", "--json")
		items := parseJSONOutput(t, out)
		found := findByID(items, blocked)
		if found == nil {
			t.Errorf("issue %s should appear in bd ready after blocker %s was closed",
				blocked, blocker)
		}
	})

	// After closing: bd list text output must NOT show "(blocked by: ...)"
	t.Run("list_text_no_blocked_annotation", func(t *testing.T) {
		out := w.run("list", "--status", "open")
		if strings.Contains(out, "blocked by") {
			t.Errorf("bd list shows 'blocked by' annotation after blocker was closed (GH#1858)\noutput:\n%s", out)
		}
	})
}

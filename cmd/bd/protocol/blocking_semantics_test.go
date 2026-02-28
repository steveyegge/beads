package protocol

import (
	"strings"
	"testing"
)

// TestProtocol_ReadyOrderingIsPriorityAsc asserts that bd ready --json returns
// issues ordered by priority ascending (P0 first, then P1, P2, ..., P4).
//
// Minimal protocol: only the primary sort key (priority ASC) is enforced.
// Tie-breaking within the same priority is intentionally left unspecified
// so that future secondary-sort changes don't break this test.
//
// This pins down the behavior that GH#1880 violates: the Dolt backend returns
// ready issues in a different order than the SQLite backend.
func TestProtocol_ReadyOrderingIsPriorityAsc(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	w.create("--title", "P4 backlog", "--type", "task", "--priority", "4")
	w.create("--title", "P0 critical", "--type", "task", "--priority", "0")
	w.create("--title", "P2 medium", "--type", "task", "--priority", "2")
	w.create("--title", "P1 high", "--type", "task", "--priority", "1")
	w.create("--title", "P3 low", "--type", "task", "--priority", "3")

	out := w.run("ready", "--json")
	items := parseJSONOutput(t, out)

	// Sanity: the ready set must be non-empty given we just created 5 open tasks
	if len(items) == 0 {
		t.Fatal("bd ready --json returned 0 issues; expected at least 5 open tasks")
	}
	if len(items) != 5 {
		t.Fatalf("bd ready --json returned %d issues, want 5", len(items))
	}

	// Non-decreasing priority (the only ordering contract we enforce)
	priorities := make([]int, len(items))
	for i, m := range items {
		if p, ok := m["priority"].(float64); ok {
			priorities[i] = int(p)
		}
	}
	for i := 1; i < len(priorities); i++ {
		if priorities[i] < priorities[i-1] {
			t.Errorf("ready ordering violated: P%d appears after P%d at position %d (want priority ASC)",
				priorities[i], priorities[i-1], i)
		}
	}
}

// TestProtocol_ClosedBlockerNotShownAsBlocking asserts that when all of an
// issue's blockers are closed, bd list must NOT display "(blocked by: ...)"
// for that issue.
//
// Pins down the behavior that GH#1858 reports: bd list shows resolved blockers
// as still blocking even though bd ready and bd show correctly identify them
// as resolved.
func TestProtocol_ClosedBlockerNotShownAsBlocking(t *testing.T) {
	t.Parallel()
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

// TestProtocol_ClosingBlockerMakesDepReady asserts that closing an issue
// that blocks another causes the blocked issue to appear in bd ready.
//
// Invariant: if B depends-on A (blocks type), and A is closed,
// then B must appear in bd ready (assuming B has no other open blockers).
func TestProtocol_ClosingBlockerMakesDepReady(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	blocker := w.create("--title", "Blocker", "--type", "task", "--priority", "1")
	blocked := w.create("--title", "Blocked work", "--type", "task", "--priority", "2")
	w.run("dep", "add", blocked, blocker)

	// Before close: blocked must NOT appear in ready
	t.Run("blocked_before_close", func(t *testing.T) {
		readyIDs := parseReadyIDs(t, w)
		if readyIDs[blocked] {
			t.Errorf("issue %s should NOT be ready while blocker %s is open", blocked, blocker)
		}
	})

	w.run("close", blocker, "--reason", "done")

	// After close: blocked MUST appear in ready
	t.Run("unblocked_after_close", func(t *testing.T) {
		readyIDs := parseReadyIDs(t, w)
		if !readyIDs[blocked] {
			t.Errorf("issue %s should be ready after blocker %s was closed", blocked, blocker)
		}
	})
}

// TestProtocol_DiamondDepBlockingSemantics asserts correct behavior for
// diamond-shaped dependency graphs:
//
//	A ← B, A ← C, B ← D, C ← D
//
// When A is closed: B and C should become ready, D should stay blocked
// (still has open blockers B and C).
//
// Invariant: an issue with multiple blockers stays blocked until ALL
// blockers are resolved.
func TestProtocol_DiamondDepBlockingSemantics(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	a := w.create("--title", "Root (A)", "--type", "task", "--priority", "1")
	b := w.create("--title", "Left (B)", "--type", "task", "--priority", "2")
	c := w.create("--title", "Right (C)", "--type", "task", "--priority", "2")
	d := w.create("--title", "Join (D)", "--type", "task", "--priority", "3")

	w.run("dep", "add", b, a) // B depends on A
	w.run("dep", "add", c, a) // C depends on A
	w.run("dep", "add", d, b) // D depends on B
	w.run("dep", "add", d, c) // D depends on C

	w.run("close", a, "--reason", "done")

	readyIDs := parseReadyIDs(t, w)

	// B and C should be ready (their only blocker A is closed)
	if !readyIDs[b] {
		t.Errorf("B (%s) should be ready after A closed", b)
	}
	if !readyIDs[c] {
		t.Errorf("C (%s) should be ready after A closed", c)
	}

	// D should NOT be ready (B and C are still open)
	if readyIDs[d] {
		t.Errorf("D (%s) should NOT be ready — B and C are still open", d)
	}

	// Close B — D still blocked by C
	w.run("close", b, "--reason", "done")
	readyIDs = parseReadyIDs(t, w)
	if readyIDs[d] {
		t.Errorf("D (%s) should NOT be ready — C is still open", d)
	}

	// Close C — now D should be ready
	w.run("close", c, "--reason", "done")
	readyIDs = parseReadyIDs(t, w)
	if !readyIDs[d] {
		t.Errorf("D (%s) should be ready after both B and C are closed", d)
	}
}

// TestProtocol_TransitiveBlockingChain asserts that transitive blocking
// works correctly through a chain: A ← B ← C ← D.
//
// When A is closed: only B should become ready. C stays blocked by B,
// D stays blocked by C (transitively).
//
// Invariant: transitive dependencies are respected — closing a root
// blocker does not unblock the entire chain.
func TestProtocol_TransitiveBlockingChain(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	a := w.create("--title", "Chain-A", "--type", "task", "--priority", "1")
	b := w.create("--title", "Chain-B", "--type", "task", "--priority", "2")
	c := w.create("--title", "Chain-C", "--type", "task", "--priority", "2")
	d := w.create("--title", "Chain-D", "--type", "task", "--priority", "2")

	w.run("dep", "add", b, a)
	w.run("dep", "add", c, b)
	w.run("dep", "add", d, c)

	w.run("close", a, "--reason", "done")

	readyIDs := parseReadyIDs(t, w)

	if !readyIDs[b] {
		t.Errorf("B should be ready after A closed")
	}
	if readyIDs[c] {
		t.Errorf("C should NOT be ready — B is still open")
	}
	if readyIDs[d] {
		t.Errorf("D should NOT be ready — C (and transitively B) still open")
	}
}

// TestProtocol_DeferredExcludedFromReady asserts that deferred issues are
// NOT in bd ready, and undefer brings them back.
//
// Invariant: deferred issues must not appear in bd ready output.
func TestProtocol_DeferredExcludedFromReady(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	a := w.create("--title", "Defer target", "--type", "task", "--priority", "2")

	// Should be ready initially
	readyIDs := parseReadyIDs(t, w)
	if !readyIDs[a] {
		t.Fatalf("issue %s should be ready initially", a)
	}

	// Defer until far future
	w.run("defer", a, "--until", "2099-12-31")

	// Should NOT be ready while deferred
	readyIDs = parseReadyIDs(t, w)
	if readyIDs[a] {
		t.Errorf("deferred issue %s should NOT appear in bd ready", a)
	}

	// Undefer
	w.run("undefer", a)

	// Should be ready again
	readyIDs = parseReadyIDs(t, w)
	if !readyIDs[a] {
		t.Errorf("issue %s should be ready after undefer", a)
	}
}

// TestProtocol_DepRmUnblocksIssue asserts that removing a blocking dependency
// makes the previously-blocked issue appear in bd ready.
//
// Invariant: after dep rm, issue with no remaining blockers is ready.
func TestProtocol_DepRmUnblocksIssue(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	a := w.create("--title", "Blocked", "--type", "task", "--priority", "2")
	b := w.create("--title", "Blocker", "--type", "task", "--priority", "2")

	w.run("dep", "add", a, b)

	// A should not be ready (blocked)
	readyIDs := parseReadyIDs(t, w)
	if readyIDs[a] {
		t.Fatalf("issue %s should NOT be ready while blocked", a)
	}

	// Remove the dep
	w.run("dep", "rm", a, b)

	// A should now be ready
	readyIDs = parseReadyIDs(t, w)
	if !readyIDs[a] {
		t.Errorf("issue %s should be ready after dep rm", a)
	}
}

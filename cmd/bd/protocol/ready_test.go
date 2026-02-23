package protocol

import "testing"

// ---------------------------------------------------------------------------
// Ready semantics: ordering and blocking
// ---------------------------------------------------------------------------

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

// TestProtocol_ClosingBlockerMakesDepReady asserts that closing an issue
// that blocks another causes the blocked issue to appear in bd ready.
//
// Invariant: if B depends-on A (blocks type), and A is closed,
// then B must appear in bd ready (assuming B has no other open blockers).
func TestProtocol_ClosingBlockerMakesDepReady(t *testing.T) {
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

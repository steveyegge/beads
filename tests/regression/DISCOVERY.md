# Regression Discovery Log

Systematic bug hunting and protocol test ideas from manual testing
against current main (Dolt server mode) compared to v0.49.6 baseline.

Date: 2026-02-22

---

## CONFIRMED BUGS

### BUG-1: `bd export` command removed from main

**Severity: HIGH** — Breaks entire regression test suite
**Affected:** `tests/regression/` — all 85 tests rely on `compareExports()` → `bd export`

The `bd export` command was removed during the JSONL→Dolt-native refactor
(commit 1e1568fa). The regression test framework calls `w.export()` which
runs `bd export` — this fails with "unknown command" on the candidate binary.

**Impact:** No differential regression testing is possible until either:
- `bd export` is restored (even as a read-only dump)
- The test harness is rewritten to use `bd show --json` / `bd list --json --all -n 0`
- A new `bd dump` or `bd export-jsonl` command is added

**Fix proposal:** Add a `bd dump` command that produces JSONL-per-issue output
(same schema as old `bd export`) for debugging and testing. Alternatively,
adapt the regression harness to use `bd list --all -n 0 --json` + `bd show <id> --json`
for each issue, but this requires restructuring the normalization pipeline.

---

### BUG-2: `dep tree` shows no children — ParentID never set (GH#1954)

**Severity: HIGH** — Core feature completely broken
**File:** `internal/storage/dolt/dependencies.go:646-649`
**Root cause:** `buildDependencyTree()` creates `TreeNode` without setting `ParentID`:

```go
node := &types.TreeNode{
    Issue: *issue,
    Depth: depth,  // ← Depth is set correctly
    // ParentID is NEVER set ← BUG
}
```

The `renderTree()` function at `cmd/bd/dep.go:721-729` builds a children map
keyed by `ParentID`. Since `ParentID` is always empty, all children go into
`children[""]` instead of `children[rootID]`. Root's children lookup returns empty.

**Fix:** Pass parent ID into recursive `buildDependencyTree` and set `node.ParentID`:

```go
func (s *DoltStore) buildDependencyTree(ctx context.Context, issueID string,
    depth, maxDepth int, reverse bool, visited map[string]bool,
    parentID string) ([]*types.TreeNode, error) {
    // ...
    node := &types.TreeNode{
        Issue:    *issue,
        Depth:    depth,
        ParentID: parentID,  // ← FIX
    }
    // ...
    for _, childID := range childIDs {
        children, err := s.buildDependencyTree(ctx, childID, depth+1,
            maxDepth, reverse, visited, issueID)  // ← pass issueID as parent
```

---

### BUG-3: `dep tree` shows `[READY]` for blocked root issue

**Severity: MEDIUM**
**File:** `cmd/bd/dep.go:835`

```go
if node.Status == types.StatusOpen && node.Depth == 0 {
    line += " " + ui.PassStyle.Bold(true).Render("[READY]")
}
```

The ready check only looks at `status == open && depth == 0`. It doesn't check
whether the issue has open blocking dependencies. A blocked issue at depth 0
(the root of a "down" tree) shows `[READY]` when it should show `[BLOCKED]`.

**Fix:** Check for open blocking dependencies before showing `[READY]`. Either
query the store or compute from the tree data.

---

### BUG-4: `list --status blocked` and `count --status blocked` return empty

**Severity: MEDIUM** — Documented status value doesn't work
**Affects:** `bd list --status blocked`, `bd count --status blocked`, `bd query "status=blocked"`

The help text for `list` says: `--status string  Filter by status (open, in_progress, blocked, deferred, closed)`

But "blocked" is a computed status derived from dependency relationships, never
stored in the `issues.status` column (which stays "open"). So:
- `bd blocked` → 4 issues ✓
- `bd list --status blocked` → 0 issues ✗
- `bd count --status blocked` → 0 ✗

**Fix options:**
1. Materialize blocked status: When a blocking dep is added, update status to "blocked"
2. Compute on query: In the list/count SQL, join with dependencies to detect blocked
3. Remove "blocked" from the documented status values and point users to `bd blocked`

---

### BUG-5: Concurrent label operations produce race conditions

**Severity: MEDIUM** — Data loss under concurrency
**Reproduction:**

```bash
# Parallel adds — expect 5 labels, get 0
for i in 1 2 3 4 5; do
  bd label add <id> "stress-$i" &
done
wait
bd show <id> --json  # labels: []
```

Sequential label adds work perfectly (5/5). Parallel adds produce 0 labels
visible immediately. After subsequent operations, some labels eventually appear.

**Root cause:** Likely a lost-update race in the Dolt server. Each concurrent
`label add` reads the current label set, adds its label, writes back. If two
writers read the same state, the last writer wins and the other's label is lost.

**Fix:** Use row-level INSERT into a labels junction table instead of
read-modify-write on a labels array/column. Or use SELECT FOR UPDATE / SERIALIZABLE
transactions.

---

### BUG-6: Workspace data isolation with shared Dolt server

**Severity: LOW for end users, HIGH for test infrastructure**

All `bd init --prefix test` workspaces on the same Dolt server (127.0.0.1:3307)
share the same `beads_test` database. Issues created in one workspace are visible
from any other workspace with the same prefix.

This is by design for collaborative use, but it breaks the regression test
harness which creates isolated workspaces with `newWorkspace(t, bdPath)`. Each
test's workspace shares the database, causing cross-test contamination.

**Fix for tests:** Use unique prefixes per test (e.g., `test-<random>`) or
create a fresh Dolt database per test workspace.

---

### BUG-7: `dep add` silently overwrites when changing dep type on same pair

**Severity: HIGH** — Silent data loss of blocking relationships
**Reproduction:**

```bash
bd dep add A B --type blocks    # ✓ Added dependency
bd dep add A B --type caused-by # ✓ Added dependency  (SILENTLY REPLACES blocks)
# DB now only has caused-by — blocks relationship is LOST
# A is no longer blocked!
```

The `dependencies` table has a unique constraint on `(issue_id, depends_on_id)`
without including `type`. Adding a second dep type on the same pair does an
upsert, replacing the existing type. Both operations report success.

**Impact:** A user who adds `caused-by` to an already-blocked pair silently
removes the blocking relationship. The issue becomes unblocked without warning.

**Fix:** Either:
1. Make the unique key `(issue_id, depends_on_id, type)` to allow multiple dep types
2. Reject the second `dep add` with an error: "dependency already exists with type X"
3. Warn the user: "changing dep type from X to Y"

---

### BUG-8: Reparented child appears under BOTH old and new parent

**Severity: MEDIUM** — Confusing behavior after reparenting
**File:** `internal/storage/dolt/queries.go:211`
**Root cause:** Parent filter uses `OR id LIKE CONCAT(?, '.%')` in addition to
dependency lookup. After `bd create --title X --parent P1` creates `P1.1`,
reparenting with `bd update P1.1 --parent P2` correctly updates the
parent-child dep to P2, but the ID `P1.1` still matches `P1.%` via LIKE.

```sql
(id IN (SELECT issue_id FROM dependencies WHERE type = 'parent-child' AND depends_on_id = ?)
 OR id LIKE CONCAT(?, '.%'))
```

**Impact:** `bd children P1` shows `P1.1` even after reparenting to P2.
`bd children P2` also correctly shows it. The child appears under BOTH parents.

**Fix options:**
1. After reparent, rename the issue ID to match new parent (e.g., `P1.1` → `P2.1`)
2. Remove the LIKE clause from parent filtering (rely solely on dependency table)
3. Add EXCEPT clause: `AND id NOT IN (SELECT issue_id FROM dependencies WHERE type = 'parent-child' AND depends_on_id != ?)`

---

### BUG-9: `list --ready` includes blocked issues (documented but confusing)

**Severity: LOW** (documented in help text)
**File:** `bd list --ready` help says "Note: 'bd list --ready' is NOT equivalent"

`bd list --ready -n 0` returns 34 issues including blocked ones.
`bd ready -n 0` returns 29 truly ready issues (excludes blocked).

The discrepancy of 5 issues = exactly the issues with open `blocks` dependencies.
The help text documents this, but the `--ready` flag name is misleading.

---

### BUG-10: Commands exit 0 on soft failures (close guard, claim, etc.)

**Severity: MEDIUM** — Breaks scripting and automation
**Affects:** `bd close` (close guard), `bd update --claim` (already claimed), likely others
**Files:** `cmd/bd/close.go:117`, `cmd/bd/update.go:278`

When close guard prevents closing a blocked issue, the command prints a message
to stderr ("cannot close X: blocked by open issues") but exits with code 0.
Similarly, `update --claim` on an already-claimed issue prints "already claimed"
to stderr but exits 0.

The pattern is: `fmt.Fprintf(os.Stderr, ...) + continue` in a loop, with no
tracking of whether any operations actually succeeded. When the loop finishes,
the command exits 0 regardless.

**Impact:** Scripts and CI/CD pipelines cannot detect these failures via exit code.
They must parse stderr text instead, which is fragile.

**Fix:** Track `errorCount` and call `os.Exit(1)` if `errorCount > 0` and
`closedCount == 0` at end of the command.

---

### BUG-11: `bd update --status` accepts arbitrary values

**Severity: MEDIUM** — Data integrity issue
**File:** `cmd/bd/update.go`

`bd update X --status "bogus"` succeeds and stores "bogus" as the status.
Valid statuses should be: open, in_progress, closed, deferred.
The `--type` flag correctly validates against a whitelist, but `--status` does not.

**Impact:** Invalid statuses are stored in the DB. Issues with invalid status
won't appear in any filtered list (they're not open, not closed, not deferred).

**Fix:** Add status validation in update command, same pattern as type validation.

---

### BUG-12: `bd update --title ""` accepts empty title

**Severity: LOW** — Data quality issue
**File:** `cmd/bd/update.go`

`bd create --title ""` correctly fails with "title required".
`bd update X --title ""` succeeds and stores an empty title.
Validation is inconsistent between create and update.

**Fix:** Add empty-title check in update command.

---

### BUG-13: Reopen of closed+deferred issue creates limbo state

**Severity: MEDIUM** — Issue becomes invisible
**Reproduction:**

```bash
bd defer X --until 2099-12-31   # status=deferred
bd close X                      # status=closed, defer_until preserved
bd reopen X                     # status=open, defer_until STILL SET
```

After reopening, the issue has status "open" but defer_until is still set.
- Not in `bd ready` (excluded by defer_until check) ✓
- Not in `bd list --status deferred` (status is "open", not "deferred") ✗
- Appears in `bd list --status open` but won't show in ready ✗

The issue is effectively invisible to normal workflows.

**Fix options:**
1. `reopen` should clear defer_until when setting status to "open"
2. `reopen` should restore "deferred" status if defer_until is still in the future
3. `close` should clear defer_until when closing a deferred issue

---

### BUG-14: `bd label add` accepts empty string label

**Severity: LOW** — Data quality issue

`bd label add X ""` succeeds and stores an empty string as a label.
This creates invisible/confusing entries in the label list.

**Fix:** Validate label is non-empty before inserting.

---

## MINOR ISSUES / OBSERVATIONS

### OBS-1: `bd supersede` and `bd duplicate` don't set close_reason

When `bd supersede X --with Y` or `bd duplicate X --of Y` closes issue X,
the `close_reason` field is empty. The relationship is tracked via a
`supersedes`/`duplicate-of` dependency, but there's no close_reason like
"superseded" or "duplicate" set on the issue. Users querying closed issues
by reason would miss these.

### OBS-2: `count --by-status` doesn't show "blocked" count

`count --by-status` shows only "open" and "closed" (and "in_progress",
"deferred" when applicable). Issues with open blocking dependencies show as
"open", not "blocked". This is consistent with BUG-4 but may confuse users.

### OBS-3: `bd sql` allows arbitrary writes (no safety check)

`bd sql "UPDATE issues SET title = 'X'"` succeeds without warning. Only
`--readonly` flag prevents it (but blocks ALL sql, even reads). There's no
write-specific safety prompt or `--force` requirement for mutating SQL.

### OBS-4: `bd label rm` is not a recognized alias for `bd label remove`

Running `bd label rm <id> <label>` shows the `bd label` help text instead of
an error message. Users might expect `rm` as a common alias. The `bd delete`
command uses `--force` not `--yes`.

### OBS-3: `bd label add` syntax is `[issue-id...] [label]` (last arg = label)

The syntax treats all args except the last as issue IDs and the last as the
label. This means you can label multiple issues at once, but only one label
at a time. This is correct but potentially confusing — `bd label add id lab1 lab2`
adds label "lab2" to issues "id" and "lab1".

---

## PROTOCOL TEST IDEAS

These should be ported to `cmd/bd/protocol/protocol_test.go` as invariant checks.

### PT-1: Close guard respects dep types

```
GIVEN issue A with caused-by dep on open issue B
WHEN close A
THEN close succeeds (caused-by is non-blocking)

GIVEN issue C with blocks dep on open issue D
WHEN close C
THEN close is rejected with suggestion to use --force
```

Already tested manually — works correctly. Good protocol invariant to formalize.

### PT-2: Epic lifecycle — children don't auto-close parent

```
GIVEN epic E with children C1, C2
WHEN close C1, close C2 (all children closed)
THEN E remains open
AND E appears in bd ready output
WHEN close E
THEN E is closed
```

Works correctly. Good invariant.

### PT-3: Delete cleans up dependency links

```
GIVEN A depends on B (blocks)
WHEN delete B --force
THEN A has no dependencies
AND A appears in bd ready output
```

Works correctly. Good invariant.

### PT-4: Reopen preserves dependencies

```
GIVEN A depends on B (caused-by)
WHEN close A, then reopen A
THEN A still has dep on B
```

Works correctly. Good invariant.

### PT-5: `dep tree` shows full tree (BLOCKED by BUG-2)

```
GIVEN diamond dependency: A→B, A→C, B→D, C→D
WHEN dep tree A
THEN output shows all 4 nodes at correct depths
AND D appears twice (or once with "shown above" marker)
```

Currently broken — only root shows. Needs BUG-2 fix first.

### PT-6: Ready semantics exclude blocked issues

```
GIVEN A→B (blocks), A→C (blocks), D (no deps)
WHEN bd ready
THEN A is NOT in ready list (blocked by B and C)
AND B is in ready list (no blockers)
AND C is in ready list (no blockers)
AND D is in ready list
```

Works correctly. Good invariant.

### PT-7: Deferred issues excluded from ready

```
GIVEN A deferred until 2099-12-31
WHEN bd ready
THEN A is NOT in ready list
WHEN undefer A
THEN A IS in ready list
```

Works correctly. Good invariant.

### PT-8: Concurrent create is safe

```
WHEN 10 parallel bd create commands
THEN all 10 issues exist with unique IDs
AND count matches expected total
```

Works correctly. Good invariant.

### PT-9: Concurrent label add is NOT safe (documents BUG-5)

```
WHEN 5 parallel bd label add <id> "label-N"
THEN only 0-4 labels survive (lost update race)
```

This would be a regression test to verify when the fix lands.

### PT-10: `list --status blocked` should match `blocked` output

```
GIVEN A→B (blocks), both open
THEN bd list --status blocked should include A
AND bd blocked should include A
AND counts should match
```

Currently fails — documents BUG-4.

### PT-11: Status transitions round-trip

```
open → in_progress → open → closed → open (via update)
open → deferred → open (via defer/undefer)
All transitions preserve issue data (deps, labels, comments)
```

Works correctly.

### PT-12: Notes append vs overwrite

```
GIVEN issue with notes "Original"
WHEN update --notes "Replaced"
THEN notes = "Replaced" (overwrite)
WHEN update --append-notes "Extra"
THEN notes = "Replaced\nExtra" (append with newline)
```

Works correctly.

### PT-13: Special characters in fields

```
GIVEN bd create --title 'Test "quotes" & <brackets>'
THEN show --json correctly escapes and preserves the title
```

Works correctly.

### PT-14: Export command existence (BLOCKED by BUG-1)

```
WHEN bd export
THEN command exists and produces JSONL output
```

Currently fails — export removed from main.

### PT-15: Supersede creates dependency and closes issue

```
GIVEN issue A and B
WHEN bd supersede A --with B
THEN A is closed
AND A has supersedes dependency on B
```

Works correctly (though close_reason is empty — see OBS-1).

### PT-16: Duplicate marks issue as closed with dependency

```
GIVEN issue A and B
WHEN bd duplicate B --of A
THEN B is closed
AND B has duplicate-of dependency on A
```

Works correctly (though close_reason is empty — see OBS-1).

### PT-17: Type change round-trip

```
GIVEN task T
WHEN update T --type bug, then update T --type epic
THEN type=epic
```

Works correctly.

### PT-18: Transitive blocking chain

```
GIVEN A→B→C→D (all blocks)
THEN only D is ready, A/B/C are blocked
WHEN close D: only C becomes ready
WHEN close C: only B becomes ready
WHEN close B: only A becomes ready
```

Works correctly. Good chain-invariant test.

### PT-19: Circular dependency prevention

```
GIVEN A→B→C (blocks)
WHEN dep add C→A (blocks)
THEN error "would create a cycle"
AND the dependency is NOT added
AND dep cycles shows no cycles
```

Works correctly. Critical invariant.

### PT-20: Close --force overrides close guard

```
GIVEN A→B (blocks), B is open
WHEN close A (no force)
THEN rejected
WHEN close A --force
THEN A is closed
```

Works correctly.

### PT-21: Claim semantics (atomic)

```
WHEN update X --claim
THEN X.status = in_progress, X.assignee = current user
WHEN update X --claim (again)
THEN error "already claimed"
```

Works correctly.

### PT-22: Create with --parent creates dotted ID

```
WHEN create --title "Child" --parent P
THEN child ID is P.N (e.g., P.1)
AND children P shows the child
AND child has parent-child dep on P
```

Works correctly.

### PT-23: Create with --deps creates blocks dependency

```
WHEN create --title "X" --deps B
THEN X has blocks dep on B
AND X is in blocked list
```

Works correctly.

### PT-24: count --by-status, --by-type, --by-priority grouping

```
GIVEN mixed issues with various statuses, types, priorities
THEN count --by-status groups correctly
AND count --by-type groups correctly
AND count --by-priority groups correctly
AND totals match count without filter
```

Works correctly.

### PT-25: Due date and defer round-trip

```
GIVEN issue I
WHEN update I --due "2099-06-15"
THEN show --json has due_at with 2099-06-15 date
WHEN defer I --until 2099-12-31
THEN status=deferred, defer_until has 2099-12-31 date
```

Works correctly.

### PT-26: dep rm unblocks issue

```
GIVEN A→B (blocks)
WHEN dep rm A B
THEN A is in ready list
AND A is NOT in blocked list
```

Works correctly.

### PT-27: Self-dependency prevention

```
WHEN dep add A A --type blocks
THEN error "would create a cycle"
```

Works correctly (caught by cycle detection).

### PT-28: Create with --deps creates blocking dep

```
GIVEN issue B
WHEN create --title "X" --deps B
THEN X is blocked by B
AND B is in ready list
AND X is NOT in ready list
```

Works correctly.

### PT-29: Label add/remove round-trip

```
GIVEN issue I with no labels
WHEN label add I "bug-fix"
WHEN label add I "urgent"
THEN I has 2 labels
WHEN label remove I "bug-fix"
THEN I has 1 label ("urgent")
```

Works correctly.

### PT-30: Comments preserved through close/reopen

```
GIVEN issue I with 2 comments
WHEN close I, reopen I
THEN I still has 2 comments
```

Works correctly.

### PT-31: Due date round-trip

```
GIVEN issue I
WHEN update I --due "2099-06-15"
THEN show --json has due_at containing "2099-06-15"
```

Works correctly.

### PT-32: Status transition round-trip

```
open → in_progress → open → closed → open (reopen)
All transitions work, data preserved at each step
```

Works correctly.

---

## TEST INFRASTRUCTURE NOTES

### Regression harness needs adaptation for Dolt-only main

The current regression test harness (`regression_test.go`) is designed around:
1. `bd export` producing JSONL
2. SQLite-based baseline binary (v0.49.6) that doesn't need a server
3. Isolated workspaces (each test gets a fresh `.beads/` dir)

On current main:
- `bd export` doesn't exist (BUG-1)
- Candidate binary requires a running Dolt server
- All workspaces with same prefix share the same Dolt database (BUG-6)

To fix the harness:
1. Replace `w.export()` with `w.run("list", "--all", "-n", "0", "--json")`
   combined with `w.run("show", id, "--json")` per issue for full data
2. The baseline binary still works with SQLite (no server needed)
3. Use unique prefixes per test: `test-<testname>-<random>`
4. Or spin up a separate Dolt server per test on a random port

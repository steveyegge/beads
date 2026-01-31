# Spec: Fix PR #1372 - spec_id Field for Beads

**PR**: https://github.com/steveyegge/beads/pull/1372
**Status**: Changes Requested
**Reviewer**: Steve Yegge (repo owner)
**Date**: 2026-01-30

---

## What You Built

A feature that lets beads issues link directly to specification documents.

### The Problem It Solves

Before: Users manually embed doc references in descriptions
```bash
bd create "Implement auth" --desc "See docs/plans/auth-design.md for details"
```

After: First-class `spec_id` field with filtering
```bash
bd create "Implement auth" --spec-id "docs/plans/auth-design.md"
bd list --spec "docs/plans/"    # Find all issues linked to specs
bd show bd-xxx                  # Shows: Spec: docs/plans/auth-design.md
```

### Files Changed (20 total)

| Category | Files |
|----------|-------|
| Types | `internal/types/types.go` |
| Migration | `internal/storage/sqlite/migrations/041_spec_id_column.go` |
| Schema | `internal/storage/sqlite/schema.go` |
| Queries | `internal/storage/sqlite/queries.go`, `transaction.go`, `issues.go` |
| Commands | `cmd/bd/create.go`, `update.go`, `list.go`, `show.go` |
| Tests | `show_test.go`, `list_filters_test.go`, `041_spec_id_column_test.go` |
| Docs | `docs/CLI_REFERENCE.md` |
| Other | `protocol.go`, `memory.go`, `dependencies.go`, `ready.go`, `labels.go` |

---

## Why It Failed

**Root Cause**: Bad rebase. When resolving merge conflicts, lines were deleted that shouldn't have been, and some additions didn't land properly.

### Critical Bug #1: SQL Syntax Error

**Location**: `queries.go` and `transaction.go`

```sql
-- What it should be:
UPDATE issues SET ... closed_by_session = ?

-- What the PR has:
UPDATE issues SET ... closed_by_session = ? = NULL
```

**Impact**: Breaks ALL issue closing. `= ? = NULL` is invalid SQL.

---

### Critical Bug #2: Scan/SELECT Column Mismatches

**Location**: Multiple query functions

When you SELECT columns, you must have matching `Scan()` arguments. The PR removed scan targets without adding replacements.

**GetIssue** (`queries.go`):
```go
// SELECT has: due_at, defer_until, spec_id (3 columns)
// Scan has: nothing for these (0 targets)
// Result: PANIC at runtime
```

**GetIssueByExternalRef** (`queries.go`):
```go
// SELECT adds spec_id but removes: &awaitType, &awaitID, &timeoutNs, &waiters
// 5 columns with 0 scan targets = PANIC
```

**scanIssueRow** (`transaction.go`):
```go
// Same pattern - removed &dueAt, &deferUntil but never added &specID
```

---

### Critical Bug #3: INSERT Placeholder Mismatches

**Location**: `insertIssue`, `insertIssueStrict`, `insertIssues`, `insertIssuesStrict`

```go
// Column list: adds spec_id (+1 column)
// Placeholders: goes from 40 to 42 (+2 placeholders)
// Values: DELETES issue.DueAt, issue.DeferUntil, never adds issue.SpecID
// Result: 42 placeholders with ~38 values = WON'T COMPILE
```

---

### Critical Bug #4: Non-Existent Column Scanned

**Location**: `dependencies.go` - `scanIssues` and `scanIssuesWithDependencyType`

```go
var specChangedAt sql.NullTime  // Declared
// ...
&specChangedAt,                  // Scanned
```

**Problem**:
- SELECT doesn't include `spec_changed_at`
- Migration doesn't create `spec_changed_at` column
- Schema doesn't define it

**Impact**: Panics on every dependency query.

---

### Medium Bug #5: Undeclared Struct Fields in Tests

**Location**: `list_filters_test.go`

```go
args.SpecChanged     // NOT defined on ListArgs
filter.SpecChanged   // NOT defined on IssueFilter
```

**Impact**: Won't compile.

---

### Medium Bug #6: Phantom Features Documented

**Location**: `CLI_REFERENCE.md`

Documented but not implemented:
- `bd spec scan`
- `bd spec list`
- `bd spec show`
- `bd spec coverage`
- `--spec-changed` flag on `bd list`

No `spec.go` command file exists. `--spec-changed` not registered in `list.go init()`.

---

### Minor Issue #7: Excessive Whitespace Reformatting

Large portions of `protocol.go` and `types.go` diffs are just alignment changes to unrelated fields. Creates merge conflicts, obscures real changes.

---

## Workflow: Fix Without New PR

**Two Approaches:**

### Approach A: Use Existing specbeads Directory

**Setup:**
- `origin` = your fork (anupamchugh/shadowbook)
- `upstream` = main repo (steveyegge/beads)
- Already configured in `/specbeads`

**Pros:**
- ✓ Faster (already set up)
- ✓ Less disk space
- ✓ Fewer steps

**Cons:**
- ✗ Directory has local specs/reference files (not part of beads repo)
- ✗ Risk of accidentally committing local files
- ✗ Confusing: your work mixed with specbeads directory

**Risk Level**: Medium (needs careful `git status` checks)

---

### Approach B: Fresh Clone of Beads Repo

**Setup:**
```bash
cd /tmp
git clone https://github.com/steveyegge/beads.git beads-clean
cd beads-clean
git remote add origin https://github.com/anupamchugh/shadowbook.git
```

**Pros:**
- ✓ Clean environment (zero local files)
- ✓ No risk of committing wrong files
- ✓ Clear separation of concerns
- ✓ Professional/safe approach

**Cons:**
- ✗ Takes 30 seconds longer
- ✗ Slight disk space overhead
- ✗ One extra setup step

**Risk Level**: None (clean slate)

---

## CHOSEN APPROACH: B (Fresh Clone)

**Why**: Clean, safe, professional. Zero risk of contaminating PR with local files.

---

## Workflow: Fix Without New PR

**The Fix**:
1. Clone fresh beads repo
2. Set up remotes (origin = your fork, upstream = main repo)
3. Create fresh branch from upstream/main
4. Apply changes correctly
5. Force-push to your origin (updates existing PR #1372 automatically)
6. Drop a comment on the same PR explaining the fix

**No new PR needed. Same PR, cleaner code.**

---

## Next Steps: Clean Implementation

### Phase 0: Clone Fresh Beads Repo

```bash
# Clone the main beads repo to a clean directory
git clone https://github.com/steveyegge/beads.git /tmp/beads-clean
cd /tmp/beads-clean

# Add your fork as origin
git remote add origin https://github.com/anupamchugh/shadowbook.git

# Verify remotes
git remote -v
# Should show:
# origin    https://github.com/anupamchugh/shadowbook.git (fetch/push)
# upstream  https://github.com/steveyegge/beads.git (fetch/push)
```

### Phase 1: Setup Fresh Branch

```bash
# 1. Fetch latest upstream
git fetch upstream

# 2. Create fresh branch from upstream/main
git checkout -b feature/spec-id-v2 upstream/main
```

### Phase 2: Implement Core Changes (Minimal)

Only touch what's necessary for the feature:

#### 2.1 Add SpecID to Issue Type

**File**: `internal/types/types.go`

```go
type Issue struct {
    // ... existing fields ...
    SpecID string `json:"spec_id,omitempty"`  // ADD THIS
}
```

#### 2.2 Create Migration

**File**: `internal/storage/sqlite/migrations/041_spec_id_column.go`

```go
package migrations

import "database/sql"

func init() {
    Register(41, migration041Up, migration041Down)
}

func migration041Up(tx *sql.Tx) error {
    _, err := tx.Exec(`ALTER TABLE issues ADD COLUMN spec_id TEXT`)
    if err != nil {
        return err
    }
    _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_spec_id ON issues(spec_id)`)
    return err
}

func migration041Down(tx *sql.Tx) error {
    _, err := tx.Exec(`DROP INDEX IF EXISTS idx_issues_spec_id`)
    if err != nil {
        return err
    }
    _, err = tx.Exec(`ALTER TABLE issues DROP COLUMN spec_id`)
    return err
}
```

#### 2.3 Update Schema

**File**: `internal/storage/sqlite/schema.go`

Add `spec_id TEXT` to the issues table definition.

#### 2.4 Update Queries (CAREFULLY)

**Files**: `queries.go`, `transaction.go`

For each query:
1. Add `spec_id` to SELECT columns
2. Add `&issue.SpecID` to Scan() in the SAME position
3. Add `spec_id` to INSERT columns
4. Add `issue.SpecID` to INSERT values
5. **DO NOT** remove any existing columns or scan targets

#### 2.5 Add CLI Flags

**File**: `cmd/bd/create.go`
```go
// In init():
createCmd.Flags().String("spec-id", "", "Link to specification document")

// In run():
if specID, _ := cmd.Flags().GetString("spec-id"); specID != "" {
    issue.SpecID = specID
}
```

**File**: `cmd/bd/update.go`
```go
// Same pattern as create
```

**File**: `cmd/bd/list.go`
```go
// Add --spec filter flag
listCmd.Flags().String("spec", "", "Filter by spec_id prefix")

// In run(), add to filter logic:
if spec, _ := cmd.Flags().GetString("spec"); spec != "" {
    // Filter issues where SpecID starts with spec
}
```

**File**: `cmd/bd/show.go`
```go
// In display logic:
if issue.SpecID != "" {
    fmt.Printf("Spec: %s\n", issue.SpecID)
}
```

### Phase 3: Test

```bash
# Must pass before pushing
go build ./...
go test ./...

# Manual smoke test
bd create "Test issue" --spec-id "specs/test.md"
bd list --spec "specs/"
bd show <issue-id>
```

### Phase 4: Push and Comment on PR

```bash
# Find your current feature branch name
git branch

# Force push to your origin fork with the branch name that PR #1372 uses
# (likely "feature/spec-id")
git push origin feature/spec-id-v2:feature/spec-id --force
```

**The PR updates automatically** with the new clean code.

Then **comment on PR #1372** with:
```
Fixed the rebase issues. Created fresh branch from upstream/main and applied changes carefully:
- Fixed SQL syntax errors
- Matched all SELECT/Scan column counts
- Matched all INSERT placeholder/value counts  
- Removed phantom column references
- Removed phantom command documentation

All tests passing, ready for review.
```

---

## Checklist Before Resubmitting

### PR Content Verification
- [ ] **Files IN the PR** (code files only):
  - `internal/types/types.go` — Add SpecID field
  - `internal/storage/sqlite/migrations/041_spec_id_column.go` — Database migration
  - `internal/storage/sqlite/schema.go` — Update schema
  - `internal/storage/sqlite/queries.go` — SELECT/INSERT queries with spec_id
  - `internal/storage/sqlite/transaction.go` — Transaction functions
  - `cmd/bd/create.go` — Add --spec-id flag
  - `cmd/bd/update.go` — Add --spec-id flag
  - `cmd/bd/list.go` — Add --spec filter flag
  - `cmd/bd/show.go` — Display spec_id output
  - Test files for the feature

- [ ] **Files NOT in the PR** (verify they're not committed):
  - `specs/SPEC_ID_PR_FIX.md` ✗ (local documentation, not part of beads)
  - Any other local files ✗

### Code Quality
- [ ] Fresh branch from `upstream/main`
- [ ] Only `spec_id` changes (no whitespace reformatting)
- [ ] Every SELECT has matching Scan() arguments
- [ ] Every INSERT has matching column/placeholder/value counts
- [ ] No references to `spec_changed_at` (doesn't exist)
- [ ] No undeclared struct fields in tests
- [ ] No phantom commands in docs (remove `bd spec *` docs)

### Testing
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] Manual smoke test works:
  ```bash
  bd create "Test issue" --spec-id "specs/test.md"
  bd list --spec "specs/"
  bd show <issue-id>  # Verify Spec: specs/test.md displays
  ```

---

## Summary

| What | Status |
|------|--------|
| Feature idea | Approved |
| Implementation | Broken (bad rebase) |
| Fix approach | Start fresh, apply minimal changes |
| Effort | ~1-2 hours if careful |

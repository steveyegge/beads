# SpecID Feature Specification

**Status:** Phase 1 Complete (MVP), Phase 2 Spec Sync Planned
**Author:** Claude
**Created:** 2026-01-28

## Overview

SpecID links beads (issues) to specification documents, enabling bidirectional navigation between implementation tasks and their design specs.

This document now serves as the authoritative **status + next-step spec** for SpecBeads.

## Motivation

When working on large codebases with extensive specs (e.g., `specs/` folder with 400+ markdown files), agents and humans need to:

1. Find which issues relate to a given spec
2. See which spec an issue implements
3. Track spec coverage (which specs have work, which don't)
4. Navigate from issue → spec → related issues

## Design

### Data Model

Add `spec_id` field to Issue:

```go
type Issue struct {
    // ... existing fields ...
    SpecID string `json:"spec_id,omitempty"` // Reference to spec file
}
```

**Format:** Flexible string, typically:
- File path: `specs/auth/login.md`
- Spec identifier: `SPEC-001`
- URL: `https://docs.example.com/spec/auth`

### Storage

| Layer | Change | Status |
|-------|--------|--------|
| types.go | Add SpecID field | ✅ Done |
| schema.go | Add spec_id column + index | ✅ Done |
| queries.go | Add to allowedUpdateFields | ✅ Done |
| issues.go | Add to INSERT statements | ✅ Done |
| transaction.go | Add to scanIssueRow + SELECT | ✅ Done |
| Migration 041 | Add column to existing DBs | ✅ Done |
| sqlite/dolt/memory | Read/write/filter parity | ✅ Done |

### CLI Interface

#### Create with SpecID

```bash
bd create --title "Implement login flow" --spec-id "specs/auth/login.md"
bd create "Fix auth bug" --spec specs/auth/login.md  # short flag
```

#### Update SpecID

```bash
bd update bd-a1b2 --spec-id "specs/auth/login.md"
bd update bd-a1b2 --spec-id ""  # clear spec reference
```

#### Filter by SpecID

```bash
bd list --spec "specs/auth/login.md"     # exact match
bd list --spec "specs/auth/"             # prefix match (all auth specs)
bd list --spec-id "specs/auth/login.md"  # alias
```

#### Show Spec Info

```bash
bd show bd-a1b2
# Output includes:
# Spec: specs/auth/login.md
```

### JSONL Format

```json
{
  "id": "bd-a1b2",
  "title": "Implement login flow",
  "spec_id": "specs/auth/login.md",
  ...
}
```

No changes needed - JSON marshaling handles this automatically via struct tag.

## Implementation Tasks

### Phase 1: Core (MVP) — Completed

#### 1.1 Migration - Add spec_id column
- Create `internal/storage/sqlite/migrations/041_spec_id_column.go`
- Add `spec_id TEXT DEFAULT ''` column
- Add index `idx_issues_spec_id`

#### 1.2 Read from Database
- Update `scanIssueRow()` in `transaction.go`
  - Add `specID sql.NullString` variable
  - Add to `row.Scan()` call
  - Assign `issue.SpecID = specID.String`
- Update SELECT queries to include `spec_id`

#### 1.3 CLI: Create --spec-id flag
- File: `cmd/bd/create.go`
- Add `--spec-id` string flag
- Add `-s` short alias (if available)
- Pass to issue creation

#### 1.4 CLI: Update --spec-id flag
- File: `cmd/bd/update.go`
- Add `--spec-id` string flag
- Handle empty string to clear

#### 1.5 CLI: List --spec filter
- File: `cmd/bd/list.go`
- Add `--spec` string flag
- Implement prefix matching for directory-style specs
- Add WHERE clause: `spec_id LIKE ?`

#### 1.6 CLI: Show spec in output
- File: `cmd/bd/show.go`
- Display SpecID in issue details

### Phase 2: Spec Sync (Next) — Proposed

Goal: **Keep specs and beads in sync**, and surface spec changes that imply work updates.

#### 2.1 Spec Registry (Local Index)
Track known spec files and their state.

**Data model (SQLite):**
```
spec_registry (
  spec_id TEXT PRIMARY KEY,      -- canonical spec identifier (path)
  path TEXT NOT NULL,             -- resolved path from repo root
  title TEXT DEFAULT '',          -- optional: first heading
  sha256 TEXT DEFAULT '',         -- content hash
  mtime DATETIME,                 -- last modified time
  discovered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_scanned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)
```

**JSONL export:** optional future extension (not required for MVP sync).

#### 2.2 Spec Scan & Registry Update
Scan `specs/**/*.md` (configurable) and update registry.

**Behavior:**
- Add new specs to registry.
- Update `sha256` + `mtime` on changes.
- Remove or mark missing specs (soft delete: `missing_at`).
- Normalize spec_id to repo‑relative path (forward slashes).

**CLI:**
```
bd spec scan [--path specs] [--json]
```

#### 2.3 Bead Linking & Coverage Views
Expose registry + linkage.

**CLI:**
```
bd spec list                      # All specs (registry)
bd spec show <spec-id>            # Spec metadata + linked beads
bd spec coverage                  # Specs with/without beads
```

#### 2.4 Spec Change → Bead Update Signals
When a spec’s hash changes, emit a signal so linked beads surface it.

**Mechanism (MVP):**
- Add **spec_change_at** to Issue (or an event).
- On scan: for each changed spec, mark linked beads with `spec_change_at=now`.
- `bd list --spec-changed` filter.
- `bd show` displays “Spec changed on YYYY‑MM‑DD”.

**CLI:**
```
bd list --spec-changed
bd list --spec-changed --spec "specs/auth/"
```

#### 2.5 Optional: Bidirectional Links (Post‑MVP)
Auto‑insert issue references into spec files.
```
bd spec sync  # updates spec markdown with linked beads
```

## What’s Done (Summary)
- `spec_id` field wired across sqlite/dolt/memory.
- Migration 041 adds column + index.
- CLI flags: create/update/list/show.
- RPC list filter supports spec filters.
- Tests added for migration, filtering, show output.
- Docs updated (CLI reference).

#### 2.1 Spec Registry
- `bd spec list` - List all unique spec_ids in use
- `bd spec show <spec-id>` - Show all issues for a spec
- `bd spec coverage` - Show specs with/without issues

#### 2.2 Spec Scanning
- Scan repository for spec files (*.md in specs/)
- Build registry of known specs
- Validate spec_id references real file

#### 2.3 Bidirectional Links
- Auto-insert issue references into spec files
- `bd spec sync` - Update spec files with issue links

## Testing

### Unit Tests

```go
// 041_spec_id_column_test.go
func TestSpecIDMigration(t *testing.T) {
    // Verify column added
    // Verify index created
    // Verify existing issues get empty spec_id
}

// issues_test.go additions
func TestCreateIssueWithSpecID(t *testing.T) {
    // Create issue with spec_id
    // Verify it persists
    // Verify it appears in list
}

func TestUpdateIssueSpecID(t *testing.T) {
    // Update existing issue to add spec_id
    // Update to change spec_id
    // Update to clear spec_id
}

func TestListFilterBySpec(t *testing.T) {
    // Exact match
    // Prefix match
    // No match
}
```

### Integration Tests

```bash
# Create with spec
bd create "Test task" --spec-id "specs/test.md"
bd show bd-xxxx | grep "specs/test.md"

# Update spec
bd update bd-xxxx --spec-id "specs/other.md"
bd show bd-xxxx | grep "specs/other.md"

# Filter by spec
bd list --spec "specs/test.md"
bd list --spec "specs/"  # prefix
```

## File Changes Summary

| File | Change Type |
|------|-------------|
| `internal/types/types.go` | ✅ Modified |
| `internal/storage/sqlite/schema.go` | ✅ Modified |
| `internal/storage/sqlite/queries.go` | ✅ Modified |
| `internal/storage/sqlite/issues.go` | ✅ Modified |
| `internal/storage/sqlite/transaction.go` | ✅ Modified |
| `internal/storage/sqlite/migrations/041_spec_id_column.go` | New |
| `internal/storage/sqlite/migrations/041_spec_id_column_test.go` | New |
| `cmd/bd/create.go` | Modify (flag) |
| `cmd/bd/update.go` | Modify (flag) |
| `cmd/bd/list.go` | Modify (filter) |
| `cmd/bd/show.go` | Modify (display) |
| `internal/rpc/*` | ✅ Modified |
| `internal/storage/dolt/*` | ✅ Modified |
| `internal/storage/memory/*` | ✅ Modified |
| `docs/SPEC_ID.md` | New (this file) |
| `docs/CLI_REFERENCE.md` | Update |

## Open Questions

1. **Spec ID format validation?** Should we validate spec_id is a valid path/URL, or keep it freeform?
   - Recommendation: Freeform for flexibility

2. **Multiple specs per issue?** Should spec_id be a single string or array?
   - Recommendation: Single string for MVP, consider array in Phase 2

3. **Reverse index?** Should we maintain a separate table mapping spec → issues?
   - Recommendation: No, use SQL queries with LIKE for MVP

## Success Criteria

- [ ] Can create issue with --spec-id flag
- [ ] Can update issue spec_id
- [ ] Can filter list by spec
- [ ] spec_id survives export/import cycle
- [ ] spec_id shown in bd show output
- [ ] Migration works on existing databases
- [ ] All existing tests pass

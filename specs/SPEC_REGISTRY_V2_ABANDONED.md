# Spec Registry V2 - Abandoned Branch Learnings

**Status:** ABANDONED
**Branch:** `feature/spec-id-v2` (deleted)
**Date:** 2026-01-31
**Reason:** Duplicate implementation - main already has spec registry

---

## What Happened

We built a spec registry feature on `feature/spec-id-v2` without realizing:
1. The branch was based on an **old version of main**
2. Main had **already evolved** with a complete spec registry (`internal/spec/`)
3. We created **duplicate code** with a different approach

### The Two Approaches

| Main Branch (Scanner-based) | Our Branch (Registration-based) |
|-----------------------------|--------------------------------|
| `bd spec scan` - Auto-discover specs | `bd spec register` - Manual registration |
| `bd spec list` - List discovered specs | `bd spec audit` - List registered specs |
| `bd spec show` - Show spec details | `bd spec status` - Show spec details |
| `bd spec coverage` - Coverage report | `bd spec progress` - Progress bars |
| `bd spec compact` - Archive old specs | `bd spec mark-done` - Mark complete |
| Auto-linking via scan | Manual `bd spec link` |

### Files We Created (Now Deleted)

```
cmd/bd/spec.go                    # Overwrote main's version
internal/rpc/server_specs.go      # Duplicate RPC handlers
internal/storage/sqlite/specs.go  # Duplicate storage layer
internal/storage/sqlite/migrations/041_spec_registry_table.go
internal/storage/sqlite/migrations/042_spec_links_table.go
```

---

## Useful Ideas to Preserve

### 1. Completion Percentage Auto-Update

We added auto-recalculation of spec completion when issues are closed:

```go
// In CloseIssue, after closing:
// Find specs linked to this issue and recalculate completion %
specRows, _ := conn.QueryContext(ctx, `
    SELECT spec_path FROM spec_issues WHERE issue_id = ?
`, id)
// ... recalculate and update spec_registry.completion_percent
```

**Main doesn't have this.** Consider adding to main's implementation.

### 2. Manual Link/Unlink Commands

```bash
bd spec link <spec-path> <issue-id>    # Explicit linking
bd spec unlink <spec-path> <issue-id>  # Explicit unlinking
```

Main uses auto-linking via scan. Manual linking could be useful for edge cases.

### 3. Spec Candidates (Ready to Close)

```bash
bd spec candidates --threshold 0.6
# Shows specs where completion >= threshold
```

Useful for finding specs ready to be marked done.

---

## Root Cause

**No branch freshness check.** We started work without:
1. Checking how far behind main the branch was
2. Checking if the feature already existed on main
3. Pulling latest main before starting

---

## Prevention: Branch Divergence Skill

A skill should run at session start to detect:
1. Branch is >10 commits behind main
2. Files being modified already exist differently on main
3. Feature being built already exists on main

See: `.claude/skills/branch-divergence-check/SKILL.md`

---

## Commits Made (For Reference)

```
6ed92415 fix: harden spec registry consistency
eac82d3b docs: add spec registry blog pitch (Westworld analogy)
675908c1 fix: spec registry correctness issues
c2ed684b feat: complete spec registry with RPC daemon support
821725a1 feat: add spec registry for tracking specification documents (WIP)
```

All abandoned. Knowledge preserved in this spec.

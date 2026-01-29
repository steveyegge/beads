# Change Points: Remove --no-daemon from gastown Codebase

**Date:** 2026-01-29
**Bead:** dolt-test-knc.1.3
**Parent:** dolt-test-knc.1 (Remove --no-daemon usage from codebase)

## Overview

This document identifies specific change points for removing `--no-daemon` flag usage from the gastown codebase. Unlike beads, gastown doesn't implement the flag - it uses `--no-daemon` when calling `bd` as a subprocess.

## Critical Architectural Decision

gastown calls `bd` ~100+ times via subprocess. Two migration paths:

### Option A: Daemon RPC Client
Create Go client library for bd daemon RPC, replace subprocess calls.

**Pros:** Clean integration, no subprocess overhead, type safety
**Cons:** Requires new client library, daemon must always be running

### Option B: Direct Database Access
gastown accesses SQLite/Dolt directly, bypassing bd CLI entirely.

**Pros:** Faster, no daemon dependency for reads
**Cons:** Duplicates storage layer, schema coupling

### Option C: Require Daemon, Remove Flag
Keep subprocess calls but remove `--no-daemon`, require daemon always running.

**Pros:** Minimal code changes
**Cons:** Requires reliable daemon, potential for hanging

**Recommended:** Option A (Daemon RPC Client) for most operations, with graceful degradation.

## Critical Path Files

### 1. Central Wrapper (HIGHEST PRIORITY)

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/beads/beads.go` | 220-224 | `fullArgs := append([]string{"--no-daemon", "--allow-stale"}, args...)` | Remove `--no-daemon`, ensure daemon running |

**This is the central wrapper - ALL gastown bd calls go through here.**
Changing this file affects 100+ call sites automatically.

### 2. Agent Operations

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/beads/beads_agent.go` | 16-37 | `cmd := exec.Command(resolvedBdPath, "--no-daemon", "slot", "set/clear", ...)` | Remove `--no-daemon` |

**Note:** Agent operations use explicit subprocess calls, not the central wrapper.

### 3. Decision Operations

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/beads/beads_decision.go` | Multiple | `"--no-daemon", // Use direct mode to avoid daemon issues` | Remove `--no-daemon` |

### 4. Mail Package

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/mail/bd.go` | ~30 | `allArgs := append([]string{"--no-daemon"}, args...)` | Remove `--no-daemon` |

**Note:** Mail operations also bypass the central wrapper.

### 5. Sling Helpers (Multiple Locations)

| File | Lines | Function | Change Required |
|------|-------|----------|-----------------|
| `internal/cmd/sling_helpers.go` | 48-49 | `verifyBeadExists()` | Remove `--no-daemon --allow-stale` |
| `internal/cmd/sling_helpers.go` | 62 | bead verification | Remove flag |
| `internal/cmd/sling_helpers.go` | 73-74 | bead verification | Remove flag |
| `internal/cmd/sling_helpers.go` | 98 | bead verification | Remove flag |
| `internal/cmd/sling_helpers.go` | 144 | bead verification | Remove flag |
| `internal/cmd/sling_helpers.go` | 156 | bead verification | Remove flag |
| `internal/cmd/sling_helpers.go` | 196 | bead verification | Remove flag |
| `internal/cmd/sling_helpers.go` | 214 | bead verification | Remove flag |

### 6. Formula Operations

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/cmd/sling_formula.go` | 53-57 | `--no-daemon --allow-stale` | Remove flag |

### 7. Convoy Operations

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/cmd/convoy.go` | 1468-1469 | `--no-daemon` for fresh data | Remove flag |
| `internal/cmd/convoy.go` | 1516 | convoy operations | Remove flag |
| `internal/cmd/convoy.go` | 1524 | convoy operations | Remove flag |

### 8. Prime Operations

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/cmd/prime_molecule.go` | 47 | Handle empty stdout bug | Remove workaround after flag removal |

### 9. Doctor Operations

| File | Lines | Current Code | Change Required |
|------|-------|--------------|-----------------|
| `internal/doctor/wisp_check.go` | ~25 | `cmd := exec.Command("bd", "--no-daemon", "mol", "wisp", "gc")` | Remove `--no-daemon` |

## Worktree Copies (CRITICAL)

Due to gastown's worktree structure, files are duplicated across:

| Location | Contains |
|----------|----------|
| `gastown/internal/` | Main codebase |
| `gastown/polecats/*/gastown/internal/` | Polecat worktrees |
| `gastown/refinery/rig/internal/` | Refinery worktree |
| `gastown/mayor/rig/internal/` | Mayor worktree |
| `gastown/crew/*/internal/` | Crew worktrees |

**Each worktree has its own copy of the beads wrapper code.**

**Strategy:** Changes must be committed to main and propagated to all worktrees, OR each worktree must be updated individually.

## Test Files

| File | Notes |
|------|-------|
| `internal/rig/manager_test.go` | Rig manager tests |
| `internal/cmd/sling_test.go` | Sling tests (multiple references) |
| `internal/cmd/rig_integration_test.go` | Integration tests |

## Documentation

| File | Notes |
|------|-------|
| `docs/storage-backends.md` | Mentions "never use with Dolt" |
| `docs/dolt-setup-report.md` | Dolt setup |
| `CHANGELOG.md` | Change history |

## Why gastown Uses --no-daemon

Understanding the rationale helps inform the migration:

1. **Avoid IPC overhead** - Direct mode is faster for reads
2. **Avoid hanging** - When daemon isn't running, subprocess hangs
3. **Fresh data** - Avoid stale daemon cache
4. **Workaround symlink issues** - Daemon has problems with Dolt symlinks

## Implementation Order

### Phase 1: Ensure Daemon Reliability
1. Daemon autostart must be 100% reliable
2. Add health check before subprocess calls
3. Implement timeout/retry logic
4. Fix daemon worktree support

### Phase 2: Update Central Wrapper
1. Modify `internal/beads/beads.go` to not use `--no-daemon`
2. Add daemon health pre-check
3. Test thoroughly (this affects all call sites)

### Phase 3: Update Explicit Subprocess Calls
1. `beads_agent.go` - Agent operations
2. `beads_decision.go` - Decision operations
3. `mail/bd.go` - Mail operations
4. `sling_helpers.go` - All sling helpers
5. `convoy.go` - Convoy operations
6. `wisp_check.go` - Doctor operations

### Phase 4: Update Tests
1. Update test helpers
2. Ensure test daemon available
3. Add integration test coverage

### Phase 5: Propagate to Worktrees
1. Commit changes to main
2. Update all worktrees (polecats, refinery, mayor, crew)
3. Verify each worktree works

### Phase 6: Documentation
1. Update `docs/storage-backends.md`
2. Update `docs/dolt-setup-report.md`
3. Update CHANGELOG

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Daemon not running | Add `bd daemon ensure-running` before operations |
| Subprocess hangs | Add timeout (10s) with retry |
| Stale data | Daemon should auto-refresh, or add explicit refresh |
| Worktree divergence | Script to propagate changes to all worktrees |

## Dependencies

- dolt-test-knc.1.1: Research complete (audit document)
- dolt-test-knc.1.2: beads changes (should complete first)
- dolt-test-9it: systemctl daemon service (parallel work)
- bd daemon must support all operations gastown needs

## Pre-requisites from beads

Before gastown changes can proceed, beads must:
1. Implement all RPC commands gastown uses
2. Fix daemon worktree support
3. Ensure daemon autostart reliability
4. Add daemon health check command

## Success Criteria

1. All `--no-daemon` references removed from gastown
2. Central wrapper (`beads.go`) uses daemon
3. All explicit subprocess calls use daemon
4. All worktrees updated
5. Tests pass without `--no-daemon`
6. gastown operations work reliably with daemon
7. No subprocess hangs when daemon is healthy

# Implementation Plan: Remove --no-daemon from gastown

**Date:** 2026-01-29
**Bead:** dolt-test-knc.1.5
**Parent:** dolt-test-knc.1 (Remove --no-daemon usage from codebase)
**Reference:** GASTOWN_NO_DAEMON_CHANGEPOINTS.md

## Executive Summary

This plan removes `--no-daemon` usage from gastown's subprocess calls to `bd` over 3 phases. gastown doesn't implement the flag - it uses it when calling `bd` as a subprocess. The migration requires coordination with beads changes.

## Prerequisites

Before starting this implementation:

1. **beads Phase 1 complete** - All RPC commands must be available
2. **beads Phase 2 complete** - Daemon must be reliable for subprocess calls
3. **dolt-test-9it (systemctl daemon service)** - Daemon must be always-on

## Architectural Decision

**Chosen Approach:** Remove `--no-daemon` flag, require daemon running

**Rationale:**
- Simplest migration path
- No new client library needed
- Daemon reliability handled by systemctl
- Performance acceptable (daemon IPC is fast)

**Rejected Alternatives:**
- Go client library: Too much new code, ongoing maintenance
- Direct database access: Schema coupling, duplicated storage layer

## Migration Strategy

### Worktree Handling

gastown has multiple worktrees with copied code. Strategy:

1. **Commit changes to main branch**
2. **Propagate via normal workflow** - As polecats spawn, they get fresh main
3. **Script for crew worktrees** - Update long-lived crew worktrees

### Testing Strategy

1. Ensure daemon is running in test environment
2. Add daemon health check before subprocess calls
3. Add timeout to prevent hanging

## Phase 1: Central Wrapper Update (2-3 work items)

### 1.1 Update beads.go

The central wrapper at `internal/beads/beads.go` handles ~100 call sites.

**Current:**
```go
fullArgs := append([]string{"--no-daemon", "--allow-stale"}, args...)
```

**New:**
```go
fullArgs := append([]string{"--allow-stale"}, args...)
```

### 1.2 Add Daemon Health Check

Add pre-check before subprocess calls:

```go
func ensureDaemonRunning() error {
    // Quick health check via bd daemon status
    // Return error if daemon not available
}

func runBd(args ...string) (string, error) {
    if err := ensureDaemonRunning(); err != nil {
        return "", fmt.Errorf("bd daemon not running: %w", err)
    }
    // ... existing subprocess code
}
```

### 1.3 Add Timeout

Prevent hanging if daemon is unresponsive:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
cmd := exec.CommandContext(ctx, bdPath, args...)
```

### Tasks

| Task | Description | Estimate |
|------|-------------|----------|
| 1.1.1 | Update beads.go central wrapper | S |
| 1.1.2 | Add ensureDaemonRunning() helper | M |
| 1.1.3 | Add timeout to subprocess calls | S |

## Phase 2: Explicit Subprocess Updates (4-5 work items)

Several files bypass the central wrapper with explicit subprocess calls.

### 2.1 Agent Operations

**File:** `internal/beads/beads_agent.go`

**Current:**
```go
cmd := exec.Command(resolvedBdPath, "--no-daemon", "slot", "set", ...)
```

**New:**
```go
cmd := exec.Command(resolvedBdPath, "slot", "set", ...)
```

### 2.2 Decision Operations

**File:** `internal/beads/beads_decision.go`

Remove all `--no-daemon` flags from decision-related subprocess calls.

### 2.3 Mail Operations

**File:** `internal/mail/bd.go`

**Current:**
```go
allArgs := append([]string{"--no-daemon"}, args...)
```

**New:**
```go
allArgs := args
```

### 2.4 Sling Helpers

**File:** `internal/cmd/sling_helpers.go`

Update all `verifyBeadExists()` and related functions to remove `--no-daemon`.

### 2.5 Other Files

| File | Change |
|------|--------|
| `internal/cmd/sling_formula.go` | Remove flag |
| `internal/cmd/convoy.go` | Remove flag |
| `internal/cmd/prime_molecule.go` | Remove exit 0 workaround |
| `internal/doctor/wisp_check.go` | Remove flag |

### Tasks

| Task | Description | Estimate |
|------|-------------|----------|
| 2.1.1 | Update beads_agent.go | S |
| 2.1.2 | Update beads_decision.go | S |
| 2.1.3 | Update mail/bd.go | S |
| 2.1.4 | Update sling_helpers.go (8 locations) | M |
| 2.1.5 | Update remaining files | S |

## Phase 3: Worktree Propagation (2-3 work items)

### 3.1 Commit to Main

All changes from Phases 1-2 committed to main branch.

### 3.2 Update Crew Worktrees

Create script to update long-lived crew worktrees:

```bash
#!/bin/bash
# update-crew-worktrees.sh
for crew in /home/ubuntu/gt11/gastown/crew/*/; do
    (cd "$crew/rig" && git pull --rebase origin main)
done
```

### 3.3 Documentation Updates

| File | Change |
|------|--------|
| `docs/storage-backends.md` | Remove --no-daemon guidance |
| `docs/dolt-setup-report.md` | Update Dolt configuration |
| `CHANGELOG.md` | Document change |

### Tasks

| Task | Description | Estimate |
|------|-------------|----------|
| 3.1.1 | Create worktree update script | S |
| 3.1.2 | Update documentation | S |
| 3.1.3 | Verify all worktrees updated | S |

## Verification Checklist

### Per-Phase Verification

**After Phase 1:**
- [ ] Central wrapper no longer uses --no-daemon
- [ ] Daemon health check works
- [ ] Timeouts prevent hanging
- [ ] All bd calls via wrapper still work

**After Phase 2:**
- [ ] No --no-daemon in any gastown file
- [ ] grep -r "\-\-no-daemon" returns empty
- [ ] All sling operations work
- [ ] All convoy operations work
- [ ] All agent operations work

**After Phase 3:**
- [ ] All crew worktrees updated
- [ ] Documentation updated
- [ ] CHANGELOG entry added

### Integration Test

After all phases:
1. Spawn new polecat - verify it works
2. Run sling operation - verify it works
3. Test convoy workflow - verify it works
4. Test mail operations - verify they work
5. Test in worktree - verify it works

## Rollback Plan

| Phase | Rollback |
|-------|----------|
| 1 | Re-add --no-daemon to beads.go |
| 2 | Re-add --no-daemon to explicit calls |
| 3 | N/A (documentation only) |

## Error Handling

When daemon is unavailable:

```
Error: bd daemon not running

The bd daemon is required for this operation. To start it:
  systemctl --user start bd-daemon

To check status:
  systemctl --user status bd-daemon

If the daemon fails to start, check logs:
  journalctl --user -u bd-daemon
```

## Dependencies

```
beads Phase 1 (RPC)
         |
         v
beads Phase 2 (hooks)
         |
         v
gastown Phase 1 (wrapper)
         |
         v
gastown Phase 2 (explicit calls)
         |
         v
gastown Phase 3 (worktrees)
```

## Success Metrics

1. **No --no-daemon in gastown** - grep returns empty
2. **All operations succeed** - No regressions in sling, convoy, mail
3. **No hanging** - Timeout catches any daemon issues
4. **All worktrees updated** - No stale copies

## Notes

### Why gastown Used --no-daemon

Understanding helps ensure we don't regress:

1. **Avoid IPC overhead** - Daemon is fast enough now
2. **Avoid hanging** - Timeout handles this
3. **Fresh data** - Daemon auto-refreshes
4. **Symlink issues** - Fixed in daemon

### Future Optimization

After this migration, if performance is a concern:
1. Consider batch operations
2. Consider persistent daemon connection
3. Profile to identify actual bottlenecks

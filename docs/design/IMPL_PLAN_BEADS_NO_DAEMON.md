# Implementation Plan: Remove --no-daemon from beads

**Date:** 2026-01-29
**Bead:** dolt-test-knc.1.4
**Parent:** dolt-test-knc.1 (Remove --no-daemon usage from codebase)
**Reference:** BEADS_NO_DAEMON_CHANGEPOINTS.md

## Executive Summary

This plan removes the `--no-daemon` flag and direct mode from the beads codebase over 4 phases, spanning approximately 15-20 work items. The migration preserves backwards compatibility during transition via deprecation warnings.

## Prerequisites

Before starting this implementation:

1. **dolt-test-9it (systemctl daemon service)** must be complete
   - Daemon must be systemctl-managed for reliability
   - Autostart must be guaranteed

2. **Daemon must support all commands** currently requiring direct mode:
   - delete, config, compact, rename-prefix, types, repo, migrate-sync
   - comments RPC (list, add)
   - Routed ID resolution
   - Cross-rig routing

## Migration Strategy

### Backwards Compatibility

**Approach:** Deprecate before remove

1. **Phase 1:** Add deprecation warning when `--no-daemon` used
2. **Phase 2:** Flag continues to work with warning for 2 releases
3. **Phase 3:** Flag errors with message pointing to alternative
4. **Phase 4:** Remove flag entirely

### Version Timeline

| Version | Behavior |
|---------|----------|
| v0.X (current) | Flag works, no warning |
| v0.X+1 | Flag works, deprecation warning |
| v0.X+2 | Flag errors with migration help |
| v0.X+3 | Flag removed |

## Phase 1: RPC Implementation (8-10 work items)

### 1.1 Add Missing RPC Commands

Each command needs server-side implementation in `internal/rpc/server_*.go` and client-side in `cmd/bd/*.go`.

| Task | Command | Server File | Estimate |
|------|---------|-------------|----------|
| 1.1.1 | delete | server_cleanup.go | S |
| 1.1.2 | config get/set/list/unset | server_config.go (new) | M |
| 1.1.3 | compact | server_compact.go (new) | M |
| 1.1.4 | rename-prefix | server_rename.go (new) | S |
| 1.1.5 | types | server_types.go (new) | S |
| 1.1.6 | repo remove/sync | server_repo.go (new) | M |
| 1.1.7 | migrate-sync | server_migrate.go (new) | S |
| 1.1.8 | comments (list, add) | server_comments.go | M |

### 1.2 Add Routed ID Support

| Task | Description | Estimate |
|------|-------------|----------|
| 1.2.1 | Implement routed ID resolution in daemon | M |
| 1.2.2 | Update show command to use daemon for routed IDs | S |
| 1.2.3 | Update update command to use daemon for routed IDs | S |
| 1.2.4 | Update close command to use daemon for routed IDs | S |

### 1.3 Protocol Updates

Update `internal/rpc/protocol.go` for each new RPC type.

## Phase 2: Git Hook Migration (3-4 work items)

### Problem

Git hooks currently use `--no-daemon` for fast, synchronous operations.

### Solution

Two options (recommend Option A):

**Option A: Daemon RPC for Hooks**
- Hooks call daemon via RPC
- Daemon must be running (guaranteed by systemctl)
- Slightly slower but consistent

**Option B: Queue Mechanism**
- Hooks queue operations
- Daemon processes queue asynchronously
- Faster hooks but eventual consistency

### Tasks

| Task | Description | Estimate |
|------|-------------|----------|
| 2.1 | Implement hook RPC in daemon | M |
| 2.2 | Update hook.go subprocess calls | M |
| 2.3 | Update hooks.go subprocess calls | M |
| 2.4 | Update init_git_hooks.go templates | S |

## Phase 3: Test Migration (3-4 work items)

### Strategy

1. **Use dual_mode_test.go framework** - Already supports testing against daemon
2. **Spawn test daemon per test suite** - Isolation with real daemon
3. **Update TestMain** - Start daemon before tests, stop after

### Tasks

| Task | Description | Estimate |
|------|-------------|----------|
| 3.1 | Update dual_mode_test.go to daemon-only | M |
| 3.2 | Add test daemon lifecycle management | M |
| 3.3 | Update cli_fast_test.go | S |
| 3.4 | Update remaining test files | M |

### Test Daemon Helper

```go
// In test helper
func StartTestDaemon(t *testing.T) (cleanup func()) {
    // Start daemon with test config
    // Return cleanup function
}
```

## Phase 4: Remove Direct Mode (4-5 work items)

### 4.1 Add Deprecation Warning

```go
// In main.go
if noDaemon {
    fmt.Fprintln(os.Stderr, "WARNING: --no-daemon is deprecated and will be removed in a future version")
}
```

### 4.2 Remove Files and Code

| Task | File | Action |
|------|------|--------|
| 4.2.1 | cmd/bd/direct_mode.go | Delete file |
| 4.2.2 | cmd/bd/context.go | Remove NoDaemon field and accessors |
| 4.2.3 | cmd/bd/main.go | Remove flag, routing, error messages |
| 4.2.4 | cmd/bd/main_daemon.go | Remove FallbackFlagNoDaemon constant |

### 4.3 Remove Fallback Paths

Update each command to remove `fallbackToDirectMode()` calls:
- comments.go
- show.go
- update.go
- close.go
- (and others)

### 4.4 Documentation

| Task | Files | Action |
|------|-------|--------|
| 4.4.1 | docs/DAEMON.md | Remove --no-daemon references |
| 4.4.2 | docs/ADVANCED.md | Update troubleshooting |
| 4.4.3 | docs/CLI_REFERENCE.md | Remove flag |
| 4.4.4 | docs/WORKTREES.md | Update worktree guidance |
| 4.4.5 | website/docs/* | Update all website docs |

## Verification Checklist

### Per-Phase Verification

**After Phase 1:**
- [ ] All commands work via daemon RPC
- [ ] `bd --help` still shows --no-daemon (deprecated)
- [ ] Existing scripts continue working

**After Phase 2:**
- [ ] Git hooks work with daemon
- [ ] Sync operations complete successfully
- [ ] No hang on commit/checkout

**After Phase 3:**
- [ ] All tests pass
- [ ] No test uses --no-daemon
- [ ] CI pipeline green

**After Phase 4:**
- [ ] `bd --help` no longer shows --no-daemon
- [ ] direct_mode.go deleted
- [ ] No references to NoDaemon in code
- [ ] Documentation updated

### Full Regression Test

After all phases:
1. Fresh install test
2. Upgrade from previous version test
3. Worktree workflow test
4. CI/CD pipeline test
5. Multi-agent workflow test

## Rollback Plan

Each phase is independently rollbackable:

| Phase | Rollback |
|-------|----------|
| 1 | Remove new RPC handlers |
| 2 | Revert hook changes |
| 3 | Revert test changes |
| 4 | Re-add --no-daemon flag (emergency only) |

## Dependencies

```
dolt-test-9it (systemctl service)
         |
         v
   Phase 1 (RPC)
         |
    +----+----+
    |         |
    v         v
Phase 2    Phase 3
  (hooks)   (tests)
    |         |
    +----+----+
         |
         v
   Phase 4 (remove)
```

## Success Metrics

1. **Zero test failures** - All tests pass without --no-daemon
2. **No user-reported regressions** - Deprecation period catches issues
3. **Daemon uptime** - >99.9% availability via systemctl
4. **Performance parity** - Operations not slower than direct mode

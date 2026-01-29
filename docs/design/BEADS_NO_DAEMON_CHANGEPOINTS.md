# Change Points: Remove --no-daemon from beads Codebase

**Date:** 2026-01-29
**Bead:** dolt-test-knc.1.2
**Parent:** dolt-test-knc.1 (Remove --no-daemon usage from codebase)

## Overview

This document identifies specific change points for removing `--no-daemon` flag and direct mode from the beads codebase.

## Critical Path Files

### 1. Flag Definition and Routing

| File | Lines | Change Required |
|------|-------|-----------------|
| `cmd/bd/main.go` | 42 | Remove `noDaemon bool` variable |
| `cmd/bd/main.go` | 661-666 | Remove flag check and direct mode routing |
| `cmd/bd/main.go` | 874 | Remove error message suggesting `--no-daemon` |
| `cmd/bd/main.go` | flag init | Remove `--no-daemon` from persistent flags |

### 2. Direct Mode Implementation (DELETE ENTIRELY)

| File | Action |
|------|--------|
| `cmd/bd/direct_mode.go` | **Delete file** - Contains `ensureDirectMode()`, `fallbackToDirectMode()`, `disableDaemonForFallback()` |

### 3. Context/State Management

| File | Lines | Change Required |
|------|-------|-----------------|
| `cmd/bd/context.go` | 28 | Remove `NoDaemon bool` from cmdContext struct |
| `cmd/bd/context.go` | 301-312 | Remove `isNoDaemon()` / `setNoDaemon()` accessors |
| `cmd/bd/context.go` | 549 | Remove `cmdCtx.NoDaemon = noDaemon` initialization |

### 4. Daemon Fallback Constants

| File | Lines | Change Required |
|------|-------|-----------------|
| `cmd/bd/main_daemon.go` | 28 | Remove `FallbackFlagNoDaemon = "flag_no_daemon"` constant |

## Commands Requiring RPC Implementation

These commands currently call `ensureDirectMode()` because daemon doesn't support them.
**Prerequisite:** Implement daemon RPC support for each before removing direct mode.

| File | Command | RPC Status | Notes |
|------|---------|------------|-------|
| `cleanup.go:103` | `delete` | **Need RPC** | Daemon doesn't support delete |
| `sync.go:178` | `sync` | **Need RPC** | Requires direct database access |
| `config.go:82,142,183,252` | `config` (set/get/list/unset) | **Need RPC** | All config operations |
| `compact.go:166,183` | `compact` | **Need RPC** | Database compaction |
| `list.go:1101` | `list --watch` | **Need RPC** | Watch mode |
| `rename_prefix.go:71` | `rename-prefix` | **Need RPC** | Prefix renaming |
| `repo.go:108,216` | `repo remove/sync` | **Need RPC** | Multi-repo operations |
| `types.go:40` | `types` | **Need RPC** | Type listing |
| `doctor_pollution.go:18` | `pollution` | **Need RPC** | Doctor pollution check |
| `migrate_sync.go:90` | `migrate-sync` | **Need RPC** | Migration |

## Commands With Fallback Logic (Remove Fallback)

These commands try daemon first, fall back to direct mode. After RPC implementation, remove fallback paths.

| File | Lines | Command | Fallback Trigger |
|------|-------|---------|------------------|
| `comments.go` | 56, 179 | `comment list/add` | Daemon doesn't support comment RPC |
| `show.go` | 116, 786, 988 | `show` (routed IDs) | Routed IDs need direct mode |
| `update.go` | 351 | `update` (routed IDs) | Routed IDs bypass daemon |
| `close.go` | 178, 358 | `close` (routed/suggest-next) | Cross-rig routing |

## Subprocess Calls (Convert to Daemon RPC or Queue)

These spawn `bd` subprocesses with `--no-daemon` for git hooks and sync operations.

| File | Lines | Purpose | Recommended Change |
|------|-------|---------|-------------------|
| `hook.go` | 375, 534, 602, 648, 817 | Sync operations in git hooks | Use daemon RPC or queue |
| `hooks.go` | 567, 648, 695, 807 | Inline import/flush in git hooks | Use daemon RPC |
| `init_git_hooks.go` | 416, 425 | Generated hook scripts | Update templates |
| `sync_import.go` | 44-45 | Import subprocess | Use daemon RPC |
| `doctor/fix/common.go` | 18 | Doctor fix commands | Use daemon RPC |

## Test Files (Extensive Updates)

~50+ test files use `--no-daemon` for isolation. Strategy options:

1. **Run against test daemon** - Spawn dedicated daemon per test
2. **Use dual-mode framework** - `dual_mode_test.go` already supports this
3. **Mock daemon client** - For unit tests that don't need real database

### High-Impact Test Files

| File | References | Notes |
|------|------------|-------|
| `dual_mode_test.go` | 72+ | Framework for testing both modes - update to daemon-only |
| `cli_fast_test.go` | 79, 691, 851, 1125 | Fast CLI tests |
| `cli_coverage_show_test.go` | 29, 49 | Coverage tests |
| `show_test.go` | 45-226 | Show command tests |
| `doctor_repair_test.go` | Multiple | Doctor tests |
| `doctor_repair_chaos_test.go` | Multiple | Chaos tests |
| `init_test.go` | 507 | Init tests |

## Documentation Updates

~60+ documentation files reference `--no-daemon`. Update after implementation.

| Category | Files |
|----------|-------|
| Primary docs | `docs/DAEMON.md`, `docs/ADVANCED.md`, `docs/CLI_REFERENCE.md` |
| Guides | `docs/FAQ.md`, `docs/GIT_INTEGRATION.md`, `docs/WORKTREES.md` |
| Quick start | `docs/QUICKSTART.md`, `docs/CONFIG.md` |
| Website | `website/docs/` (~20 files) |
| Plugin | `claude-plugin/` docs |

## Implementation Order

### Phase 1: RPC Implementation (Prerequisites)
1. Implement missing RPC commands (delete, config, compact, etc.)
2. Add comment RPC (list, add)
3. Implement routed ID resolution in daemon
4. Add cross-rig routing to daemon

### Phase 2: Git Hook Migration
1. Update hook templates to use daemon or queue mechanism
2. Ensure daemon autostart is reliable
3. Add hook-specific health checks

### Phase 3: Test Migration
1. Update dual_mode_test.go framework
2. Convert high-impact tests to daemon mode
3. Add test daemon lifecycle management

### Phase 4: Remove Direct Mode
1. Delete `direct_mode.go`
2. Remove NoDaemon from context
3. Remove --no-daemon flag
4. Remove all fallback paths

### Phase 5: Documentation
1. Update all docs to remove --no-daemon references
2. Update troubleshooting guides
3. Update worktree documentation

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Daemon not running | Daemon autostart, health checks, clear error messages |
| Git hook slowdown | Pre-warmed daemon, async queue for non-critical ops |
| Test flakiness | Proper test daemon lifecycle, increased timeouts |
| CI/CD failures | Ensure daemon starts reliably in CI environment |

## Dependencies

- dolt-test-knc.1.1: Research complete (audit document)
- dolt-test-9it: systemctl daemon service (parallel work)
- Daemon must be reliable before removing direct mode fallback

## Success Criteria

1. All `--no-daemon` references removed from codebase
2. `direct_mode.go` deleted
3. All tests pass using daemon
4. All commands work via daemon RPC
5. Git hooks work without `--no-daemon`
6. Documentation updated

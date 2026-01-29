# Audit: --no-daemon and Direct Mode Usage Patterns

**Date:** 2026-01-29
**Bead:** bd-0lh64.2.1
**Parent Epic:** bd-0lh64.2 (Remove --no-daemon and direct mode from codebase)

## Executive Summary

This audit identifies all `--no-daemon` flag references and direct mode implementations across the beads and gastown codebases. The findings categorize each usage by purpose (testing, fallback, legitimate use case) to inform the daemon removal epic.

## Findings Overview

| Category | beads | gastown | Total |
|----------|-------|---------|-------|
| CLI Flag/Implementation | 15+ | 0 | 15+ |
| Tests | 50+ | 30+ | 80+ |
| Git Hooks/Subprocess Calls | 15+ | 100+ | 115+ |
| Documentation | 60+ | 20+ | 80+ |
| Fallback/Error Handling | 20+ | 5+ | 25+ |

## beads Codebase Change Points

### 1. Core Implementation (`cmd/bd/`)

#### Primary Flag Definition
- **`cmd/bd/main.go:42`**: `noDaemon bool` - Flag storage
- **`cmd/bd/main.go:661-666`**: Flag check and routing to direct mode
- **`cmd/bd/main.go:874`**: Error message suggesting `--no-daemon`

#### Direct Mode Functions (`cmd/bd/direct_mode.go`)
- **Line 15**: `ensureDirectMode()` - Switches to direct mode if daemon active
- **Line 26**: `fallbackToDirectMode()` - Disables daemon client, opens local store
- **Line 31**: `disableDaemonForFallback()` - Closes daemon client, updates status

#### Fallback Reason Constant (`cmd/bd/main_daemon.go`)
- **Line 28**: `FallbackFlagNoDaemon = "flag_no_daemon"`

#### Context/State (`cmd/bd/context.go`)
- **Line 28**: `NoDaemon bool` in cmdContext struct
- **Lines 301-312**: `isNoDaemon()` / `setNoDaemon()` accessors
- **Line 549**: `cmdCtx.NoDaemon = noDaemon` initialization

### 2. Commands Requiring Direct Mode

These commands call `ensureDirectMode()` because daemon doesn't support them:

| File | Command | Reason |
|------|---------|--------|
| `cleanup.go:103` | `delete` | Daemon doesn't support delete |
| `sync.go:178` | `sync` | Requires direct database access |
| `config.go:82,142,183,252` | `config` (set/get/list/unset) | Requires direct database access |
| `compact.go:166,183` | `compact` | Requires direct database access |
| `list.go:1101` | `list --watch` | Watch mode requires direct access |
| `rename_prefix.go:71` | `rename-prefix` | Not supported by daemon |
| `repo.go:108,216` | `repo remove/sync` | Requires direct database access |
| `types.go:40` | `types` | Requires direct database access |
| `doctor_pollution.go:18` | `pollution` | Requires direct mode |
| `migrate_sync.go:90` | `migrate-sync` | Requires direct database access |

### 3. Commands With Fallback to Direct Mode

These commands try daemon first, fall back if unsupported:

| File | Command | Fallback Trigger |
|------|---------|------------------|
| `comments.go:56,179` | `comment list/add` | Daemon doesn't support comment RPC |
| `show.go:116,786,988` | `show` (routed IDs) | Routed IDs need direct mode |
| `update.go:351` | `update` (routed IDs) | Routed IDs bypass daemon |
| `close.go:178,358` | `close` (routed/suggest-next) | Cross-rig routing |

### 4. Subprocess Calls Using --no-daemon

These spawn `bd` subprocesses with `--no-daemon`:

| File | Purpose |
|------|---------|
| `hook.go:375,534,602,648,817` | Sync operations in git hooks |
| `hooks.go:567,648,695,807` | Inline import/flush in git hooks |
| `init_git_hooks.go:416,425` | Generated hook scripts |
| `sync_import.go:44-45` | Import subprocess |
| `doctor/fix/common.go:18` | Doctor fix commands |

### 5. Test Files

Extensive test coverage uses `--no-daemon` for isolation:

- `dual_mode_test.go` - Framework for testing both modes (72+ references to DirectMode)
- `cli_fast_test.go` - Fast CLI tests (lines 79, 691, 851, 1125)
- `cli_coverage_show_test.go` - Coverage tests (lines 29, 49)
- `show_test.go` - Show command tests (lines 45-226)
- `doctor_repair_test.go`, `doctor_repair_chaos_test.go` - Doctor tests
- `init_test.go:507` - Init tests
- Many others in `cmd/bd/` directory

### 6. Documentation References

Files with `--no-daemon` documentation (60+ occurrences):

- `docs/DAEMON.md` - Primary daemon documentation
- `docs/ADVANCED.md` - Advanced usage
- `docs/CLI_REFERENCE.md` - CLI reference
- `docs/FAQ.md` - FAQ
- `docs/GIT_INTEGRATION.md` - Git worktree usage
- `docs/WORKTREES.md` - Worktree-specific docs
- `docs/QUICKSTART.md` - Quick start guide
- `docs/CONFIG.md` - Configuration reference
- `website/docs/` - Website documentation (20+ files)
- `claude-plugin/` - Claude plugin docs

## gastown Codebase Change Points

### 1. Core Beads Wrapper (`internal/beads/`)

#### `beads.go` (Lines 220-224)
```go
// Use --no-daemon for faster read operations (avoids daemon IPC overhead)
fullArgs := append([]string{"--no-daemon", "--allow-stale"}, args...)
```
**This is the central wrapper - ALL gastown bd calls go through here.**

#### `beads_agent.go` (Lines 16-37)
```go
// Uses --no-daemon to avoid hanging when daemon isn't running (fix: fhc-e520ae)
cmd := exec.Command(resolvedBdPath, "--no-daemon", "slot", "set", ...)
cmd := exec.Command(resolvedBdPath, "--no-daemon", "slot", "clear", ...)
```

#### `beads_decision.go` (Multiple locations)
```go
"--no-daemon", // Use direct mode to avoid daemon issues
```

### 2. Mail Package (`internal/mail/bd.go`)
```go
// Use --no-daemon to bypass daemon connection issues with Dolt symlinked databases.
allArgs := append([]string{"--no-daemon"}, args...)
```

### 3. Command Helpers (`internal/cmd/`)

#### `sling_helpers.go`
- Lines 48-49, 62, 73-74, 98, 144, 156, 196, 214
- All `verifyBeadExists()` and related functions use `--no-daemon --allow-stale`

#### `sling_formula.go` (Line 53-57)
```go
// Uses --no-daemon with --allow-stale for consistency with verifyBeadExists.
```

#### `convoy.go` (Lines 1468-1469, 1516, 1524)
```go
// Use --no-daemon to ensure fresh data (avoid stale cache from daemon)
```

#### `prime_molecule.go` (Line 47)
```go
// Handle bd --no-daemon exit 0 bug: empty stdout means not found
```

### 4. Doctor Checks (`internal/doctor/wisp_check.go`)
```go
// Run bd --no-daemon mol wisp gc
cmd := exec.Command("bd", "--no-daemon", "mol", "wisp", "gc")
```

### 5. Test Files

Extensive test coverage in gastown:
- `internal/rig/manager_test.go`
- `internal/cmd/sling_test.go` (multiple references)
- `internal/cmd/rig_integration_test.go`
- Many others across worktrees

### 6. Documentation

- `docs/storage-backends.md` - Storage backend docs (mentions never use with Dolt)
- `docs/dolt-setup-report.md`
- `CHANGELOG.md`

### 7. Worktree Copies

Due to gastown's worktree structure, many files are duplicated across:
- `gastown/internal/` (main)
- `gastown/polecats/*/gastown/internal/`
- `gastown/refinery/rig/internal/`
- `gastown/mayor/rig/internal/`
- `gastown/crew/*/internal/`

**Each worktree has its own copy of the beads wrapper code.**

## Categorized Usage Patterns

### Category 1: Testing (Should Remove)

~80+ test files use `--no-daemon` for test isolation. These should:
- Use the dual-mode test framework
- Or run against a test daemon
- Or be updated to not need the flag

### Category 2: Fallback/Degradation (Should Remove)

~25+ locations fall back to direct mode when:
- Daemon doesn't support the RPC
- Daemon connection fails
- Daemon is unhealthy

After removing direct mode, these need:
- All commands implemented in daemon RPC
- Better daemon error handling
- No fallback path

### Category 3: Git Hooks (Needs Alternative)

~15+ locations in git hooks use `--no-daemon` for:
- Inline sync during commit/checkout
- Import operations

**Legitimate Concern:** Git hooks need fast, reliable execution without daemon startup delay.

**Alternative Solutions:**
1. Ensure daemon is always running (require daemon for git hooks)
2. Use daemon RPC for hook operations
3. Implement a lightweight hook-specific protocol

### Category 4: Subprocess Calls in gastown (Needs Alternative)

~100+ locations where gastown calls `bd --no-daemon`:

**Why gastown uses --no-daemon:**
1. Avoid daemon IPC overhead for reads (performance)
2. Avoid hanging when daemon isn't running (reliability)
3. Ensure fresh data (avoid stale daemon cache)
4. Bypass daemon connection issues with Dolt symlinks

**Alternative Solutions:**
1. gastown connects to bd daemon via RPC (requires daemon client library for Go)
2. gastown uses SQLite/Dolt directly (bypass bd CLI entirely)
3. Ensure daemon is always running with proper health checks

### Category 5: Worktree Support (Needs Alternative)

Multiple docs and code paths use `--no-daemon` for git worktrees because:
- Daemon has issues with symlinked databases
- Each worktree may need its own daemon

**Alternative Solutions:**
1. Fix daemon worktree support
2. One daemon per worktree with proper routing
3. Centralized daemon with worktree-aware routing

### Category 6: Documentation (Should Update)

~80+ documentation references need updating to:
- Remove `--no-daemon` examples
- Update troubleshooting guides
- Update worktree guides

## Legitimate Use Cases Requiring Alternatives

1. **Git Hooks**: Need fast, synchronous operations without daemon startup
2. **gastown Subprocess Calls**: Need reliable bd access without hanging
3. **Worktrees**: Need proper database isolation/routing
4. **CI/CD**: Need reliable execution without daemon dependency
5. **Testing**: Need isolated test environments
6. **Offline Work**: Need operation without daemon running

## Recommended Approach

### Phase 1: beads Changes (bd-0lh64.2.2)

1. Implement remaining RPC commands in daemon:
   - `delete`, `config`, `compact`, `rename-prefix`, `types`, `repo`, `migrate-sync`
   - `comments` RPC (list, add)
   - Routed ID resolution

2. Update git hooks to use daemon or queue mechanism

3. Update dual-mode tests to always use daemon

4. Remove `--no-daemon` flag and direct mode code

### Phase 2: gastown Changes (bd-0lh64.2.3)

1. Create Go client library for bd daemon RPC

2. Update `internal/beads/beads.go` to use daemon RPC instead of subprocess

3. Or: Migrate to direct SQLite/Dolt access (bypass bd CLI)

4. Update all worktree copies

5. Handle daemon health/startup in gastown

### Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Daemon not running | High - all bd operations fail | Daemon autostart, health checks |
| Daemon startup delay | Medium - slower git hooks | Pre-warmed daemon, queue mechanism |
| Worktree issues | High - multi-worktree workflows break | Fix daemon worktree support |
| Test reliability | Medium - flaky tests | Proper test daemon management |

## Files Summary for Implementation

### beads: Remove direct mode (bd-0lh64.2.2)

High-priority files:
- `cmd/bd/main.go` - Remove flag, update routing
- `cmd/bd/direct_mode.go` - Remove entirely
- `cmd/bd/context.go` - Remove NoDaemon field
- `cmd/bd/main_daemon.go` - Remove FallbackFlagNoDaemon
- All files calling `ensureDirectMode()` or `fallbackToDirectMode()`

Medium-priority (commands to add RPC support):
- `cmd/bd/cleanup.go`, `config.go`, `compact.go`, etc.

Low-priority (docs):
- All documentation files

### gastown: Remove --no-daemon usage (bd-0lh64.2.3)

High-priority:
- `internal/beads/beads.go` - Central wrapper
- `internal/beads/beads_agent.go` - Agent operations
- `internal/beads/beads_decision.go` - Decision operations
- `internal/mail/bd.go` - Mail operations
- `internal/cmd/sling_helpers.go` - Sling helpers

Medium-priority:
- `internal/cmd/convoy.go`
- `internal/cmd/sling_formula.go`
- `internal/doctor/wisp_check.go`

Note: Changes must be applied to all worktree copies.

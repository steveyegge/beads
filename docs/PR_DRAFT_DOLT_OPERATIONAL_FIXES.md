# Draft PR: Dolt Backend Operational Improvements

> **Status**: SUBMITTED - Core PR merged
> **Tracking**: hq-3446fc.13
> **PR**: https://github.com/steveyegge/beads/pull/1260 (Lock retry + stale lock cleanup)

## Summary

This PR adds operational improvements to the Dolt storage backend for increased reliability in production environments with multiple concurrent clients.

## Problem

The Dolt embedded driver creates exclusive locks that can cause:
- "database is read only" errors when lock acquisition fails
- Connection leaks from idle processes
- Stale lock files after crashes
- Lock contention between daemon and CLI commands

## Changes

### 1. Lock Retry with Exponential Backoff
- Automatic retry on lock contention (30 retries, ~6 second window)
- Exponential backoff starting at 100ms
- Prevents immediate failures on transient lock conflicts

### 2. Idle Connection Management
- Releases database lock after configurable idle period (default: 30s)
- Allows external `dolt` CLI access while beads daemon is running
- Automatic reconnection on next operation

### 3. Stale Lock File Cleanup
- Detects and cleans orphaned `.dolt/noms/LOCK` files on startup
- Prevents "database is read only" after unexpected termination
- Checks PID validity before cleanup

### 4. Read-Only Mode Improvements
- Dedicated read-only path that skips schema initialization
- Avoids acquiring write locks for read-only commands
- Improves concurrency for list/show/ready operations

### 5. Import Support
- `bd import` now works with Dolt backend
- Batch issue creation with configurable options
- Comment import with timestamp preservation

### 6. Doctor Dolt Support
- `bd doctor` can diagnose Dolt-specific issues
- Checks lock state, connection health, schema validity

## Files Changed

- `internal/storage/dolt/store.go` - Core lock and connection management
- `internal/storage/dolt/import.go` - New file for import support
- `internal/storage/dolt/server.go` - sql-server mode enhancements
- `internal/storage/factory/factory_dolt.go` - Factory integration
- `cmd/bd/doctor/dolt.go` - Diagnostic support

## Testing

- [ ] Unit tests for lock retry logic
- [ ] Integration tests for concurrent access
- [ ] End-to-end daemon coexistence test
- [ ] Performance benchmarks under contention

## Breaking Changes

None. All changes are additive or improve existing behavior.

## Migration

No migration required. New configuration options have sensible defaults:
- `LockRetries`: 30
- `LockRetryDelay`: 100ms
- `IdleTimeout`: 30s (daemon), 0 (CLI)

---

## Submission Status (2026-01-22)

### PR #1260 - Core Operational Fixes (Submitted)
- ✅ Lock retry with exponential backoff
- ✅ Stale lock file cleanup
- ✅ Transient error detection (format version, lock contention)

### Remaining for Follow-up PRs
- ⏳ Idle connection management (bd-d705ea)
- ⏳ Read-only mode improvements (bd-o3gt)
- ⏳ Import support for Dolt backend
- ⏳ Doctor Dolt diagnostics
- ⏳ Server mode auto-start (bd-f4f78a)

The core PR addresses the most critical production issues. Additional
improvements can be submitted incrementally as separate PRs.

---

*Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>*

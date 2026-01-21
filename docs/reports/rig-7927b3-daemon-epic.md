# Epic: Enable bd daemon mode for gt (rig-7927b3)

**Priority:** P0
**Status:** Open
**Created:** 2026-01-20

## Problem

Multiple bd processes across Gas Town (polecats, crew, daemons) all use `--no-daemon`
flag when executing bd commands. Each process opens/closes the Dolt database directly,
causing lock contention with the single-writer constraint.

## Current State

- gt commands in `internal/cmd/*.go` use `exec.Command("bd", "--no-daemon", ...)`
- Each bd invocation acquires Dolt write lock, does work, releases
- When multiple agents run simultaneously, they compete for locks
- Retry logic helps within one bd process but not across processes

## Goal

Make gt use the bd daemon by default so all database operations go through a single
connection, eliminating lock contention.

## Success Criteria

1. `gt sling <formula> <rig>` works reliably without lock errors
2. Multiple polecats can spawn simultaneously without contention
3. bd daemon handles concurrent requests properly
4. Fallback to --no-daemon only when daemon unavailable

## Child Tasks

### Task 1: Audit --no-daemon usage in gt codebase
- Find all `exec.Command("bd", "--no-daemon"...)` calls
- Location: `gastown/internal/cmd/*.go`
- Document which commands need write vs read access

### Task 2: Remove --no-daemon from sling commands
- Update sling.go
- Update sling_formula.go
- Update sling_helpers.go
- Test operations work through daemon

### Task 3: Remove --no-daemon from hook and other commands
- Update hook.go
- Update handoff.go
- Update other gt commands as needed

### Task 4: Add daemon health check and fallback
- Check if daemon running before using it
- Auto-start daemon if needed
- Graceful fallback to --no-daemon if daemon unavailable

### Task 5: Test concurrent polecat spawning
- Test multiple `gt sling` commands simultaneously
- Spawn 3+ polecats in parallel
- Verify no lock errors

## Related Issues

- rig-f70e33: Lock retry logic restored
- bd-iw6: Lock occurs during hook operations
- bd-nnq: Stale hook slot in agent bead
- bd-b2h: Polecat .beads not initialized

## Notes

The lock contention is so severe that we can't even reliably file beads right now
when multiple agents are running. This epic is critical for Gas Town stability.

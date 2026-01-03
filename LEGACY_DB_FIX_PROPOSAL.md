# Legacy Database Fix Proposal

## Problem

When daemon tries to start with a legacy database (pre-v0.17.5), it:
1. Detects missing repository fingerprint
2. Logs error to `daemon.log`
3. Exits immediately
4. CLI never sees the error (buried in log file)
5. CLI waits for socket timeout (5s or 500ms)
6. Falls back to direct mode silently

**User experience:** Commands are mysteriously slow, no clear error.

## Proposed Fix: Surface Error to CLI

Instead of silent fallback, detect when daemon failed due to legacy DB and show actionable error.

### Implementation Option A: CLI Checks Log (Recommended)

**File:** `cmd/bd/main.go` (around line 410)

```go
if tryAutoStartDaemon(socketPath) {
    // Daemon started successfully
    client, err := rpc.TryConnect(socketPath)
    // ... existing code
} else {
    // Auto-start failed - check if it's due to legacy DB
    if isLegacyDatabaseError() {
        fmt.Fprintf(os.Stderr, `
╔════════════════════════════════════════════════════════════╗
║ LEGACY DATABASE DETECTED                                   ║
╠════════════════════════════════════════════════════════════╣
║ Your database was created before v0.17.5 and needs         ║
║ migration to work with the daemon.                          ║
║                                                             ║
║ FIX: Run this command once:                                 ║
║      bd migrate --update-repo-id                            ║
║                                                             ║
║ Or disable daemon mode:                                     ║
║      export BEADS_NO_DAEMON=1                               ║
╚════════════════════════════════════════════════════════════╝

`)
        os.Exit(1)
    }
    // Fall back to direct mode for other reasons
    daemonStatus.FallbackReason = FallbackAutoStartFailed
}

func isLegacyDatabaseError() bool {
    logPath := filepath.Join(filepath.Dir(dbPath), "daemon.log")
    data, err := os.ReadFile(logPath)
    if err != nil {
        return false
    }
    // Check last 2KB of log for legacy database error
    tail := string(data)
    if len(tail) > 2048 {
        tail = tail[len(tail)-2048:]
    }
    return strings.Contains(tail, "LEGACY DATABASE DETECTED")
}
```

**Pros:**
- User sees clear error immediately
- Provides exact fix command
- No auto-migration (safer)
- Non-intrusive (only shows on error)

**Cons:**
- Requires reading log file
- Adds slight overhead (one-time)

### Implementation Option B: Auto-Migrate with Confirmation

**File:** `cmd/bd/daemon_server.go` (before the error)

```go
if !hasRepoID {
    fmt.Fprintf(os.Stderr, "Legacy database detected. Migrate now? [y/N]: ")
    var response string
    fmt.Scanln(&response)
    if strings.ToLower(response) == "y" {
        if err := migrateRepoID(ctx, store); err != nil {
            return fmt.Errorf("migration failed: %w", err)
        }
        fmt.Fprintln(os.Stderr, "✓ Migration complete. Starting daemon...")
    } else {
        return fmt.Errorf("migration declined - daemon cannot start with legacy database")
    }
}
```

**Pros:**
- One-command fix
- Guides user through migration

**Cons:**
- Daemon can't prompt for input (runs in background)
- More complex

### Implementation Option C: Auto-Migrate (Safest)

Add to daemon startup:

```go
if !hasRepoID {
    debug.Logf("Legacy database detected, auto-migrating...")
    repoID := generateRepoFingerprint()
    if err := store.SetConfig(ctx, "repository.id", repoID); err != nil {
        return fmt.Errorf("auto-migration failed: %w", err)
    }
    debug.Logf("✓ Auto-migrated legacy database (repo_id: %s)", repoID)
}
```

**Pros:**
- Zero user action needed
- Just works™
- Daemon starts successfully

**Cons:**
- Silent migration (though logged)
- Users might want to know this happened

## Recommendation

**Option C (Auto-Migrate)** with CLI notification:

1. Daemon auto-migrates legacy DBs on first start
2. Logs the migration
3. CLI checks daemon log after first successful connection
4. Shows one-time notice: "✓ Migrated legacy database to v0.17.5+"

This gives:
- Best UX (just works)
- User awareness (notification)
- No repeated prompts
- Daemon starts reliably

## Testing

After implementing, test:
```bash
# Create legacy DB (remove repo_id from config)
sqlite3 .beads/beads.db "DELETE FROM config WHERE key = 'repository.id'"

# Verify daemon starts and auto-migrates
bd daemon --start
bd daemon --status  # Should be running

# Verify migration worked
bd list  # Should be fast (< 100ms)
```

## Impact

Fixing this helps:
- All beads users (no more 5s delays)
- Tool integrations (GasTown UI, MCP servers, etc.)
- CI/CD pipelines using beads
- Developer experience

## Files to Modify

1. `cmd/bd/daemon_server.go` - Add auto-migration logic
2. `cmd/bd/main.go` - Add migration notification
3. `cmd/bd/daemon_autostart.go` - Already fixed timeout (committed)
4. Tests - Add legacy DB migration test

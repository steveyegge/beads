# Sync Architecture (DEPRECATED)

> **DEPRECATED:** `bd sync` has been deprecated. Dolt backend now handles
> synchronization automatically. Use `bd export` or `bd import` for manual
> JSONL operations. This document is retained for historical reference.

This document explains the design decisions behind `bd sync` - why it works the way it does, and the problems each design choice solves.

> **Looking for something else?**
> - Command usage: [commands/sync.md](/claude-plugin/commands/sync.md) (Reference)
> - Troubleshooting: [website/docs/recovery/sync-failures.md](/website/docs/recovery/sync-failures.md) (How-To)
> - Deletion behavior: [docs/DELETIONS.md](/docs/DELETIONS.md) (Explanation)

## Why Pull-First?

The core problem: if you export local state before seeing what's on the remote, you commit to a snapshot that may conflict with changes you haven't seen yet. Any changes that arrive during pull get imported to the database but never make it back to the exported JSONL — they're silently lost on the next push.

Pull-first sync solves this by reversing the order:

```
Machine A: Create bd-43, sync
            ↳ Load local state (bd-43 in memory)
            ↳ Pull (bd-42 edit arrives in JSONL)
            ↳ Merge local + remote
            ↳ Export merged state
            ↳ Push (contains both bd-43 AND bd-42 edit)
```

By loading local state into memory before pulling, we can perform a proper merge that preserves both sets of changes.

## The 3-Way Merge Model

Beads uses 3-way merge - the same algorithm Git uses for merging branches. The reason: it distinguishes between "unchanged" and "deleted".

With 2-way merge (just comparing local vs remote), you cannot tell if an issue is missing because:
- It was deleted locally
- It was deleted remotely
- It never existed in one copy

3-way merge adds a **base state** - the snapshot from the last successful sync:

```
        Base (last sync)
             |
      +------+------+
      |             |
    Local        Remote
   (your DB)   (git pull)
      |             |
      +------+------+
             |
          Merged
```

This enables precise conflict detection:

| Base | Local | Remote | Result | Reason |
|------|-------|--------|--------|--------|
| A | A | A | A | No changes |
| A | A | B | B | Only remote changed |
| A | B | A | B | Only local changed |
| A | B | B | B | Both made same change |
| A | B | C | **merge** | True conflict |
| A | - | A | **deleted** | Local deleted, remote unchanged |
| A | A | - | **deleted** | Remote deleted, local unchanged |
| A | B | - | B | Local changed after remote deleted |
| A | - | B | B | Remote changed after local deleted |

The last two rows show why 3-way merge prevents accidental data loss: if one side deleted while the other modified, we keep the modification.

## Sync Flow

```
bd sync

  1. Pull  -->  2. Merge  -->  3. Export  -->  4. Push
   Remote       3-way          JSONL          Remote
     |            |              |              |
     v            v              v              v
  Fetch       Compare all    Write merged   Commit +
  issues.jsonl  three states   to issues.jsonl  push
```

Step-by-step:

1. **Load local state** - Read all issues from SQLite database into memory
2. **Load base state** - Read `sync_base.jsonl` (last successful sync snapshot)
3. **Pull** - Fetch and merge remote git changes
4. **Load remote state** - Parse `issues.jsonl` after pull
5. **3-way merge** - Compare base vs local vs remote for each issue
6. **Import** - Write merged result to database
7. **Export** - Write database to JSONL (ensures DB is source of truth)
8. **Commit & Push** - Commit changes and push to remote
9. **Update base** - Save current state as base for next sync

## Why Different Merge Strategies?

Not all fields should merge the same way. Consider labels: if Machine A adds "urgent" and Machine B adds "blocked", the merged result should have both labels - not pick one or the other.

Beads uses field-specific merge strategies:

| Field Type | Strategy | Why This Strategy? |
|------------|----------|-------------------|
| Scalars (title, status, priority) | LWW | Only one value possible; most recent wins |
| Labels | Union | Multiple valid; keep all (no data loss) |
| Dependencies | Union | Links should not disappear silently |
| Comments | Append | Chronological; dedup by ID prevents duplicates |

**LWW (Last-Write-Wins)** uses the `updated_at` timestamp to determine which value wins. On timestamp tie, remote wins (arbitrary but deterministic).

**Union** combines both sets. If local has `["urgent"]` and remote has `["blocked"]`, the result is `["blocked", "urgent"]` (sorted for determinism).

**Append** collects all comments from both sides, deduplicating by comment ID. This ensures conversations are never lost.

## Why "Zombie" Issues?

When merging, there is an edge case: what happens when one machine deletes an issue while another modifies it?

```
Machine A: Delete bd-42 → sync
Machine B: (offline) → Edit bd-42 → sync
           Pull reveals bd-42 was deleted, but local has edits
```

Beads follows the principle of **no silent data loss**. If local has meaningful changes to an issue that remote deleted, the local changes win. The issue "resurrects" - it comes back from the dead.

This is intentional: losing someone's work without warning is worse than keeping a deleted issue. The user can always delete it again if needed.

However, if the local copy is unchanged from base (meaning the user on this machine never touched it since last sync), the deletion propagates normally.

## Concurrency Protection

What happens if you run `bd sync` twice simultaneously? Without protection, both processes could:

1. Load the same base state
2. Pull at different times (seeing different remote states)
3. Merge differently
4. Overwrite each other's exports
5. Push conflicting commits

Beads uses an **exclusive file lock** (`.beads/.sync.lock`) to serialize sync operations:

```go
lock := flock.New(lockPath)
locked, err := lock.TryLock()
if !locked {
    return fmt.Errorf("another sync is in progress")
}
defer lock.Unlock()
```

The lock is non-blocking - if another sync is running, the second sync fails immediately with a clear error rather than waiting indefinitely.

The lock file is not git-tracked (it only matters on the local machine).

## Clock Skew Considerations

LWW relies on timestamps, which introduces a vulnerability: what if machine clocks disagree?

```
Machine A (clock correct):     Edit bd-42 at 10:00:00
Machine B (clock +1 hour):     Edit bd-42 at "11:00:00" (actually 10:00:30)
                               Machine B wins despite editing later
```

Beads cannot fully solve clock skew (distributed systems limitation), but it mitigates the risk:

1. **24-hour warning threshold** - If two timestamps differ by more than 24 hours, a warning is emitted. This catches grossly misconfigured clocks.

2. **Union for collections** - Labels and dependencies use union merge, which is immune to clock skew (both values kept).

3. **Append for comments** - Comments are sorted by `created_at` but never lost due to clock skew.

For maximum reliability, ensure machine clocks are synchronized via NTP.

## Files Reference

| File | Purpose |
|------|---------|
| `.beads/issues.jsonl` | Current state (git-tracked) |
| `.beads/sync_base.jsonl` | Last-synced state (not tracked, per-machine) |
| `.beads/.sync.lock` | Concurrency guard (not tracked) |
| `.beads/beads.db` | SQLite database (not tracked) |

The JSONL files are the source of truth for git. The database is derived from JSONL on each machine.

## Sync Modes

Beads supports several sync modes for different use cases:

| Mode | Trigger | Flow | Use Case |
|------|---------|------|----------|
| **Normal** | Default `bd sync` | Pull → Merge → Export → Push | Standard multi-machine sync |
| **Sync-branch** | `sync.branch` config | Separate git branch for beads files | Isolated beads history |
| **External** | `BEADS_DIR` env | Separate repo for beads | Shared team database |
| **From-main** | `sync.from_main` config | Clone beads from main branch | Feature branch workflow |
| **Local-only** | No git remote | Export only (no push) | Single-machine usage |
| **Export-only** | `--no-pull` flag | Export → Push (skip pull/merge) | Force local state to remote |

### Mode Selection Logic

```
sync:
├─ --no-pull flag?
│   └─ Yes → Export-only (skip pull/merge)
├─ No remote configured?
│   └─ Yes → Local-only (export only)
├─ BEADS_DIR or external .beads?
│   └─ Yes → External repo mode
├─ sync.branch configured?
│   └─ Yes → Sync-branch mode
├─ sync.from_main configured?
│   └─ Yes → From-main mode
└─ Normal pull-first sync
```

### Test Coverage

Each mode has E2E tests in `cmd/bd/`:

| Mode | Test File |
|------|-----------|
| Normal | `sync_test.go`, `sync_merge_test.go` |
| Sync-branch | `sync_modes_test.go` |
| External | `sync_external_test.go` |
| From-main | `sync_branch_priority_test.go` |
| Local-only | `sync_local_only_test.go` |
| Export-only | `sync_modes_test.go` |
| Sync-branch (CLI E2E) | `syncbranch_e2e_test.go` |
| Sync-branch (Daemon E2E) | `daemon_sync_branch_e2e_test.go` |

## Sync Paths: CLI vs Daemon

Sync-branch mode has two distinct code paths that must be tested independently:

```
bd sync (CLI)                     Daemon (background)
     │                                  │
     ▼                                  ▼
Force close daemon              daemon_sync_branch.go
(prevent stale conn)            syncBranchCommitAndPush()
     │                                  │
     ▼                                  ▼
syncbranch.CommitToSyncBranch   Direct database + git
syncbranch.PullFromSyncBranch   with forceOverwrite flag
```

### Why Two Paths?

SQLite connections become stale when the daemon holds them while the CLI operates on the same database. The CLI path forces daemon closure before sync to prevent connection corruption. The daemon path operates directly since it owns the connection.

### Test Isolation Strategy

Each E2E test requires proper isolation to prevent interference:

| Variable | Purpose |
|----------|---------|
| `BEADS_NO_DAEMON=1` | Prevent daemon auto-start (set in TestMain) |
| `BEADS_DIR=<clone>/.beads` | Isolate database per clone |

### E2E Test Architecture: Bare Repo Pattern

E2E tests use a bare repository as a local "remote" to enable real git operations:

```
     ┌─────────────┐
     │  bare.git   │  ← Local "remote"
     └──────┬──────┘
            │
     ┌──────┴──────┐
     ▼             ▼
 Machine A    Machine B
 (clone)      (clone)
     │             │
     │ bd-1        │ bd-2
     │ push        │ push (wins)
     │             │
     │◄────────────┤ divergence
     │ 3-way merge │
     ▼             │
 [bd-1, bd-2]      │
```

| Aspect | update-ref (old) | bare repo (new) |
|--------|------------------|-----------------|
| Push testing | Simulated | Real |
| Fetch testing | Fake refs | Real |
| Divergence | Cannot test | Non-fast-forward |

### E2E Test Coverage Matrix

| Test | Path | What It Tests |
|------|------|---------------|
| TestSyncBranchE2E | CLI | syncbranch.CommitToSyncBranch/Pull |
| TestDaemonSyncBranchE2E | Daemon | syncBranchCommitAndPush/Pull |
| TestDaemonSyncBranchForceOverwrite | Daemon | forceOverwrite delete propagation |

## Historical Context

The pull-first sync design was introduced in PR #918 to fix issue #911 (data loss during concurrent edits). The original export-first design was simpler but could not handle the "edit during sync" scenario correctly.

The 3-way merge algorithm borrows concepts from:
- Git's merge strategy (base state concept)
- CRDT research (union for sets, LWW for scalars)
- Tombstone patterns (deletion tracking with TTL)

## See Also

- [DELETIONS.md](DELETIONS.md) - Tombstone behavior and deletion tracking
- [GIT_INTEGRATION.md](GIT_INTEGRATION.md) - How beads integrates with git
- [DAEMON.md](DAEMON.md) - Automatic sync via daemon
- [ARCHITECTURE.md](ARCHITECTURE.md) - Overall system architecture

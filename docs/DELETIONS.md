# Deletion Tracking

This document describes how bd tracks and propagates deletions across repository clones.

## Overview

When issues are deleted in one clone, those deletions need to propagate to other clones. Without this mechanism, deleted issues would "resurrect" when another clone's database is imported.

The **deletions manifest** (`.beads/deletions.jsonl`) is an append-only log that records every deletion. This file is committed to git and synced across all clones.

## File Format

The deletions manifest is a JSON Lines file where each line is a deletion record:

```jsonl
{"id":"bd-abc","ts":"2025-01-15T10:00:00Z","by":"stevey","reason":"duplicate of bd-xyz"}
{"id":"bd-def","ts":"2025-01-15T10:05:00Z","by":"claude","reason":"cleanup"}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Issue ID that was deleted |
| `ts` | string | Yes | ISO 8601 UTC timestamp |
| `by` | string | Yes | Actor who performed the deletion |
| `reason` | string | No | Optional context (e.g., "duplicate", "cleanup") |

## Commands

### Deleting Issues

```bash
bd delete bd-42                    # Delete single issue
bd delete bd-42 bd-43 bd-44        # Delete multiple issues
bd cleanup -f                      # Delete all closed issues
```

All deletions are automatically recorded to the manifest.

### Viewing Deletions

```bash
bd deleted                         # Recent deletions (last 7 days)
bd deleted --since=30d             # Deletions in last 30 days
bd deleted --all                   # All tracked deletions
bd deleted bd-xxx                  # Lookup specific issue
bd deleted --json                  # Machine-readable output
```

## Propagation Mechanism

### Export (Local Delete)

1. `bd delete` removes issue from SQLite
2. Deletion record appended to `deletions.jsonl`
3. `bd sync` commits and pushes the manifest

### Import (Remote Delete)

1. `bd sync` pulls updated manifest
2. Import checks each DB issue against manifest
3. If issue ID is in manifest, it's deleted from local DB
4. If issue ID is NOT in manifest and NOT in JSONL:
   - Check git history (see fallback below)
   - If found in history → deleted upstream, remove locally
   - If not found → local unpushed work, keep it

## Git History Fallback

The manifest is pruned periodically to prevent unbounded growth. When a deletion record is pruned but the issue still exists in some clone's DB:

1. Import detects: "DB issue not in JSONL, not in manifest"
2. Falls back to git history search
3. Uses `git log -S` to check if issue ID was ever in JSONL
4. If found in history → it was deleted, remove from DB
5. **Backfill**: Re-append the deletion to manifest (self-healing)

This fallback ensures deletions propagate even after manifest pruning.

## Configuration

### Retention Period

By default, deletion records are kept for 7 days. Configure via:

```bash
bd config set deletions.retention_days 30
```

Or in `.beads/config.yaml`:

```yaml
deletions:
  retention_days: 30
```

### Auto-Compact Threshold

Auto-compaction during `bd sync` is opt-in:

```bash
bd config set deletions.auto_compact_threshold 100
```

When the manifest exceeds this threshold, old records are pruned during sync. Set to 0 to disable (default).

### Manual Pruning

```bash
bd compact --retention 7           # Prune records older than 7 days
bd compact --retention 0           # Prune all records (use git fallback)
```

## Size Estimates

- Each record: ~80 bytes
- 7-day retention with 100 deletions/day: ~56KB
- Git compressed: ~10KB

The manifest stays small even with heavy deletion activity.

## Conflict Resolution

When multiple clones delete issues simultaneously:

1. Both append their deletion records
2. Git merges (append-only = no conflicts)
3. Result: duplicate entries for same ID (different timestamps)
4. `LoadDeletions` deduplicates by ID (keeps any entry)
5. Result: deletion propagates correctly

Duplicate records are harmless and cleaned up during pruning.

## Troubleshooting

### Deleted Issue Reappearing

If a deleted issue reappears after sync:

```bash
# Check if in manifest
bd deleted bd-xxx

# Force re-import
bd import --force

# If still appearing, check git history
git log -S '"id":"bd-xxx"' -- .beads/beads.jsonl
```

### Manifest Not Being Committed

Ensure deletions.jsonl is tracked:

```bash
git add .beads/deletions.jsonl
```

And NOT in .gitignore.

### Large Manifest

If the manifest is growing too large:

```bash
# Check size
wc -l .beads/deletions.jsonl

# Manual prune
bd compact --retention 7

# Enable auto-compact
bd config set deletions.auto_compact_threshold 100
```

## Design Rationale

### Why JSONL?

- Append-only: natural for deletion logs
- Human-readable: easy to audit
- Git-friendly: line-based diffs
- No merge conflicts: append = trivial merge

### Why Not Delete from JSONL?

Removing lines from `beads.jsonl` would work but:
- Loses audit trail (who deleted what when)
- Harder to merge (line deletions can conflict)
- Can't distinguish "deleted" from "never existed"

### Why Time-Based Pruning?

- Bounds manifest size
- Git history fallback handles edge cases
- 7-day default handles most sync scenarios
- Configurable for teams with longer sync cycles

### Why Git Fallback?

- Handles pruned records gracefully
- Self-healing via backfill
- Works with shallow clones (partial fallback)
- No data loss from aggressive pruning

## Related

- [CONFIG.md](CONFIG.md) - Configuration options
- [DAEMON.md](DAEMON.md) - Daemon auto-sync behavior
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - General troubleshooting

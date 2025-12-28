---
id: jsonl-sync
title: JSONL Sync
sidebar_position: 4
---

# JSONL Sync

How beads synchronizes issues across git.

## The Magic

Beads uses a dual-storage architecture:

```
SQLite DB (.beads/beads.db, gitignored)
    ↕ auto-sync (5s debounce)
JSONL (.beads/issues.jsonl, git-tracked)
    ↕ git push/pull
Remote JSONL (shared across machines)
```

**Why this design?**
- SQLite for fast local queries
- JSONL for git-friendly versioning
- Automatic sync keeps them aligned

## Auto-Sync Behavior

### Export (SQLite → JSONL)

Triggers:
- Any database change
- After 5 second debounce (batches multiple changes)
- Manual `bd sync`

```bash
# Force immediate export
bd sync

# Check what would be exported
bd export --dry-run
```

### Import (JSONL → SQLite)

Triggers:
- After `git pull` (via git hooks)
- When JSONL is newer than database
- Manual `bd import`

```bash
# Force import
bd import -i .beads/issues.jsonl

# Preview import
bd import -i .beads/issues.jsonl --dry-run
```

## Git Hooks

Install hooks for seamless sync:

```bash
bd hooks install
```

Hooks installed:
- **pre-commit** - Exports to JSONL before commit
- **post-merge** - Imports from JSONL after pull
- **pre-push** - Ensures sync before push

## Manual Sync

```bash
# Full sync cycle: export + commit + push
bd sync

# Just export
bd export

# Just import
bd import -i .beads/issues.jsonl
```

## Conflict Resolution

When JSONL conflicts occur during git merge:

### With Merge Driver (Recommended)

The beads merge driver handles JSONL conflicts automatically:

```bash
# Install merge driver
bd init  # Prompts for merge driver setup
```

The driver:
- Merges non-conflicting changes
- Preserves both sides for real conflicts
- Uses latest timestamp for same-issue edits

### Without Merge Driver

Manual resolution:

```bash
# After merge conflict
git checkout --ours .beads/issues.jsonl   # or --theirs
bd import -i .beads/issues.jsonl
bd sync
```

## Orphan Handling

When importing issues with missing parents:

```bash
# Configure orphan handling
bd config set import.orphan_handling allow     # Import anyway (default)
bd config set import.orphan_handling resurrect # Restore deleted parents
bd config set import.orphan_handling skip      # Skip orphans
bd config set import.orphan_handling strict    # Fail on orphans
```

Per-command override:

```bash
bd import -i issues.jsonl --orphan-handling resurrect
```

## Deletion Tracking

Deleted issues are tracked in `.beads/deletions.jsonl`:

```bash
# Delete issue (records to manifest)
bd delete bd-42

# View deletions
bd deleted
bd deleted --since=30d

# Deletions propagate via git
git pull  # Imports deletions from remote
```

## Troubleshooting Sync

### JSONL out of sync

```bash
# Force full sync
bd sync

# Check sync status
bd info
```

### Import errors

```bash
# Check import status
bd import -i .beads/issues.jsonl --dry-run

# Allow orphans if needed
bd import -i .beads/issues.jsonl --orphan-handling allow
```

### Duplicate detection

```bash
# Find duplicates after import
bd duplicates

# Auto-merge duplicates
bd duplicates --auto-merge
```

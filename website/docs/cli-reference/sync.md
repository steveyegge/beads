---
id: sync
title: Sync & Export
sidebar_position: 6
---

# Sync & Export Commands

Commands for synchronizing with git.

## bd sync

:::warning DEPRECATED
`bd sync` is deprecated. Dolt now handles synchronization automatically. Use `bd export` or `bd import` for manual data transfer when needed. The `bd sync` command will be removed in a future release.
:::

Full sync cycle: export, commit, push.

```bash
bd sync [flags]
```

**What it does:**
1. Exports database to `.beads/issues.jsonl`
2. Stages the JSONL file
3. Commits with auto-generated message
4. Pushes to remote

**Flags:**
```bash
--json     JSON output
--dry-run  Preview without changes
```

**Examples:**
```bash
# DEPRECATED - Dolt handles sync automatically
# Use bd export or bd import instead
bd export
bd import -i .beads/issues.jsonl
```

**When to use:**
- ~~End of work session~~ No longer needed -- Dolt syncs automatically
- ~~Before switching branches~~ No longer needed -- Dolt syncs automatically
- ~~After significant changes~~ No longer needed -- Dolt syncs automatically

## bd export

Export database to JSONL.

```bash
bd export [flags]
```

**Flags:**
```bash
--output, -o    Output file (default: .beads/issues.jsonl)
--dry-run       Preview without writing
--json          JSON output
```

**Examples:**
```bash
bd export
bd export -o backup.jsonl
bd export --dry-run
```

## bd import

Import from JSONL file.

```bash
bd import -i <file> [flags]
```

**Flags:**
```bash
--input, -i           Input file (required)
--dry-run             Preview without changes
--orphan-handling     How to handle missing parents
--dedupe-after        Run duplicate detection after import
--json                JSON output
```

**Orphan handling modes:**
| Mode | Behavior |
|------|----------|
| `allow` | Import orphans without validation (default) |
| `resurrect` | Restore deleted parents as tombstones |
| `skip` | Skip orphaned children with warning |
| `strict` | Fail if parent missing |

**Examples:**
```bash
bd import -i .beads/issues.jsonl
bd import -i backup.jsonl --dry-run
bd import -i issues.jsonl --orphan-handling resurrect
bd import -i issues.jsonl --dedupe-after --json
```

## bd migrate

Migrate database schema.

```bash
bd migrate [flags]
```

**Flags:**
```bash
--inspect    Show migration plan (for agents)
--dry-run    Preview without changes
--cleanup    Remove old files after migration
--yes        Skip confirmation
--json       JSON output
```

**Examples:**
```bash
bd migrate --inspect --json
bd migrate --dry-run
bd migrate
bd migrate --cleanup --yes
```

## bd hooks

Manage git hooks.

```bash
bd hooks <subcommand> [flags]
```

**Subcommands:**
| Command | Description |
|---------|-------------|
| `install` | Install git hooks |
| `uninstall` | Remove git hooks |
| `status` | Check hook status |

**Examples:**
```bash
bd hooks install
bd hooks status
bd hooks uninstall
```

## Auto-Sync Behavior

### Dolt-Native Sync (Current)

Dolt now handles synchronization automatically. There is no need to run `bd sync` manually. Data is synchronized natively through Dolt's built-in replication and merge capabilities.

### Legacy Daemon Mode

Previously, the daemon handled sync automatically:
- Exports to JSONL after changes (5s debounce)
- Imports from JSONL when newer

This is no longer required. Dolt manages sync natively.

### Without Daemon

Use `--no-daemon` flag:
- Changes only written to SQLite
- Use `bd export` or `bd import` for manual data transfer

```bash
bd --no-daemon create "Task"
bd export  # Manual export if needed
```

## Conflict Resolution

### Merge Driver (Recommended)

Install the beads merge driver:

```bash
bd init  # Prompts for merge driver setup
```

The driver automatically:
- Merges non-conflicting changes
- Preserves both sides for real conflicts
- Uses latest timestamp for same-issue edits

### Manual Resolution

```bash
# After merge conflict
git checkout --ours .beads/issues.jsonl
bd import -i .beads/issues.jsonl
bd export  # Re-export after resolving conflicts
```

## Deletion Tracking

Deletions sync via `.beads/deletions.jsonl`:

```bash
# Delete issue
bd delete bd-42

# View deletions
bd deleted
bd deleted --since=30d

# Deletions propagate via git
git pull  # Imports deletions from remote
```

## Best Practices

1. **Dolt handles sync automatically** -- manual `bd sync` is no longer needed
2. **Install git hooks** - `bd hooks install`
3. **Use merge driver** - Avoids manual conflict resolution
4. **Check sync status** - `bd info` shows daemon/sync state
5. **Use `bd export` / `bd import`** for manual data transfer when needed

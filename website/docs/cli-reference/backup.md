---
id: backup
title: bd backup
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc backup` (bd version 0.59.0)

## bd backup

Back up your beads database for off-machine recovery.

Without a subcommand, exports all tables to JSONL files in .beads/backup/.
Events are exported incrementally using a high-water mark.

For Dolt-native backups (preserves full commit history, faster for large databases):
  bd backup init <path>     Set up a backup destination (filesystem or DoltHub)
  bd backup sync            Push to configured backup destination

Other subcommands:
  bd backup status          Show backup status (JSONL + Dolt)
  bd backup restore [path]  Restore from JSONL backup files

DoltHub is recommended for cloud backup:
  bd backup init https://doltremoteapi.dolthub.com/<user>/<repo>
  Set DOLT_REMOTE_USER and DOLT_REMOTE_PASSWORD for authentication.

Note: Git-protocol remotes are NOT recommended for Dolt backups — push times
exceed 20 minutes, cache grows unboundedly, and force-push is needed after recovery.

```
bd backup [flags]
```

**Flags:**

```
      --force   Export even if nothing changed
```

### bd backup init

Configure a filesystem path as a Dolt backup destination.

The path can be a local directory (external drive, NAS, Dropbox folder) or a
DoltHub remote URL.

Filesystem examples:
  bd backup init /mnt/usb/beads-backup
  bd backup init ~/Dropbox/beads-backup

DoltHub (recommended for cloud backup):
  bd backup init https://doltremoteapi.dolthub.com/myuser/beads-backup

After initializing, run 'bd backup sync' to push your data.

Under the hood this calls DOLT_BACKUP('add', ...) to register the destination.

```
bd backup init <path>
```

### bd backup restore

Restore the beads database from JSONL backup files.

By default, reads from .beads/backup/ (or the configured backup directory).
Optionally specify a path to a directory containing JSONL backup files.

This command:
  1. Detects .beads/backup/*.jsonl files (or accepts a custom path)
  2. Imports config, issues, comments, dependencies, labels, and events
  3. Restores backup_state.json watermarks so incremental backup resumes correctly

Use this after losing your Dolt database (machine crash, new clone, etc.)
when you have JSONL backups on disk or in git.

The database must already be initialized (run 'bd init' first if needed).
To initialize and restore in one step, use: bd init && bd backup restore

```
bd backup restore [path] [flags]
```

**Flags:**

```
      --dry-run   Show what would be restored without making changes
```

### bd backup status

Show last backup status

```
bd backup status
```

### bd backup sync

Sync the current beads database to the configured Dolt backup destination.

This pushes the entire database state (all branches, full history) to the
backup location configured with 'bd backup init'.

The backup is atomic — if the sync fails, the previous backup state is preserved.

Run 'bd backup init <path>' first to configure a destination.

```
bd backup sync
```


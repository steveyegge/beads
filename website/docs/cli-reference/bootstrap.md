---
id: bootstrap
title: bd bootstrap
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc bootstrap` (bd version 0.59.0)

## bd bootstrap

Bootstrap sets up the beads database without destroying existing data.
Unlike 'bd init --force', bootstrap will never delete existing issues.

Bootstrap auto-detects the right action:
  • If sync.git-remote is configured: clones from the remote
  • If .beads/backup/*.jsonl exists: restores from backup
  • If no database exists: creates a fresh one
  • If database already exists: validates and reports status

This is the recommended command for:
  • Setting up beads on a fresh clone
  • Recovering after moving to a new machine
  • Repairing a broken database configuration

Examples:
  bd bootstrap              # Auto-detect and set up
  bd bootstrap --dry-run    # Show what would be done
  bd bootstrap --json       # Output plan as JSON


```
bd bootstrap [flags]
```

**Flags:**

```
      --dry-run   Show what would be done without doing it
```


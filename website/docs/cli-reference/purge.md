---
id: purge
title: bd purge
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc purge` (bd version 0.59.0)

## bd purge

Permanently delete closed ephemeral beads and their associated data.

Closed ephemeral beads (wisps, transient molecules) accumulate rapidly and
have no value once closed. This command removes them to reclaim storage.

Deletes: issues, dependencies, labels, events, and comments for matching beads.
Skips: pinned beads (protected).

EXAMPLES:
  bd purge                           # Preview what would be purged
  bd purge --force                   # Delete all closed ephemeral beads
  bd purge --older-than 7d --force   # Only purge items closed 7+ days ago
  bd purge --pattern "*-wisp-*"      # Only purge matching ID pattern
  bd purge --dry-run                 # Detailed preview with stats

```
bd purge [flags]
```

**Flags:**

```
      --dry-run             Preview what would be purged with stats
  -f, --force               Actually purge (without this, shows preview)
      --older-than string   Only purge beads closed more than N ago (e.g., 7d, 2w, 30)
      --pattern string      Only purge beads matching ID glob pattern (e.g., *-wisp-*)
```


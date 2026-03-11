---
id: history
title: bd history
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc history` (bd version 0.59.0)

## bd history

Show the complete version history of an issue, including all commits
where the issue was modified.

This command requires the Dolt storage backend. If you're using SQLite,
you'll see an error message suggesting to use Dolt for versioning features.

Examples:
  bd history bd-123           # Show all history for issue bd-123
  bd history bd-123 --limit 5 # Show last 5 changes

```
bd history <id> [flags]
```

**Flags:**

```
      --limit int   Limit number of history entries (0 = all)
```


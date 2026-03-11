---
id: orphans
title: bd orphans
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc orphans` (bd version 0.59.0)

## bd orphans

Identify orphaned issues - issues that are referenced in commit messages but remain open or in_progress in the database.

This helps identify work that has been implemented but not formally closed.

Examples:
  bd orphans              # Show orphaned issues
  bd orphans --json       # Machine-readable output
  bd orphans --details    # Show full commit information
  bd orphans --fix        # Close orphaned issues with confirmation

```
bd orphans [flags]
```

**Flags:**

```
      --details   Show full commit information
  -f, --fix       Close orphaned issues with confirmation
```


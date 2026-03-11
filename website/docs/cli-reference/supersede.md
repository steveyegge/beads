---
id: supersede
title: bd supersede
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc supersede` (bd version 0.59.0)

## bd supersede

Mark an issue as superseded by a newer version.

The superseded issue is automatically closed with a reference to the replacement.
Useful for design docs, specs, and evolving artifacts.

Examples:
  bd supersede bd-old --with bd-new    # Mark bd-old as superseded by bd-new

```
bd supersede <id> --with <new> [flags]
```

**Flags:**

```
      --with string   Replacement issue ID (required)
```


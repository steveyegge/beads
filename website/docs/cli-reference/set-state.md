---
id: set-state
title: bd set-state
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc set-state` (bd version 0.59.0)

## bd set-state

Atomically set operational state on an issue.

This command:
1. Creates an event bead recording the state change (source of truth)
2. Removes any existing label for the dimension
3. Adds the new dimension:value label (fast lookup cache)

State labels follow the convention <dimension>:<value>, for example:
  patrol:active, patrol:muted
  mode:normal, mode:degraded
  health:healthy, health:failing

Examples:
  bd set-state witness-abc patrol=muted --reason "Investigating stuck polecat"
  bd set-state witness-abc mode=degraded --reason "High error rate detected"
  bd set-state witness-abc health=healthy

The --reason flag provides context for the event bead (recommended).

```
bd set-state <issue-id> <dimension>=<value> [flags]
```

**Flags:**

```
      --reason string   Reason for the state change (recorded in event)
```


---
id: slot
title: bd slot
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc slot` (bd version 0.59.0)

## bd slot

Manage slots on agent beads.

Agent beads have named slots that reference other beads:
  hook  - Current work attached to agent's hook (0..1 cardinality)
  role  - Role definition bead (required for agents)

Slots enforce cardinality constraints - the hook slot can only hold one bead.

Examples:
  bd slot show gt-mayor           # Show all slots for mayor agent
  bd slot set gt-emma hook bd-xyz # Attach work bd-xyz to emma's hook
  bd slot clear gt-emma hook      # Clear emma's hook (detach work)

```
bd slot
```

### bd slot clear

Clear a slot on an agent bead.

This detaches whatever bead is currently in the slot.

Examples:
  bd slot clear gt-emma hook   # Detach work from emma's hook
  bd slot clear gt-mayor role  # Clear mayor's role (not recommended)

```
bd slot clear <agent> <slot>
```

### bd slot set

Set a slot on an agent bead.

The slot command enforces cardinality: if the hook slot is already occupied,
the command will error. Use 'bd slot clear' first to detach existing work.

Examples:
  bd slot set gt-emma hook bd-xyz   # Attach bd-xyz to emma's hook
  bd slot set gt-mayor role gt-role # Set mayor's role bead

```
bd slot set <agent> <slot> <bead>
```

### bd slot show

Show all slots on an agent bead.

Displays the current values of all slot fields.

Examples:
  bd slot show gt-emma   # Show emma's slots
  bd slot show gt-mayor  # Show mayor's slots

```
bd slot show <agent>
```


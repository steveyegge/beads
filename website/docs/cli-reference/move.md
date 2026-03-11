---
id: move
title: bd move
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc move` (bd version 0.59.0)

## bd move

Move an issue from one rig to another, updating dependencies.

This command:
1. Creates a new issue in the target rig with the same content
2. Updates dependencies that reference the old ID (see below)
3. Closes the source issue with a redirect note

The target rig can be specified as:
  - A rig name: beads, gastown
  - A prefix: bd-, gt-
  - A prefix without hyphen: bd, gt

Dependency handling for cross-rig moves:
  - Issues that depend ON the moved issue: updated to external refs
  - Issues that the moved issue DEPENDS ON: removed (recreate manually in target)

Note: Labels are copied. Comments and event history are not transferred.

Examples:
  bd move hq-c21fj --to beads     # Move to beads by rig name
  bd move hq-q3tki --to gt-       # Move to gastown by prefix
  bd move hq-1h2to --to gt        # Move to gastown (prefix without hyphen)

```
bd move <issue-id> --to <rig|prefix> [flags]
```

**Flags:**

```
      --keep-open   Keep the source issue open (don't close it)
      --skip-deps   Skip dependency remapping
      --to string   Target rig or prefix (required)
```


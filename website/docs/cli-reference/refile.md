---
id: refile
title: bd refile
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc refile` (bd version 0.59.0)

## bd refile

Move an issue from one rig to another.

This creates a new issue in the target rig with the same content,
then closes the source issue with a reference to the new location.

The target rig can be specified as:
  - A rig name: beads, gastown
  - A prefix: bd-, gt-
  - A prefix without hyphen: bd, gt

Examples:
  bd refile bd-8hea gastown     # Move to gastown by rig name
  bd refile bd-8hea gt-         # Move to gastown by prefix
  bd refile bd-8hea gt          # Move to gastown (prefix without hyphen)

```
bd refile <source-id> <target-rig> [flags]
```

**Flags:**

```
      --keep-open   Keep the source issue open (don't close it)
```


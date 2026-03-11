---
id: ship
title: bd ship
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc ship` (bd version 0.59.0)

## bd ship

Ship a capability to satisfy cross-project dependencies.

This command:
  1. Finds issue with export:<capability> label
  2. Validates issue is closed (or --force to override)
  3. Adds provides:<capability> label

External projects can depend on this capability using:
  bd dep add <issue> external:<project>:<capability>

The capability is resolved when the external project has a closed issue
with the provides:<capability> label.

Examples:
  bd ship mol-run-assignee              # Ship the mol-run-assignee capability
  bd ship mol-run-assignee --force      # Ship even if issue is not closed
  bd ship mol-run-assignee --dry-run    # Preview without making changes

```
bd ship <capability> [flags]
```

**Flags:**

```
      --dry-run   Preview without making changes
      --force     Ship even if issue is not closed
```


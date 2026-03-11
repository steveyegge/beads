---
id: reopen
title: bd reopen
sidebar_position: 80
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc reopen` (bd version 0.59.0)

## bd reopen

Reopen closed issues by setting status to 'open' and clearing the closed_at timestamp.
This is more explicit than 'bd update --status open' and emits a Reopened event.

```
bd reopen [id...] [flags]
```

**Flags:**

```
  -r, --reason string   Reason for reopening
```


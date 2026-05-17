---
id: daemon
title: bd daemon
slug: /cli-reference/daemon
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc daemon`

## bd daemon

Inspect and manage the bdd background daemon (bd daemon stats).

```
bd daemon
```

### bd daemon stats

Show runtime statistics for the bdd background daemon.

When the daemon is not running, the command exits with a non-zero status
and a diagnostic message.

Examples:
  bd daemon stats          # Human-readable output
  bd daemon stats --json   # JSON output for scripting


```
bd daemon stats
```

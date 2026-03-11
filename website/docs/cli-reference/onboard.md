---
id: onboard
title: bd onboard
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc onboard` (bd version 0.59.0)

## bd onboard

Display a minimal snippet to add to AGENTS.md for bd integration.

This outputs a small (~10 line) snippet that points to 'bd prime' for full
workflow context. This approach:

  • Keeps AGENTS.md lean (doesn't bloat with instructions)
  • bd prime provides dynamic, always-current workflow details
  • Hooks auto-inject bd prime at session start

The old approach of embedding full instructions in AGENTS.md is deprecated
because it wasted tokens and got stale when bd upgraded.

```
bd onboard
```


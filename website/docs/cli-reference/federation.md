---
id: federation
title: bd federation
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc federation` (bd version 0.59.0)

## bd federation

Federation commands require CGO and the Dolt storage backend.

This binary was built without CGO support. To use federation features:
  1. Use pre-built binaries from GitHub releases, or
  2. Build from source with CGO enabled

Federation enables synchronized issue tracking across multiple Gas Towns,
each maintaining their own Dolt database while sharing updates via remotes.

```
bd federation
```


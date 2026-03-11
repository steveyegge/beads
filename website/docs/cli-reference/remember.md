---
id: remember
title: bd remember
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc remember` (bd version 0.59.0)

## bd remember

Store a memory that persists across sessions and account rotations.

Memories are injected at prime time (bd prime / gt prime) so you have them
in every session without manual loading.

Examples:
  bd remember "always run tests with -race flag"
  bd remember "Dolt phantom DBs hide in three places" --key dolt-phantoms
  bd remember "auth module uses JWT not sessions" --key auth-jwt

```
bd remember "<insight>" [flags]
```

**Flags:**

```
      --key string   Explicit key for the memory (auto-generated from content if not set)
```


---
sidebar_position: 3
title: Merge Conflicts
description: Resolve JSONL merge conflicts
---

# Merge Conflicts Recovery

This runbook helps you resolve JSONL merge conflicts that occur during Git operations.

## Symptoms

- Git merge conflicts in `.beads/*.jsonl` files
- `bd sync` fails with conflict errors
- Different issue states between clones

## Diagnosis

```bash
# Check for conflicted files
git status

# Look for conflict markers
grep -l "<<<<<<" .beads/*.jsonl
```

## Solution

:::warning
JSONL files are append-only logs. Manual editing requires care.
:::

**Step 1:** Identify conflicted files
```bash
git diff --name-only --diff-filter=U
```

**Step 2:** For each conflicted JSONL file, keep both versions
```bash
# Accept both changes (append-only is safe)
git checkout --ours .beads/issues.jsonl
git add .beads/issues.jsonl
```

**Step 3:** Force rebuild to reconcile
```bash
bd doctor --fix
```

**Step 4:** Verify state
```bash
bd list
bd status
```

**Step 5:** Complete the merge
```bash
git commit -m "Resolved beads merge conflicts"
```

## Prevention

- Sync before and after Git operations
- Use `bd sync` regularly
- Avoid concurrent modifications from multiple clones

---
id: diff
title: bd diff
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc diff` (bd version 0.59.0)

## bd diff

Show the differences in issues between two commits or branches.

This command requires the Dolt storage backend. The refs can be:
- Commit hashes (e.g., abc123def)
- Branch names (e.g., main, feature-branch)
- Special refs like HEAD, HEAD~1

Examples:
  bd diff main feature-branch   # Compare main to feature branch
  bd diff HEAD~5 HEAD           # Show changes in last 5 commits
  bd diff abc123 def456         # Compare two specific commits

```
bd diff <from-ref> <to-ref>
```


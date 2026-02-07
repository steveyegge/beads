---
description: "[DEPRECATED] Synchronize issues with git remote"
argument-hint: [--dry-run] [--message] [--status] [--merge]
---

**DEPRECATED:** `bd sync` is deprecated. Dolt handles sync automatically now.

- For manual export: use `bd export`
- For manual import: use `bd import`
- For flush only: use `bd export`

No manual sync is needed in normal workflows.

## Legacy Usage (Deprecated)

Previously, `bd sync` performed these steps:

1. Export pending changes to JSONL
2. Commit changes to git
3. Pull from remote (with conflict resolution)
4. Import updated JSONL
5. Push local commits to remote

These operations are now handled automatically by the Dolt-backed storage layer.

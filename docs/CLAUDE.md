# Claude Code Setup (User Guide)

This guide is for end users who want to use beads with Claude Code. For contributor architecture notes, see [docs/ARCHITECTURE.md](ARCHITECTURE.md) and [AGENTS.md](../AGENTS.md).

## Quick Setup

```bash
# Initialize in your repo
bd init

# Install Claude Code hooks (global or per-project)
bd setup claude
# or: bd setup claude --project

# Start working
bd ready
bd create "Example task" -p 2
```

## Syncing Changes

```bash
# Export to JSONL only (no git operations)
bd sync

# Full sync (pull → merge → export → commit → push)
bd sync --full

# Local-only export (no git operations)
bd sync --flush-only
```

## Worktrees and Protected Branches

If your main branch is protected, use a sync branch:

```bash
bd init --branch beads-sync
# or later:
bd config set sync.branch beads-sync
```

`bd sync --full` creates and uses an internal worktree at `.git/beads-worktrees/<sync-branch>` and commits issue data there. Your working branch stays untouched.

## About the Daemon

The current CLI runs in direct mode. The `--no-daemon` flag is kept for backward compatibility but has no effect. If you see documentation that instructs `bd daemon ...`, treat it as legacy and use `bd sync --full` (or git hooks) instead.

## Need More Detail?

- Claude integration design notes: [docs/CLAUDE_INTEGRATION.md](CLAUDE_INTEGRATION.md)
- Sync workflow and file roles: [docs/SYNC.md](SYNC.md)
- Worktrees and protected branches: [docs/WORKTREES.md](WORKTREES.md), [docs/PROTECTED_BRANCHES.md](PROTECTED_BRANCHES.md)

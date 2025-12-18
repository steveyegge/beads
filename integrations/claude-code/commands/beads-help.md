# Beads Quick Reference

## description:
Explain how to use beads for cross-session task tracking.

---

## What is Beads?

Beads (`bd`) is a git-backed task tracker designed for AI coding agents. It provides **persistent memory across sessions** - tasks survive between Claude Code conversations.

## Core Workflow

```
/beads-start  →  Work on tasks  →  /beads-end
```

## Essential Commands

| Command | Purpose |
|---------|---------|
| `bd init --quiet` | Initialize beads in a project |
| `bd ready` | Show unblocked tasks ready for work |
| `bd create "title" -d "desc"` | Create a new task |
| `bd show <id>` | View task details |
| `bd update <id> --status in_progress` | Claim a task |
| `bd close <id> --reason "done"` | Complete a task |
| `bd dep add <child> <parent>` | Add dependency (child needs parent first) |
| `bd sync` | Export changes and push to git |
| `bd list` | List all tasks |

## Task IDs

Tasks use hash-based IDs like `bd-a1b2`. These prevent merge conflicts across branches.

## Priority Levels

| Priority | Meaning |
|----------|---------|
| 0 | Critical (security, data loss, broken builds) |
| 1 | High (major features, important bugs) |
| 2 | Medium (default) |
| 3 | Low (polish, optimization) |
| 4 | Backlog (future ideas) |

## Dependencies

Dependencies express "X needs Y first":
```bash
bd dep add bd-abc1 bd-def2  # abc1 is blocked by def2
```

Tasks with unmet dependencies won't appear in `bd ready`.

## Creating Tasks with Context

When you discover work during a session:
```bash
bd create "Found bug in auth" \
  -d "Login fails when email has + character. Found while testing signup flow." \
  -p 1 \
  --json
```

## Session Discipline

1. **Start:** Run `/beads-start` to see where you left off
2. **Work:** Create issues for discovered problems
3. **End:** Run `/beads-end` to sync and push

**Critical:** Work is NOT saved until `git push` succeeds.

## Slash Commands

- `/beads-start` - Check ready tasks, inject context
- `/beads-end` - Sync all work, push to git
- `/beads-help` - This reference

## More Info

- Full docs: https://github.com/steveyegge/beads
- Run `bd help <command>` for detailed command help

# Next Session: Shadowbook Maintenance

## Context

**Shadowbook** — beads fork with drift detection, volatility badges, and Pacman mode.

**Repo:** `specbeads` (github.com/anupamchugh/shadowbook)

## What's Done

- ✅ Snap Streaks (volatility tracking)
- ✅ Spec drift detection
- ✅ `bd reflect` command
- ✅ Pacman Mode Phase 1 + Phase 2
  - `bd pacman` dashboard
  - `bd pacman --pause/--resume/--join`
  - `bd assign <id> --to <agent>`
  - `bd ready --mine`
  - Root command pause warning
  - Auto-score on `bd close`

## Current State

```bash
bd pacman
# DOTS NEARBY: None (all caught up)
```

## Optional: Phase 3 Polish

| Feature | Priority |
|---------|----------|
| Achievements (ghost-buster, speed-run) | P4 |
| `bd pacman --global` multi-project | P4 |
| ASCII art | P4 |

## Quick Reference

```bash
bd pacman                    # Dashboard
bd pacman --pause "reason"   # Stop agents
bd pacman --resume           # Continue
bd assign bd-xxx --to codex  # Hand off task
bd ready --mine              # My assignments
AGENT_NAME=codex bd pacman   # Run as different agent
```

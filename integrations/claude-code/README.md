# Claude Code Integration for Beads

Slash commands for using beads with [Claude Code](https://docs.anthropic.com/en/docs/claude-code), implementing patterns from [Anthropic's guide to effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents).

## Features

- **Context-efficient**: Commands delegate to Task agents, keeping main context clean
- **Session continuity**: Start and end sessions with full context preservation
- **Plan conversion**: Convert Claude Code plans to tracked beads tasks
- **Verification discipline**: Enforce testing before marking tasks complete

## Architecture

These commands use Claude Code's **Task tool** to spawn subagents for beads operations. This keeps the main conversation context clean - only summaries are returned, not raw command output.

```
/beads-start
    │
    ▼
┌─────────────────────┐
│  Task Agent         │  ← Runs bd commands, processes JSON
│  (general-purpose)  │
└─────────────────────┘
    │
    ▼
Summary only returned to main context
```

## Installation

### 1. Install beads CLI

```bash
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash
```

### 2. Copy slash commands

```bash
cp commands/*.md ~/.claude/commands/
```

### 3. Add permission (optional)

Add to `~/.claude/settings.json` under `permissions.allow`:

```json
"Bash(bd:*)"
```

This allows beads commands to run without approval prompts.

## Commands

| Command | Purpose |
|---------|---------|
| `/beads-start` | Session onboarding: verify env, review history, select and claim task |
| `/beads-end` | Session completion: verify work, sync to git, recommend next session |
| `/beads-help` | Quick reference for all beads commands |
| `/beads-init-project` | First-session setup: init beads, create backlog, baseline commit |
| `/plan-to-beads` | Convert Claude Code plan file to beads epic + tasks |

## Workflow

### Starting a new project

```
/beads-init-project
```

Creates beads infrastructure, feature backlog, and baseline commit.

### Daily workflow

```
/beads-start          # Onboard, select task, claim it
... work on task ...
/beads-end            # Verify, sync, push, recommend next
```

### Converting plans to tasks

After exiting Claude Code plan mode:

```
/plan-to-beads        # Converts most recent plan
/plan-to-beads path/to/plan.md  # Or specify path
```

## Patterns Implemented

Based on [Anthropic's engineering blog post](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents):

| Anthropic Pattern | Implementation |
|-------------------|----------------|
| Structured feature list (JSON) | `bd list --json` |
| Progress file | `.beads/*.jsonl` (git-native) |
| Session onboarding protocol | `/beads-start` |
| Incremental work (one feature) | `bd ready` + dependency blocking |
| E2E testing before complete | `/beads-end` verification step |
| Clean handoffs | `bd sync` + mandatory push |
| Initializer agent | `/beads-init-project` |
| Context efficiency | Task agent delegation |

## Requirements

- [beads CLI](https://github.com/steveyegge/beads) v0.30.0+
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
- Git repository

## License

Same as beads (see repository root).

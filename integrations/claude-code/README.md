# Claude Code Integration for Beads

Slash command for converting [Claude Code](https://docs.anthropic.com/en/docs/claude-code) plans to beads tasks.

## Prerequisites

Install beads CLI and Claude Code hooks:

```bash
# Install beads
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

# Install hooks (auto-injects bd workflow context on session start)
bd setup claude
```

The hooks call `bd prime` automatically, providing session start/end guidance. See `bd prime --help` for details.

## Installation

```bash
cp commands/*.md ~/.claude/commands/
```

Optionally add to `~/.claude/settings.json` under `permissions.allow`:

```json
"Bash(bd:*)"
```

## Command

### /plan-to-beads

Converts a Claude Code plan file into a beads epic with tasks.

```
/plan-to-beads                    # Convert most recent plan
/plan-to-beads path/to/plan.md    # Convert specific plan
```

**What it does:**
- Parses plan structure (title, summary, phases)
- Creates an epic for the plan
- Creates tasks from each phase
- Sets up sequential dependencies
- Returns a summary of created issues

**Example output:**
```
Created from: peaceful-munching-spark.md

Epic: Standardize ID Generation (bd-abc)
  ├── Add dependency (bd-def) - ready
  ├── Create ID utility (bd-ghi) - blocked by bd-def
  └── Update schema (bd-jkl) - blocked by bd-ghi

Total: 4 tasks
Run `bd ready` to start.
```

## Architecture

The command uses Claude Code's **Task tool** to spawn a subagent, keeping main context clean:

```
/plan-to-beads
    │
    ▼
┌─────────────────────┐
│  Task Agent         │  ← Parses plan, creates issues
│  (general-purpose)  │
└─────────────────────┘
    │
    ▼
Summary only returned
```

## Related

- `bd prime` - Workflow context (auto-injected via hooks)
- `bd setup claude` - Install/manage hooks
- `bd ready` - Find unblocked work
- `bd sync` - Sync and push changes

## License

Same as beads (see repository root).

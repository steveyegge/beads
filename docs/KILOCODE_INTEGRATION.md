# Kilo Code Integration Guide

This guide explains how to integrate [Kilo Code](https://kilo.ai/) with Beads for AI-assisted coding with issue tracking.

## Overview

Kilo Code is a VS Code extension that provides AI-powered coding assistance with customizable modes. The beads integration uses Kilo Code's **rules system** to provide workflow context.

The beads integration for Kilo Code:
- Creates `.kilocode/rules/bd.md` with bd workflow instructions
- Works with all Kilo Code modes (Code, Ask, Architect, Debug, Orchestrator)
- Supports both project-level and global rules
- Automatically loaded on session start

## Installation

### 1. Install Beads

```bash
# Install beads CLI
go install github.com/steveyegge/beads/cmd/bd@latest

# Initialize in your project
cd your-project
bd init --quiet
```

### 2. Setup Kilo Code Integration

```bash
# Install for current project (recommended)
bd setup kilocode

# Or install globally for all projects
bd setup kilocode --global

# Verify installation
bd setup kilocode --check
```

This creates:
- `.kilocode/rules/bd.md` - bd workflow rules for your project

### 3. Restart Kilo Code

After installing the rules, restart your Kilo Code session for the rules to take effect.

## How It Works

Kilo Code automatically loads rules from `.kilocode/rules/` at the start of each session. The rules are included in the AI's context, ensuring it follows the bd workflow.

### Rule Loading Order

1. Global rules from `~/.kilocode/rules/`
2. Project rules from `.kilocode/rules/`

Project rules take precedence over global rules when there are conflicts.

## Usage Workflow

### Starting Work

```bash
# Check what's available to work on
bd ready

# Claim an issue
bd update bd-42 --status in_progress
```

### Creating Issues

When you find bugs or tasks during development, ask Kilo Code to help create issues:

```
You: I found a bug in the auth code

Kilo Code: Let me create an issue for that.
[Executes: bd create "Fix auth bug" --description="..." -t bug -p 1]
```

### Completing Work

```bash
# Mark issue as complete
bd close bd-42 --reason "Implemented and tested"

# Sync with git remote
bd sync
```

## Configuration Options

### Project Installation (Default)

```bash
bd setup kilocode
```

Creates `.kilocode/rules/bd.md` in your project root. This is the recommended approach for team projects since the rules can be committed to version control.

### Global Installation

```bash
bd setup kilocode --global
```

Creates `~/.kilocode/rules/bd.md` which applies to all projects. Useful for personal workflows where you want bd integration everywhere.

### Check Installation Status

```bash
bd setup kilocode --check
```

Shows which rules are installed (project, global, or both).

### Remove Integration

```bash
# Remove project rules
bd setup kilocode --remove

# Remove global rules
bd setup kilocode --remove --global
```

## Mode-Specific Behavior

Kilo Code's modes all have access to the bd rules:

| Mode | bd Behavior |
|------|-------------|
| **Code** | Full access - can run bd commands |
| **Ask** | Can explain bd workflow but won't modify issues |
| **Architect** | Can plan work and create issues |
| **Debug** | Can create bug issues and update status |
| **Orchestrator** | Can coordinate multi-issue workflows |

## Comparison: Kilo Code vs Other Editors

### Kilo Code (Rules-Based)

- ✅ Rules automatically loaded on session start
- ✅ Works with all Kilo Code modes
- ✅ Supports both project and global rules
- ✅ Rules stored in standard `.kilocode/rules/` directory
- ✅ Version-controllable for team standardization

### Claude Code (Hook-Based)

- ✅ Hooks run `bd prime` on session start and compaction
- ✅ Automatic context refresh
- ⚠️ Requires hook configuration

### Cursor (Rules-Based)

- ✅ Similar rules-based approach
- ✅ Rules in `.cursor/rules/`
- ⚠️ Project-level only (no global option)

## Team Usage

For team projects, commit the rules to version control:

```bash
# Setup once
bd setup kilocode

# Commit the rules
git add .kilocode/rules/bd.md
git commit -m "Add bd workflow rules for Kilo Code"
```

All team members using Kilo Code will automatically get the bd workflow rules.

## Troubleshooting

### "bd commands aren't recognized"

1. Check that rules are installed:
   ```bash
   bd setup kilocode --check
   ```

2. Restart Kilo Code to reload rules

3. Verify beads is initialized:
   ```bash
   bd doctor
   ```

### "Rules aren't being applied"

1. Check the rules file exists:
   ```bash
   cat .kilocode/rules/bd.md
   ```

2. Ensure you're in a directory where the rules apply

3. Try the global installation if project rules aren't working:
   ```bash
   bd setup kilocode --global
   ```

### "I want to customize the rules"

The rules file is plain markdown. You can edit `.kilocode/rules/bd.md` to add project-specific instructions:

```markdown
# Additional Project Rules

## Code Style
- Use TypeScript for all new files
- Follow the project's ESLint configuration

## Testing
- All new features require unit tests
- Run `npm test` before marking issues complete
```

### "Remove the integration"

```bash
# Remove project rules
bd setup kilocode --remove

# Or remove global rules
bd setup kilocode --remove --global
```

## Advanced Usage

### Custom Modes

If you create custom Kilo Code modes, they will automatically have access to the bd rules. No additional configuration is needed.

### Mode-Specific Rules

Kilo Code supports mode-specific rules via `.kilocode/rules-${mode}/`. For example, to have debug-specific bd instructions:

```bash
mkdir -p .kilocode/rules-debug
cp .kilocode/rules/bd.md .kilocode/rules-debug/bd.md
# Edit .kilocode/rules-debug/bd.md for debug-specific workflow
```

### Integration with Git Hooks

bd automatically syncs with git via hooks. Combined with Kilo Code's git support:

1. Work on issue: `bd update bd-42 --status in_progress`
2. Make changes with Kilo Code
3. Commit changes (bd auto-exports issues to JSONL)
4. Complete issue: `bd close bd-42`
5. Push to remote

## Example Session

```
You: What issues are ready to work on?

Kilo Code: Let me check...
[Runs: bd ready]

📋 Ready work (3 issues with no blockers):
1. [P1] bd-42: Fix login bug
2. [P2] bd-99: Add password reset
3. [P3] bd-17: Refactor auth tests

You: Let's work on bd-42

Kilo Code: I'll claim that issue.
[Runs: bd update bd-42 --status in_progress]

✓ Updated bd-42 status to in_progress

You: Show me the issue details

Kilo Code: [Runs: bd show bd-42]

[Shows issue details]

You: [After implementing the fix] Great, the bug is fixed

Kilo Code: I'll mark the issue as complete.
[Runs: bd close bd-42 --reason "Fixed login bug"]

✓ Closed bd-42

Don't forget to sync before ending the session:
[Runs: bd sync]
```

## References

- [Kilo Code Documentation](https://kilo.ai/docs)
- [Beads Documentation](https://github.com/steveyegge/beads)
- [AGENTS.md](../AGENTS.md) - Complete bd workflow guide
- [QUICKSTART.md](QUICKSTART.md) - Quick start guide

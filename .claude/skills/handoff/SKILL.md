---
name: handoff
description: >
  Hand off to a fresh Claude session. Use when context is full, you've finished
  a logical chunk of work, or need a fresh perspective. Work continues from hook.
allowed-tools: "Bash(gt handoff:*),Bash(gt mail send:*)"
version: "1.0.0"
author: "Gas Town"
---

# Handoff - Session Cycling for Gas Town Agents

Hand off your current session to a fresh Claude instance while preserving work context.

## When to Use

- Context getting full (approaching token limit)
- Finished a logical chunk of work
- Need a fresh perspective on a problem
- Human requests session cycling

## Usage

```
/handoff [optional message]
```

## How It Works

1. If you provide a message, it's sent as handoff mail to yourself
2. `gt handoff` respawns your session with a fresh Claude
3. New session auto-primes via SessionStart hook
4. Work continues from your hook (pinned molecule persists)

## Examples

```bash
# Simple handoff (molecule persists, fresh context)
/handoff

# Handoff with context notes
/handoff "Found the bug in token refresh - check line 145 in auth.go first"
```

## What Persists

- **Hooked molecule**: Your work assignment stays on your hook
- **Beads state**: All issues, dependencies, progress
- **Git state**: Commits, branches, staged changes

## What Resets

- **Conversation context**: Fresh Claude instance
- **TodoWrite items**: Ephemeral, session-scoped
- **In-memory state**: Any uncommitted analysis

## Implementation

When invoked, execute:

1. If user provided a message, send handoff mail:
   ```bash
   gt mail send <your-address> -s "HANDOFF: Session cycling" -m "<message>"
   ```

2. Run the handoff command:
   ```bash
   gt handoff
   ```

The new session will find your handoff mail and hooked work automatically.

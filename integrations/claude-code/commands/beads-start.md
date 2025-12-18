# Start Session with Full Onboarding

## description:
Full session onboarding: verify environment, review history, select work, claim task.

---

## Session Onboarding Protocol

Based on [Anthropic's long-running agent patterns](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents).

### Step 1: Verify Environment

```bash
pwd
git status
```

Check:
- Correct working directory?
- Any uncommitted changes from previous session?
- Clean working tree?

If uncommitted changes exist, ask whether to:
1. Commit them now
2. Stash them
3. Continue anyway

### Step 2: Check Beads Status

```bash
bd info 2>/dev/null || echo "NO_BEADS"
```

**If NO_BEADS:** Ask if user wants to initialize with `bd init --quiet`

**If beads exists:** Continue to Step 3.

### Step 3: Review Recent History

```bash
git log --oneline -5
bd info --whats-new
```

Present:
- Last 5 commits (what was done recently)
- Any beads changes since last session

### Step 4: Understand Current State

```bash
bd list --status open --json
bd blocked
```

Present summary:
- Total open tasks
- Tasks by priority (P0, P1, P2, etc.)
- What's blocking what

### Step 5: Select Work

```bash
bd ready --json
```

**If no ready tasks:**
- Show blocked tasks and what they're waiting on
- Ask if user wants to create new work

**If ready tasks exist:**
- List all ready tasks by priority
- Recommend the highest priority one
- Show full context: `bd show <recommended-id>`

### Step 6: Claim Task

Once user confirms which task to work on:
```bash
bd update <id> --status in_progress --json
```

---

## Output Format

```
Session Start: [project name]
Directory: /path/to/project
Git: [branch] - [clean/dirty]

Recent commits:
  abc1234 feat: added login form
  def5678 fix: validation bug
  ...

Beads status:
  Open: [N] tasks ([X] P1, [Y] P2, [Z] P3)
  Blocked: [M] tasks
  Ready: [K] tasks

Recommended task:
  [.proj-xxx] [P1] Add user authentication
  Description: [first 100 chars...]
  Blocked by: nothing (ready to start)

Claim this task? (bd update .proj-xxx --status in_progress)
```

---

## Quick Start Alternative

For returning to a known task:
```bash
bd ready          # See what's unblocked
bd show <id>      # Review specific task
bd update <id> --status in_progress
```

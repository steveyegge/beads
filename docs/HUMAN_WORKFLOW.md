# Beads Workflow: Human Operator Guide

## Overview

Beads is a **delegation tool for AI agents**, not a human-operated issue tracker. Humans set it up once, then direct agents to use it. The human's role is oversight and direction, not operation.

## Using Beads in External Repos

### Setup (One-Time)
```bash
bd init            # Initialize beads in any repo
bd hooks install   # Enable automatic git sync
```

Add to project's `AGENTS.md`:
```
Use `bd` commands instead of markdown TODOs for task tracking.
```

### Workflow Modes

| Mode | Command | Use Case |
|------|---------|----------|
| Solo | `bd init` | Personal projects |
| Team | `bd init --team` | Shared repo, real-time sync via git |
| Contributor | `bd init --contributor` | OSS work - keeps beads out of upstream PRs |

---

## Human-Agent Rituals

### 1. Session Start (Automatic)
**What happens:** Agent receives workflow context automatically via `bd prime`

**Human action:** None - hooks inject context when agent session starts

**Agent receives:**
- Available work (`bd ready`)
- Recent activity
- Workflow quick-reference

---

### 2. Work Assignment
**What happens:** Human directs agent to work on issues

**Human action:** Tell agent what to work on, or ask agent to pick from ready work

**Example prompts:**
- "Work on bd-123"
- "What's ready to work on?"
- "Create a task to refactor the auth module"

---

### 3. Progress Monitoring
**What happens:** Human checks status mid-session or across sessions

**Human actions:**
```bash
bd list              # All issues
bd ready             # Unblocked work
bd show <id>         # Issue details
bd monitor           # Web dashboard (real-time)
```

---

### 4. Discovery Tracking
**What happens:** Agent finds new work while implementing something else

**Human expectation:** Agent creates linked issues with `discovered-from` dependency

**Why it matters:** Maintains causality chain, keeps discovered work in same repo as parent

---

### 5. Session End ("Landing the Plane")
**What happens:** Agent wraps up and syncs all work

**Human expectation:** This is **non-negotiable** - agent MUST:
1. Close completed issues
2. Create issues for remaining TODOs
3. Run `bd sync` to flush changes
4. Push to remote (`git push`)
5. Verify clean state (`git status` shows "up to date")

**Why it matters:** Unpushed work breaks multi-agent coordination. Work is NOT done until pushed.

---

### 6. Session Handoff
**What happens:** Agent provides context for next session/agent

**Human expectation:** Agent gives resumable prompt like:
> "Continue work on bd-123: Implement auth middleware. Backend logic complete, need to add tests."

**Next session:** New agent runs `bd prime` and picks up where previous left off.

---

## Sync Model

```
Agent -> SQLite (local) -> JSONL (git-tracked) -> Remote
```

- **Automatic:** Git hooks sync on commit, merge, push, checkout
- **Manual:** Agent runs `bd sync` at session end (forces immediate flush)
- **Conflict-free:** Hash-based IDs prevent collisions

---

## Human Oversight Points

| When | Human Can |
|------|-----------|
| Anytime | Run `bd list`, `bd ready`, `bd monitor` |
| Between sessions | Review what agents accomplished |
| Planning | Ask agent to create/organize issues |
| Handoff | Review agent's summary before next session |

---

## Troubleshooting for Humans

These are the situations where human intervention may be needed. For detailed technical solutions, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

### "Agent says work is done but I don't see it"

**Likely cause:** Agent didn't push to remote.

**Check:**
```bash
git status          # Should say "up to date with origin"
bd list             # See what's tracked locally
```

**If behind remote:**
```bash
git pull
bd sync
```

**Prevention:** Remind agent that work isn't complete until pushed.

---

### "Two agents worked on the same issue"

**Symptom:** Conflicting changes or lost work.

**Resolution:**
1. Check git history: `git log --oneline .beads/`
2. If conflict in JSONL, keep the version with newer `updated_at`
3. Re-import: `bd import -i .beads/issues.jsonl`

**Prevention:** Have agents claim work with `bd update <id> --status in_progress`

---

### "Agent created duplicate issues"

**Check for duplicates:**
```bash
bd duplicates                    # List potential duplicates
bd duplicates --auto-merge       # Merge them automatically
```

**Prevention:** Instruct agents to search before creating: `bd list --json | grep "keyword"`

---

### "I pulled changes but bd shows old data"

**Cause:** Auto-import didn't trigger (rare).

**Fix:**
```bash
bd sync           # Force import from JSONL
```

If hooks aren't installed:
```bash
bd hooks install  # Enable automatic sync
```

---

### "bd commands fail with 'database locked'"

**Cause:** Multiple processes accessing database, or unclean shutdown.

**Quick fix:**
```bash
bd daemons killall    # Stop all daemons
bd ready              # Restarts automatically
```

See [TROUBLESHOOTING.md#database-issues](TROUBLESHOOTING.md#database-issues) for details.

---

## Edge Cases

### Multiple Repos on Same Machine

Each repo has its own isolated beads database:
```bash
cd ~/project-a && bd list    # Uses project-a/.beads/
cd ~/project-b && bd list    # Uses project-b/.beads/
```

No cross-contamination. Multiple agents can work on different repos simultaneously.

---

### Branch-Scoped Work

Issues live in the branch where they're created:
- Create issue on `feature-x` branch -> issue exists in that branch
- Merge `feature-x` to `main` -> issue now in `main`
- Different branches can have different issue states

**Best practice:** Do major issue reorganization on `main`, not feature branches.

---

### Recovering from Bad State

If beads data seems corrupted or inconsistent:

```bash
# JSONL is source of truth (lives in git)
# Database is just a cache

# Option 1: Re-import from JSONL
bd import -i .beads/issues.jsonl --force

# Option 2: Full reset (preserves JSONL history in git)
rm .beads/*.db
bd init
bd import -i .beads/issues.jsonl
```

---

### Working Offline

Beads works fully offline:
- All commands query local SQLite
- Sync happens when you `git push/pull`
- Agents can work without network access

**Caveat:** Multiple offline agents may create conflicting work. Resolve after reconnecting.

---

### Contributor Mode (OSS Workflows)

When contributing to repos you don't own:

```bash
bd init --contributor
```

This:
- Keeps beads data in `~/.beads-planning/` (not the repo)
- Your issues never pollute upstream PRs
- Clean commits, clean history

---

## Key Principle

**Humans direct, agents operate.** The human's job is to:
1. Set up beads once (`bd init`)
2. Tell agents what to work on
3. Check progress when curious
4. Trust the sync protocol

Everything else is automated via hooks and agent discipline.

---

## Related Documentation

- [QUICKSTART.md](QUICKSTART.md) - Getting started guide
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - Technical problem solving
- [FAQ.md](FAQ.md) - Common questions
- [ADVANCED.md](ADVANCED.md) - Power user features
- [GIT_INTEGRATION.md](GIT_INTEGRATION.md) - Git sync details

# THE BEADS HARNESS PATTERN

## Safe Yolo Sessions with Git-Backed Domain Memory

This document synthesizes three sources into a complete workflow pattern:

1. **Anthropic's Research**: ["Effective Harnesses for Long-Running Agents"](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
2. **Nate B. Jones's Commentary**: "AI Agents That Actually Work: The Pattern Anthropic Just Revealed"
3. **Beads Tooling**: This repository's git-backed issue tracker

The result is a practical implementation guide for running coding agents in yolo mode (`--dangerously-skip-permissions`) safely, using Beads as the domain memory layer and git as the safety net.

---

## Part 1: Core Philosophy

### The Amnesia Problem

Every coding agent session starts fresh. Without external memory, agents either:
- **One-shot** everything (exhaust context, leave mess)
- **Declare premature victory** (see partial progress, assume done)  
- **Guess at prior state** (waste tokens, introduce inconsistency)

Anthropic observed these exact failure modes:
> "Claude's failures manifested in two patterns. First, the agent tended to try to do too much at once—essentially to attempt to one-shot the app. [...] A second failure mode would often occur later in a project. After some features had already been built, a later agent instance would look around, see that progress had been made, and declare the job done."

### The Solution: Domain Memory

Instead of fighting the amnesia, embrace it. The agent doesn't need memory — it needs a **STAGE** to perform on. Domain memory is:

- **Persistent** — survives sessions
- **Structured** — machine-readable goals, status, constraints
- **External** — not in context window
- **Versioned** — git-backed, rollback-able

As Nate Jones puts it:
> "The agent is now just a policy that transforms one consistent memory state into another. The magic is in the memory. The magic is in the harness. The magic is not in the personality layer."

### The Beads Implementation

Beads provides domain memory as a graph-based issue tracker:

| Anthropic's Ad-Hoc Files | Beads Equivalent |
|--------------------------|------------------|
| `feature_list.json` with `passes: false/true` | `bd list --status open/closed` |
| `claude-progress.txt` | Issue notes + audit trail |
| Manual JSON editing | CLI commands (less error-prone) |
| No dependency tracking | 4 dependency types (blocks, parent-child, related, discovered-from) |
| No ready work detection | `bd ready` auto-filters unblocked work |
| Single-agent only | Multi-agent via hash IDs + Agent Mail |

### The Safety Model

Safety doesn't require sandboxing when you have:

1. **Scoped tasks** — one issue per session
2. **Observable state** — beads + git show everything
3. **Reversible changes** — git reset/revert
4. **Structured rituals** — bootup, landing

---

## Part 2: Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     HUMAN OPERATOR                          │
│  (Strategic orchestrator: priorities, approvals, triage)    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    BEADS (Domain Memory)                    │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐        │
│  │ Issues  │  │  Deps   │  │  Ready  │  │  Audit  │        │
│  │ (goals) │  │ (order) │  │ (queue) │  │ (trail) │        │
│  └─────────┘  └─────────┘  └─────────┘  └─────────┘        │
│                    ▲                                        │
│                    │ bd sync                                │
│                    ▼                                        │
│              .beads/beads.jsonl ◄──► Git                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              CLAUDE CODE (Yolo Mode)                        │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Session Lifecycle:                                   │  │
│  │  1. Boot → Read beads → Orient                       │  │
│  │  2. Pick ONE issue from bd ready                     │  │
│  │  3. Execute → Test → Update beads                    │  │
│  │  4. Commit → Sync → Land                             │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      GIT (Safety Net)                       │
│  • Atomic commits (reversible)                              │
│  • History (reconstruction)                                 │
│  • Reset/revert (recovery)                                  │
└─────────────────────────────────────────────────────────────┘
```

---

## Part 3: The Two Agent Roles

### INITIALIZER ROLE (Run once per project/epic)

**Purpose:** Transform vague goal into structured domain memory

**Inputs:**
- High-level user vision
- Project constraints
- Existing codebase context

**Outputs:**
- Beads epic with child task issues
- All issues initially `status=open`
- Dependency graph (blocks relationships)

The initializer doesn't need long-term memory — it's a one-shot transformation of prompt → structured issues.

Anthropic describes this role:
> "The very first agent session uses a specialized prompt that asks the model to set up the initial environment: an `init.sh` script, a claude-progress.txt file that keeps a log of what agents have done, and an initial git commit that shows what files were added."

### CODING ROLE (Run every session)

**Purpose:** Transform domain memory state by completing one task

**Inputs:**
- Beads state (via `bd ready`, `bd show`)
- Git state (via `git log`, `git status`)
- Codebase

**Outputs:**
- Code changes (tested, working)
- Updated beads state (closed issues, new discovered issues)
- Git commits (atomic, documented)
- Handoff context for next session

The coding agent is stateless — all memory comes from beads + git.

---

## Part 4: Implementation Phases

### PHASE 0: Project Setup (One-time)

```bash
# 1. Install beads
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

# 2. Initialize in project root
cd ~/projects/your-project
bd init --quiet

# 3. Install git hooks (critical for sync discipline)
bd hooks install

# 4. Verify
bd doctor
bd info --json
```

### PHASE 1: Initialization Session

This is the "Initializer Agent" role. Run as a dedicated Claude session:

**Human prompt:**
```
I'm starting a new project/epic. Here's my vision: [description]

Your job is to expand this into a structured feature breakdown using beads.
Create an epic issue and child tasks. All should be status=open.
Use priority 0-4 (0=critical, 4=nice-to-have).
Establish blocking dependencies where order matters.
Do NOT start implementation — only create the issue structure.
```

**Agent actions:**
```bash
# Create epic
bd create "Feature System Name" -t epic -p 1 --json
# Returns bd-a1b2

# Create child tasks
bd create "Subtask one" -t task -p 1 --json
bd create "Subtask two" -t task -p 1 --json  
bd create "Subtask three" -t task -p 2 --json

# Establish dependencies
bd dep add bd-a1b3 bd-a1b2 --type parent-child
bd dep add bd-a1b5 bd-a1b3 --type blocks  # Task blocked by prior task

# Sync to git
bd sync
git push
```

**Result:** Domain memory scaffold exists. Every subsequent session has a "stage."

---

### PHASE 2: Session Start Ritual

Every yolo session **MUST** begin with this bootup sequence:

```bash
# 1. GROUND: Where am I?
pwd
git status

# 2. SYNC: Pull remote state
git pull --rebase
bd sync

# 3. ORIENT: Read the memory
bd ready --json              # What's unblocked?
bd list --status in_progress # Any WIP from last session?
git log --oneline -10        # Recent history

# 4. SELECT: Pick ONE issue
bd show bd-XXXX --json       # Full context on chosen issue
bd dep tree bd-XXXX          # Dependency context

# 5. VERIFY: Run basic health check
# (run tests/build to confirm clean starting state)
```

Only AFTER this ritual does the agent begin implementation work.

Anthropic describes this as:
> "Every coding agent is prompted to run through a series of steps to get its bearings: Run `pwd` to see the directory you're working in. Read the git logs and progress files to get up to speed on what was recently worked on. Read the features list file and choose the highest-priority feature that's not yet done to work on."

---

### PHASE 3: Execution (Yolo Mode)

With `--dangerously-skip-permissions` active:

**Scope Constraint:**
> "I am working on bd-XXXX and ONLY bd-XXXX. Any tangential work I discover gets filed as a new issue, not implemented now."

**Work Pattern:**
1. Implement the feature/fix
2. Write/update tests
3. Run tests to verify
4. If tests pass → continue
5. If tests fail → debug, iterate
6. Commit atomically with clear message referencing issue ID

**Discovered Work:**
```bash
# Found a bug while working on bd-a1b3
bd create "Bug: Connection drops after 30min" -t bug -p 1 \
  --deps discovered-from:bd-a1b3 --json
# Automatically inherits source_repo, linked to parent
# Do NOT fix now — file and continue with original issue
```

**Progress Updates:**
```bash
bd update bd-a1b3 --status in_progress
bd update bd-a1b3 --notes "Completed connection layer, working on data parsing"
```

---

### PHASE 4: Session End Ritual ("Landing the Plane")

This is **NON-NEGOTIABLE**. The session is not complete until all steps finish:

```bash
# 1. FILE remaining work
bd create "TODO: Add retry logic" -t task -p 2 \
  --deps discovered-from:bd-a1b3

# 2. RUN quality gates (if code changed)
go test ./...           # or npm test, pytest, etc.
golangci-lint run ./... # or eslint, etc.

# 3. UPDATE beads state
bd close bd-a1b3 --reason "Implementation complete. Tests passing."
bd update bd-a1b5 --status in_progress --notes "60% complete"

# 4. SYNC and PUSH (MANDATORY)
bd sync
git status  # MUST show "up to date with origin"

# If sync fails due to conflicts:
git checkout --theirs .beads/beads.jsonl
bd import -i .beads/beads.jsonl
bd sync

# 5. VERIFY clean state
git status              # Clean working tree
bd ready --json         # Shows next available work

# 6. GENERATE handoff prompt
```

**Handoff Prompt Template:**
```
Continue work on bd-{id}: {title}

Context: {issue description}
Status: {current status and notes}
Blockers: {any blocking issues}
Last commit: {git log -1 --oneline}
Next step: {what to do next}

Start session with:
  git pull && bd sync && bd show bd-{id} --json
```

---

## Part 5: CLAUDE.md Template

Add this to your project's `CLAUDE.md` file:

```markdown
# CLAUDE.md - Beads Harness Configuration

## Session Protocol

### MANDATORY: Session Start Ritual
Before ANY work, execute this sequence:
1. `pwd` - confirm working directory
2. `git status` - check for uncommitted changes
3. `git pull --rebase` - get remote changes
4. `bd sync` - sync beads state
5. `bd ready --json` - identify available work
6. `bd show <issue-id> --json` - load full context
7. Run health check (tests/build) to confirm clean state

### MANDATORY: Session End Ritual
Before ending ANY session:
1. File discovered work as new issues with `discovered-from` dependency
2. Run quality gates if code changed
3. Close completed issues: `bd close <id> --reason "..."`
4. Update in-progress issues with notes
5. `bd sync` - MUST complete successfully
6. `git push` - MUST complete successfully  
7. `git status` - MUST show "up to date with origin"
8. Generate handoff prompt for next session

### Scope Discipline
- Work on ONE issue per session (from `bd ready`)
- Do NOT implement discovered work — file it as new issue
- Do NOT close issues without verified passing tests
- Do NOT skip the landing ritual

### Git Discipline
- Commit atomically with issue ID: "bd-a1b3: Implement feature X"
- Never leave uncommitted changes at session end
- Use `bd sync` to ensure beads state is committed
```

---

## Part 6: Failure Recovery Procedures

| Scenario | Recovery |
|----------|----------|
| Session ended without landing | `bd sync && git add .beads/ && git commit -m "Recover beads" && git push` |
| Agent made destructive changes | `git revert HEAD` or `git reset --hard HEAD~1` |
| Beads merge conflict | `git checkout --theirs .beads/beads.jsonl && bd import -i .beads/beads.jsonl` |
| Agent closed issue without testing | `bd update bd-XXXX --status open --notes "Reopened: tests not verified"` |
| Abandon session entirely | `git stash push -m "Abandoned" && bd import -i .beads/beads.jsonl` |

---

## Part 7: Quick Reference Card

**Session Start:**
```bash
pwd && git pull && bd sync && bd ready --json && bd show <id> --json
```

**During Work:**
```bash
bd update <id> --status in_progress
bd create "Discovered: ..." --deps discovered-from:<id>
git commit -m "bd-<id>: <message>"
```

**Session End:**
```bash
bd close <id> --reason "..."
bd sync && git push && git status
# Generate handoff prompt
```

**Recovery:**
```bash
git reset --hard HEAD~1
bd import -i .beads/beads.jsonl
```

---

## Part 8: Why This Pattern Works

| Traditional YOLO Risk | Beads Harness Mitigation |
|----------------------|-------------------------|
| Agent does too much | Scope limited to ONE issue |
| Changes can't be undone | Git history, atomic commits |
| Lost context between sessions | Beads + git = persistent memory |
| No visibility into agent work | Audit trail, commit messages |
| Multi-agent conflicts | Hash-based IDs, Agent Mail |

**The Moat:**
> "The moat isn't a smarter AI agent — it's your domain memory and harness." — Nate Jones

By investing in beads setup and session rituals, you build:
- Consistent, repeatable agent behavior
- Auditable work history
- Safe autonomous execution
- Multi-session continuity
- Team/multi-agent scalability

---

## Part 9: Key Insights from Source Material

### From Anthropic's Research

**The Feature List Structure:**
```json
{
  "category": "functional",
  "description": "New chat button creates a fresh conversation",
  "steps": [
    "Navigate to main interface",
    "Click the 'New Chat' button",
    "Verify a new conversation is created"
  ],
  "passes": false
}
```

**Why JSON over Markdown:**
> "We use strongly-worded instructions like 'It is unacceptable to remove or edit tests because this could lead to missing or buggy functionality.' After some experimentation, we landed on using JSON for this, as the model is less likely to inappropriately change or overwrite JSON files compared to Markdown files."

**The Testing Gap:**
> "One final major failure mode that we observed was Claude's tendency to mark a feature as complete without proper testing. Absent explicit prompting, Claude tended to make code changes, and even do testing with unit tests or `curl` commands against a development server, but would fail to recognize that the feature didn't work end-to-end."

### From Nate Jones's Commentary

**Domain Memory Definition:**
> "Domain memory is not 'we have a vector database and we go and get stuff out of the vector database.' Instead, it's a persistent structured representation of the work."

**The Key Insight:**
> "If you have no shared feature list, every run will rederive its own definition of done. If you have no durable progress log, every run will guess what happened wrongly."

**Design Principles:**
1. Externalize the goal — turn "do X" into machine-readable backlog
2. Make progress atomic and observable
3. Enforce "leave campsite cleaner" discipline
4. Standardize the bootup ritual
5. Keep tests close to memory

---

## References

- [Anthropic: Effective Harnesses for Long-Running Agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
- [Claude Agent SDK Quickstart](https://github.com/anthropics/claude-quickstarts/tree/main/autonomous-coding)
- [Beads Issue Tracker](https://github.com/steveyegge/beads)
- Nate B. Jones: "AI Agents That Actually Work: The Pattern Anthropic Just Revealed" (YouTube)

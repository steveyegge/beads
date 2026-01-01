# Beads Philosophy — Design Rationale and Principles

> **The moat isn't a smarter AI agent — it's your domain memory and harness.**
> — Nate B. Jones

This document explains the "why" behind the beads harness pattern. For operational reference, see [../CLAUDE.md](../CLAUDE.md).

---

## The Amnesia Problem

Every AI agent session starts fresh. Without external memory, agents either:
- **One-shot** everything (exhaust context, leave a mess)
- **Declare premature victory** (see partial progress, assume done)
- **Guess at prior state** (waste tokens, introduce inconsistency)

## The Solution: Domain Memory

The agent doesn't need memory — it needs a **STAGE** to perform on.

> "The agent is now just a policy that transforms one consistent memory state into another. The magic is in the memory. The magic is in the harness. The magic is not in the personality layer."

**Beads** provides that stage:
- **Persistent** — survives sessions (git-backed JSONL)
- **Structured** — machine-readable goals, status, dependencies
- **External** — not in context window
- **Versioned** — git history enables rollback

## The Safety Model

Safety doesn't require sandboxing when you have:
1. **Scoped tasks** — one issue per session
2. **Observable state** — beads + git show everything
3. **Reversible changes** — git reset/revert
4. **Structured rituals** — bootup and landing protocols

---

## Architecture Overview

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
│              .beads/issues.jsonl ◄──► Git                   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    CLAUDE (Agent)                           │
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

## The Two Agent Roles

### INITIALIZER (Run once per project/epic)

**Purpose**: Transform vague goal into structured domain memory.

```bash
# Create epic
bd create "User Authentication System" -t epic -p 1 --json

# Create child tasks
bd create "Add user model" -t task -p 1 --deps parent-child:bd-epic
bd create "Add login endpoint" -t task -p 1 --deps parent-child:bd-epic
bd create "Add registration endpoint" -t task -p 1 --deps parent-child:bd-epic

# Establish blocking dependencies
bd dep add login-endpoint user-model --type blocks
bd dep add registration-endpoint user-model --type blocks

# Sync
bd sync && git push
```

**Result**: Domain memory scaffold exists. Every subsequent session has a stage.

### CODER (Run every session)

**Purpose**: Transform domain memory state by completing one task.

1. Run session start ritual
2. Pick ONE issue from `bd ready`
3. Implement, test, iterate
4. File any discovered work (don't fix it)
5. Run session end ritual
6. Generate handoff prompt

---

## A Session in Practice — Narrative Walkthrough

This section follows a hypothetical agent session from start to finish.

### The Setup

**Project**: An authentication microservice for a SaaS platform.
**Epic**: `bd-001` — "User Authentication System"
**Available tasks**: Three child issues created by an initializer session.

### Session Start (9:00 AM)

Claude begins a new coding session. First, the mandatory bootup ritual:

```bash
$ pwd
/home/dev/auth-service

$ git status
On branch main. Your branch is up to date with 'origin/main'.

$ git pull --rebase
Already up to date.

$ bd sync
Imported 3 issues from .beads/issues.jsonl

$ bd ready --json
[
  {"id": "bd-002", "title": "Add user model", "priority": 1, "status": "open"},
  {"id": "bd-003", "title": "Add login endpoint", "priority": 1, "status": "open", "blocked_by": ["bd-002"]},
  {"id": "bd-004", "title": "Add registration endpoint", "priority": 1, "status": "open", "blocked_by": ["bd-002"]}
]
```

Only `bd-002` is unblocked — the other two depend on it. Claude claims it:

```bash
$ bd update bd-002 --status in_progress
Updated bd-002: status -> in_progress

$ bd show bd-002 --json
{
  "id": "bd-002",
  "title": "Add user model",
  "description": "Create User struct with fields for id, email, password_hash, created_at.",
  "type": "task",
  "priority": 1,
  "status": "in_progress"
}
```

**Stage is set.** Claude knows exactly what to do.

### Implementation (9:05 - 9:45 AM)

Claude creates the user model:

```go
// models/user.go
type User struct {
    ID           uuid.UUID `db:"id" json:"id"`
    Email        string    `db:"email" json:"email"`
    PasswordHash string    `db:"password_hash" json:"-"`
    CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

func (u *User) Validate() error {
    if u.Email == "" {
        return errors.New("email is required")
    }
    // ... validation logic
}
```

While writing validation, Claude notices the email regex is duplicated elsewhere. **This is discovered work** — file it, don't fix it:

```bash
$ bd create "Refactor: Extract email validation to shared util" \
    --description="Email regex pattern duplicated in models/user.go and handlers/contact.go.

    Found while implementing user model (bd-002).

    Approach: Create pkg/validation/email.go with shared ValidateEmail function." \
    -t task -p 3 \
    --deps discovered-from:bd-002

Created bd-005: "Refactor: Extract email validation to shared util"
```

Claude continues with the original task — **single-issue discipline maintained**.

Tests are written:

```bash
$ go test ./models/...
ok      auth-service/models    0.042s
```

Migration added, tested locally. The user model is complete.

### Landing the Plane (9:45 AM)

Work is done. Time for the session end ritual:

```bash
# Close the completed issue
$ bd close bd-002 --reason "User model implemented with validation and migration. Tests passing."
Closed bd-002

# Sync beads state
$ bd sync
Exported 5 issues to .beads/issues.jsonl

# Commit everything
$ git add -A
$ git commit -m "bd-002: Add user model with validation and migration

Implements User struct with:
- UUID primary key
- Email with validation
- Password hash (bcrypt)
- Created timestamp

Includes migration 001_create_users.sql and unit tests."

# Push to remote — THE PLANE LANDS HERE
$ git push
To github.com:example/auth-service.git
   abc1234..def5678  main -> main

# Verify clean state
$ git status
On branch main
Your branch is up to date with 'origin/main'.
nothing to commit, working tree clean
```

**Session complete.** The domain memory now reflects:
- `bd-002`: closed (user model done)
- `bd-003`, `bd-004`: now unblocked and ready
- `bd-005`: new discovered work, queued for later

### Next Session Preview

A subsequent agent runs:

```bash
$ bd ready --json
[
  {"id": "bd-003", "title": "Add login endpoint", "priority": 1, "status": "open"},
  {"id": "bd-004", "title": "Add registration endpoint", "priority": 1, "status": "open"},
  {"id": "bd-005", "title": "Refactor: Extract email validation", "priority": 3, "status": "open"}
]
```

The dependency on `bd-002` is resolved. Login and registration are now unblocked. **Progress is visible. Context is preserved. Work continues.**

### What Made This Work

| Principle                    | How It Appeared                                        |
|------------------------------|--------------------------------------------------------|
| **Session rituals**          | Bootup grounded Claude; landing ensured persistence    |
| **Single-issue discipline**  | Email refactor was filed, not fixed inline             |
| **Dependency thinking**      | `bd-003`/`bd-004` correctly blocked until `bd-002` done |
| **Description quality**      | Discovered issue included why/what/how                 |
| **Atomic progress**          | One issue claimed, completed, closed                   |
| **Git as safety net**        | All work committed and pushed before session end       |

> **The agent was stateless. The harness provided continuity.**

---

## Design Principles

From Nate Jones and Anthropic's research:

1. **Externalize the goal** — Turn "do X" into machine-readable backlog with pass/fail criteria
2. **Make progress atomic** — Pick one item, work it, update shared state
3. **Leave campsite cleaner** — End every run with clean, passing state
4. **Standardize bootup** — Same protocol every session: read memory, run checks, then act
5. **Keep tests close to memory** — Pass/fail status is the source of truth

> "If you have no shared feature list, every run will rederive its own definition of done. If you have no durable progress log, every run will guess what happened wrongly."

---

## Why This Pattern Works

| Traditional Agent Risk           | Beads Harness Mitigation                  |
|----------------------------------|-------------------------------------------|
| Agent does too much              | Scope limited to ONE issue                |
| Changes can't be undone          | Git history, atomic commits               |
| Lost context between sessions    | Beads + git = persistent memory           |
| No visibility into agent work    | Audit trail, commit messages              |
| Multi-agent conflicts            | Hash-based IDs, dependency tracking       |

**The competitive moat**: Models will improve and become commoditized. What won't be commoditized are:
- The schemas you define for your work
- The harnesses that turn LLM calls into durable progress
- The testing loops that keep agents honest

---

## Integration with Claude Code Plugins

This repo's beads patterns integrate with the [wshobson/agents](https://github.com/wshobson/agents) plugin ecosystem through the `beads-workflows` plugin:

### Agents

- **beads-disciplinarian** — Validates compliance with session rituals, dependency direction, description quality, single-issue focus
- **beads-workflow-orchestrator** — Coordinates multi-step beads workflows
- **beads-issue-reviewer** — Reviews issue quality and structure

### Skills

- **session-rituals** — Detailed session start/end protocols
- **dependency-thinking** — Causal reasoning patterns
- **description-quality** — Issue description templates
- **single-issue-discipline** — Scope management techniques
- **beads-cli-reference** — Complete command reference

### Commands

- `beads-session-start` — Orchestrated session initialization
- `beads-session-end` — Orchestrated session close with handoff
- `beads-issue-create` — Guided issue creation
- `beads-dependency-add` — Guided dependency management

---

## References

- [Anthropic: Effective Harnesses for Long-Running Agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
- [Nate B. Jones: AI Agents That Actually Work](https://www.youtube.com/watch?v=...) (YouTube)
- [Beads Issue Tracker](https://github.com/steveyegge/beads)
- [Claude Code Plugins](https://github.com/wshobson/agents) (wshobson/agents)
- [BEADS_HARNESS_PATTERN.md](BEADS_HARNESS_PATTERN.md)
- [HUMAN_WORKFLOW.md](HUMAN_WORKFLOW.md)

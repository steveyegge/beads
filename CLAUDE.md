# CLAUDE.md — Beads Agent Harness

> **The moat isn't a smarter AI agent — it's your domain memory and harness.**
> — Nate B. Jones

This file configures Claude as a **native beads user** — an agent that transforms domain memory state through disciplined, single-issue work sessions with git-backed persistence.

---

## Part 1: The Core Insight

### The Amnesia Problem

Every AI agent session starts fresh. Without external memory, agents either:
- **One-shot** everything (exhaust context, leave a mess)
- **Declare premature victory** (see partial progress, assume done)
- **Guess at prior state** (waste tokens, introduce inconsistency)

### The Solution: Domain Memory

The agent doesn't need memory — it needs a **STAGE** to perform on.

> "The agent is now just a policy that transforms one consistent memory state into another. The magic is in the memory. The magic is in the harness. The magic is not in the personality layer."

**Beads** provides that stage:
- **Persistent** — survives sessions (git-backed JSONL)
- **Structured** — machine-readable goals, status, dependencies
- **External** — not in context window
- **Versioned** — git history enables rollback

### The Safety Model

Safety doesn't require sandboxing when you have:
1. **Scoped tasks** — one issue per session
2. **Observable state** — beads + git show everything
3. **Reversible changes** — git reset/revert
4. **Structured rituals** — bootup and landing protocols

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

## Part 3: Four Pillars of Beads Discipline

### 1. Session Rituals

**Session Start (MANDATORY)**:
```bash
pwd && git status           # Ground: Where am I?
git pull --rebase           # Sync: Get remote state
bd sync                     # Import beads updates
bd ready --json             # Orient: What's available?
bd update <id> --status in_progress  # Claim ONE issue
bd show <id> --json         # Load full context
```

**Automatic Version Awareness** (Claude Code plugin handles this):

When using the beads Claude Code plugin, session startup automatically:

1. **Detects bd upgrades** — Compares current version to last-seen version
2. **Shows changelog** — Displays `bd info --whats-new` if version changed
3. **Updates git hooks** — Auto-installs if hooks are outdated
4. **Injects workflow context** — Runs `bd prime` for session protocols

If you see a version upgrade notification like:

```text
🔄 bd upgraded: 0.27.0 → 0.29.0
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
[changelog details]
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💡 Review changes above and adapt your workflow accordingly
```

**What to do**: Read the changelog for new commands, changed behaviors, or deprecated patterns. Adapt your workflow if needed. The notification only appears once per upgrade.

**Requires**: jq installed for version tracking. Without jq, shows a warning but continues normally.

**Session End (NON-NEGOTIABLE)**:
```bash
bd close <id> --reason "..."  # Or update with notes
bd sync                       # Export to JSONL
git add -A && git commit -m "bd-<id>: <description>"
git push                      # MUST complete
git status                    # MUST show "up to date"
```

> **THE PLANE HAS NOT LANDED UNTIL `git push` COMPLETES**

### 2. Single-Issue Discipline

**One issue, one focus, one completion.**

When you claim issue X:
- Work on issue X only
- Complete issue X fully
- Create a 'Discovered issue bead' for any new work
- Close issue X

If you discover issue Y while working on X:
- **DO NOT** fix Y
- **DO** file Y as a new issue:
  ```bash
  bd create "Discovered: ..." --description="Found while working on X. ..." \
    --deps discovered-from:X -t task
  ```
- **DO** continue working on X

**Why**: Tracked work is accountable work. Untracked fixes become technical debt.

### 3. Dependency Thinking

**The Cognitive Trap**: Temporal language inverts dependencies.

| Temporal (WRONG) | Requirement (RIGHT) | Command |
|------------------|---------------------|---------|
| "X before Y" | "Y needs X" | `bd dep add Y X` |
| "First X, then Y" | "Y requires X" | `bd dep add Y X` |
| "Phase 1 → Phase 2" | "Phase 2 depends on Phase 1" | `bd dep add phase2 phase1` |

**Mental Model**: Ask "Which issue NEEDS the other?" → `bd dep add DEPENDENT REQUIRED`

**Always Verify**: `bd blocked` — blocked tasks should be waiting for prerequisites.

### 4. Description Quality

Every issue must answer **Why / What / How**:

```bash
bd create "Fix auth login 500 error" \
  --description="Login fails with 500 when password contains special chars.
  
  Found while testing user registration. Stack trace shows unescaped SQL 
  in auth/login.go:45.
  
  Approach: Add parameterized queries and input validation.
  
  Related: bd-123 (auth refactor epic)" \
  -t bug -p 1
```

**Minimum**: 50 characters. Include discovery context if filed during work.

---

## Part 4: Essential Commands

### Finding Work
```bash
bd ready --json              # Unblocked, open issues
bd list --status open        # All open issues  
bd show <id> --json          # Full issue details
bd blocked                   # What's waiting on what
```

### Managing Work
```bash
bd update <id> --status in_progress  # Claim work
bd update <id> --notes "Progress..."  # Add context
bd close <id> --reason "Done"         # Complete work
```

### Creating Issues
```bash
bd create "<title>" \
  --description="Why: ... What: ... How: ..." \
  -t bug|feature|task|epic|chore \
  -p 0-4 \
  --deps discovered-from:<parent-id>
```

### Dependencies
```bash
bd dep add <dependent> <required> --type blocks
bd dep add <child> <parent> --type parent-child
bd dep add <new> <source> --type discovered-from
bd dep tree <id>             # View dependency graph
```

### Sync & Git
```bash
bd sync                      # Bidirectional sync with JSONL
git add .beads/ && git commit -m "..."
git push
```

---

## Part 5: The Two Agent Roles

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

## Part 5.5: Context Exhaustion & Emergency Checkpoints

### The Disruption Problem

Context windows fill up. Compaction may occur. Sessions may be interrupted. **Domain memory must anticipate disruption** — the next agent (or you after compaction) needs enough state to continue.

### Recognizing Context Pressure

Watch for these signals:
- Long conversation history
- Large code files read into context
- Multiple tool invocations accumulating
- Sense that "things are getting full"

**Don't wait for compaction to hit** — checkpoint proactively.

### Emergency Checkpoint Protocol

When context is filling up but work isn't complete:

```bash
# 1. STOP current implementation work immediately

# 2. CAPTURE state in beads (the critical step)
bd update <id> --status in_progress --notes "
CHECKPOINT: Context exhaustion imminent

COMPLETED:
- [x] What's done so far
- [x] Files modified: path/to/file.go, path/to/other.go

IN PROGRESS:
- [ ] Current task being worked on
- [ ] Specific function/section being modified

NEXT STEPS:
- [ ] What remains to complete the issue
- [ ] Any blockers or decisions needed

CONTEXT FOR CONTINUATION:
- Key insight: [important discovery or approach]
- Watch out for: [gotchas, edge cases found]
- Test with: [command to verify progress]
"

# 3. COMMIT partial progress (even if incomplete)
git add -A
git commit -m "bd-<id>: WIP checkpoint - context exhaustion

Partial implementation of <description>.
See issue notes for continuation context."

# 4. SYNC to ensure state is persisted
bd sync
git push

# 5. GENERATE handoff prompt for next session
```

### Handoff Prompt Template

Create this for the next agent/session:

```markdown
## Continue: bd-<id> - <title>

**Status**: In progress (checkpointed due to context exhaustion)

**Last checkpoint**: <timestamp or commit hash>

**What's done**:
- <completed item 1>
- <completed item 2>

**Current state**:
- Working on: <specific task>
- Files touched: <list>
- Tests status: <passing/failing/not yet written>

**To continue**:
1. Run: `bd show <id> --json` (read checkpoint notes)
2. Run: `git log -3 --oneline` (see recent commits)
3. Resume from: <specific file:line or function>

**Key context**:
- <important insight that would be lost>
- <approach being taken and why>
```

### Checkpoint vs Close Decision Tree

```
Is the issue complete enough to close?
├─ YES (all acceptance criteria met)
│   └─ Close normally: bd close <id> --reason "..."
│
└─ NO (work remains)
    │
    ├─ Can I finish before context exhausts?
    │   ├─ YES → Continue working
    │   └─ NO → CHECKPOINT NOW
    │
    └─ Is remaining work well-defined?
        ├─ YES → Checkpoint with clear notes
        └─ NO → File discovered sub-tasks first, then checkpoint
```

### Partial Commit Strategy

**Commit incomplete work** rather than lose it:

```bash
# Good: Preserves progress even if not working
git commit -m "bd-<id>: WIP - auth middleware (incomplete)

Adds basic structure for auth middleware.
NOT YET WORKING - needs token validation logic.
Checkpointed due to context limits."

# Bad: Waiting for "done" state that never comes
# (uncommitted changes lost on context reset)
```

### Recovery After Compaction

The next session (or you after compaction) runs:

```bash
# 1. Standard session start
pwd && git pull && bd sync

# 2. Find the in-progress work
bd list --status in_progress --json

# 3. Load checkpoint context
bd show <id> --json  # Read the detailed notes

# 4. Check git state
git log -5 --oneline  # See recent commits including WIP
git diff HEAD~1       # See what changed in last commit

# 5. Resume work
bd update <id> --notes "Resuming from checkpoint..."
```

### Why This Matters

Without checkpointing:
- **Lost work** — uncommitted changes vanish
- **Lost context** — insights and approach forgotten
- **Duplicate effort** — next agent rediscovers same things
- **Broken continuity** — no clear resumption point

With checkpointing:
- **Progress preserved** — even partial work is committed
- **Context captured** — notes survive context reset
- **Clear handoff** — next session knows exactly where to start
- **Audit trail** — git history shows the journey

> **Principle**: Domain memory must be robust to disruption. Checkpoint early, checkpoint often. The cost of over-documenting is trivial; the cost of lost context is re-doing work.

---

## Part 6: Integration with Claude Code Plugins

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

## Part 7: Failure Recovery

| Scenario | Recovery |
|----------|----------|
| Session ended without landing | `bd sync && git add . && git commit -m "Recover" && git push` |
| Agent made destructive changes | `git revert HEAD` or `git reset --hard HEAD~1` |
| Beads merge conflict | `git checkout --theirs .beads/issues.jsonl && bd import -i .beads/issues.jsonl` |
| Agent closed issue without testing | `bd update <id> --status open --notes "Reopened: tests not verified"` |
| Multiple in_progress issues | Choose one, update others: `bd update <id> --status open` |
| Context exhaustion mid-task | Run Emergency Checkpoint Protocol (Part 5.5) |

### Logging Integration

Beads maintains an audit trail, but critical events should also be logged for observability:

```bash
# Log significant events to stdout (captured by harness)
echo "[BEADS] $(date -Iseconds) EVENT: <event_type> ISSUE: <id> DETAILS: <message>"

# Event types to log:
# - SESSION_START: Agent began work
# - ISSUE_CLAIMED: Issue moved to in_progress  
# - CHECKPOINT: Emergency checkpoint created
# - ISSUE_CLOSED: Work completed
# - SESSION_END: Agent completed landing
# - FAILURE: Something went wrong
# - ALERT: Requires human attention
```

**Structured log format**:
```json
{
  "timestamp": "2025-12-27T14:32:00Z",
  "event": "CHECKPOINT",
  "issue_id": "bd-a1b3",
  "agent": "claude",
  "severity": "warn",
  "message": "Context exhaustion - checkpointed incomplete work",
  "context": {
    "files_modified": ["auth/login.go", "auth/middleware.go"],
    "completion_pct": 60
  }
}
```

### Owner Alert Protocol

Some situations require **immediate human attention**. Use this escalation pattern:

**Alert Triggers** (require human notification):
- Security vulnerability discovered
- Breaking change to public API
- Test suite failing after changes
- Merge conflict that can't be auto-resolved
- Repeated failures on same issue (> 2 attempts)
- Blocking dependency on external resource
- Context exhaustion on critical-path work

**Alert Mechanism**:

```bash
# 1. Create high-priority alert issue
bd create "ALERT: <situation>" \
  --description="Requires human attention.
  
  **Situation**: <what happened>
  **Impact**: <why it matters>
  **Current state**: <what's been done>
  **Recommendation**: <suggested action>
  
  Discovered while working on: bd-<parent-id>" \
  -t task -p 0 \
  --deps discovered-from:<current-id>

# 2. Log the alert
echo "[BEADS] $(date -Iseconds) ALERT: <situation> - human attention required"

# 3. Update current issue with alert reference
bd update <current-id> --notes "BLOCKED: Created alert bd-<alert-id> for human review"

# 4. Sync immediately (alerts should persist even if session fails)
bd sync && git push
```

**Alert Issue Template**:
```markdown
Title: ALERT: [Brief description]
Priority: 0 (Critical)
Type: task

Description:
## Situation
[What triggered this alert]

## Impact  
[Why this needs human attention]

## Current State
- Working on: bd-<id>
- Files affected: [list]
- Tests: [passing/failing]

## Recommendation
[What the agent suggests doing]

## Context for Human
[Any additional info to help decision-making]
```

**Post-Alert Behavior**:
- Do NOT continue working on blocked issue
- May work on other unblocked issues from `bd ready`
- Check for alert resolution in subsequent sessions
- Resume blocked work only after human clears the alert

---

## Part 8: Quick Reference Card

**Session Start**:
```bash
pwd && git pull && bd sync && bd ready --json && bd show <id> --json
```

**During Work**:
```bash
bd update <id> --status in_progress
bd create "Discovered: ..." --deps discovered-from:<id>
git commit -m "bd-<id>: <message>"
```

**Session End**:
```bash
bd close <id> --reason "..."
bd sync && git push && git status
```

**Recovery**:
```bash
git reset --hard HEAD~1
bd import -i .beads/issues.jsonl
```

---

## Part 8.5: A Session in Practice — Narrative Walkthrough

This section follows a hypothetical agent session from start to finish, demonstrating how the beads harness works in practice.

---

### The Setup

**Project**: An authentication microservice for a SaaS platform.  
**Epic**: `bd-001` — "User Authentication System"  
**Available tasks**: Three child issues created by an initializer session.

---

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
  "description": "Create User struct with fields for id, email, password_hash, created_at. Include validation methods and database schema migration.",
  "type": "task",
  "priority": 1,
  "status": "in_progress"
}
```

**Stage is set.** Claude knows exactly what to do.

---

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

While writing validation, Claude notices the email regex is duplicated elsewhere in the codebase. **This is discovered work** — file it, don't fix it:

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

---

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

---

### Next Session Preview

A subsequent agent (or Claude after context cycling) runs:

```bash
$ bd ready --json
[
  {"id": "bd-003", "title": "Add login endpoint", "priority": 1, "status": "open"},
  {"id": "bd-004", "title": "Add registration endpoint", "priority": 1, "status": "open"},
  {"id": "bd-005", "title": "Refactor: Extract email validation", "priority": 3, "status": "open"}
]
```

The dependency on `bd-002` is resolved. Login and registration are now unblocked. The refactoring task waits at lower priority. **Progress is visible. Context is preserved. Work continues.**

---

### What Made This Work

| Principle | How It Appeared |
|-----------|----------------|
| **Session rituals** | Bootup grounded Claude; landing ensured persistence |
| **Single-issue discipline** | Email refactor was filed, not fixed inline |
| **Dependency thinking** | `bd-003`/`bd-004` correctly blocked until `bd-002` done |
| **Description quality** | Discovered issue included why/what/how |
| **Atomic progress** | One issue claimed, completed, closed |
| **Git as safety net** | All work committed and pushed before session end |

> **The agent was stateless. The harness provided continuity.**

---

## Part 9: Design Principles

From Nate Jones and Anthropic's research:

1. **Externalize the goal** — Turn "do X" into machine-readable backlog with pass/fail criteria
2. **Make progress atomic** — Pick one item, work it, update shared state
3. **Leave campsite cleaner** — End every run with clean, passing state
4. **Standardize bootup** — Same protocol every session: read memory, run checks, then act
5. **Keep tests close to memory** — Pass/fail status is the source of truth

> "If you have no shared feature list, every run will rederive its own definition of done. If you have no durable progress log, every run will guess what happened wrongly."

---

## Part 10: Why This Pattern Works

| Traditional Agent Risk | Beads Harness Mitigation |
|----------------------|-------------------------|
| Agent does too much | Scope limited to ONE issue |
| Changes can't be undone | Git history, atomic commits |
| Lost context between sessions | Beads + git = persistent memory |
| No visibility into agent work | Audit trail, commit messages |
| Multi-agent conflicts | Hash-based IDs, dependency tracking |

**The competitive moat**: Models will improve and become commoditized. What won't be commoditized are:
- The schemas you define for your work
- The harnesses that turn LLM calls into durable progress
- The testing loops that keep agents honest

---

## References

- [Anthropic: Effective Harnesses for Long-Running Agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
- [Nate B. Jones: AI Agents That Actually Work](https://www.youtube.com/watch?v=...) (YouTube)
- [Beads Issue Tracker](https://github.com/steveyegge/beads)
- [Claude Code Plugins](https://github.com/wshobson/agents) (wshobson/agents)
- [docs/BEADS_HARNESS_PATTERN.md](docs/BEADS_HARNESS_PATTERN.md)
- [docs/HUMAN_WORKFLOW.md](docs/HUMAN_WORKFLOW.md)

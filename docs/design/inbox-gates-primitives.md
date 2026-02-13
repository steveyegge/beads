# Inbox + Gates: First-Class Primitives for Agent Communication

**Issue**: bd-xtahx
**Status**: Design complete (v3.3), ready for implementation
**Author**: Matthew Baker + Claude
**Date**: 2026-02-13

## Summary

Generalize the existing decision/gate/hook injection system into two clean
first-class primitives:

1. **Inbox** — async, non-blocking notifications to agents ("what to tell the agent")
2. **Gates** — blocking conditions that prevent agents from stopping ("when the agent can stop")

Decisions become a composed feature built on top of both: creating a decision =
creating a gate + a decision record; responding = resolving the gate + pushing a
message to the inbox.

## Motivation

### Current Architecture (three parallel systems)

1. **Inject queue** (`internal/inject/queue.go`): JSONL file at
   `.runtime/inject-queue/<session-id>.jsonl` with flock. Used by
   `bd decision check --inject` and `gt mail check --inject`. Drained on hook
   fire.

2. **Session gates** (`internal/gate/`): File-marker-based, per-hook behavioral
   rules. Built-ins: `decision` (strict), `commit-push` (soft), `bead-update`
   (soft). Checked by `GateHandler` at priority 20 in event bus.

3. **StopDecisionHandler** (priority 15): Separate handler that creates/checks
   decision points on Stop events. Runs before GateHandler. Overlaps with the
   `decision` session gate.

4. **DecisionHandler** (priority 30): Polls DB for recently-responded decisions
   via `bd decision check --inject`, formats them, enqueues to inject queue.

### Problems

- Decision injection is pull-based (polls on every hook) instead of push-based
- StopDecisionHandler and the `decision` session gate overlap in purpose
- No general mechanism for external processes to inject messages
- Adding a new notification type requires: new handler, new check command, new
  injection logic

## Current Gate Architecture

Gates exist at three layers today. Understanding all three is essential context
for the design.

### Layer 1: Session Gates (`internal/gate/`)

Ephemeral, per-hook behavioral rules using file markers. Registered at session
start, checked by `GateHandler` (priority 20) on each hook event.

**Mechanism**: A gate is satisfied if (a) a file marker exists at
`.runtime/gates/<session-id>/<gate-id>`, or (b) its `AutoCheck` function returns
true (which auto-creates the marker).

**Modes**: `strict` (block the hook) or `soft` (warn but allow).

**Built-in gates by hook type:**

| Hook | Gate ID | Mode | AutoCheck | Purpose |
|------|---------|------|-----------|---------|
| Stop | `decision` | strict | none (must mark explicitly) | Agent offered a decision before stopping |
| Stop | `commit-push` | soft | `git status --porcelain` clean | Changes committed and pushed |
| Stop | `bead-update` | soft | `GT_HOOK_BEAD` not set | Hooked bead status updated |
| PreToolUse | `destructive-op` | strict | none | Blocks destructive commands |
| PreToolUse | `sandbox-boundary` | soft | none | Warns on commands outside workspace |
| UserPromptSubmit | `context-injection` | soft | none | Pending inject queue items |
| UserPromptSubmit | `stale-context` | soft | none | Session may need `gt prime` |
| PreCompact | `state-checkpoint` | soft | none | Pending injections not drained |
| PreCompact | `dirty-work` | soft | none | Uncommitted changes before compaction |

**Custom gates**: `bd gate register <id> --hook <type> --description '...'`
allows runtime registration of new session gates.

### Layer 2: DB Gates (issues table)

Persistent async wait conditions stored as issue metadata. Used by the
formula/molecule system for multi-step workflows that span sessions.

**Schema** (columns on `issues` table):
```
await_type   TEXT    -- gh:run, gh:pr, timer, human, mail, bead
await_id     TEXT    -- workflow name, PR number, bead ID, etc.
timeout_ns   INTEGER -- timeout in nanoseconds
waiters      TEXT    -- JSON array of agents to notify on resolution
```

**Supported gate types:**

| Type | Resolves when | Can escalate |
|------|--------------|--------------|
| `gh:run` | GitHub Actions workflow completes successfully | Yes (failure/canceled) |
| `gh:pr` | Pull request merges | Yes (closed unmerged) |
| `timer` | Duration elapses since creation | No |
| `human` | Manual `bd close` | No |
| `mail` | Mail response received | No |
| `bead` | Target bead in another rig closes | No |

**Auto-resolution**: `bd gate check` periodically evaluates conditions (queries
GitHub API, checks timestamps, opens target rig DB). Resolves gates whose
conditions are met.

**Cross-rig gates**: `await_id="<rig>:<bead-id>"` waits for a bead in a
different rig to close. Enables cross-agent coordination.

### Layer 3: Stop-Decision Handler

Not technically a "gate" but acts as one — a separate event bus handler at
priority 15 (`StopDecisionHandler`) that:

- Reads `claude-hooks` config bead for `RequireAgentDecision` setting
- Calls `findPendingAgentDecision()` via daemon RPC
- If no decision exists: blocks with multi-line instruction block telling agent
  what to create
- Respects `stop_loop_break` flag from `StopLoopDetector` (priority 14)

**The overlap**: Both `StopDecisionHandler` (priority 15) and the `decision`
session gate (checked by `GateHandler` at priority 20) enforce "agent must offer
a decision before stopping." The handler is the "creator" (tells agent what to
do), the gate is the "enforcer" (blocks if agent didn't do it).

### Bridge Gate: `mol-gate-pending`

A session gate (Layer 1, soft mode) that bridges to DB gates (Layer 2). Its
`AutoCheck` queries `bd list --type gate --parent <hookBead> --status open` to
detect pending formula gates requiring human action. Connects the two layers so
the agent is warned about outstanding DB gates at stop time.

### Event Bus Handler Chain (Stop hook)

```
Priority 14: StopLoopDetector      -- detects infinite stop loops, sets break flag
Priority 15: StopDecisionHandler   -- creates/checks decision points (Layer 3)
Priority 20: GateHandler           -- evaluates session gates (Layer 1)
                                      including bridge to DB gates (Layer 2)
Priority 30: DecisionHandler       -- polls for decision responses (to be replaced)
```

### What this design changes about gates

**Minimal.** The gate architecture stays intact:
- Session gates (Layer 1): unchanged
- DB gates (Layer 2): unchanged
- StopDecisionHandler (Layer 3): stays at priority 15, unchanged
- Bridge gate: unchanged

The only gate-related change is the **Gate -> Inbox bridge**: when a gate
resolves, it pushes a notification to the inbox so the agent learns about it.
This is additive — gate resolution logic is untouched.

## Architecture Principles

**P1: The daemon DB is the source of truth.**
The inbox table in the daemon database is the canonical store. Local JSONL is a
merge buffer. NATS/JetStream is a real-time delivery transport. Both are
optimizations — the DB is what's correct.

**P2: Three tiers — JetStream for real-time, JSONL for batching, DB for reconciliation.**
Each tier is a fallback for the one above. Common case: message arrives via
JetStream, written to JSONL, drained on next hook. Degraded case: JetStream
down, DB reconciliation catches it.

**P3: StopDecisionHandler stays as a handler at priority 15.**
Gates are predicates. StopDecisionHandler has side effects (RPC, config reads,
instruction blocks). They are complementary: handler is the "creator" (tells
agent what to do), gate is the "enforcer" (blocks if agent didn't do it).

**P4: Backwards-compatible migration via dual-read with phased removal.**

**P5: Agent names are the routing primitive.**
Messages are addressed to agent names (mayor, dog, sling), not bead IDs or
session IDs. Agent names are stable, meaningful, and already exist in the system.
Bead IDs are per-session ephemera. Session IDs are even more transient. The agent
name is what producers actually know and care about.

## Routing Model

### Agent-Name Routing

Every agent in Gas Town has a name (`GT_ROLE`): mayor, dog, sling, etc. These
are the natural addressing primitive for inbox messages.

```bash
bd inbox push --to=mayor "CI build failed on main"
bd inbox push --to=dog "Your subtask bd-xyz is done"
bd inbox push --to=all "Maintenance in 10 minutes"
```

The daemon resolves agent names to active beads/sessions. Producers don't need to
know bead IDs, session IDs, or pod locations — just the agent name.

### Default scope is the current rig

When `--to` is omitted, the message goes to all agents in the current rig:

```bash
bd inbox push --type=alert "CI failed"           # all agents in this rig
bd inbox push --to=mayor --type=alert "CI failed" # just mayor
bd inbox push --to=all --type=system "Maintenance" # explicit broadcast
```

For cross-rig messaging:

```bash
bd inbox push --rig=gastown --to=mayor "Deploy failed"
```

### NATS Subject Design

```
inbox.agent.{agent_name}    -- directed message to named agent
inbox.rig.{rig_name}        -- all agents in a rig (default when --to omitted)
inbox.all                   -- broadcast to all agents everywhere
```

Coop subscribes to:
- `inbox.agent.{GT_ROLE}` — messages for this specific agent
- `inbox.rig.{rig_name}` — messages for all agents in this rig
- `inbox.all` — global broadcasts

### Decision Response Routing

Decisions naturally route via agent names. The `requested_by` field on a decision
point already identifies the requesting agent by name.

```
1. Agent "mayor" creates decision:
   bd decision create --prompt="Deploy to prod?"
   → DB stores: requested_by = "mayor"

2. Human responds:
   bd decision respond hq-xyz --select=yes
   → DecisionResolve RPC (updates DB)
   → InboxPush: --to={requested_by} i.e. --to=mayor
   → JetStream: publishes to inbox.agent.mayor
   → Coop receives, writes to JSONL, nudges if idle
   → Next hook: InboxDrainHandler drains, mayor sees response
```

### Session Resilience

Agent-name routing is resilient across restarts:

- If mayor crashes and restarts mid-decision, the response still arrives at the
  new session via `inbox.agent.mayor`. The inbox message is self-contained
  (includes prompt, options, selected option, text) — no prior session context
  needed.

- Blocking decisions (`bd decision create --wait`) are session-scoped — the
  blocking wait dies with the session. But the response arrives via inbox on the
  next session. Semantics shift from "blocking wait resolved" to "notification
  of response" — which is more resilient.

- Stop-decision flow: mayor stops, human responds async, next mayor session
  drains inbox, sees "Human chose: fix-bug", acts on it.

## Relationship to gt mail

### Mail as Feature, Inbox as Transport

Gas Town already has a rich mail system (`gt mail`) with threading, CC, mailing
lists, queues, announce channels, and read/unread state. Mail messages are stored
as beads issues (`type=message`) in the town-level `.beads/` directory, making
them persistent, git-tracked, searchable, and threadable.

Mail currently has its own delivery path: `gt mail send` writes a beads issue,
emits a `BusMailSent` event, and the `MailNudgeHandler` nudges the recipient via
coop. The recipient's `gt mail check --inject` polls for unread mail and writes
to the inject queue.

**The inbox absorbs mail's delivery layer.** Mail keeps its rich storage and
semantics. The inbox becomes the universal delivery transport that mail (and
decisions, gates, CI alerts, etc.) all use.

### How mail uses the inbox

```
gt mail send --to=mayor "Review this PR"
  → writes beads issue (type=message) — persistent storage (unchanged)
  → bd inbox push --to=mayor --type=mail --dedup-key=mail:{msg_id}
      "New mail from dog: Review this PR"
  → inbox handles delivery: DB → JetStream → coop → JSONL → drain
```

Mail's own injection path (`gt mail check --inject`, `MailNudgeHandler`) becomes
a thin wrapper around `bd inbox push`, or is replaced entirely:

| Current | After inbox migration |
|---------|----------------------|
| `gt mail send` → `bd bus emit MailSent` → `MailNudgeHandler` nudges coop | `gt mail send` → `bd inbox push --to=<recipient>` |
| `gt mail check --inject` → polls beads DB → inject queue | Replaced by `bd inbox drain` (mail notifications arrive via inbox) |
| `MailHandler` in event bus (polls for mail) | Removed (InboxDrainHandler handles all) |

### What stays in mail vs inbox

| Concern | Lives in | Why |
|---------|----------|-----|
| Message storage | `gt mail` (beads issues) | Threading, CC, search, git tracking |
| Mailing lists, queues, channels | `gt mail` | Rich routing semantics |
| Read/unread state | `gt mail` (issue status) | Persistent per-message state |
| Delivery notification | `bd inbox` | "You have mail" is an inbox message |
| Decision responses | `bd inbox` | Ephemeral notifications, not threaded |
| CI alerts, gate notifications | `bd inbox` | Ephemeral, TTL, dedup |

Mail is a **communication feature** (conversations between agents). The inbox is
a **delivery primitive** (getting notifications to agents). Mail uses inbox for
delivery, just as decisions use inbox for response notification.

### All producers converge at inbox

```
gt mail send         → bd inbox push --type=mail
bd decision respond  → bd inbox push --type=decision
bd gate resolve      → bd inbox push --type=gate
CI webhook           → bd inbox push --type=alert
Other agent          → bd inbox push --type=agent

All → inbox DB → JetStream → coop → JSONL → drain → agent sees it
```

One delivery layer instead of three parallel ones (mail inject, decision inject,
custom inject). One drain handler. One nudge path. One reconciliation mechanism.

## Three-Tier Delivery Model

```
Tier 1: JetStream -> coop durable consumer -> local JSONL (real-time push)
  | fallback if JetStream unavailable or coop restarting
Tier 2: Local JSONL queue (merge buffer, drained on hook fire)
  | reconciliation on SessionStart + periodic heartbeat
Tier 3: DB query (source of truth, catches anything missed)
```

### Tier 1: JetStream (real-time, K8s)

Add `INBOX_EVENTS` stream to daemon's embedded NATS server:
- Subjects: `inbox.>` (captures all inbox subjects)
- Storage: file (survives daemon restart if on PVC)
- MaxMsgs: 10000 (same as other streams)
- Added in `EnsureStreams()` alongside existing streams

Coop subscription (Rust, runs in same pod as agent):
- Durable consumer named `inbox-{agent_name}` with `DeliverPolicy: DeliverNew`
- Subscribes to: `inbox.agent.{GT_ROLE}` + `inbox.rig.{rig}` + `inbox.all`
- On receive: parse JSON, write to `.runtime/inject-queue/{session_id}.jsonl` with flock
- Ack AFTER successful JSONL write (at-least-once guarantee)
- `AckWait: 30s`
- On restart: durable consumer replays unacked messages automatically

Wake-up after JSONL write:
- Write to JSONL first (durably staged)
- If agent is idle: nudge via `POST /api/v1/agent/nudge`
- If agent is working: do nothing (next hook cycle drains naturally)
- Do NOT write to hook FIFO pipe (fragile coupling)

### Tier 2: Local JSONL (merge buffer)

Purpose: batch multiple injections into one coherent hook response. Claude Code's
hook API returns a single stdout response — multiple concurrent handlers would
interleave without this buffer.

Storage: `.runtime/inject-queue/<session-id>.jsonl` (existing path, unchanged)

Writers: coop JetStream subscriber, `bd inbox push` (local dev), existing producers.

Concurrency: `flock(2)` advisory lock. Safe across Go and Rust (same POSIX
syscall). Writers MUST use `O_APPEND|O_CREATE|O_WRONLY` and single `write()`
calls per entry.

### Tier 3: DB reconciliation

When: SessionStart (always) + periodic heartbeat (every 10 minutes in coop).

Catches messages missed during: coop downtime, JetStream gaps, version skew,
daemon restart. Costs one RPC per 10 minutes per agent.

NOT run on every hook fire. PreCompact drains local JSONL only (no RPC).

## Inbox

### DB Schema

```sql
CREATE TABLE inbox (
    id           TEXT PRIMARY KEY,
    agent_name   TEXT NOT NULL,       -- target agent (e.g., "mayor", "dog")
    rig          TEXT,                -- target rig (NULL = current rig)
    session_id   TEXT,                -- resolved session for JSONL routing
    type         TEXT NOT NULL,       -- "decision", "alert", "event", "agent",
                                     -- "mail", "gate", "system"
    source       TEXT NOT NULL,       -- producer identity
    content      TEXT NOT NULL,       -- the message
    priority     INTEGER DEFAULT 2,  -- 0=critical, 2=normal, 4=low
    created_at   DATETIME NOT NULL,
    delivered_at DATETIME,           -- NULL until drained to agent
    expires_at   DATETIME,           -- NULL = never expires
    dedup_key    TEXT NOT NULL        -- MANDATORY: prevents duplicate delivery
);
CREATE INDEX idx_inbox_pending ON inbox(agent_name, delivered_at)
    WHERE delivered_at IS NULL;
CREATE UNIQUE INDEX idx_inbox_dedup ON inbox(dedup_key);
```

- `agent_name`: target agent by name. "all" for broadcast. Can be a rig-scoped
  wildcard if sent via `--to` omitted (daemon expands to all active agents).
- `dedup_key`: NOT NULL, UNIQUE. INSERT OR IGNORE for idempotent retries.

Dedup key conventions:
- Decision response: `decision:{decision_id}`
- Mail: `mail:{message_id}`
- Gate resolution: `gate:{gate_id}`
- Alert: `alert:{source}:{timestamp_ms}`
- Broadcast: `broadcast:{id}`
- Rig-wide: `rig:{rig_name}:{source}:{timestamp_ms}`

### JSONL Entry Schema

```json
{
  "id": "uuid",
  "type": "decision",
  "source": "bd decision respond",
  "content": "<system-reminder>Decision X resolved: Y</system-reminder>",
  "priority": 2,
  "timestamp": 1707858243000,
  "dedup_key": "decision:hq-abc123",
  "ttl_seconds": 600
}
```

Backwards-compatible with existing entries (missing fields default to zero values).

### Commands

```bash
# Push a message to a specific agent
bd inbox push --to=mayor --type=alert "CI build failed on main"
bd inbox push --to=dog --type=agent "Your subtask bd-xyz is done"

# Push to all agents in current rig (default when --to omitted)
bd inbox push --type=alert "CI build failed on main"

# Explicit broadcast to all agents everywhere
bd inbox push --to=all --type=system "Maintenance in 10 minutes"

# Cross-rig messaging
bd inbox push --rig=gastown --to=mayor "Deploy failed"

# With TTL and dedup
bd inbox push --to=mayor --type=event --ttl=10m --dedup-key=ci-run-42 "CI started"

# List pending
bd inbox list
bd inbox list --all
bd inbox list --delivered

# Drain (called by hook handler)
bd inbox drain --session=<id> [--reconcile]
```

### Drain Implementation

1. Acquire exclusive flock on JSONL file
2. Read all lines
3. Truncate to 0
4. Release flock
5. IF --reconcile: query daemon DB for undelivered items via RPC (by agent_name)
6. Merge local + DB entries
7. Deduplicate by dedup_key (DB wins on conflict)
8. Discard expired items
9. Sort by priority (asc) then created_at (asc)
10. Cap at 20 messages (priority 0 never deferred)
11. Mark DB items as delivered
12. Output as system-reminder tags

### Event Bus Handler

```go
type InboxDrainHandler struct{}
func (h *InboxDrainHandler) Priority() int { return 30 }
func (h *InboxDrainHandler) Handles() []EventType {
    return []EventType{EventSessionStart, EventPreCompact}
}
```

Runs `bd inbox drain --session <id>`, adds `--reconcile` on SessionStart only.

## Gates (minimal changes)

Session gates, DB gates, GateHandler, built-in gates: all unchanged.

New: **Gate -> Inbox bridge.** When a gate resolves, push notification to inbox:
```go
inboxPush(InboxItem{
    To:       agentName,
    Type:     "gate",
    DedupKey: fmt.Sprintf("gate:%s", gateID),
    Content:  fmt.Sprintf("Gate %s resolved: %s", gateID, reason),
})
```

## Handler Priority Chain

```
14: StopLoopDetector      -- sets stop_loop_break if looping
15: StopDecisionHandler   -- tells agent to create decision (if not already done)
20: GateHandler           -- enforces all gates including decision (strict)
30: InboxDrainHandler     -- drains inbox (decisions, alerts, gates, etc.)
```

StopDecisionHandler is the "creator" (side effects: RPC, config reads, instruction
blocks). The decision gate is the "enforcer" (pure predicate). Complementary.

## Decision Migration

### `bd decision respond` changes

After DB resolve, push to inbox. Critical sequencing:
1. `DecisionResolve` RPC — resolve in DB, close gate (MUST succeed)
2. `InboxPush` RPC — write to inbox DB + JetStream publish (best-effort)
   - Routes to `--to={requested_by}` (the agent that created the decision)
3. Decision hook fire (existing)
4. Decision event emit (existing)

Step 2 MUST happen after step 1 but before step 3. If the hook fires first, a
nudge could wake the agent before the inbox message exists.

### Session resilience for decisions

- Agent restarts between decision create and respond: response arrives via inbox
  to new session. Message is self-contained (prompt, options, selected, text).
- Blocking wait dies with session, but response persists in DB + inbox. Next
  session picks it up via reconciliation.
- Stop-decision flow: agent stops → human responds async → next session drains
  inbox → agent sees response and acts on it.

### Deprecation path

| Version | DecisionHandler | `bd decision check --inject` | InboxDrainHandler |
|---------|-----------------|------------------------------|-------------------|
| v0.59   | Priority 30     | Works, warns on stderr       | Priority 31       |
| v0.60   | Removed         | No-op                        | Priority 30       |
| v0.61   | Removed         | Removed                      | Priority 30       |

## Delivery Paths

### Local dev (no NATS, no coop)

```
bd inbox push --to=mayor -> daemon RPC (DB) + local JSONL write
  -> Next hook fires -> InboxDrainHandler reads JSONL
  -> On SessionStart: also queries DB (reconciliation)
```

### K8s (with NATS, with coop)

```
bd inbox push --to=mayor -> daemon RPC (DB) -> JetStream: inbox.agent.mayor
  -> coop durable consumer receives -> JSONL write -> nudge if idle
  -> Next hook fires -> InboxDrainHandler reads JSONL
  -> On SessionStart: also queries DB (reconciliation)
```

### K8s degraded (NATS down)

```
bd inbox push --to=mayor -> daemon RPC (DB) -> JetStream publish fails
  -> Periodic heartbeat (10min): coop queries daemon -> JSONL write
  -> Or: SessionStart reconciliation catches it
```

## Correctness Specifications

### Atomicity
- Drain: flock held across read+truncate (atomic)
- Respond: resolve gate -> push inbox -> fire hook (ordered, not atomic)
- Reconciliation catches partial failures

### Dedup
- dedup_key NOT NULL with UNIQUE index
- INSERT OR IGNORE for idempotency
- Drain: DB version wins when merging local + DB entries
- Old JSONL entries without dedup_key: treated as unique

### TTL
- Drain-time: expired items discarded
- DB reaper: daemon cleans expired+delivered items older than 24h
- Write-time: no enforcement

### Queue depth
- Max 20 messages per drain
- Priority 0 never deferred
- Excess re-written to JSONL or left in DB

### Cross-language flock
- Go: `syscall.Flock` + `O_APPEND|O_CREATE|O_WRONLY`
- Rust: `libc::flock` + identical flags
- Single `write()` per entry for atomicity

### JetStream durability
- Durable consumer: `inbox-{agent_name}` (deterministic name, stable across restarts)
- `DeliverPolicy: DeliverNew`, `AckWait: 30s`
- NATS store on PVC in K8s
- If PVC lost: heartbeat reconciliation catches up in 10 minutes

## Implementation Plan

### Phase 1: Inbox infrastructure (non-breaking)
- DB migration: `inbox` table with agent_name routing
- JetStream: `INBOX_EVENTS` stream with `inbox.>` subjects
- RPC: InboxPush, InboxDrain, InboxList
- `cmd/bd/inbox.go`: push (--to, --rig, --type, --ttl, --dedup-key), list, drain
- InboxDrainHandler at priority 31 (alongside DecisionHandler at 30)

### Phase 2: Coop JetStream subscription
- Coop: durable consumer `inbox-{GT_ROLE}` subscribing to agent + rig + all
- Coop: JSONL write + nudge on receive
- Coop: 10-minute heartbeat reconciliation
- Integration test: Go + Rust concurrent JSONL writes

### Phase 3: Decision -> inbox migration
- `bd decision respond` pushes to inbox (--to={requested_by})
- InboxDrainHandler to priority 30, DecisionHandler fallback at 31
- `bd decision check --inject` deprecation warning

### Phase 4: Mail -> inbox migration
- `gt mail send` calls `bd inbox push --type=mail` after writing beads issue
- Remove `MailHandler` / `MailNudgeHandler` from event bus (InboxDrainHandler handles delivery)
- `gt mail check --inject` deprecated (mail notifications arrive via inbox)
- Mail keeps storage, threading, lists, queues — only delivery layer changes

### Phase 5: Cleanup + external producers
- Remove DecisionHandler
- Remove `bd decision check --inject`
- Remove `gt mail check --inject`
- Gate -> inbox bridge
- DB reaper for expired items
- External producer documentation

## Open Questions

1. **Multiple agents with same name**: If two "mayor" agents are active (e.g.,
   during rolling deploy), both receive the message via the same NATS subject.
   The durable consumer is shared, so only one gets each message. Is this correct?
   Should each instance have a unique consumer (e.g., `inbox-mayor-{bead_id}`)?
   Or is one-of-N delivery acceptable for same-name agents?

2. **Agent name discovery**: How does `bd inbox push --to=mayor` know if mayor
   is a valid agent name? The daemon could maintain an active-agent registry
   (populated from hook events). Push to unknown agent: write to DB anyway
   (delivered when agent starts) or reject?

3. **Self-notification**: When agent calls `bd inbox push --to=<other-agent>`,
   skip local JSONL for own session. Route via DB + JetStream only.

4. **Drain trigger frequency**: Currently SessionStart + PreCompact. Coop's
   heartbeat reconciliation partially addresses the mid-turn delivery gap.

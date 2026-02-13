# Inbox + Gates: First-Class Primitives for Agent Communication

**Issue**: bd-xtahx
**Status**: Design complete (v3.1), ready for implementation
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

Subject design (routes by bead ID, not session ID — bead IDs are stable):
```
inbox.targeted.{bead_id}    -- directed message to specific agent
inbox.broadcast             -- broadcast to all agents
```

Coop subscription (Rust, runs in same pod as agent):
- Durable consumer named `inbox-{bead_id}` with `DeliverPolicy: DeliverNew`
- Subscribes to: `inbox.targeted.{bead_id}` + `inbox.broadcast`
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
    bead_id      TEXT,
    session_id   TEXT,
    type         TEXT NOT NULL,
    source       TEXT NOT NULL,
    content      TEXT NOT NULL,
    priority     INTEGER DEFAULT 2,
    created_at   DATETIME NOT NULL,
    delivered_at DATETIME,
    expires_at   DATETIME,
    dedup_key    TEXT NOT NULL
);
CREATE INDEX idx_inbox_pending ON inbox(bead_id, delivered_at)
    WHERE delivered_at IS NULL;
CREATE INDEX idx_inbox_session ON inbox(session_id, delivered_at)
    WHERE delivered_at IS NULL;
CREATE UNIQUE INDEX idx_inbox_dedup ON inbox(dedup_key);
```

- `type`: "decision", "alert", "event", "agent", "mail", "gate", "system"
- `priority`: 0=critical, 2=normal, 4=low
- `dedup_key`: NOT NULL, UNIQUE. INSERT OR IGNORE for idempotent retries.

Dedup key conventions:
- Decision response: `decision:{decision_id}`
- Mail: `mail:{message_id}`
- Gate resolution: `gate:{gate_id}`
- Alert: `alert:{source}:{timestamp_ms}`
- Broadcast: `broadcast:{id}`

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
# Push a message
bd inbox push --type=alert "CI build failed on main"
bd inbox push --type=decision --bead=<id> "Decision X resolved: option Y"
bd inbox push --broadcast --type=system "Maintenance in 10 minutes"
bd inbox push --type=event --ttl=10m --dedup-key=ci-run-42 "CI run 42 started"

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
5. IF --reconcile: query daemon DB for undelivered items via RPC
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
3. Decision hook fire (existing)
4. Decision event emit (existing)

Step 2 MUST happen after step 1 but before step 3.

### Deprecation path

| Version | DecisionHandler | `bd decision check --inject` | InboxDrainHandler |
|---------|-----------------|------------------------------|-------------------|
| v0.59   | Priority 30     | Works, warns on stderr       | Priority 31       |
| v0.60   | Removed         | No-op                        | Priority 30       |
| v0.61   | Removed         | Removed                      | Priority 30       |

## Delivery Paths

### Local dev (no NATS, no coop)

```
bd inbox push -> daemon RPC (DB) + local JSONL write
  -> Next hook fires -> InboxDrainHandler reads JSONL
  -> On SessionStart: also queries DB (reconciliation)
```

### K8s (with NATS, with coop)

```
bd inbox push -> daemon RPC (DB) -> JetStream publish
  -> coop durable consumer receives -> JSONL write -> nudge if idle
  -> Next hook fires -> InboxDrainHandler reads JSONL
  -> On SessionStart: also queries DB (reconciliation)
```

### K8s degraded (NATS down)

```
bd inbox push -> daemon RPC (DB) -> JetStream publish fails
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
- Durable consumer: `inbox-{bead_id}` (deterministic name)
- `DeliverPolicy: DeliverNew`, `AckWait: 30s`
- NATS store on PVC in K8s
- If PVC lost: heartbeat reconciliation catches up in 10 minutes

## Implementation Plan

### Phase 1: Inbox infrastructure (non-breaking)
- DB migration: `inbox` table
- JetStream: `INBOX_EVENTS` stream
- RPC: InboxPush, InboxDrain, InboxList
- `cmd/bd/inbox.go`: push, list, drain
- InboxDrainHandler at priority 31 (alongside DecisionHandler at 30)

### Phase 2: Coop JetStream subscription
- Coop: durable consumer for inbox subjects
- Coop: JSONL write + nudge on receive
- Coop: 10-minute heartbeat reconciliation
- Integration test: Go + Rust concurrent JSONL writes

### Phase 3: Decision -> inbox migration
- `bd decision respond` pushes to inbox
- InboxDrainHandler to priority 30, DecisionHandler fallback at 31
- `bd decision check --inject` deprecation warning

### Phase 4: Cleanup + external producers
- Remove DecisionHandler
- Remove `bd decision check --inject`
- Gate -> inbox bridge
- DB reaper for expired items
- External producer documentation

## Open Questions

1. **Broadcast delivery tracking**: First-drain-wins for broadcasts. Late-joining
   agents miss if NATS was down. Accept this (broadcasts are ephemeral) or add
   per-agent tracking table?

2. **Self-notification**: When agent calls `bd inbox push` to notify another
   agent, skip local JSONL for own session. Route via DB + JetStream only.

3. **Drain trigger frequency**: Currently SessionStart + PreCompact. Add periodic
   drain (background goroutine)? Coop's heartbeat reconciliation partially
   addresses this.

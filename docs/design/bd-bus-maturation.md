# BD Bus Maturation Design (od-k3o)

> **Epic**: od-k3o — BD Bus Maturation for OJ Integration
> **Status**: In Progress (3/18 tasks complete)
> **Date**: 2026-02-08

## 1. Current State

The bd bus is an event dispatch system inside the daemon:

- **4 built-in handlers** (in-memory only, lost on restart):
  - `prime` (priority 10) — injects workflow context on SessionStart/PreCompact
  - `stop-decision` (priority 15) — creates decision point on Stop hook
  - `gate` (priority 20) — evaluates session gates on Stop/PreToolUse
  - `decision` (priority 30) — injects decision responses on SessionStart/PreCompact

- **2 JetStream streams**:
  - `HOOK_EVENTS` (subjects: `hooks.>`) — Claude Code hook events
  - `DECISION_EVENTS` (subjects: `decisions.>`) — decision lifecycle events

- **~15 event types**: SessionStart, Stop, PreToolUse, PostToolUse, PreCompact, SubagentStart/Stop, DecisionCreated/Responded/Escalated/Expired, advice CRUD

- **CLI commands**: `bd bus emit`, `bd bus status`, `bd bus handlers`, `bd bus subscribe`, `bd bus nats-info`

- **Key files**:
  - `internal/eventbus/bus.go` — Bus struct, Dispatch(), JetStream publishing
  - `internal/eventbus/types.go` — EventType constants, Event/Result structs
  - `internal/eventbus/handler.go` — Handler interface (ID, Handles, Priority, Handle)
  - `internal/eventbus/handlers.go` — DefaultHandlers() + built-in implementations
  - `internal/eventbus/streams.go` — JetStream stream setup
  - `cmd/bd/bus.go` — CLI: status, handlers, nats-info
  - `cmd/bd/bus_emit.go` — CLI: emit (daemon RPC or local fallback)
  - `cmd/bd/bus_subscribe.go` — CLI: subscribe (debug tool)

### What's Already Done (3/18)

| Task | Description | Status |
|------|-------------|--------|
| od-k3o.1 | Enable NATS JetStream by default in daemon | ✅ |
| od-k3o.15 | Integrate decision points with bd bus events | ✅ |
| od-k3o.16 | Integrate Slack bot with bd bus events | ✅ |

## 2. Architecture

```
Claude Code Hooks ──→ bd bus emit --hook=<type> ──→ Daemon RPC
                                                        │
                                                   ┌────▼────┐
                                                   │  Bus     │
                                                   │ Dispatch │
                                                   └────┬────┘
                                                        │
                               ┌────────────────────────┼────────────────┐
                               │                        │                │
                        ┌──────▼──────┐          ┌──────▼──────┐  ┌─────▼─────┐
                        │ Built-in    │          │ External    │  │ JetStream │
                        │ Handlers    │          │ Handlers    │  │ Publish   │
                        │ (Go code)   │          │ (subprocess)│  │ (async)   │
                        └─────────────┘          └─────────────┘  └─────┬─────┘
                                                                        │
                                                     ┌──────────────────┼──────────┐
                                                     │                  │          │
                                              ┌──────▼──────┐   ┌──────▼───┐ ┌────▼────┐
                                              │ Slack Bot   │   │ Coop     │ │ Future  │
                                              │ (sidecar)   │   │ Sidecar  │ │ Consumer│
                                              └─────────────┘   └──────────┘ └─────────┘
```

**Key design decisions:**
- Handlers are **synchronous** and run in priority order (blocking pipeline for Claude Code hooks)
- JetStream publishing is **async/fire-and-forget** (supplementary, not prerequisite)
- External handlers use **stdin/stdout JSON protocol** (same as built-in but in a subprocess)
- Handler registrations are **persisted in Dolt** `bus_handlers` table (survive daemon restarts)

## 3. Phased Implementation

### Phase 1: Foundation (P1 — do first)

**od-k3o.2: Handler Persistence**

Add a `bus_handlers` config table:

```sql
CREATE TABLE bus_handlers (
    id         VARCHAR(64) PRIMARY KEY,
    events     TEXT NOT NULL,          -- comma-separated event types
    command    TEXT NOT NULL,          -- shell command to execute
    priority   INT NOT NULL DEFAULT 50,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

New type in `internal/eventbus/`:

```go
// ExternalHandler runs an external command for each matching event.
// Event JSON is passed on stdin; result JSON is read from stdout.
type ExternalHandler struct {
    id       string
    events   []EventType
    priority int
    command  string
}
```

On daemon startup: `bus.LoadPersisted(store)` reads `bus_handlers` table, creates `ExternalHandler` for each enabled row, registers them. SIGHUP triggers reload without restart.

**od-k3o.4: Registration CLI**

```bash
# Register a handler
bd bus register --id=my-notify --events=Stop,SessionEnd \
    --command="python /path/to/notify.py" --priority=50

# Unregister
bd bus unregister --id=my-notify

# List (already exists: bd bus handlers)
bd bus handlers
```

RPC operations: `OpBusRegister`, `OpBusUnregister` → write to `bus_handlers` table + register/unregister in-memory.

**od-k3o.5: Tests**

| Test Area | What |
|-----------|------|
| Unit | ExternalHandler dispatch (mock subprocess), NATS stream creation |
| Integration | Full emit→dispatch→handler→result pipeline |
| Persistence | Register, restart daemon, verify handlers survive |
| Blocking | Handler that returns block=true stops Claude Code |
| Concurrent | Multiple handlers executing in parallel (future) |
| Benchmark | Emit latency <50ms target |

### Phase 2: Event Types (P2 — enable consumers)

**od-k3o.17: BeadStatusChanged** + **od-k3o.18: WorkCompleted**

New event types in `types.go`:

```go
EventBeadStatusChanged EventType = "BeadStatusChanged"
EventWorkCompleted     EventType = "WorkCompleted"
```

New payload structs:

```go
type BeadStatusChangedPayload struct {
    BeadID    string `json:"bead_id"`
    Title     string `json:"title"`
    OldStatus string `json:"old_status"`
    NewStatus string `json:"new_status"`
    Assignee  string `json:"assignee,omitempty"`
    ChangedBy string `json:"changed_by,omitempty"`
}

type WorkCompletedPayload struct {
    BeadID   string `json:"bead_id"`
    Title    string `json:"title"`
    Assignee string `json:"assignee,omitempty"`
    Summary  string `json:"summary,omitempty"`
    Branch   string `json:"branch,omitempty"`
    Commit   string `json:"commit,omitempty"`
}
```

Emit sites:
- `bd update --status=<new>` → emits BeadStatusChanged via bus
- `gt done` / polecat completion → emits WorkCompleted via bus

Consumer: Slack bot already has handlers for both events (commit 0e54f5d4).

**od-k3o.6: OJ Event Types**

```go
EventOjJobCreated     EventType = "OjJobCreated"
EventOjStepAdvanced   EventType = "OjStepAdvanced"
EventOjAgentSpawned   EventType = "OjAgentSpawned"
EventOjAgentIdle      EventType = "OjAgentIdle"
EventOjAgentEscalated EventType = "OjAgentEscalated"
EventOjJobCompleted   EventType = "OjJobCompleted"
EventOjJobFailed      EventType = "OjJobFailed"
EventOjWorkerPoll     EventType = "OjWorkerPollComplete"
```

New JetStream stream: `OJ_EVENTS` (subjects: `oj.>`) — separate from HOOK_EVENTS for consumer isolation.

### Phase 3: OJ/Coop Integration (P2 — bridge to Coop sidecar)

**od-k3o.10: BusNotifyAdapter** — Highest-value OJ integration

In the Coop Rust crate, implement a `NotifyAdapter` trait that calls:
```bash
bd bus emit --event=OjNotify --payload='{"message":"...","urgency":"..."}'
```

This is zero-change to OJ hook logic — just swap the adapter at executor construction. Desktop notifications become a registered bus handler instead of hardcoded behavior. Enables multi-target routing (desktop + Slack + bead comments) via handler registration.

**od-k3o.7: OJ Runtime Emit**

Approach: subprocess `bd bus emit --event=<type> --payload='...'` calls at OJ lifecycle points. Simpler than a Rust Effect variant, matches existing handler protocol.

Emit sites in Coop/OJ:
- Job creation → `OjJobCreated`
- Step advancement → `OjStepAdvanced`
- Agent spawn/idle/escalation → `OjAgentSpawned`/`OjAgentIdle`/`OjAgentEscalated`
- Completion/failure → `OjJobCompleted`/`OjJobFailed`

**od-k3o.8: Default OJ Handlers**

Pre-registered handlers for common OJ→beads workflows:
```bash
bd bus register --id=oj-bead-sync --events=OjJobCompleted \
    --command="bd close $BEAD_ID --reason='OJ job completed'"

bd bus register --id=oj-status-update --events=OjStepAdvanced \
    --command="bd update $BEAD_ID --status=in_progress"
```

### Phase 4: Gas Town Unification (P2 — hooks → bus)

**od-k3o.12: Unify Claude Code hooks**
- Verify all `.claude/settings.json` hooks route through `bd bus emit --hook=<type>`
- Remove any remaining direct shell commands in hook configs

**od-k3o.13: GT hook commands as bus handlers**
- Register GT commands (gt sling, gt witness, etc.) as persistent bus handlers
- Example: `bd bus register --id=gt-witness --events=SessionStart --command="gt witness start"`

**od-k3o.14: GT witness subscribes to bus**
- Replace tmux polling with NATS subscription to OJ_EVENTS stream
- Witness uses `bd bus subscribe --filter=OjAgentIdle` instead of `tmux capture-pane`

### Phase 5: Advanced (P3 — defer)

**od-k3o.9: Benchmark** — Measure emit→handler latency, optimize subprocess spawning for hot path
**od-k3o.11: Gate action** — Add `EventGateAction` for bus-based policy decisions

## 4. Kubernetes Deployment

### Current State (gastown-uat)
- Embedded NATS runs inside the daemon pod
- Slack bot sidecar connects to `localhost:4222` with `BD_DAEMON_TOKEN` auth
- Config: `deploy.nats_url=nats://localhost:4222` in Dolt config table

### For OJ/Coop Integration
- **Option A (recommended)**: Expose NATS via ClusterIP service for cross-pod access
  - OJ pods in same namespace connect to `nats://gastown-uat-daemon:4222`
  - Simple, no NATS cluster overhead
- **Option B**: NATS cluster (overkill for single-namespace deployment)

### Sidecar Pattern
Proven by Slack bot. OJ/Coop sidecar follows the same pattern:
1. Mount `BD_DAEMON_TOKEN` from shared ExternalSecret
2. Connect to `localhost:4222` (if sidecar) or ClusterIP service (if separate pod)
3. Subscribe to relevant JetStream streams

### Config Keys
```
deploy.nats_url        = nats://localhost:4222      (sidecar default)
deploy.nats_port       = 4222                        (embedded NATS port)
deploy.nats_auth_token = <from BD_DAEMON_TOKEN>     (shared secret)
```

## 5. Implementation Order

| # | Task | Phase | Priority | Dependencies |
|---|------|-------|----------|-------------|
| 1 | od-k3o.2: Handler persistence | 1 | P1 | None |
| 2 | od-k3o.4: Registration CLI | 1 | P1 | od-k3o.2 |
| 3 | od-k3o.5: Comprehensive tests | 1 | P1 | od-k3o.2, od-k3o.4 |
| 4 | od-k3o.17: BeadStatusChanged event | 2 | P2 | None |
| 5 | od-k3o.18: WorkCompleted event | 2 | P2 | None |
| 6 | od-k3o.6: OJ event types | 2 | P2 | None |
| 7 | od-k3o.10: BusNotifyAdapter (Coop) | 3 | P1 | od-k3o.6 |
| 8 | od-k3o.7: OJ runtime emit | 3 | P2 | od-k3o.6 |
| 9 | od-k3o.8: Default OJ handlers | 3 | P2 | od-k3o.2, od-k3o.7 |
| 10 | od-k3o.12: Unify Claude Code hooks | 4 | P2 | None |
| 11 | od-k3o.13: GT hooks as bus handlers | 4 | P2 | od-k3o.2 |
| 12 | od-k3o.14: Witness bus subscription | 4 | P2 | od-k3o.6 |
| 13 | od-k3o.9: Benchmark | 5 | P3 | od-k3o.5 |
| 14 | od-k3o.11: Bus gate action | 5 | P3 | od-k3o.2 |

## 6. ExternalHandler Protocol

External handlers receive event JSON on stdin and return result JSON on stdout:

**Input (stdin):**
```json
{
  "hook_event_name": "Stop",
  "session_id": "abc-123",
  "cwd": "/path/to/workspace",
  "tool_name": "Bash",
  "published_at": "2026-02-08T15:00:00Z"
}
```

**Output (stdout):**
```json
{
  "block": false,
  "reason": "",
  "inject": ["Injected context for Claude"],
  "warnings": ["Warning message"]
}
```

**Exit codes:**
- 0 = success (parse stdout for result)
- 1 = handler error (logged, chain continues)
- 2 = fatal (handler removed from chain)

**Timeout:** 30 seconds default (configurable per handler in `bus_handlers` table).

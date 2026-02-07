# NATS JetStream Event Schema

Reference for external consumers (e.g., Coop sidecar) subscribing to the
bd daemon's hook event stream.

## Connection

| Parameter | Env Var | Default | Discovery |
|-----------|---------|---------|-----------|
| NATS URL | `COOP_NATS_URL` / `BD_NATS_PORT` | `nats://127.0.0.1:4222` | `.runtime/nats-info.json` or `bd bus nats-info` |
| Auth token | `BD_DAEMON_TOKEN` | (none) | Same token used for daemon RPC |
| Stream | — | `HOOK_EVENTS` | — |
| Subject pattern | — | `hooks.>` | — |

### nats-info.json

Written to `$BEADS_DIR/.runtime/nats-info.json` on daemon startup:

```json
{
  "url": "nats://127.0.0.1:4222",
  "port": 4222,
  "token": "...",
  "jetstream": true,
  "stream": "HOOK_EVENTS",
  "subjects": "hooks.>"
}
```

## Stream Configuration

| Setting | Value |
|---------|-------|
| Name | `HOOK_EVENTS` |
| Subjects | `hooks.>` |
| Storage | File |
| Max messages | 10,000 |
| Max bytes | 100 MB |
| Retention | Limits (oldest evicted first) |

## Subject Format

```
hooks.<EventType>
```

Examples: `hooks.SessionStart`, `hooks.PreToolUse`, `hooks.Stop`

## Event Types

| EventType | Subject | When it fires |
|-----------|---------|---------------|
| `SessionStart` | `hooks.SessionStart` | Agent session begins |
| `UserPromptSubmit` | `hooks.UserPromptSubmit` | User sends a message |
| `PreToolUse` | `hooks.PreToolUse` | Before a tool executes |
| `PostToolUse` | `hooks.PostToolUse` | After a tool succeeds |
| `PostToolUseFailure` | `hooks.PostToolUseFailure` | After a tool fails |
| `Stop` | `hooks.Stop` | Agent finishes a turn (idle) |
| `PreCompact` | `hooks.PreCompact` | Before context compaction |
| `SubagentStart` | `hooks.SubagentStart` | Subagent spawned |
| `SubagentStop` | `hooks.SubagentStop` | Subagent completed |
| `Notification` | `hooks.Notification` | System notification |
| `SessionEnd` | `hooks.SessionEnd` | Agent session ends |

## Event JSON Fields

The payload is the original Claude Code hook JSON, published as-is.

### Common fields (all events)

| Field | Type | Always present | Description |
|-------|------|----------------|-------------|
| `hook_event_name` | string | yes | Event type (matches subject suffix) |
| `session_id` | string | yes | Claude Code session identifier |
| `cwd` | string | yes | Working directory |
| `transcript_path` | string | usually | Path to session JSONL log |
| `permission_mode` | string | usually | Permission mode (e.g., "default") |

### Tool events (PreToolUse, PostToolUse, PostToolUseFailure)

| Field | Type | Description |
|-------|------|-------------|
| `tool_name` | string | Tool name (e.g., "Bash", "Write", "AskUserQuestion") |
| `tool_input` | object | Tool parameters (varies by tool) |
| `tool_response` | object | Tool output (PostToolUse only) |

### Prompt events (UserPromptSubmit)

| Field | Type | Description |
|-------|------|-------------|
| `prompt` | string | User's message text |
| `source` | string | Input source |

### Agent events (SubagentStart, SubagentStop)

| Field | Type | Description |
|-------|------|-------------|
| `agent_id` | string | Subagent identifier |
| `agent_type` | string | Subagent type |
| `model` | string | Model being used |

### Error events

| Field | Type | Description |
|-------|------|-------------|
| `error` | string | Error description |

## Example Payloads

### SessionStart
```json
{
  "hook_event_name": "SessionStart",
  "session_id": "abc-123-def",
  "cwd": "/home/user/project",
  "transcript_path": "/home/user/.claude/sessions/abc-123-def.jsonl",
  "permission_mode": "default",
  "model": "claude-sonnet-4-5-20250929"
}
```

### PreToolUse (Bash)
```json
{
  "hook_event_name": "PreToolUse",
  "session_id": "abc-123-def",
  "cwd": "/home/user/project",
  "tool_name": "Bash",
  "tool_input": {
    "command": "npm install express"
  }
}
```

### PreToolUse (AskUserQuestion)
```json
{
  "hook_event_name": "PreToolUse",
  "session_id": "abc-123-def",
  "cwd": "/home/user/project",
  "tool_name": "AskUserQuestion",
  "tool_input": {
    "questions": [
      {
        "question": "Which database should we use?",
        "options": [
          {"label": "PostgreSQL (Recommended)"},
          {"label": "SQLite"},
          {"label": "MySQL"}
        ]
      }
    ]
  }
}
```

### Stop
```json
{
  "hook_event_name": "Stop",
  "session_id": "abc-123-def",
  "cwd": "/home/user/project"
}
```

## Consumer Patterns

### Ephemeral consumer (debugging)

```bash
bd bus subscribe                    # All events
bd bus subscribe --filter=Stop      # Only Stop events
bd bus subscribe --json             # Machine-readable
```

### Durable consumer (production, e.g., Coop)

```
Stream:   HOOK_EVENTS
Consumer: coop-<instance-id>
Subject:  hooks.>
Deliver:  DeliverNew (or DeliverAll for replay)
Ack:      Explicit
```

Multiple durable consumers can subscribe independently — each tracks
its own offset. Stream retention (10k messages / 100MB) provides a
catch-up window for consumers that restart.

## Coop State Mapping

Suggested mapping from bd events to Coop AgentState:

| Event | Coop AgentState | Notes |
|-------|----------------|-------|
| `SessionStart` | `starting` | Agent launched |
| `PreToolUse` | `working` | Tool about to execute |
| `PreToolUse` (tool=AskUserQuestion) | `ask_user` | Agent asking a question |
| `PreToolUse` (tool=EnterPlanMode) | `plan_prompt` | Agent presenting a plan |
| `PostToolUse` | `working` | Tool completed |
| `Stop` | `waiting_for_input` | After grace timer (60s) |
| `SessionEnd` | `exited` | Agent session ended |
| `SubagentStart` | `working` | Subagent spawned |

Use the grace timer pattern from Coop design (Section 5) before
confirming `waiting_for_input` — the gap between tool calls can
falsely appear as idle.

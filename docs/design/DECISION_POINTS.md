# Decision Points Design Specification

> **Epic**: hq-946577 - Decision Point Beads
> **Author**: beads/crew/decision_point
> **Date**: 2026-01-21
> **Status**: Draft

## Executive Summary

Decision Points are a new beads feature for remote, asynchronous human-in-the-loop decisions. An agent creates a Decision Point with options, the system notifies the human via external channels (email, phone app, web), the human picks one option OR provides custom text, and the agent's workflow unblocks.

## Design Principles

1. **Simplicity** - One interaction model: single-select from options, with text always available
2. **Extend, don't reinvent** - Build on the existing gate system for blocking semantics
3. **Remote-first** - Assume human is not at terminal; notifications go through external channels
4. **Async by default** - Agent can continue other work while waiting, or block if needed
5. **Rich content** - Options can contain substantial content (design docs, not just labels)
6. **Audit trail** - All decisions are beads, creating permanent record

---

## Part 1: Data Model (hq-946577.4)

### Approach: Extend Gate System

Decision Points are a specialized type of gate. This reuses:
- Blocking via `blocks` dependency type
- `blocked_issues_cache` for O(1) ready detection
- Gate resolution patterns (`bd gate check`, `bd gate resolve`)
- Waiters notification system
- Timeout handling

### Interaction Model

**One unified type**: Single-select from options, with rich text input always available.

Human can respond by:
1. **Selecting one option** from the provided list
2. **Entering custom text** instead of (or in addition to) selecting an option

This covers all use cases:
- Yes/no → options: `[{id:"yes"}, {id:"no"}]`
- Multiple choice → options with rich descriptions
- Pure text input → empty options list, human must provide text
- Hybrid → pick an option AND add clarifying text

### Rich Text Responses

The text response field supports **substantial, formatted content**:

- **Length**: Up to 32KB of text (roughly 8,000 tokens)
- **Format**: Markdown supported for structure and formatting
- **Use cases**:
  - Detailed design feedback
  - Alternative approaches the agent should consider
  - Specific requirements or constraints
  - Code snippets or examples
  - References to external docs

**Example rich text response:**
```markdown
## Alternative Approach

Instead of the options presented, consider a **tiered caching strategy**:

1. **L1 Cache**: In-memory LRU (process-local, 100ms TTL)
2. **L2 Cache**: Redis (shared, 5min TTL)
3. **L3 Cache**: Database query cache

### Requirements
- Must support cache invalidation via pub/sub
- Need metrics on hit rates per tier
- Budget constraint: $500/month max for Redis

### Reference
See our caching guidelines: https://internal.docs/caching-policy
```

This enables the human to provide complete guidance when none of the options fit, effectively redirecting the agent with full context.

### New Issue Fields

Add these fields to the Issue struct in `internal/types/types.go`:

```go
// ===== Decision Point Fields (human-in-the-loop choices) =====
// Decision points are gates that wait for structured human input.
// Model: single-select from options + optional text input.

// DecisionPrompt is the question shown to the human.
// Can contain markdown for rich formatting.
DecisionPrompt string `json:"decision_prompt,omitempty"`

// DecisionOptions are the available choices (JSON array of Option objects).
// Can be empty if only text input is needed.
DecisionOptions string `json:"decision_options,omitempty"` // JSON array

// DecisionDefault is the option ID selected if timeout occurs.
// Empty means no default (timeout = no response).
DecisionDefault string `json:"decision_default,omitempty"`

// DecisionSelected is the option ID the human chose (set when resolved).
// Empty if human provided only text without selecting an option.
DecisionSelected string `json:"decision_selected,omitempty"`

// DecisionText is custom text input from the human (set when resolved).
// Can be provided alongside a selection, or instead of one.
DecisionText string `json:"decision_text,omitempty"`

// DecisionRespondedAt is when the human responded.
DecisionRespondedAt *time.Time `json:"decision_responded_at,omitempty"`

// DecisionRespondedBy identifies who responded (email, user ID, etc.).
DecisionRespondedBy string `json:"decision_responded_by,omitempty"`
```

### Option Schema

Decision options are stored as JSON in `DecisionOptions`:

```go
// DecisionOption represents one choice in a decision point
type DecisionOption struct {
    // ID is the short identifier (e.g., "a", "b", "yes", "no")
    ID string `json:"id"`

    // Label is the display text shown to human
    Label string `json:"label"`

    // Description is optional rich content (markdown)
    // Can contain full design documents, code snippets, etc.
    Description string `json:"description,omitempty"`
}
```

Example JSON:
```json
[
  {
    "id": "a",
    "label": "Use Redis for caching",
    "description": "## Redis Approach\n\n- Proven technology\n- 10ms p99 latency\n- Requires Redis cluster..."
  },
  {
    "id": "b",
    "label": "Use in-memory LRU cache",
    "description": "## In-Memory Approach\n\n- Zero external deps\n- Process-local only\n- Lost on restart..."
  }
]
```

### AwaitType

Single `await_type` value for decision points:

| AwaitType | Description |
|-----------|-------------|
| `decision` | Human decision point (single-select + text) |

### Issue Type

Decision points use `type = "gate"` (not a new type) to reuse gate infrastructure.

Filtering for decision points: `await_type = 'decision'`

### Status Lifecycle

| Status | Meaning |
|--------|---------|
| `open` | Waiting for human response |
| `closed` | Human responded, decision recorded |

### Database Migration

New migration `029_decision_point_columns.go`:

```go
var migrations = []Migration{
    {
        ID: 29,
        Up: func(tx *sql.Tx) error {
            _, err := tx.Exec(`
                ALTER TABLE issues ADD COLUMN decision_prompt TEXT;
                ALTER TABLE issues ADD COLUMN decision_options TEXT;
                ALTER TABLE issues ADD COLUMN decision_default TEXT;
                ALTER TABLE issues ADD COLUMN decision_selected TEXT;
                ALTER TABLE issues ADD COLUMN decision_text TEXT;
                ALTER TABLE issues ADD COLUMN decision_responded_at TEXT;
                ALTER TABLE issues ADD COLUMN decision_responded_by TEXT;
            `)
            return err
        },
    },
}
```

---

## Part 2: Notification Interface (hq-946577.5)

### Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                      bd decision create                         │
│  Creates decision point bead with options                       │
└───────────────────────────┬────────────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────────────┐
│                    Notification Dispatch                        │
│  Reads escalation.json routes, sends to configured channels    │
└───────────────────────────┬────────────────────────────────────┘
                            │
            ┌───────────────┼───────────────┬────────────────┐
            ▼               ▼               ▼                ▼
        ┌───────┐     ┌─────────┐     ┌─────────┐      ┌─────────┐
        │ Email │     │ Web UI  │     │   SMS   │      │  Slack  │
        │       │     │Webhook  │     │         │      │         │
        └───┬───┘     └────┬────┘     └────┬────┘      └────┬────┘
            │              │               │                │
            └──────────────┴───────────────┴────────────────┘
                                   │
                                   ▼
┌────────────────────────────────────────────────────────────────┐
│                    Response Webhook                             │
│  POST /api/decisions/{id}/respond                              │
│  Calls: bd decision respond <id> <answer>                      │
└────────────────────────────────────────────────────────────────┘
```

### Notification Payload

When a decision point is created, the notification system sends:

```json
{
  "type": "decision_point",
  "id": "gt-abc123.decision-deploy",
  "prompt": "Which caching strategy should we use?",
  "options": [
    {"id": "a", "label": "Redis", "description": "..."},
    {"id": "b", "label": "In-memory", "description": "..."}
  ],
  "default": "a",
  "timeout_at": "2026-01-22T10:00:00Z",
  "respond_url": "https://beads.example.com/api/decisions/gt-abc123.decision-deploy/respond",
  "view_url": "https://beads.example.com/decisions/gt-abc123.decision-deploy",
  "source": {
    "agent": "beads/crew/decision_point",
    "molecule": "gt-abc123",
    "step": "deploy"
  }
}
```

### Notification Channels

#### Email

Subject: `[Decision Required] Which caching strategy should we use?`

Body (HTML/text):
```
A decision is needed for workflow gt-abc123:

  Which caching strategy should we use?

Options:
  [A] Redis - Proven technology, requires Redis cluster
  [B] In-memory - Zero deps, process-local only

Default (if no response by Jan 22, 10:00 UTC): A

Reply with your choice:
  → Click here to respond: [RESPOND LINK]
  → Or reply to this email with just "A" or "B"
```

#### SMS

```
[Gas Town] Decision needed: Which caching strategy?
A) Redis  B) In-memory
Reply A or B. Default: A (in 24h)
https://beads.example.com/d/gt-abc123
```

#### Webhook (for custom integrations)

```bash
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -H "X-Beads-Event: decision_point" \
  -d "$NOTIFICATION_PAYLOAD"
```

### Response Interface

#### Webhook Endpoint

```
POST /api/decisions/{decision_id}/respond
Content-Type: application/json

{
  "selected": "a",
  "text": "Let's also add a local fallback cache",
  "respondent": "steve@example.com",
  "auth_token": "<token>"
}
```

Either `selected` or `text` (or both) must be provided.

Response:
```json
{
  "success": true,
  "decision_id": "gt-abc123.decision-deploy",
  "selected": "a",
  "text": "Let's also add a local fallback cache",
  "responded_at": "2026-01-21T15:30:00Z"
}
```

#### Email Reply Parsing

If email provider supports reply parsing:
1. First line/word = option selection (if matches option ID)
2. Remaining text = custom text input
3. Call `bd decision respond` with parsed answer

Example email reply:
```
a

Also consider adding a local fallback for resilience.
```

#### CLI Response (for testing/local use)

```bash
# Select option only
bd decision respond gt-abc123.decision-deploy --select=a

# Text only
bd decision respond gt-abc123.decision-deploy --text="Use a hybrid approach"

# Both
bd decision respond gt-abc123.decision-deploy --select=a --text="Add local fallback too"

# With respondent
bd decision respond gt-abc123.decision-deploy --select=a --by="steve@example.com"
```

### Configuration

Extend `settings/escalation.json` with decision routing:

```json
{
  "type": "escalation",
  "version": 2,
  "routes": {
    "low": ["bead"],
    "medium": ["bead", "mail:mayor"],
    "high": ["bead", "mail:mayor", "email:human"],
    "critical": ["bead", "mail:mayor", "email:human", "sms:human"]
  },
  "decision_routes": {
    "default": ["email:human", "webhook"],
    "urgent": ["email:human", "sms:human", "webhook"]
  },
  "contacts": {
    "human_email": "steve@example.com",
    "human_sms": "+1234567890",
    "decision_webhook": "https://beads.example.com/api/notify"
  },
  "decision_settings": {
    "default_timeout": "24h",
    "remind_interval": "4h",
    "max_reminders": 3
  }
}
```

### Security Considerations

1. **Response Authentication**
   - Signed tokens in response URLs (HMAC)
   - Token includes: decision_id, expiry, expected_respondent
   - Token validated before accepting response

2. **Rate Limiting**
   - Max 1 response per decision (idempotent)
   - Rate limit notification sends (prevent spam)

3. **Respondent Verification**
   - Verify respondent matches expected recipient
   - Or accept any response from authorized domain
   - Configurable strictness level

---

## Part 3: Agent-Side API (hq-946577.6)

### Command: bd decision

New subcommand for decision point management:

```
bd decision <subcommand>

Subcommands:
  create    Create a new decision point
  respond   Record a response to a decision point
  list      List pending decision points
  show      Show decision point details
  remind    Send reminder for pending decision
  cancel    Cancel a pending decision point
```

### bd decision create

```bash
bd decision create \
  --prompt="Which caching strategy should we use?" \
  --options='[{"id":"a","label":"Redis"},{"id":"b","label":"In-memory"}]' \
  --default=a \
  --timeout=24h \
  --parent=gt-abc123 \
  --blocks=gt-abc123.4 \
  [--priority=high] \
  [--no-notify]
```

**Flags:**
- `--prompt` (required): The question shown to the human
- `--options`: JSON array of options (empty = text-only decision)
- `--default`: Default option if timeout (empty = no default)
- `--timeout`: How long to wait (default: 24h from config)
- `--parent`: Parent issue (molecule)
- `--blocks`: Issue(s) this decision blocks
- `--priority`: Notification priority (low/default/high/urgent)
- `--no-notify`: Don't send notifications (manual/testing)

**Output:**
```
✓ Created decision point: gt-abc123.decision-1

  Which caching strategy should we use?

  [a] Redis
  [b] In-memory (default)

  Or provide custom text response.

  Timeout: 2026-01-22 10:00 UTC (24h)
  Blocks: gt-abc123.4

  📧 Notifications sent to: steve@example.com
```

### bd decision respond

```bash
bd decision respond <decision-id> [--select=<option-id>] [--text="..."] [--by=<respondent>]
```

Either `--select` or `--text` (or both) must be provided.

**Examples:**
```bash
# Select an option
bd decision respond gt-abc123.decision-1 --select=a

# Provide text only (no option selected)
bd decision respond gt-abc123.decision-1 --text="Use a hybrid approach instead"

# Select option AND provide additional context
bd decision respond gt-abc123.decision-1 --select=a --text="Also add local fallback"

# With respondent identity
bd decision respond gt-abc123.decision-1 --select=a --by="steve@example.com"
```

**Behavior:**
1. Validates `--select` matches a valid option ID (if provided)
2. Sets `decision_selected` and/or `decision_text` fields
3. Sets `decision_responded_at` and `decision_responded_by`
4. Closes the gate (unblocks waiting issues)
5. Notifies waiters via mail

### bd decision list

```bash
bd decision list [--pending] [--all] [--parent=<id>]
```

**Output:**
```
📋 Pending Decisions (2)

  ○ gt-abc123.decision-1 - Which caching strategy?
    Options: [a] Redis, [b] In-memory
    Created: 2h ago · Timeout: 22h · Blocks: gt-abc123.4

  ○ gt-def456.decision-1 - Proceed with migration?
    Options: [yes] Yes, [no] No
    Created: 1d ago · Timeout: OVERDUE · Blocks: gt-def456.3

Use 'bd decision show <id>' for details
```

### bd decision show

```bash
bd decision show <decision-id>
```

**Output (pending):**
```
○ gt-abc123.decision-1 · Which caching strategy should we use?   [● P2 · OPEN]
Created: 2026-01-21 10:00 UTC · Timeout: 2026-01-22 10:00 UTC (22h remaining)

PROMPT
Which caching strategy should we use for the API rate limiter?
We need to decide before implementing the rate limiting feature.

OPTIONS
  [a] Redis (default)
      Proven technology, 10ms p99 latency, requires Redis cluster

  [b] In-memory LRU
      Zero external deps, process-local only, lost on restart

  Or provide custom text response.

METADATA
  Parent: gt-abc123
  Blocks: gt-abc123.4 (Implement rate limiting)
  Waiters: beads/crew/decision_point
  Notifications: email:steve@example.com

STATUS: ⏳ Awaiting response (22h remaining)
```

**Output (resolved):**
```
✓ gt-abc123.decision-1 · Which caching strategy should we use?   [● P2 · CLOSED]
Created: 2026-01-21 10:00 UTC · Resolved: 2026-01-21 15:30 UTC

RESPONSE
  Selected: [a] Redis
  Text: "Also add a local fallback for resilience"
  Responded by: steve@example.com

PROMPT
Which caching strategy should we use for the API rate limiter?

OPTIONS
  [a] Redis ← SELECTED
  [b] In-memory LRU

STATUS: ✓ Resolved
```

### Formula Integration

Decision points can be defined in formulas:

```toml
formula = "feature-with-approval"

[[steps]]
id = "implement"
title = "Implement the feature"

[[steps]]
id = "design-approval"
title = "Approve design approach"
needs = ["implement"]

[steps.decision]
prompt = "Review the implementation and choose deployment strategy:"
options = [
  { id = "immediate", label = "Deploy immediately", description = "Push to production now" },
  { id = "staged", label = "Staged rollout", description = "10% -> 50% -> 100% over 3 days" },
  { id = "hold", label = "Hold for review", description = "Schedule review meeting first" }
]
default = "staged"
timeout = "48h"

[[steps]]
id = "deploy"
title = "Deploy the feature"
needs = ["design-approval"]
```

When cooked, this creates:
1. `gt-abc123.1` (implement)
2. `gt-abc123.decision-design-approval` (gate with decision fields)
3. `gt-abc123.2` (deploy) - blocked by the decision gate

### Agent Wait Patterns

**Blocking wait (molecule step):**
```bash
# Agent claims step that depends on decision
bd update gt-abc123.2 --status=in_progress

# Step is blocked - agent sees this:
bd show gt-abc123.2
# Status: blocked
# Blocked by: gt-abc123.decision-design-approval (decision - pending)
```

**Non-blocking wait (parallel work):**
```bash
# Create decision but continue other work
bd decision create --prompt="Proceed with migration?" \
  --options='[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}]'

# Later, check status
bd decision list --pending

# Or poll for resolution
bd show gt-abc123.decision-1 --json | jq '.decision_selected, .decision_text'
```

**Webhook notification on resolution:**

Agents can register as waiters:
```bash
bd decision create ... --waiter=beads/crew/decision_point
```

When resolved, waiters receive mail:
```
Subject: Decision resolved: gt-abc123.decision-1
Body: Selected: "a" (Redis)
      Text: "Also add local fallback"
      By: steve@example.com
```

---

## Part 4: CLI Integration for LLM Training (hq-946577.7)

Decision points should be visible and natural in CLI output so LLMs learn to use them.

### Goal

Make decision points a first-class concept that LLMs see repeatedly in:
1. Command output (prompts, hints, examples)
2. Blocked work messages
3. Ready work listings
4. Help text and documentation

### Integration Points

#### 1. bd ready - Show Pending Decisions

When decisions are pending, show them prominently:

```
📋 Ready Work (3 issues)

  ⏳ DECISION PENDING: gt-abc123.decision-1
     Which caching strategy should we use?
     Options: [a] Redis, [b] In-memory
     → Respond: bd decision respond gt-abc123.decision-1 --select=<option>

  ○ gt-def456.2 [P1] Implement user auth
  ○ gt-ghi789.1 [P2] Fix login bug
```

#### 2. bd show - Decision Prompts in Blocked Issues

When showing an issue blocked by a decision:

```
○ gt-abc123.4 · Implement rate limiting   [● P2 · BLOCKED]

  ⚠️  BLOCKED BY DECISION: gt-abc123.decision-1

  Which caching strategy should we use?

  [a] Redis (default)
  [b] In-memory LRU

  → To unblock: bd decision respond gt-abc123.decision-1 --select=a
  → Or provide text: bd decision respond gt-abc123.decision-1 --text="..."
```

#### 3. bd hooks - Decision Point Hooks

Add hooks that fire on decision events:

```yaml
# .beads/config.yaml
hooks:
  on_decision_create: ".beads/hooks/on_decision_create"
  on_decision_respond: ".beads/hooks/on_decision_respond"
  on_decision_timeout: ".beads/hooks/on_decision_timeout"
```

Hook receives decision JSON on stdin:
```json
{
  "id": "gt-abc123.decision-1",
  "prompt": "Which caching strategy?",
  "options": [...],
  "event": "create|respond|timeout",
  "response": {"selected": "a", "text": "..."}
}
```

#### 4. System Prompts - Decision Point Awareness

Add decision point context to system prompts (CLAUDE.md, AGENTS.md):

```markdown
## Decision Points

When you need human input on a choice, create a decision point:

\`\`\`bash
bd decision create \
  --prompt="Which approach should we use?" \
  --options='[{"id":"a","label":"Option A"},{"id":"b","label":"Option B"}]' \
  --blocks=<issue-to-block>
\`\`\`

The human will be notified and can respond asynchronously.
Check status with: bd decision list --pending
```

#### 5. Startup Hook - Pending Decision Reminder

On session start, remind about pending decisions:

```
🚀 beads Crew decision_point, checking in.

⏳ You have 2 pending decisions awaiting human response:
   → gt-abc123.decision-1: Which caching strategy? (2h remaining)
   → gt-def456.decision-1: Proceed with migration? (OVERDUE)

   View: bd decision list --pending
```

#### 6. bd mol status - Decision State in Molecule Progress

```
📊 Molecule: gt-abc123 (Feature: Rate Limiting)

  ✓ gt-abc123.1 Implement core logic
  ⏳ gt-abc123.decision-1 DECISION: Which caching strategy? [PENDING]
  ○ gt-abc123.2 Implement caching (blocked by decision)
  ○ gt-abc123.3 Add tests

  Progress: 1/4 (25%) · Blocked: 1 decision pending
```

### LLM Training Signals

The key is **repetition and consistency**. LLMs learn patterns from:

1. **Consistent formatting** - Always show decisions the same way
2. **Action hints** - Always show the command to respond
3. **Status indicators** - ⏳ for pending, ✓ for resolved
4. **Blocking visibility** - Make it clear what's blocked and why

### Implementation Tasks

- [ ] Add `bd decision` command group
- [ ] Show pending decisions in `bd ready` output
- [ ] Show decision details in blocked issue `bd show`
- [ ] Add decision hooks to hook system
- [ ] Update CLAUDE.md with decision point documentation
- [ ] Add pending decision reminder to startup hook
- [ ] Show decision state in `bd mol status`

---

## Implementation Plan

### Phase 1: Core Data Model
1. Add decision fields to Issue struct
2. Database migration for new columns
3. JSONL import/export support
4. Update content hash

### Phase 2: Basic Commands
1. `bd decision create` (local, no notifications)
2. `bd decision respond`
3. `bd decision list`
4. `bd decision show`

### Phase 3: Formula Integration
1. Parse `[steps.decision]` in formula TOML
2. Cook creates decision gates
3. Blocking works via standard gate mechanism

### Phase 4: Notification System
1. Notification dispatch on create
2. Email integration
3. Webhook integration
4. Response webhook endpoint

### Phase 5: Production Hardening
1. Reminder system
2. Token-based authentication
3. Rate limiting
4. Monitoring/metrics

---

## Open Questions

1. **Reminder behavior**: Should reminders go to same channels or escalate?
   - Proposal: Same channels, with escalation to mayor after N reminders

2. **Multi-respondent**: Should decisions support multiple approvers?
   - Proposal: Future enhancement, v1 is single respondent

3. **Response editing**: Can human change their answer?
   - Proposal: No, one response is final (create new decision if needed)

4. **Offline mode**: What if external services are unreachable?
   - Proposal: Queue notifications, retry with backoff, log failures

---

## Related Documents

- [Gates Documentation](../../../mayor/rig/website/docs/workflows/gates.md)
- [Escalation System Design](../../../../gastown/refinery/rig/docs/design/escalation-system.md)
- [Decision Protocol (scripts)](../../../../gastown/mayor/rig/scripts/decision-receiver.sh)

---

## Appendix: Prior Art Summary

### Existing gastown decision-receiver.sh
- Types: yesno, choice, multiselect (we simplified to single-select + text)
- IPC via named pipes (FIFOs)
- Local terminal/notification presentation
- No persistence, no remote async

### Existing gate system
- Types: human, timer, gh:run, gh:pr, bead, mail
- Blocking via dependency
- Resolution via `bd gate check` or `bd gate resolve`
- Timeout and escalation support
- **Decision points extend this** with `await_type = "decision"`

### Escalation system design
- Severity-based routing
- Multiple channels (bead, mail, email, SMS, Slack)
- Stale detection and re-escalation
- Config-driven
- **Decision points use this** for notification delivery

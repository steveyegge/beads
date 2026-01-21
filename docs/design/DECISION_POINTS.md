# Decision Points Design Specification

> **Epic**: hq-946577 - Decision Point Beads
> **Author**: beads/crew/decision_point
> **Date**: 2026-01-21
> **Status**: Draft

## Executive Summary

Decision Points are a new beads feature for remote, asynchronous human-in-the-loop decisions. An agent can create a Decision Point with multiple-choice options (or yes/no, or text input), and the system notifies the human via external channels (email, phone app, web). The human responds asynchronously, and the agent's workflow unblocks.

## Design Principles

1. **Extend, don't reinvent** - Build on the existing gate system, which already provides blocking semantics
2. **Remote-first** - Assume human is not at terminal; notifications go through external channels
3. **Async by default** - Agent can continue other work while waiting, or block if needed
4. **Rich content** - Options can contain substantial content (design docs, not just labels)
5. **Audit trail** - All decisions are beads, creating permanent record

---

## Part 1: Data Model (hq-946577.4)

### Approach: Extend Gate System

Decision Points are a specialized type of gate. This reuses:
- Blocking via `blocks` dependency type
- `blocked_issues_cache` for O(1) ready detection
- Gate resolution patterns (`bd gate check`, `bd gate resolve`)
- Waiters notification system
- Timeout handling

### New Issue Fields

Add these fields to the Issue struct in `internal/types/types.go`:

```go
// ===== Decision Point Fields (human-in-the-loop choices) =====
// Decision points are gates that wait for structured human input

// DecisionType specifies the input format expected
// Values: "yesno", "choice", "multiselect", "text"
DecisionType string `json:"decision_type,omitempty"`

// DecisionContext is the question or prompt shown to the human
// Can contain markdown for rich formatting
DecisionContext string `json:"decision_context,omitempty"`

// DecisionOptions are the available choices (JSON array of Option objects)
// For yesno: can be empty (defaults to yes/no)
// For choice/multiselect: required list of options
// For text: can contain placeholder/hint
DecisionOptions string `json:"decision_options,omitempty"` // JSON array

// DecisionDefault is the option selected if timeout occurs
// For yesno: "yes" or "no"
// For choice: option ID
// For multiselect: JSON array of option IDs
DecisionDefault string `json:"decision_default,omitempty"`

// DecisionResponse is the human's answer (set when resolved)
// For yesno: "yes" or "no"
// For choice: selected option ID
// For multiselect: JSON array of selected option IDs
// For text: the entered text
DecisionResponse string `json:"decision_response,omitempty"`

// DecisionRespondedAt is when the human responded
DecisionRespondedAt *time.Time `json:"decision_responded_at,omitempty"`

// DecisionRespondedBy identifies who responded (email, user ID, etc.)
DecisionRespondedBy string `json:"decision_responded_by,omitempty"`

// DecisionAllowCustom permits custom text input in addition to options
// For choice: human can type alternative instead of picking an option
DecisionAllowCustom bool `json:"decision_allow_custom,omitempty"`
```

### Option Schema

Decision options are stored as JSON in `DecisionOptions`:

```go
// DecisionOption represents one choice in a decision point
type DecisionOption struct {
    // ID is the short identifier (e.g., "a", "b", "option-1")
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

### AwaitType Values

New `await_type` values for decision points:

| AwaitType | Description |
|-----------|-------------|
| `decision:yesno` | Binary yes/no choice |
| `decision:choice` | Single selection from options |
| `decision:multiselect` | Multiple selections allowed |
| `decision:text` | Free text input |

### Issue Type

Decision points use `type = "gate"` (not a new type) to reuse gate infrastructure.

Filtering for decision points: `await_type LIKE 'decision:%'`

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
                ALTER TABLE issues ADD COLUMN decision_type TEXT;
                ALTER TABLE issues ADD COLUMN decision_context TEXT;
                ALTER TABLE issues ADD COLUMN decision_options TEXT;
                ALTER TABLE issues ADD COLUMN decision_default TEXT;
                ALTER TABLE issues ADD COLUMN decision_response TEXT;
                ALTER TABLE issues ADD COLUMN decision_responded_at TEXT;
                ALTER TABLE issues ADD COLUMN decision_responded_by TEXT;
                ALTER TABLE issues ADD COLUMN decision_allow_custom INTEGER DEFAULT 0;
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
  "id": "gt-abc123.gate-deploy",
  "decision_type": "choice",
  "context": "Which caching strategy should we use?",
  "options": [
    {"id": "a", "label": "Redis", "description": "..."},
    {"id": "b", "label": "In-memory", "description": "..."}
  ],
  "default": "a",
  "timeout_at": "2026-01-22T10:00:00Z",
  "respond_url": "https://beads.example.com/api/decisions/gt-abc123.gate-deploy/respond",
  "view_url": "https://beads.example.com/decisions/gt-abc123.gate-deploy",
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
  "response": "a",
  "respondent": "steve@example.com",
  "auth_token": "<token>"
}
```

Response:
```json
{
  "success": true,
  "decision_id": "gt-abc123.gate-deploy",
  "response": "a",
  "responded_at": "2026-01-21T15:30:00Z"
}
```

#### Email Reply Parsing

If email provider supports reply parsing:
1. Extract response from email body (first word/line)
2. Validate against options
3. Call `bd decision respond` with parsed answer

#### CLI Response (for testing/local use)

```bash
bd decision respond gt-abc123.gate-deploy --answer=a --by="steve@example.com"
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
  --type=choice \
  --context="Which caching strategy should we use?" \
  --options='[{"id":"a","label":"Redis"},{"id":"b","label":"In-memory"}]' \
  --default=a \
  --timeout=24h \
  --parent=gt-abc123 \
  --blocks=gt-abc123.4 \
  [--priority=high] \
  [--no-notify]
```

**Flags:**
- `--type` (required): yesno, choice, multiselect, or text
- `--context` (required): The question/prompt
- `--options`: JSON array of options (required for choice/multiselect)
- `--default`: Default option if timeout
- `--timeout`: How long to wait (default: 24h from config)
- `--parent`: Parent issue (molecule)
- `--blocks`: Issue(s) this decision blocks
- `--priority`: Notification priority (low/default/high/urgent)
- `--no-notify`: Don't send notifications (manual/testing)

**Output:**
```
✓ Created decision point: gt-abc123.decision-1
  Type: choice
  Context: Which caching strategy should we use?
  Options: [a] Redis, [b] In-memory
  Default: a (if no response by 2026-01-22 10:00 UTC)
  Blocks: gt-abc123.4

  Notifications sent:
  → Email: steve@example.com
  → Webhook: https://beads.example.com/api/notify
```

### bd decision respond

```bash
bd decision respond <decision-id> --answer=<response> [--by=<respondent>]
```

**Examples:**
```bash
# Yes/no
bd decision respond gt-abc123.decision-1 --answer=yes

# Choice
bd decision respond gt-abc123.decision-1 --answer=a

# Multiselect
bd decision respond gt-abc123.decision-1 --answer='["a","c"]'

# Text
bd decision respond gt-abc123.decision-1 --answer="Use hybrid approach with Redis primary and in-memory fallback"

# With respondent
bd decision respond gt-abc123.decision-1 --answer=a --by="steve@example.com"
```

**Behavior:**
1. Validates answer matches decision type/options
2. Sets `decision_response` field
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

  ○ gt-abc123.decision-1 [choice] - Which caching strategy?
    Created: 2h ago · Timeout: 22h · Blocks: gt-abc123.4

  ○ gt-def456.decision-1 [yesno] - Proceed with migration?
    Created: 1d ago · Timeout: 0h (OVERDUE) · Blocks: gt-def456.3

Use 'bd decision show <id>' for details
```

### bd decision show

```bash
bd decision show <decision-id>
```

**Output:**
```
○ gt-abc123.decision-1 · Which caching strategy should we use?   [● P2 · OPEN]
Type: choice · Created: 2026-01-21 10:00 UTC · Timeout: 2026-01-22 10:00 UTC

CONTEXT
Which caching strategy should we use for the API rate limiter?
We need to decide before implementing the rate limiting feature.

OPTIONS
  [a] Redis (default)
      Proven technology, 10ms p99 latency, requires Redis cluster

  [b] In-memory LRU
      Zero external deps, process-local only, lost on restart

METADATA
  Parent: gt-abc123
  Blocks: gt-abc123.4 (Implement rate limiting)
  Waiters: beads/crew/decision_point
  Notifications: email:steve@example.com, webhook

STATUS
  ○ Pending response
  Reminders sent: 0
  Time remaining: 22h
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
type = "choice"
context = "Review the implementation and choose deployment strategy:"
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
# Blocked by: gt-abc123.decision-design-approval (decision:choice - pending)
```

**Non-blocking wait (parallel work):**
```bash
# Create decision but continue other work
bd decision create --type=yesno --context="Proceed?" --no-block

# Later, check status
bd decision list --pending

# Or poll for resolution
bd show gt-abc123.decision-1 --json | jq '.decision_response'
```

**Webhook notification on resolution:**

Agents can register as waiters:
```bash
bd decision create ... --waiter=beads/crew/decision_point
```

When resolved, waiters receive mail:
```
Subject: Decision resolved: gt-abc123.decision-1
Body: Response: "a" (Redis) by steve@example.com
```

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
- Types: yesno, choice, multiselect
- IPC via named pipes (FIFOs)
- Local terminal/notification presentation
- No persistence, no remote async

### Existing gate system
- Types: human, timer, gh:run, gh:pr, bead, mail
- Blocking via dependency
- Resolution via `bd gate check` or `bd gate resolve`
- Timeout and escalation support

### Escalation system design
- Severity-based routing
- Multiple channels (bead, mail, email, SMS, Slack)
- Stale detection and re-escalation
- Config-driven

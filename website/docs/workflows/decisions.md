---
id: decisions
title: Decision Points
sidebar_position: 5
---

# Decision Points

Decision points are human-in-the-loop gates that allow agents to request input from humans during autonomous workflow execution.

## What are Decision Points?

Unlike [gates](/workflows/gates) which block on conditions (human approval, timers, CI), decision points:

- Present a **question** with multiple **options**
- Support **text guidance** for refining options
- Enable **iterative refinement** via feedback loops
- Work asynchronously across sessions

Decision points are ideal when agents need human judgment on:
- Architectural choices (which database, which framework)
- Business decisions (feature prioritization, scope tradeoffs)
- Ambiguous requirements (clarifying vague instructions)

## Creating a Decision Point

### Via CLI

```bash
bd decision create bd-xyz \
  --prompt "Which caching strategy should we use?" \
  --option "redis:Use Redis for distributed caching" \
  --option "memcached:Use Memcached for simple caching" \
  --option "local:Use in-memory cache (single node)"
```

### Via Formula

```toml
[[steps]]
id = "choose-cache"
title = "Select caching strategy"
type = "decision"

[steps.decision]
prompt = "Which caching strategy should we use?"

[[steps.decision.options]]
key = "redis"
label = "Redis"
description = "Distributed caching with persistence"

[[steps.decision.options]]
key = "memcached"
label = "Memcached"
description = "Simple, fast distributed cache"

[[steps.decision.options]]
key = "local"
label = "Local"
description = "In-memory cache, single node only"
```

## Responding to Decisions

### Check Pending Decisions

```bash
# See all pending decisions
bd decision list

# Decisions also appear in bd ready output
bd ready
```

### Select an Option

```bash
bd decision respond hq-abc123 --select redis
```

### Provide Text Guidance

Instead of selecting an option, provide feedback for refinement:

```bash
bd decision respond hq-abc123 \
  --guidance "Redis seems overkill. Can you propose a simpler solution that still handles 1000 req/s?"
```

### Accept As-Is

The special `_accept` option accepts the current guidance without further refinement:

```bash
bd decision respond hq-abc123 --select _accept
```

## Iterative Refinement

When you provide text guidance instead of selecting an option, beads creates a new iteration:

1. Agent receives the guidance
2. Agent creates refined options based on feedback
3. New decision point created (ID: `original.r2`, `original.r3`, etc.)
4. Cycle continues until human selects an option

### Max Iterations

To prevent infinite loops, decisions have a configurable max iterations limit (default: 3). When reached:

- If `auto-accept-on-max` is enabled: last guidance is auto-accepted
- Otherwise: human must select from available options

## Formula Integration

### Basic Decision Step

```toml
[[steps]]
id = "api-design"
title = "Choose API approach"
type = "decision"

[steps.decision]
prompt = "How should we design the API?"
timeout = "48h"

[[steps.decision.options]]
key = "rest"
label = "REST"
description = "Standard REST endpoints"

[[steps.decision.options]]
key = "graphql"
label = "GraphQL"
description = "Single flexible endpoint"
```

### Decision with Dependencies

```toml
[[steps]]
id = "implement-api"
title = "Implement the API"
needs = ["api-design"]
```

The `implement-api` step blocks until the decision is resolved.

### Conditional Branching

```toml
[[steps]]
id = "setup-graphql"
title = "Set up GraphQL server"
needs = ["api-design"]
when = "{{decision.api-design}} == 'graphql'"

[[steps]]
id = "setup-rest"
title = "Set up REST endpoints"
needs = ["api-design"]
when = "{{decision.api-design}} == 'rest'"
```

## Configuration

Configure decision behavior in `.beads/config.yaml`:

```yaml
decision:
  routes:
    default:
      - email
      - webhook
    urgent:
      - email
      - sms
      - webhook

  settings:
    default-timeout: 24h     # How long before timeout
    remind-interval: 4h      # Reminder frequency
    max-reminders: 3         # Maximum reminders to send
    max-iterations: 3        # Maximum refinement iterations
    auto-accept-on-max: false # Auto-accept on max iterations
```

### Notification Routes

Routes are selected based on priority:
- **P0, P1 issues**: Use `urgent` routes (email + SMS + webhook)
- **P2+ issues**: Use `default` routes (email + webhook)

## Hooks

Decision points trigger lifecycle hooks:

| Hook | Triggered When |
|------|----------------|
| `on_decision_create` | New decision point created |
| `on_decision_respond` | Human responds to decision |
| `on_decision_timeout` | Decision times out |

### Hook Script Example

```bash
#!/bin/bash
# .beads/hooks/on_decision_create

# Send Slack notification
curl -X POST "$SLACK_WEBHOOK" \
  -d "{\"text\": \"Decision needed: $BD_DECISION_PROMPT\"}"
```

### Hook Environment Variables

| Variable | Description |
|----------|-------------|
| `BD_DECISION_ID` | Decision point ID |
| `BD_DECISION_PROMPT` | The question being asked |
| `BD_DECISION_OPTIONS` | JSON array of options |
| `BD_DECISION_PRIOR_ID` | Prior issue ID |
| `BD_DECISION_RESPONSE` | Selected option (respond hook) |
| `BD_DECISION_GUIDANCE` | Text guidance (respond hook) |

## CLI Reference

| Command | Description |
|---------|-------------|
| `bd decision create` | Create a decision point |
| `bd decision list` | List pending decisions |
| `bd decision show <id>` | Show decision details |
| `bd decision respond <id>` | Respond to a decision |
| `bd decision remind <id>` | Send reminder |
| `bd decision cancel <id>` | Cancel pending decision |

### Common Options

```bash
# Create with priority
bd decision create bd-xyz --prompt "..." --priority 1

# Create with timeout
bd decision create bd-xyz --prompt "..." --timeout 48h

# Respond with reason
bd decision respond bd-xyz --select redis --reason "Best for our scale"
```

## Best Practices

1. **Write clear prompts** - Include context the human needs to decide
2. **Provide meaningful options** - Include descriptions explaining tradeoffs
3. **Set appropriate timeouts** - Match urgency to importance
4. **Use iteration sparingly** - Prefer well-defined options over open-ended refinement
5. **Include a default option** - Mark the recommended choice when there's a clear winner

## Examples

### Architecture Decision

```bash
bd decision create bd-feature-123 \
  --prompt "The feature needs real-time updates. Which approach?" \
  --option "websocket:WebSocket - bidirectional, complex" \
  --option "sse:Server-Sent Events - simpler, one-way" \
  --option "polling:Long polling - simplest, higher latency"
```

### Scope Clarification

```bash
bd decision create bd-story-456 \
  --prompt "Story mentions 'user notifications'. What scope?" \
  --option "email:Email notifications only" \
  --option "push:Push notifications (mobile)" \
  --option "both:Both email and push" \
  --option "later:Defer to future story"
```

### Formula-Driven Decision

```toml
formula = "database-migration"

[[steps]]
id = "analyze"
title = "Analyze current schema"

[[steps]]
id = "choose-strategy"
title = "Select migration strategy"
needs = ["analyze"]
type = "decision"

[steps.decision]
prompt = "Migration involves breaking changes. How to proceed?"
timeout = "72h"

[[steps.decision.options]]
key = "blue-green"
label = "Blue-Green"
description = "Run old and new in parallel, switch at cutover"

[[steps.decision.options]]
key = "rolling"
label = "Rolling"
description = "Gradual migration with compatibility layer"

[[steps.decision.options]]
key = "downtime"
label = "Scheduled Downtime"
description = "Faster migration, requires maintenance window"

[[steps]]
id = "execute"
title = "Execute migration"
needs = ["choose-strategy"]
```

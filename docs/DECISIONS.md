# Decisions (Human-in-the-Loop Gates)

**Status:** Storage layer implemented, CLI commands planned
**Version:** 0.22.0+

## Overview

Decision Points provide structured human-in-the-loop gates for agent workflows. When an agent needs human input to proceed, it creates a decision point attached to an issue. The human responds with a choice (or custom text), and the agent continues.

**Key use cases:**
- Architecture choices ("Redis vs in-memory caching?")
- Approval gates ("Deploy to production?")
- Clarification requests ("Which authentication method?")
- Iterative refinement ("Is this design acceptable?")

## Concepts

### Decision Point

A decision point is a structured request for human input, attached to an issue:

```
Issue (bd-abc123)
  └── DecisionPoint
        ├── prompt: "Which caching strategy should we use?"
        ├── options: [{id: "redis", label: "Use Redis"}, {id: "memory", label: "In-memory"}]
        ├── default_option: "redis" (auto-selected on timeout)
        └── selected_option: null (waiting for response)
```

### Options

Each option has:
- **id**: Short identifier ("a", "b", "yes", "no", "redis")
- **short**: 1-3 word summary for compact display ("Redis", "In-memory")
- **label**: Sentence description ("Use Redis for distributed caching")
- **description**: Optional rich markdown content

### Iteration

Decisions support iterative refinement:
1. Agent proposes options
2. Human provides guidance (not selecting an option)
3. Agent refines options based on guidance
4. Repeat until human selects or accepts

The `_accept` option is automatically added for iteration > 1, allowing humans to accept the current proposal.

## Data Model

### DecisionPoint Type

```go
type DecisionPoint struct {
    IssueID        string     // Parent issue ID
    Prompt         string     // Question to ask human
    Options        string     // JSON array of DecisionOption
    DefaultOption  string     // Option ID selected on timeout
    SelectedOption string     // Option ID the human chose
    ResponseText   string     // Custom text input from human
    RespondedAt    *time.Time // When human responded
    RespondedBy    string     // Who responded
    Iteration      int        // Current iteration (1-indexed)
    MaxIterations  int        // Limit on refinement loops (default: 3)
    PriorID        string     // Previous iteration's issue ID
    Guidance       string     // Human's text that triggered this iteration
    RequestedBy    string     // Agent/session that created this decision
    CreatedAt      time.Time
}
```

### DecisionOption Type

```go
type DecisionOption struct {
    ID          string // Short identifier
    Short       string // 1-3 word summary
    Label       string // Sentence description
    Description string // Optional markdown content
}
```

## Storage API

The storage interface provides four operations:

```go
// Create a new decision point
CreateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error

// Get decision point by issue ID
GetDecisionPoint(ctx context.Context, issueID string) (*types.DecisionPoint, error)

// Update an existing decision point
UpdateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error

// List all pending (unresponded) decisions
ListPendingDecisions(ctx context.Context) ([]*types.DecisionPoint, error)
```

## Planned CLI Commands

The following CLI commands are planned for the decisions feature:

### List Pending Decisions

```bash
# List all decisions awaiting response
bd decision list --json

# Output:
# [
#   {
#     "issue_id": "bd-abc123",
#     "prompt": "Which caching strategy?",
#     "options": [...],
#     "created_at": "2024-01-15T10:30:00Z"
#   }
# ]
```

### Show Decision Details

```bash
# Show decision for a specific issue
bd decision show bd-abc123 --json
```

### Respond to Decision

```bash
# Select an option
bd decision respond bd-abc123 --option redis --json

# Provide custom response
bd decision respond bd-abc123 --text "Use a hybrid approach" --json

# Provide guidance for iteration
bd decision respond bd-abc123 --guidance "Consider memory constraints" --json
```

### Create Decision (for testing/manual use)

```bash
# Create decision point on existing issue
bd decision create bd-abc123 \
  --prompt "Which approach?" \
  --option "a:Use approach A" \
  --option "b:Use approach B" \
  --default a \
  --json
```

## Integration Patterns

### Agent Workflow

```
Agent starts work
    ↓
Agent encounters choice point
    ↓
Agent creates DecisionPoint on work item
    ↓
Agent sets work item status to "blocked"
    ↓
Agent yields/sleeps
    ↓
[Human reviews and responds]
    ↓
Human response triggers wake notification
    ↓
Agent resumes with selected option
    ↓
Agent continues work
```

### Hook Integration

Claude Code hooks can use decisions for structured input:

```bash
# In a PreToolUse hook, check for pending decisions
pending=$(bd decision list --json 2>/dev/null)
if [ -n "$pending" ]; then
  echo "Pending decisions require attention"
  exit 1  # Block tool execution
fi
```

### Polling Pattern

For systems without push notifications:

```bash
# Check every N seconds for pending decisions
while true; do
  pending=$(bd decision list --json 2>/dev/null | jq length)
  if [ "$pending" -gt 0 ]; then
    # Notify user or trigger handler
    notify-send "BD: $pending decisions pending"
  fi
  sleep 30
done
```

### MCP Server Integration

The beads-mcp server exposes decision operations:

```json
{
  "method": "tools/call",
  "params": {
    "name": "bd_decision_list",
    "arguments": {}
  }
}
```

## Database Schema

```sql
CREATE TABLE decision_points (
    issue_id TEXT PRIMARY KEY,
    prompt TEXT NOT NULL,
    options TEXT NOT NULL,           -- JSON array
    default_option TEXT,
    selected_option TEXT,
    response_text TEXT,
    responded_at DATETIME,
    responded_by TEXT,
    iteration INTEGER DEFAULT 1,
    max_iterations INTEGER DEFAULT 3,
    prior_id TEXT,                   -- FK to issues for iteration chain
    guidance TEXT,
    requested_by TEXT,               -- Agent/session for wake notifications
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (prior_id) REFERENCES issues(id) ON DELETE SET NULL
);
```

## Examples

### Simple Yes/No Decision

```go
dp := &types.DecisionPoint{
    IssueID: "bd-abc123",
    Prompt:  "Deploy to production?",
    Options: `[
        {"id": "yes", "short": "Yes", "label": "Deploy now"},
        {"id": "no", "short": "No", "label": "Wait for review"}
    ]`,
    DefaultOption: "no",
    MaxIterations: 1,
    RequestedBy:   "agent-session-xyz",
}
err := storage.CreateDecisionPoint(ctx, dp)
```

### Multiple Choice with Rich Options

```go
dp := &types.DecisionPoint{
    IssueID: "bd-def456",
    Prompt:  "Which caching strategy should we implement?",
    Options: `[
        {
            "id": "redis",
            "short": "Redis",
            "label": "Use Redis for distributed caching",
            "description": "## Redis\n\nPros:\n- Distributed\n- Persistent\n\nCons:\n- Additional infrastructure"
        },
        {
            "id": "memory",
            "short": "In-memory",
            "label": "Use in-memory LRU cache",
            "description": "## In-Memory\n\nPros:\n- Simple\n- No dependencies\n\nCons:\n- Not shared across instances"
        }
    ]`,
    MaxIterations: 3,
    RequestedBy:   "agent-session-xyz",
}
```

### Handling Response

```go
// After human responds
dp, _ := storage.GetDecisionPoint(ctx, "bd-abc123")
if dp.SelectedOption != "" {
    // Human selected an option
    switch dp.SelectedOption {
    case "redis":
        // Implement Redis caching
    case "memory":
        // Implement in-memory caching
    }
} else if dp.ResponseText != "" {
    // Human provided custom text
    // Parse and handle accordingly
}
```

## See Also

- [MOLECULES.md](MOLECULES.md) - Workflow templates that may use decisions
- [DAEMON.md](DAEMON.md) - Background processing for decision notifications
- [CLI_REFERENCE.md](CLI_REFERENCE.md) - Command reference (decision commands TBD)

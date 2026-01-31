# Advice Hook Schema Design

**Task**: hq--uaim.4
**Date**: 2026-01-31
**Author**: beads/crew/decisions_fix

## Overview

This document defines the schema for advice stop hooks - commands that run at specific
lifecycle points (session-end, before-commit, before-push, before-handoff). This makes
advice actionable, not just guidance.

## Design Decisions

### Storage Approach: Dedicated Fields on Issue Struct

**Chosen**: Add dedicated fields to the Issue struct (similar to existing AdviceTarget* fields).

**Alternatives considered**:
1. **Metadata field**: The Issue.Metadata field (json.RawMessage) could store hook config.
   - Pro: No schema changes
   - Con: No type safety, harder to query, not indexed

2. **Separate table**: Like DecisionPoint, create an AdviceHook table with FK.
   - Pro: Clean separation, can have multiple hooks per advice
   - Con: Over-engineered for 1:1 relationship, adds query complexity

3. **Dedicated fields**: Add new fields alongside AdviceTarget* fields.
   - Pro: Type-safe, queryable, follows existing pattern
   - Con: Adds fields to Issue struct (acceptable given existing precedent)

**Rationale**: The existing advice fields (AdviceTargetRig, AdviceTargetRole, AdviceTargetAgent)
set the pattern. Adding hook fields follows the same approach. The Issue struct already has
many domain-specific field groups (Skill, Gate, Agent, Event). This is the established pattern.

## Schema Definition

### New Fields on types.Issue

```go
// ===== Advice Hook Fields (hq--uaim) =====
// Hook command to execute at trigger point (e.g., "git status", "bd lint")
AdviceHookCommand   string `json:"advice_hook_command,omitempty"`

// Trigger point: session-end, before-commit, before-push, before-handoff
AdviceHookTrigger   string `json:"advice_hook_trigger,omitempty"`

// Timeout in seconds (default: 30, max: 300)
AdviceHookTimeout   int    `json:"advice_hook_timeout,omitempty"`

// Failure behavior: block, warn, ignore (default: warn)
AdviceHookOnFailure string `json:"advice_hook_on_failure,omitempty"`
```

### Field Details

#### AdviceHookCommand (string)
The shell command to execute. Runs in the agent's working directory.

**Examples**:
- `"git status"` - Check for uncommitted changes
- `"bd lint"` - Run bead linting
- `"make test"` - Run tests before commit
- `"gt mail check"` - Check mail before handoff

**Constraints**:
- Max length: 1000 characters
- Empty = no hook (default)

#### AdviceHookTrigger (string, enum)
When the hook should execute.

| Value | Lifecycle Point | Use Case |
|-------|-----------------|----------|
| `session-end` | When agent session ends | Cleanup, status checks |
| `before-commit` | Before `git commit` | Pre-commit validation |
| `before-push` | Before `git push` | Pre-push checks |
| `before-handoff` | Before `gt handoff` | Context preservation |

**Mapping to Claude Code hooks**:
- `session-end` → Claude Code `Stop` hook
- `before-commit`, `before-push` → Not directly mapped (agent-level enforcement)
- `before-handoff` → Custom Gas Town lifecycle hook

**Note**: These are advisory hooks (advice beads), not Claude Code hooks. The hook
execution engine (hq--uaim.6) will invoke these at appropriate lifecycle points by
querying applicable advice and running commands.

#### AdviceHookTimeout (int)
Maximum execution time in seconds.

- Default: 30
- Minimum: 1
- Maximum: 300 (5 minutes)
- Value 0 means use default (30)

#### AdviceHookOnFailure (string, enum)
What happens when the hook command fails (non-zero exit).

| Value | Behavior |
|-------|----------|
| `block` | Abort the lifecycle action (e.g., prevent commit) |
| `warn` | Show warning but continue (default) |
| `ignore` | Silent continue |

## Query Mechanism

### Finding Applicable Hooks for an Agent

Query all advice beads that:
1. Match the agent's context (rig, role, or specific agent)
2. Have a hook command defined
3. Match the current trigger point
4. Are not closed/tombstoned

```sql
SELECT * FROM issues
WHERE issue_type = 'advice'
  AND status NOT IN ('closed', 'tombstone')
  AND advice_hook_command IS NOT NULL
  AND advice_hook_command != ''
  AND advice_hook_trigger = :trigger
  AND (
    -- Global advice (no targeting)
    (advice_target_rig IS NULL OR advice_target_rig = '')
    -- OR matches agent's rig
    OR advice_target_rig = :agent_rig
  )
  AND (
    -- No role targeting
    (advice_target_role IS NULL OR advice_target_role = '')
    -- OR matches agent's role
    OR advice_target_role = :agent_role
  )
  AND (
    -- No agent targeting
    (advice_target_agent IS NULL OR advice_target_agent = '')
    -- OR matches specific agent
    OR advice_target_agent = :agent_id
  )
ORDER BY
  -- More specific targeting wins
  CASE WHEN advice_target_agent != '' THEN 0
       WHEN advice_target_role != '' THEN 1
       WHEN advice_target_rig != '' THEN 2
       ELSE 3
  END,
  -- Then by priority
  priority ASC,
  -- Then by creation date
  created_at ASC;
```

### Storage Layer Changes

Add to `internal/storage/storage.go` interface:
```go
// GetApplicableAdviceHooks returns advice beads with hooks for the given trigger and agent context
GetApplicableAdviceHooks(ctx context.Context, trigger string, agentRig, agentRole, agentID string) ([]*types.Issue, error)
```

## Hook Ordering

When multiple hooks apply for the same trigger:

1. **Specificity** (most specific first):
   - Agent-targeted (`advice_target_agent` set)
   - Role-targeted (`advice_target_role` set)
   - Rig-targeted (`advice_target_rig` set)
   - Global (no targeting)

2. **Within same specificity** - by priority (P0 > P1 > P2 > P3 > P4)

3. **Same priority** - by creation date (oldest first, FIFO)

This ensures:
- Specific overrides can take precedence
- High-priority advice runs first
- Predictable ordering for equal-priority advice

## Validation Rules

### On Create/Update

1. If `advice_hook_trigger` is set, it must be a valid enum value
2. If `advice_hook_on_failure` is set, it must be a valid enum value
3. If `advice_hook_timeout` is set, it must be in range [1, 300]
4. `advice_hook_command` can be any non-empty string up to 1000 chars
5. Hook fields only valid when `issue_type = 'advice'`

### Type Constants

Add to `internal/types/types.go`:

```go
// Advice hook trigger constants
const (
    AdviceHookTriggerSessionEnd    = "session-end"
    AdviceHookTriggerBeforeCommit  = "before-commit"
    AdviceHookTriggerBeforePush    = "before-push"
    AdviceHookTriggerBeforeHandoff = "before-handoff"
)

// ValidAdviceHookTriggers lists all valid trigger values
var ValidAdviceHookTriggers = []string{
    AdviceHookTriggerSessionEnd,
    AdviceHookTriggerBeforeCommit,
    AdviceHookTriggerBeforePush,
    AdviceHookTriggerBeforeHandoff,
}

// Advice hook failure behavior constants
const (
    AdviceHookOnFailureBlock  = "block"
    AdviceHookOnFailureWarn   = "warn"
    AdviceHookOnFailureIgnore = "ignore"
)

// ValidAdviceHookOnFailure lists all valid failure behaviors
var ValidAdviceHookOnFailure = []string{
    AdviceHookOnFailureBlock,
    AdviceHookOnFailureWarn,
    AdviceHookOnFailureIgnore,
}

// AdviceHookTimeoutDefault is the default timeout in seconds
const AdviceHookTimeoutDefault = 30

// AdviceHookTimeoutMax is the maximum allowed timeout in seconds
const AdviceHookTimeoutMax = 300
```

## CLI Changes

### bd advice add

Extend with hook options:

```bash
bd advice add "Always run tests" \
  --rig beads \
  --role polecat \
  --hook-command "make test" \
  --hook-trigger before-commit \
  --hook-timeout 60 \
  --hook-on-failure block
```

### bd advice list

Show hook info in list output:

```
  bd-abc123 · Always run tests [before-commit: make test] (block)
  Target: beads/polecat
```

### bd advice hooks

New subcommand to list hooks for current context:

```bash
bd advice hooks                    # List all applicable hooks
bd advice hooks --trigger session-end  # Filter by trigger
bd advice hooks --dry-run          # Show what would run
```

## Migration

No database migration needed - new fields are nullable/optional. Existing advice
beads without hooks continue to work as pure guidance.

## Content Hash Update

Add new fields to `ComputeContentHash()`:

```go
// Advice hook fields
w.str(i.AdviceHookCommand)
w.str(i.AdviceHookTrigger)
w.int(i.AdviceHookTimeout)
w.str(i.AdviceHookOnFailure)
```

## Example Advice Bead with Hook

```json
{
  "id": "bd-adv-pre_commit_tests",
  "title": "Run tests before committing",
  "description": "All code changes must pass tests before commit.",
  "issue_type": "advice",
  "status": "pinned",
  "advice_target_rig": "beads",
  "advice_target_role": "polecat",
  "advice_hook_command": "make test",
  "advice_hook_trigger": "before-commit",
  "advice_hook_timeout": 120,
  "advice_hook_on_failure": "block"
}
```

## Security Considerations

1. **Command injection**: Hook commands run as the agent's user. Commands come from
   trusted advice beads created by authorized users. No user input in commands.

2. **Timeout enforcement**: Mandatory timeout prevents runaway processes.

3. **Scope limitation**: Hooks run in agent's working directory with agent's
   permissions. No privilege escalation.

4. **Audit trail**: Hook execution logged via existing beads event system.

## Implementation Order

1. **hq--uaim.4** (this task): Schema design ✓
2. **hq--uaim.5**: Add fields to types.Issue, update validation, CLI
3. **hq--uaim.6**: Hook execution engine (query + run + handle failures)

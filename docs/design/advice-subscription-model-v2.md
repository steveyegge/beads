# Advice Subscription Model v2

## Status: Design Revision

**Task:** hq--80lv.8 (reopened)
**Date:** 2026-01-31

## Overview

This revision proposes an **agent subscription model** where advice is a reusable
library and agents opt-in to relevant advice via subscriptions.

## Problem with Current Model

The current targeting model has limitations:

```bash
# Advice author must duplicate for each context
bd advice add "Always run tests" --rig beads
bd advice add "Always run tests" --rig gastown  # Duplicate!
bd advice add "Always run tests" --rig otherrig # More duplication!
```

- Advice authors must know all recipients upfront
- Same advice cannot be reused across contexts
- Agents cannot discover and opt-in to relevant advice
- No way for agents to exclude irrelevant advice they inherit

## Proposed Model: Label-Based Subscriptions

### Core Concept

1. **Advice is tagged with labels** (categories)
2. **Agents subscribe to labels** in their config
3. **Matching**: Agent receives advice where labels intersect

```
Advice Labels:     [testing, go, ci]
Agent Subscriptions: [testing, security]
                         ↓
Agent receives advice tagged "testing"
```

### Advice Creation (Updated)

```bash
# Create reusable advice with labels
bd advice add "Always run tests before pushing" \
  -l testing -l ci \
  -d "Run 'go test ./...' or 'npm test' before git push"

bd advice add "Check for secrets in commits" \
  -l security -l git \
  -d "Use 'git secrets --scan' before pushing"

bd advice add "Use gofmt for Go code" \
  -l go -l formatting \
  -d "Run 'gofmt -w .' before committing Go changes"
```

### Agent Subscription Config

**Option A: In rig config (rig.yaml)**

```yaml
# beads/rig.yaml
advice:
  subscriptions:
    # All agents in this rig subscribe to these
    - testing
    - go

  role_subscriptions:
    polecat:
      - ci
      - quick-tips
    crew:
      - architecture
      - design-patterns
```

**Option B: In agent bead**

```bash
# Agent bead has subscriptions field
bd update beads/crew/advice_architect \
  --advice-subscriptions "testing,security,architecture"
```

**Option C: Hybrid (recommended)**

- Rig config provides defaults
- Agent bead can add/override subscriptions
- More specific wins

```yaml
# Rig default
advice:
  subscriptions: [testing, go]

# Agent override (in agent bead)
advice_subscriptions: [testing, security, architecture]  # Replaces rig default
advice_subscriptions_add: [security]  # Adds to rig default
advice_subscriptions_exclude: [go]    # Removes from rig default
```

### Subscription Resolution

```
1. Start with rig-level subscriptions
2. Apply role-level subscriptions (if any)
3. Apply agent-level overrides:
   - Replace (if advice_subscriptions set)
   - Add (advice_subscriptions_add)
   - Exclude (advice_subscriptions_exclude)
4. Query advice matching final subscription set
```

### Backward Compatibility

Keep existing targeting for migration:

| Old (Targeting) | New (Subscriptions) |
|-----------------|---------------------|
| `--rig beads` | `-l rig:beads` (auto-subscribed) |
| `--role polecat` | `-l role:polecat` (auto-subscribed) |
| `--agent X` | `-l agent:X` (auto-subscribed) |
| (global) | `-l global` (everyone subscribes) |

**Auto-subscriptions**: Every agent automatically subscribes to:
- `global`
- `rig:{their-rig}`
- `role:{their-role}`
- `agent:{their-id}`

This preserves current behavior while enabling new subscription patterns.

### Query Changes

```bash
# Current: --for uses targeting fields
bd advice list --for beads/polecats/quartz

# New: --for resolves subscriptions then matches labels
# 1. Get agent's subscriptions: [global, rig:beads, role:polecat, testing, ci]
# 2. Find advice with any matching label
```

### Example Workflow

**1. Create reusable advice:**
```bash
bd advice add "Verify tests pass" -l testing -l ci
bd advice add "Check Go formatting" -l go -l formatting
bd advice add "Review security implications" -l security
```

**2. Configure rig subscriptions:**
```yaml
# beads/rig.yaml
advice:
  subscriptions: [testing, go]
  role_subscriptions:
    polecat: [ci]
```

**3. Agent sees relevant advice:**
```bash
gt prime  # beads/polecats/quartz
# Subscriptions resolved: [global, rig:beads, role:polecat, testing, go, ci]
# Shows:
#   - "Verify tests pass" (matches: testing, ci)
#   - "Check Go formatting" (matches: go)
```

**4. Different rig, same advice:**
```yaml
# gastown/rig.yaml
advice:
  subscriptions: [testing, security]  # No 'go' - it's a JS project
```

```bash
gt prime  # gastown/polecats/alpha
# Shows:
#   - "Verify tests pass" (matches: testing)
#   - "Review security implications" (matches: security)
# Does NOT show "Check Go formatting" (no 'go' subscription)
```

## Schema Changes

### Issues Table

```sql
-- No changes needed - advice already supports labels
-- Labels stored in labels table (issue_id, label)
```

### New: Agent Subscriptions

**Option A: In issues table (agent beads)**
```sql
ALTER TABLE issues ADD COLUMN advice_subscriptions TEXT;  -- JSON array
```

**Option B: Separate table**
```sql
CREATE TABLE advice_subscriptions (
  agent_id TEXT NOT NULL,      -- e.g., "beads/crew/wolf"
  label TEXT NOT NULL,
  source TEXT DEFAULT 'agent', -- 'rig', 'role', 'agent'
  PRIMARY KEY (agent_id, label)
);
```

**Option C: Config file only (no DB)**
- Subscriptions live in rig.yaml
- Resolved at runtime
- No migration needed

**Recommendation: Option C for MVP**, upgrade to B if needed.

## Implementation Plan

### Phase 1: Label-Based Matching (MVP)

1. Update `bd advice add` to encourage labels over targeting
2. Add `bd advice list --label X` filtering
3. Update `gt prime` to match by labels
4. Keep targeting as auto-labels for compatibility

### Phase 2: Rig Config Subscriptions

1. Add `advice.subscriptions` to rig.yaml schema
2. Update `gt prime` to read rig config
3. Resolve subscriptions before querying advice

### Phase 3: Agent-Level Overrides

1. Add subscription fields to agent beads
2. Implement override logic (add/exclude)
3. CLI for managing agent subscriptions

## Migration Path

1. **Existing advice continues to work** via auto-labels
2. **New advice uses labels** for reusability
3. **Rigs adopt subscriptions** gradually
4. **Deprecate targeting flags** (long-term)

## Trade-offs

| Aspect | Targeting (Current) | Subscriptions (Proposed) |
|--------|---------------------|--------------------------|
| Reusability | Poor | Good |
| Discoverability | None | Agents browse labels |
| Config complexity | None | Rig/agent config |
| Migration | N/A | Backward compatible |
| Author burden | Must know recipients | Just tag appropriately |
| Agent control | None | Full opt-in/out |

## Open Questions

1. **Standard labels**: Should we define a standard label taxonomy?
   - `testing`, `security`, `formatting`, `git`, `ci`, etc.

2. **Label discovery**: How do agents discover available labels?
   - `bd advice labels` command?

3. **Subscription UI**: How do agents manage subscriptions?
   - CLI commands? Edit rig.yaml? Both?

---

## Implementation Status

### Phase 1: Label-Based Matching (COMPLETED)

1. [x] Prototype label-based matching in `bd advice list`
   - Added `--label` flag for filtering by explicit labels
   - Added `--subscribe` flag for simulating agent subscriptions
   - Implemented `matchesAnyLabel()` for label filtering
   - Implemented `matchesSubscriptions()` with auto-label support
   - Unit tests added and passing

**Usage examples:**
```bash
# Filter by labels (matches advice with ANY of these labels)
bd advice list -l testing -l security

# Simulate what an agent with these subscriptions would see
bd advice list --subscribe testing --subscribe go --subscribe global

# Auto-labels work for backward compatibility
bd advice list --subscribe "rig:beads"  # Matches advice targeting beads rig
```

### Next Steps

2. [ ] Add rig.yaml subscription config parsing
3. [ ] Update `gt prime` advice delivery
4. [ ] Write tests for subscription resolution
5. [ ] Document standard label taxonomy

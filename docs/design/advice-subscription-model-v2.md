# Advice Subscription Model v2

## Status: IMPLEMENTED

**Task:** hq--80lv.8, gu-epc-advice_subscriptions_implement
**Date:** 2026-01-31 (design), 2026-02-01 (implemented)

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

### Backward Compatibility (CLI Flags Preserved)

The `--rig`, `--role`, `--agent` CLI flags remain for convenience but now add labels:

| CLI Flag | Label Added |
|----------|-------------|
| `--rig beads` | `rig:beads` |
| `--role polecat` | `role:polecat` |
| `--agent X` | `agent:X` |
| (no targeting) | `global` |

**Note:** The underlying `AdviceTargetRig`, `AdviceTargetRole`, `AdviceTargetAgent` fields
have been **removed** from the Issue struct. All targeting now uses labels.

**Auto-subscriptions**: When using `--for`, agents automatically subscribe to:
- `global`
- `rig:{their-rig}`
- `role:{their-role}` (both plural and singular forms)
- `agent:{their-id}`

This enables both explicit targeting via flags and flexible label-based subscriptions.

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
   - Added `--for` flag for auto-subscribing to agent context labels
   - Implemented `matchesAnyLabel()` for label filtering
   - Implemented `matchesSubscriptions()` - labels only (no targeting fields)
   - Implemented `buildAgentSubscriptions()` for auto-generating agent context labels
   - Unit tests added and passing

### Legacy Targeting Removal (COMPLETED - 2026-02-01)

1. [x] Removed `AdviceTargetRig`, `AdviceTargetRole`, `AdviceTargetAgent` fields from types.Issue
2. [x] Removed targeting fields from storage layer (Dolt INSERT/SELECT)
3. [x] Removed targeting fields from RPC protocol
4. [x] Converted `--rig`, `--role`, `--agent` CLI flags to add labels instead
5. [x] Added default `global` label when no targeting specified
6. [x] Updated all tests to use label-based approach
7. [x] Added comprehensive E2E integration tests

**Usage examples:**
```bash
# Create advice with labels
bd advice add "Always run tests" -l testing -l ci

# Create advice targeting a rig (adds rig:beads label)
bd advice add "Use go test" --rig beads

# Create advice targeting a role (adds role:polecat label)
bd advice add "Complete work before gt done" --role polecat

# Create advice for specific agent (adds agent:X label)
bd advice add "Focus on CLI" --agent beads/polecats/quartz

# List advice for an agent (auto-subscribes to context labels)
bd advice list --for beads/polecats/quartz

# Filter by explicit labels
bd advice list -l testing -l security
```

### Compound Label Groups (AND/OR Semantics) - IMPLEMENTED (2026-02-03)

Labels support compound targeting with AND and OR semantics:

#### AND Semantics (comma-separated)
Labels within the same `-l` flag form an AND group:
```bash
bd advice add 'X' -l 'role:polecat,rig:beads'
```
This advice only matches agents who are BOTH polecats AND in beads rig.

#### OR Semantics (multiple flags)
Separate `-l` flags form OR groups:
```bash
bd advice add 'X' -l 'role:polecat' -l 'role:crew'
```
This advice matches agents who are EITHER polecats OR crew.

#### Complex Combinations
You can combine AND and OR for sophisticated targeting:
```bash
# (polecat+beads) OR crew
bd advice add 'X' -l 'role:polecat,rig:beads' -l 'role:crew'
```
This matches agents who are (polecats in beads rig) OR (crew members).

#### Storage Format
Labels are stored with group prefixes: `g0:`, `g1:`, etc.
- `g0:role:polecat, g0:rig:beads` = AND (both must match)
- `g0:role:polecat, g1:role:crew` = OR (either matches)

#### Backward Compatibility
Existing labels without group prefixes are treated as separate groups (OR behavior),
maintaining full backward compatibility with previously created advice.

### Future Work (Optional)

- [ ] Add rig.yaml subscription config parsing for per-rig defaults
- [ ] Add agent-level subscription overrides (add/exclude)
- [ ] Document standard label taxonomy

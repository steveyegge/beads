# Advice Subscription Model Research

## Status: Research Complete

**Task:** hq--80lv.8
**Date:** 2026-01-31

## Executive Summary

After researching the current advice system, I recommend **maintaining the current implicit
matching model** rather than adding explicit subscriptions. The current system's simplicity
is a feature, not a limitation. This document explains why and addresses the research questions.

---

## Current System Overview

### How It Works

1. **Advice Storage**: Advice is stored as beads with `TypeAdvice` issue type and three
   targeting fields: `advice_target_rig`, `advice_target_role`, `advice_target_agent`

2. **Advice Delivery**: When `gt prime` runs, it:
   - Queries all open advice beads: `bd list -t advice --json`
   - Builds agent identity from role context (e.g., `beads/polecats/quartz`)
   - Filters applicable advice via `matchesAgentScope()`
   - Outputs matching advice in the prime context

3. **Matching Logic**: Hierarchical matching where an agent sees all applicable advice:
   - **Global**: Empty targeting fields → matches all agents
   - **Rig**: `advice_target_rig="beads"` → matches agents in beads rig
   - **Role**: `advice_target_role="polecat"` → matches polecats in that rig
   - **Agent**: `advice_target_agent="beads/polecats/quartz"` → matches only quartz

### Key Files

| Component | Location |
|-----------|----------|
| Types | `internal/types/types.go:140-143` |
| Matching | `cmd/bd/advice_list.go:195-235` |
| Delivery | `internal/cmd/prime_advice.go` (gastown) |
| Migration | `internal/storage/sqlite/migrations/049_advice_fields.go` |

---

## Research Questions Answered

### 1. Where should advice subscriptions live?

**Recommendation: Keep the current implicit model (no subscriptions)**

The current system uses targeting on the advice side rather than subscriptions on the agent side.
This is the right design for several reasons:

| Approach | Pros | Cons |
|----------|------|------|
| **Current: Targeting on advice** | Simple, no agent config needed, advice authors control scope | Less flexible per-agent filtering |
| Agent config YAML | Per-agent control | Config drift, complex sync |
| Per-agent PRIME.md | Agent self-documents | Manual maintenance burden |
| Labels on role definitions | Clean separation | Adds indirection layer |
| Dynamic via bd command | Flexible | State management complexity |

**Why implicit matching wins:**
- Advice authors know who needs to see their advice
- No agent config maintenance burden
- Works with ephemeral polecats (no state to configure)
- Simple mental model: "I create advice for polecats" vs "each polecat subscribes to tags"

### 2. How should ordering work?

**Recommendation: Priority field (current) + implicit hierarchy**

Current ordering:
1. `Priority` field (1-5, ASC) - explicit ordering within scope
2. `CreatedAt` (DESC) - newer advice first within same priority

The implicit hierarchy (global < rig < role < agent) doesn't filter - all levels are shown.
This is correct because:
- Agent-specific advice doesn't replace role advice, it supplements it
- Global advice ("always check git status") applies even when agent-specific advice exists

**No changes recommended.** The current priority-based ordering is sufficient.

### 3. What's the subscription syntax?

**Recommendation: None needed (current targeting is sufficient)**

If subscriptions were added (not recommended), the natural syntax would be:

```yaml
# Hypothetical - NOT RECOMMENDED
advice:
  subscribe:
    - label:security
    - label:testing
    - rig:beads
    - role:polecat
  exclude:
    - gt-advice-123  # Specific advice to ignore
```

**Why this isn't needed:**
- The current targeting model achieves the same filtering from the advice side
- Advice authors are better positioned to know who needs to see advice
- Adding subscriptions creates two places to reason about (advice targeting + agent subscription)

### 4. Inheritance model

**Current behavior (correct):**
- Role subscriptions DO inherit to all agents of that role
- Agents CANNOT override/exclude inherited advice
- All applicable advice is shown (no exclusion mechanism)

**Recommendation: Keep this behavior**

Agents seeing all applicable advice is safer than letting them exclude advice. If advice
is noise, the advice should be retargeted, not hidden per-agent.

If exclusion is needed in the future, it should be done via advice targeting (advice authors
exclude specific agents) not agent subscriptions (agents filtering out advice).

---

## Recommendations

### Keep Current System

The current implicit matching model is:
- **Simple**: No configuration needed
- **Safe**: Agents can't hide advice they should see
- **Maintainable**: Advice authors control scope
- **Ephemeral-friendly**: Works with polecats that have no persistent config

### Minor Improvements (Optional)

1. **Add `--exclude-agent` flag to `bd advice add`**
   Allow advice authors to exclude specific agents from otherwise-matching advice.
   ```bash
   bd advice add "Use go test" --rig beads --exclude-agent beads/crew/special
   ```

2. **Add advice categories/labels for grouping**
   Already supported via `--label`. Could add category-based filtering to `bd advice list`.

3. **Add advice expiration**
   `--expires` flag for temporary advice that auto-closes after a date.

### Explicitly NOT Recommended

- **Agent subscription config files**: Adds complexity without value
- **Per-agent override/exclude**: Dangerous - agents shouldn't hide advice
- **Complex priority/ordering rules**: Current priority field is sufficient
- **Label-based subscription**: Targeting achieves the same goal more simply

---

## Design Principle

**Advice flows from authors to agents, not vice versa.**

This is like a broadcast model vs subscription model:
- **Broadcast** (current): Authors say "this advice goes to polecats"
- **Subscription** (not recommended): Agents say "I want advice about security"

Broadcast is better for advice because:
1. Advice authors know what's important
2. Agents might not know what categories of advice exist
3. New advice automatically reaches all relevant agents
4. No "I didn't subscribe to that category" gaps

---

## Implementation Notes

If extending the system, maintain these invariants:

1. **All matching advice shown**: Don't filter to "most specific"
2. **Silent failure on delivery**: `gt prime` shouldn't fail if advice unavailable
3. **Advice is beads**: Keep using the issue system, not separate storage
4. **Targeting is immutable per-advice**: Don't allow advice scope to change
5. **Open advice only by default**: Closed advice is historical record

---

## Conclusion

The current advice system implements an effective implicit matching model. Adding explicit
subscriptions would increase complexity without meaningful benefit. The research questions
have clear answers: keep the current design, maintain the implicit hierarchy, and avoid
adding subscription configuration.

The only enhancements worth considering are on the advice creation side (exclusions,
expiration) rather than the agent subscription side.

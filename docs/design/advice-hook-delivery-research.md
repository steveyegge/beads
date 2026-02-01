# Hook-Based Advice Delivery Research

## Status: Research Complete (Recommendations Implemented)

**Task:** hq--80lv.9
**Date:** 2026-01-31

**Update (2026-02-01):** Advice now uses label-based subscriptions instead of targeting
fields. The delivery mechanism via `gt prime` remains unchanged.

## Executive Summary

This document analyzes hook-based advice delivery options. The **recommendation is to keep
advice delivery at session start (current `gt prime` approach)** and avoid injecting advice
at other hook points. Periodic advice delivery creates noise without value.

---

## Current Delivery Mechanism

Advice is delivered via `gt prime`:

```
gt prime
├── Session metadata
├── Role context (template)
├── Agent Advice          ← HERE
├── Handoff content
└── ...
```

**Format:**
```markdown
## 📝 Agent Advice

**[Polecat]** Check hook before checking mail
  The hook is authoritative. Always run 'gt hook' first on startup.

**[Global]** Always verify git status before pushing
  Run 'git status' to check for uncommitted changes before 'git push'
```

---

## Research Questions Answered

### 1. Which hook points make sense for advice injection?

| Hook Point | Use Case | Recommendation |
|------------|----------|----------------|
| **Session start** (gt prime) | Initial context | ✅ Current, works well |
| Pre-tool-use | Context reminder | ❌ Too noisy |
| Post-tool-use | Error recovery hints | ❌ Better served by docs |
| Periodic reminder | Memory refresh | ❌ Creates noise |
| Context compaction | Preserve critical info | 🔶 Maybe useful |

**Recommendation: Session start only**

Reasoning:
- **Pre-tool-use hooks** fire frequently (every tool call). Injecting advice here would
  flood the context with repeated information.
- **Post-tool-use hooks** are for error handling, not general advice. If a tool fails,
  the error message should be sufficient.
- **Periodic reminders** work against the LLM's attention mechanism. Important advice
  should be in the initial prompt where it gets proper attention, not repeated mid-session.

**The one exception: Context compaction**

When Claude Code compacts context (summarizes earlier turns), critical advice could be
lost. A hook to re-inject advice during compaction could be valuable:

```
# Hypothetical compaction hook
hooks:
  on_context_compact:
    - bd advice list --for $AGENT_ID --system-reminder
```

But this is a Claude Code feature request, not something we control.

### 2. How should `<system-reminder>` wrapping work?

**Current behavior:**
- Advice appears in `gt prime` output as markdown
- Claude Code shows this in the initial system prompt
- No explicit `<system-reminder>` tags used

**Recommendation: No change needed**

The `<system-reminder>` tag is useful when:
- Information needs to appear mid-conversation
- Content is injected by hooks (not initial prompts)

Since advice is delivered at session start via `gt prime`, standard markdown formatting
is appropriate. The LLM sees it in the initial context where it has full attention.

**If we did want system-reminder wrapping:**
```bash
# Hypothetical --system-reminder flag
bd advice list --for beads/polecats/quartz --system-reminder
```

Output:
```
<system-reminder>
**[Polecat]** Check hook before checking mail
  The hook is authoritative.
</system-reminder>
```

This is unnecessary for session-start delivery but could be useful for compaction hooks.

### 3. Configuration surface

**Recommendation: Minimal configuration**

The current system has no configuration - advice delivery is automatic via `gt prime`.
This is correct. Configuration creates:
- Maintenance burden (more files to keep in sync)
- Failure modes (misconfigured agents miss advice)
- Complexity without clear benefit

**If configuration were needed:**

```yaml
# rig.yaml - NOT RECOMMENDED
advice:
  delivery:
    session_start: true       # Default
    compaction_refresh: true  # Re-inject on compaction
    max_items: 10            # Limit advice shown
  format:
    system_reminder: false   # Use markdown, not <system-reminder>
```

But this adds complexity for marginal benefit. Keep it simple.

### 4. Performance considerations

**Current performance characteristics:**
- Advice query runs once per `gt prime` call
- Query: `bd list -t advice --json --limit 100`
- Filtering happens in memory (fast)
- No caching needed for session-start delivery

**If periodic delivery were added (not recommended):**

Caching would be necessary:
```go
type AdviceCache struct {
    advice    []*types.Issue
    timestamp time.Time
    ttl       time.Duration  // e.g., 5 minutes
}
```

Invalidation strategies:
1. **TTL-based**: Simple but may show stale advice
2. **Event-based**: Invalidate on `bd advice add/remove` events
3. **Hash-based**: Compare content hash to detect changes

**Recommendation: No caching needed**

Session-start delivery doesn't benefit from caching. Each session starts fresh with
current advice. The query is fast enough (< 100ms) that caching adds complexity without
benefit.

---

## Design Recommendations

### Keep Current Approach

The current `gt prime` delivery is optimal:
1. Advice appears in initial context (highest attention)
2. No repeated injections (avoids noise)
3. No configuration needed (simplicity)
4. No caching required (fresh every session)

### Don't Add Hook-Based Delivery

Mid-session advice injection would:
- Distract the LLM from current work
- Flood context with repeated information
- Create configuration complexity
- Require caching infrastructure

### Future Enhancement: Compaction Hook

If Claude Code adds compaction hooks, consider:
```bash
# Re-inject critical advice when context is compacted
hooks:
  on_context_compact:
    - gt advice-refresh  # Custom command to re-inject advice
```

This would preserve advice through context compaction without mid-session noise.

---

## Alternative Approaches Considered

### 1. Error-Triggered Advice

**Idea:** Show relevant advice when specific errors occur.
**Example:** "git push failed" → show "Always check git status" advice
**Verdict:** ❌ Complex pattern matching, better served by error docs

### 2. Tool-Specific Advice

**Idea:** Inject advice relevant to tools being used.
**Example:** Before bash commands, show "Check git status" advice
**Verdict:** ❌ Too noisy, advice is general guidance not tool-specific

### 3. Urgency-Based Periodic Refresh

**Idea:** High-priority advice gets periodic refresh, low-priority doesn't.
**Example:** P1 advice refreshes every 10 turns, P2+ only at session start
**Verdict:** ❌ Adds complexity, priority already affects display order

---

## Conclusion

**Hook-based advice delivery beyond session start is not recommended.**

The current `gt prime` approach is optimal:
- Advice appears at session start where it gets proper attention
- No mid-session distractions or repeated injections
- Simple implementation with no configuration or caching needed

The only future enhancement to consider is compaction hook support, which would require
Claude Code changes and is outside our control.

---

## Implementation Notes

### Current code path (updated for label-based subscriptions)

```
gt prime
  → outputAdviceContext(ctx RoleInfo)
    → exec bd advice list --for $AGENT_ID --json
    → buildAgentSubscriptions(agentID) generates: [global, rig:X, role:Y, agent:Z]
    → matchesSubscriptions() filters by label intersection
    → output markdown section
```

**Note:** The `--for` flag auto-subscribes to the agent's context labels (global, rig, role,
agent). This replaced the old `matchesAgentScope()` targeting field approach.

### Hypothetical compaction hook (future, Claude Code dependent)

```yaml
# .claude/hooks.yaml
on_context_compact:
  - command: gt advice-refresh
    env:
      AGENT_ID: $BD_ACTOR
```

Would require:
1. Claude Code to support compaction hooks
2. New `gt advice-refresh` command
3. System-reminder wrapping for mid-conversation injection

Not actionable now - depends on Claude Code feature requests.

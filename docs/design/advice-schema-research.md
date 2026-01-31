# Advice as First-Class Beads Type Research

## Status: Research Complete

**Task:** hq--80lv.10
**Date:** 2026-01-31

## Executive Summary

After analyzing the current advice implementation and potential extensions, the **recommendation
is to keep advice as an issue subtype** (current approach) rather than creating a separate
table or collection. The current schema is sufficient with minor enhancements.

---

## Current State

### Storage

Advice is stored in the `issues` table with:
- `issue_type = 'advice'`
- `advice_target_rig` (TEXT)
- `advice_target_role` (TEXT)
- `advice_target_agent` (TEXT)

### Schema (types.go)

```go
type Issue struct {
    // Core fields
    ID          string
    Title       string
    Description string
    Status      Status
    Priority    int
    IssueType   IssueType  // "advice"

    // Advice targeting
    AdviceTargetRig   string
    AdviceTargetRole  string
    AdviceTargetAgent string

    // Standard issue fields
    Labels      []string
    CreatedAt   time.Time
    // ...
}
```

### Query Pattern

```go
filter := types.IssueFilter{
    IssueType: &types.TypeAdvice,
    Status:    &types.StatusOpen,
}
advice, _ := store.SearchIssues(ctx, "", filter)
```

---

## Research Questions Answered

### 1. Should advice have its own table/collection?

**Recommendation: No - keep as issue subtype**

| Approach | Pros | Cons |
|----------|------|------|
| **Current: Issues table** | Reuses infrastructure, labels work, events tracked | Less type safety |
| Separate table | Type safety, custom indexes | Migration complexity, duplicate infra |
| Proto-defined collection | Schema validation | Major refactor, Dolt complexity |

**Why keep in issues table:**
1. **Infrastructure reuse**: Labels, events, comments, dependencies all work
2. **Migration cost**: Separate table requires new CRUD, RPC endpoints, CLI commands
3. **Query patterns**: Already efficient with indexed advice targeting fields
4. **Simplicity**: One concept (beads) vs two (beads + advice)

### 2. What additional fields are needed?

**Current fields are sufficient.** Optional enhancements:

| Field | Purpose | Recommendation |
|-------|---------|----------------|
| `expires_at` | Auto-close after date | 🔶 Nice to have |
| `category` | Grouping (security, testing) | ❌ Use labels instead |
| `delivery_hook` | When to show | ❌ Session-start only |
| `wrapper_style` | system-reminder vs markdown | ❌ Always markdown |
| `conditions` | Show if X | ❌ Too complex |
| `supersedes` | Replaces advice Y | ❌ Use close + new |

**Recommended enhancement:**

```sql
ALTER TABLE issues ADD COLUMN advice_expires_at TIMESTAMP
```

This allows temporary advice (e.g., "deploy moratorium until Jan 15").

### 3. Relationship modeling

**Current model is sufficient:**

| Relationship | Current Support | Enhancement Needed? |
|--------------|-----------------|---------------------|
| Advice → Agent | Implicit (targeting) | No |
| Agent → Advice | Query-based | No |
| Advice → Advice | Close + create new | No |
| Advice → Issue | `discovered_from` dep | Already works |

**Why no explicit subscriptions:**
- Per research in hq--80lv.8, subscriptions add complexity without value
- Targeting on advice side is the right model

**Advice chaining (supersedes):**
Rather than explicit `supersedes` field:
1. Close old advice with reason "Superseded by gt-advice-xyz"
2. Create new advice
3. Use `discovered_from` dependency if needed

### 4. Query patterns

**Already supported:**

```bash
# Get all advice for agent X
bd advice list --for beads/polecats/quartz

# Get advice by label
bd list -t advice -l security

# Get all open advice
bd advice list
```

**Not needed:**
- "Get all agents subscribed to advice Y" - no subscriptions exist
- Complex category queries - labels serve this purpose

---

## Schema Proposal

### Minimal Enhancement (Recommended)

Add optional expiration:

```sql
-- Migration 050_advice_expiration.sql
ALTER TABLE issues ADD COLUMN advice_expires_at TIMESTAMP;

-- Auto-close expired advice (via daemon job)
UPDATE issues
SET status = 'closed', close_reason = 'Expired'
WHERE issue_type = 'advice'
  AND advice_expires_at IS NOT NULL
  AND advice_expires_at < CURRENT_TIMESTAMP
  AND status = 'open';
```

CLI support:
```bash
bd advice add "Deploy freeze" --expires 2026-02-01 -d "No deploys during audit"
```

### No Change Needed

The following are explicitly **not recommended**:

1. **Separate advice table**: Duplicates infrastructure, migration burden
2. **Subscription relationships**: Targeting is sufficient (see hq--80lv.8)
3. **Complex conditions**: "Show if X" logic belongs in advice text, not schema
4. **Hook configuration**: Session-start delivery is correct (see hq--80lv.9)
5. **Categories**: Labels already provide this functionality

---

## Migration Plan

### Phase 1: Expiration (Optional)

1. Add `advice_expires_at` column (nullable)
2. Add daemon job to close expired advice
3. Add `--expires` flag to `bd advice add`
4. Update `bd advice list` to show expiration

### Phase 2: None

No further schema changes recommended. The current model is appropriate.

---

## Type Safety Consideration

If stronger type safety is desired, use the existing `IssueType` validation:

```go
func (issue *Issue) Validate() error {
    if issue.IssueType == TypeAdvice {
        // Advice-specific validation
        if issue.AdviceTargetRole != "" && issue.AdviceTargetRig == "" {
            return errors.New("role requires rig")
        }
        if issue.AdviceTargetAgent != "" &&
           (issue.AdviceTargetRig != "" || issue.AdviceTargetRole != "") {
            return errors.New("agent targeting cannot combine with rig/role")
        }
    }
    return nil
}
```

This already exists in the CLI validation. Adding to storage layer is optional.

---

## Relationship to Other Research

| Research | Finding | Impact |
|----------|---------|--------|
| hq--80lv.8 (Subscriptions) | Keep implicit matching | No subscription tables |
| hq--80lv.9 (Hooks) | Session-start only | No delivery config fields |
| hq--80lv.10 (This doc) | Keep as issue subtype | No new tables |

All three research tasks converge on: **keep it simple**.

---

## Conclusion

Advice should remain an issue subtype (`issue_type = 'advice'`). The current schema is
sufficient with one optional enhancement (expiration dates).

Creating a separate advice table or collection would:
- Duplicate existing infrastructure (CRUD, RPC, CLI)
- Require complex migration
- Add concepts without proportional value

The beads philosophy is "everything is an issue." Advice fits this model well.

---

## Implementation Checklist (if proceeding with expiration)

- [ ] Add `advice_expires_at` column (migration 050)
- [ ] Add `--expires` flag to `bd advice add`
- [ ] Add expiration display to `bd advice list`
- [ ] Add daemon job to close expired advice
- [ ] Add tests for expiration behavior
- [ ] Update documentation

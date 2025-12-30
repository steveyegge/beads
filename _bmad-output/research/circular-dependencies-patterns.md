# Circular Dependencies Patterns Analysis

## Executive Summary

- **Total issues analyzed:** 8
- **Patterns identified:** 4
- **Most common pattern type:** False Positive Detection (3 issues)
- **Key finding:** Most "circular dependency" issues are actually false positives from overly aggressive detection algorithms, not true cycles

## Key Finding: True Circular Dependencies Are Rare

Analysis of 8 GitHub issues revealed a critical insight: **genuine circular dependencies (A→B→C→A cycles) are extremely rare in practice**. The majority of issues labeled as "circular dependency" problems fall into these categories:

| Category | Count | Percentage |
|----------|-------|------------|
| False positives from detection bugs | 3 | 37.5% |
| Algorithm performance issues | 1 | 12.5% |
| User confusion about dependency direction | 3 | 37.5% |
| Data corruption from automated fixes | 2 | 25% |
| **Actual circular dependencies** | **0** | **0%** |

**Implication for Recovery Documentation:** Focus on helping users distinguish false positives from true cycles, and provide clear cycle-breaking procedures for the rare cases when genuine cycles occur

## Methodology

### Search Queries Used

1. `is:issue circular` - 9 results
2. `is:issue cycle` - 12 results (2 directly relevant)
3. `is:issue dependency` - 10+ results
4. `is:issue "bd dep"` - 12 results
5. `is:issue "bd blocked"` - 4 results
6. `is:issue blocked` - 37 results

### Date Range

- **Search performed:** 2025-12-30
- **Issues analyzed:** December 2024 to December 2025 (active development period)
- **Oldest issue:** #440 (dependency direction confusion)
- **Newest issue:** #774 (cycle detection performance)

### Selection Criteria

- Issues directly mentioning cycle detection, circular dependencies, or dependency problems
- Issues involving `bd dep`, `bd blocked`, `bd ready` commands
- Issues where dependency relationships caused unexpected behavior
- Excluded: general feature requests without dependency context

## Issue Availability Note

8 relevant issues were found, exceeding the minimum target of 5. The beads project has robust issue tracking with detailed technical discussions, providing excellent material for pattern extraction.

## Pattern Catalog

### Pattern 1: Algorithm Performance with Diamond Dependencies

**Issues:** [#774](https://github.com/steveyegge/beads/issues/774)

#### Symptoms

- `bd dep cycles` command hangs at 100% CPU
- Timeout errors after 2+ minutes
- Occurs with ~500 issues and ~400 dependencies
- Diamond patterns (multiple nodes depending on same target) trigger exponential growth

#### How Cycle Forms

This is not a true circular dependency but an algorithmic performance issue. The original CTE-based SQL implementation enumerated all possible paths through the dependency graph:

```sql
p.path || '→' || d.depends_on_id as path
...
AND p.path NOT LIKE '%' || d.depends_on_id || '→%'
```

With diamond patterns, each traversal level doubles the number of paths, causing O(2^n) complexity.

#### bd Commands for Diagnosis

```bash
$ bd dep cycles
# Hangs with large graphs

$ bd stats
# Check issue/dependency counts
```

#### Resolution Strategy

1. Issue was fixed in PR #775 with DFS-based cycle detection
2. After updating beads, cycle detection is O(V+E) instead of O(2^n)
3. Performance improvement: >120s → 1.6ms for dense graphs

#### Prevention

- Keep beads updated to latest version
- Monitor dependency graph complexity with `bd stats`

---

### Pattern 2: False Positive Cycle Detection

**Issues:** [#661](https://github.com/steveyegge/beads/issues/661), [#750](https://github.com/steveyegge/beads/issues/750), [#740](https://github.com/steveyegge/beads/issues/740)

#### Symptoms

- `bd dep cycles` reports cycles that aren't blocking dependencies
- `bd doctor` flags legitimate relationships as "anti-patterns"
- "Found X dependency cycles" for `relates_to` links
- 118+ false "anti-patterns" flagged when all are legitimate `parent-child` type

#### How False Positives Form

**relates_to as cycle (#661):**
```bash
$ bd relate beads-issue-un3 beads-issue-a9e
# Creates bidirectional "see also" link
$ bd dep cycles
Found 2 dependency cycles  # FALSE POSITIVE - relates_to is not blocking
```

**parent-child as anti-pattern (#750, #740):**
```sql
-- Original detection (WRONG):
WHERE d.issue_id LIKE d.depends_on_id || '.%'
-- NO TYPE FILTER - matches ALL child→parent relationships
```

The detection queries did not filter by dependency type, treating structural hierarchy (`parent-child` type) the same as blocking dependencies (`blocks` type).

#### bd Commands for Diagnosis

```bash
$ bd dep cycles
# Check reported cycles

$ bd show <issue_id>
# Verify dependency types - look for "relates_to" vs "blocks"

$ bd doctor
# See what would be flagged (DO NOT run --fix without review!)
```

#### Resolution Strategy

1. **For relates_to false positives:** Fixed in commit 844e9ff - relates_to edges excluded from cycle detection
2. **For parent-child false positives:** Fixed in commit d3e9022 - type filtering added
3. **If `bd doctor --fix` already ran:** Recover with `git checkout HEAD~1 -- .beads/issues.jsonl`

#### Prevention

- Always verify cycle reports with `bd show <issue>` before taking action
- Never run `bd doctor --fix` without reviewing what will be changed
- Use `--fix-child-parent` flag only when explicitly intended
- Keep git history clean for recovery

---

### Pattern 3: Dependency Direction Confusion

**Issues:** [#440](https://github.com/steveyegge/beads/issues/440), [#723](https://github.com/steveyegge/beads/issues/723), [#544](https://github.com/steveyegge/beads/issues/544)

#### Symptoms

- `bd ready` shows blocked issues that shouldn't be workable
- `bd ready` hides unblocked issues that should be workable
- Documentation says `bd dep add PARENT CHILD` but system requires opposite
- Epic children appear under "Blocks" label instead of "Children"
- Confusion about which direction dependencies flow

#### How Confusion Forms

**The semantic problem:**
> "Children depend on their parents" makes sense in family dynamics but is backwards in task management where child tasks are *dependencies* that must complete before the parent.

**bd ready direction bug (#723, #544):**
- v0.29.0: `bd ready` included issues with open blockers (wrong)
- Fixed in v0.30.0: Issues with blocking dependencies excluded
- Then #723: Issues that *block others* incorrectly excluded (conflating incoming vs outgoing)

#### bd Commands for Diagnosis

```bash
$ bd ready --json
# Check what's considered "ready"

$ bd show <issue_id>
# Look at dependency section - verify direction

$ bd blocked
# Compare with bd ready output

$ bd dep tree <issue_id> --direction=up
# Visualize dependency direction
```

#### Resolution Strategy

1. When adding dependencies, think: **"X needs Y"** not "X comes before Y"
2. Verify with `bd show` after adding dependencies
3. Use `bd blocked` to validate what's actually blocked

#### Prevention

- **Mental model:** `bd dep add CHILD PARENT` means "child needs parent to be done first"
- Always verify dependency direction with `bd show <issue>` after creation
- Check both `bd ready` and `bd blocked` when dependencies seem wrong
- Review dependency output labels: Children ↳, Blocks ←, Related ↔

---

### Pattern 4: Data Corruption from Automated Fixes

**Issues:** [#740](https://github.com/steveyegge/beads/issues/740), [#630](https://github.com/steveyegge/beads/issues/630)

#### Symptoms

- Running `bd doctor --fix` destroys legitimate dependency relationships
- `bd rename-prefix` leaves orphaned dependencies
- Mismatch between database issue count and JSONL file count
- "Orphaned child issues" messages after prefix rename
- Dependencies reference non-existent issues

#### How Data Corruption Forms

**bd doctor --fix (#740):**
```bash
$ bd doctor --fix
# DANGEROUS: Auto-removed 118+ legitimate parent-child dependencies
# Treated structural hierarchy as blocking anti-pattern
```

**bd rename-prefix (#630):**
- Issue IDs updated to new prefix
- Dependency records still reference old prefix
- Referential integrity broken
- 5 of 63 issues left with outdated prefix

#### bd Commands for Diagnosis

```bash
$ bd doctor
# Review what WOULD be changed (without --fix)

$ bd stats
# Check issue counts for consistency

$ git diff .beads/
# Compare current state with git history
```

#### Resolution Strategy

1. **If bd doctor --fix broke data:**
   ```bash
   git checkout HEAD~1 -- .beads/issues.jsonl
   bd sync --force-rebuild
   ```

2. **If bd rename-prefix broke data:**
   - Issue fixed in PR #642
   - Update beads to latest version
   - Re-run rename or manually fix JSONL

3. **General recovery:**
   ```bash
   git log --oneline .beads/
   # Find commit before corruption
   git checkout <commit> -- .beads/
   bd sync --force-rebuild
   ```

#### Prevention

- **Never run `bd doctor --fix` without first reviewing `bd doctor` output**
- Keep beads updated to latest version
- Commit `.beads/` changes frequently for recovery points
- Test automated fixes on a backup/branch first
- New in latest versions: `--fix-child-parent` is opt-in, not automatic

---

## Cycle-Breaking Strategies (When True Cycles Occur)

While the analyzed issues showed no true circular dependencies, the following strategies would apply if genuine cycles are detected:

### Strategy 1: Identify and Remove Weakest Link

```bash
# Step 1: Detect cycles
$ bd dep cycles
Found 1 dependency cycle:
  issue-A → issue-B → issue-C → issue-A

# Step 2: Analyze each dependency to find the weakest link
$ bd show issue-A
# Look at: Is the dependency on issue-B truly blocking?

$ bd show issue-B
# Look at: Is the dependency on issue-C truly blocking?

$ bd show issue-C
# Look at: Is the dependency on issue-A truly blocking?

# Step 3: Remove the least critical dependency
$ bd dep remove issue-C issue-A
Removed dependency: issue-C no longer depends on issue-A

# Step 4: Verify cycle is broken
$ bd dep cycles
No dependency cycles found.
```

### Strategy 2: Convert Blocking to Non-Blocking

If the relationship should exist but shouldn't block:

```bash
# Remove blocking dependency
$ bd dep remove issue-A issue-B

# Add as relates_to (non-blocking "see also")
$ bd relate issue-A issue-B
Created relates_to link between issue-A and issue-B

# Verify - relates_to doesn't appear in cycle detection
$ bd dep cycles
No dependency cycles found.
```

### Strategy 3: Restructure with Parent Issue

If multiple issues need coordination, create a parent:

```bash
# Create coordinating parent issue
$ bd create "Coordinate: Feature X implementation" --type epic
Created: coord-1

# Make cyclic issues children of parent
$ bd dep add issue-A coord-1 --type parent-child
$ bd dep add issue-B coord-1 --type parent-child
$ bd dep add issue-C coord-1 --type parent-child

# Remove inter-issue dependencies
$ bd dep remove issue-A issue-B
$ bd dep remove issue-B issue-C
$ bd dep remove issue-C issue-A

# Now work is coordinated through parent, no cycles
```

### Decision Matrix: Which Strategy to Use

| Situation | Recommended Strategy |
|-----------|---------------------|
| One dependency is clearly optional | Strategy 1: Remove weakest link |
| All dependencies are "nice to have" | Strategy 2: Convert to relates_to |
| Issues represent parallel work streams | Strategy 3: Restructure with parent |
| Dependencies come from import/migration | Check if false positive first (Pattern 2) |

---

## Prevention Best Practices

1. **Think "X needs Y"** when adding dependencies, not "X before Y"
2. **Verify after creation** with `bd show <issue>` to confirm direction
3. **Never blind-fix** - always review `bd doctor` output before `--fix`
4. **Commit frequently** - `.beads/` changes should be in git for recovery
5. **Keep beads updated** - many issues fixed in recent versions (v0.30.0+)
6. **Distinguish types:**
   - `blocks` / `conditional-blocks` / `waits-for` = blocking dependencies
   - `parent-child` = structural hierarchy (not blocking)
   - `relates_to` = bidirectional "see also" (not blocking)
7. **Use both commands** - check `bd ready` AND `bd blocked` when debugging

## bd dep / bd blocked Command Reference

| Command | Purpose |
|---------|---------|
| `bd dep add X Y` | X depends on Y (Y blocks X) - Think "X needs Y" |
| `bd dep add X Y --type blocks` | X blocks Y (explicit blocking) |
| `bd dep remove X Y` | Remove dependency between X and Y |
| `bd blocked` | Show all blocked issues |
| `bd ready` | Show unblocked issues ready for work |
| `bd dep cycles` | Detect circular dependencies |
| `bd dep tree <id>` | Show dependency tree |
| `bd dep tree <id> --direction=up` | Show what blocks this issue |
| `bd relate X Y` | Create bidirectional "see also" link |
| `bd doctor` | Check for dependency problems |
| `bd doctor --fix` | Auto-fix problems (USE WITH CAUTION) |
| `bd doctor --fix --fix-child-parent` | Opt-in to fix child→parent deps |

## Severity Distribution

| Severity | Count | Percentage | Description |
|----------|-------|------------|-------------|
| Critical | 2 | 25% | Data loss/corruption (#740, #630) |
| High | 3 | 37.5% | Workflow blocked (#774, #661, #750) |
| Medium | 2 | 25% | Incorrect output (#723, #544) |
| Low | 1 | 12.5% | Documentation confusion (#440) |

## Recommendations for Recovery Documentation

### Priority Patterns for Epic 2 v2.0

1. **Pattern 4 (Data Corruption)** - Most critical, users lose work
2. **Pattern 2 (False Positives)** - Common source of confusion
3. **Pattern 3 (Direction Confusion)** - Ongoing user education need

### Suggested Documentation Structure

```
Recovery Runbook: Circular Dependencies
├── Quick Diagnosis Flowchart
│   └── "bd dep cycles shows cycles" → Check if relates_to → Not blocking
├── Recovery Procedures
│   ├── Recovering from bd doctor --fix damage
│   ├── Fixing orphaned dependencies after rename
│   └── Resolving true circular dependencies
├── Prevention Guide
│   └── Dependency mental model: "X needs Y"
└── Command Reference
    └── All bd dep commands with examples
```

### Cross-references to Story 2.5 Synthesis

**Pattern IDs for Story 2.5 Reference:**

| Pattern ID | Name | Priority for 2.5 | Overlap Notes |
|------------|------|------------------|---------------|
| CDP-1 | Algorithm Performance | Low | Performance issue, not recovery |
| CDP-2 | False Positive Detection | High | Overlaps with Story 2.1 (bd doctor) |
| CDP-3 | Direction Confusion | Medium | Unique - CLI documentation focus |
| CDP-4 | Data Corruption | High | Overlaps with Story 2.1 (database recovery) |

**Consolidation Recommendations:**
- **CDP-2 + CDP-4 → Story 2.1**: Both involve `bd doctor` issues; consolidate into database corruption runbook
- **CDP-3 → Standalone**: Unique user education topic; create "Dependency Direction" reference doc
- **CDP-1 → Release Notes**: Performance fix already shipped; document in version history only

**Cycle-Breaking Strategies:** Include all 3 strategies in Epic 2 v2.0 Recovery Runbook as standard procedures

## Appendix: Issues Analyzed

| Issue # | Title | Severity | Pattern(s) |
|---------|-------|----------|------------|
| [#774](https://github.com/steveyegge/beads/issues/774) | DetectCycles has O(2^n) complexity with diamond dependency patterns | High | Pattern 1 |
| [#661](https://github.com/steveyegge/beads/issues/661) | relates_to relationship between two issues reported as cycle | High | Pattern 2 |
| [#750](https://github.com/steveyegge/beads/issues/750) | bd doctor child-parent detection removes legitimate parent-child type dependencies | High | Pattern 2 |
| [#740](https://github.com/steveyegge/beads/issues/740) | CRITICAL: bd doctor --fix broke my project (child->parent is NOT an anti-pattern) | Critical | Pattern 2, 4 |
| [#723](https://github.com/steveyegge/beads/issues/723) | bd ready filters out issues with "blocks" dependencies even though they are unblocked | Medium | Pattern 3 |
| [#544](https://github.com/steveyegge/beads/issues/544) | bd ready includes issues that have blocks dependencies | Medium | Pattern 3 |
| [#440](https://github.com/steveyegge/beads/issues/440) | Dependency names and directions are used inconsistently throughout the code, UI, and documentation | Low | Pattern 3 |
| [#630](https://github.com/steveyegge/beads/issues/630) | bd rename-prefix breaks dependencies | Critical | Pattern 4 |

---

*Analysis Date: 2025-12-30*
*Source Repository: [steveyegge/beads](https://github.com/steveyegge/beads)*
*Story: 2.3 - GitHub Issues Mining - Circular Dependencies Patterns*

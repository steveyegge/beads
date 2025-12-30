# Recovery Framework Design - Epic 2 v2.0

## Executive Summary

This document synthesizes analysis from Stories 2.1-2.4 (54 GitHub issues, 23 source patterns → 21 consolidated unique patterns) to design the recovery documentation framework for Epic 2 v2.0. Key findings inform both content structure and documentation priorities.

> **Pattern Consolidation Note:** Source documents identified 23 patterns total (7+6+4+6). After cross-pattern analysis, 2 patterns were merged into existing categories due to significant overlap: Merge P1 (Worktree Path) consolidated into B1, and Sync P3 (Data Loss) consolidated into A5/C3. This yields 21 unique patterns in the inventory.

### Key Findings

| Finding | Impact on Documentation |
|---------|------------------------|
| **Zero true circular dependencies** | No dedicated "Circular Deps" runbook needed |
| **65% of issues are High/Critical severity** | Recovery runbooks are highest priority |
| **Data loss patterns share root causes** | Consolidate into unified sync/merge runbook |
| **`bd doctor --fix` is dangerous** | Prominent warnings required |
| **Worktree issues are most frequent (31%)** | Needs comprehensive troubleshooting |
| **JSONL is recovery source of truth** | Emphasize JSONL backup/recovery |

### Research Documents Synthesized

| Document | Issues | Patterns | Primary Focus |
|----------|--------|----------|---------------|
| [database-corruption-patterns.md](./database-corruption-patterns.md) | 14 | 7 | Database state, migrations, data integrity |
| [merge-conflicts-patterns.md](./merge-conflicts-patterns.md) | 16 | 6 | Concurrent access, multi-agent, worktrees |
| [circular-dependencies-patterns.md](./circular-dependencies-patterns.md) | 8 | 4 | Dependency detection, false positives |
| [sync-failures-patterns.md](./sync-failures-patterns.md) | 16 | 6 | Daemon sync, worktree management |
| **Total** | **54** | **23** | |

---

## Cross-Pattern Insights

### Pattern Overlaps

Six significant overlaps were identified where patterns from different stories share root causes:

| Overlap ID | Patterns | Shared Root Cause | Shared Solution |
|------------|----------|-------------------|-----------------|
| **OV-1** | DB P5 + Sync P3 | Multi-machine sync logic | `bd sync --import-only`, merge logic |
| **OV-2** | DB P4 + Sync P2 | Daemon mode inconsistencies | `bd daemons killall`, `--no-daemon` |
| **OV-3** | Merge P3 + DB P2 | Parallel migration race | `BEGIN EXCLUSIVE`, serialize commands |
| **OV-4** | Merge P4 + Sync P3 | Fresh clone overwrite | Bootstrap from sync branch first |
| **OV-5** | Circ P2/P4 + DB P1 | `bd doctor --fix` damage | Git recovery: `git checkout HEAD~1` |
| **OV-6** | Merge P1 + Sync P1 | Worktree path resolution | `git worktree prune`, updated beads |

### Common Themes Across All Stories

1. **JSONL is Truth**: Database can be rebuilt from JSONL; JSONL recovery is critical path
2. **Daemon Complexity**: Many issues stem from daemon vs direct mode inconsistencies
3. **Multi-Clone Fragility**: Multi-machine/multi-agent workflows are most vulnerable
4. **Version Sensitivity**: Many issues fixed in v0.30.0 - v0.40.0 range
5. **`bd doctor` Risk**: Automated fixes can cause more damage than original issue

---

## Severity Matrix

### Aggregated Distribution (54 Issues)

| Severity | Count | Percentage | Description |
|----------|-------|------------|-------------|
| **Critical** | 10 | 19% | Data loss, unrecoverable without backup |
| **High** | 25 | 46% | Workflow blocked, requires manual intervention |
| **Medium** | 15 | 28% | Inconvenient, workaround available |
| **Low** | 4 | 7% | Minor impact, cosmetic |

### By Story

| Story | Critical | High | Medium | Low | Total |
|-------|----------|------|--------|-----|-------|
| 2.1 Database | 3 (21%) | 6 (43%) | 4 (29%) | 1 (7%) | 14 |
| 2.2 Merge | 2 (12%) | 8 (50%) | 4 (25%) | 2 (13%) | 16 |
| 2.3 Circular | 2 (25%) | 3 (37.5%) | 2 (25%) | 1 (12.5%) | 8 |
| 2.4 Sync | 3 (19%) | 8 (50%) | 5 (31%) | 0 | 16 |

### Critical Path Issues (Document First)

**Tier 1 - Document Immediately (Data Loss Risk):**
1. A5/C3 Multi-Machine Sync Data Loss (Issues #464, #746)
2. A3 Tombstone Corruption (Issue #552)
3. A1 Validation Catch-22 (Issue #806)
4. D4 `bd doctor --fix` damage (Issues #740, #630)

**Tier 2 - Document Early (Workflow Blocking):**
5. B1 Worktree Path Resolution (Issues #785, #639, #609)
6. B2 Daemon Race Conditions (Issues #694, #695)
7. C2 Migration Race (Issues #720, #607)

---

## Solution Effectiveness Matrix

### Universal Recovery Commands

These commands resolve 70%+ of reported issues:

```bash
# Primary Recovery Sequence
bd daemons killall           # Stop daemons (prevents race conditions)
git worktree prune           # Clean orphaned worktrees
rm .beads/beads.db*          # Remove potentially corrupted database
bd sync --import-only        # Rebuild from JSONL source of truth
```

### Command → Pattern Mapping

| Command | Patterns Resolved | Risk | Prerequisites |
|---------|-------------------|------|---------------|
| `bd sync --import-only` | A1, A5, C3 | Low | JSONL intact |
| `bd sync --force-rebuild` | A2, A7, B5, C3 | Medium | Backup recommended |
| `bd daemons killall` | A4, B2, B4 | Low | None |
| `rm .beads/beads.db*` | A1, A2, B5 | Medium | JSONL intact |
| `git worktree prune` | B1 | Low | None |
| `git checkout HEAD~1 -- .beads/` | A3, D4 | Medium | Clean git state |
| `bd migrate --update-repo-id` | A5 | Low | Run once |
| `bd --no-daemon <cmd>` | A4, B2 | None | None |
| `bd doctor` | All (diagnostic) | None | None |
| `bd export -o <file>` | B5, C4 | Low | Database accessible |
| `bd import -i <file>` | A5, C3 | Low | Valid JSONL file |
| `bd config set <key> <val>` | B3, B4 | Low | None |

### Command Prerequisites & Warnings

| Command | Warning |
|---------|---------|
| `bd doctor --fix` | **DANGER:** Can destroy legitimate data. Always review `bd doctor` output first |
| `rm .beads/beads.db*` | Database state lost, requires JSONL to be intact |
| `git checkout HEAD~1 -- .beads/` | Uncommitted changes to `.beads/` lost |
| `bd sync --force-rebuild` | May trigger data loss if JSONL is corrupted |

---

## Recovery Runbook Template

Per Architecture.md Recovery Section Format (lines 277-299):

```markdown
## Recovery: [Problem Name]

**Pattern ID:** [A1-xxx]
**Severity:** [Critical/High/Medium/Low]
**Version Fixed:** [v0.xx.0 or "Open"]

### Symptoms
- [Observable symptom 1]
- [Observable symptom 2]
- Error: `[exact error message]`

### Quick Diagnosis
```bash
$ [diagnostic command]
# Expected output: [explanation]
```

### Solution

:::danger Before You Begin
[Any critical warnings or prerequisites]
:::

1. **[Step description]**
   ```bash
   $ [command]
   ```
   Expected: [what you should see]

2. **[Verification step]**
   ```bash
   $ [verification command]
   ```
   Success: [expected success output]

### Prevention
- [How to avoid in future]
- [Configuration recommendation]

### Related
- [#123](https://github.com/steveyegge/beads/issues/123) - Brief description
- See also: [Related Pattern ID]
```

### Admonition Types

Per Architecture.md:
- `:::tip` - Helpful suggestions
- `:::note` - Additional context
- `:::warning` - Potential issues
- `:::danger` - Critical warnings (data loss risk)
- `:::info` - Background information

---

## Prevention Checklist Template

Use this template for each recovery pattern category:

```markdown
## Prevention: [Category Name]

### Before Starting Work
- [ ] Run `bd daemons status` to check daemon health
- [ ] Run `bd doctor` to validate database state
- [ ] Commit all `.beads/` changes to git
- [ ] Note current issue count: `bd stats`

### During Multi-Clone/Multi-Agent Work
- [ ] Push changes before switching machines
- [ ] Run `bd sync` before creating new issues
- [ ] Avoid parallel `bd` commands
- [ ] Use `--no-daemon` in containers/CI

### Before Dangerous Operations
- [ ] Backup `.beads/` directory
- [ ] Never run `bd doctor --fix` without reviewing output first
- [ ] Test on branch before applying to main
- [ ] Document recovery point: `git log -1 --oneline`

### Regular Maintenance
- [ ] Update beads to latest version monthly
- [ ] Run `git worktree prune` weekly
- [ ] Monitor `~/.beads/daemon.log` for errors
- [ ] Verify sync branch health: `git log origin/<sync-branch> -3`
```

---

## Symptom-Based Navigation (Decision Tree)

```
START: "My beads isn't working"
│
├─► Error contains "database"?
│   ├─► "failed to open database" → [A1-VALIDATION-CATCH22]
│   ├─► "database locked" → [C2-MIGRATION-RACE]
│   ├─► "database not initialized" → [A4-DAEMON-STORE-INIT]
│   └─► "no beads database found" → [A7-JSONL-ONLY-BROKEN]
│
├─► Error contains "worktree"?
│   ├─► "missing but already registered" → [B1-WORKTREE-PATH]
│   └─► "already checked out" → [B1-WORKTREE-PATH]
│
├─► Error contains "sync" or "push"?
│   ├─► "push failed" → [B2-DAEMON-RACE] or [C1-PUSH-REJECTION]
│   ├─► "fetch first" → [C1-PUSH-REJECTION]
│   └─► "context canceled" → [B4-HOOK-BLOCKING]
│
├─► Issues disappearing?
│   ├─► After sync → [A5-MULTI-MACHINE-LOSS] or [C3-JSONL-OVERWRITE]
│   └─► After bd doctor → [D4-DOCTOR-FIX-DAMAGE]
│
├─► bd doctor shows problems?
│   ├─► "orphaned dependencies" → [A1-VALIDATION-CATCH22]
│   ├─► "dependency cycles" → [D2-FALSE-POSITIVES]
│   └─► "anti-patterns" → [D2-FALSE-POSITIVES]
│
├─► Error contains "config" or "prefix"?
│   ├─► "prefix mismatch detected" → [B3-CONFIG-IGNORED]
│   ├─► "sync.remote" not honored → [B3-CONFIG-IGNORED]
│   └─► "not in a bd workspace" → [B3-CONFIG-IGNORED]
│
├─► Daemon-related issues?
│   ├─► `bd daemons status` shows errors → [B2-DAEMON-RACE]
│   ├─► Commands hang/freeze → [B4-HOOK-BLOCKING]
│   ├─► Works with `--no-daemon` only → [A4-DAEMON-STORE-INIT]
│   └─► Daemon won't start → [B2-DAEMON-RACE]
│
└─► None of above → Run Universal Recovery Sequence → [General Troubleshooting]
```

---

## Epic 2 v2.0 Story Recommendations

### Proposed Story Structure

| Story | Title | Priority | Patterns | Est. Scope |
|-------|-------|----------|----------|------------|
| **2.1v2** | Database Recovery Runbook | P1 | A1, A2, A3, A6, A7, C2, D4 | 8 patterns, 3000-4000 words |
| **2.2v2** | Sync & Worktree Recovery Runbook | P1 | A5, B1-B5, C1, C3 | 8 patterns, 3500-4500 words |
| **2.3v2** | Multi-Agent Workflow Guide | P2 | A4, C4, C5 | 3 patterns, 1500-2000 words |
| **2.4v2** | Quick Reference & Decision Tree | P1 | All (navigation) | Decision tree, 800-1000 words |
| **2.5v2** | Prevention Best Practices | P2 | D2, D3, prevention | 6-8 sections, 1500-2000 words |

### Key Changes from Original Proposal

1. **No dedicated "Circular Dependencies" runbook** - Research found zero true cycles
2. **D4 (`bd doctor --fix` damage) moved to Database Recovery** - Critical recovery scenario
3. **Multi-Agent guide separated** - Unique scenarios warrant dedicated documentation
4. **Quick Reference elevated to P1** - Highest user-facing value for troubleshooting

### Pattern → Story Mapping

**Story 2.1v2 (Database Recovery):**
- A1-VALIDATION-CATCH22: Can't open DB = can't fix DB
- A2-MIGRATION-SCHEMA: Column mismatch during upgrade
- A3-TOMBSTONE-CORRUPTION: Deletions manifest corrupted
- A6-ID-COLLISION: Hierarchical ID parsing issues
- A7-JSONL-ONLY-BROKEN: No-db mode fails
- C2-MIGRATION-RACE: Parallel migration conflicts
- D4-DOCTOR-FIX-DAMAGE: Automated fix destroys data

**Story 2.2v2 (Sync & Worktree):**
- A5-MULTI-MACHINE-LOSS: Sync overwrites data
- B1-WORKTREE-PATH: Path resolution failures
- B2-DAEMON-RACE: Push/pull race conditions
- B3-CONFIG-IGNORED: Settings not honored
- B4-HOOK-BLOCKING: Auth/hook deadlocks
- B5-STATE-DRIFT: Hash/state reconstruction
- C1-PUSH-REJECTION: Multi-clone push race
- C3-JSONL-OVERWRITE: Fresh clone data loss

**Story 2.3v2 (Multi-Agent):**
- A4-DAEMON-STORE-INIT: Daemon mode nil store
- C4-COMPACTION-CONFLICT: Multi-agent compaction
- C5-GITIGNORE-CONFLICT: Config file conflicts

**Story 2.5v2 (Prevention):**
- D2-FALSE-POSITIVES: relates_to/parent-child detection
- D3-DIRECTION-CONFUSION: "X needs Y" mental model
- All prevention sections from other patterns

### Patterns NOT Requiring Runbooks

- **D1-ALGO-PERFORMANCE**: Fixed in v0.35.0, document in release notes only

---

## Appendix: Full Pattern Inventory

### Pattern ID Convention

| Category | Prefix | Source Document | Description |
|----------|--------|-----------------|-------------|
| **A** | A1-A7 | database-corruption-patterns.md | Database state, migrations, data integrity |
| **B** | B1-B5 | sync-failures-patterns.md | Sync operations, worktree management |
| **C** | C1-C5 | merge-conflicts-patterns.md | Concurrent access, merge conflicts |
| **D** | D1-D4 | circular-dependencies-patterns.md | Dependency detection, validation |

**Mapping to Source Pattern Numbers:**
- A1 = Database Pattern 1 (Validation Catch-22)
- A2 = Database Pattern 2 (Migration Schema)
- B1 = Sync Pattern 1 + Merge Pattern 1 (Worktree, consolidated)
- C3 = Merge Pattern 4 + Sync Pattern 3 (Data Loss, consolidated)

### Category A: Database State Issues

| ID | Name | Severity | Issues | Status |
|----|------|----------|--------|--------|
| A1 | VALIDATION-CATCH22 | Critical | #806 | Open (PR #805) |
| A2 | MIGRATION-SCHEMA | High | #757, #720, #669 | Fixed v0.35.0+ |
| A3 | TOMBSTONE-CORRUPTION | Critical | #552, #590 | Fixed v0.30.2+ |
| A4 | DAEMON-STORE-INIT | Medium | #719, #751, #669 | Fixed v0.35.0+ |
| A5 | MULTI-MACHINE-LOSS | Critical | #464, #746 | Fixed v0.30.0+ |
| A6 | ID-COLLISION | High | #728, #664 | Fixed v0.36.0+ |
| A7 | JSONL-ONLY-BROKEN | High | #534 | Fixed |

### Category B: Sync & Worktree Issues

| ID | Name | Severity | Issues | Status |
|----|------|----------|--------|--------|
| B1 | WORKTREE-PATH | High | #785, #639, #609, #807, #570 | Fixed v0.40.0+ |
| B2 | DAEMON-RACE | Critical | #694, #695, #693, #785 | Fixed v0.35.0+ |
| B3 | CONFIG-IGNORED | High | #736, #686, #546 | Fixed v0.37.0+ |
| B4 | HOOK-BLOCKING | Medium | #647, #532 | Fixed v0.32.0+ |
| B5 | STATE-DRIFT | Medium | #520, #532 | Fixed |

### Category C: Merge & Concurrency Issues

| ID | Name | Severity | Issues | Status |
|----|------|----------|--------|--------|
| C1 | PUSH-REJECTION | High | #694, #746 | Fixed v0.35.0+ |
| C2 | MIGRATION-RACE | Critical | #720, #607, #536 | Fixed v0.35.0+ |
| C3 | JSONL-OVERWRITE | Critical | #464, #158 | Fixed v0.30.0+ |
| C4 | COMPACTION-CONFLICT | Medium | #650, #747, #158 | Architectural |
| C5 | GITIGNORE-CONFLICT | Low | #797, #349 | Fixed |

### Category D: Dependency Issues

| ID | Name | Severity | Issues | Status |
|----|------|----------|--------|--------|
| D1 | ALGO-PERFORMANCE | High | #774 | Fixed v0.35.0+ |
| D2 | FALSE-POSITIVES | High | #661, #750, #740 | Fixed |
| D3 | DIRECTION-CONFUSION | Low | #440, #723, #544 | Documentation |
| D4 | DOCTOR-FIX-DAMAGE | Critical | #740, #630 | Fixed |

---

## Document Metadata

- **Generated:** 2025-12-30
- **Reviewed:** 2025-12-30 (Code Review fixes applied)
- **Story:** 2.5 - Analysis Synthesis + Recovery Framework Design
- **Epic:** 2 - Recovery Documentation Analysis & Knowledge Gathering
- **Beads ID:** bd-9g9.5
- **Author:** Dev Agent (Claude Opus 4.5)
- **Beads Version Range:** v0.29.0 - v0.41.0
- **Total GitHub Issues Analyzed:** 54
- **Source Patterns:** 23 (from 4 research documents)
- **Consolidated Unique Patterns:** 21 (after overlap analysis)

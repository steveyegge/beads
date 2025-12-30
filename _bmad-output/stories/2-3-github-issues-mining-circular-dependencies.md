# Story 2.3: GitHub Issues Mining - Circular Dependencies Patterns

Status: done

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

**Beads IDs:**

- Epic: `bd-9g9`
- Story: `bd-9g9.3`

**Quick Commands:**

- View tasks: `bd list --parent bd-9g9.3`
- Find ready work: `bd ready --parent bd-9g9.3`
- Mark task done: `bd close <task_id>`

## Story

As a **documentation author**,
I want **extracted circular dependency patterns from GitHub issues**,
so that **recovery documentation helps users detect and break dependency cycles**.

## Acceptance Criteria

1. **Given** access to beads GitHub repository issues
   **When** I analyze issues related to circular dependencies, blocked issues, and dependency errors
   **Then** pattern document captures:
   - Cycle detection error messages
   - Scenarios that create circular dependencies
   - `bd blocked` and `bd dep` command usage patterns
   - Cycle-breaking strategies that worked

2. **And** minimum 5 real issues analyzed (if available, may be fewer)

3. **And** patterns include prevention best practices

4. **And** output saved to `_bmad-output/research/circular-dependencies-patterns.md`

## Technical Prerequisites

### GitHub API Access

**Search Strategy:** Use GitHub web search (no authentication required for public repos)

**Direct Search URLs:**
- [circular issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+circular)
- [cycle issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+cycle)
- [dependency issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+dependency)
- [bd dep issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+%22bd+dep%22)
- [bd blocked issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+%22bd+blocked%22)

**Alternative (if rate limited):**
```bash
# Using gh CLI (requires: gh auth login)
gh issue list -R steveyegge/beads --search "circular" --limit 100
gh issue list -R steveyegge/beads --search "cycle" --limit 100
gh issue view 123 -R steveyegge/beads --comments
```

**Rate Limits:**
- Unauthenticated: 60 requests/hour
- Authenticated: 5000 requests/hour
- Web interface: No practical limit for manual browsing

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

### Task 1: Search & Identify Circular Dependency Issues (AC: #1, #2)

- [x] **1.1** Search GitHub issues with queries:
  - `repo:steveyegge/beads is:issue circular`
  - `repo:steveyegge/beads is:issue cycle`
  - `repo:steveyegge/beads is:issue dependency`
  - `repo:steveyegge/beads is:issue "bd dep"`
  - `repo:steveyegge/beads is:issue "bd blocked"`
  - `repo:steveyegge/beads is:issue deadlock`
- [x] **1.2** Create list of candidate issues (aim for 8-10 to filter to 5+ quality issues)
- [x] **1.3** Document issue metadata: ID, title, status, labels
- [x] **1.4** **Fallback**: If <5 circular dependency issues found, expand search to include `blocked`, `ready`, `dependency chain` queries and document actual count with justification
- [x] **1.5** Update task status: `bd update bd-9g9.3.1 --status=completed`

### Task 2: Deep Analysis of Each Issue (AC: #1, #3)

For each identified issue:

- [x] **2.1** Extract cycle detection symptoms:
  - Error messages verbatim
  - `bd blocked` output showing cycles
  - Unexpected blocking behavior
- [x] **2.2** Extract scenarios that create cycles:
  - Dependency chain patterns (A→B→C→A)
  - User workflows that led to circular deps
  - Accidental vs intentional dependency creation
- [x] **2.3** Extract `bd dep` and `bd blocked` usage:
  - Commands used to diagnose
  - Command output interpretation
  - Dependency visualization if available
- [x] **2.4** Extract cycle-breaking strategies:
  - `bd dep remove` commands
  - Manual JSONL editing (if mentioned)
  - Issue restructuring approaches
- [x] **2.5** Update task status: `bd update bd-9g9.3.2 --status=completed`

### Task 3: Pattern Synthesis & Prevention (AC: #1, #3)

- [x] **3.1** Categorize cycle patterns:
  - Simple A↔B mutual dependency
  - Chain cycles (A→B→C→A)
  - Complex multi-node cycles
- [x] **3.2** Identify prevention best practices from issue discussions
- [x] **3.3** Document common user mistakes that lead to cycles
- [x] **3.4** Map detection → diagnosis → resolution flow
- [x] **3.5** Update task status: `bd update bd-9g9.3.3 --status=completed`

### Task 4: Create Output Document (AC: #4)

- [x] **4.1** Create `_bmad-output/research/circular-dependencies-patterns.md` with structure:

```markdown
# Circular Dependencies Patterns Analysis

## Executive Summary
- Total issues analyzed: X
- Patterns identified: Y
- Most common pattern type: Z

## Methodology
- Search queries used
- Date range of issues analyzed
- Selection criteria applied

## Issue Availability Note
(Document if fewer than 5 issues were found, explain search methodology)

## Pattern Catalog

### Pattern 1: [Descriptive Name]

#### Symptoms
- [Observable symptom 1]
- [Observable symptom 2]

#### How Cycle Forms
[Technical explanation of how this cycle type occurs]

#### bd Commands for Diagnosis
```bash
$ bd blocked
$ bd dep list <issue>
```

#### Resolution Strategy
1. [Step with explicit command]
2. [Verification step]

#### Prevention
[How to avoid this in future]

### Pattern 2: ...
(Repeat for each pattern)

## Prevention Best Practices
- [Best practice 1]
- [Best practice 2]

## bd dep / bd blocked Command Reference
| Command | Purpose |
|---------|---------|
| `bd dep add X Y` | X depends on Y (Y blocks X) |
| `bd dep remove X Y` | Remove dependency |
| `bd blocked` | Show all blocked issues |
| `bd ready` | Show unblocked issues |

## Recommendations for Recovery Documentation
- Priority patterns for Epic 2 v2.0
- Suggested documentation structure
- Cross-references to Story 2.5 synthesis

## Appendix: Issues Analyzed
| Issue # | Title | Pattern(s) |
|---------|-------|------------|
| [#123](https://github.com/steveyegge/beads/issues/123) | [Title] | Pattern 1 |
```

- [x] **4.2** Reference all analyzed issues with GitHub links: `[#123](https://github.com/steveyegge/beads/issues/123)`
- [x] **4.3** Verify completeness against AC
- [x] **4.4** Ensure output format aligns with Architecture.md Recovery Section Format (lines 277-299)
- [x] **4.5** Update task status: `bd update bd-9g9.3.4 --status=completed`

## Dev Notes

### Research Context

This is a **Phase 2 Analysis** story. Circular dependencies may be less common than other issues, hence the lower minimum (5 vs 10).

**Source Repository:** [steveyegge/beads](https://github.com/steveyegge/beads)
- 363+ closed issues as of Epic 2 planning
- Active development with regular issue discussions

### Beads Dependency Model

Beads supports issue dependencies via:
- `bd dep add <issue> <depends-on>` - Add dependency
- `bd dep remove <issue> <depends-on>` - Remove dependency
- `bd blocked` - Show blocked issues
- `bd ready` - Show issues with no blockers

**Temporal Language Gotcha** (from project-context.md):
- WRONG: `bd dep add phase1 phase2` (temporal: "1 before 2")
- RIGHT: `bd dep add phase2 phase1` (requirement: "2 needs 1")
- Think "X needs Y", not "X comes before Y"

### Expected Patterns (Hypothesis)

1. **Mutual dependency** - Two issues each marked as blocking the other
2. **Chain cycles** - A blocks B blocks C blocks A
3. **Self-dependency** - Issue accidentally depends on itself
4. **Import/migration cycles** - Bulk import creates circular refs

### Search Strategy Notes

Circular dependency issues might be labeled or discussed as:
- "circular", "cycle", "deadlock"
- "blocked forever", "can't resolve"
- Dependency-related error messages
- `bd blocked` showing unexpected results

### Output Quality Criteria

The output document must be actionable for:
- Writing Circular Dependencies Recovery Runbook (Epic 2 v2.0)
- Creating dependency management best practices documentation
- Informing `bd dep` and `bd blocked` command documentation
- Creating troubleshooting decision trees

Even with potentially fewer issues:
- Document methodology thoroughly
- Extract maximum value from each issue found
- Note gaps for future investigation
- Provide actionable prevention guidance

### Story 2.5 Handoff Requirements

This story's output feeds into **Story 2.5: Analysis Synthesis**. Ensure:
- Patterns are clearly named and numbered for cross-referencing
- Issue count documented (even if <5) with search methodology
- Recommendations section explicitly suggests Epic 2 v2.0 story structure
- All issue links are valid GitHub URLs

### References

- [Source: _bmad-output/epics/epic-2-*.md#Story 2.3]
- [Source: _bmad-output/architecture.md#Recovery Section Format] - lines 277-299
- [Source: _bmad-output/prd.md#FR4] - Recovery Runbook for circular dependencies
- [Source: _bmad-output/project-context.md#Gotchas when Filing Beads]
- [GitHub Repository](https://github.com/steveyegge/beads) - Source for issue mining

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

- WebFetch queries to GitHub issues API (8 issue URLs verified)
- Search queries: `is:issue circular`, `is:issue cycle`, `is:issue dependency`, `is:issue "bd dep"`, `is:issue "bd blocked"`, `is:issue blocked`
- Beads CLI commands: `bd show`, `bd close`, `bd update`, `bd label`
- No rate limiting encountered (public repo access)

### Completion Notes List

- **8 issues analyzed** (exceeds minimum 5): #774, #661, #750, #740, #723, #544, #440, #630
- **4 patterns identified:**
  1. Algorithm Performance (O(2^n) → O(V+E) fix)
  2. False Positive Detection (relates_to, parent-child misclassified)
  3. Dependency Direction Confusion (documentation vs reality)
  4. Data Corruption from Automated Fixes (bd doctor --fix, bd rename-prefix)
- **Key insight:** Most "circular dependency" reports are false positives from overly aggressive detection
- **Recovery strategies documented:** git checkout for .beads/, bd sync --force-rebuild
- **Prevention best practices:** "Think X needs Y", verify with bd show, never blind-fix

### File List

- `_bmad-output/research/circular-dependencies-patterns.md` (created)

### Change Log

- 2025-12-30: Story completed - analyzed 8 GitHub issues, identified 4 patterns, created comprehensive output document
- 2025-12-30: Code review fixes applied:
  - Added "Key Finding: True Circular Dependencies Are Rare" section (H1 fix)
  - Added "Cycle-Breaking Strategies" section with 3 explicit strategies (H1 fix)
  - Added pattern IDs (CDP-1 to CDP-4) for Story 2.5 cross-reference (M3 fix)
  - Updated methodology with precise search date (L1 fix)
  - Updated Debug Log References with query details (L2 fix)

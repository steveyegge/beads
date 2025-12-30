# Story 2.1: GitHub Issues Mining - Database Corruption Patterns

Status: done

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- Beads is the source of truth for task status. Update via bd commands. -->

**Beads IDs:**

- Epic: `bd-9g9`
- Story: `bd-9g9.1`

**Quick Commands:**

- View tasks: `bd list --parent bd-9g9.1`
- Find ready work: `bd ready --parent bd-9g9.1`
- Mark task done: `bd close <task_id>`

## Story

As a **documentation author**,
I want **extracted database corruption patterns from GitHub issues**,
so that **recovery documentation is based on real user experiences, not guesswork**.

## Acceptance Criteria

1. **Given** access to beads GitHub repository (363+ closed issues)
   **When** I analyze issues related to database corruption, SQLite errors, and .beads/beads.db problems
   **Then** pattern document captures:
   - Common symptoms reported by users
   - Root causes identified in issue discussions
   - Solutions that worked (with bd commands used)
   - Prevention strategies mentioned

2. **And** minimum 10 real issues analyzed and documented

3. **And** patterns categorized by severity and frequency

4. **And** output saved to `_bmad-output/research/database-corruption-patterns.md`

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- The checkboxes below are for reference only. Use bd commands to update.    -->
<!-- To view current status: bd list --parent bd-9g9.1                          -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

### Task 1: Search & Identify Database Corruption Issues (AC: #1, #2)

- [x] **1.1** Search GitHub issues using these direct URLs:
  - [database issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+database)
  - [SQLite issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+SQLite)
  - [beads.db issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+%22beads.db%22)
  - [corruption issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+corruption)
  - [database locked issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+%22database+locked%22)
  - [.beads/ issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+.beads%2F)
- [x] **1.2** Create initial list of candidate issues (aim for 15-20 to filter down to 10+ quality issues)
- [x] **1.3** Document issue metadata: ID, title, status, labels, comment count
- [x] **1.4** **Fallback**: If <10 database corruption issues found, expand search to include `error`, `fail`, `broken` queries and document actual count with justification

### Task 2: Deep Analysis of Each Issue (AC: #1, #3)

For each of the 10+ identified issues:

- [x] **2.1** Extract symptoms:
  - Error messages verbatim
  - User-reported behavior
  - Environment context (OS, version if mentioned)
- [x] **2.2** Extract root causes:
  - Technical cause discussed in comments
  - Conditions that triggered the issue
  - Related code/configuration factors
- [x] **2.3** Extract solutions:
  - Commands that resolved the issue (`bd sync --force-rebuild`, etc.)
  - Workarounds applied
  - Configuration changes
- [x] **2.4** Categorize by severity:
  - **Critical**: Data loss, unrecoverable without backup (e.g., "beads.db corrupted, all issues lost")
  - **High**: Workflow blocked, requires manual intervention (e.g., "database locked, cannot sync")
  - **Medium**: Inconvenient, workaround available (e.g., "slow queries, restart helps")
  - **Low**: Cosmetic, minimal impact (e.g., "warning message but continues working")

### Task 3: Pattern Synthesis (AC: #1, #3)

- [x] **3.1** Group issues by common symptoms
- [x] **3.2** Identify recurring root cause patterns
- [x] **3.3** Rank patterns by frequency (how many issues exhibit each)
- [x] **3.4** Map solutions to patterns (which fixes work for which problems)

### Task 4: Create Output Document (AC: #4)

- [x] **4.1** Create `_bmad-output/research/` directory if not exists
- [x] **4.2** Create `_bmad-output/research/database-corruption-patterns.md` following this template:

```markdown
# Database Corruption Patterns Analysis

## Executive Summary
- Total issues analyzed: X
- Patterns identified: Y
- Most common severity: Z

## Methodology
- Search queries used
- Date range of issues analyzed
- Selection criteria applied

## Pattern Catalog

### Pattern 1: [Descriptive Name]

#### Symptoms
- [Observable symptom 1]
- [Observable symptom 2]

#### Root Cause
[Technical explanation of why this happens]

#### Frequency
X of Y issues (Z%)

#### Solutions
1. [Step with explicit command]
2. [Verification step]

#### Prevention
[How to avoid this in future]

### Pattern 2: ...
(Repeat for each pattern)

## Severity Distribution
| Severity | Count | Percentage |
|----------|-------|------------|
| Critical | X | Y% |
| High | X | Y% |
| Medium | X | Y% |
| Low | X | Y% |

## Recommendations for Recovery Documentation
- Priority patterns for Epic 2 v2.0
- Suggested documentation structure
- Cross-references to Story 2.5 synthesis

## Appendix: Issues Analyzed
| Issue # | Title | Severity | Pattern(s) |
|---------|-------|----------|------------|
| #123 | [Title] | High | Pattern 1, 3 |
```

- [x] **4.3** Ensure all issues are referenced with GitHub links: `[#123](https://github.com/steveyegge/beads/issues/123)`
- [x] **4.4** Verify pattern document completeness against AC
- [x] **4.5** Ensure output format aligns with Architecture.md Recovery Section Format (lines 278-298)

## Dev Notes

### Research Context

This is a **Phase 2 Analysis** story, not code implementation. The goal is knowledge extraction from existing GitHub issues to inform future recovery documentation.

**Source Repository:** [steveyegge/beads](https://github.com/steveyegge/beads)
- 363+ closed issues as of Epic 2 planning
- Active development with regular issue discussions

### GitHub Access Strategy

**Recommended approach:** Use GitHub web interface (no authentication required for public repos)

**Alternative (if rate limited):**
```bash
# Using gh CLI (requires: gh auth login)
gh issue list -R steveyegge/beads --search "database" --limit 100
gh issue view 123 -R steveyegge/beads --comments
```

**Rate Limits:**
- Unauthenticated: 60 requests/hour
- Authenticated: 5000 requests/hour
- Web interface: No practical limit for manual browsing

### Story 2.5 Handoff Requirements

This story's output feeds into **Story 2.5: Analysis Synthesis**. Ensure:
- Patterns are clearly named and numbered for cross-referencing
- Severity distribution is quantified (not just described)
- Recommendations section explicitly suggests Epic 2 v2.0 story structure
- All issue links are valid GitHub URLs

### Output Quality Criteria

The output document must be actionable for:
- Writing Recovery Runbook (Epic 2 v2.0)
- Informing `bd sync --force-rebuild` documentation
- Creating troubleshooting decision trees

### Project Structure Notes

- Output location: `_bmad-output/research/` (research artifacts, not production docs)
- Final Recovery docs will be in `website/docs/recovery/` (future epic)
- This analysis feeds into Story 2.5: Analysis Synthesis

### References

- Epic 2 source: `_bmad-output/epics/epic-2-*.md` (Story 2.1 section)
- Architecture: `_bmad-output/architecture.md` (Recovery Section Format, lines 273-298) - **CRITICAL: Use this format for pattern documentation**
- PRD: `_bmad-output/prd.md` (FR2: Recovery Runbook for database corruption)
- Project Context: `_bmad-output/project-context.md` (Go CLI section)
- [GitHub Repository](https://github.com/steveyegge/beads) - Source for issue mining

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

N/A - Research story, no code debugging required.

### Completion Notes List

1. **Search Coverage:** Executed 8 distinct GitHub search queries covering database, SQLite, corruption, beads.db, .beads/, migrate, error, and fail keywords.

2. **Issue Selection:** Identified 14 high-quality issues from initial pool of 50+ candidates. Selection criteria focused on database state, data integrity, and corruption scenarios.

3. **Pattern Identification:** Synthesized 7 distinct patterns from the 14 analyzed issues:
   - Pattern 1: Validation Catch-22 (unopenable database)
   - Pattern 2: Migration Schema Mismatch
   - Pattern 3: Tombstone/Manifest Corruption
   - Pattern 4: Daemon Mode Store Initialization Failures
   - Pattern 5: Multi-Machine Sync Data Loss
   - Pattern 6: Hierarchical ID Collision/Parsing
   - Pattern 7: JSONL-Only Mode Broken

4. **Severity Distribution:** Critical: 3 (21%), High: 6 (43%), Medium: 4 (29%), Low: 1 (7%)

5. **Key Finding:** Patterns 3 and 5 (sync/merge issues) represent the highest risk for data loss and should be prioritized in recovery documentation.

6. **Handoff to Story 2.5:** Document includes explicit recommendations section for Epic 2 v2.0 structure and cross-references.

## File List

- `_bmad-output/research/database-corruption-patterns.md` (created)
- `_bmad-output/stories/2-1-github-issues-mining-database-corruption.md` (updated)
- `_bmad-output/sprint-status.yaml` (updated)

## Change Log

| Date | Change | Author |
|------|--------|--------|
| 2025-12-30 | Story created from Epic 2 | PM Agent |
| 2025-12-30 | Completed all tasks: searched 8 query types, analyzed 14 issues, identified 7 patterns, created output document | Claude Opus 4.5 |
| 2025-12-30 | Code review: Added Format Note and Quick Diagnostic Commands section to output doc; verified GitHub links; fixed reference formatting | Claude Opus 4.5 (Review) |

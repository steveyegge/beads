# Story 2.2: GitHub Issues Mining - Merge Conflicts Patterns

Status: done

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

**Beads IDs:**

- Epic: `bd-9g9`
- Story: `bd-9g9.2`

**Quick Commands:**

- View tasks: `bd list --parent bd-9g9.2`
- Find ready work: `bd ready --parent bd-9g9.2`
- Mark task done: `bd close <task_id>`

## Story

As a **documentation author**,
I want **extracted merge conflict patterns from GitHub issues**,
so that **recovery documentation addresses real JSONL and sync merge scenarios**.

## Acceptance Criteria

1. **Given** access to beads GitHub repository issues
   **When** I analyze issues related to merge conflicts, JSONL conflicts, and git sync problems
   **Then** pattern document captures:
   - Conflict symptoms and error messages
   - Workflow scenarios that trigger conflicts
   - Resolution strategies that worked
   - `bd sync` options and their effects

2. **And** minimum 10 real issues analyzed and documented
   **Quality-first approach:** Aim for 10+ issues, accept minimum 5 high-quality issues
   **Fallback strategy:** If <10 merge conflict issues exist, supplement with related sync/conflict/Git issues

3. **And** patterns include multi-agent and team collaboration scenarios
   **Multi-agent criteria:** Issues mentioning multiple AI agents editing simultaneously, human + AI agent conflicts, or CI/CD + human workflow conflicts

4. **And** output saved to `_bmad-output/research/merge-conflicts-patterns.md`

## Technical Prerequisites

### GitHub API Access

**Required:** GitHub CLI (recommended) or personal access token for API access.

**Setup Options:**

1. **GitHub CLI (Recommended)**
   ```bash
   gh auth login
   # Test access: gh issue list --repo steveyegge/beads --limit 5
   ```

2. **Personal Access Token**
   ```bash
   export GITHUB_TOKEN="your_personal_access_token"
   # Test access: curl -H "Authorization: token $GITHUB_TOKEN" https://api.github.com/repos/steveyegge/beads/issues
   ```

**Rate Limits:** GitHub API allows 5,000 requests/hour for authenticated users.

**Search Strategy:** Use GitHub web search + manual issue review (no API automation needed for this research task).

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

### Task 1: Search & Identify Merge Conflict Issues (AC: #1, #2)

- [x] **1.1** Search GitHub issues using these direct URLs:
  - [merge conflict issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+merge+conflict)
  - [JSONL issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+JSONL)
  - [sync conflict issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+sync+conflict)
  - [git merge issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+git+merge)
  - [conflict marker issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+%22conflict+marker%22)
  - [<<< marker issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+%22%3C%3C%3C%3C%3C%3C%22)
  - [rebase issues](https://github.com/steveyegge/beads/issues?q=is%3Aissue+rebase)
- [x] **1.2** Create initial list of candidate issues (aim for 15-20 to filter down to 10+ quality issues)
- [x] **1.3** Document issue metadata: ID, title, status, labels, comment count
- [x] **1.4** **Fallback**: If <10 merge conflict issues found, expand to related sync/git/concurrent access issues
- [x] **1.5** Update task status: `bd close <task_id>`

### Task 2: Deep Analysis of Each Issue (AC: #1, #3)

For each of the 10+ identified issues:

- [x] **2.1** Extract conflict symptoms:
  - Error messages verbatim (especially conflict markers)
  - User-reported merge failures
  - JSONL-specific conflict indicators
  - Git status output during conflicts
- [x] **2.2** Extract workflow scenarios:
  - Multi-user/multi-agent workflows that triggered conflict
  - Branch patterns that led to issues
  - Timing/concurrency conditions
  - Team collaboration contexts
- [x] **2.3** Extract resolution strategies:
  - `bd sync` command variations used
  - Manual resolution steps
  - JSONL-specific merge techniques
  - Git commands that helped
- [x] **2.4** Document `bd sync` options effects:
  - `--force-rebuild` behavior
  - `--dry-run` for preview
  - Other flags mentioned in issues
- [x] **2.5** Categorize by severity:
  - **Critical**: Data loss, JSONL corruption unrecoverable (e.g., "all issues lost after merge")
  - **High**: Workflow blocked, manual intervention required (e.g., "sync fails with conflict markers")
  - **Medium**: Inconvenient, workaround available (e.g., "had to manually resolve JSONL")
  - **Low**: Cosmetic, minimal impact (e.g., "warning but sync completed")
- [x] **2.6** Update task status: `bd close <task_id>`

### Task 3: Pattern Synthesis (AC: #1, #3)

- [x] **3.1** Group issues by conflict type:
  - JSONL content conflicts
  - beads.db state conflicts
  - Git branch conflicts
  - Multi-agent simultaneous edit conflicts
- [x] **3.2** Identify recurring workflow patterns that cause conflicts
- [x] **3.3** Rank by frequency and impact
- [x] **3.4** Map resolution strategies to conflict types
- [x] **3.5** Update task status: `bd close <task_id>`

### Task 4: Create Output Document (AC: #4)

- [x] **4.1** Create `_bmad-output/research/` directory if not exists
- [x] **4.2** Create `_bmad-output/research/merge-conflicts-patterns.md` following this template:

```markdown
# Merge Conflicts Patterns Analysis

## Executive Summary
- Total issues analyzed: X
- Patterns identified: Y
- Most common conflict type: Z
- Multi-agent scenarios found: N

## Methodology
- Search queries used
- Date range of issues analyzed
- Selection criteria applied

## Conflict Type Taxonomy
| Type | Description | Count |
|------|-------------|-------|
| JSONL Content | Conflict markers in JSON lines | X |
| beads.db State | SQLite vs JSONL mismatch | X |
| Git Branch | Standard git merge conflicts | X |
| Multi-Agent | Concurrent AI/human edits | X |

## Pattern Catalog

### Pattern 1: [Descriptive Name]

#### Symptoms & Error Messages
- [Observable symptom 1]
- Error: `[exact error message]`

#### Triggering Workflow
[Scenario that causes this conflict]

#### Frequency
X of Y issues (Z%)

#### Resolution Strategy
1. [Step with explicit command]
2. [Verification step]

#### bd sync Options Used
- `--force-rebuild`: [when/why used]
- Other flags: [if applicable]

#### Prevention
[How to avoid this in future]

### Pattern 2: ...
(Repeat for each pattern)

## Multi-Agent Collaboration Scenarios
| Scenario | Frequency | Resolution |
|----------|-----------|------------|
| Claude Code + Human | X issues | [approach] |
| Multiple AI Agents | X issues | [approach] |
| CI/CD + Human | X issues | [approach] |

## bd sync Command Reference (from real usage)
| Flag | Effect | When to Use |
|------|--------|-------------|
| `--force-rebuild` | [effect] | [scenario] |
| `--dry-run` | [effect] | [scenario] |

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
| Issue # | Title | Conflict Type | Severity | Pattern(s) |
|---------|-------|---------------|----------|------------|
| [#123](https://github.com/steveyegge/beads/issues/123) | [Title] | JSONL | High | Pattern 1 |
```

- [x] **4.3** Ensure all issues are referenced with GitHub links: `[#123](https://github.com/steveyegge/beads/issues/123)`
- [x] **4.4** Verify pattern document completeness against AC
- [x] **4.5** Ensure output format aligns with Architecture.md Recovery Section Format (lines 278-298)
- [x] **4.6** Update task status: `bd close <task_id>`

## Dev Notes

### Critical JSONL Context

**Beads-Specific:** Issues stored as JSONL (one JSON per line). Merge conflicts break JSON parsing.
- Conflict markers (`<<<<<<<`) corrupt JSON structure
- Line-based Git merging can split JSON objects
- `bd sync` operations vulnerable to concurrent access

### Story 2.5 Handoff Requirements

This story's output feeds into **Story 2.5: Analysis Synthesis**. Ensure:
- Patterns are clearly named and numbered for cross-referencing
- Conflict type taxonomy is consistent with other Story 2.x outputs
- Multi-agent scenarios are explicitly documented (key differentiator from Story 2.1)
- Severity distribution is quantified (not just described)
- Recommendations section explicitly suggests Epic 2 v2.0 story structure
- All issue links are valid GitHub URLs

### Key Technical Focus

- `bd sync --force-rebuild` vs `--dry-run` usage patterns
- JSONL conflict resolution strategies vs standard Git merging
- Multi-agent workflow scenarios (Claude Code + human, CI/CD conflicts)

### References

- [Source: _bmad-output/epics/epic-2-*.md#Story 2.2]
- [Source: _bmad-output/architecture.md#Recovery Section Format] - **CRITICAL: Use this format for pattern documentation**
- [Source: _bmad-output/prd.md#FR3] - Recovery Runbook for merge conflicts
- [Source: _bmad-output/project-context.md#Go CLI (beads/bd)]
- [GitHub Repository](https://github.com/steveyegge/beads) - Source for issue mining
- [Story 2.1](./2-1-github-issues-mining-database-corruption.md) - Sibling story for format consistency

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

- Used WebFetch to analyze 16 GitHub issues
- Search queries: merge conflict, JSONL, sync conflict, git merge, rebase, worktree, concurrent, multi-agent, database locked

### Completion Notes List

1. **16 issues analyzed** (exceeded minimum 10 requirement)
2. **6 major patterns identified:**
   - Sync Branch Worktree Path Resolution Failure (31%)
   - Multi-Clone Push Rejection Race Condition (12%)
   - Parallel Database Migration Race (12%)
   - Fresh Clone JSONL Overwrite Data Loss (12%)
   - Multi-Agent Compaction Conflicts (19%)
   - Gitignore and Exclude Configuration Conflicts (12%)
3. **Severity distribution:** 25% Critical, 44% High, 19% Medium, 12% Low
4. **Multi-agent scenarios:** 4 issues explicitly document multi-agent conflicts
5. **bd sync options documented:** --force-rebuild, --dry-run, --no-daemon, --no-db
6. **Cross-references:** Aligned with Story 2.1 format for synthesis in Story 2.5

### File List

- `_bmad-output/research/merge-conflicts-patterns.md` (created)

### Change Log

- 2025-12-30: Created merge-conflicts-patterns.md with 6 patterns from 16 issues
- 2025-12-30: Completed all 4 tasks, marked story for review
- 2025-12-30: **Code Review Fixes (Claude Opus 4.5)**:
  - Added "Diagnosis" sections with diagnostic commands to all 6 patterns (Architecture.md compliance)
  - Clarified Conflict Type Taxonomy count (issues can belong to multiple types)
  - Verified 3 sample GitHub issue links (#785, #464, #720) - all valid
  - Confirmed Beads status tracking is correct (in_progress + bmad:stage:review label)
- 2025-12-30: **Code Review #2 Fixes (Claude Opus 4.5)**:
  - Fixed Severity Distribution table to match Appendix counts (Critical=2, High=8, Medium=4, Low=2)
  - Added #536 to Pattern 3 Related Issues and updated frequency (2→3 of 16, 12%→19%)
  - Removed duplicate `bmad:stage:review` label from Beads (kept `bmad:stage:done`)

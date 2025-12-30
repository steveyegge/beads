# Story 3.1: Architecture Overview Enhancement

Status: review

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- Beads is the source of truth for task status. Update via bd commands. -->

**Beads IDs:**

- Epic: `bd-gg5`
- Story: `bd-gg5.1`

**Quick Commands:**

- View tasks: `bd list --parent bd-gg5.1`
- Find ready work: `bd ready --parent bd-gg5.1`
- Mark task done: `bd close <task_id>`

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- CRITICAL: EXISTING CONTENT                                                  -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- `website/docs/architecture/index.md` ALREADY EXISTS (118 lines).            -->
<!-- This story ENHANCES existing content with Epic 2 research integration.      -->
<!-- DO NOT overwrite - review existing content first, then add missing sections.-->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

## Story

As a **developer evaluating beads**,
I want **comprehensive architecture documentation**,
So that **I understand how Git, JSONL, and SQLite work together, including recovery procedures**.

## Acceptance Criteria

1. **Given** basic architecture documentation exists at `website/docs/architecture/index.md`
   **When** I enhance the document with Epic 2 research findings
   **Then** document includes expanded sync mechanism details (import-only, force-rebuild modes)

2. **And** explains the distinction: Git = historical truth, JSONL = operational truth (rebuild source)

3. **And** daemon section includes `--no-daemon` flag usage for CI/containers

4. **And** daemon section warns about race conditions in multi-clone scenarios

5. **And** includes Universal Recovery Sequence from Epic 2 research

6. **And** contains `:::danger` warning about `bd doctor --fix` (per project-context.md Priority 0 rule)

7. **And** cross-references recovery documentation with pattern IDs where relevant

8. **And** follows Diátaxis Explanation category (understanding-oriented)

9. **And** may exceed 2000 words (NFR7 exemption for Explanation docs)

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- The checkboxes below are for reference only. Use bd commands to update.    -->
<!-- To view current status: bd list --parent bd-gg5.1                          -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

- [x] **Task 1: Review Existing Architecture Document** (AC: all) `bd-gg5.1.1`
  - [x] 1.1 Read `website/docs/architecture/index.md` (118 lines exist)
  - [x] 1.2 Identify gaps vs. acceptance criteria
  - [x] 1.3 Plan enhancement sections (don't duplicate existing content)

- [x] **Task 2: Enhance Sync Mechanism Section** (AC: #1, #5) `bd-gg5.1.2`
  - [x] 2.1 Add `--import-only` mode explanation (rebuild from JSONL)
  - [x] 2.2 Add `--force-rebuild` mode explanation
  - [x] 2.3 Add multi-machine sync considerations
  - [x] 2.4 Add Universal Recovery Sequence

- [x] **Task 3: Clarify Source of Truth Distinction** (AC: #2) `bd-gg5.1.3`
  - [x] 3.1 Clarify: Git = historical source of truth (commits preserve history)
  - [x] 3.2 Clarify: JSONL = operational source of truth (SQLite rebuilds from this)
  - [x] 3.3 Update existing "Recovery Model" section with this distinction

- [x] **Task 4: Enhance Daemon Section** (AC: #3, #4) `bd-gg5.1.4`
  - [x] 4.1 Add `--no-daemon` flag section for CI/containers/single-use scenarios
  - [x] 4.2 Add race condition warning for multi-clone workflows (Pattern B2)
  - [x] 4.3 Reference when daemon is NOT recommended

- [x] **Task 5: Add Critical Safety Warning** (AC: #6) **[PRIORITY 0]** `bd-gg5.1.5`
  - [x] 5.1 Add `:::danger` admonition about `bd doctor --fix`

- [x] **Task 6: Add Recovery Cross-References** (AC: #7) `bd-gg5.1.6`
  - [x] 6.1 Link to `/recovery` docs from relevant sections
  - [x] 6.2 Add cross-references to sync-failures and database-corruption recovery docs
  - [x] 6.3 Add Related Documentation section with links

- [x] **Task 7: Validation and Final Review** (AC: all) `bd-gg5.1.7`
  - [x] 7.1 Verify all acceptance criteria are met
  - [x] 7.2 Check document follows Diátaxis Explanation style
  - [x] 7.3 Validate internal links work
  - [x] 7.4 Run `npm run build` to verify site builds (SUCCESS)
  - [x] 7.5 Run `./scripts/generate-llms-full.sh` to update llms-full.txt

## Dev Notes

### CRITICAL: Existing Content Warning

**`website/docs/architecture/index.md` ALREADY EXISTS with substantive content:**

| Section | Status | Lines |
|---------|--------|-------|
| Three-layer intro | Complete | 9-22 |
| Layer 1: Git | Complete | 23-35 |
| Layer 2: JSONL | Complete | 37-48 |
| Layer 3: SQLite | Complete | 50-59 |
| Data Flow (basic) | Partial | 61-83 |
| Daemon (basic) | Partial | 85-97 |
| Recovery Model | Needs enhancement | 99-107 |
| Design Decisions | Complete | 109-122 |

**DO NOT recreate existing sections. ENHANCE with missing details.**

### Specific Gaps to Fill

From validation analysis, the existing doc is missing:

1. **Sync modes** - `--import-only` and `--force-rebuild` not documented
2. **`--no-daemon` flag** - Not mentioned (critical for CI/containers)
3. **Race condition warnings** - Multi-clone daemon issues not covered
4. **`bd doctor --fix` danger** - No warning (Priority 0 per project-context.md)
5. **JSONL vs Git distinction** - Current doc says "Git is truth" but operationally JSONL is recovery source
6. **Pattern cross-references** - No links to Epic 2 research patterns

### Epic 2 Research Integration

From `_bmad-output/research/recovery-framework-design.md`:

**Universal Recovery Sequence (resolves 70%+ of issues):**
```bash
bd daemons killall           # Stop daemons (prevents race conditions)
git worktree prune           # Clean orphaned worktrees
rm .beads/beads.db*          # Remove potentially corrupted database
bd sync --import-only        # Rebuild from JSONL source of truth
```

**Key Patterns to Reference:**
- A5/C3: Multi-machine sync data loss
- B2: Daemon race conditions
- D4: `bd doctor --fix` damage

### bd doctor --fix Rule (Priority 0)

Per `project-context.md` lines 308-331:

> **DANGER: Never Use `bd doctor --fix`**
> - Automated fixes delete "circular" dependencies that are actually valid
> - False positive detection removes legitimate parent-child relationships
> - Recovery after `--fix` is harder than recovery from original issue

**Safe alternatives:**
- `bd doctor` (diagnostic only)
- `bd blocked` (check blocked issues)
- `bd show <issue-id>` (inspect specific issue)

### Anti-Patterns to Avoid

Per PRD anti-marketing rule:
- **NO** promotional language ("powerful", "amazing")
- **NO** marketing claims about performance
- **YES** factual explanations of design decisions
- **YES** honest discussion of tradeoffs and failure modes

### References

- [Existing doc: website/docs/architecture/index.md] - Current content baseline
- [Source: _bmad-output/research/recovery-framework-design.md] - Epic 2 patterns
- [Source: _bmad-output/project-context.md] - Priority 0 bd doctor rule
- [Source: _bmad-output/architecture.md#Diataxis] - Content style guidelines

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

- No blocking issues encountered during implementation.

### Completion Notes List

- Enhanced existing architecture document (was 118 lines, now 242 lines)
- Added `:::info Two Sources of Truth` block explaining Git vs JSONL distinction
- Added Sync Modes section with `--import-only` and `--force-rebuild` explanations
- Added Multi-Machine Sync Considerations with best practices
- Added `--no-daemon` section for CI/containers with use cases
- Added `:::warning Race Conditions` block for multi-clone scenarios
- Added Universal Recovery Sequence (70%+ issue resolution)
- Added `:::danger Never Use bd doctor --fix` warning (Priority 0)
- Added Related Documentation section with cross-references
- Fixed broken links (anchors to existing recovery pages)
- Regenerated llms-full.txt (92KB, 5240 lines)
- Website build successful with no broken link errors

### File List

- `website/docs/architecture/index.md` - Enhanced from 118 to 242 lines
- `website/static/llms-full.txt` - Regenerated

## Change Log

| Date | Change | Author |
|------|--------|--------|
| 2025-12-30 | Story created by create-story workflow | SM Agent (Claude Opus 4.5) |
| 2025-12-30 | **REVALIDATED**: Fixed AC #1 precondition (doc exists), added bd doctor warning (P0), clarified tasks as enhancement not creation, streamlined Dev Notes | SM Agent (Validation) |
| 2025-12-30 | **IMPLEMENTED**: All 7 tasks completed. Enhanced architecture doc with sync modes, source of truth distinction, daemon warnings, Universal Recovery Sequence, bd doctor --fix danger warning, and recovery cross-references. Build successful. | Dev Agent (Claude Opus 4.5) |

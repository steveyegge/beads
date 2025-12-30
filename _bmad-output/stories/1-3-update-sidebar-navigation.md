# Story 1.3: Update Sidebar Navigation

Status: closed

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- Beads is the source of truth for task status. Update via bd commands. -->

**Beads IDs:**

- Epic: `bd-fyy`
- Story: `bd-fyy.3`

**Quick Commands:**

- View tasks: `bd list --parent bd-fyy.3`
- Find ready work: `bd ready --parent bd-fyy.3`
- Mark task done: `bd close <task_id>`

## Story

As a **documentation reader**,
I want **recovery and architecture sections in navigation**,
So that **I can find troubleshooting and architectural information easily**.

## Acceptance Criteria

1. **Given** sidebars.ts has current navigation structure **When** I add recovery/ and architecture/ categories **Then** sidebar shows "Recovery" section under How-to category
2. sidebar shows "Architecture" section under Explanation category
3. navigation follows Diataxis category rules
4. build succeeds with updated sidebar

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- The checkboxes below are for reference only. Use bd commands to update.    -->
<!-- To view current status: bd list --parent bd-fyy.3                          -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

- [x] Task 1: Analyze current sidebar structure (AC: 1, 3)
  - [x] Read website/sidebars.ts to understand current structure
  - [x] Identify appropriate locations for Recovery and Architecture sections
  - [x] Review Diataxis category mappings in existing sidebar

- [x] Task 2: Add Recovery section to sidebar (AC: 1, 3)
  - [x] Add "Recovery" category under "How-to" parent category
  - [x] Configure placeholder items for future recovery docs (index, database-corruption, merge-conflicts, circular-dependencies, sync-failures)
  - [x] Ensure correct Diataxis classification (How-to = task-oriented)

- [x] Task 3: Add Architecture section to sidebar (AC: 2, 3)
  - [x] Add "Architecture" category under "Explanation" parent category
  - [x] Configure placeholder item for future architecture doc (index)
  - [x] Ensure correct Diataxis classification (Explanation = understanding-oriented)

- [x] Task 4: Create placeholder documentation files (AC: 1, 2, 4)
  - [x] Create docs/recovery/index.md with placeholder content
  - [x] Create docs/architecture/index.md with placeholder content
  - [x] Ensure files have proper frontmatter for Docusaurus

- [x] Task 5: Validate build (AC: 4)
  - [x] Run npm run build in website directory
  - [x] Verify no build errors
  - [x] Verify sidebar renders correctly

## Dev Notes

### Architecture References

From `_bmad-output/architecture.md`:
- sidebars.ts needs recovery/ and architecture/ categories
- Recovery docs belong under How-to (task-oriented, Diataxis)
- Architecture docs belong under Explanation (understanding-oriented, Diataxis)
- Kebab-case file naming for all documentation files

### Project Structure Notes

- Website location: `website/`
- Sidebar config: `website/sidebars.ts`
- Docs location: `website/docs/`
- Recovery docs target: `website/docs/recovery/`
- Architecture docs target: `website/docs/architecture/`

### Diataxis Category Rules

| Category | Purpose | Doc Types |
|----------|---------|-----------|
| Tutorial | Learning-oriented | Step-by-step lessons |
| How-to | Task-oriented | Problem-solving guides, Recovery runbooks |
| Reference | Information-oriented | API docs, CLI reference |
| Explanation | Understanding-oriented | Architecture, Design decisions |

### References

- [Source: _bmad-output/architecture.md#Files to Extend]
- [Source: _bmad-output/epics.md#Story 1.3]
- [Source: _bmad-output/architecture.md#Implementation Patterns]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

- Build succeeded with warnings (expected: `onBrokenLinks: 'warn'` configuration)
- Webpack cache warning is unrelated to our changes (node_modules issue)

### Completion Notes List

- Added Architecture section after Core Concepts (Explanation category - understanding-oriented)
- Added Recovery section after Workflows (How-to category - task-oriented)
- Created comprehensive placeholder docs for all 5 recovery runbooks:
  - `recovery/index.md` - Overview with symptom-to-runbook mapping
  - `recovery/database-corruption.md` - SQLite recovery procedures
  - `recovery/merge-conflicts.md` - JSONL merge conflict resolution
  - `recovery/circular-dependencies.md` - Dependency cycle breaking
  - `recovery/sync-failures.md` - bd sync troubleshooting
- Created `architecture/index.md` with three-layer architecture explanation
- All files follow Docusaurus frontmatter conventions
- Build verified successful with all pages generated

### File List

**Modified:**
- `website/sidebars.ts` - Added Architecture and Recovery categories

**Created:**
- `website/docs/architecture/index.md` - Architecture overview page
- `website/docs/recovery/index.md` - Recovery overview page
- `website/docs/recovery/database-corruption.md` - Database recovery runbook
- `website/docs/recovery/merge-conflicts.md` - Merge conflicts runbook
- `website/docs/recovery/circular-dependencies.md` - Circular deps runbook
- `website/docs/recovery/sync-failures.md` - Sync failures runbook

## Change Log

| Date | Change |
|------|--------|
| 2025-12-30 | Story file created from epic definition |
| 2025-12-30 | Implemented all tasks: sidebar config + 7 new doc files |
| 2025-12-30 | Build validated, all acceptance criteria met |

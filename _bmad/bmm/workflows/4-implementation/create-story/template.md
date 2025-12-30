# Story {{epic_num}}.{{story_num}}: {{story_title}}

Status: ready-for-dev

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- Beads is the source of truth for task status. Update via bd commands. -->

**Beads IDs:**

- Epic: `{{beads_epic_id}}`
- Story: `{{beads_story_id}}`

**Quick Commands:**

- View tasks: `_bmad/bin/bd list --parent {{beads_story_id}}`
- Find ready work: `_bmad/bin/bd ready --parent {{beads_story_id}}`
- Mark task done: `_bmad/bin/bd close <task_id>`

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a {{role}},
I want {{action}},
so that {{benefit}}.

## Acceptance Criteria

1. [Add acceptance criteria from epics/PRD]

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- The checkboxes below are for reference only. Use bd commands to update.    -->
<!-- To view current status: _bmad/bin/bd list --parent {{beads_story_id}}      -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

- [ ] Task 1 (AC: #) `{{beads_task_id_1}}`
  - [ ] Subtask 1.1 `{{beads_subtask_id_1_1}}`
- [ ] Task 2 (AC: #) `{{beads_task_id_2}}`
  - [ ] Subtask 2.1 `{{beads_subtask_id_2_1}}`

## Dev Notes

- Relevant architecture patterns and constraints
- Source tree components to touch
- Testing standards summary

### Project Structure Notes

- Alignment with unified project structure (paths, modules, naming)
- Detected conflicts or variances (with rationale)

### References

- Cite all technical details with source paths and sections, e.g. [Source: docs/<file>.md#Section]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

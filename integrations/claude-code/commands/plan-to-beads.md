# Convert Plan to Beads Tasks

## description:
Convert an approved Claude Code plan into beads tasks for cross-session tracking.

## Arguments
$ARGUMENTS (optional - path to plan file, defaults to most recent)

---

## Instructions

### 1. Find the Plan File

```bash
# If argument provided, use it
# Otherwise find most recent plan
ls -t ~/.claude/plans/*.md | head -1
```

### 2. Parse the Plan Structure

Claude Code plans follow this format:

```markdown
# Plan: [Title]           → Epic title

## Summary                 → Epic description
[Brief description]

## Implementation Steps/Plan

### Phase 1: [Name]        → Task 1
[Details]

### Phase 2: [Name]        → Task 2 (depends on Task 1)
[Details]

### 3. [Name]              → Also valid phase format
[Details]

## Files to Modify         → Context (include in epic description)
```

**Extraction rules:**
- Title: First `# Plan:` or `#` heading
- Description: Content under `## Summary`
- Tasks: Each `### Phase N:` or `### N.` becomes a task
- File list: Include count in epic description

### 3. Create the Epic

```bash
bd create "[Plan Title]" \
  -t epic \
  -p 1 \
  -d "[Summary paragraph]. Files: [N] to modify, [M] to create." \
  --json
```

Save the epic ID.

### 4. Create Tasks from Phases

For each phase/step:

```bash
bd create "[Phase title without number]" \
  -d "[First paragraph of phase content]" \
  -t task \
  -p 2 \
  --json
```

### 5. Add Sequential Dependencies

Phases are sequential by default:
```bash
bd dep add <phase2-id> <phase1-id>
bd dep add <phase3-id> <phase2-id>
# ... etc
```

### 6. Link Tasks to Epic

Add all tasks as dependencies of the epic (epic is done when all tasks done):
```bash
bd dep add <epic-id> <task1-id>
bd dep add <epic-id> <task2-id>
# ... etc
```

### 7. Output Summary

```
Created from: [plan filename]

Epic: [title] ([epic-id])
  ├── [Phase 1 title] ([id]) - ready
  ├── [Phase 2 title] ([id]) - blocked by [prev]
  ├── [Phase 3 title] ([id]) - blocked by [prev]
  └── ...

Total: [N] tasks
Run `bd ready` to start working.
```

---

## Example Conversion

**Input plan:** `peaceful-munching-spark.md`
```markdown
# Plan: Standardize ID Generation with Prefixed ULIDs

## Summary
Replace inconsistent ID generation with prefixed ULIDs...

### 1. Add dependency
...

### 2. Create centralized ID utility
...
```

**Output:**
```
Created from: peaceful-munching-spark.md

Epic: Standardize ID Generation with Prefixed ULIDs (.proj-abc)
  ├── Add dependency (.proj-def) - ready
  ├── Create centralized ID utility (.proj-ghi) - blocked by .proj-def
  ├── Update strategy config schema (.proj-jkl) - blocked by .proj-ghi
  └── ... (8 total tasks)

Run `bd ready` to start working.
```

---

## Notes

- Sequential phases get automatic dependencies
- Epic depends on all tasks (closes when all done)
- Original plan file preserved for reference
- Task descriptions use first paragraph only (keeps them scannable)

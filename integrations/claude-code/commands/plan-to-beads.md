# Convert Plan to Beads Tasks

## description:
Convert a Claude Code plan file into beads epic + tasks for cross-session tracking.

## Arguments
$ARGUMENTS (optional - path to plan file, defaults to most recent in ~/.claude/plans/)

---

Use the **Task tool** with `subagent_type='general-purpose'` to convert the plan.

## Agent Instructions

The agent should:

1. **Find the plan file**
   - If argument provided, use that path
   - Otherwise: `ls -t ~/.claude/plans/*.md | head -1`

2. **Parse the plan structure**
   - Title: First `# Plan:` or `#` heading
   - Description: Content under `## Summary`
   - Tasks: Each `### Phase N:` or `### N.` section
   - File list: Include in epic description

3. **Create the epic**
   ```bash
   bd create "[Plan Title]" -t epic -p 1 -d "[summary]. Files: N to modify." --json
   ```

4. **Create tasks from phases**
   - Each phase becomes a task
   - Use first paragraph of phase content as description
   ```bash
   bd create "[Phase title]" -t task -p 2 -d "[description]" --json
   ```

5. **Add sequential dependencies**
   - Phases are sequential: `bd dep add <phase2> <phase1>`

6. **Link tasks to epic**
   - `bd dep add <epic> <task>` for each task

7. **Return a concise summary** (not raw output):
   ```
   Created from: [filename]

   Epic: [title] ([epic-id])
     ├── [Phase 1] ([id]) - ready
     ├── [Phase 2] ([id]) - blocked by [prev]
     └── [Phase 3] ([id]) - blocked by [prev]

   Total: [N] tasks
   Run `bd ready` to start.
   ```

## Notes

- Original plan file is preserved for reference
- Task descriptions use first paragraph only (keeps them scannable)
- Sequential phases get automatic dependencies

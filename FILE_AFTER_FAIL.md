# The "Fail then File" Principle

## Core Philosophy

When working on tasks, **file bugs immediately when you encounter failures**. This creates institutional memory and ensures nothing is lost.

## The Workflow

1. **<FAIL>** - You encounter an issue, error, bug, hindrance, failure, or mistake
2. **<FILE>** - Immediately create a tracking bug and assign it to an epic

## Bug Assignment Priority

1. Your current epic (if working on one)
2. Another existing relevant epic
3. The "Untracked Work" epic (create if needed)

**DO NOT** create new epics for individual bugs.

## For Crew Members

Your primary responsibility is the epic hooked to you. To complete your task, the epic must be:
- Researched
- Designed
- Implemented
- Tested
- Integrated

Work through your epic using the "Fail then File" principle as your **primum mobile**.

## Tips

- Peek at your polecats while they're running - valuable FAILs can be FILEd from their output
- Many tasks will be added to your epic as you work - spawn polecats to complete them
- **Failures are information. Untracked failures are lost knowledge.**

## Quick Reference

```bash
# File a bug
bd create -t bug "Brief description" -d "Details..." --parent <epic-id>

# File with rig routing
bd create -t bug --rig gastown "Description" -d "Details..."
```

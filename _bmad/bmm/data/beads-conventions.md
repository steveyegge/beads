# BMAD Beads Conventions

This document defines the canonical conventions for integrating Beads issue tracking with BMAD workflows. **Beads is required** for BMAD task tracking—all agents must follow these conventions.

## CLI Invocation

Use `bd` from system PATH:

```bash
# Use system bd (installed via go install or package manager)
bd <command>
```

## Issue Hierarchy

BMAD maps its work breakdown structure to Beads hierarchical issues:

| BMAD Concept       | Beads Issue Type | Parent | Example ID        |
| ------------------ | ---------------- | ------ | ----------------- |
| **Epic**           | `epic`           | None   | `proj-a3f8`       |
| **Story**          | `task`           | Epic   | `proj-a3f8.1`     |
| **Task**           | `task`           | Story  | `proj-a3f8.1.1`   |
| **Subtask**        | `task`           | Task   | `proj-a3f8.1.1.1` |
| **Review Finding** | `bug` or `task`  | Story  | `proj-a3f8.1.2`   |

### Creating Hierarchical Issues

```bash
# Create epic
bd create "Epic: User Authentication" --type epic

# Create story under epic (note: --parent uses the epic ID)
bd create "Implement login flow" --parent proj-a3f8 --type task

# Create task under story
bd create "Create login form component" --parent proj-a3f8.1 --type task

# Create subtask under task
bd create "Add form validation" --parent proj-a3f8.1.1 --type task
```

## Labels (Stage Tracking)

Beads has limited built-in statuses (`open`, `in_progress`, `blocked`, `closed`). BMAD uses **labels** to represent workflow stages:

| BMAD Stage    | Beads Label                | Beads Status  |
| ------------- | -------------------------- | ------------- |
| Backlog       | `bmad:stage:backlog`       | `open`        |
| Ready for Dev | `bmad:stage:ready-for-dev` | `open`        |
| In Progress   | `bmad:stage:in-progress`   | `in_progress` |
| Review        | `bmad:stage:review`        | `in_progress` |
| Done          | `bmad:stage:done`          | `closed`      |

### Applying Labels

```bash
# Set stage when creating
bd create "My Story" --label "bmad:stage:backlog"

# Update stage
bd label add <issue-id> "bmad:stage:ready-for-dev"
bd label remove <issue-id> "bmad:stage:backlog"
```

### Additional Labels

| Label                  | Purpose                       |
| ---------------------- | ----------------------------- |
| `bmad:story`           | Marks issue as a BMAD story   |
| `bmad:task`            | Marks issue as a BMAD task    |
| `bmad:subtask`         | Marks issue as a BMAD subtask |
| `bmad:review-finding`  | Code review finding           |
| `bmad:severity:high`   | High severity finding         |
| `bmad:severity:medium` | Medium severity finding       |
| `bmad:severity:low`    | Low severity finding          |

## Dependencies (Task Ordering & Blocking)

### Sequential Task Execution

BMAD enforces "implement tasks in order." Use `blocks` dependencies:

```bash
# Task 2 is blocked by Task 1 (Task 1 must complete first)
bd dep add proj-a3f8.1.2 proj-a3f8.1.1 --type blocks

# Result: bd ready won't show Task 2 until Task 1 is closed
```

### Review Blockers (Option A)

High/medium code review findings **block** story completion:

```bash
# Create review finding under story
bd create "Fix SQL injection vulnerability" \
  --parent proj-a3f8.1 \
  --type bug \
  --label "bmad:review-finding" \
  --label "bmad:severity:high"

# Block story completion until finding is resolved
bd dep add proj-a3f8.1 proj-a3f8.1.2 --type blocks

# Result: Story cannot be closed until the review finding is closed
```

### Discovered Work

When implementing a task, if you discover additional work needed:

```bash
bd create "Discovered: Need to update database schema" \
  --parent proj-a3f8.1 \
  --deps discovered-from:proj-a3f8.1.1 \
  --label "bmad:task"
```

## Title Conventions

Use consistent title formats for clarity:

| Type           | Title Format                      | Example                       |
| -------------- | --------------------------------- | ----------------------------- |
| Epic           | `Epic: <description>`             | `Epic: User Authentication`   |
| Story          | `<description>`                   | `Implement login flow`        |
| Task           | `<action verb> <object>`          | `Create login form component` |
| Subtask        | `<action verb> <object>`          | `Add form validation`         |
| Review Finding | `Fix: <issue>` or `<description>` | `Fix: SQL injection in login` |

## Common Commands

### Work Discovery (for Dev Agent)

```bash
# Find next ready work item (no blocking dependencies)
bd ready --json --label "bmad:stage:ready-for-dev"

# Find all ready stories
bd ready --json | jq '.[] | select(.labels | contains(["bmad:story"]))'
```

### Claiming Work

```bash
# Mark as in progress
bd update <issue-id> --status in_progress
bd label add <issue-id> "bmad:stage:in-progress"
bd label remove <issue-id> "bmad:stage:ready-for-dev"
```

### Completing Work

```bash
# Close a task/story
bd close <issue-id>

# Move story to review stage (don't close yet)
bd label add <issue-id> "bmad:stage:review"
bd label remove <issue-id> "bmad:stage:in-progress"
```

### Viewing Issue Details

```bash
# Show issue with dependencies
bd show <issue-id> --json

# List all issues for an epic
bd list --parent <epic-id> --json

# List blockers for a story
bd dep list <story-id> --type blocks --json
```

## Linking Beads to BMAD Documents

Story files should include Beads metadata in YAML frontmatter:

```yaml
---
beads_epic_id: proj-a3f8
beads_story_id: proj-a3f8.1
---
```

Or in a dedicated section:

```markdown
## Beads Tracking

- **Epic ID**: `proj-a3f8`
- **Story ID**: `proj-a3f8.1`
- **Tasks**: See `bd list --parent proj-a3f8.1`
```

## Workflow Integration Points

### Scrum Master (SM) Agent

- Creates epic/story issues during `sprint-planning`
- Sets up sequential story blockers
- Applies `bmad:stage:ready-for-dev` when story is refined

### Developer (Dev) Agent

- Starts from `bd ready --label "bmad:stage:ready-for-dev"`
- Claims work with `bd update ... --status in_progress`
- Creates task/subtask children before implementation
- Closes task children as completed
- Files discovered work with `--deps discovered-from:...`
- Moves story to `bmad:stage:review` when all tasks done

### Code Review

- Creates review finding issues as story children
- Adds `blocks` dependencies for high/medium findings
- Story remains blocked until findings resolved

## Persistence

Beads auto-syncs to `.beads/issues.jsonl` (committed to git). The JSONL file is the source of truth for sharing across sessions and team members.

```bash
# Explicit sync (usually not needed - auto-sync is on)
bd sync

# Force export
bd export
```

## Preflight Check

Every BMAD workflow that uses tracking should include this preflight:

```bash
# Verify bd is available
if ! bd version > /dev/null 2>&1; then
  echo "ERROR: Beads CLI not available. Install with: go install github.com/steveyegge/beads/bd@latest"
  exit 1
fi

# Verify beads is initialized
if [ ! -d ".beads" ]; then
  echo "ERROR: Beads not initialized. Run: bd init --quiet"
  exit 1
fi
```

---

**Remember**: Beads is the operational source of truth for task status. BMAD documents remain the rich specification layer.

---
name: beads-initializer
description: |
  Transforms vague goals into structured backlogs with epic and child task
  hierarchies. Guides through goal capture, task decomposition, dependency
  establishment, and git sync. Use for any new feature or project that spans
  multiple sessions. Trigger with "create backlog", "plan feature", "break down",
  "initialize epic", "structure this work", or "create task hierarchy".
allowed-tools: "Read,Bash(bd:*),Bash(git:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Initializer Skill

> **STATUS: ACTIVE - This skill creates epic + backlog structures**
> Transforms vague goals into structured domain memory.

## Purpose

The initializer skill implements the **Initializer Role** from the Beads Agent Harness pattern:

> Transform vague goal into structured domain memory
> - High-level user vision -> Beads epic with child task issues
> - All issues initially status=open
> - Dependency graph (blocks relationships)

This is distinct from `beads-init-app` which bootstraps a new repository.
This skill creates backlog structures for **any** feature or project.

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
./scripts/beads-log-event.sh sk.initializer.activated 2>/dev/null || true

# Or PowerShell
.\scripts\beads-log-event.ps1 -EventCode sk.initializer.activated 2>$null
```

Then output:

```
═══════════════════════════════════════════════════════════════
SKILL ACTIVATED: beads-initializer
PURPOSE: Transform vague goals into structured backlogs
STATUS: Ready to create epic + child task structure
═══════════════════════════════════════════════════════════════

This skill will guide you through:

1. CAPTURE - Define the high-level goal
2. DECOMPOSE - Break into discrete tasks
3. STRUCTURE - Create epic with children
4. CONNECT - Establish blocking dependencies
5. COMMIT - Sync to git

Provide a high-level goal to initialize:
> Example: "User authentication with OAuth"
> Example: "Migrate database to PostgreSQL"
> Example: "Add dark mode to application"
```

---

## Ceremony Steps

### Step 1: Capture the Goal

Ask the user for:
- **Goal title**: Brief epic name (e.g., "User Authentication System")
- **Goal description**: What success looks like (2-3 sentences)
- **Priority**: 0-4 (default: 1)
- **Type**: Usually `epic`, but could be `feature` for smaller scope

**Prompt**:
```
What is the high-level goal?

Title: ____________________________
Description: ______________________
Priority (0-4, default 1): ________
```

### Step 2: Decompose into Tasks

Guide the user through task breakdown:

**Prompt**:
```
Let's break this into discrete tasks.

For each task, we need:
- Title (action-oriented: "Add X", "Create Y", "Fix Z")
- Brief description (what does "done" mean?)
- Dependencies (what must complete first?)

Task 1: ____________________________
Task 2: ____________________________
Task 3: ____________________________
...

(Enter blank line when done)
```

**Guidelines for decomposition**:
- Each task should be completable in a single session
- Tasks should have clear "done" criteria
- Prefer 3-10 tasks per epic
- Identify natural ordering/dependencies

### Step 3: Review Before Creation

Present the planned structure:

```
═══════════════════════════════════════════════════════════════
REVIEW: Planned Epic Structure
═══════════════════════════════════════════════════════════════

EPIC: <title> [P<priority>]
  <description>

TASKS:
  1. <task-1-title>
     └── Blocked by: (none - ready to start)

  2. <task-2-title>
     └── Blocked by: Task 1

  3. <task-3-title>
     └── Blocked by: Task 2

═══════════════════════════════════════════════════════════════

Proceed with creation? (yes/no/edit)
```

### Step 4: Create Epic Structure

After user confirmation, execute:

```bash
# Create the epic
EPIC_ID=$(bd create "<epic-title>" -t epic -p <priority> --description "<description>" --json | jq -r '.id')
echo "Created epic: $EPIC_ID"
./scripts/beads-log-event.sh bd.issue.create $EPIC_ID "epic"

# Create child tasks (repeat for each task)
TASK_N_ID=$(bd create "<task-n-title>" -t task -p <priority> --description "<description>" --json | jq -r '.id')
bd dep add $TASK_N_ID $EPIC_ID --type parent-child
./scripts/beads-log-event.sh bd.issue.create $TASK_N_ID "child of $EPIC_ID"
echo "Created task: $TASK_N_ID"
```

### Step 5: Establish Blocking Dependencies

For tasks with ordering constraints:

```bash
# If task 2 depends on task 1
bd dep add $TASK_2_ID $TASK_1_ID --type blocks
./scripts/beads-log-event.sh bd.dep.add "$TASK_2_ID blocks $TASK_1_ID"

# Verify dependency graph
bd blocked
```

### Step 6: Sync and Report

```bash
# Sync to git
bd sync

# Show the created structure
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "INITIALIZER COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo ""

# List the epic and children
bd show $EPIC_ID

# Show what's ready to work on
echo ""
echo "Ready to work on:"
bd ready

./scripts/beads-log-event.sh sk.initializer.complete $EPIC_ID "structure created"
```

---

## Example Session

**User Input**:
```
Goal: Add user authentication with OAuth support

Tasks:
1. Research OAuth providers (Google, GitHub, Microsoft)
2. Create User model and database schema
3. Implement OAuth callback endpoints
4. Add session management middleware
5. Create login/logout UI components
6. Write integration tests
```

**Dependencies Identified**:
- Task 2 (User model) blocks Task 3 (OAuth endpoints)
- Task 3 (OAuth endpoints) blocks Task 4 (Session middleware)
- Task 4 (Session middleware) blocks Task 5 (UI components)
- Task 5 (UI components) blocks Task 6 (Integration tests)
- Task 1 (Research) can proceed in parallel

**Created Structure**:
```
beads-xyz: OAuth Authentication [P1] [epic]
├── beads-abc: Research OAuth providers [P1] [task] - READY
├── beads-def: Create User model [P1] [task] - READY
├── beads-ghi: Implement OAuth callbacks [P1] [task] - blocked by def
├── beads-jkl: Add session middleware [P1] [task] - blocked by ghi
├── beads-mno: Create login/logout UI [P2] [task] - blocked by jkl
└── beads-pqr: Write integration tests [P2] [task] - blocked by mno
```

**Ready Queue**:
```
beads-abc [P1] [task] open - Research OAuth providers
beads-def [P1] [task] open - Create User model
```

---

## Parallel Task Creation

For efficiency, when creating multiple independent tasks:

```bash
# Create tasks in parallel using subshells
(
  bd create "Task 1" -t task -p 1 --json &
  bd create "Task 2" -t task -p 1 --json &
  bd create "Task 3" -t task -p 1 --json &
  wait
)

# Or use bd create with --batch flag if available
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.initializer.activated` | Skill loads | Always |
| `bd.issue.create` | Each issue created | Issue ID, type |
| `bd.dep.add` | Each dependency added | Parent → child |
| `sk.initializer.complete` | Structure finished | Epic ID |

---

## Integration with Bootup

After initialization completes, the bootup skill will:
1. Show the new epic's children in `bd ready`
2. Allow the agent to pick and start work immediately
3. Track progress through the dependency graph

---

## When to Use This Skill

**Use beads-initializer when**:
- Starting a new feature/project that spans multiple sessions
- User provides a high-level goal that needs task breakdown
- Work has natural phases or dependencies
- Multiple people may work on different parts

**Use simpler bd create when**:
- Creating a single task
- Adding a discovered task during work
- The structure is already established

---

**STATUS:** This skill actively creates beads structures.
It transforms "I want X" into "Here's exactly how we'll build X."

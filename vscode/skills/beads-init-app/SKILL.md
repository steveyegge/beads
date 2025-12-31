---
name: beads-init-app
description: |
  Orchestrates the Application Initialization Ceremony for new beads-first repos.
  Creates the InitApp epic and all child tasks that must complete before other
  work can proceed. Establishes the Epoch - the foundational commit from which
  all work flows. Trigger with "initialize application", "init app", "bootstrap repo",
  "beads-first setup", or "create epoch".
allowed-tools: "Read,Bash(bd:*),Bash(git:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Init-App Skill

> **STATUS: ACTIVE - This skill creates the InitApp structure**
> Unlike other skills, this one performs actual work (creating beads).

## Purpose

The init-app skill orchestrates the **Application Initialization Ceremony**.
It creates the InitApp epic and all child tasks that must be completed
before any other work can proceed.

This establishes the **Epoch** - the foundational commit from which all work flows.

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
./scripts/beads-log-event.sh sk.initapp.activated
./scripts/beads-log-event.sh ep.init.start bd-0001 "InitApp ceremony beginning"

# Or PowerShell
.\scripts\beads-log-event.ps1 -EventCode sk.initapp.activated
.\scripts\beads-log-event.ps1 -EventCode ep.init.start -IssueId bd-0001 -Details "InitApp ceremony beginning"
```

Then output:

```
═══════════════════════════════════════════════════════════════
SKILL ACTIVATED: beads-init-app
PURPOSE: Initialize a Beads-First Application
STATUS: Ready to create InitApp epic structure
═══════════════════════════════════════════════════════════════

This ceremony will create:

  bd-0001: InitApp Epic (BLOCKS ALL FUTURE WORK)
  ├── bd-0002: Initialize beads in repository
  ├── bd-0003: Install git hooks
  ├── bd-0004: Create event logging infrastructure
  ├── bd-0005: Create CLAUDE.md with beads protocol
  ├── bd-0006: Create beads-bootup skill
  ├── bd-0007: Create beads-landing skill
  ├── bd-0008: Create beads-scope skill
  ├── bd-0009: Verify all hooks log correctly
  ├── bd-0010: Verify all skills log correctly
  └── bd-0011: Create Epoch commit

After completion:
- bd ready will ONLY show InitApp children
- NO other work can proceed until InitApp closes
- Closing InitApp establishes the Epoch

Proceed with initialization? (yes/no)
```

---

## Ceremony Steps

Execute these steps IN ORDER after user confirms:

### Step 1: Initialize Beads (if not already done)
```bash
if [ ! -d ".beads" ]; then
    bd init --quiet
    ./scripts/beads-log-event.sh bd.init none "beads initialized"
fi
```

### Step 2: Create InitApp Epic
```bash
BD_INITAPP=$(bd create "InitApp - Application Initialization Epoch" -t epic -p 0 --json | jq -r '.id')
./scripts/beads-log-event.sh bd.issue.create $BD_INITAPP "InitApp epic"
echo "Created: $BD_INITAPP"
```

### Step 3: Create Child Tasks

```bash
# Task 1: Initialize beads (may already be done)
BD_0002=$(bd create "Initialize beads in repository" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0002 $BD_INITAPP --type parent-child
./scripts/beads-log-event.sh bd.issue.create $BD_0002 "init beads task"

# Task 2: Install git hooks
BD_0003=$(bd create "Install git hooks with event logging" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0003 $BD_INITAPP --type parent-child
bd dep add $BD_0003 $BD_0002 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0003 "hooks task"

# Task 3: Event logging infrastructure
BD_0004=$(bd create "Create event logging infrastructure" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0004 $BD_INITAPP --type parent-child
bd dep add $BD_0004 $BD_0003 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0004 "event logging task"

# Task 4: CLAUDE.md
BD_0005=$(bd create "Create CLAUDE.md with beads protocol" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0005 $BD_INITAPP --type parent-child
bd dep add $BD_0005 $BD_0002 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0005 "CLAUDE.md task"

# Task 5: beads-bootup skill
BD_0006=$(bd create "Create beads-bootup skill" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0006 $BD_INITAPP --type parent-child
bd dep add $BD_0006 $BD_0004 --type blocks
bd dep add $BD_0006 $BD_0005 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0006 "bootup skill task"

# Task 6: beads-landing skill
BD_0007=$(bd create "Create beads-landing skill" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0007 $BD_INITAPP --type parent-child
bd dep add $BD_0007 $BD_0004 --type blocks
bd dep add $BD_0007 $BD_0005 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0007 "landing skill task"

# Task 7: beads-scope skill
BD_0008=$(bd create "Create beads-scope skill" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0008 $BD_INITAPP --type parent-child
bd dep add $BD_0008 $BD_0004 --type blocks
bd dep add $BD_0008 $BD_0005 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0008 "scope skill task"

# Task 8: Verify hooks
BD_0009=$(bd create "Verify all hooks log correctly" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0009 $BD_INITAPP --type parent-child
bd dep add $BD_0009 $BD_0003 --type blocks
bd dep add $BD_0009 $BD_0004 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0009 "verify hooks task"

# Task 9: Verify skills
BD_0010=$(bd create "Verify all skills log correctly" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0010 $BD_INITAPP --type parent-child
bd dep add $BD_0010 $BD_0006 --type blocks
bd dep add $BD_0010 $BD_0007 --type blocks
bd dep add $BD_0010 $BD_0008 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0010 "verify skills task"

# Task 10: Epoch commit
BD_0011=$(bd create "Create Epoch commit and tag" -t task -p 0 --json | jq -r '.id')
bd dep add $BD_0011 $BD_INITAPP --type parent-child
bd dep add $BD_0011 $BD_0009 --type blocks
bd dep add $BD_0011 $BD_0010 --type blocks
./scripts/beads-log-event.sh bd.issue.create $BD_0011 "epoch commit task"
```

### Step 4: Sync and Commit Structure
```bash
bd sync
git add .beads/
git commit -m "bd-0001: Initialize beads-first application structure"
./scripts/beads-log-event.sh ep.init.structure none "InitApp structure created"
```

### Step 5: Report Status
```bash
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "INITAPP CEREMONY COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo ""
bd list --json
echo ""
echo "Dependency tree:"
bd dep tree $BD_INITAPP
echo ""
echo "Ready queue (should show first unblocked task):"
bd ready --json
echo ""
echo "NEXT STEPS:"
echo "1. Work through tasks via: bd ready"
echo "2. Close each task as completed"
echo "3. When bd-0011 is done, close InitApp"
echo "4. Tag the Epoch: git tag -a epoch-v1 -m 'Beads-first foundation'"
echo "5. Begin normal project work"
echo ""
./scripts/beads-log-event.sh ep.init.structure none "ceremony output complete"
```

---

## The Epoch Tag

When all InitApp children are closed:

```bash
# Close InitApp epic
bd close $BD_INITAPP --reason "All initialization tasks complete. Epoch established."
./scripts/beads-log-event.sh bd.issue.close $BD_INITAPP "InitApp complete"

# Create the Epoch tag
git tag -a epoch-v1 -m "Beads-first application foundation established"
./scripts/beads-log-event.sh ep.init.complete none "Epoch tagged"
./scripts/beads-log-event.sh ep.version none "epoch-v1"
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.initapp.activated` | Skill loads | Always |
| `ep.init.start` | Ceremony begins | Start |
| `bd.init` | Beads initialized | Step 1 |
| `bd.issue.create` | Each task created | Step 2-3 |
| `ep.init.structure` | Structure complete | Step 4 |
| `bd.issue.close` | InitApp closed | End |
| `ep.init.complete` | Epoch established | End |
| `ep.version` | Tag created | End |

---

## Why InitApp Blocks Everything

The InitApp epic has `priority: 0` and all future epics must declare
a `blocks` dependency on it. This means:

1. `bd ready` only shows InitApp children until InitApp closes
2. beads-bootup skill checks InitApp status and blocks other work
3. No shortcuts - the foundation must be solid

This is intentional. A beads-first application needs:
- Working event logging (observability)
- Working hooks (enforcement)
- Working skills (rituals)
- Working CLAUDE.md (protocol)

Without these, the pattern falls apart.

---

**STATUS:** This skill actively creates beads structure.
It bootstraps itself - using beads to track its own completion.

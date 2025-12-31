---
name: beads-scope
description: |
  Enforces the ONE ISSUE AT A TIME discipline. Monitors work to ensure agent
  stays focused on the selected issue and properly files discovered work instead
  of implementing it. Trigger with "scope check", "am I on track", "scope violation",
  "discovered work", "file this for later", or "stay focused".
allowed-tools: "Read,Bash(bd:*),Bash(git:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Scope Skill

> **STATUS: GREEN FIELD - LOGGING ONLY**
> This skill announces its activation but performs no processing yet.

<!--
## IMPLEMENTATION PLAN

### Phase 1: Session State Management
- [ ] Create `.beads/current-issue` file on issue selection (from bootup)
- [ ] Add `bd select <id>` command to set current issue
- [ ] Expose $BEADS_CURRENT_ISSUE via environment or file read

### Phase 2: Discovery Filing Workflow
- [ ] Implement `bd create --discovered-from=<id>` flag
- [ ] Auto-set `discovered-from` dependency when flag is used
- [ ] Add `sk.scope.discovery` event emission

### Phase 3: Scope Violation Detection (Advisory)
- [ ] Add pre-commit hook check that reads `.beads/current-issue`
- [ ] Compare changed files against issue scope (if defined in issue metadata)
- [ ] Log `sk.scope.violation` when scope mismatch detected (advisory only, non-blocking)
- [ ] Output warning to agent/user about potential scope violation

### Phase 4: Integration
- [ ] Verify bootup sets current-issue properly
- [ ] Verify landing clears/archives current-issue
- [ ] Add scope status to `bd status` output

### Dependencies
- Requires: Event logging infrastructure (beads-log-event scripts)
- Requires: beads-bootup to set current issue
- Blocks: beads-landing (scope verification at session end)

### Verification Criteria
- [ ] `bd select <id>` creates `.beads/current-issue`
- [ ] `bd create --discovered-from=<id>` adds dependency
- [ ] Events logged to `.beads/events.log`
- [ ] Pre-commit hook produces advisory warnings
-->


## Purpose

The scope skill enforces the ONE ISSUE AT A TIME discipline.
It monitors work and ensures discovered work is filed, not implemented.

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
./scripts/beads-log-event.sh sk.scope.activated

# Or PowerShell
.\scripts\beads-log-event.ps1 -EventCode sk.scope.activated
```

Then output exactly:

```
═══════════════════════════════════════════════════════════════
SKILL ACTIVATED: beads-scope
STATUS: Logging only - no processing implemented
EVENT: sk.scope.activated logged to .beads/events.log
═══════════════════════════════════════════════════════════════

Scope discipline (NOT YET ACTIVE):
- [ ] Track currently selected issue
- [ ] Monitor file changes against issue scope
- [ ] Detect scope violations
- [ ] Enforce discovered-from filing

Core principle: ONE ISSUE PER SESSION
Discovered work → FILE IT, don't implement it

NEXT: Verify event appears in .beads/events.log
```

---

## The Scope Discipline

### Rule 1: One Issue Per Session
- Bootup ritual selects ONE issue from `bd ready`
- ALL work in the session relates to that issue
- Session ends when issue is done OR time expires

### Rule 2: Discovered Work Gets Filed
When you encounter something that needs doing but isn't your current issue:

```bash
# DO NOT implement it!
# File it as a new issue with discovered-from dependency:

bd create "Discovered: <description>" -t <type> --deps discovered-from:<current-issue>
./scripts/beads-log-event.sh sk.scope.discovery <new-issue-id> "<description>"
```

The `discovered-from` dependency:
- Links the new issue to where it was found
- Preserves context for later
- Automatically inherits source_repo
- Creates audit trail

### Rule 3: Scope Violations Are Logged
If you start working on something outside your selected issue:

```bash
./scripts/beads-log-event.sh sk.scope.violation <current-issue> "worked on unrelated file"
```

---

## Processing Logic (DEFINED BUT NOT ACTIVE)

### Track Current Issue
```bash
# At session start, store selected issue
export BEADS_CURRENT_ISSUE="bd-XXXX"
echo "$BEADS_CURRENT_ISSUE" > .beads/current-issue
```

### Monitor Changes (Future: Git Hook Integration)
```bash
# In pre-commit hook, check if changes relate to current issue
# This is ADVISORY, not blocking in green field

# Get changed files
CHANGED_FILES=$(git diff --cached --name-only)

# Log for visibility
./scripts/beads-log-event.sh gd.scope.check $BEADS_CURRENT_ISSUE "checking $CHANGED_FILES"
```

### Detect Tangential Work
```bash
# If agent mentions work outside current scope:
# 1. Pause implementation
# 2. File as discovered issue
# 3. Return to original issue

# Example detection (future NLP/pattern matching):
# "While working on X, I noticed Y needs fixing"
# → Trigger: bd create "Discovered: Y" --deps discovered-from:X
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.scope.activated` | Skill loads | Always |
| `sk.scope.discovery` | Work properly filed | Future |
| `sk.scope.violation` | Worked outside scope | Future |
| `gd.scope.check` | Scope verified | Future |

---

## Why This Matters

Without scope discipline:
- Agents try to fix everything they see
- Context window fills with tangential work
- Original issue never completes
- Session ends with partial work everywhere

With scope discipline:
- One issue gets full attention
- Tangential work is captured (not lost)
- Clean commits with clear attribution
- Predictable progress

---

## Integration with Other Skills

**beads-bootup:** Sets the current issue via selection
**beads-scope:** Monitors adherence to that selection
**beads-landing:** Verifies scope was maintained, files remaining discoveries

---

**GREEN FIELD STATUS:** This skill only logs activation.
Processing will be enabled once event logging is verified working.

**MANTRA:** If it's not your issue, file it and move on.

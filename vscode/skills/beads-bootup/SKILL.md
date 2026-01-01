---
name: beads-bootup
description: |
  Executes the mandatory session start ritual. Grounds agent in current state,
  syncs with remote, orients via bd ready, and selects ONE issue to work on.
  Enforces InitApp guard - if InitApp is open, only InitApp children are workable.
  Trigger with "start session", "begin work", "bootup", "session start",
  "what should I work on", or "initialize session".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(pwd:*),Bash(mkdir:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Bootup Skill

> **STATUS: ACTIVE - FULL PROCESSING** (bd-03gn)
> This skill executes the complete 5-phase bootup ritual.

## IMPLEMENTATION PLAN

### Phase 1: Ground and Sync
- [ ] Verify working directory contains `.beads/` (exit with guidance if not)
- [ ] Execute `git pull --rebase` (handle conflicts gracefully)
- [ ] Execute `bd sync` to pull remote beads state
- [ ] Log `ss.bootup.ground` and `ss.bootup.sync` events

### Phase 2: Orient - State Assessment
- [ ] Run `bd ready --json` to get available work
- [ ] Run `bd list --status=in_progress --json` to check existing WIP
- [ ] If WIP exists, prompt: "Resume in-progress issue or pick new?"
- [ ] Display last 5 commits with `git log --oneline -5`
- [ ] Log `ss.bootup.orient` event

### Phase 3: InitApp Guard
- [ ] Check if `bd-0001` (InitApp) exists and is open
- [ ] If InitApp is open, filter `bd ready` to show ONLY InitApp children
- [ ] Display clear message: "InitApp not complete. Only InitApp work available."
- [ ] Log `gd.initapp.check` and `gd.initapp.blocked` or `gd.initapp.passed`

### Phase 4: Select - Issue Selection
- [ ] Present filtered `bd ready` list (InitApp children or all ready issues)
- [ ] Accept user selection (issue ID)
- [ ] Validate issue exists and is not blocked
- [ ] Write selected issue to `.beads/current-issue`
- [ ] Log `ss.bootup.select` event with issue ID

### Phase 5: Verify - Health Check
- [ ] Run project health check (detect from CLAUDE.md or config)
- [ ] Common checks: `go test ./...`, `npm test`, `pytest`, etc.
- [ ] Log `ss.bootup.verify` with pass/fail status
- [ ] If failed, warn but allow continuation
- [ ] Log `ss.bootup.complete` event

### Output Format
```
═══════════════════════════════════════════════════════════════
BOOTUP COMPLETE
═══════════════════════════════════════════════════════════════
Selected Issue: bd-XXXX - <title>
Status: <status>
Health Check: PASSED/FAILED
═══════════════════════════════════════════════════════════════
```

### Dependencies
- Requires: Event logging infrastructure (beads-log-event scripts)
- Requires: `bd` CLI with ready, list, show, sync commands
- Provides: `.beads/current-issue` for scope skill
- Provides: `.beads/.session-active` marker

### Verification Criteria
- [ ] Session marker `.beads/.session-active` created
- [ ] Git pull and bd sync execute successfully
- [ ] InitApp guard filters work correctly
- [ ] Selected issue written to `.beads/current-issue`
- [ ] All events logged to `.beads/events.log`


## Purpose

The bootup skill executes at the START of every coding session.
It grounds the agent in current state and selects work.

---

## Activation

When this skill is loaded, execute the **5-phase bootup ritual** below.

---

### Phase 1: Ground and Sync

```bash
# Verify we're in a beads workspace
if [ ! -d ".beads" ]; then
    echo "❌ ERROR: Not in a beads workspace (.beads/ directory not found)"
    echo "Run 'bd init' to initialize beads in this directory"
    exit 1
fi

# Create session marker
mkdir -p .beads
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > .beads/.session-active

# Sync with remote
git pull --rebase || echo "⚠️  Git pull failed - continuing with local state"
bd sync || echo "⚠️  bd sync failed - continuing with local state"
```

```powershell
# PowerShell version
if (-not (Test-Path ".beads")) {
    Write-Host "❌ ERROR: Not in a beads workspace (.beads/ directory not found)"
    Write-Host "Run 'bd init' to initialize beads in this directory"
    exit 1
}

New-Item -ItemType Directory -Force -Path .beads | Out-Null
Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ" | Out-File -FilePath .beads\.session-active -NoNewline

git pull --rebase
bd sync
```

---

### Phase 2: Orient - State Assessment

```bash
# Check for existing work-in-progress
echo "═══════════════════════════════════════════════════════════════"
echo "CURRENT STATE"
echo "═══════════════════════════════════════════════════════════════"

# Show in-progress issues
IN_PROGRESS=$(bd list --status=in_progress 2>/dev/null)
if [ -n "$IN_PROGRESS" ]; then
    echo "⚠️  Work in progress:"
    echo "$IN_PROGRESS"
    echo ""
    echo "Resume existing work or pick new? (Check with user)"
fi

# Show recent commits for context
echo ""
echo "Recent commits:"
git log --oneline -5

# Show ready work
echo ""
echo "Available work:"
bd ready
```

---

### Phase 3: InitApp Guard

```bash
# Check if InitApp (bd-0001) exists and is still open
INITAPP_STATUS=$(bd show bd-0001 --json 2>/dev/null | grep -o '"status":"[^"]*"' | cut -d'"' -f4)

if [ "$INITAPP_STATUS" = "open" ] || [ "$INITAPP_STATUS" = "in_progress" ]; then
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "⛔ INITAPP GUARD: bd-0001 is not complete"
    echo "═══════════════════════════════════════════════════════════════"
    echo "Only InitApp children (bd-0001.*) are workable until InitApp closes."
    echo ""
    echo "InitApp children:"
    bd list | grep "bd-0001\."
    echo ""
    echo "Complete InitApp work before working on other issues."
fi
```

---

### Phase 4: Issue Selection

After reviewing available work, **ask the user which issue to work on**.

When user selects an issue:

```bash
# Validate and claim the issue
ISSUE_ID="<selected-id>"  # Replace with user's choice

# Verify issue exists
bd show "$ISSUE_ID" || { echo "❌ Issue not found"; exit 1; }

# Claim the issue
bd update "$ISSUE_ID" --status in_progress

# Write to current-issue marker
echo "$ISSUE_ID" > .beads/current-issue
echo "✓ Selected issue: $ISSUE_ID"
```

---

### Phase 5: Health Check (Optional)

```bash
# Detect and run project tests
if [ -f "go.mod" ]; then
    echo "Running Go tests..."
    go test ./... && HEALTH="PASSED" || HEALTH="FAILED"
elif [ -f "package.json" ]; then
    echo "Running npm tests..."
    npm test && HEALTH="PASSED" || HEALTH="FAILED"
elif [ -f "pytest.ini" ] || [ -f "setup.py" ]; then
    echo "Running pytest..."
    pytest && HEALTH="PASSED" || HEALTH="FAILED"
else
    echo "No test framework detected - skipping health check"
    HEALTH="SKIPPED"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "BOOTUP COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo "Selected Issue: $(cat .beads/current-issue 2>/dev/null || echo 'None')"
echo "Health Check: $HEALTH"
echo "═══════════════════════════════════════════════════════════════"
```

---

## Quick Start

For agents, execute these phases in order:

1. **Ground**: Verify `.beads/` exists, create session marker
2. **Sync**: Run `git pull --rebase && bd sync`
3. **Orient**: Run `bd list --status=in_progress` then `bd ready`
4. **Guard**: Check if `bd-0001` is open (if so, only work on its children)
5. **Select**: Ask user to pick an issue, then `bd update <id> --status in_progress`
6. **Verify**: Run tests if available, report result

**Output the BOOTUP COMPLETE banner when done.**

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.bootup.activated` | Skill loads | Session marker created |
| `ss.bootup.ground` | After workspace check | Verified .beads/ exists |
| `ss.bootup.sync` | After sync | git pull + bd sync complete |
| `ss.bootup.orient` | After bd ready | State assessment complete |
| `ss.bootup.select` | Issue chosen | Issue claimed, written to current-issue |
| `ss.bootup.verify` | Health check done | Tests run (if available) |
| `ss.bootup.complete` | Ritual finished | Bootup banner displayed |
| `gd.initapp.check` | Checking epoch | InitApp status checked |
| `gd.initapp.blocked` | InitApp open | Only children workable |
| `gd.initapp.passed` | InitApp closed | All issues workable |

---

**STATUS: ACTIVE** (bd-03gn) - Full 5-phase bootup ritual implemented.

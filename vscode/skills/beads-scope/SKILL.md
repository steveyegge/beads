---
name: beads-scope
description: |
  Enforces the ONE ISSUE AT A TIME discipline. Monitors work to ensure agent
  stays focused on the selected issue and properly files discovered work instead
  of implementing it. Trigger with "scope check", "am I on track", "scope violation",
  "discovered work", "file this for later", or "stay focused".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(echo:*)"
version: "0.2.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Scope Skill

> **STATUS: ACTIVE - FULL PROCESSING** (bd-u91r)
> This skill provides scope discipline enforcement and discovery filing.

## Purpose

The scope skill enforces the **ONE ISSUE AT A TIME** discipline.
It monitors work, tracks the current issue, and ensures discovered work
is filed rather than implemented.

**CORE PRINCIPLE:** If it's not your issue, file it and move on.

---

## Activation

When this skill is loaded, execute the **scope check** below.

---

### Scope Check

```bash
# Bash - Check current scope status
./scripts/beads-log-event.sh sk.scope.activated

echo "==============================================================="
echo "SCOPE CHECK"
echo "==============================================================="

# Check for current issue
if [ -f ".beads/current-issue" ]; then
    CURRENT_ISSUE=$(cat .beads/current-issue)
    echo "Current issue: $CURRENT_ISSUE"
    echo ""
    bd show "$CURRENT_ISSUE"
else
    echo "WARNING: No current issue set"
    echo "Run bootup ritual or select an issue with:"
    echo "  bd update <issue-id> --status in_progress"
    echo "  echo '<issue-id>' > .beads/current-issue"
fi

echo ""
echo "==============================================================="
```

```powershell
# PowerShell - Check current scope status
.\scripts\beads-log-event.ps1 -EventCode sk.scope.activated

Write-Host "==============================================================="
Write-Host "SCOPE CHECK"
Write-Host "==============================================================="

if (Test-Path ".beads\current-issue") {
    $CurrentIssue = (Get-Content ".beads\current-issue" -Raw).Trim()
    Write-Host "Current issue: $CurrentIssue"
    Write-Host ""
    bd show $CurrentIssue
} else {
    Write-Warning "No current issue set"
    Write-Host "Run bootup ritual or select an issue with:"
    Write-Host "  bd update <issue-id> --status in_progress"
    Write-Host "  echo '<issue-id>' > .beads\current-issue"
}

Write-Host ""
Write-Host "==============================================================="
```

Then output:

```
Scope discipline:
- ONE issue per session: <current-issue or "Not set">
- Discovered work → FILE IT, don't implement it

Commands:
- Check scope: cat .beads/current-issue
- File discovery: bd create "Discovered: <description>" -t task
- Change issue: bd update <id> --status in_progress && echo <id> > .beads/current-issue
```

---

## The Scope Discipline

### Rule 1: One Issue Per Session

- The bootup ritual selects ONE issue from `bd ready`
- ALL work in the session relates to that issue
- Session ends when issue is done OR context exhausted

### Rule 2: Discovered Work Gets Filed

When you encounter something that needs doing but isn't your current issue:

```bash
# Bash - File discovered work
# DO NOT implement it! File it as a new issue:

CURRENT_ISSUE=$(cat .beads/current-issue 2>/dev/null)
DISCOVERED_TITLE="Discovered: <description>"
DISCOVERED_TYPE="task"  # or bug, feature, chore

bd create "$DISCOVERED_TITLE" -t "$DISCOVERED_TYPE"
./scripts/beads-log-event.sh sk.scope.discovery "$CURRENT_ISSUE" "$DISCOVERED_TITLE"

echo "Filed discovery: $DISCOVERED_TITLE"
echo "Returning to: $CURRENT_ISSUE"
```

```powershell
# PowerShell - File discovered work
$CurrentIssue = (Get-Content ".beads\current-issue" -Raw -ErrorAction SilentlyContinue)?.Trim()
$DiscoveredTitle = "Discovered: <description>"
$DiscoveredType = "task"  # or bug, feature, chore

bd create $DiscoveredTitle -t $DiscoveredType
.\scripts\beads-log-event.ps1 -EventCode sk.scope.discovery -IssueId $CurrentIssue -Description $DiscoveredTitle

Write-Host "Filed discovery: $DiscoveredTitle"
Write-Host "Returning to: $CurrentIssue"
```

### Rule 3: Scope Violations Are Advisory

If you realize you've been working on something outside your selected issue:

```bash
# Bash - Log scope violation (advisory, non-blocking)
CURRENT_ISSUE=$(cat .beads/current-issue 2>/dev/null || echo "unknown")
VIOLATION_DESC="Worked on unrelated files"

./scripts/beads-log-event.sh sk.scope.violation "$CURRENT_ISSUE" "$VIOLATION_DESC"

echo "WARNING: Scope violation detected"
echo "You were working on: $CURRENT_ISSUE"
echo "But made changes to: $VIOLATION_DESC"
echo ""
echo "Options:"
echo "1. File the tangential work as discovered issue"
echo "2. Update current issue to include this work"
echo "3. Continue (violation logged for review)"
```

```powershell
# PowerShell - Log scope violation (advisory, non-blocking)
$CurrentIssue = (Get-Content ".beads\current-issue" -Raw -ErrorAction SilentlyContinue)?.Trim()
if (-not $CurrentIssue) { $CurrentIssue = "unknown" }
$ViolationDesc = "Worked on unrelated files"

.\scripts\beads-log-event.ps1 -EventCode sk.scope.violation -IssueId $CurrentIssue -Description $ViolationDesc

Write-Warning "Scope violation detected"
Write-Host "You were working on: $CurrentIssue"
Write-Host "But made changes to: $ViolationDesc"
Write-Host ""
Write-Host "Options:"
Write-Host "1. File the tangential work as discovered issue"
Write-Host "2. Update current issue to include this work"
Write-Host "3. Continue (violation logged for review)"
```

---

## Issue Selection

To change the current issue mid-session (use sparingly):

```bash
# Bash - Select a different issue
NEW_ISSUE="bd-XXXX"

# Verify issue exists and is available
bd show "$NEW_ISSUE" || { echo "Issue not found"; exit 1; }

# Update status and set as current
bd update "$NEW_ISSUE" --status in_progress
echo "$NEW_ISSUE" > .beads/current-issue

./scripts/beads-log-event.sh sk.scope.select "$NEW_ISSUE" "Changed current issue"

echo "Now working on: $NEW_ISSUE"
```

```powershell
# PowerShell - Select a different issue
$NewIssue = "bd-XXXX"

# Verify issue exists
bd show $NewIssue
if ($LASTEXITCODE -ne 0) {
    Write-Error "Issue not found"
    exit 1
}

# Update status and set as current
bd update $NewIssue --status in_progress
$NewIssue | Out-File -FilePath ".beads\current-issue" -NoNewline

.\scripts\beads-log-event.ps1 -EventCode sk.scope.select -IssueId $NewIssue -Description "Changed current issue"

Write-Host "Now working on: $NewIssue"
```

---

## Common Patterns

### "I found a bug while working on my feature"

```bash
# Don't fix it now! File it:
bd create "Discovered: Bug in auth module causes 500 errors" -t bug
# Continue with your feature
```

### "This code needs refactoring"

```bash
# Don't refactor now! File it:
bd create "Discovered: Refactor payment processing for clarity" -t chore
# Continue with your issue
```

### "I should add tests for this other thing"

```bash
# Don't add tests now! File it:
bd create "Discovered: Missing tests for user registration" -t task
# Continue with your issue
```

### "There's a security vulnerability here"

```bash
# Security issues might warrant immediate attention - use judgment
# If it can wait:
bd create "Discovered: SQL injection vulnerability in search" -t bug -p 0
# If critical, discuss with user before changing scope
```

---

## Scope Verification (Pre-Commit Advisory)

For enhanced scope tracking, you can check changed files against the issue:

```bash
# Bash - Pre-commit scope check (advisory)
CURRENT_ISSUE=$(cat .beads/current-issue 2>/dev/null)
if [ -z "$CURRENT_ISSUE" ]; then
    echo "No current issue set - skipping scope check"
    exit 0
fi

CHANGED_FILES=$(git diff --cached --name-only)
echo "Committing changes to: $CHANGED_FILES"
echo "Current issue: $CURRENT_ISSUE"

# Advisory only - log but don't block
./scripts/beads-log-event.sh gd.scope.check "$CURRENT_ISSUE" "Files: $CHANGED_FILES"
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.scope.activated` | Skill loads | Scope check initiated |
| `sk.scope.discovery` | Work properly filed | Discovered work captured |
| `sk.scope.violation` | Worked outside scope | Advisory warning |
| `sk.scope.select` | Issue changed | New issue selected |
| `gd.scope.check` | Files checked | Pre-commit advisory |

---

## Integration with Other Skills

**beads-bootup**: Sets the current issue via `echo <id> > .beads/current-issue`
**beads-scope**: Monitors adherence to that selection (this skill)
**beads-landing**: Prompts for discovered work filing before session end

---

## Why This Matters

**Without scope discipline:**
- Agents try to fix everything they see
- Context window fills with tangential work
- Original issue never completes
- Session ends with partial work everywhere

**With scope discipline:**
- One issue gets full attention
- Tangential work is captured (not lost)
- Clean commits with clear attribution
- Predictable progress

---

## Quick Reference

```
Check current scope:    cat .beads/current-issue
File discovered work:   bd create "Discovered: ..." -t task
Change issue:           bd update <id> --status in_progress && echo <id> > .beads/current-issue
Log violation:          ./scripts/beads-log-event.sh sk.scope.violation <id> "<reason>"
```

---

**STATUS: ACTIVE** (bd-u91r) - Scope discipline enforcement implemented.

**MANTRA:** If it's not your issue, file it and move on.

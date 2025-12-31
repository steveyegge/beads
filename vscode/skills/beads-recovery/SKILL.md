---
name: beads-recovery
description: |
  Detects common failure states and provides step-by-step guided recovery.
  Handles scenarios like session ended without landing, destructive changes,
  merge conflicts, issues closed without testing, multiple in-progress issues,
  abandoned sessions, and context exhaustion. Trigger with "recover session",
  "fix beads state", "something went wrong", "session crashed", "recover from",
  "beads broken", or "need recovery help".
allowed-tools: "Read,Bash(bd:*),Bash(git:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Recovery Skill

> **STATUS: ACTIVE**
> This skill detects common failure states and provides step-by-step guided recovery.

## Purpose

The recovery skill helps users recover from common mistakes without needing to remember exact commands. It:

1. Detects the current failure state
2. Explains what went wrong
3. Provides step-by-step guided recovery
4. Logs recovery events for observability

---

## When to Invoke

Trigger this skill when ANY of these are true:

- [ ] Session ended without `git push` completing
- [ ] `bd sync` failed with a merge conflict
- [ ] Agent closed an issue without running tests
- [ ] Agent made destructive changes that need reverting
- [ ] Multiple issues are marked `in_progress` (scope violation)
- [ ] Need to abandon a session and start fresh
- [ ] Context exhaustion left work in unknown state

---

## Activation

When this skill is loaded, IMMEDIATELY run detection:

### Step 1: Detect Failure State

```bash
# Bash
echo "=== BEADS RECOVERY: Detecting failure state ==="

# Check 1: Uncommitted beads changes?
BEADS_DIRTY=$(git status --porcelain .beads/ 2>/dev/null | wc -l)

# Check 2: Unpushed commits?
UNPUSHED=$(git log origin/$(git branch --show-current 2>/dev/null)..HEAD 2>/dev/null | wc -l)

# Check 3: Merge conflict in beads?
CONFLICT=$(git diff --name-only --diff-filter=U .beads/ 2>/dev/null | wc -l)

# Check 4: Multiple in_progress issues?
MULTI_WIP=$(bd list --status in_progress --json 2>/dev/null | jq 'length' 2>/dev/null || echo 0)

# Check 5: Stashed changes?
STASHES=$(git stash list 2>/dev/null | wc -l)

# Report findings
echo ""
echo "Detection Results:"
echo "  Uncommitted beads changes: $BEADS_DIRTY"
echo "  Unpushed commits: $UNPUSHED"
echo "  Merge conflicts: $CONFLICT"
echo "  In-progress issues: $MULTI_WIP"
echo "  Stashed changes: $STASHES"

./scripts/beads-log-event.sh rc.detect.complete "none" "beads=$BEADS_DIRTY unpushed=$UNPUSHED conflict=$CONFLICT wip=$MULTI_WIP stash=$STASHES"
```

```powershell
# PowerShell
Write-Host "=== BEADS RECOVERY: Detecting failure state ===" -ForegroundColor Cyan

# Check 1: Uncommitted beads changes?
$beadsDirty = (git status --porcelain .beads/ 2>$null | Measure-Object -Line).Lines

# Check 2: Unpushed commits?
$currentBranch = git branch --show-current 2>$null
$unpushed = if ($currentBranch) { (git log "origin/$currentBranch..HEAD" 2>$null | Measure-Object -Line).Lines } else { 0 }

# Check 3: Merge conflict in beads?
$conflict = (git diff --name-only --diff-filter=U .beads/ 2>$null | Measure-Object -Line).Lines

# Check 4: Multiple in_progress issues?
try {
    $multiWip = (bd list --status in_progress --json 2>$null | ConvertFrom-Json).Count
} catch {
    $multiWip = 0
}

# Check 5: Stashed changes?
$stashes = (git stash list 2>$null | Measure-Object -Line).Lines

# Report findings
Write-Host ""
Write-Host "Detection Results:"
Write-Host "  Uncommitted beads changes: $beadsDirty"
Write-Host "  Unpushed commits: $unpushed"
Write-Host "  Merge conflicts: $conflict"
Write-Host "  In-progress issues: $multiWip"
Write-Host "  Stashed changes: $stashes"

.\scripts\beads-log-event.ps1 -EventCode rc.detect.complete -Details "beads=$beadsDirty unpushed=$unpushed conflict=$conflict wip=$multiWip stash=$stashes"
```

### Step 2: Present Recovery Options

Based on detection results, output the appropriate recovery guide:

```
==================================================================
SKILL ACTIVATED: beads-recovery
STATUS: Active
EVENT: rc.detect.complete logged to .beads/events.log
==================================================================

DETECTED ISSUES:
[List detected issues based on Step 1]

RECOMMENDED RECOVERY:
[Show appropriate section from below based on detected issues]

==================================================================
```

---

## Recovery Procedures

### Scenario 1: Session Ended Without Landing

**Symptoms:** Uncommitted beads changes, unpushed commits

**Detection:**
```bash
git status --porcelain .beads/
git log origin/main..HEAD --oneline
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Session ended without landing ==="

# Step 1: Sync beads state
bd sync
./scripts/beads-log-event.sh rc.recover.sync "none" "session-without-landing"

# Step 2: Stage beads changes
git add .beads/

# Step 3: Commit with recovery message
git commit -m "Recover beads state from incomplete session

Session ended without proper landing ritual.
Recovery performed by beads-recovery skill."
./scripts/beads-log-event.sh rc.recover.commit "none" "recovery commit created"

# Step 4: Push to remote
git push
./scripts/beads-log-event.sh rc.recover.push "none" "changes pushed"

# Step 5: Verify
git status
./scripts/beads-log-event.sh rc.recover.complete "none" "session-without-landing"

echo ""
echo "RECOVERY COMPLETE: Beads state synchronized and pushed."
```

```powershell
# PowerShell
Write-Host "=== RECOVERY: Session ended without landing ===" -ForegroundColor Yellow

# Step 1: Sync beads state
bd sync
.\scripts\beads-log-event.ps1 -EventCode rc.recover.sync -Details "session-without-landing"

# Step 2: Stage beads changes
git add .beads/

# Step 3: Commit with recovery message
git commit -m "Recover beads state from incomplete session

Session ended without proper landing ritual.
Recovery performed by beads-recovery skill."
.\scripts\beads-log-event.ps1 -EventCode rc.recover.commit -Details "recovery commit created"

# Step 4: Push to remote
git push
.\scripts\beads-log-event.ps1 -EventCode rc.recover.push -Details "changes pushed"

# Step 5: Verify
git status
.\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -Details "session-without-landing"

Write-Host ""
Write-Host "RECOVERY COMPLETE: Beads state synchronized and pushed." -ForegroundColor Green
```

---

### Scenario 2: Destructive Changes (Revert Last Commit)

**Symptoms:** Agent made unwanted changes in the last commit

**Detection:**
```bash
git log -1 --oneline
git diff HEAD~1 --stat
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Reverting destructive changes ==="

# Show what will be reverted
echo "Last commit to revert:"
git log -1 --oneline
echo ""
echo "Files affected:"
git diff HEAD~1 --stat

# Confirm with user before proceeding
read -p "Proceed with revert? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    # Create revert commit (keeps history)
    git revert HEAD --no-edit
    ./scripts/beads-log-event.sh rc.recover.revert "none" "reverted HEAD"

    # Re-import beads state from before the change
    bd import -i .beads/issues.jsonl
    ./scripts/beads-log-event.sh rc.recover.import "none" "reimported beads"

    # Push the revert
    git push
    ./scripts/beads-log-event.sh rc.recover.complete "none" "destructive-changes-reverted"

    echo ""
    echo "RECOVERY COMPLETE: Changes reverted. Check 'git log -3' to confirm."
else
    echo "Revert cancelled."
    ./scripts/beads-log-event.sh rc.recover.cancelled "none" "user cancelled revert"
fi
```

```powershell
# PowerShell
Write-Host "=== RECOVERY: Reverting destructive changes ===" -ForegroundColor Yellow

# Show what will be reverted
Write-Host "Last commit to revert:"
git log -1 --oneline
Write-Host ""
Write-Host "Files affected:"
git diff HEAD~1 --stat

# Confirm with user before proceeding
$reply = Read-Host "Proceed with revert? (y/n)"
if ($reply -eq 'y' -or $reply -eq 'Y') {
    # Create revert commit (keeps history)
    git revert HEAD --no-edit
    .\scripts\beads-log-event.ps1 -EventCode rc.recover.revert -Details "reverted HEAD"

    # Re-import beads state from before the change
    bd import -i .beads/issues.jsonl
    .\scripts\beads-log-event.ps1 -EventCode rc.recover.import -Details "reimported beads"

    # Push the revert
    git push
    .\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -Details "destructive-changes-reverted"

    Write-Host ""
    Write-Host "RECOVERY COMPLETE: Changes reverted. Check 'git log -3' to confirm." -ForegroundColor Green
} else {
    Write-Host "Revert cancelled."
    .\scripts\beads-log-event.ps1 -EventCode rc.recover.cancelled -Details "user cancelled revert"
}
```

**Alternative - Hard Reset (DESTRUCTIVE - discards local changes):**

```bash
# ONLY use if you want to completely discard uncommitted changes
git reset --hard HEAD~1
./scripts/beads-log-event.sh rc.recover.reset "none" "hard reset HEAD~1"
```

---

### Scenario 3: Beads Merge Conflict

**Symptoms:** `bd sync` fails, git shows conflict markers in `.beads/issues.jsonl`

**Detection:**
```bash
git diff --name-only --diff-filter=U .beads/
cat .beads/issues.jsonl | grep "<<<<<" | head -1
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Beads merge conflict ==="

# Step 1: Accept remote version (their changes win)
git checkout --theirs .beads/issues.jsonl
./scripts/beads-log-event.sh rc.recover.conflict "none" "accepted theirs"

# Step 2: Mark as resolved
git add .beads/issues.jsonl

# Step 3: Re-import to sync database with JSONL
bd import -i .beads/issues.jsonl
./scripts/beads-log-event.sh rc.recover.import "none" "reimported after conflict"

# Step 4: Complete the merge
git commit -m "Resolve beads merge conflict (accepted remote)

Conflict resolved by beads-recovery skill.
Remote version was accepted to maintain consistency."
./scripts/beads-log-event.sh rc.recover.commit "none" "conflict resolution committed"

# Step 5: Sync again to export any local-only changes
bd sync
git push
./scripts/beads-log-event.sh rc.recover.complete "none" "merge-conflict"

echo ""
echo "RECOVERY COMPLETE: Merge conflict resolved."
echo "Note: If you had local beads changes, recreate them with 'bd create' or 'bd update'."
```

```powershell
# PowerShell
Write-Host "=== RECOVERY: Beads merge conflict ===" -ForegroundColor Yellow

# Step 1: Accept remote version (their changes win)
git checkout --theirs .beads/issues.jsonl
.\scripts\beads-log-event.ps1 -EventCode rc.recover.conflict -Details "accepted theirs"

# Step 2: Mark as resolved
git add .beads/issues.jsonl

# Step 3: Re-import to sync database with JSONL
bd import -i .beads/issues.jsonl
.\scripts\beads-log-event.ps1 -EventCode rc.recover.import -Details "reimported after conflict"

# Step 4: Complete the merge
git commit -m "Resolve beads merge conflict (accepted remote)

Conflict resolved by beads-recovery skill.
Remote version was accepted to maintain consistency."
.\scripts\beads-log-event.ps1 -EventCode rc.recover.commit -Details "conflict resolution committed"

# Step 5: Sync again to export any local-only changes
bd sync
git push
.\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -Details "merge-conflict"

Write-Host ""
Write-Host "RECOVERY COMPLETE: Merge conflict resolved." -ForegroundColor Green
Write-Host "Note: If you had local beads changes, recreate them with 'bd create' or 'bd update'."
```

---

### Scenario 4: Issue Closed Without Testing

**Symptoms:** Issue marked closed but tests weren't verified

**Detection:**
```bash
bd show <issue-id> --json | jq '.status'
# If status is "closed" but you know tests weren't run
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Reopening issue closed without testing ==="

ISSUE_ID="$1"  # Pass issue ID as argument

# Step 1: Reopen the issue
bd reopen "$ISSUE_ID" --notes "Reopened: Tests not verified before closure. Recovery skill."
./scripts/beads-log-event.sh rc.recover.reopen "$ISSUE_ID" "tests not verified"

# Step 2: Update status
bd update "$ISSUE_ID" --status in_progress
./scripts/beads-log-event.sh rc.recover.update "$ISSUE_ID" "status set to in_progress"

# Step 3: Sync
bd sync
./scripts/beads-log-event.sh rc.recover.complete "$ISSUE_ID" "closed-without-testing"

echo ""
echo "RECOVERY COMPLETE: Issue $ISSUE_ID reopened."
echo "NEXT: Run tests and close properly with 'bd close $ISSUE_ID --reason \"Tests passed\"'"
```

```powershell
# PowerShell
param([string]$IssueId)

Write-Host "=== RECOVERY: Reopening issue closed without testing ===" -ForegroundColor Yellow

# Step 1: Reopen the issue
bd reopen $IssueId --notes "Reopened: Tests not verified before closure. Recovery skill."
.\scripts\beads-log-event.ps1 -EventCode rc.recover.reopen -IssueId $IssueId -Details "tests not verified"

# Step 2: Update status
bd update $IssueId --status in_progress
.\scripts\beads-log-event.ps1 -EventCode rc.recover.update -IssueId $IssueId -Details "status set to in_progress"

# Step 3: Sync
bd sync
.\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -IssueId $IssueId -Details "closed-without-testing"

Write-Host ""
Write-Host "RECOVERY COMPLETE: Issue $IssueId reopened." -ForegroundColor Green
Write-Host "NEXT: Run tests and close properly with 'bd close $IssueId --reason `"Tests passed`"'"
```

---

### Scenario 5: Multiple In-Progress Issues (Scope Violation)

**Symptoms:** More than one issue marked `in_progress`

**Detection:**
```bash
bd list --status in_progress --json | jq -r '.[].id'
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Multiple in_progress issues detected ==="

# List all in_progress issues
echo "Current in_progress issues:"
bd list --status in_progress

./scripts/beads-log-event.sh rc.recover.scope "none" "multiple WIP detected"

# Prompt user to select which to keep
echo ""
echo "Select ONE issue to continue working on."
echo "Other issues will be reset to 'open' status."
echo ""
read -p "Enter the issue ID to keep in_progress: " KEEP_ID

# Get all WIP issues
WIP_IDS=$(bd list --status in_progress --json | jq -r '.[].id')

for id in $WIP_IDS; do
    if [ "$id" != "$KEEP_ID" ]; then
        bd update "$id" --status open --notes "Reset to open by recovery skill (scope violation)"
        ./scripts/beads-log-event.sh rc.recover.scope "$id" "reset to open"
        echo "Reset $id to open"
    fi
done

./scripts/beads-log-event.sh rc.recover.complete "$KEEP_ID" "scope-violation-resolved"

echo ""
echo "RECOVERY COMPLETE: Now working only on $KEEP_ID"
```

```powershell
# PowerShell
Write-Host "=== RECOVERY: Multiple in_progress issues detected ===" -ForegroundColor Yellow

# List all in_progress issues
Write-Host "Current in_progress issues:"
bd list --status in_progress

.\scripts\beads-log-event.ps1 -EventCode rc.recover.scope -Details "multiple WIP detected"

# Prompt user to select which to keep
Write-Host ""
Write-Host "Select ONE issue to continue working on."
Write-Host "Other issues will be reset to 'open' status."
Write-Host ""
$keepId = Read-Host "Enter the issue ID to keep in_progress"

# Get all WIP issues
$wipIssues = bd list --status in_progress --json | ConvertFrom-Json

foreach ($issue in $wipIssues) {
    if ($issue.id -ne $keepId) {
        bd update $issue.id --status open --notes "Reset to open by recovery skill (scope violation)"
        .\scripts\beads-log-event.ps1 -EventCode rc.recover.scope -IssueId $issue.id -Details "reset to open"
        Write-Host "Reset $($issue.id) to open"
    }
}

.\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -IssueId $keepId -Details "scope-violation-resolved"

Write-Host ""
Write-Host "RECOVERY COMPLETE: Now working only on $keepId" -ForegroundColor Green
```

---

### Scenario 6: Abandon Session Entirely

**Symptoms:** Need to completely discard current session work

**Detection:**
```bash
git status --porcelain | wc -l  # Uncommitted changes
bd list --status in_progress    # Active work
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Abandoning session entirely ==="

# Step 1: Stash any uncommitted changes (preserves them just in case)
if [ -n "$(git status --porcelain)" ]; then
    git stash push -m "Abandoned session $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    ./scripts/beads-log-event.sh rc.recover.stash "none" "changes stashed"
    echo "Uncommitted changes stashed."
fi

# Step 2: Reset any in_progress issues to open
WIP_IDS=$(bd list --status in_progress --json 2>/dev/null | jq -r '.[].id' 2>/dev/null)
for id in $WIP_IDS; do
    bd update "$id" --status open --notes "Session abandoned. Work not completed."
    ./scripts/beads-log-event.sh rc.recover.abandon "$id" "reset to open"
done

# Step 3: Re-import beads from last known good state
bd import -i .beads/issues.jsonl
./scripts/beads-log-event.sh rc.recover.import "none" "reimported from JSONL"

# Step 4: Sync to ensure consistency
bd sync
./scripts/beads-log-event.sh rc.recover.complete "none" "session-abandoned"

echo ""
echo "RECOVERY COMPLETE: Session abandoned."
echo "Stashed changes can be recovered with: git stash list && git stash pop"
```

```powershell
# PowerShell
Write-Host "=== RECOVERY: Abandoning session entirely ===" -ForegroundColor Yellow

# Step 1: Stash any uncommitted changes (preserves them just in case)
$hasChanges = git status --porcelain
if ($hasChanges) {
    $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    git stash push -m "Abandoned session $timestamp"
    .\scripts\beads-log-event.ps1 -EventCode rc.recover.stash -Details "changes stashed"
    Write-Host "Uncommitted changes stashed."
}

# Step 2: Reset any in_progress issues to open
try {
    $wipIssues = bd list --status in_progress --json 2>$null | ConvertFrom-Json
    foreach ($issue in $wipIssues) {
        bd update $issue.id --status open --notes "Session abandoned. Work not completed."
        .\scripts\beads-log-event.ps1 -EventCode rc.recover.abandon -IssueId $issue.id -Details "reset to open"
    }
} catch {}

# Step 3: Re-import beads from last known good state
bd import -i .beads/issues.jsonl
.\scripts\beads-log-event.ps1 -EventCode rc.recover.import -Details "reimported from JSONL"

# Step 4: Sync to ensure consistency
bd sync
.\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -Details "session-abandoned"

Write-Host ""
Write-Host "RECOVERY COMPLETE: Session abandoned." -ForegroundColor Green
Write-Host "Stashed changes can be recovered with: git stash list && git stash pop"
```

---

### Scenario 7: Context Exhaustion Recovery

**Symptoms:** Session ended mid-work due to context limits, state unknown

**Detection:**
```bash
bd list --status in_progress  # Check for WIP
git status                     # Check for uncommitted changes
git log -1 --format="%s"       # Check last commit message
```

**Recovery:**

```bash
# Bash
echo "=== RECOVERY: Context exhaustion - unknown state ==="

# Step 1: Assess current state
echo "Last commit:"
git log -1 --oneline

echo ""
echo "Uncommitted changes:"
git status --short

echo ""
echo "In-progress issues:"
bd list --status in_progress

./scripts/beads-log-event.sh rc.recover.assess "none" "context exhaustion assessment"

# Step 2: Commit any partial progress
if [ -n "$(git status --porcelain)" ]; then
    git add -A
    git commit -m "WIP: Context exhaustion checkpoint

Partial work committed by recovery skill.
Review and continue in next session."
    ./scripts/beads-log-event.sh rc.recover.checkpoint "none" "WIP committed"
    echo "Partial changes committed."
fi

# Step 3: Update in_progress issues with checkpoint note
WIP_IDS=$(bd list --status in_progress --json 2>/dev/null | jq -r '.[].id' 2>/dev/null)
for id in $WIP_IDS; do
    bd update "$id" --notes "Context exhaustion checkpoint. Review git log for partial progress."
    ./scripts/beads-log-event.sh rc.recover.checkpoint "$id" "issue updated with checkpoint note"
done

# Step 4: Sync and push
bd sync
git push
./scripts/beads-log-event.sh rc.recover.complete "none" "context-exhaustion"

echo ""
echo "RECOVERY COMPLETE: Checkpoint saved."
echo ""
echo "NEXT SESSION START:"
echo "  bd show <issue-id> --json  # Review issue notes"
echo "  git log -5 --oneline       # Review recent commits"
echo "  git diff HEAD~1            # See what changed in checkpoint"
```

```powershell
# PowerShell
Write-Host "=== RECOVERY: Context exhaustion - unknown state ===" -ForegroundColor Yellow

# Step 1: Assess current state
Write-Host "Last commit:"
git log -1 --oneline

Write-Host ""
Write-Host "Uncommitted changes:"
git status --short

Write-Host ""
Write-Host "In-progress issues:"
bd list --status in_progress

.\scripts\beads-log-event.ps1 -EventCode rc.recover.assess -Details "context exhaustion assessment"

# Step 2: Commit any partial progress
$hasChanges = git status --porcelain
if ($hasChanges) {
    git add -A
    git commit -m "WIP: Context exhaustion checkpoint

Partial work committed by recovery skill.
Review and continue in next session."
    .\scripts\beads-log-event.ps1 -EventCode rc.recover.checkpoint -Details "WIP committed"
    Write-Host "Partial changes committed."
}

# Step 3: Update in_progress issues with checkpoint note
try {
    $wipIssues = bd list --status in_progress --json 2>$null | ConvertFrom-Json
    foreach ($issue in $wipIssues) {
        bd update $issue.id --notes "Context exhaustion checkpoint. Review git log for partial progress."
        .\scripts\beads-log-event.ps1 -EventCode rc.recover.checkpoint -IssueId $issue.id -Details "issue updated with checkpoint note"
    }
} catch {}

# Step 4: Sync and push
bd sync
git push
.\scripts\beads-log-event.ps1 -EventCode rc.recover.complete -Details "context-exhaustion"

Write-Host ""
Write-Host "RECOVERY COMPLETE: Checkpoint saved." -ForegroundColor Green
Write-Host ""
Write-Host "NEXT SESSION START:"
Write-Host "  bd show <issue-id> --json  # Review issue notes"
Write-Host "  git log -5 --oneline       # Review recent commits"
Write-Host "  git diff HEAD~1            # See what changed in checkpoint"
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `rc.detect.complete` | Detection finished | Detection results |
| `rc.recover.sync` | bd sync during recovery | Scenario name |
| `rc.recover.commit` | Recovery commit created | Commit description |
| `rc.recover.push` | Changes pushed | Push result |
| `rc.recover.revert` | git revert executed | What was reverted |
| `rc.recover.reset` | git reset executed | Reset target |
| `rc.recover.import` | bd import executed | Import reason |
| `rc.recover.conflict` | Merge conflict resolved | Resolution strategy |
| `rc.recover.reopen` | Issue reopened | Reason for reopening |
| `rc.recover.update` | Issue updated | Update details |
| `rc.recover.scope` | Scope violation resolved | Issue affected |
| `rc.recover.stash` | Changes stashed | Stash description |
| `rc.recover.abandon` | Session abandoned | Issues reset |
| `rc.recover.assess` | State assessment done | Assessment type |
| `rc.recover.checkpoint` | Checkpoint saved | Checkpoint details |
| `rc.recover.complete` | Recovery finished | Scenario name |
| `rc.recover.cancelled` | Recovery cancelled | Reason |

---

## Quick Reference

| Scenario | Quick Command |
|----------|---------------|
| Session didn't land | `bd sync && git add .beads/ && git commit -m "Recover" && git push` |
| Revert last commit | `git revert HEAD --no-edit && bd import -i .beads/issues.jsonl` |
| Revert destructively | `git reset --hard HEAD~1` |
| Merge conflict | `git checkout --theirs .beads/issues.jsonl && bd import -i .beads/issues.jsonl` |
| Reopen closed issue | `bd reopen <id> --notes "Tests not verified"` |
| Multiple WIP issues | `bd update <other-ids> --status open` |
| Abandon session | `git stash push && bd import -i .beads/issues.jsonl` |

---

**STATUS:** This skill is ACTIVE and functional.

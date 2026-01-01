---
name: beads-landing
description: |
  Executes the mandatory session end ritual. Files discovered work, runs quality
  gates (tests), updates beads state, syncs to git, and generates handoff prompt.
  The landing ritual is NON-NEGOTIABLE - session has not ended until git push
  completes. Trigger with "end session", "landing", "session end", "wrap up",
  "finish work", or "close session".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(mkdir:*),Bash(cat:*),Bash(go:*),Bash(npm:*),Bash(pytest:*)"
version: "0.2.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Landing Skill

> **STATUS: ACTIVE - FULL PROCESSING** (bd-ku92)
> This skill executes the complete 6-phase landing ritual.

## Purpose

The landing skill executes at the END of every coding session.
It ensures clean state and enables session continuity.

**THIS RITUAL IS NON-NEGOTIABLE.**

---

## Activation

When this skill is loaded, execute the **6-phase landing ritual** below.

---

### Phase 1: Session Validation

```bash
# Bash - Validate session state
if [ ! -d ".beads" ]; then
    echo "ERROR: Not in a beads workspace (.beads/ directory not found)"
    exit 1
fi

# Log landing start
./scripts/beads-log-event.sh ss.landing.start

# Check for session marker
if [ ! -f ".beads/.session-active" ]; then
    echo "WARNING: No active session marker found (.beads/.session-active)"
    echo "Proceeding with landing anyway..."
fi

# Read current issue if available
if [ -f ".beads/current-issue" ]; then
    CURRENT_ISSUE=$(cat .beads/current-issue)
    echo "Current issue: $CURRENT_ISSUE"
    bd show "$CURRENT_ISSUE"
else
    echo "WARNING: No current issue set (.beads/current-issue not found)"
    CURRENT_ISSUE=""
fi
```

```powershell
# PowerShell - Validate session state
if (-not (Test-Path ".beads")) {
    Write-Error "Not in a beads workspace (.beads/ directory not found)"
    exit 1
}

# Log landing start
.\scripts\beads-log-event.ps1 -EventCode ss.landing.start

# Check for session marker
if (-not (Test-Path ".beads\.session-active")) {
    Write-Warning "No active session marker found (.beads\.session-active)"
    Write-Host "Proceeding with landing anyway..."
}

# Read current issue if available
if (Test-Path ".beads\current-issue") {
    $CurrentIssue = Get-Content ".beads\current-issue" -Raw
    $CurrentIssue = $CurrentIssue.Trim()
    Write-Host "Current issue: $CurrentIssue"
    bd show $CurrentIssue
} else {
    Write-Warning "No current issue set (.beads\current-issue not found)"
    $CurrentIssue = ""
}
```

---

### Phase 2: File Discovered Work

**Ask the user**: "Any discovered work to file before closing?"

For each discovered item, run:

```bash
# Bash - File discovered work
DISCOVERED_TITLE="$1"  # e.g., "Discovered: Need to refactor auth module"
DISCOVERED_TYPE="${2:-task}"

if [ -n "$CURRENT_ISSUE" ]; then
    bd create "$DISCOVERED_TITLE" -t "$DISCOVERED_TYPE" --deps "discovered-from:$CURRENT_ISSUE"
    ./scripts/beads-log-event.sh ss.discovered "$CURRENT_ISSUE" "$DISCOVERED_TITLE"
else
    bd create "$DISCOVERED_TITLE" -t "$DISCOVERED_TYPE"
    ./scripts/beads-log-event.sh ss.discovered none "$DISCOVERED_TITLE"
fi
```

```powershell
# PowerShell - File discovered work
param($DiscoveredTitle, $DiscoveredType = "task")

if ($CurrentIssue) {
    bd create $DiscoveredTitle -t $DiscoveredType --deps "discovered-from:$CurrentIssue"
    .\scripts\beads-log-event.ps1 -EventCode ss.discovered -IssueId $CurrentIssue -Description $DiscoveredTitle
} else {
    bd create $DiscoveredTitle -t $DiscoveredType
    .\scripts\beads-log-event.ps1 -EventCode ss.discovered -Description $DiscoveredTitle
}
```

If no discoveries, skip to Phase 3.

---

### Phase 3: Quality Gates (MANDATORY)

Run the project's test suite:

```bash
# Bash - Detect and run tests
mkdir -p .beads
TEST_RESULT="SKIPPED"

if [ -f "go.mod" ]; then
    echo "Running Go tests..."
    if go test ./...; then
        TEST_RESULT="PASSED"
    else
        TEST_RESULT="FAILED"
    fi
elif [ -f "package.json" ]; then
    echo "Running npm tests..."
    if npm test; then
        TEST_RESULT="PASSED"
    else
        TEST_RESULT="FAILED"
    fi
elif [ -f "pytest.ini" ] || [ -f "setup.py" ] || [ -f "pyproject.toml" ]; then
    echo "Running pytest..."
    if pytest; then
        TEST_RESULT="PASSED"
    else
        TEST_RESULT="FAILED"
    fi
else
    echo "No test framework detected - skipping tests"
    TEST_RESULT="SKIPPED"
fi

# Log test result
./scripts/beads-log-event.sh ss.landing.test none "Tests: $TEST_RESULT"

# Write landing marker with test result (bd-uo2u enforcement)
echo "${TEST_RESULT}:$(date -u +%Y-%m-%dT%H:%M:%SZ)" > .beads/.landing-complete

echo "TESTS: $TEST_RESULT"
```

```powershell
# PowerShell - Detect and run tests
New-Item -ItemType Directory -Force -Path .beads | Out-Null
$TestResult = "SKIPPED"

if (Test-Path "go.mod") {
    Write-Host "Running Go tests..."
    go test ./...
    if ($LASTEXITCODE -eq 0) { $TestResult = "PASSED" } else { $TestResult = "FAILED" }
}
elseif (Test-Path "package.json") {
    Write-Host "Running npm tests..."
    npm test
    if ($LASTEXITCODE -eq 0) { $TestResult = "PASSED" } else { $TestResult = "FAILED" }
}
elseif ((Test-Path "pytest.ini") -or (Test-Path "setup.py") -or (Test-Path "pyproject.toml")) {
    Write-Host "Running pytest..."
    pytest
    if ($LASTEXITCODE -eq 0) { $TestResult = "PASSED" } else { $TestResult = "FAILED" }
}
else {
    Write-Host "No test framework detected - skipping tests"
}

# Log test result
.\scripts\beads-log-event.ps1 -EventCode ss.landing.test -Description "Tests: $TestResult"

# Write landing marker with test result (bd-uo2u enforcement)
$Timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
"${TestResult}:${Timestamp}" | Out-File -FilePath .beads\.landing-complete -NoNewline

Write-Host "TESTS: $TestResult"
```

**IMPORTANT (bd-uo2u)**: If tests FAIL:
- DO NOT close the issue
- Update issue with failure notes
- Fix tests before pushing

```
TESTS FAILED - PUSH WILL BE BLOCKED
Fix failing tests before proceeding with push.
```

---

### Phase 4: Update Beads State

Based on test results and work completion:

```bash
# Bash - Update beads state
if [ -z "$CURRENT_ISSUE" ]; then
    echo "No current issue to update"
else
    if [ "$TEST_RESULT" = "PASSED" ]; then
        # Ask user: Is this issue complete?
        # If complete:
        echo "Work complete? Close issue with: bd close $CURRENT_ISSUE --reason '<summary>'"
        # If not complete:
        echo "Or update progress with: bd update $CURRENT_ISSUE --notes '<progress>'"
    else
        echo "Tests failed - updating issue with failure notes"
        bd update "$CURRENT_ISSUE" --notes "Landing blocked: tests failing"
        ./scripts/beads-log-event.sh bd.issue.update "$CURRENT_ISSUE" "Landing blocked: tests failing"
    fi
fi

./scripts/beads-log-event.sh ss.landing.update "$CURRENT_ISSUE"
```

```powershell
# PowerShell - Update beads state
if (-not $CurrentIssue) {
    Write-Host "No current issue to update"
} else {
    if ($TestResult -eq "PASSED") {
        Write-Host "Work complete? Close issue with: bd close $CurrentIssue --reason '<summary>'"
        Write-Host "Or update progress with: bd update $CurrentIssue --notes '<progress>'"
    } else {
        Write-Host "Tests failed - updating issue with failure notes"
        bd update $CurrentIssue --notes "Landing blocked: tests failing"
        .\scripts\beads-log-event.ps1 -EventCode bd.issue.update -IssueId $CurrentIssue -Description "Landing blocked: tests failing"
    }
}

.\scripts\beads-log-event.ps1 -EventCode ss.landing.update -IssueId $CurrentIssue
```

---

### Phase 5: Sync and Push (MANDATORY - MUST SUCCEED)

```bash
# Bash - Sync and push
echo "Syncing beads..."
bd sync
./scripts/beads-log-event.sh bd.sync.complete

echo "Adding and committing..."
git add -A
git status

# Commit if there are changes
if ! git diff --cached --quiet; then
    git commit -m "bd-${CURRENT_ISSUE:-session}: Session landing"
fi

echo "Pushing to remote..."
if git push; then
    ./scripts/beads-log-event.sh gt.push.complete
    echo "Push successful"
else
    ./scripts/beads-log-event.sh gt.push.reject none "Push rejected"
    echo "Push rejected - attempting rebase..."
    git pull --rebase
    if git push; then
        ./scripts/beads-log-event.sh gt.push.complete none "Push succeeded after rebase"
    else
        echo ""
        echo "LANDING FAILED - WORK NOT PUSHED"
        echo "Manual intervention required. Run:"
        echo "  git status"
        echo "  git push origin $(git branch --show-current)"
        echo "Do NOT end session until work is pushed."
        ./scripts/beads-log-event.sh ss.landing.failed none "Push failed after retry"
        exit 1
    fi
fi

# Verify clean state
git status
./scripts/beads-log-event.sh ss.landing.sync
```

```powershell
# PowerShell - Sync and push
Write-Host "Syncing beads..."
bd sync
.\scripts\beads-log-event.ps1 -EventCode bd.sync.complete

Write-Host "Adding and committing..."
git add -A
git status

# Commit if there are changes
$StagedChanges = git diff --cached --name-only
if ($StagedChanges) {
    $CommitMsg = "bd-$($CurrentIssue): Session landing"
    if (-not $CurrentIssue) { $CommitMsg = "Session landing" }
    git commit -m $CommitMsg
}

Write-Host "Pushing to remote..."
git push
if ($LASTEXITCODE -eq 0) {
    .\scripts\beads-log-event.ps1 -EventCode gt.push.complete
    Write-Host "Push successful"
} else {
    .\scripts\beads-log-event.ps1 -EventCode gt.push.reject -Description "Push rejected"
    Write-Host "Push rejected - attempting rebase..."
    git pull --rebase
    git push
    if ($LASTEXITCODE -eq 0) {
        .\scripts\beads-log-event.ps1 -EventCode gt.push.complete -Description "Push succeeded after rebase"
    } else {
        Write-Host ""
        Write-Host "LANDING FAILED - WORK NOT PUSHED"
        Write-Host "Manual intervention required. Run:"
        Write-Host "  git status"
        $Branch = git branch --show-current
        Write-Host "  git push origin $Branch"
        Write-Host "Do NOT end session until work is pushed."
        .\scripts\beads-log-event.ps1 -EventCode ss.landing.failed -Description "Push failed after retry"
        exit 1
    }
}

# Verify clean state
git status
.\scripts\beads-log-event.ps1 -EventCode ss.landing.sync
```

---

### Phase 6: Generate Handoff

```bash
# Bash - Generate handoff document
HANDOFF_FILE=".beads/last-handoff.md"

cat > "$HANDOFF_FILE" << EOF
# Session Handoff

**Date:** $(date -u +"%Y-%m-%dT%H:%M:%SZ")
**Issue:** ${CURRENT_ISSUE:-None}
**Tests:** $TEST_RESULT

## Work Done
[Agent should summarize work completed this session]

## Next Steps
[Agent should list recommended next actions]

## Quick Start
\`\`\`bash
git pull && bd sync && bd ready
\`\`\`
EOF

./scripts/beads-log-event.sh ss.landing.handoff "$CURRENT_ISSUE"

# Clean up session marker
rm -f .beads/.session-active
rm -f .beads/current-issue

./scripts/beads-log-event.sh ss.landing.complete
./scripts/beads-log-event.sh ss.end

echo ""
echo "Handoff written to: $HANDOFF_FILE"
```

```powershell
# PowerShell - Generate handoff document
$HandoffFile = ".beads\last-handoff.md"
$Timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"

$HandoffContent = @"
# Session Handoff

**Date:** $Timestamp
**Issue:** $($CurrentIssue ?? 'None')
**Tests:** $TestResult

## Work Done
[Agent should summarize work completed this session]

## Next Steps
[Agent should list recommended next actions]

## Quick Start
```bash
git pull && bd sync && bd ready
```
"@

$HandoffContent | Out-File -FilePath $HandoffFile -Encoding utf8

.\scripts\beads-log-event.ps1 -EventCode ss.landing.handoff -IssueId $CurrentIssue

# Clean up session marker
Remove-Item -Path ".beads\.session-active" -ErrorAction SilentlyContinue
Remove-Item -Path ".beads\current-issue" -ErrorAction SilentlyContinue

.\scripts\beads-log-event.ps1 -EventCode ss.landing.complete
.\scripts\beads-log-event.ps1 -EventCode ss.end

Write-Host ""
Write-Host "Handoff written to: $HandoffFile"
```

---

## Output Format

After completing the landing ritual, display:

```
===============================================================
LANDING COMPLETE
===============================================================
Issue: <issue-id> - <status>
Tests: PASSED/FAILED/SKIPPED
Discovered: N new issues filed (if any)
Sync: Complete
Push: Complete
Handoff: Generated at .beads/last-handoff.md
===============================================================
```

If tests FAILED, add:
```
TESTS FAILED - PUSH WILL BE BLOCKED
Fix failing tests before proceeding with push.
```

If push FAILED after retry, output:
```
LANDING FAILED - WORK NOT PUSHED
Manual intervention required. Run:
  git status
  git push origin <branch>
Do NOT end session until work is pushed.
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `ss.landing.start` | Ritual begins | Session ending |
| `ss.discovered` | Work filed | Discovered work captured |
| `ss.landing.test` | Tests run | PASSED/FAILED/SKIPPED |
| `ss.landing.update` | Beads updated | Issue state changed |
| `bd.sync.complete` | Sync done | Beads synced to JSONL |
| `gt.push.complete` | Push done | Changes pushed to remote |
| `gt.push.reject` | Push failed | Needs rebase/retry |
| `ss.landing.sync` | Sync verified | Clean state confirmed |
| `ss.landing.handoff` | Handoff created | Document generated |
| `ss.landing.complete` | Ritual finished | Landing successful |
| `ss.landing.failed` | Ritual failed | Push could not complete |
| `ss.end` | Session terminated | Clean shutdown |

---

## Conflict Resolution Strategy

If `bd sync` fails with conflict:

```bash
git checkout --theirs .beads/issues.jsonl
bd import -i .beads/issues.jsonl
bd sync
```

---

## Dependencies

- **Requires**: Event logging infrastructure (`scripts/beads-log-event.sh`)
- **Requires**: beads-bootup to have set `.beads/current-issue`
- **Optional**: beads-handoff skill for enhanced handoff generation

---

**STATUS: ACTIVE** (bd-ku92) - Full 6-phase landing ritual implemented.

**CRITICAL:** The landing ritual is NON-NEGOTIABLE. A session that ends
without proper landing breaks continuity for the next session.

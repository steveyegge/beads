---
name: beads-handoff
description: |
  Generates structured handoff prompts at session end for seamless continuity.
  Captures current issue context, git history, blockers, and next steps into
  a formatted prompt that can be used to resume work in the next session.
  Trigger with "generate handoff", "end session", "create handoff prompt",
  "session handoff", or "prepare for next session".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(cat:*),Bash(echo:*)"
version: "0.2.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Handoff Skill

> **STATUS: ACTIVE - FULL PROCESSING** (bd-x2wv)
> This skill executes the complete 5-phase handoff generation.

## Purpose

The handoff skill generates a structured handoff prompt at session end.
It captures context for the next session to continue work seamlessly.

**Use this skill when:**
- You are ending a session with work in progress
- You need to hand off an issue to another session
- You want to capture current state for continuity

---

## Activation

When this skill is loaded, execute the **5-phase handoff generation** below.

---

### Phase 1: Issue Context Loading

```bash
# Bash - Load current issue context
./scripts/beads-log-event.sh sk.handoff.activated

echo "==============================================================="
echo "HANDOFF GENERATION"
echo "==============================================================="

# Get current issue
CURRENT_ISSUE=$(cat .beads/current-issue 2>/dev/null || echo "")
if [ -z "$CURRENT_ISSUE" ]; then
    # Fall back to in_progress issues
    CURRENT_ISSUE=$(bd list --status=in_progress 2>/dev/null | head -1 | awk '{print $2}' | tr -d '[]')
fi

if [ -z "$CURRENT_ISSUE" ]; then
    ./scripts/beads-log-event.sh sk.handoff.error none "No current issue found"
    echo "ERROR: No current issue found."
    echo "Run 'bd ready' to see available work, or 'bd list --status=in_progress'"
    exit 1
fi

echo "Current issue: $CURRENT_ISSUE"

# Get issue details (bd show outputs human-readable format)
ISSUE_OUTPUT=$(bd show "$CURRENT_ISSUE" 2>/dev/null)
ISSUE_TITLE=$(echo "$ISSUE_OUTPUT" | head -1 | sed "s/^$CURRENT_ISSUE: //")
ISSUE_STATUS=$(echo "$ISSUE_OUTPUT" | grep "^Status:" | cut -d: -f2 | tr -d ' ')
ISSUE_DESC=$(echo "$ISSUE_OUTPUT" | sed -n '/^Description:/,/^$/p' | tail -n +2 | head -5)

if [ -z "$ISSUE_DESC" ]; then
    ISSUE_DESC="No description"
fi

./scripts/beads-log-event.sh sk.handoff.issue "$CURRENT_ISSUE" "Context loaded"
```

```powershell
# PowerShell - Load current issue context
.\scripts\beads-log-event.ps1 -EventCode sk.handoff.activated

Write-Host "==============================================================="
Write-Host "HANDOFF GENERATION"
Write-Host "==============================================================="

# Get current issue
$CurrentIssue = $null
if (Test-Path ".beads\current-issue") {
    $CurrentIssue = (Get-Content ".beads\current-issue" -Raw).Trim()
}

if (-not $CurrentIssue) {
    # Fall back to in_progress issues
    $InProgress = bd list --status=in_progress 2>$null
    if ($InProgress) {
        $FirstLine = $InProgress | Select-Object -First 1
        if ($FirstLine -match '\[([^\]]+)\]') {
            $CurrentIssue = $Matches[1]
        }
    }
}

if (-not $CurrentIssue) {
    .\scripts\beads-log-event.ps1 -EventCode sk.handoff.error -Description "No current issue found"
    Write-Host "ERROR: No current issue found."
    Write-Host "Run 'bd ready' to see available work, or 'bd list --status=in_progress'"
    exit 1
}

Write-Host "Current issue: $CurrentIssue"

# Get issue details
$IssueOutput = bd show $CurrentIssue 2>$null
$IssueLines = $IssueOutput -split "`n"
$IssueTitle = ($IssueLines[0] -replace "^${CurrentIssue}: ", "").Trim()
$IssueStatus = ($IssueLines | Where-Object { $_ -match "^Status:" }) -replace "Status:\s*", ""
$IssueDesc = "No description"

# Find description section
$DescStart = $false
$DescLines = @()
foreach ($line in $IssueLines) {
    if ($line -match "^Description:") { $DescStart = $true; continue }
    if ($DescStart -and $line -match "^\S+:") { break }
    if ($DescStart) { $DescLines += $line }
}
if ($DescLines.Count -gt 0) {
    $IssueDesc = ($DescLines | Select-Object -First 5) -join "`n"
}

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.issue -IssueId $CurrentIssue -Description "Context loaded"
```

---

### Phase 2: Git History Gathering

```bash
# Bash - Gather git history
echo ""
echo "Gathering git history..."

LAST_COMMIT=$(git log -1 --oneline 2>/dev/null || echo "No commits")
RECENT_COMMITS=$(git log --oneline -5 2>/dev/null || echo "No commits")
UNCOMMITTED=$(git status --short 2>/dev/null | head -10)

if [ -z "$UNCOMMITTED" ]; then
    UNCOMMITTED="None"
fi

./scripts/beads-log-event.sh sk.handoff.git "$CURRENT_ISSUE" "Git history gathered"
```

```powershell
# PowerShell - Gather git history
Write-Host ""
Write-Host "Gathering git history..."

$LastCommit = git log -1 --oneline 2>$null
if (-not $LastCommit) { $LastCommit = "No commits" }

$RecentCommits = git log --oneline -5 2>$null
if (-not $RecentCommits) { $RecentCommits = "No commits" }

$Uncommitted = git status --short 2>$null | Select-Object -First 10
if (-not $Uncommitted) { $Uncommitted = "None" }

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.git -IssueId $CurrentIssue -Description "Git history gathered"
```

---

### Phase 3: Dependency Analysis

```bash
# Bash - Analyze dependencies
echo "Analyzing dependencies..."

# Get blockers (issues blocking this one)
BLOCKERS=$(bd show "$CURRENT_ISSUE" 2>/dev/null | grep -A 100 "^Blocked by:" | grep "^  -" | head -5)
if [ -z "$BLOCKERS" ]; then
    BLOCKERS="None"
fi

# Get issues this one is blocking
BLOCKING=$(bd show "$CURRENT_ISSUE" 2>/dev/null | grep -A 100 "^Blocking:" | grep "^  -" | head -5)
if [ -z "$BLOCKING" ]; then
    BLOCKING="None"
fi

./scripts/beads-log-event.sh sk.handoff.deps "$CURRENT_ISSUE" "Dependencies gathered"
```

```powershell
# PowerShell - Analyze dependencies
Write-Host "Analyzing dependencies..."

$IssueOutput = bd show $CurrentIssue 2>$null
$Blockers = "None"
$Blocking = "None"

# Parse blockers from output
$InBlockedBy = $false
$BlockerLines = @()
foreach ($line in ($IssueOutput -split "`n")) {
    if ($line -match "^Blocked by:") { $InBlockedBy = $true; continue }
    if ($InBlockedBy -and $line -match "^\S" -and $line -notmatch "^  -") { break }
    if ($InBlockedBy -and $line -match "^  -") { $BlockerLines += $line.Trim() }
}
if ($BlockerLines.Count -gt 0) {
    $Blockers = ($BlockerLines | Select-Object -First 5) -join "`n"
}

# Parse blocking from output
$InBlocking = $false
$BlockingLines = @()
foreach ($line in ($IssueOutput -split "`n")) {
    if ($line -match "^Blocking:") { $InBlocking = $true; continue }
    if ($InBlocking -and $line -match "^\S" -and $line -notmatch "^  -") { break }
    if ($InBlocking -and $line -match "^  -") { $BlockingLines += $line.Trim() }
}
if ($BlockingLines.Count -gt 0) {
    $Blocking = ($BlockingLines | Select-Object -First 5) -join "`n"
}

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.deps -IssueId $CurrentIssue -Description "Dependencies gathered"
```

---

### Phase 4: Prompt Generation

```bash
# Bash - Generate handoff prompt
echo "Generating handoff prompt..."

TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

HANDOFF=$(cat << EOF
# Session Handoff

**Generated:** $TIMESTAMP

---

## Continue work on $CURRENT_ISSUE: $ISSUE_TITLE

### Context
$ISSUE_DESC

### Status
$ISSUE_STATUS

### Blockers
$BLOCKERS

### Blocking (issues waiting on this)
$BLOCKING

### Last Commit
$LAST_COMMIT

### Recent Activity
$RECENT_COMMITS

### Uncommitted Changes
$UNCOMMITTED

---

## Next Step
Review the issue context above and continue implementation.

## Quick Start
\`\`\`bash
git pull && bd sync && bd show $CURRENT_ISSUE
\`\`\`
EOF
)

# Output to console
echo ""
echo "$HANDOFF"

# Write to file
echo "$HANDOFF" > .beads/last-handoff.md

./scripts/beads-log-event.sh sk.handoff.generate "$CURRENT_ISSUE" "Handoff prompt generated"
```

```powershell
# PowerShell - Generate handoff prompt
Write-Host "Generating handoff prompt..."

$Timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"

$Handoff = @"
# Session Handoff

**Generated:** $Timestamp

---

## Continue work on ${CurrentIssue}: $IssueTitle

### Context
$IssueDesc

### Status
$IssueStatus

### Blockers
$Blockers

### Blocking (issues waiting on this)
$Blocking

### Last Commit
$LastCommit

### Recent Activity
$($RecentCommits -join "`n")

### Uncommitted Changes
$($Uncommitted -join "`n")

---

## Next Step
Review the issue context above and continue implementation.

## Quick Start
``````bash
git pull && bd sync && bd show $CurrentIssue
``````
"@

# Output to console
Write-Host ""
Write-Host $Handoff

# Write to file
$Handoff | Out-File -FilePath ".beads\last-handoff.md" -Encoding utf8

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.generate -IssueId $CurrentIssue -Description "Handoff prompt generated"
```

---

### Phase 5: Clipboard Integration

```bash
# Bash - Copy to clipboard (platform-aware)
CLIPBOARD_STATUS="Not available"

if command -v pbcopy &> /dev/null; then
    echo "$HANDOFF" | pbcopy
    CLIPBOARD_STATUS="Copied (macOS)"
elif command -v xclip &> /dev/null; then
    echo "$HANDOFF" | xclip -selection clipboard
    CLIPBOARD_STATUS="Copied (Linux/xclip)"
elif command -v xsel &> /dev/null; then
    echo "$HANDOFF" | xsel --clipboard
    CLIPBOARD_STATUS="Copied (Linux/xsel)"
elif command -v clip.exe &> /dev/null; then
    echo "$HANDOFF" | clip.exe
    CLIPBOARD_STATUS="Copied (Windows/WSL)"
fi

./scripts/beads-log-event.sh sk.handoff.complete "$CURRENT_ISSUE" "$CLIPBOARD_STATUS"

echo ""
echo "==============================================================="
echo "HANDOFF COMPLETE"
echo "==============================================================="
echo "Issue: $CURRENT_ISSUE - $ISSUE_TITLE"
echo "Saved to: .beads/last-handoff.md"
echo "Clipboard: $CLIPBOARD_STATUS"
echo "==============================================================="
```

```powershell
# PowerShell - Copy to clipboard (Windows)
$ClipboardStatus = "Not available"

try {
    $Handoff | Set-Clipboard
    $ClipboardStatus = "Copied (Windows)"
} catch {
    $ClipboardStatus = "Failed: $_"
}

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.complete -IssueId $CurrentIssue -Description $ClipboardStatus

Write-Host ""
Write-Host "==============================================================="
Write-Host "HANDOFF COMPLETE"
Write-Host "==============================================================="
Write-Host "Issue: $CurrentIssue - $IssueTitle"
Write-Host "Saved to: .beads\last-handoff.md"
Write-Host "Clipboard: $ClipboardStatus"
Write-Host "==============================================================="
```

---

## Output Format

The generated handoff prompt follows this structure:

```markdown
# Session Handoff

**Generated:** <timestamp>

---

## Continue work on bd-{id}: {title}

### Context
{issue description}

### Status
{current status}

### Blockers
{blocking issues or "None"}

### Blocking (issues waiting on this)
{issues blocked by this or "None"}

### Last Commit
{git log -1 --oneline}

### Recent Activity
{git log --oneline -5}

### Uncommitted Changes
{git status --short or "None"}

---

## Next Step
Review the issue context above and continue implementation.

## Quick Start
```bash
git pull && bd sync && bd show {issue-id}
```
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.handoff.activated` | Skill loads | Handoff starting |
| `sk.handoff.issue` | Issue context loaded | Issue details retrieved |
| `sk.handoff.git` | Git history gathered | Commits and status captured |
| `sk.handoff.deps` | Dependencies gathered | Blockers analyzed |
| `sk.handoff.generate` | Prompt generated | Written to file |
| `sk.handoff.complete` | Handoff finished | Clipboard status |
| `sk.handoff.error` | Error occurred | No issue found, etc. |

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| No current issue | Error message, suggest `bd ready` |
| Issue not found | Error message, suggest `bd list` |
| Git errors | Include "No commits" in handoff |
| Clipboard failure | Log warning, continue with file output |

---

## Integration with Other Skills

- **beads-bootup**: Starts the session, sets current issue
- **beads-scope**: Monitors adherence to selected issue
- **beads-landing**: Calls handoff as Phase 6 of landing ritual
- **beads-handoff**: Generates the handoff prompt (this skill)

---

## Standalone vs. Integrated Usage

This skill can be invoked:
1. **Standalone**: Load skill directly for ad-hoc handoff generation
2. **Integrated**: Called by beads-landing as part of session close ritual

Both modes use the same logic; the difference is triggering context.

---

## Quick Usage

```bash
# Generate handoff for current issue
cat .beads/current-issue              # Verify current issue
# (load this skill)                   # Generates handoff

# View last handoff
cat .beads/last-handoff.md

# Copy handoff to clipboard manually (if auto-copy failed)
cat .beads/last-handoff.md | clip     # Windows
cat .beads/last-handoff.md | pbcopy   # macOS
cat .beads/last-handoff.md | xclip -selection clipboard  # Linux
```

---

**STATUS: ACTIVE** (bd-x2wv) - Full 5-phase handoff generation implemented.

**PURPOSE:** Ensure seamless continuity between sessions by capturing
exactly what the next session needs to know to resume work.

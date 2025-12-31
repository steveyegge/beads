---
name: beads-handoff
description: |
  Generates structured handoff prompts at session end for seamless continuity.
  Captures current issue context, git history, blockers, and next steps into
  a formatted prompt that can be used to resume work in the next session.
  Trigger with "generate handoff", "end session", "create handoff prompt",
  "session handoff", or "prepare for next session".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(cat:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Handoff Skill

> **STATUS: GREEN FIELD - LOGGING ONLY**
> This skill announces its activation but performs no processing yet.

<!--
## IMPLEMENTATION PLAN

### Phase 1: Issue Context Loading
- [ ] Read `.beads/current-issue` to get current issue ID
- [ ] Fallback: Query `bd list --status=in_progress` for first match
- [ ] If no issue found, log error and exit gracefully
- [ ] Run `bd show <id> --json` to get full issue details
- [ ] Extract: title, description, status, notes
- [ ] Log `sk.handoff.issue` event

### Phase 2: Git History Gathering
- [ ] Run `git log -1 --oneline` for last commit
- [ ] Run `git log --oneline -5` for recent context
- [ ] Run `git status --short` for uncommitted changes (first 10 lines)
- [ ] Log `sk.handoff.git` event

### Phase 3: Dependency Analysis
- [ ] Extract `blocked_by` from issue JSON
- [ ] Extract `blocking` from issue JSON
- [ ] Format as bullet lists or "None"
- [ ] Log `sk.handoff.deps` event

### Phase 4: Prompt Generation
- [ ] Assemble handoff template with gathered data
- [ ] Include: Issue ID, Title, Context, Status, Notes, Blockers, Last Commit, Next Step
- [ ] Output to console
- [ ] Write to `.beads/last-handoff.txt`
- [ ] Log `sk.handoff.generate` event

### Phase 5: Clipboard Integration (Platform-Aware)
- [ ] Windows: Use `Set-Clipboard` (PowerShell) or `clip` (cmd)
- [ ] macOS: Use `pbcopy`
- [ ] Linux: Try `xclip -selection clipboard` or `xsel --clipboard`
- [ ] Log success/failure, don't fail skill if clipboard unavailable
- [ ] Log `sk.handoff.complete` event

### Output Format
```
═══════════════════════════════════════════════════════════════
HANDOFF GENERATED
═══════════════════════════════════════════════════════════════

Continue work on bd-XXXX: <title>

Context: <issue description>

Status: <current status>
Notes: <progress notes>

Blockers: <blocking issues or "None">

Last commit: <git log -1 --oneline>

Next step: Review the issue and continue implementation.

═══════════════════════════════════════════════════════════════
Saved to: .beads/last-handoff.txt
Clipboard: Copied (or "Not available")
═══════════════════════════════════════════════════════════════
```

### Error Handling
- [ ] No current issue: Output error message, suggest `bd ready`
- [ ] Issue not found: Output error message, suggest `bd list`
- [ ] Git errors: Include "Git info unavailable" in handoff
- [ ] Clipboard failure: Log warning, continue with file output

### Dependencies
- Requires: Event logging infrastructure
- Requires: `.beads/current-issue` (from bootup/scope)
- Requires: `bd` CLI with show command and JSON output
- Called by: beads-landing (Step 5)

### Verification Criteria
- [ ] Issue context loaded correctly
- [ ] Git history included in handoff
- [ ] Dependencies formatted correctly
- [ ] Handoff written to `.beads/last-handoff.txt`
- [ ] Clipboard copy attempted on supported platforms
- [ ] All events logged to `.beads/events.log`

### Standalone vs. Integrated Usage
This skill can be invoked:
1. **Standalone**: Agent/user loads skill directly for ad-hoc handoff
2. **Integrated**: Called by beads-landing as part of session close ritual

Both modes use the same logic; the difference is triggering context.
-->


## Purpose

The handoff skill generates a structured handoff prompt at session end.
It captures context for the next session to continue work seamlessly.

**Use this skill when:**
- You are ending a session with work in progress
- You need to hand off an issue to another session
- You want to capture current state for continuity

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
./scripts/beads-log-event.sh sk.handoff.activated

# Or PowerShell
.\scripts\beads-log-event.ps1 -EventCode sk.handoff.activated
```

Then output exactly:

```
===================================================================
SKILL ACTIVATED: beads-handoff
STATUS: Logging only - no processing implemented
EVENT: sk.handoff.activated logged to .beads/events.log
===================================================================

Handoff generation steps (NOT YET ACTIVE):
1. [ ] Read current issue context
2. [ ] Get recent git history
3. [ ] Gather blockers and dependencies
4. [ ] Generate formatted handoff prompt
5. [ ] Optionally copy to clipboard

Output format:
  Continue work on bd-{id}: {title}
  Context: {issue description}
  Status: {current status and notes}
  Blockers: {any blocking issues}
  Last commit: {git log -1 --oneline}
  Next step: {what to do next}

NEXT: Verify event appears in .beads/events.log
```

---

## Processing Steps (DEFINED BUT NOT ACTIVE)

These steps will be implemented after green field validation:

### Step 1: Read Current Issue Context

```bash
# Bash
CURRENT_ISSUE=$(cat .beads/current-issue 2>/dev/null || echo "")
if [ -z "$CURRENT_ISSUE" ]; then
  # Fall back to in_progress issues
  CURRENT_ISSUE=$(bd list --status=in_progress --json 2>/dev/null | jq -r '.[0].id // empty')
fi

if [ -z "$CURRENT_ISSUE" ]; then
  ./scripts/beads-log-event.sh sk.handoff.error none "No current issue found"
  echo "ERROR: No current issue found. Cannot generate handoff."
  exit 1
fi

ISSUE_JSON=$(bd show "$CURRENT_ISSUE" --json)
ISSUE_TITLE=$(echo "$ISSUE_JSON" | jq -r '.title')
ISSUE_DESC=$(echo "$ISSUE_JSON" | jq -r '.description // "No description"')
ISSUE_STATUS=$(echo "$ISSUE_JSON" | jq -r '.status')
ISSUE_NOTES=$(echo "$ISSUE_JSON" | jq -r '.notes // "No notes"')

./scripts/beads-log-event.sh sk.handoff.issue "$CURRENT_ISSUE" "Context loaded"

# Or PowerShell
$currentIssue = if (Test-Path .beads/current-issue) {
    Get-Content .beads/current-issue
} else {
    $null
}
if (-not $currentIssue) {
    $inProgress = bd list --status=in_progress --json 2>$null | ConvertFrom-Json
    $currentIssue = $inProgress[0].id
}

if (-not $currentIssue) {
    .\scripts\beads-log-event.ps1 -EventCode sk.handoff.error -Details "No current issue found"
    Write-Host "ERROR: No current issue found. Cannot generate handoff."
    exit 1
}

$issueJson = bd show $currentIssue --json | ConvertFrom-Json
$issueTitle = $issueJson.title
$issueDesc = if ($issueJson.description) { $issueJson.description } else { "No description" }
$issueStatus = $issueJson.status
$issueNotes = if ($issueJson.notes) { $issueJson.notes } else { "No notes" }

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.issue -IssueId $currentIssue -Details "Context loaded"
```

### Step 2: Get Recent Git History

```bash
# Bash
LAST_COMMIT=$(git log -1 --oneline 2>/dev/null || echo "No commits")
RECENT_COMMITS=$(git log --oneline -5 2>/dev/null || echo "No commits")
UNCOMMITTED=$(git status --short 2>/dev/null | head -10)

./scripts/beads-log-event.sh sk.handoff.git none "Git history gathered"

# Or PowerShell
$lastCommit = git log -1 --oneline 2>$null
if (-not $lastCommit) { $lastCommit = "No commits" }
$recentCommits = git log --oneline -5 2>$null
$uncommitted = git status --short 2>$null | Select-Object -First 10

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.git -Details "Git history gathered"
```

### Step 3: Gather Blockers and Dependencies

```bash
# Bash
BLOCKERS=$(bd show "$CURRENT_ISSUE" --json | jq -r '.blocked_by // [] | .[] | "- \(.)"' 2>/dev/null)
if [ -z "$BLOCKERS" ]; then
  BLOCKERS="None"
fi

BLOCKING=$(bd show "$CURRENT_ISSUE" --json | jq -r '.blocking // [] | .[] | "- \(.)"' 2>/dev/null)
if [ -z "$BLOCKING" ]; then
  BLOCKING="None"
fi

./scripts/beads-log-event.sh sk.handoff.deps "$CURRENT_ISSUE" "Dependencies gathered"

# Or PowerShell
$blockers = $issueJson.blocked_by | ForEach-Object { "- $_" }
if (-not $blockers) { $blockers = "None" }

$blocking = $issueJson.blocking | ForEach-Object { "- $_" }
if (-not $blocking) { $blocking = "None" }

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.deps -IssueId $currentIssue -Details "Dependencies gathered"
```

### Step 4: Generate Formatted Handoff Prompt

```bash
# Bash
HANDOFF=$(cat << EOF
Continue work on ${CURRENT_ISSUE}: ${ISSUE_TITLE}

Context: ${ISSUE_DESC}

Status: ${ISSUE_STATUS}
Notes: ${ISSUE_NOTES}

Blockers: ${BLOCKERS}

Last commit: ${LAST_COMMIT}

Next step: Review the issue and continue implementation.
EOF
)

echo "$HANDOFF"
echo "$HANDOFF" > .beads/last-handoff.txt

./scripts/beads-log-event.sh sk.handoff.generate "$CURRENT_ISSUE" "Handoff prompt generated"

# Or PowerShell
$handoff = @"
Continue work on ${currentIssue}: ${issueTitle}

Context: ${issueDesc}

Status: ${issueStatus}
Notes: ${issueNotes}

Blockers: $($blockers -join "`n")

Last commit: ${lastCommit}

Next step: Review the issue and continue implementation.
"@

Write-Host $handoff
$handoff | Out-File -FilePath .beads/last-handoff.txt -Encoding UTF8

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.generate -IssueId $currentIssue -Details "Handoff prompt generated"
```

### Step 5: Optionally Copy to Clipboard

```bash
# Bash (requires xclip or xsel on Linux, pbcopy on macOS)
if command -v pbcopy &> /dev/null; then
  echo "$HANDOFF" | pbcopy
  echo "Copied to clipboard (macOS)"
elif command -v xclip &> /dev/null; then
  echo "$HANDOFF" | xclip -selection clipboard
  echo "Copied to clipboard (Linux/xclip)"
elif command -v xsel &> /dev/null; then
  echo "$HANDOFF" | xsel --clipboard
  echo "Copied to clipboard (Linux/xsel)"
else
  echo "Clipboard not available. Handoff saved to .beads/last-handoff.txt"
fi

./scripts/beads-log-event.sh sk.handoff.complete "$CURRENT_ISSUE" "Handoff complete"

# Or PowerShell
$handoff | Set-Clipboard
Write-Host "Copied to clipboard (Windows)"

.\scripts\beads-log-event.ps1 -EventCode sk.handoff.complete -IssueId $currentIssue -Details "Handoff complete"
```

---

## Output Format

The generated handoff prompt follows this structure:

```
Continue work on bd-{id}: {title}

Context: {issue description}

Status: {current status}
Notes: {progress notes}

Blockers: {blocking issues or "None"}

Last commit: {git log -1 --oneline}

Next step: Review the issue and continue implementation.
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.handoff.activated` | Skill loads | Always |
| `sk.handoff.issue` | Issue context loaded | Future |
| `sk.handoff.git` | Git history gathered | Future |
| `sk.handoff.deps` | Dependencies gathered | Future |
| `sk.handoff.generate` | Prompt generated | Future |
| `sk.handoff.complete` | Handoff finished | Future |
| `sk.handoff.error` | Error occurred | Future |

---

## Integration with Other Skills

**beads-bootup:** Starts the session, sets current issue
**beads-scope:** Monitors adherence to selected issue
**beads-landing:** Calls handoff as Step 5 of landing ritual
**beads-handoff:** Generates the handoff prompt for next session

---

## Quick Usage

When you need a handoff prompt:

1. Invoke in VS Code chat: "Load beads-handoff skill"
2. The skill reads current issue context
3. Gathers git history and blockers
4. Outputs formatted handoff prompt
5. Copies to clipboard if available

---

**GREEN FIELD STATUS:** This skill only logs activation.
Processing will be enabled once event logging is verified working.

**PURPOSE:** Ensure seamless continuity between sessions by capturing
exactly what the next session needs to know to resume work.

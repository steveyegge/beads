---
name: beads-circuit-breaker
description: |
  Prevents wasted time on minor issues with no functional impact. After 3 failed
  attempts to fix a non-critical issue, documents the situation in BLOCKED_N.md,
  creates a low-priority beads issue, and resumes primary work. Trigger with
  "I've tried this multiple times", "this keeps failing", "should I move on",
  "stuck on minor issue", or "circuit breaker".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(mkdir:*),Bash(cat:*),Bash(ls:*),Bash(sed:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Circuit Breaker Skill

> **STATUS: ACTIVE**
> This skill prevents wasted time on minor issues with no functional impact.

## Purpose

After 3 failed attempts to fix a minor issue, this skill:
1. Documents the situation in `BLOCKED_N.md`
2. Creates a low-priority beads issue for later review
3. Tells the agent to move on with primary work

## When to Invoke

Trigger this skill when ALL of these are true:
- [ ] You've made 3+ attempts to fix an issue
- [ ] The issue has NO functional impact (work can proceed)
- [ ] No data loss risk
- [ ] Tests still pass (or N/A)
- [ ] Continuing would waste more time than it's worth

## Activation

When this skill is loaded, IMMEDIATELY execute:

### Step 1: Find Next Number

```bash
# Bash
NEXT_NUM=$(ls BLOCKED_*.md 2>/dev/null | sed 's/BLOCKED_//' | sed 's/.md//' | sort -n | tail -1)
if [ -z "$NEXT_NUM" ]; then
  NEXT_NUM=1
else
  NEXT_NUM=$((NEXT_NUM + 1))
fi
echo "Creating BLOCKED_${NEXT_NUM}.md"

# Or PowerShell
$files = Get-ChildItem -Filter "BLOCKED_*.md" -ErrorAction SilentlyContinue
if ($files) {
    $nums = $files | ForEach-Object { [int]($_.Name -replace 'BLOCKED_|.md','') }
    $nextNum = ($nums | Measure-Object -Maximum).Maximum + 1
} else {
    $nextNum = 1
}
Write-Host "Creating BLOCKED_$nextNum.md"
```

### Step 2: Create Summary File

**IMPORTANT**: Fill in the placeholders [Brief Issue Title], [What is the problem?], etc. with actual details from the current situation.

```bash
# Bash
cat > "BLOCKED_${NEXT_NUM}.md" << EOF
# Circuit Breaker: [Brief Issue Title]

**Date**: $(date -u +%Y-%m-%d)
**Session**: [Session ID if available]
**Agent**: [Agent name/ID]

## Issue
[What is the problem?]

## Impact Assessment
- [ ] No functional impact
- [ ] No data loss
- [ ] Tests passing (or N/A)
- [ ] Work can proceed

## Attempts Made
1. [First attempt and result]
2. [Second attempt and result]
3. [Third attempt and result]

## Current Workaround
[How can work proceed despite this issue?]

## Recommendation
[ ] Fix later when not blocking
[ ] Ignore (cosmetic only)
[ ] Investigate root cause when time permits
[ ] Consider upstream bug report

## Related Links
- Beads issue: [Created below]
- Session transcript: [If available]
EOF

./scripts/beads-log-event.sh ss.circuitbreaker.triggered "none" "BLOCKED_${NEXT_NUM}.md created"

# Or PowerShell
@"
# Circuit Breaker: [Brief Issue Title]

**Date**: $(Get-Date -Format "yyyy-MM-dd")
**Session**: [Session ID if available]
**Agent**: [Agent name/ID]

## Issue
[What is the problem?]

## Impact Assessment
- [ ] No functional impact
- [ ] No data loss
- [ ] Tests passing (or N/A)
- [ ] Work can proceed

## Attempts Made
1. [First attempt and result]
2. [Second attempt and result]
3. [Third attempt and result]

## Current Workaround
[How can work proceed despite this issue?]

## Recommendation
[ ] Fix later when not blocking
[ ] Ignore (cosmetic only)
[ ] Investigate root cause when time permits
[ ] Consider upstream bug report

## Related Links
- Beads issue: [Created below]
- Session transcript: [If available]
"@ | Out-File -FilePath "BLOCKED_$nextNum.md" -Encoding UTF8

.\scripts\beads-log-event.ps1 -EventCode ss.circuitbreaker.triggered -Details "BLOCKED_$nextNum.md created"
```

### Step 3: Create Beads Issue

```bash
# Bash
ISSUE_ID=$(bd create \
  --title="Circuit Breaker ${NEXT_NUM}: [Brief Title]" \
  --type=task \
  --priority=3 \
  --description="See BLOCKED_${NEXT_NUM}.md for details.

This issue was deferred by the circuit breaker skill after 3 failed
attempts with no functional impact. Review when time permits." \
  --json | jq -r '.id')

# Update BLOCKED_N.md with issue ID
sed -i "s/\[Created below\]/$ISSUE_ID/" "BLOCKED_${NEXT_NUM}.md"

./scripts/beads-log-event.sh ss.circuitbreaker.deferred "$ISSUE_ID" "Issue created for BLOCKED_${NEXT_NUM}.md"

# Or PowerShell
$result = bd create `
  --title="Circuit Breaker $nextNum: [Brief Title]" `
  --type=task `
  --priority=3 `
  --description="See BLOCKED_$nextNum.md for details.

This issue was deferred by the circuit breaker skill after 3 failed
attempts with no functional impact. Review when time permits." `
  --json | ConvertFrom-Json

$issueId = $result.id

# Update BLOCKED_N.md with issue ID
(Get-Content "BLOCKED_$nextNum.md") -replace '\[Created below\]', $issueId | Set-Content "BLOCKED_$nextNum.md"

.\scripts\beads-log-event.ps1 -EventCode ss.circuitbreaker.deferred -IssueId $issueId -Details "Issue created for BLOCKED_$nextNum.md"
```

### Step 4: Git Track the Summary

```bash
# Bash
git add "BLOCKED_${NEXT_NUM}.md"
echo "✓ BLOCKED_${NEXT_NUM}.md added to git staging"

# Or PowerShell
git add "BLOCKED_$nextNum.md"
Write-Host "✓ BLOCKED_$nextNum.md added to git staging"
```

### Step 5: Output and Resume

```bash
./scripts/beads-log-event.sh ss.circuitbreaker.resume "none" "Returning to primary work"

# Or PowerShell
.\scripts\beads-log-event.ps1 -EventCode ss.circuitbreaker.resume -Details "Returning to primary work"
```

Then output exactly:

```
═══════════════════════════════════════════════════════════════
CIRCUIT BREAKER ACTIVATED
═══════════════════════════════════════════════════════════════

After 3 failed attempts, this issue has been deferred:

📄 Summary: BLOCKED_[N].md
🎫 Issue:   [ISSUE_ID] (priority 3)
📊 Events:  Logged to .beads/events.log

This issue has NO functional impact. Work can proceed.

✅ RESUMING PRIMARY WORK
═══════════════════════════════════════════════════════════════
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `ss.circuitbreaker.triggered` | Skill loads | BLOCKED_N.md filename |
| `ss.circuitbreaker.deferred` | Issue created | Beads issue ID |
| `ss.circuitbreaker.resume` | Before returning to work | Always |

---

## Example Usage

**When stuck on a minor issue:**

1. Recognize the pattern: "I've tried this 3 times, it's not critical, I should move on"
2. Invoke this skill in VS Code chat: "Load beads-circuit-breaker skill"
3. Fill in the placeholders in BLOCKED_N.md with actual details
4. The skill creates the markdown file, beads issue, logs events, and stages the file
5. Resume primary work

---

**STATUS:** This skill is ACTIVE and functional.

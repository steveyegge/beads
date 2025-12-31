---
name: beads-landing
description: |
  Executes the mandatory session end ritual. Files discovered work, runs quality
  gates (tests), updates beads state, syncs to git, and generates handoff prompt.
  The landing ritual is NON-NEGOTIABLE - session has not ended until git push
  completes. Trigger with "end session", "landing", "session end", "wrap up",
  "finish work", or "close session".
allowed-tools: "Read,Bash(bd:*),Bash(git:*),Bash(mkdir:*),Bash(cat:*)"
version: "0.1.0"
author: "justSteve <https://github.com/justSteve>"
license: "MIT"
---

# Beads Landing Skill

> **STATUS: GREEN FIELD - LOGGING ONLY**
> This skill announces its activation but performs no processing yet.

<!--
## IMPLEMENTATION PLAN

### Phase 1: Session Validation
- [ ] Verify `.beads/.session-active` exists (warn if missing)
- [ ] Read `.beads/current-issue` to get selected issue
- [ ] Log `ss.landing.start` event

### Phase 2: File Discovered Work
- [ ] Prompt agent/user: "Any discovered work to file?"
- [ ] For each discovered item, run `bd create --discovered-from=<current-issue>`
- [ ] Log `ss.discovered` event for each filed issue
- [ ] Skip if no discoveries reported

### Phase 3: Quality Gates
- [ ] Detect test command from CLAUDE.md or common patterns
- [ ] Run test suite (`go test`, `npm test`, `pytest`, etc.)
- [ ] Run linter if configured
- [ ] Log `ss.landing.test` with PASSED/FAILED result
- [ ] If FAILED: Block issue closure, update issue with failure notes

### Phase 4: Update Beads State
- [ ] If tests passed and work complete: `bd close <issue> --reason="<summary>"`
- [ ] If work incomplete: `bd update <issue> --notes="<progress>"`
- [ ] Log `bd.issue.close` or `bd.issue.update` events
- [ ] Log `ss.landing.update` event

### Phase 5: Sync and Push (MANDATORY - MUST SUCCEED)
- [ ] Execute `bd sync` - handle conflicts by preferring remote
- [ ] Log `bd.sync.complete` or `bd.sync.conflict` events
- [ ] Execute `git push` - if rejected, `git pull --rebase && git push`
- [ ] Log `gt.push.complete` or `gt.push.reject` events
- [ ] Verify with `git status` shows "up to date"
- [ ] Log `ss.landing.sync` event
- [ ] FAIL LOUDLY if push does not succeed after retry

### Phase 6: Generate Handoff
- [ ] Invoke beads-handoff skill to generate handoff prompt
- [ ] Write to `.beads/last-handoff.md`
- [ ] Log `ss.landing.handoff` event
- [ ] Clean up `.beads/.session-active` marker
- [ ] Log `ss.landing.complete` and `ss.end` events

### Conflict Resolution Strategy
```bash
# If bd sync fails with conflict:
git checkout --theirs .beads/beads.jsonl
bd import -i .beads/beads.jsonl
bd sync
```

### Output Format
```
═══════════════════════════════════════════════════════════════
LANDING COMPLETE
═══════════════════════════════════════════════════════════════
Issue: bd-XXXX - <status>
Tests: PASSED/FAILED
Discovered: N new issues filed
Sync: Complete
Push: Complete
Handoff: Generated at .beads/last-handoff.md
═══════════════════════════════════════════════════════════════
```

### Dependencies
- Requires: Event logging infrastructure
- Requires: beads-bootup to have set `.beads/current-issue`
- Requires: beads-handoff skill for Step 6
- Requires: beads-scope for discovered work tracking

### Verification Criteria
- [ ] Tests run and result logged
- [ ] Issue state updated correctly
- [ ] `bd sync` completes successfully
- [ ] `git push` completes successfully
- [ ] Handoff prompt generated
- [ ] Session marker cleaned up
- [ ] All events logged to `.beads/events.log`

### Non-Negotiable Enforcement
The landing ritual MUST complete successfully. If push fails after retries,
the skill should output:

```
🚨 LANDING FAILED - WORK NOT PUSHED 🚨
Manual intervention required. Run:
  git status
  git push origin <branch>
Do NOT end session until work is pushed.
```
-->


## Purpose

The landing skill executes at the END of every coding session.
It ensures clean state and enables session continuity.

**THIS RITUAL IS NON-NEGOTIABLE.**

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
# Create landing marker
mkdir -p .beads
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > .beads/.landing-complete
./scripts/beads-log-event.sh sk.landing.activated

# Or PowerShell
# Create landing marker
New-Item -ItemType Directory -Force -Path .beads | Out-Null
Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ" | Out-File -FilePath .beads\.landing-complete -NoNewline
.\scripts\beads-log-event.ps1 -EventCode sk.landing.activated
```

Then output exactly:

```
═══════════════════════════════════════════════════════════════
SKILL ACTIVATED: beads-landing
STATUS: Logging only - no processing implemented
EVENT: sk.landing.activated logged to .beads/events.log
═══════════════════════════════════════════════════════════════

Landing ritual steps (NOT YET ACTIVE):
1. [ ] File discovered work
2. [ ] Run quality gates
3. [ ] Update beads state
4. [ ] Sync and push (MUST SUCCEED)
5. [ ] Generate handoff

⚠️  DO NOT END SESSION WITHOUT COMPLETING LANDING

NEXT: Verify event appears in .beads/events.log
```

---

## Processing Steps (DEFINED BUT NOT ACTIVE)

These steps will be implemented after green field validation:

### Step 1: File Discovered Work
```bash
./scripts/beads-log-event.sh ss.landing.start

# For each discovered item during session:
bd create "Discovered: <description>" -t task --deps discovered-from:<current-issue>
./scripts/beads-log-event.sh ss.discovered <new-issue-id> "<description>"
```

### Step 2: Run Quality Gates
```bash
# Execute project-specific test/lint commands
# (Read from CLAUDE.md or project config)
npm test  # or go test, pytest, etc.
npm run lint

./scripts/beads-log-event.sh ss.landing.test none "tests passed"
# Or on failure:
./scripts/beads-log-event.sh ss.landing.test none "tests FAILED - do not close issue"
```

### Step 3: Update Beads State
```bash
# Close completed issues (only if tests passed!)
bd close <issue-id> --reason "<summary of work done>"
./scripts/beads-log-event.sh bd.issue.close <issue-id> "<summary>"

# Update work-in-progress
bd update <issue-id> --notes "<progress description>"
./scripts/beads-log-event.sh bd.issue.update <issue-id> "<progress>"

./scripts/beads-log-event.sh ss.landing.update
```

### Step 4: Sync and Push (MANDATORY)
```bash
bd sync
./scripts/beads-log-event.sh bd.sync.complete

git push
./scripts/beads-log-event.sh gt.push.complete

# Verify clean state
git status
# MUST show: "Your branch is up to date with 'origin/main'"

./scripts/beads-log-event.sh ss.landing.sync
```

### Step 5: Generate Handoff
```bash
# Create handoff document for next session
cat > .beads/last-handoff.md << EOF
# Session Handoff

**Date:** $(date -u +"%Y-%m-%dT%H:%M:%SZ")
**Issue:** <issue-id>
**Status:** <completed|in-progress>

## Work Done
<summary of changes>

## Next Steps
<recommended next actions>

## Start Command
\`\`\`bash
git pull && bd sync && bd show <issue-id> --json
\`\`\`
EOF

./scripts/beads-log-event.sh ss.landing.handoff <issue-id>
./scripts/beads-log-event.sh ss.landing.complete
./scripts/beads-log-event.sh ss.end
```

---

## Failure Handling (DEFINED BUT NOT ACTIVE)

If any step fails:

```bash
# DO NOT EXIT - attempt recovery

# If sync fails (conflict):
./scripts/beads-log-event.sh bd.sync.conflict none "merge conflict"
git checkout --theirs .beads/beads.jsonl
bd import -i .beads/beads.jsonl
bd sync

# If push fails (rejected):
./scripts/beads-log-event.sh gt.push.reject none "push rejected"
git pull --rebase
git push

# If tests fail:
./scripts/beads-log-event.sh ss.landing.test none "FAILED"
# DO NOT close issue - update with failure notes instead
bd update <issue-id> --notes "Landing blocked: tests failing"
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.landing.activated` | Skill loads | Always |
| `ss.landing.start` | Ritual begins | Future |
| `ss.discovered` | Work filed | Future |
| `ss.landing.test` | Tests run | Future |
| `ss.landing.update` | Beads updated | Future |
| `ss.landing.sync` | Sync complete | Future |
| `ss.landing.handoff` | Handoff created | Future |
| `ss.landing.complete` | Ritual finished | Future |
| `ss.end` | Session terminated | Future |
| `bd.sync.conflict` | Merge conflict | Future |
| `gt.push.reject` | Push rejected | Future |

---

**GREEN FIELD STATUS:** This skill only logs activation.
Processing will be enabled once event logging is verified working.

**CRITICAL:** The landing ritual is NON-NEGOTIABLE. A session that ends
without proper landing breaks continuity for the next session.

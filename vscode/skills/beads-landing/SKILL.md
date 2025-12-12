# Beads Landing Skill

> **STATUS: GREEN FIELD - LOGGING ONLY**
> This skill announces its activation but performs no processing yet.

## Purpose

The landing skill executes at the END of every coding session.
It ensures clean state and enables session continuity.

**THIS RITUAL IS NON-NEGOTIABLE.**

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
./scripts/beads-log-event.sh sk.landing.activated

# Or PowerShell
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

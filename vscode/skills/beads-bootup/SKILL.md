# Beads Bootup Skill

> **STATUS: GREEN FIELD - LOGGING ONLY**
> This skill announces its activation but performs no processing yet.

## Purpose

The bootup skill executes at the START of every coding session.
It grounds the agent in current state and selects work.

---

## Activation

When this skill is loaded, IMMEDIATELY execute:

```bash
# Bash
# Create session marker
mkdir -p .beads
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > .beads/.session-active
./scripts/beads-log-event.sh sk.bootup.activated

# Or PowerShell
# Create session marker
New-Item -ItemType Directory -Force -Path .beads | Out-Null
Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ" | Out-File -FilePath .beads\.session-active -NoNewline
.\scripts\beads-log-event.ps1 -EventCode sk.bootup.activated
```

Then output exactly:

```
═══════════════════════════════════════════════════════════════
SKILL ACTIVATED: beads-bootup
STATUS: Logging only - no processing implemented
EVENT: sk.bootup.activated logged to .beads/events.log
═══════════════════════════════════════════════════════════════

Bootup ritual steps (NOT YET ACTIVE):
1. [ ] Ground - confirm working directory
2. [ ] Sync - pull remote state
3. [ ] Orient - read domain memory
4. [ ] Select - choose ONE issue
5. [ ] Verify - run health check

Guard check (NOT YET ACTIVE):
- [ ] Is InitApp (bd-0001) closed?
- [ ] If not, only InitApp children are workable

NEXT: Verify event appears in .beads/events.log
```

---

## Processing Steps (DEFINED BUT NOT ACTIVE)

These steps will be implemented after green field validation:

### Step 1: Ground
```bash
pwd
./scripts/beads-log-event.sh ss.bootup.ground
```

### Step 2: Sync
```bash
git pull --rebase
bd sync
./scripts/beads-log-event.sh ss.bootup.sync
```

### Step 3: Orient
```bash
bd ready --json
bd list --status in_progress
git log --oneline -5
./scripts/beads-log-event.sh ss.bootup.orient
```

### Step 4: Select
```bash
# Present ready issues to user
# Wait for selection
bd show <selected-id> --json
./scripts/beads-log-event.sh ss.bootup.select <selected-id>
```

### Step 5: Verify
```bash
# Run project health check (tests, build)
./scripts/beads-log-event.sh ss.bootup.verify
./scripts/beads-log-event.sh ss.bootup.complete
```

---

## Guard: InitApp Check (DEFINED BUT NOT ACTIVE)

Before processing, check epoch status:

```bash
# Check if InitApp exists and is open
bd show bd-0001 --json 2>/dev/null

# If status != "closed":
./scripts/beads-log-event.sh gd.initapp.check bd-0001
./scripts/beads-log-event.sh gd.initapp.blocked bd-0001 "InitApp not complete"
# Output: "⛔ InitApp is not complete. Only InitApp children are workable."
# Filter bd ready to show only InitApp children

# If status == "closed":
./scripts/beads-log-event.sh gd.initapp.passed bd-0001 "Epoch established"
# Proceed with normal bootup
```

---

## Events Emitted

| Event Code | When | Details |
|------------|------|---------|
| `sk.bootup.activated` | Skill loads | Always |
| `ss.bootup.ground` | After pwd | Future |
| `ss.bootup.sync` | After sync | Future |
| `ss.bootup.orient` | After bd ready | Future |
| `ss.bootup.select` | Issue chosen | Future |
| `ss.bootup.verify` | Health check done | Future |
| `ss.bootup.complete` | Ritual finished | Future |
| `gd.initapp.check` | Checking epoch | Future |
| `gd.initapp.blocked` | InitApp open | Future |
| `gd.initapp.passed` | InitApp closed | Future |

---

**GREEN FIELD STATUS:** This skill only logs activation.
Processing will be enabled once event logging is verified working.

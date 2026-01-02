# CLAUDE.md - Beads-First Application Protocol

> **This project follows the Beads-First discipline.**
> All work is tracked via beads. All sessions follow rituals.

## Epoch Status

| Field | Value |
|-------|-------|
| **InitApp Epic** | `bd-0001` |
| **Status** | ⏳ IN PROGRESS |
| **Epoch Tag** | Not yet created |
| **Event Log** | `.beads/events.log` |

> ⚠️ **IF INITAPP IS NOT CLOSED, ALL WORK IS BLOCKED.**
> Run `bd show bd-0001 --json` to check status.
> Only InitApp child tasks are workable until the Epoch is established.

---

## Mandatory Skills

This project uses Claude Skills located in `.claude/skills/`.

| Skill | Purpose | When | Status |
|-------|---------|------|--------|
| `beads-bootup` | Session start ritual | EVERY session start | Green Field |
| `beads-landing` | Session end ritual | EVERY session end | Green Field |
| `beads-scope` | One-issue discipline | During all work | Green Field |
| `beads-init-app` | Project initialization | Once per project | Active |

**GREEN FIELD** means the skill logs its activation but doesn't process yet.

---

## Session Protocol

### BEFORE ANY WORK

1. **Load beads-bootup skill**
2. **Check InitApp status:** `bd show bd-0001 --json`
3. **Run bootup ritual** (ground, sync, orient, select, verify)
4. **Verify event logged:** `tail -1 .beads/events.log`

### DURING WORK

1. **ONE issue only** - from `bd ready`
2. **Commit atomically** - format: `bd-XXXX: description`
3. **File discoveries:**
   ```bash
   bd create "Discovered: <desc>" -t task --deps discovered-from:<current>
   ```

### BEFORE ENDING SESSION

1. **Load beads-landing skill**
2. **Complete ALL steps** - no shortcuts
3. **Sync must succeed:** `bd sync && git push`
4. **Verify:** `git status` shows "up to date"

---

## Event Logging

Log events via:
```bash
./scripts/beads-log-event.sh EVENT_CODE [ISSUE_ID] [DETAILS]
```

See `events/EVENT_TAXONOMY.md` for codes.

---

## Project Configuration

| Setting | Value |
|---------|-------|
| Test command | `___` |
| Lint command | `___` |
| Build command | `___` |
| Primary language | `___` |

### VS Code Tasks Integration

Health check tasks are defined in `.vscode/tasks.json`:

| Task | Purpose | When to Run |
|------|---------|-------------|
| `Health: Doctor` | Run `bd doctor` health checks | Session bootup |
| `Health: Doctor (Fix)` | Auto-fix detected issues | When doctor reports problems |
| `Health: Test` | Run project tests | Before landing/closing issues |
| `Health: Lint` | Run linter | Before landing/closing issues |
| `Health: Build` | Verify project builds | Before landing/closing issues |
| `Health: Quality Gates (All)` | Run all checks in sequence | Session landing |
| `Session: Bootup Health Check` | Quick health check (silent) | Auto-run on folder open |
| `Session: Landing Quality Gates` | Build + Test before push | Before git push |

**Run tasks via:** `Ctrl+Shift+P` -> `Tasks: Run Task` -> Select task

**Keybindings:**
- `Ctrl+Shift+R` (terminal): `bd ready --json`
- `Ctrl+Shift+S` (terminal): `bd sync && git status`
- `Ctrl+Shift+D` (terminal): `bd doctor`
- `Ctrl+Shift+Q` (anywhere): Run all quality gates

---

**This protocol is NON-NEGOTIABLE.**

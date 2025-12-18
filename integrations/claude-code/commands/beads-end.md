# End Session - Land the Plane

## description:
Complete session: verify work, update statuses, sync to git, recommend next steps.

---

## Session Completion Protocol

Based on [Anthropic's long-running agent patterns](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents).

> "The plane has NOT landed until `git push` completes successfully."

### Step 1: Verify Completed Work

**Before closing ANY task, verify it works:**

```bash
# Run project tests
uv run pytest -v          # Python
npm test                  # Node.js
# or project-specific test command
```

**Rules:**
- Only close tasks where tests pass
- If tests fail, leave task as `in_progress`
- If partially done, add comment with progress notes

### Step 2: Review In-Progress Tasks

```bash
bd list --status in_progress
```

For each in-progress task, decide:

| Status | Action |
|--------|--------|
| **Completed + verified** | `bd close <id> --reason "description"` |
| **Completed, not verified** | Leave in_progress, add comment |
| **Partially done** | Leave in_progress, add progress comment |
| **Blocked** | `bd update <id> --status blocked`, create blocker issue |
| **Won't do** | `bd close <id> --reason "won't do: reason"` |

### Step 3: File Discovered Issues

For any bugs, TODOs, or improvements found during the session:

```bash
bd create "Issue title" \
  -d "What was discovered, why it matters, how to reproduce" \
  -p [priority] \
  --json
```

**Don't lose context:** If you discovered something, file it now.

### Step 4: Add Blocking Dependencies

If a task is blocked by something:

```bash
# Create the blocker
bd create "Blocking issue" -d "details" --json
# Link it
bd dep add <blocked-task> <blocker-task>
```

### Step 5: Sync to Git

```bash
bd sync
```

This:
- Exports changes to JSONL
- Commits beads changes
- Pulls remote updates
- Pushes to origin

### Step 6: Verify Push Success

```bash
git status
```

**Must show:** "Your branch is up to date with 'origin/...'"

**If not up to date:**
- Resolve any conflicts
- Push again
- Do NOT end session until pushed

### Step 7: Recommend Next Session

```bash
bd ready --json
```

Present:
- What's now unblocked
- Highest priority ready task
- Any context needed for next session

---

## Output Format

```
Session End: [project name]

Completed this session:
  ✓ [.proj-xxx] Add login form
  ✓ [.proj-yyy] Fix validation

Still in progress:
  → [.proj-zzz] Add password reset (80% done)

New issues filed:
  + [.proj-aaa] Bug: email validation edge case

Sync status:
  ✓ Changes committed
  ✓ Pushed to origin
  ✓ Up to date

Next session recommendations:
  1. [.proj-zzz] Finish password reset (in progress)
  2. [.proj-bbb] Add session management (ready, P1)
```

---

## Critical Rules

1. **Never end session before push succeeds**
2. **Never close unverified tasks**
3. **Always file discovered issues**
4. **Always recommend next steps**

If offline or push fails:
```
⚠️  WARNING: Work is LOCAL ONLY
Push failed/skipped. Run `bd sync && git push` before next session.
```

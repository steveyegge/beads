# Start Beads Session

## description:
Session onboarding: verify environment, review history, select and claim a task.

---

Use the **Task tool** with `subagent_type='general-purpose'` to perform session onboarding.

## Agent Instructions

The agent should:

1. **Verify environment**
   - Run `pwd` and `git status`
   - Check for uncommitted changes
   - If dirty, ask whether to commit, stash, or continue

2. **Check beads status**
   - Run `bd info 2>/dev/null || echo "NO_BEADS"`
   - If NO_BEADS, ask if user wants to run `bd init --quiet`

3. **Review recent history**
   - Run `git log --oneline -5`
   - Run `bd info --whats-new` if available

4. **Find ready work**
   - Run `bd ready --json`
   - Run `bd list --status in_progress --json`

5. **Return a concise summary** (not raw output):
   ```
   Session Start: [project name]
   Directory: [path]
   Git: [branch] - [clean/dirty]

   Recent commits: [1-line summary of last 3]

   Beads: [N] open, [M] ready, [K] in-progress

   Recommended task:
     [id] [priority] [title]
     [first 80 chars of description]

   To claim: bd update [id] --status in_progress
   ```

## After Agent Returns

If user confirms the recommended task, run:
```bash
bd update <id> --status in_progress
```

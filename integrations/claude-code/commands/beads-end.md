# End Beads Session

## description:
Session completion: verify work, update statuses, sync to git, recommend next steps.

---

Use the **Task tool** with `subagent_type='general-purpose'` to perform session completion.

## Agent Instructions

The agent should:

1. **Verify completed work**
   - Run project tests (`uv run pytest`, `npm test`, or detect from project)
   - Only close tasks if tests pass
   - If tests fail, note which tasks should stay in_progress

2. **Review in-progress tasks**
   - Run `bd list --status in_progress --json`
   - For each, determine: completed? partially done? blocked?

3. **Check for uncommitted changes**
   - Run `git status`
   - If changes exist, they need to be committed before sync

4. **Sync to git**
   - Run `bd sync`
   - Verify push succeeded

5. **Find next session work**
   - Run `bd ready --json`

6. **Return a concise summary** (not raw output):
   ```
   Session End: [project name]

   Tests: [passed/failed - brief summary]

   Completed:
     [x] [id] [title]

   Still in progress:
     [-] [id] [title] ([reason])

   Sync: [success/failed]

   Next session:
     [id] [priority] [title]
   ```

## After Agent Returns

If any tasks were completed and verified, run:
```bash
bd close <id> --reason "completed and verified"
```

If sync failed, resolve and run:
```bash
bd sync && git push
```

---
description: Landing and handoff agent for deterministic session closeout
---

You are the beads cleanup agent. You prepare safe, resumable landing state.

# Mission

- Ensure no ambiguous in-progress work is left behind.
- Ensure blocked/open work has context packs and clear next commands.
- Produce a handoff snapshot for the next session.

# Write Policy

- Use `flow` wrappers for any lifecycle changes.
- Do not call raw lifecycle write tools directly.

# Landing Checklist

1. Inspect active work (`list` for `in_progress`, `blocked`, `open`).
2. For completed but open work, close with `flow(action="close_safe", ...)`.
3. For stuck work, set blocked with `flow(action="block_with_context", ...)`.
4. Ensure verification evidence and safe close reasons are present.
5. Run deterministic landing gates with `bd land`:
   - Example full choreography:
     - `bd land --epic <epic-id> --require-quality --quality-summary "<tests|lint|build>" --require-handoff --next-prompt "<prompt>" --stash "<none|restore>" --pull-rebase --sync --push --json`
   - Treat `result=landed` as fully distributed closeout.
   - Treat `result=landed_with_skipped_gate3` as partial and include explicit Gate 3 skip rationale in handoff.
6. Return next-session prompt with top `ready` items.

# ABORT Runbook (Mandatory)

Use this path for unrecoverable conditions (security issue, wrong repo/branch, corrupted state):

1. If `bd` writes are safe, run:
   - `bd flow transition --type session_abort --issue "<id-or-empty>" --reason "<why>" --context "<state summary>" --abort-handoff ABORT_HANDOFF.md`
2. If `bd` writes are unsafe/unavailable, run no-write abort:
   - `bd flow transition --type session_abort --reason "<why>" --context "<state summary>" --abort-handoff ABORT_HANDOFF.md --abort-no-bd-write`
3. If `bd` cannot run at all, write `ABORT_HANDOFF.md` manually and stop lifecycle writes.
4. Ensure `ABORT_HANDOFF.md` includes:
   - reason
   - current state and touched files
   - exact recovery next commands

# Handoff Format

- `completed`: IDs + one-line summary
- `still_open`: IDs + blockers/context (context pack order: `state; repro; next; files; blockers`)
- `next_ready`: top queue candidates
- `next_prompt`: exact resume instruction
- `stash`: `none` or restore command

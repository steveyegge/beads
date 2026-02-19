# AGENTS.md - bd Control-Plane Bootloader

This file is the operating system for AI agent behavior in this repo.
It exists to enforce deterministic execution over probabilistic model behavior.

## Purpose

Use this workflow to prevent:
- intent drift
- scope creep
- context loss across sessions
- unverifiable completion claims
- coordination and dependency-state failures

Success means:
- deterministic state transitions in `bd`
- verification evidence recorded
- safe close reasons
- resumable handoff
- clean landing state

## Failure Patterns Mitigated

This file is designed to prevent these common agent failures:
- intent drift from user request
- unplanned scope expansion
- context loss across turns/sessions
- unverifiable "done" claims
- multi-agent overlap/race conditions
- task graph wiring errors (missing links/cycles/orphans)
- coding before intake mapping/audit gates
- unsafe closes without acceptance evidence
- dependency-state corruption from unsafe close reasons
- liveness stalls from deferred/blocked work not resurfacing
- interactive/destructive command misuse in automation
- weak handoff artifacts with no exact next command

## Authority Order

When instructions conflict, resolve in this order:
1. user instruction
2. `bd` source behavior and `bd <cmd> --help`
3. `docs/CONTROL_PLANE_CONTRACT.md`
4. split-agent docs (`claude-plugin/agents/*.md`, `docs/agents/*.md`)
5. this file

When conflict handling is required, record:
- `Conflict: <sources>; Resolution: <decision>`

## Ownership Boundary

- `bd` deterministic owner:
  - preflight gates
  - lifecycle transitions
  - intake audits
  - recovery loop/signature
  - landing gates
  - reason lint and result envelopes
- split-agent judgment owner:
  - decomposition strategy
  - prioritization and tradeoffs
  - architecture decisions
  - handoff writing quality

## Cold Start (Run First)

Before any claim or lifecycle write:

```bash
bd where
bd preflight gate --action claim --json
bd ready --limit 5 --json
```

If preflight does not return pass, do not claim or write. Remediate first.

## State Machine

Required execution order:

`BOOT -> PLANNING -> INTAKE -> EXECUTING <-> RECOVERING -> LANDING -> END`

Abort path is always available:

`* -> ABORT -> END`

Do not reorder stages.

## Mandatory Write Policy

For lifecycle transitions, use `bd flow` wrappers:
- `bd flow claim-next`
- `bd flow create-discovered`
- `bd flow block-with-context`
- `bd flow close-safe`
- `bd flow transition`

Do not use interactive lifecycle mutation paths.
Do not use `bd edit`.

## Deterministic Execution Loop

1. Select candidate from `bd ready`.
2. Run preclaim lint:
   - `bd flow preclaim-lint --issue <id>`
3. Claim:
   - `bd flow claim-next ...`
4. Execute and verify required behavior.
5. Close with strict checks:
   - `bd flow close-safe --issue <id> --reason "<safe reason>" --verification "<command+result>" --require-traceability --require-spec-drift-proof --require-priority-poll --require-parent-cascade`

Never claim tests/commands ran unless they were executed.

## Intake Hard Gate

If plan items are 2+ or decomposition creates 4+ tasks:
- complete mapping/audit before claim or coding
- require `INTAKE_AUDIT=PASS`

Use:

```bash
bd intake map-sync ...
bd intake audit --epic <id> --write-proof --json
```

## Recover Loop (When Scoped Ready Is Empty)

Run deterministic recovery before declaring idle:

```bash
bd recover loop --parent <epic-id> --module-label module/<name> --json
bd recover signature --parent <epic-id> --iteration <n> --elapsed-minutes <m> --json
```

Interpret results:
- `recover_ready_found` or `recover_ready_found_widened`: return to execute loop
- `recover_limbo_detected`: stay in recovery until resolved
- signature escalation required: escalate with explicit decision request

## Landing

Use deterministic landing engine:

```bash
bd land --epic <epic-id> --require-quality --quality-summary "<tests|lint|build>" --require-handoff --next-prompt "<prompt>" --stash "<none|restore>" --pull-rebase --sync --push --json
```

Result handling:
- `landed`: complete
- `landed_with_skipped_gate3`: partial; include explicit skip rationale in handoff

## Abort Runbook

For unrecoverable conditions:

1. Writable abort:
   - `bd flow transition --type session_abort --issue "<id-or-empty>" --reason "<why>" --context "<state summary>" --abort-handoff ABORT_HANDOFF.md`
2. No-write abort:
   - `bd flow transition --type session_abort --reason "<why>" --context "<state summary>" --abort-handoff ABORT_HANDOFF.md --abort-no-bd-write`
3. If `bd` cannot run:
   - write `ABORT_HANDOFF.md` manually with reason, state, touched files, exact recovery commands

## Close-Reason Safety

Success close reasons must be safe and deterministic.
Do not include failure-trigger keywords in success reasons.

Use:

```bash
bd reason lint --reason "<close reason>"
```

## Auditability Protocol

Minimum auditability requirements for every executed task:
- task exists in `bd` before execution
- task is linked to the correct parent outcome
- close path includes verification evidence
- close reason passes `bd reason lint`

For non-routine decisions (ambiguity, override, gate bypass, force-close), append:
- `Decision | Evidence | Risk | Follow-up ID: <decision> | <evidence> | <risk> | <id-or-none>`

For non-hermetic checks, append evidence tuple:
- `Evidence tuple: {ts:<UTC>, env:<env-id>, artifact:<id>}`

## Output Protocol

For claim/close/handoff/intake updates, report:
- `commands | verification (or skipped reason) | state changes | next action`
- include key assumptions and key files consulted when relevant

## Decision Request Template

When blocked >30 minutes or impact >1 hour, use:
- `Decision: <one-line choice>`
- `Blocker: <what cannot proceed>`
- `State: <current behavior + constraints>`
- `Options: A/B(/C) with benefit/cost`
- `Rec: <recommended option + rationale>`
- `Impact: <what changes>`
- `Need: <explicit A/B/C or missing info>`

## Handoff Contract

Always provide:
- commands executed
- verification evidence
- state changes
- next ready items
- next session prompt
- stash status (`none` or restore command)

For blocked/deferred work, include context pack order:
`state; repro; next; files; blockers`

## Operational Guardrails

- Track work in `bd`, not markdown TODO lists.
- Do not manually edit `.beads/issues.jsonl`.
- Prefer non-interactive shell flags (`-f`, `-y`, batch mode).
- Keep one active WIP per actor unless preempted by explicit policy.
- Preserve invariants: external contracts, data integrity, security boundaries.
- Internal refactors may break internal interfaces if invariants are preserved.

## Canonical References

- Deterministic control-plane contract: `docs/CONTROL_PLANE_CONTRACT.md`
- Coverage matrix (old AGENTS -> current owners): `docs/control-plane/agents-control-flow-matrix-2026-02-19.md`
- Split orchestrator: `claude-plugin/agents/task-agent.md`
- Split roles:
  - `claude-plugin/agents/beads-query-agent.md`
  - `claude-plugin/agents/beads-issue-manager-agent.md`
  - `claude-plugin/agents/beads-execution-coordinator-agent.md`
  - `claude-plugin/agents/beads-cleanup-agent.md`
- Detailed development guidance and test commands: `AGENT_INSTRUCTIONS.md`

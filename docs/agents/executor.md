# executor playbook

## Role
Claim ready work, implement the smallest scoped change, verify, and close with evidence.

## Boundary
- CLI is source of truth for deterministic claim/close transitions.
- Executor judgment covers debugging, decomposition choices, and communication only.
- Do not invent parallel policy paths outside `bd` command behavior.

## Executor Checklist
1. Pick work from `bd ready` and enforce single-WIP discipline.
2. Run pre-claim quality checks and baseline verification.
3. Implement scoped change and run task verify command.
4. Append verification evidence, then close with safe reason text.

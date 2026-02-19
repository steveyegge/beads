# planner playbook

## Role
Turn user intent into atomic `bd` tasks with dependency order and verification paths.

## Boundary
- CLI is source of truth for deterministic control-plane behavior.
- Do not duplicate command semantics in planning docs.
- Reference canonical commands/contracts instead of embedding policy scripts.

## Planner Checklist
1. Define outcome, modules, and milestone boundaries.
2. Create atomic child tasks with one objective and one verification path.
3. Wire dependencies (`blocks`, `parent-child`) before execution starts.
4. Validate intake/readiness gates before first claim.

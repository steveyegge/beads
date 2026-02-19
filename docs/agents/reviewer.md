# reviewer playbook

## Role
Evaluate correctness, risk, and contract conformance before merge/closeout.

## Boundary
- CLI is source of truth for deterministic policy enforcement and gate semantics.
- Reviewer judgment focuses on risk, missing tests, and unintended behavior changes.
- Keep recommendations aligned with canonical CLI contract and role boundaries.

## Reviewer Checklist
1. Confirm acceptance criteria are satisfied by observed behavior.
2. Verify evidence quality (commands run, output captured, notes updated).
3. Check dependency and close-reason safety impacts.
4. Flag residual risks and required follow-up issues.

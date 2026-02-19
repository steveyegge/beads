# Split-Agent Role Playbooks

This folder defines role-specific guidance for planner, executor, and reviewer work.

`bd` CLI deterministic control behavior is authoritative; role playbooks cover judgment, sequencing, and communication.

See:
- `docs/agents/planner.md`
- `docs/agents/executor.md`
- `docs/agents/reviewer.md`

## Shared Ownership Matrix

| Owner | Scope |
|---|---|
| `CLI-owned` | Deterministic enforcement: readiness truth, claim/write guards, intake audits, close safety, and policy-violation exit semantics. |
| `Split-agent-owned` | Judgment and strategy: intent translation, decomposition choices, prioritization/preemption decisions, and tradeoffs. |
| `MCP-owned` | Adapter translation only: map tool inputs/outputs to `bd` CLI calls and surface raw deterministic envelopes without policy forks. |

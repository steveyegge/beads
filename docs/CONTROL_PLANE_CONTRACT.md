# Control-Plane Contract (Generated)

> Source: `AGENTS.md` section `Control-Plane Contract (Inlined)`.
> Regenerate with `./scripts/generate_control_plane_contract.sh`.

Deterministic `bd` command surfaces:
- `flow claim-next`
- `flow create-discovered`
- `flow block-with-context`
- `flow close-safe`
- `flow transition`
- `intake audit`
- `intake map-sync`
- `intake planning-exit`
- `intake bulk-guard`
- `preflight gate`
- `preflight runtime-parity`
- `recover loop`
- `recover signature`
- `resume`
- `land`
- `reason lint`

All control-plane commands in JSON mode are expected to emit:

```json
{
  "ok": true,
  "command": "flow claim-next",
  "result": "claimed",
  "issue_id": "bd-123",
  "details": {},
  "recovery_command": "bd ready --limit 5",
  "events": ["claimed"]
}
```

Envelope fields:
- `ok`: boolean command outcome
- `command`: canonical command identifier
- `result`: deterministic result-state enum
- `issue_id`: issue identifier when relevant
- `details`: structured context payload
- `recovery_command`: remediation command when applicable
- `events`: deterministic event tags

Common result values (non-exhaustive):
- `claimed`
- `wip_blocked`
- `no_ready`
- `contention`
- `policy_violation`
- `partial_state`
- `invalid_input`
- `system_error`
- `gate_failed`
- `operation_failed`
- `check_passed`
- `landed_with_skipped_gate3`
- `ok`

Exit codes:
- `0`: success or non-fatal deterministic state outcome
- `1`: generic command/system error
- `3`: policy violation
- `4`: partial state

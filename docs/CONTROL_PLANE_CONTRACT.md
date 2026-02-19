# Control-Plane Contract (v1)

This document is the canonical contract for deterministic control-plane commands.
It defines the command map, JSON envelope shape, result-state semantics, and exit-code policy.

## Command Map

The following command surfaces are part of the v1 deterministic control plane:

- `flow claim-next`
- `flow create-discovered`
- `flow block-with-context`
- `flow close-safe`
- `intake audit`
- `resume`
- `land`
- `reason lint`

## JSON Envelope

Control-plane commands emit a stable envelope in JSON mode.

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

- `ok`: boolean success indicator for command outcome.
- `command`: canonical command identifier.
- `result`: deterministic result-state enum for machine handling.
- `issue_id`: issue identifier when relevant.
- `details`: structured context payload.
- `recovery_command`: suggested remediation command when applicable.
- `events`: deterministic event tags.

## Result-State Conventions

Typical result values include (non-exhaustive):

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
- `ok`

## Exit-Code Policy

- `0`: success or non-fatal deterministic state outcome.
- `1`: generic command/system error.
- `3`: `policy_violation`.
- `4`: `partial_state`.

## Notes

- This contract is the source for CLI/MCP parity behavior.
- Any contract change must update this document and corresponding tests.

# Control-Plane Migration Verification Report

## Scope
- Epic: `bd-bmf`
- Date: 2026-02-19
- Goal: verify migration closeout evidence, parity state, and residual risks.

## Command Evidence
1. Intake contract replay (requested final verify command)
   - Command: `bd intake audit --epic bd-bmf --json`
   - Result: `contract_violation`
   - Failed checks: `CHILD_COUNT`, `PLAN_RECONCILE`, `READY_SET`
   - Interpretation: `intake audit` is a pre-claim intake gate. At closeout, the parent has no open children and no ready-wave members, so the intake contract intentionally no longer matches execution-time shape.

2. Open/ready queue snapshot
   - Command: `bd list --status open --parent bd-bmf --json`
   - Result: `0` open child issues
   - Command: `bd ready --parent bd-bmf --limit 20 --json`
   - Result: `0` ready issues

3. Deterministic control-plane test pack
   - Command: `CGO_ENABLED=0 go test ./cmd/bd -run 'TestMachineLintablePolicyMovedToCLI|TestTransitionHandlerConformance|TestCutoverGateBlocksUnresolvedGaps|TestPolicyDriftGuard|TestAgentsScriptReferencesCliCommands|TestDecompositionInvalidDamperThreshold|TestTransientFailureRetryPolicyDeterministic|TestMechanicalTransitionHandlers' -count=1`
   - Result: `ok`

4. MCP parity regression
   - Command: `uv run pytest integrations/beads-mcp/tests/test_control_plane_parity.py -q`
   - Result: `6 passed` (2 pytest config warnings)

## Artifact Links
- Evidence matrix: `docs/control-plane/evidence-matrix.json`
- This report: `docs/control-plane/migration-verification-report.md`

## Residual Risks
- Local CGO toolchain is missing ICU headers (`unicode/regex.h`), so raw CGO-enabled `go test` cannot run without the project script/Make wrapper.
- `bd intake audit` remains intake-phase strict and returns `contract_violation` at closeout by design; closeout evidence should rely on queue-empty checks, deterministic test packs, and parity regression outcomes.

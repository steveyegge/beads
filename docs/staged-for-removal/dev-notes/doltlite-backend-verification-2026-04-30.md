# Dolt and Doltlite Backend Verification

Date: 2026-04-30

## Scope

Focused verification for:

- `bd where`
- `bd list`
- `bd show`
- `bd create`
- `bd update --claim`
- `bd close`
- `gc doctor`
- `gc status`
- `gc events`

## Result Summary

### Dolt backend

Manual CLI matrix passed:

- `bd init --backend dolt`
- `bd create`
- `bd where`
- `bd list --json`
- `bd show --json`
- `bd update --claim`
- `bd close --reason done`

Observed state transitions:

- after claim: `in_progress`, assignee set
- after close: `closed`

### Doltlite backend

Manual CLI matrix is blocked at init:

```text
Error: failed to open doltlite store: doltlite: init schema: dolt add after migrations: no such function: dolt_add
```

This prevents follow-on CLI verification for `create/list/show/claim/close`.

## Code Fix Applied

Claim/event recording for SQLite-compatible backends was using the Dolt path and
implicitly relying on `UUID()` in the events table. That fails for SQLite.

Applied fix:

- added `ClaimIssueInTxWithDialect(...)`
- routed doltlite `ClaimIssue(...)` through `SQLDialectSQLite`
- claim events now use explicit UUID generation on the SQLite path

## Regression Coverage

Added targeted test:

- `internal/storage/issueops/claim_sqlite_test.go`

Verified:

```text
go test ./internal/storage/issueops -run TestClaimIssueInTxWithDialectSQLiteRecordsUUIDEvent -count=1
```

Passes.

## Additional GC Checks

From `/data/projects/beads-doltlite`:

- `gc doctor`: reports missing local `city.toml`, legacy layout warning, invalid inherited Dolt endpoint
- `gc status`: fails to load local `city.toml`
- `gc events --limit 5`: invalid flag; command supports `--since`, `--follow`, `--watch`, `--seq`

## Remaining Blocker

Doltlite schema/version-control initialization still assumes `dolt_add` SQL
function availability. That path must be reconciled before full doltlite CLI
matrix coverage is possible.

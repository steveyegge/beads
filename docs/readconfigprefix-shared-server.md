# ReadConfigPrefix: per-repo prefix in shared-server mode

## Problem

In shared-server mode, multiple repos share one Dolt database. The `config`
table stores a single global `issue_prefix` row (e.g. "punt-labs").
`ReadConfigPrefix()` in `internal/storage/issueops/helpers.go` reads this row
to generate issue IDs, so every repo that calls `bd create` gets IDs with the
global prefix instead of its own project prefix.

## Root cause

`ReadConfigPrefix()` (helpers.go:500) unconditionally queried the DB:

```go
err := tx.QueryRowContext(ctx, "SELECT value FROM config WHERE `key` = ?", "issue_prefix").Scan(&configPrefix)
```

No per-repo override existed. The DB row is authoritative for the entire
server, which is correct in single-repo mode but wrong in shared-server mode
where each repo needs its own prefix.

## Fix

Three changes, one per file:

1. **`internal/configfile/configfile.go`** -- Added `IssuePrefix string` field
   (JSON key `issue_prefix`) to the `Config` struct. This is the per-repo
   identity file (`metadata.json`), already committed per project.

2. **`cmd/bd/main.go`** (PersistentPreRun, line 966) -- After loading
   metadata.json, if `cfg.IssuePrefix` is non-empty and config.yaml has not
   already set `issue-prefix`, push it into viper:
   `config.Set("issue-prefix", cfg.IssuePrefix)`. This makes the per-repo
   value available to all downstream callers via `config.GetString`.

3. **`internal/storage/issueops/helpers.go`** (ReadConfigPrefix, line 504) --
   Before the DB query, check `config.GetString("issue-prefix")`. If non-empty,
   return it immediately, skipping the DB entirely.

## Why metadata.json

metadata.json is the per-repo identity file. It already holds `project_id`,
`dolt_database`, and connection settings that distinguish one repo from
another on a shared server. Adding `issue_prefix` here keeps all per-repo
identity in one place and requires no DB schema changes.

## Precedence

1. **config.yaml `issue-prefix`** -- Explicit user/team override. Highest.
2. **metadata.json `issue_prefix`** -- Per-repo default set at `bd init` time.
3. **DB `config` table `issue_prefix`** -- Global server default. Lowest.

This matches the validation path in `create.go:451` which already checked
`config.GetString("issue-prefix")` before falling back to the DB.

## What does NOT change

- **`allowed_prefixes`** in the DB `config` table -- still read from the DB
  and used for multi-prefix validation in `create.go:456`.
- **Validation in `create.go`** -- `ValidateIDPrefixAllowed` still runs
  against the resolved prefix. The fix only changes *which* prefix is
  resolved, not how it is validated.
- **Single-repo (embedded) mode** -- metadata.json `issue_prefix` is empty by
  default, so the DB path is still taken. No behavioral change.

# Release gate — be-ldr (migration runner stderr progress + large-rig warning)

**Verdict:** PASS
**Date:** 2026-05-07
**Branch:** `release/be-ldr` @ `8dea13f0f` (off `origin/main` `6a6421740`)
**Bead:** be-ldr — *Review: migration runner stderr progress + large-rig warning*

This is the second gate evaluation for be-ldr. The first attempt (2026-05-06)
FAILed criterion 6 — the original commits sat below `9db6b56f2` (GH#3363's
`splitStatements` per-statement fix) on the prior `release/be-ldr` branch.
The builder rebuilt the work onto current `origin/main` on
`rebase-be-ldr-2026-05-06`, the reviewer re-verified PASS at
2026-05-07T01:32:46Z, and this gate runs against that rebased state.

## Commits on the branch (vs `origin/main`)

| SHA | Subject |
|---|---|
| `7170dd163` | feat(schema): stderr progress + large-rig warning for MigrateUp (be-8ja) |
| `8dea13f0f` | test(schema): unit tests for large-rig notice + progress formatting (be-8ja) |

Diff: 3 files changed, +174 / -0 (`internal/storage/schema/schema.go`,
`internal/storage/schema/progress_test.go` (new),
`internal/storage/schema/schema_test.go`).

## Criteria

### 1. Review PASS present — PASS

Single-pass review (gemini second-pass disabled). Bead notes record the
reviewer verdict at 2026-05-07T01:32:46Z:

> ### Verdict
> **PASS** — routing to deployer for release-gate retry.

Routing metadata `gc.routed_to=beads/deployer` and label `+needs-deploy`
both set on the bead.

### 2. Acceptance criteria met — PASS

| Criterion (from bead) | Evidence |
|---|---|
| One-shot large-rig notice fires before the migration loop | `schema.go` lines 238-242: `count, countErr := issueRowCounter(ctx, db); emitLargeRigNotice(progressOut, count, countErr)` runs once before the `for _, mf := range pending` loop |
| Notice silences cleanly on fresh install | `emitLargeRigNotice` returns immediately when `err != nil` (table-missing path) — `TestEmitLargeRigNotice/fresh_install_table_missing` covers this |
| Per-migration progress + timing wraps the full work unit | `Applying migration NNNN: name…` printed at line 250 before per-statement loop; `done (%.1fs)` at line 272 after both the loop and the `INSERT IGNORE` schema_migrations write — timing reflects full per-migration cost |
| Per-statement `splitStatements` loop preserved verbatim | `schema.go` lines 255-265, including the GH#3363 comment and per-statement `db.ExecContext`, identical to origin/main |
| Stderr default protects bd JSON pipelines | `progressOut io.Writer = os.Stderr` at the top of `schema.go`; `TestProgressOutDefaultsToStderr` guards against regression |
| Test mock fix keeps `TestMigration0032ToleratesMissingAppliedAtColumn` green | `schema_test.go` adds 12-line save/restore + stub block for `issueRowCounter`; full schema test pass below confirms |

### 3. Tests pass — PASS (schema package; failing-package failures are an
environmental gc-rig leak unrelated to this work)

Schema package — the only surface this PR touches — runs clean:

```
$ unset BEADS_DOLT_SERVER_PORT BEADS_DOLT_PORT BEADS_DOLT_AUTO_START \
        BEADS_DIR GC_DOLT_PORT && \
  go test -tags gms_pure_go -count=1 -v ./internal/storage/schema/...
...
ok  	github.com/steveyegge/beads/internal/storage/schema	0.004s
```

All new tests pass:
- `TestEmitLargeRigNotice` (5 sub-cases: fresh-install error path, below
  threshold, at threshold, one past threshold, typical large rig)
- `TestHumanMigrationName` (5 cases)
- `TestProgressOutDefaultsToStderr`
- `TestMigration0032ToleratesMissingAppliedAtColumn` (with the new stub —
  output shows `Applying migration 0032: drop_schema_migrations_applied_at…
  / done (0.0s)`, proving the new Fprintf paths execute under existing
  coverage)
- `TestAllMigrationsSQLBootstrapsSchemaMigrationsBeforeDrop`

The full `make test` run reported failures in `internal/configfile` and
`internal/storage/dolt` (port/credential expectations), all of which:

1. Trace to env vars set by the gc-rig harness (`BEADS_DOLT_SERVER_PORT=28231`,
   `BEADS_DOLT_PORT`, etc.) being read by tests that should isolate them
2. Reproduce identically on `origin/main` with the same environment
3. All pass with the env vars unset:

```
$ unset BEADS_DOLT_SERVER_PORT BEADS_DOLT_PORT ... && \
  go test -tags gms_pure_go -count=1 ./internal/configfile/... && \
  go test -tags gms_pure_go -count=1 -run TestApplyConfigDefaults \
    ./internal/storage/dolt/...
ok  	.../internal/configfile	0.011s
ok  	.../internal/storage/dolt	0.126s
```

This is the gc-rig env leak previously tracked on bead **be-dbb** (fixed on
`users/jaword/postgres-backend`, not yet on `main`). It is unrelated to
be-ldr and would block every PR otherwise; the reviewer's notes call this
out explicitly.

### 4. No high-severity review findings open — PASS

Reviewer findings list: **0 HIGH, 0 MEDIUM, 0 LOW.**

### 5. Final branch is clean — PASS

`git status --porcelain` (excluding untracked rig artifacts `.gc/`,
`.gitkeep`, `bench/`) shows nothing.

### 6. Branch diverges cleanly from `origin/main` — PASS

`git merge-base --is-ancestor origin/main HEAD` succeeds — `origin/main`
(`6a6421740`) is a strict ancestor of `8dea13f0f`. No merge conflicts,
no rebase needed. The two be-ldr commits sit cleanly on top of current
main, which itself contains GH#3363's `splitStatements` per-statement fix
(`9db6b56f2`).

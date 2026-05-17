# Release Gate: be-o7fh35 + be-gudo26 (schema-stale guard + bd migrate schema)

**Branch:** `feat/be-o7fh35-be-gudo26-schema-guards`
**PR:** https://github.com/gastownhall/beads/pull/4015
**Deploy bead:** be-b7ixnu
**Gate date:** 2026-05-17

## Beads in this PR

| Bead | Title | Status |
|------|-------|--------|
| be-lw5fak | Build: schema.PendingMigrationCount() | ✓ CLOSED |
| be-l4z4xw | Build: add 'bd migrate schema' subcommand | ✓ CLOSED |
| be-o7fh35 | Build: embedded Dolt Store Open refuses stale schema | ✓ CLOSED |
| be-gudo26 | Build: Dolt server Store Open refuses stale schema | ✓ CLOSED |
| be-ipgt8m | Build: regenerate CLI reference docs | ✓ CLOSED |

## Criterion 1: Review PASS present

**PASS**

- Reviewer: beads/reviewer (maintainer-pr-review ensemble: Qwen + Claude/Opus 4.7 + Codex)
- Verdict: PASS (auto-merge), published 2026-05-17T10:39:41Z
- Evidence: bead be-b7ixnu notes: `Reviewer verdict: PASS (auto-merge)`
- CI: All 41 checks green, upgrade-smoke v1.0.0–v1.0.4 all pass

## Criterion 2: Acceptance criteria met

**PASS**

### be-lw5fak: PendingMigrationCount
- `internal/storage/schema/schema.go` — `PendingMigrationCount()` returns count of pending migrations without applying them ✓

### be-l4z4xw: bd migrate schema subcommand
- `cmd/bd/migrate.go:645` — `migrateSchemaCmd` implemented with `--json` flag ✓
- `cmd/bd/migrate.go:679` — calls `sm.MigrateSchemaUp(ctx)` on `storage.SchemaMigrator` ✓
- `cmd/bd/migrate.go:691` — prints "already up to date" for idempotent case ✓
- `internal/storage/embeddeddolt/store.go:482` — `EmbeddedDoltStore.MigrateSchemaUp` ✓
- `internal/storage/dolt/store.go:1532` — `DoltStore.MigrateSchemaUp` ✓

### be-o7fh35: embedded Dolt schema guard
- `internal/storage/embeddeddolt/store.go:66` — `autoMigrate bool` flag on `newStore` ✓
- `internal/storage/embeddeddolt/store.go:191` — returns `"beads schema is at version %d, binary requires %d — run: bd migrate schema"` when stale ✓
- `internal/storage/embeddeddolt/store.go:181` — `autoMigrate=true` bypasses guard for `bd init` / bootstrap ✓

### be-gudo26: Dolt server schema guard
- `internal/storage/dolt/store.go:262` — `AutoMigrate bool` in `dolt.Config` ✓
- `internal/storage/dolt/store.go:1518` — same error message on stale schema ✓
- `internal/storage/uow/doltserver_provider.go` — guard applied consistently ✓

### Fix: HookFiringStore unwrap
- `cmd/bd/migrate.go:660` — `storage.UnwrapStore(getStore())` before `SchemaMigrator` type assertion ✓
- Prevents `bd migrate schema` from failing when `HookFiringStore` wraps the underlying store ✓

### be-ipgt8m: CLI docs
- `docs/CLI_REFERENCE.md` — `bd migrate schema` subcommand documented ✓
- Website docs updated (version-1.0.0, version-1.0.4 canonical paths) ✓

## Criterion 3: Tests pass

**PASS** (with gc-rig environment caveat)

Tests run on `deployer/schema-guards` (same HEAD as `fork/feat/be-o7fh35-be-gudo26-schema-guards`):

**Passing packages (no regression):**
```
ok  github.com/steveyegge/beads
ok  github.com/steveyegge/beads/cmd/bd/doctor/fix
ok  github.com/steveyegge/beads/cmd/bd/protocol
ok  github.com/steveyegge/beads/cmd/bd/setup
ok  github.com/steveyegge/beads/format
ok  github.com/steveyegge/beads/internal/ado
ok  github.com/steveyegge/beads/internal/atomicfile
ok  github.com/steveyegge/beads/internal/audit
ok  github.com/steveyegge/beads/internal/compact
ok  github.com/steveyegge/beads/internal/configfile
ok  github.com/steveyegge/beads/internal/debug
ok  github.com/steveyegge/beads/internal/doltserver
ok  github.com/steveyegge/beads/internal/git
ok  github.com/steveyegge/beads/internal/github
ok  github.com/steveyegge/beads/internal/gitlab
ok  github.com/steveyegge/beads/internal/hooks
ok  github.com/steveyegge/beads/internal/idgen
ok  github.com/steveyegge/beads/internal/jira
ok  github.com/steveyegge/beads/internal/linear
ok  github.com/steveyegge/beads/internal/lockfile
ok  github.com/steveyegge/beads/internal/molecules
ok  github.com/steveyegge/beads/internal/notion
ok  github.com/steveyegge/beads/internal/query
ok  github.com/steveyegge/beads/internal/recipes
ok  github.com/steveyegge/beads/internal/remotecache
ok  github.com/steveyegge/beads/internal/routing
ok  github.com/steveyegge/beads/internal/storage
ok  github.com/steveyegge/beads/internal/storage/dbproxy/proxy
ok  github.com/steveyegge/beads/internal/storage/dbproxy/server
ok  github.com/steveyegge/beads/internal/storage/dolt
ok  github.com/steveyegge/beads/internal/storage/doltutil
ok  github.com/steveyegge/beads/internal/storage/embeddeddolt
ok  github.com/steveyegge/beads/internal/storage/issueops
ok  github.com/steveyegge/beads/internal/storage/uow
ok  github.com/steveyegge/beads/internal/storage/versioncontrolops
ok  github.com/steveyegge/beads/internal/templates/agents
ok  github.com/steveyegge/beads/internal/testutil
ok  github.com/steveyegge/beads/internal/timeparsing
ok  github.com/steveyegge/beads/internal/tracker
ok  github.com/steveyegge/beads/internal/types
ok  github.com/steveyegge/beads/internal/ui
ok  github.com/steveyegge/beads/internal/utils
ok  github.com/steveyegge/beads/internal/validation
ok  github.com/steveyegge/beads/scripts
```

**Failing packages (pre-existing, identical to origin/main baseline):**
```
FAIL github.com/steveyegge/beads/cmd/bd
FAIL github.com/steveyegge/beads/cmd/bd/doctor
FAIL github.com/steveyegge/beads/internal/beads
FAIL github.com/steveyegge/beads/internal/config
FAIL github.com/steveyegge/beads/internal/formula
```

These failures are present in `origin/main` with the exact same test names — caused by the gc-rig worktree environment (live `.beads/` database present, repo detection tests assume a clean environment). Verified by running `make test` on `origin/main` and comparing output.

Coverage changes (minor, expected): dolt 17.0%→16.9%, embeddeddolt 3.0%→2.9%, uow 28.9%→27.3%. New guard paths not yet directly tested — tracked in be-3zy32x and be-vuf19k.

CI on PR #4015: All 41 checks green (clean environment).

## Criterion 4: No high-severity review findings open

**PASS**

Review findings from maintainer-pr-review:
- info: `SchemaMigrator` interface + `UnwrapStore(getStore())` — clean abstraction, right defensive call
- info: `embeddeddolt.OpenForMigration` split from `Open` bypasses schema-init correctly
- low: Codecov patch coverage ~4.9%; stale-refusal paths not directly tested — tracked in be-3zy32x and be-vuf19k (acceptable to ship)
- info: Upgrade-smoke `|| true` bidirectional shim for pre-`migrate schema` versions is correct

No HIGH or CRITICAL findings.

## Criterion 5: Final branch is clean

**PASS**

```
On branch deployer/schema-guards
Untracked files: .gc/, .gitkeep  (gc-rig management files, not part of beads)
nothing added to commit but untracked files present
```

## Criterion 6: Branch diverges cleanly from main

**PASS**

- `git merge-base --is-ancestor origin/main HEAD` → true (HEAD descends from main)
- PR #4015 shows `mergeable: MERGEABLE`, `mergeStateStatus: CLEAN`
- 10 commits ahead of origin/main, no conflicts

## Verdict: PASS

All 6 criteria pass. PR #4015 is ready to merge.

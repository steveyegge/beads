# Release gate — be-sm6v (TestWhereCommand opt-out of repo .beads/config.yaml)

- **Bead:** be-sm6v (test-environment fix for `TestWhereCommand_ReadsPrefixFromEmbeddedStore`)
- **Commit shipped:** `fb66156d2` (cherry-pick of `9f18dc09c` from `fix/be-0d4-postgres-init-guards`)
- **Branch:** `release/be-sm6v` off `origin/main`
- **Evaluated:** 2026-05-12 by beads/deployer

## Scope note

The source commit `9f18dc09c` is a **+9 LOC test-only** change to
`cmd/bd/where_cgo_test.go`. The target file exists on `origin/main` with the
same `TestWhereCommand_ReadsPrefixFromEmbeddedStore` function; the fix only
adds `t.Setenv("BEADS_TEST_IGNORE_REPO_CONFIG", "1")` and `t.Chdir(t.TempDir())`
at the top of the function plus an inline explanatory comment.

**Not PG-area-derivative:** no PG-only files involved, no dependency on
unmerged PG-backend scaffolding. The bead was discovered while unblocking
the canonical `cmd/bd` build (be-hple → be-sm6v) but the failure mode is a
pre-existing dogfood-config interaction with `whereCmd`'s YAML-first
precedence — orthogonal to the PG ship gate.

## Gate criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | beads/reviewer recorded `Verdict: PASS` in be-sm6v notes (gm-2qey0y, session 2026-05-12). Scope-deviation call on `t.Chdir(t.TempDir())` explicitly accepted as defense-in-depth covering the `worktreeFallbackConfigPath` probe that `BEADS_TEST_IGNORE_REPO_CONFIG` alone doesn't suppress. |
| 2 | Acceptance criteria met | **PASS** | All 3 architect acceptance tests pass (see below). Constraint #4 (test-only, edits limited to `cmd/bd/where_cgo_test.go`) satisfied: `git show --stat fb66156d2` → 1 file changed, +9/-0. No production code touched. |
| 3 | Tests pass | **PASS** | Full `./scripts/test.sh ./cmd/bd/` PASS (89.3s, no regressions). See "Tests run" below. |
| 4 | No high-severity review findings open | **PASS** | 0 HIGH findings. Reviewer noted one pre-existing test-isolation observation (`TestResolveWhereBeadsDir_UsesInitializedDBPath` global-state pollution from other tests) as informational, not in scope and not exhibited under the verification command. |
| 5 | Final branch is clean | **PASS** | `git status` on `release/be-sm6v` shows nothing modified; only worktree-scaffolding untracked paths (`.gc/`, `.gitkeep`) that are never staged. |
| 6 | Branch diverges cleanly from main | **PASS** | Branch cut via `git checkout -B release/be-sm6v origin/main`; `git cherry-pick 9f18dc09c` applied with zero conflicts; `git log origin/main..HEAD` = 1 commit. |

## Tests run on release branch

| Test | Result | Notes |
|------|--------|-------|
| `./scripts/test.sh -v -run 'TestWhereCommand_ReadsPrefixFromEmbeddedStore\|TestResolveWhereBeadsDir_UsesInitializedDBPath\|TestWhereCommand_UsesConfigPrefixFromSelectedDB' ./cmd/bd/` | PASS | All 3 architect acceptance tests: `TestWhereCommand_ReadsPrefixFromEmbeddedStore` (0.28s), `TestResolveWhereBeadsDir_UsesInitializedDBPath` (0.00s), `TestWhereCommand_UsesConfigPrefixFromSelectedDB` (0.00s). |
| `./scripts/test.sh ./cmd/bd/` | PASS | Full package suite 89.317s. No regressions. |
| `source .buildflags && go vet ./cmd/bd/...` | PASS | Clean (no output, exit 0). |

## Source commit context

The fix addresses a documented test-environment trap: the repo dogfoods bd,
so `<repo>/.beads/config.yaml` contains `issue_prefix: be`. viper's
`Initialize()` merges this file via either the module-root cwd-walk **or**
the worktree-fallback probe (`git rev-parse --git-common-dir`). The first
probe is suppressed by `BEADS_TEST_IGNORE_REPO_CONFIG=1`; the second one
isn't, because in gc-rig worktrees the module-root and the upstream repo
dir diverge. `t.Chdir(t.TempDir())` makes both probes miss, restoring the
test's intended exercise of `where.go:79-86`'s store-fallback path.

## Verdict

**PASS** — push to `fork` (`origin` is locked for quad341; `fork =
quad341/beads`), open cross-repo PR against `gastownhall/beads:main`.

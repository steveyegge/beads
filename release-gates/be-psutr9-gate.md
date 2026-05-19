# Release gate: be-psutr9 — cobra/pflag race-fix (cherry-pick from Julian Knutsen)

**Verdict: PASS.**

Branch: `fix/cobra-pflag-race-d8a97c6bb`
Base branch: `main` (origin/main → merged via fork as quad341/beads PR)
HEAD: `1b5536624`
Review bead: `be-psutr9` (initial PASS); re-review: `be-mouz` (PASS, 2026-05-19).

## Commits

| # | SHA | Subject |
|---|-----|---------|
| 1 | `1b5536624` | test: prevent Cobra command flag cache races |

Original upstream commit: `d8a97c6bb` by Julian Knutsen. Cherry-picked onto this PR branch.

Files changed: `cmd/bd/stdio_race_guard_test.go`, `cmd/bd/test_helpers_pure_test.go`.

**Note:** An earlier gate version listed `cmd/bd/notion_test.go` and `cmd/bd/stale_test.go` as changed files (those were in an earlier iteration of the PR). The current branch HEAD `1b5536624` only touches `stdio_race_guard_test.go` and `test_helpers_pure_test.go`. Re-review `be-mouz` flagged this as a LOW non-blocking finding; verdict remained PASS.

## Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | be-psutr9 initial PASS (no findings); re-review be-mouz PASS (2026-05-19): "Correct fix for pflag lazy-merge race. No production code changes." LOW: stale gate file resolved in this update. |
| 2 | Acceptance criteria met | **PASS** | (a) `TestStaleCommandInit` and `TestNotionCommandsRegistered` deparallelized — removes racy concurrent access to shared pflag cache. (b) `cobraOutputMethods` renamed to `cobraParallelUnsafeMethods`; `.Find(` and `.InheritedFlags(` added — policy guard now catches the root-cause methods. (c) Test (macos-latest) with race detector PASS in CI (6m2s), confirming the flake is gone. |
| 3 | Tests pass | **PASS** | CI on PR #4011: Test (macos-latest) PASS 6m2s, Test (ubuntu-latest) PASS 10m24s, Test (Windows-smoke) PASS 3m1s, Test Nix Flake PASS 3m35s, Lint PASS, all other checks PASS. Test (Embedded Dolt Cmd 17/20) FAIL — assessed as known `bd-embedded-test-json-stderr-leak` flake; PR touches no embedded test helper. |
| 4 | No high-severity review findings open | **PASS** | Reviewer be-psutr9: "Findings: none." No security surface — test-only change. |
| 5 | Final branch is clean | **PASS** | `git status`: nothing added to commit (untracked `.beads/formulas/` files are rig scaffolding, not tracked). |
| 6 | Branch diverges cleanly from main | **PASS** | 1 commit ahead of `origin/main`. No commits on main missing from branch. `git merge-tree` shows zero conflicts. |

## Push target

`PUSH_REMOTE=fork`. PR #4011 opened within fork: `quad341:fix/cobra-pflag-race-d8a97c6bb` → `gastownhall/beads:main`.

## Flake note

The single CI failure (Test Embedded Dolt Cmd 17/20) is the known `bd-embedded-test-json-stderr-leak` flake — a test helper uses `cmd.CombinedOutput()` causing auto-export warnings on stderr to leak into JSON stdout. This PR touches none of the relevant files (`cmd/bd/*_embedded_test.go` helper). See bd memory `bd-embedded-test-json-stderr-leak`.

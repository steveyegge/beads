# Release gate: be-psutr9 — cobra/pflag race-fix (cherry-pick from Julian Knutsen)

**Verdict: PASS.**

Branch: `fix/cobra-pflag-race-d8a97c6bb`
Base branch: `main` (origin/main → merged via fork as quad341/beads PR)
HEAD: `af60f7900`
Review bead: `be-psutr9` (PASS).

## Commits

| # | SHA | Subject |
|---|-----|---------|
| 1 | `af60f7900` | test: prevent Cobra command flag cache races |

Original upstream commit: `d8a97c6bb` by Julian Knutsen. Cherry-picked onto this PR branch.

Files changed: `cmd/bd/notion_test.go`, `cmd/bd/stale_test.go`, `cmd/bd/stdio_race_guard_test.go`, `cmd/bd/test_helpers_pure_test.go`.

## Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | be-psutr9 notes: "VERDICT: pass. Findings: none. Decision: PASS — label needs-deploy." |
| 2 | Acceptance criteria met | **PASS** | (a) `TestStaleCommandInit` and `TestNotionCommandsRegistered` deparallelized — removes racy concurrent access to shared pflag cache. (b) `cobraOutputMethods` renamed to `cobraParallelUnsafeMethods`; `.Find(` and `.InheritedFlags(` added — policy guard now catches the root-cause methods. (c) Test (macos-latest) with race detector PASS in CI (6m2s), confirming the flake is gone. |
| 3 | Tests pass | **PASS** | CI on PR #4011: Test (macos-latest) PASS 6m2s, Test (ubuntu-latest) PASS 10m24s, Test (Windows-smoke) PASS 3m1s, Test Nix Flake PASS 3m35s, Lint PASS, all other checks PASS. Test (Embedded Dolt Cmd 17/20) FAIL — assessed as known `bd-embedded-test-json-stderr-leak` flake; PR touches no embedded test helper. |
| 4 | No high-severity review findings open | **PASS** | Reviewer be-psutr9: "Findings: none." No security surface — test-only change. |
| 5 | Final branch is clean | **PASS** | `git status`: nothing added to commit (untracked `.beads/formulas/` files are rig scaffolding, not tracked). |
| 6 | Branch diverges cleanly from main | **PASS** | 1 commit ahead of `origin/main`. No commits on main missing from branch. `git merge-tree` shows zero conflicts. |

## Push target

`PUSH_REMOTE=fork`. PR #4011 opened within fork: `quad341:fix/cobra-pflag-race-d8a97c6bb` → `gastownhall/beads:main`.

## Flake note

The single CI failure (Test Embedded Dolt Cmd 17/20) is the known `bd-embedded-test-json-stderr-leak` flake — a test helper uses `cmd.CombinedOutput()` causing auto-export warnings on stderr to leak into JSON stdout. This PR touches none of the relevant files (`cmd/bd/*_embedded_test.go` helper). See bd memory `bd-embedded-test-json-stderr-leak`.

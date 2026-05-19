# Release gate: be-5hl6 — gate JSONL auto-import to embedded server mode

- **Bead:** be-5hl6 (Review: gate JSONL upgrade-recovery to embedded server mode, PR #3995)
- **Source PR:** https://github.com/gastownhall/beads/pull/3995 (shaunc:fix/auto-import-gate-embedded-mode)
- **Deploy branch:** `release/be-5hl6-auto-import-embedded` off `origin/main` (e569c7488)
- **Cherry-picked commit:** `b1609dff` (auto-import: gate JSONL upgrade-recovery to embedded server mode)
- **Evaluated:** 2026-05-19 by beads/deployer

## Commit

| SHA | Subject |
|-----|---------|
| `32b8b32be` | auto-import: gate JSONL upgrade-recovery to embedded server mode (cherry-pick of b1609dff) |

1 file changed: `cmd/bd/main.go` — gates `maybeAutoImportJSONL` call with `doltserver.ResolveServerMode(beadsDir) == doltserver.ServerModeEmbedded`.

## Gate criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | be-5hl6 notes: "Review verdict: PASS. Gate correct: maybeAutoImportJSONL gated to ServerModeEmbedded using existing tested ResolveServerMode. CI all green. No findings." (2026-05-19) |
| 2 | Acceptance criteria met | **PASS** | cmd/bd/main.go:1018 — condition now includes `&& doltserver.ResolveServerMode(beadsDir) == doltserver.ServerModeEmbedded`; compiles clean with `-tags gms_pure_go` |
| 3 | Tests pass | **PASS** | CI 40/40 SUCCESS on source PR #3995; local TestWhereCommand_ReadsPrefixFromEmbeddedStore fails identically on origin/main (pre-existing, Docker not available in this environment — covered by cross-version smokes in CI) |
| 4 | No high-severity review findings open | **PASS** | 0 findings in be-5hl6 (reviewer noted only a coverage gap on PersistentPreRun as legitimately hard to test) |
| 5 | Final branch is clean | **PASS** | `git status` clean after cherry-pick commit |
| 6 | Branch diverges cleanly from main | **PASS** | Cherry-pick applied with no conflicts onto origin/main; 1 commit ahead |

## Verdict: PASS

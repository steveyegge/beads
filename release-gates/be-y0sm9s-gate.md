# Release Gate: be-y0sm9s — fail-fast on unsupported backend metadata

**Branch:** `fix/be-y0sm9s-fail-fast-pg-backend`
**Date:** 2026-05-16
**Deployer:** beads/deployer (quad341-claude)

## Gate Checklist

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | be-cginlu closed with PASS: "GetBackend() hardcodes 'dolt' so using raw cfg.Backend field is essential correctness; 3 unit tests pass; nil/supported/unsupported branches covered; error format safe." |
| 2 | Acceptance criteria met | **PASS** | See details below |
| 3 | Tests pass | **PASS** | 3 new unit tests pass; pre-existing failures in 4 packages (doctor, beads, config, doltserver) confirmed identical on `origin/main` — not introduced by this change |
| 4 | No high-severity review findings open | **PASS** | Review bead be-cginlu: no HIGH findings; independent verifier confirmed safe |
| 5 | Final branch is clean | **PASS** | `git status` shows no uncommitted changes; only untracked `.beads/formulas/` workspace files (not part of PR) |
| 6 | Branch diverges cleanly from main | **PASS** | Cherry-pick of c581f82268 onto fresh `origin/main` HEAD — zero conflicts |

## Acceptance Criteria Evaluation

From bead description:

- [x] **<100ms exit, <100MB RSS, clear error on unsupported backend:** `checkMetadataBackendError()` reads raw `cfg.Backend` after `configfile.Load` and exits immediately (no embedded Dolt, no network I/O) with backend name + binary path + version/commit + upgrade hint.
- [x] **Test passes:** 3 unit tests in `cmd/bd/main_errors_test.go` all PASS:
  - `TestCheckMetadataBackendError_UnsupportedBackend`
  - `TestCheckMetadataBackendError_SupportedBackends` (subtests: `backend=`, `backend=dolt`)
  - `TestCheckMetadataBackendError_NilConfig`
- [x] **Commit c581f8226 lives ONLY on `fix/be-y0sm9s-fail-fast-pg-backend`:** Local `tests/be-uwvs.4-lite-correctness-v2` reset to match fork (which never received the wrong commit); be-2bh9s3 (PR #3906 rebase) already closed and unaffected.
- [x] **Extracted from wrong branch:** `tests/be-uwvs.4-lite-correctness-v2` on fork is at `a180548b9` (no wrong commit present).

## Commits

| SHA | Message |
|-----|---------|
| `5dd606254` | fix(startup): be-y0sm9s — fail-fast on unsupported backend in metadata.json |

(Cherry-picked from `c581f82268c79b4fb6f1d97b71e917519b397050` on `tests/be-uwvs.4-lite-correctness-v2`)

## Pre-existing test failures (not introduced by this change)

These 4 packages fail on both this branch AND `origin/main`:
- `cmd/bd/doctor`
- `internal/beads`
- `internal/config`
- `internal/doltserver`

Verified by running `make test` on `origin/main` HEAD (`d85881083`) — same failures.

## Verdict: PASS

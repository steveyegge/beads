# Release Gate: be-uo71at-init-backend-stub

**Date**: 2026-05-17
**Feature bead**: be-uo71at — Build: bd init --help text honesty + --backend flag stub (item 28)
**Review bead**: be-ascj09 — Review: PR #4012 backend stub rework (CI now green)
**Branch**: `feat/be-uo71at-init-backend-stub`
**PR**: https://github.com/gastownhall/beads/pull/4012

## Gate Summary: PASS

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | claude-reviewer PASS in be-ascj09 notes (single-pass; gemini second-pass disabled) |
| 2 | Acceptance criteria met | **PASS** | All 6 items verified against code (see below) |
| 3 | Tests pass | **PASS** | 40/40 CI checks green on PR #4012 run [25981241985] |
| 4 | No high-severity findings open | **PASS** | Reviewer found no blocking findings |
| 5 | Final branch is clean | **PASS** | `git status`: only untracked `.gc/` and `.gitkeep` (not feature-related) |
| 6 | Branch diverges cleanly from main | **PASS** | 5 commits ahead of `origin/main`; CI runs on merged result with zero failures |

## Acceptance Criteria (from be-uo71at)

- [x] `bd init --help` no longer claims Dolt is the only backend
  - `cmd/bd/init.go:39-41`: long description updated; lists `dolt` and `postgres (experimental)` without "only" claim
- [x] `bd init --backend=postgres` outputs "requires --experimental flag" error
  - `cmd/bd/init.go:168-169`: explicit guard before postgres stub path
- [x] `bd init --backend=postgres --experimental` outputs "not yet implemented" error
  - `cmd/bd/init.go:170-171`: placeholder FatalError with Phase 1 mention
- [x] `bd init --backend=dolt` works (current behavior, now wired explicitly)
  - Dolt path unchanged; all 40 CI test shards pass
- [x] Help text updated to reflect PG as experimental backend
  - `cmd/bd/init.go:1509-1510`: flag descriptions updated; `--backend` description no longer says "only dolt"
- [x] No functional change to existing `bd init` behavior
  - SQLite deprecation notice path unchanged (`init.go:159-167`); Dolt path unchanged; CI confirms

## CI Evidence (PR #4012, run 25981241985)

All checks PASS, including:
- Build (Embedded Dolt): PASS
- Check cmd/bd pure-Go tests compile (CGO_ENABLED=0): PASS
- Check doc flags freshness: PASS
- Lint: PASS
- Test (Embedded Dolt Cmd 1–20/20): all PASS
- Test (Embedded Dolt Storage): PASS
- Test (ubuntu-latest / macos-latest / Windows smoke): all PASS
- Test Nix Flake: PASS
- Upgrade smoke (v1.0.0–v1.0.4 → candidate): all PASS

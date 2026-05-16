# Release gate — be-ht5qm4 + be-jo8pxm + be-hpivhb (daemon kill + bddgen + StoreLocator)

- **Review bead:** be-t9ox0j (verdict PASS in notes)
- **Branch:** `release/be-ht5qm4-daemon-kill` stacked on `release/be-fqjs3v-daemon-foundation`
- **Commits cherry-picked:**
  - `e9fd8b486` ← `3714eecff` — feat(daemon): be-ht5qm4 — Kill() module + be-jo8pxm bddgen codegen
  - `5759dd47d` ← `e2c5f97a1` — feat(daemon): be-hpivhb — RPC package StoreLocator
  - `230e76a81` ← `d02c1c9f5` — fix(daemon): be-fqjs3v followup — PidFile.StartedAt to *time.Time
- **Evaluated:** 2026-05-15 by beads/deployer

## Gate criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | be-t9ox0j verdict PASS; 1 LOW (blind sleep before SIGKILL), non-blocking |
| 2 | Acceptance criteria met | **PASS** | be-ht5qm4 Kill ACs: non-existent dir→nil, no pidfile→nil, stale cleaned, idempotent, bdd.log preserved ✓; be-jo8pxm bddgen ACs: build clean, DO NOT EDIT headers, go vet pass ✓; be-hpivhb StoreLocator ACs verified ✓ |
| 3 | Tests pass | **PASS** | `go test ./internal/daemon/...` → ok; `make build` clean |
| 4 | No high-severity review findings open | **PASS** | 0 HIGH findings |
| 5 | Final branch is clean | **PASS** | `git status` clean |
| 6 | Branch diverges cleanly from main | **PASS** | All 3 cherry-picks applied with no conflicts |

## Follow-up tracked

- be-ojx6um: `gob.Register` for `map[string]interface{}` in UpdateIssue transport — filed separately, does not block this release gate

## Verdict: PASS

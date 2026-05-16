# Release gate — be-fup0q9 (bddgen exported reply field fix)

- **Review bead:** be-xs17ge (verdict PASS in notes)
- **Branch:** `feat/be-fup0q9-bddgen-fix-getlabels-exported` (tip: 2501451ac)
- **Evaluated:** 2026-05-15 by beads/deployer

## Gate criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | be-xs17ge verdict PASS; 0 findings |
| 2 | Acceptance criteria met | **PASS** | `resultFieldName()` applies `exportedName()` to base before concatenation ✓; `GetLabelsReply.Strings` exported ✓; RPC tests pass ✓ |
| 3 | Tests pass | **PASS** | `go test ./internal/storage/rpc/...` → ok; `make build` clean |
| 4 | No high-severity review findings open | **PASS** | 0 findings of any severity |
| 5 | Final branch is clean | **PASS** | `git status` clean |
| 6 | Branch diverges cleanly | **PASS** | Build clean on branch tip |

## Verdict: PASS

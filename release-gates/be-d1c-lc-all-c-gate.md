# Release Gate: be-d1c — fix LC_ALL=C in generate-llms-full.sh

**Bead:** be-dsj (deploy) → be-d1c (task) → be-dsp (review)
**Branch:** fix/be-d1c-lc-all-c
**Commit:** 5ef705287 (cherry-picked from 8e07ad962 on feat/be-3w6-be-0c8-nopush-dolt)
**Date:** 2026-05-29

## Gate Checklist

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | be-dsp: `REVIEW VERDICT: PASS` — style clean, security clean, spec fully met, both --check modes verified by builder |
| 2 | Acceptance criteria met | **PASS** | See AC verification below |
| 3 | Tests pass | **PASS** | `go test ./scripts/...` — all pass. `internal/tracker` fails with Dolt server infrastructure error pre-existing on main (database "tracker_pkg_shared" not found at 127.0.0.1:28231; unrelated to this script change) |
| 4 | No high-severity review findings open | **PASS** | Review found no HIGH findings; style/security/spec all clean |
| 5 | Final branch is clean | **PASS** | `git status` shows branch ahead of main by 1 commit; no uncommitted changes to tracked files |
| 6 | Branch diverges cleanly from main | **PASS** | Cherry-pick of 8e07ad962 onto origin/main applied with no conflicts; single file changed: `scripts/generate-llms-full.sh` |
| 7 | Single feature theme | **PASS** | One commit, one file (`scripts/generate-llms-full.sh`), one-line locale pin. Source branch (`feat/be-3w6-be-0c8-nopush-dolt`) also carries no-push commits, but those are being tracked separately under PR #4212. This PR is cherry-picked clean. |

**Overall: PASS**

## Acceptance Criteria Verification

From be-d1c done-when:

1. **`./scripts/generate-llms-full.sh` produces byte-identical output to `LC_ALL=C ./scripts/generate-llms-full.sh` regardless of caller locale** — PASS: both run passes `--check` (exit 0). `export LC_ALL=C` inside the script makes every invocation locale-proof.

2. **`./scripts/generate-llms-full.sh --check` passes under both default locale and `LC_ALL=C`** — PASS:
   - `./scripts/generate-llms-full.sh --check` → `PASS: website/static/llms-full.txt is fresh` (exit 0)
   - `LC_ALL=C ./scripts/generate-llms-full.sh --check` → `PASS: website/static/llms-full.txt is fresh` (exit 0)

3. **CI "Check doc flags freshness" passes on a PR that merges current main** — to be verified by CI on the opened PR.

## Change Scope

Single-line addition to `scripts/generate-llms-full.sh`:
```diff
 set -euo pipefail
+export LC_ALL=C
```

Placed immediately after `set -euo pipefail` (line 8). Ensures glob collation in `for file in $DOCS_DIR/$dir/*.md` (line 143) uses byte order (matching CI) regardless of caller locale. Addresses recurring CI failure on PRs #3908/#3985/#4157/#4160/#4212.

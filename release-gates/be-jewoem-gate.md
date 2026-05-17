# Release Gate: be-jewoem — reference-aware bd prune

**Branch:** `feat/be-jewoem-reference-aware-prune`
**Date:** 2026-05-17
**Deployer:** beads/deployer (quad341-claude)

## Beads covered

| Bead | Title | Status |
|------|-------|--------|
| be-jewoem | Implement reference-aware bd prune core logic | OPEN → deploying |
| be-u2mw2x | Update bd prune --help and README for reference-aware skip | OPEN → deploying |
| be-0wt833 | Bench: bd prune reference scan under 5s on 10K-open-bead fixture | OPEN → deploying |

## Gate Checklist

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | be-kl6ce2 (PASS): all be-8t7gj1 blockers resolved; integration tests T1–T7 present; §4d wording + JSON guard fixed. be-1pfq2i (PASS): TestPruneLargeFixture 0.87s, lint clean, all AC met. |
| 2 | Acceptance criteria met | **PASS** | See details below |
| 3 | Tests pass | **PASS** | Reviewer verified: `go test -short ./cmd/bd/` 0 failures; `go test -run TestPruneLargeFixture ./cmd/bd/` PASS 0.63s–0.87s; `golangci-lint ./cmd/bd/` 0 issues. CI on schema-guards branch (which contains these commits): 41/41 green. |
| 4 | No high-severity review findings open | **PASS** | be-kl6ce2: MEDIUM (purge_rejects_ignore_references_flag test absent — follow-up be-g76wkv P3 filed, not blocking); LOW (pre-existing duplicate Store(true) at purge.go:314+348). No HIGH findings. |
| 5 | Final branch is clean | **PASS** | `git status`: no uncommitted changes; untracked .gc/ workspace only |
| 6 | Branch diverges cleanly from main | **PASS** | 3 commits cherry-picked from feat/be-o7fh35-be-gudo26-schema-guards onto origin/main HEAD (ed2b5aea5) with zero conflicts |

## Acceptance Criteria Evaluation

### be-jewoem (core logic + be-u2mw2x docs)
- [x] `bd prune` skips referenced closed beads by default — `buildReferencedSet` scans open bead descriptions/notes/comments with word-boundary regex
- [x] `bd prune --ignore-references` deletes referenced beads anyway — flag registered in pruneCmd, passed through purgeScope
- [x] `bd purge --ignore-references` gives 'unknown flag' — flag NOT registered in purgeCmd
- [x] `referenced_skipped` always in JSON; `referenced_ids_sample` omitted when 0; `referenced_count` always present — 3 output paths updated + guarded with `!scope.ignoreReferences`
- [x] Integration tests present (T1–T7 in prune_refs_embedded_test.go): referenced-skip, ignore-references override, closed-to-closed refs, word-boundary precision, empty set, body/comment/notes citations
- [x] `bd prune --help` shows `--ignore-references` flag; `bd purge --help` does not
- [x] README.md updated with reference-aware skip documentation

### be-0wt833 (bench)
- [x] `cmd/bd/prune_bench_test.go` exists with `TestPruneLargeFixture`
- [x] `testing.Short()` guard skips the bench
- [x] Fixture: 10K open beads × 5KB body + 100 closed candidates, 20 referenced
- [x] `buildReferencedSet` called and timed; fails if >5s — passes in 0.63–0.87s (5× headroom)
- [x] `refSet == exactly the 20 seeded referenced IDs` asserted
- [x] `golangci-lint` clean

## Commits

| SHA | Message |
|-----|---------|
| `2c110d1e2` | feat(prune): be-jewoem/be-u2mw2x — reference-aware bd prune |
| `273dfa894` | fix(prune): address reviewer feedback on reference-aware bd prune (be-jewoem) |
| `b92a7507a` | test(prune): 10K-bead fixture bench for buildReferencedSet (be-0wt833) |

(Cherry-picked from feat/be-o7fh35-be-gudo26-schema-guards onto origin/main HEAD ed2b5aea5)

## Review beads

- be-kl6ce2 (PASS): bench + core fix (6c1544600 + 44598fdc1) — all be-8t7gj1 blockers resolved
- be-1pfq2i (PASS): 10K-bead bench (44598fdc1) — 0.87s, lint clean

## Follow-up

- be-g76wkv (P3): `purge_rejects_ignore_references_flag` test not written — not blocking; behavioral guarantee is in place via Cobra flag registration scope

## Verdict: PASS

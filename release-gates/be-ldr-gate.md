# Release Gate: be-ldr — stderr progress + large-rig warning for MigrateUp

**Result: GATE PASS**
**Date:** 2026-05-12
**Deployer:** beads/deployer
**Branch under review:** `rebase/be-ldr-2026-05-12` (3 commits ahead of origin/main)
**Commits:** `51ab499ee`, `19fd8851b`, `4435de4ac`
**Target:** origin/main (`da73b7511`)
**PR:** https://github.com/gastownhall/beads/pull/3919

---

## Criteria Checklist

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | **PASS** | Reviewer verdict PASS in bead notes (beads/reviewer, Opus 4.7). Review pre-dates rebase; code logic unchanged — only rebased onto newer main. |
| 2 | Acceptance criteria met | **PASS** | See below. All criteria verified against code. |
| 3 | Tests pass | **PASS** | `go test ./internal/storage/schema/...` → 7/7 PASS. See below. |
| 4 | No high-severity review findings open | **PASS** | Previous informational findings F1 (warning text understatement) and F2 (deferred unit tests) both resolved: F2 moot — tests shipped in commit `19fd8851b`. |
| 5 | Final branch is clean | **PASS** | `git status` clean (only untracked `.gc/`, `.gitkeep`). |
| 6 | Branch diverges cleanly from main | **PASS** | Rebased cleanly onto `da73b7511`. `git merge --no-commit --no-ff origin/main` → "Already up to date." Zero conflicts. |

---

## Criterion 2: Acceptance Criteria

| Spec | Code | Verdict |
|------|------|---------|
| Progress to stderr only | `var progressOut io.Writer = os.Stderr` (`schema.go:13`) | ✅ |
| Plain text (no ANSI/color) | `fmt.Fprintf(progressOut, "Applying migration %04d: %s…\n", ...)` | ✅ |
| Large-rig gate at >10 000 rows | `const largeRigThreshold = 10000`; `count <= largeRigThreshold` returns early | ✅ |
| Per-migration timing format `  done (X.Xs)` | `fmt.Fprintf(progressOut, "  done (%.1fs)\n", time.Since(start).Seconds())` | ✅ |
| `humanMigrationName` present | Function at `schema.go:40` strips version prefix + `.up.sql` | ✅ |
| Large-rig warning text includes 90s estimate | `"... may take up to 90 seconds; do not interrupt."` (be-mzjo commit) | ✅ |
| Fresh install (missing table) → no warning | `issueRowCounter` error path → `emitLargeRigNotice` returns early on `err != nil` | ✅ |

---

## Criterion 3: Test Run

```
go test ./internal/storage/schema/... -v
=== RUN   TestIgnoredTableDDL           --- PASS (0.00s)
=== RUN   TestReadMigrationSQL          --- PASS (0.00s)
=== RUN   TestReadMigrationSQL_Panics   --- PASS (0.00s)
=== RUN   TestEmitLargeRigNotice
    --- PASS: fresh_install_table_missing  (0.00s)
    --- PASS: small_rig_below_threshold    (0.00s)
    --- PASS: at_threshold_no_warning      (0.00s)
    --- PASS: one_past_threshold_warns     (0.00s)
    --- PASS: typical_large_rig            (0.00s)
=== RUN   TestHumanMigrationName        --- PASS (0.00s)
=== RUN   TestProgressOutDefaultsToStderr --- PASS (0.00s)
PASS
ok  github.com/steveyegge/beads/internal/storage/schema  0.002s
```

`go build ./internal/storage/schema/...` — PASS
`go vet ./internal/storage/schema/...` — PASS
`gofmt -l internal/storage/schema/` — PASS (no output)

Note: `go build ./...` (full binary) failed with "disk quota exceeded" at the linker step
due to /tmp pressure at the time of evaluation; schema package builds and vets cleanly.
The builder's own gate run (`go build ./...`) succeeded and is recorded in the PR description.

---

## Commits in PR

| Commit | Message |
|--------|---------|
| `51ab499ee` | feat(schema): stderr progress + large-rig warning for MigrateUp (be-8ja) |
| `19fd8851b` | test(schema): unit tests for large-rig notice + progress formatting (be-8ja) |
| `4435de4ac` | fix(schema): bump large-rig warning from 60s to 90s (be-mzjo) |

---

## Prior Gate History

First gate run (2026-05-12 earlier session): FAIL — cherry-pick of `38f2f1e4` onto
`da73b7511` conflicted because PR #3871 removed `tolerateExisting bool` from
`runMigrations`. Builder rebased; new branch and PR opened. This is the re-gate on
the rebased branch.

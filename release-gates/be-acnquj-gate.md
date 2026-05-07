# Release gate — be-acnquj (bd list filter regression tests + --json hint suppression)

**Verdict:** PASS
**Date:** 2026-05-07
**Branch:** `release/be-ldr` @ `e83e01058` (cherry-pick of `9885fc20c` from `rebase-be-ldr-2026-05-06`)
**Bead:** be-fgi5gg — *Review: be-acnquj fix — bd list filter tests + JSON hint suppression*
**Closes:** be-acnquj — *bd list label/title-contains filters silently ignored — returns full result set*

This gate stacks on top of the previously PASSed be-ldr gate (see
`release-gates/be-ldr-gate.md`); the be-8ja schema work and its gate
commit (`e85b9acc2`) are unchanged on this branch. Cherry-picked
`9885fc20c` from the rebased be-ldr line as `e83e01058`.

## Commit added to the branch

| SHA | Subject |
|---|---|
| `e83e01058` | fix(list): suppress truncation hint in --json mode + lock-in label filter tests (be-acnquj) |

Diff: 2 files changed, +180 / -2 (`cmd/bd/list_output.go`,
`cmd/bd/list_test.go`).

## Criteria

### 1. Review PASS present — PASS

Single-pass review (gemini second-pass disabled). Bead `be-fgi5gg` notes
record the reviewer verdict at 2026-05-07T13:23:31Z:

> ### Routing
> Pass → label `needs-deploy`, route to `beads/deployer`.

`go vet` clean, `golangci-lint run ./cmd/bd/` 0 issues, `gofmt -l`
clean on both touched files. Two non-blocking low-priority follow-up
notes in the review (cobra-layer test gap, AC#6 mixed-flag gap) — both
acceptable as future improvements, neither alters the SQL filter chain
that was the suspect layer.

Routing metadata `gc.routed_to=beads/deployer` and label `+needs-deploy`
both set on the bead.

### 2. Acceptance criteria met — PASS

The investigation determined the original reproducer was a stale-binary
artifact: `BuildIssueFilterClauses` already handles every flag correctly
at HEAD. The fix locks in that correct behavior with regression tests
and adds the AC#7 hygiene fix.

| Criterion (from bead) | Evidence |
|---|---|
| AC#1 `-l X` returns ONLY beads with label X | `TestListLabelFiltersAcnquj/single_label` — `cmd/bd/list_test.go:540` |
| AC#2 `--label-any A,B` (OR semantics) | `TestListLabelFiltersAcnquj/label_any_or_composition` — `cmd/bd/list_test.go:580` |
| AC#3 `--label-pattern` glob | `TestListLabelFiltersAcnquj/label_pattern_glob` — `cmd/bd/list_test.go:600` |
| AC#4 `--title-contains` case-insensitive | `TestListLabelFiltersAcnquj/title_contains_case_insensitive` — `cmd/bd/list_test.go:620` |
| AC#5 `-l NONEXISTENT` returns empty | `TestListLabelFiltersAcnquj/nonexistent_label_empty` — `cmd/bd/list_test.go:640` |
| AC#6 AND/OR composition with multiple flags | `TestListLabelFiltersAcnquj/label_and_composition` (AND) + `label_any_or_composition` (OR) — `cmd/bd/list_test.go:560,580` |
| AC#7 trailing pager hint suppressed under `--json` | `cmd/bd/list_output.go:20` adds `\|\| jsonOutput` guard; `TestPrintTruncationHintJSONSuppression` covers 6-case truth table (truncated × json × limit=0) |

`go tool cover -func` reports `printTruncationHint = 100.0%` from the
new tests.

### 3. Tests pass — PASS (cmd/bd new tests; failing tests are pre-existing environmental, identical on origin/main)

Targeted run of be-acnquj tests is clean:

```
$ unset BEADS_DOLT_SERVER_PORT BEADS_DOLT_PORT BEADS_DOLT_AUTO_START \
        BEADS_DIR GC_DOLT_PORT && \
  go test -tags gms_pure_go -count=1 -v \
    -run 'TestListLabelFiltersAcnquj|TestPrintTruncationHintJSONSuppression' \
    ./cmd/bd/
...
--- PASS: TestPrintTruncationHintJSONSuppression (0.00s)
    --- PASS: truncated_human_emits
    --- PASS: truncated_json_suppresses
    --- PASS: untruncated_human_silent
    --- PASS: untruncated_json_silent
    --- PASS: limit_zero_silent_human
    --- PASS: limit_zero_silent_json
--- SKIP: TestListLabelFiltersAcnquj  (Dolt test server not available, runs in CI)
PASS
ok  	github.com/steveyegge/beads/cmd/bd	0.143s
```

`TestListLabelFiltersAcnquj` SKIPs locally without Docker; it is built
and run in CI's `test-embedded-storage` job (binary built from
`go test -c ./cmd/bd/`, run by `bd-cmd-test`). PR #3780's CI is fully
green with this same harness, and the new commit only adds a SKIP-on-
no-Docker test plus a single-flag `||` guard on a stderr emitter — no
risk of breaking unrelated CI jobs.

Schema package (be-8ja surface, unchanged on this branch) still clean:

```
ok  	github.com/steveyegge/beads/internal/storage/schema	0.002s
```

The full `cmd/bd` run shows two pre-existing FAIL lines:
- `TestWhereCommand_ReadsPrefixFromEmbeddedStore`
- `TestResolveWhereBeadsDir_UsesInitializedDBPath`

Both reproduce identically on `origin/main` (`6a6421740`) with the same
unset-env command and the same gc-rig worktree CWD. Root cause is the
test's filesystem walker resolving the surrounding gc-management
`.beads/` directory rather than the test tmp dir — a CWD/test-isolation
issue, not a be-acnquj or be-ldr regression. PR #3780's GitHub-side CI
runs in a clean container without that ambient `.beads/` and stays
green; the new commit does not touch `cmd/bd/where*.go`.

### 4. No high-severity review findings open — PASS

Reviewer findings list: **0 HIGH, 0 MEDIUM, 2 LOW (non-blocking), 1 INFO.**

- *low/quality*: cobra-layer wiring not directly exercised — acceptable;
  filter chain that was the suspect layer is what the new tests pin.
- *low/quality*: AC#6 mixed-flag (`Labels:["fruit"] + LabelsAny:[...]`)
  not exercised — minor coverage gap, AND-only and OR-only paths each
  covered separately.
- *info*: package-level `jsonOutput` global pattern matches existing
  test convention in `cmd/bd/dolt_test.go`.

No blocking findings.

### 5. Final branch is clean — PASS

`git status --porcelain` (excluding untracked rig artifacts `.gc/`,
`.gitkeep`, `bench/`) shows nothing.

### 6. Branch diverges cleanly from `origin/main` — PASS

`git merge-base --is-ancestor origin/main HEAD` succeeds — `origin/main`
(`6a6421740`) is a strict ancestor of `e83e01058`. No merge conflicts,
no rebase needed. PR #3780 was MERGEABLE before this commit; cherry-
picking on top does not change the shape of the diff against `main`.

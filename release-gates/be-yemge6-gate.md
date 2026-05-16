# Release gate: be-yemge6 — split stdout/stderr in getIssueStatus

**Verdict: PASS.**

Branch: `fix/be-yemge6-defer-status-stdout-split`
Base on origin: `origin/main` @ `d85881083` (commit before the fix)
HEAD: `afaf390dd`
Source bead: `be-yemge6`; review bead: `be-8fypm3` (PASS).

## Commit

| # | SHA | Subject |
|---|-----|---------|
| 1 | `afaf390dd` | test(embedded): split stdout/stderr in getIssueStatus to fix TestEmbeddedUndeferConcurrent flake (be-yemge6) |

1 file changed: `cmd/bd/defer_embedded_test.go` (+7 / -4 lines). No production code changes.

## Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | be-8fypm3 notes: "VERDICT: PASS. No findings. Clean test fix." Gemini second-pass disabled per current policy. |
| 2 | Acceptance criteria met | **PASS** | `getIssueStatus` now uses separate `bytes.Buffer` for stdout/stderr via `cmd.Stdout` / `cmd.Stderr` instead of `cmd.CombinedOutput()`. JSON parsed from `stdout.String()` only. Error messages improved to show both streams. Matches fix spec exactly. |
| 3 | Tests pass | **PASS** | `go build -tags gms_pure_go ./...` clean. `go vet -tags gms_pure_go ./cmd/bd/...` clean. `go test -tags gms_pure_go -count=1 -short ./cmd/bd/...`: `cmd/bd` PASS (0.269s). `cmd/bd/doctor` 2 pre-existing failures (`TestCheckBeadsRole_NotConfigured`, `TestCheckBeadsRole_NotGitRepo`) byte-equal with `origin/main` — rig worktree has `beads.role=maintainer` configured globally. No new regressions. |
| 4 | No high-severity review findings open | **PASS** | 0 findings of any severity in be-8fypm3. |
| 5 | Final branch is clean | **PASS** | `git status` clean (untracked `.gc/`, `.gitkeep` are rig scaffolding). |
| 6 | Branch diverges cleanly from main | **PASS** | 1 commit ahead of `origin/main`. `git merge-tree` shows zero conflicts. |

## Test environment

- Host: Linux 6.19.14-300.fc44.x86_64
- `TMPDIR`/`GOTMPDIR` pinned to `~/.gotmp` (per /tmp per-user quota constraint).
- `go test -tags gms_pure_go -count=1 -short ./cmd/bd/...` on this branch and `origin/main` produce byte-equal failure sets.

## Push target

`PUSH_REMOTE=fork` (origin returns 403 for quad341). Cross-repo PR head: `quad341:fix/be-yemge6-defer-status-stdout-split`.

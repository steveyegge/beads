# Release gate: be-0x7ztf — bump listenWait 2s→10s (PR #3988)

**Verdict: PASS.**

Branch: `fix/be-0x7ztf-proxy-listen-wait`
Base on origin: `origin/main`
HEAD: `e875ccfc4`
Source bead: `be-0x7ztf`; review bead: `be-rmbk9b` (PASS).

Note: A parallel deploy branch (`deploy/be-0x7ztf-proxy-listen-wait`, PR #3987) was opened by an earlier deployer session and has its own gate (`be-0x7ztf-gate.md`). This gate covers the reviewed builder branch (PR #3988). Both carry the same single-line fix.

## Commit

| # | SHA | Subject |
|---|-----|---------|
| 1 | `e875ccfc4` | fix(test): be-0x7ztf — bump listenWait 2s→10s to fix flaky TestProxy_IdleTimeout_Fires |

1 file changed: `internal/storage/dbproxy/proxy/server_test.go` line 26 (`2 * time.Second` → `10 * time.Second`).

## Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | be-rmbk9b notes: "Review Verdict: PASS. No findings. Single-line listenWait fix is correct, CI failure unrelated to PR." Gemini second-pass disabled per current policy. |
| 2 | Acceptance criteria met | **PASS** | `server_test.go:26` reads `listenWait = 10 * time.Second`. `gofmt -l` clean. No other lines changed. |
| 3 | Tests pass | **PASS** | `go test -tags gms_pure_go -count=1 ./internal/storage/dbproxy/proxy/...`: ok 6.014s. |
| 4 | No high-severity review findings open | **PASS** | 0 findings in be-rmbk9b. |
| 5 | Final branch is clean | **PASS** | `git status` clean (untracked `.gc/`, `.gitkeep` are rig scaffolding). |
| 6 | Branch diverges cleanly from main | **PASS** | 1 commit ahead of `origin/main`. `git merge-tree` shows zero conflicts. |

## Test environment

- Host: Linux 6.19.14-300.fc44.x86_64
- `TMPDIR`/`GOTMPDIR` pinned to `~/.gotmp`.

## Push target

`PUSH_REMOTE=fork`. Cross-repo PR head: `quad341:fix/be-0x7ztf-proxy-listen-wait`.

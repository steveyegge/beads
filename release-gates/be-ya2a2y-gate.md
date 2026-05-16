# Release gate: be-ya2a2y — batch wisp partition in GetCommentCountsInTx

**Verdict: PASS.**

Branch: `fix/be-ya2a2y-comment-counts-wisp-partition`
Base on origin: `origin/main`
HEAD: `e1a1edb97`
Source bead: `be-ya2a2y`; review bead: `be-7xtky2` (PASS).

## Commit

| # | SHA | Subject |
|---|-----|---------|
| 1 | `e1a1edb97` | perf(comments): replace per-id wisp loop with PartitionWispIDsInTx (be-ya2a2y) |

1 file changed: `internal/storage/issueops/comments.go` (3 insertions, 7 deletions).

## Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | be-7xtky2 notes: "Review verdict: PASS. No findings." Gemini second-pass disabled per current policy. |
| 2 | Acceptance criteria met | **PASS** | `GetCommentCountsInTx` now calls `PartitionWispIDsInTx(ctx, tx, issueIDs)` (single batched query). Per-id `IsActiveWispInTx` loop removed. Matches done-when exactly: O(N)→O(1) query. |
| 3 | Tests pass | **PASS** | `go build -tags gms_pure_go ./...` clean. `go test -tags gms_pure_go -count=1 -short ./internal/storage/issueops/... ./internal/storage/dolt/...`: all PASS (issueops 0.006s, dolt 5.101s). |
| 4 | No high-severity review findings open | **PASS** | 0 findings of any severity in be-7xtky2. |
| 5 | Final branch is clean | **PASS** | `git status` clean (untracked `.gc/`, `.gitkeep` are rig scaffolding). |
| 6 | Branch diverges cleanly from main | **PASS** | 1 commit ahead of `origin/main`. `git merge-tree` shows zero conflicts. |

## Test environment

- Host: Linux 6.19.14-300.fc44.x86_64
- `TMPDIR`/`GOTMPDIR` pinned to `~/.gotmp`.

## Push target

`PUSH_REMOTE=fork`. Cross-repo PR head: `quad341:fix/be-ya2a2y-comment-counts-wisp-partition`.

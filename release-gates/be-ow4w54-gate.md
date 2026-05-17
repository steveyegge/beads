# Release gate: be-ow4w54 — Defer chroma/glamour init: extract internal/ui/markdown.go to its own package

**Verdict: PASS.**

Branch: `feat/be-ow4w54-uimd-markdown`
Base on origin: `origin/main` (merge-base `d85881083`)
HEAD: `549c83631`
Source bead: `be-ow4w54`; review bead: `be-2y5h4f` (PASS).

## Commits

| # | SHA | Subject |
|---|-----|---------|
| 1 | `549c83631` | refactor(ui): extract markdown rendering to internal/uimd package |

5 files changed: `cmd/bd/comments.go`, `cmd/bd/show.go`, `cmd/bd/show_children.go`, `cmd/bd/show_display.go`, `internal/{ui→uimd}/markdown.go`.

## Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| 1 | Review PASS present | **PASS** | be-2y5h4f notes: "VERDICT: pass. Findings: none." |
| 2 | Acceptance criteria met | **PASS** | `internal/ui` contains no glamour/chroma imports (`grep -rn 'glamour\|chroma' internal/ui/` → empty). `internal/uimd/markdown.go` created and all 4 callers (show.go, show_display.go, show_children.go, comments.go) updated to import `internal/uimd`. Build clean. |
| 3 | Tests pass | **PASS** | `go test -tags gms_pure_go -count=1 -short ./internal/ui/... ./internal/uimd/...`: ok (0.005s). `go test -tags gms_pure_go -count=1 -short -run TestShow ./cmd/bd/`: ok (0.335s). `go build -tags gms_pure_go ./...`: clean. Note: `TestCheckBeadsRole_*` failures in `cmd/bd/doctor` are pre-existing rig-env failures unrelated to this change. |
| 4 | No high-severity review findings open | **PASS** | Reviewer be-2y5h4f: "Findings: none." |
| 5 | Final branch is clean | **PASS** | `git status` clean (untracked `.gc/`, `.gitkeep` are rig scaffolding). |
| 6 | Branch diverges cleanly from main | **PASS** | 1 commit ahead of merge-base. `git merge-tree` shows zero conflicts (main's flake.lock and workflow updates merge cleanly). |

## Test environment

- Host: Linux 6.19.14-300.fc44.x86_64

## Push target

`PUSH_REMOTE=fork`. Cross-repo PR head: `quad341:feat/be-ow4w54-uimd-markdown`.

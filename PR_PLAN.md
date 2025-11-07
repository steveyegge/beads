## Objective

Keep `ui-tests-refresh` tightly scoped to the refreshed UI + backend work, with no `.beads` noise, so it is ready to hand to yegge for review.

## Constraints and Guardrails

- Do not commit or push `.beads` metadata; keep tracker data local.
- Only carry over the functional changes needed for the UI refresh (backend plumbing, CLI entry points, UI/static, e2e suites).
- Preserve meaningful authorship, but squash where it improves readability.
- Ensure `go test ./...` plus the tagged UI/e2e suites are green before opening the PR.

## Branch Snapshot — Nov 7, 2025

- `ui-tests-refresh` is rebased on upstream `0106371` (release 0.22.1); `main` and the fork are in sync.
- Git history is organized into three commits: backend plumbing (`internal/daemonrunner`, RPC/storage/types), CLI helpers (`cmd/bd/ui*`, git helper), and the UI layer (internal UI APIs, static assets, e2e harness/screenshots), plus this planning note.
- Working tree is clean and `.beads` remains unstaged.
- Backend/RPC/storage layers supply the list, search, delete, events, and pagination behavior the UI depends on.
- CLI exposes `bd ui` with the workspace helpers/tests needed for local smoke and automation.
- UI/static delivers the redesigned surfaces with Playwright coverage and curated screenshots stored under `ui-*.png`.

## Validation — Nov 7, 2025

- `go test ./...` — ✅
- `go test -tags ui_e2e ./ui/e2e` — ✅ (`TestRedesignedCommandPaletteTrigger` now waits on `page.WaitForFunction`)
- `GOOS=windows GOARCH=amd64 go build -o bd.exe ./cmd/bd` — ✅
- Manual smoke: Python harness launched `bd daemon` + `bd ui --listen 127.0.0.1:60100`, `/healthz` and `/api/issues?queue=ready` both returned HTTP 200 with seeded data — ✅

## Screenshot Shortlist (checked in)

- `evidence-ui-after.png`
- `ui-smoke-home.png`
- `ui-test-detail.png`
- `ui-mcp-session-start.png`
- `ui-mcp-inprogress.png`
- `ui-mcp-after-filter.png`
- `ui-mcp-post-activity.png`

## Outstanding Work

1. Draft the PR summary (bullets + risks) and double-check no end-user docs outside `README.md` changed.
2. Decide whether to prune any legacy screenshots before opening the PR; do **not** capture new images unless the UI changes again.
3. Re-run `go test ./...`, `go test -tags ui_e2e ./ui/e2e`, and the manual smoke harness right before filing the PR, then open against `upstream/main`.

## Manual Smoke Checklist

1. **Prepare**
   - `cd d:\Users\Dan\GoogleDrivePersonal\code\github_projects_from_others\beads`
   - Ensure no lingering `bd.exe` processes: `pwsh -NoLogo -Command "tasklist | Select-String bd.exe"` → `Stop-Process -Id <pid>` if needed.
   - Build a Windows binary from the current tree: `bash -lc "GOOS=windows GOARCH=amd64 go build -o bd.exe ./cmd/bd"`.
2. **Launch**
   - Start the daemon: `pwsh -NoLogo -Command "$d = Start-Process -FilePath (Resolve-Path './bd.exe') -ArgumentList 'daemon' -PassThru; $d.Id"`
   - Start the UI without auto-opening a browser: `pwsh -NoLogo -Command "$u = Start-Process -FilePath (Resolve-Path './bd.exe') -ArgumentList 'ui','--no-open','--listen','127.0.0.1:60100' -PassThru; $u.Id"`
3. **Verify**
   - `curl http://127.0.0.1:60100/healthz` → expect `{"status":"ok"}`.
   - `curl http://127.0.0.1:60100/api/issues?queue=ready` → expect HTTP 200 with seeded issues.
   - (Optional) attach the MCP-controlled browser at `http://127.0.0.1:60100` for exploratory clicks.
4. **Tear Down**
   - `Stop-Process -Id <uiPid>`
   - `Stop-Process -Id <daemonPid>`
   - Delete any transient logs (`ui_*.log`, `smoke_result.json`, etc.) before committing.

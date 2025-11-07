## Objective

Rebuild the `ui-tests` work on top of the current `upstream/main` without carrying over historical bead-tracking commits or pushing `.beads` metadata. The resulting branch will contain only the functional and UI changes needed for the upcoming PR.

## Constraints and Guardrails

- Do not commit or push `.beads` directory changes; keep local bead tracking private.
- Prefer cherry-picking or manually reapplying only the necessary feature commits instead of rebasing 100+ historical commits.
- Preserve authorship metadata where it matters, but be willing to squash related work for clarity.
- Validate the refreshed branch with `go test ./...` and relevant UI checks before opening the PR.

## High-Level Steps

  - [x] **Update Local `main`**
    - [x] Fetch latest refs: `git fetch upstream`.
    - [x] Switch to `main`: `git checkout main`.
    - [x] Fast-forward to upstream: `git reset --hard upstream/main` (or `git pull --ff-only upstream main`).
    - [x] Push the fast-forwarded `main` to the fork if needed: `git push origin main`.
    - [x] _Nov 7, 2025:_ Upstream advanced to `0106371` (release 0.22.1, nested `.beads` guard, etc.). Re-ran the fetch/ff-only/push sequence (`git pull --ff-only upstream main`, `git push origin main`) and rebased `ui-tests-refresh` on the new tip.

- [x] **Create Fresh Branch**
  - [x] Create branch from updated `main`, e.g., `git checkout -b ui-tests-refresh`.
  - [ ] (Optional) Set upstream: `git push -u origin ui-tests-refresh`.

- [x] **Audit Existing Work**
  - [x] Run `git diff upstream/main..ui-tests` to list changes worth porting.
  - [x] Review `git log --stat upstream/main..ui-tests` to group commits.

_Findings:_ Legacy branch introduces an entire `ui/` tree (static assets + Go e2e harness) plus supporting backend changes (`cmd/bd/ui_*`, RPC tweaks, daemon/launcher helpers). Many `.beads` and documentation updates will be excluded from the refreshed branch.

**Current Reality Check (Nov 7, 2025):**
- ✅ `go test ./...` now green again after restoring `internal/daemonrunner`, fixing RPC cursor regression, and ensuring CLI harness builds a Linux bd binary instead of trying to run a Windows PE executable.
- ⚠️ Manual smoke validation can’t proceed until the Chrome DevTools proxy (Chrome 127.0.0.1:9223 ←→ ::1:9222) actually accepts connections. This remains the gating item for UI sign-off.
- ⚠️ `.beads` noise and screenshots/log artifacts are still staged; history needs to be sculpted before any PR.

- **Current Status (Nov 5, 2025)**  
- ✅ Backend RPC/storage enhancements (sort order, cursors, orphan handling) landed; `go test ./...` green with UI reapplied.  
- ✅ UI layer (`internal/ui/*`, `ui/static/*`) restored with hover/focus polish and vendored dependencies for offline operation.  
- ✅ Playwright/UI harness fully passing (`go test -tags ui_e2e ./ui/e2e`).  
- ✅ CLI `bd ui` workflow, RPC endpoints, and diagnostics helpers reintroduced with tests.

- [ ] **Port Intended Changes**
  - **Backend Foundation (do first)**
    - [x] Temporarily stash/branch the UI tree so backend can compile & test in isolation. _(stash `ui-refresh snapshot`)_
    - [x] Cherry-pick daemon/process management fixes that the backend needs (PID/launcher helpers only if required for tests).  
      _Decision:_ confirmed not required for this refresh; no action needed.
    - [x] Port RPC/client/server updates (watch events, delete endpoint, advanced list filters, response status codes). _(added `server_events`, watch routing, delete handler, client + protocol support)_
    - [x] Extend storage/query layer for new filter/sort behavior (ID prefix, closed cursor, orphan handling tweaks).  
      _Coverage:_ sqlite + memory storage, RPC integration, and regression tests for pagination/sorts.
    - [x] Restore green build: `go test ./...` must succeed before reintroducing UI. _(passes locally after backend wiring + SQLite scanner update)_
  - **Frontend/UI Layer (after backend passes tests)**
    - [x] Cherry-pick or manually reapply UI refresh changes. _(ui/static and internal/ui packages brought over — will reapply after backend commit)_
    - [x] Add required module deps for renderer/tests (`bluemonday`, `goldmark`, Playwright harness).
    - [x] Recreate any additional tweaks (assets, scripts, CSS) that were trimmed during backend isolation.  
      _Action:_ restored hover/focus styling + relative time copy; continue diffing legacy branch for stragglers.
    - [x] After each chunk, ensure branch builds and stays green (backend + UI Playwright suites).  
      _Run status:_ `go test ./...` green; Playwright harness still queued.

- [ ] **Clean Up History**
  - [ ] Squash or reorder commits for clarity.
  - [ ] Confirm `git status` excludes `.beads` files.
  - [ ] Sweep transient UI/daemon logs before sculpting commits (`rm ui_server*.log .beads/daemon-global.log .beads/ui-session.json` etc.) to keep the diff signal-only for review.
  - [ ] Inline-rule: every time new assets/logs appear (e.g., `ui-mcp-*.png`, `ui-session-long-*.png`), decide per-file whether they belong in this PR before staging history again.

- [ ] **Validation**
  - [x] Run `go test ./...`. _(Passing on Nov 7, 2025 after daemonrunner import + CLI test harness fixes.)_
  - [x] Cross-compile the CLI for Windows to catch platform regressions (`GOOS=windows GOARCH=amd64 go build ./cmd/bd`); yegge reviews from Windows, so tighten this before asking for feedback. _(Nov 7, 2025: `GOOS=windows GOARCH=amd64 go build -o /tmp/bd-win.exe ./cmd/bd` succeeded.)_
  - [x] Execute relevant UI/build checks (Playwright or npm scripts). _(Vendored browser dependencies, restored CLI wiring, and now both `go test ./...` and `go test -tags ui_e2e ./ui/e2e` pass.)_
  - [ ] Smoke-check application if feature requires it (details below).
- [ ] **Prepare PR Assets**
  - [ ] Update to the latest code. Run all tests and ensure nothing is broken.
  - [ ] Review each file change and ensure that it is necessary for the PR. Delete unnecessary files. Revert changes that aren't necessary for the fix. Minimize pointless churn.
    - Special watch items: reintroduced `internal/daemonrunner/*`, `cmd/bd/git_test_helper_nonintegration_test.go`, and the host of `ui-session-long-*.png` screenshots. Decide which of those belong vs. should be moved to a follow-on bead before history cleanup.
  - [ ] Run all tests, then run manual smoke tests (below) to make sure everything still works. 
  - [ ] Draft PR summary and acceptance details.
  - [ ] Capture before/after visuals if UI changed.
  - [ ] Update end-user docs only if necessary (otherwise keep notes here).

## PR Collateral Draft (Nov 7, 2025)

**Summary bullets**
- Rebased `ui-tests-refresh` on `0106371` and sculpted history into three logical commits: backend plumbing (`internal/daemonrunner`, RPC/storage/types), CLI helpers (`cmd/bd/ui*`, new git helper), and UI surface (internal UI APIs, static assets, e2e harness, curated screenshots).
- Purged `.beads` spillover plus the long `ui-session-long-*` capture set; kept only the screenshots that tell the PR story (home, detail, MCP states, before/after evidence).
- `go test ./...` passes locally on Nov 7, 2025; after switching the palette test to `WaitForFunction`, the ui_e2e suite is now green headless as well.

**Screenshot shortlist (checked in)**
- `evidence-ui-after.png`
- `ui-smoke-home.png`
- `ui-test-detail.png`
- `ui-mcp-session-start.png`
- `ui-mcp-inprogress.png`
- `ui-mcp-after-filter.png`
- `ui-mcp-post-activity.png`

**Test + smoke log**
- ✅ `go test ./...` (Nov 7, 2025 01:32 PT) — all packages green after commit restructuring.
- ✅ `go test -tags ui_e2e ./ui/e2e` (Nov 7, 2025 01:55 PT) — switching `TestRedesignedCommandPaletteTrigger` to `page.WaitForFunction` fixed the headless flake; the full suite now passes consistently.
- ✅ Targeted headed repro (Nov 7, 2025 01:44 PT): `BD_E2E_HEADLESS=false go test -tags ui_e2e ./ui/e2e -run TestRedesignedCommandPaletteTrigger -count=1` confirmed the palette focuses correctly with a visible browser (kept for future debugging reference).
- ✅ Manual smoke (Nov 7, 2025 13:55 PT): Python harness spawned `bd daemon`/`bd ui --listen 127.0.0.1:60100`, hit `/healthz` (200) and `/api/issues?queue=ready` (200) returning seeded issue payload—no screenshots captured per guidance.

**Outstanding risks / follow-ups**
- Re-run ui_e2e + manual smoke immediately before filing the PR if the UI changes again (reuse the Python harness; still no new screenshots requested).

- [ ] **Finalize and Share**
  - [ ] Verify no `.beads` changes staged. 
  - [ ] Refresh local bead metadata from upstream before the last push so we never clobber yegge's tracker: `git fetch upstream` then `git show upstream/main:.beads/beads.jsonl > /tmp/upstream-beads.jsonl` for a quick diff (discard afterwards; do not stage it).
  - [ ] Push branch when ready: `git push origin ui-tests-refresh`.
  - [ ] Open PR targeting `upstream/main`.

## Follow-Up Tasks

- Consider pruning or archiving the old `ui-tests` branch once the refreshed branch replaces it.
- If upstream diverges while porting, rerun Steps 1–2 to rebase the new branch before final validation.

## Quick Reference / Next Session Checklist

- **Branch:** `ui-tests-refresh`
- **UI stash:** `git stash apply stash^{/ui-refresh snapshot}`
- **Backend done:** RPC delete + watch events, storage filters/pagination, orphan handling, tests passing.
- **Frontend re-applied:** UI + static assets in tree; CSS adjustments landed.
- **Next up:**
  1. Draft the PR summary (bullets + risks) and ensure README remains untouched.
  2. Decide if the existing screenshot set is sufficient or if legacy captures should be pruned for brevity (no new captures requested).
  3. Once narrative is ready, push any remaining tweaks and open the PR targeting `upstream/main`.


# Manual Smoke Test Setup

Prepare the workspace

cd d:\Users\Dan\GoogleDrivePersonal\code\github_projects_from_others\beads
Ensure no leftover bd daemons/UI servers: pwsh -NoLogo -Command "tasklist | Select-String bd.exe" → if any, Stop-Process -Id <pid>.
Build a Windows binary so you’re using repo code: bash -lc "GOOS=windows GOARCH=amd64 go build -o bd.exe ./cmd/bd".
Launch daemon + UI from the repo build

Start daemon: pwsh -NoLogo -Command "$daemon = Start-Process -FilePath (Resolve-Path './bd.exe') -ArgumentList 'daemon' -PassThru; $daemon.Id"
Start UI without auto-opening a browser: pwsh -NoLogo -Command "$ui = Start-Process -FilePath (Resolve-Path './bd.exe') -ArgumentList 'ui','--no-open','--listen','127.0.0.1:60100' -PassThru; $ui.Id"
Confirm with Get-CimInstance Win32_Process -Filter "name='bd.exe'" | Select ProcessId,CommandLine.

# Release Gate: be-t5hh3u daemon+postgres stack

**Branch:** `feat/be-t5hh3u-daemon-child-endpoint`  
**Branch tip:** `3a59ad0f4`  
**Gate date:** 2026-05-16  
**Verdict: FAIL (criterion 3) → FIXED by be-vk7ovi (3a59ad0f4) — re-gate pending**

## Build beads (reviewed)

| Build bead | Feature | Review bead | Verdict |
|---|---|---|---|
| be-2dv4s2 | bd daemon subcommands, sql.go exit-1 fix | be-p0t6f8 | PASS |
| be-s67c8m | postgres env-override wiring + TestOpen_ErrorFormat_WithOverride | be-m4mupw | PASS |
| be-3nj8kh | bd backend status command | be-6phsqa | PASS |
| be-nzo2iu | CheckLegacyDoltArtifacts false-positive fix | be-1re4uh | PASS |

## Gate criteria

### 1. Review PASS present — PASS

All four build beads have a reviewer PASS verdict in their review bead notes:
- be-p0t6f8: "REVIEWER VERDICT: PASS" (be-2dv4s2 sql exit code fix)
- be-m4mupw: "REVIEWER VERDICT: PASS" (be-s67c8m TestOpen_ErrorFormat_WithOverride)
- be-6phsqa: "REVIEWER VERDICT: PASS" (be-3nj8kh bd backend status)
- be-1re4uh: "Review Verdict: PASS" (be-nzo2iu legacy artifacts fix)

Note: commit `7cd0d45ed` (be-s67c8m comment-only fix: "TCP probe" → "authentication handshake")
sits above the last reviewed commit `7c6caaa76`. The change is 4 lines of comment text
only; no code logic changed. This is within the scope of be-m4mupw's review of the
postgres store code.

### 2. Acceptance criteria met — PASS (per reviewed build beads)

- **be-2dv4s2**: daemon status/kill/stats subcommands present; sql.go exits 1 on daemon-mode RawDBAccessor miss; init --reinit kills daemon. All per be-oyer9z §5.3/§4/§7.
- **be-s67c8m**: `dsn.ApplyEnvOverrides` wired into runtime path; `TestOpen_ErrorFormat_WithOverride` passes (`go test ./internal/storage/postgres/... -run TestOpen_ErrorFormat_WithOverride` → PASS 0.005s per reviewer).
- **be-3nj8kh**: `bd backend status` outputs text+JSON; exit 0 healthy/exit 1 unhealthy; password never in output.
- **be-nzo2iu**: `CheckLegacyDoltArtifacts` uses `bi.LegacyDoltDir` (backend-gated); no false positives on Dolt workspaces. Reviewer confirmed single-file review of `cmd/bd/doctor/legacy_dolt_artifacts.go`.

### 3. Tests pass — FAIL ⛔

Command: `go test ./...` on branch tip `7cd0d45ed`

**Pre-existing failures (also fail on origin/main ad046c96c):**
```
FAIL: TestWhereCommand_ReadsPrefixFromEmbeddedStore (cmd/bd)
FAIL: TestCheckBeadsRole_NotConfigured (cmd/bd/doctor)
FAIL: TestCheckBeadsRole_NotGitRepo (cmd/bd/doctor)
FAIL: TestCheckServerReachable_UnreachableHost (cmd/bd/doctor)
FAIL: TestRole_NoConfig (internal/beads)
FAIL: TestRequireRole_NotConfigured (internal/beads)
FAIL: TestGitCmd_WorktreeContext (internal/beads)
```
These are pre-existing environment-specific failures present on main. Not a regression.

**New failure (passes on main, fails on feature branch):**
```
--- FAIL: TestInitBackendFlag/unknown_backend_errors (cmd/bd/init_test.go:2051)
    init_test.go:2051: Expected 'unknown backend' error, got: Error: no local
    Postgres cluster found at /tmp/beads-bd-tests-.../.local/share/beads/postgres/data;
    either start it, pass --pg-host=<remote>, or pass --dsn=<full>
```

**Root cause:** The test was written when `--backend=postgres` was unsupported (it expected
"unknown backend" error). Commit `bdee54845` (`be-fsobdu`) implemented `--backend=postgres`
support, making the test stale. The test now needs to use a genuinely unknown backend value
(e.g., `--backend=notavalidbackend`) to test unknown-backend rejection.

**Fix required (trivial):**
In `cmd/bd/init_test.go` around line 2043, change:
```go
cmd := exec.Command(bd, "init", "--backend", "postgres", "--quiet")
```
to:
```go
cmd := exec.Command(bd, "init", "--backend", "notavalidbackend", "--quiet")
```
This tests what the test was always meant to test (unknown backends fail) without conflicting
with the now-supported postgres backend.

### 4. No high-severity review findings open — PASS

Findings from reviewers:
- be-6phsqa noted: minor doc inconsistency (INFO level, BackendStatusResult comment). Not HIGH.
- be-1re4uh noted: LOW observation (secondary message hardcodes 'postgres'). Not blocking.
- No HIGH-severity unresolved findings.

### 5. Final branch is clean — PASS

`git status` on branch tip `7cd0d45ed`: clean, no uncommitted changes.

### 6. Branch diverges cleanly from main — PASS

`git log origin/main..feat/be-t5hh3u-daemon-child-endpoint` shows 40 commits, no merge
conflicts detected.

## Summary

**GATE RESULT: FAIL**

Criterion 3 (tests pass) fails due to `TestInitBackendFlag/unknown_backend_errors`.
Fix is a 1-line test update in `cmd/bd/init_test.go:2043` (change `--backend=postgres` to
`--backend=notavalidbackend`). Route to builder for fix, then re-submit to deployer.

# Smoke Test Results

_Date:_ October 21, 2025  
_Tester:_ Codex (GPT-5)  
_Environment:_  
- Linux run: WSL (Ubuntu), Go 1.24.0, locally built `bd` binary  
- Windows run: Windows 11 (via WSL interop), cross-compiled `bd.exe`

## Scope

- Full CLI lifecycle using local SQLite database: init, create, list, ready/blocked, label ops, deps, rename, comments, markdown import/export, delete (single & batch), renumber, auto-flush/import behavior, daemon interactions (local mode fallback).
- JSONL sync verification.
- Error handling and edge cases (duplicate IDs, validation failures, cascade deletes, daemon fallback scenarios).

## Test Matrix – Linux CLI (`bd`)

| Test Case | Description | Status | Notes |
|-----------|-------------|--------|-------|
| Init-001 | Initialize new workspace with custom prefix | ✅ Pass | `/tmp/bd-smoke`, `./bd init --prefix smoke` |
| CRUD-001 | Create issues with JSON output (task/feature/bug) | ✅ Pass | Created smoke-1..3 via `bd create` with flags |
| Read-001 | Verify list/ready/blocked views (human & JSON) | ✅ Pass | `bd list/ready/blocked` with `--json` |
| Label-001 | Add/remove/list labels | ✅ Pass | Added backend label to smoke-2 and removed |
| Dep-001 | Add/remove dependency, view tree, cycle prevention | ✅ Pass | Added blocks, viewed tree, removal succeeded, cycle rejected |
| Comment-001 | Add/list comments (direct mode) | ✅ Pass | Added inline + file-based comments to smoke-3; verified JSON & human output |
| ImportExport-001 | Manual export + import new issue | ✅ Pass | `bd export -o export.jsonl`; imported smoke-4 from JSONL |
| Delete-001 | Single delete preview/force flush check | ✅ Pass | smoke-4 removed; `.beads/issues.jsonl` updated |
| Delete-002 | Batch delete multi issues | ✅ Pass | Deleted smoke-5 & smoke-6 with `--dry-run`, `--force` |
| ImportExport-002 | Auto-import detection from manual JSONL edit | ✅ Pass | Append smoke-8 to `.beads/issues.jsonl`; `bd list` auto-imported |
| Renumber-001 | Force renumber to close gaps | ✅ Pass | `bd renumber --force --json`; IDs compacted |
| Rename-001 | Prefix rename dry-run | ✅ Pass | `bd rename-prefix new- --dry-run` |

## Test Matrix – Windows CLI (`bd.exe`)

| Test Case | Description | Status | Notes |
|-----------|-------------|--------|-------|
| Win-Init-001 | Initialize workspace on `D:\tmp\bd-smoke-win` | ✅ Pass | `/mnt/d/.../bd.exe init --prefix win` |
| Win-CRUD-001 | Create task/feature/bug issues | ✅ Pass | win-1..3 via `bd.exe create` |
| Win-Read-001 | list/ready/blocked output | ✅ Pass | `bd.exe list/ready/blocked` |
| Win-Label-001 | Label add/list/remove | ✅ Pass | `platform` label on win-2 |
| Win-Dep-001 | Add dep, cycle prevention, removal | ✅ Pass | win-2 blocks win-1; cycle rejected |
| Win-Comment-001 | Add/list comments | ✅ Pass | Added comment to win-3 |
| Win-Export-001 | Export + JSONL inspection | ✅ Pass | `bd.exe export -o export.jsonl` |
| Win-Import-001 | Manual JSONL edit triggers auto-import | ✅ Pass | Appended `win-4` directly to `.beads\issues.jsonl` |
| Win-Delete-001 | Delete issue with JSONL rewrite | ✅ Pass | `bd.exe delete win-5 --force` (initial failure -> B-001; retest after fix succeeded) |

## Bugs / Issues

| ID | Description | Status | Notes |
|----|-------------|--------|-------|
| B-001 | `bd delete --force` on Windows warned `Access is denied` while renaming issues.jsonl temp file | ✅ Fixed | Closed by ensuring `.beads/issues.jsonl` reader closes before rename (`cmd/bd/delete.go`) |

## Follow-up Actions

| Action | Owner | Status |
|--------|-------|--------|

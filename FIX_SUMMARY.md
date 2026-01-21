# Fix Summary: GH#1224 - Stack Overflow in bd on WSL2 with SQLite WAL Locking Error

## Issue Description
The `bd` CLI tool crashed with a stack overflow error when running on WSL2 with a repository located on a Docker Desktop bind mount (`/mnt/wsl/docker-desktop-bind-mounts/...`). The crash occurred due to:

1. **SQLite WAL Mode Incompatibility**: WAL mode fails with "sqlite3: locking protocol" error on network filesystems like Docker Desktop bind mounts
2. **Infinite Recursion**: When WAL mode failed, the daemon's `acquireStartLock` function could theoretically enter infinite recursion

## Root Cause Analysis

### Primary Issue: WAL Mode on Network Filesystems
SQLite's Write-Ahead Logging (WAL) mode requires specific filesystem semantics (especially shared memory locking) that are not available on network filesystems. The existing code detected WSL2 Windows paths (`/mnt/[a-zA-Z]/`) but did not detect Docker Desktop bind mounts (`/mnt/wsl/*`), which are also network filesystems with the same limitations.

### Secondary Issue: Bounded Recursion
The current code in `acquireStartLock` (line 267 of daemon_autostart.go) already has proper bounds checking with `maxRetries = 3`, preventing infinite recursion. This fix was already in place.

## Solution

### 1. Enhanced Path Detection (internal/storage/sqlite/store.go)
- Added `wslNetworkPathPattern` regex to detect `/mnt/wsl/*` paths (Docker Desktop bind mounts)
- Expanded `isWSL2WindowsPath()` function to check for both:
  - Windows filesystem mounts: `/mnt/[a-zA-Z]/` 
  - Network filesystem mounts: `/mnt/wsl/*`
- Optimized logic to check WSL2 environment once (cheaper /proc/version check) before path matching

### 2. Added Comprehensive Tests
Created `internal/storage/sqlite/store_wsl_test.go` with tests for:
- Windows filesystem detection (`/mnt/c/`, `/mnt/d/`, etc.)
- Docker Desktop bind mount detection (`/mnt/wsl/docker-desktop-bind-mounts/`)
- Native WSL2 filesystem paths (allow WAL mode)
- Edge cases and path pattern matching
- Journal mode selection logic

### 3. Integration Test
Created `test_issue_gh1224.sh` to verify:
- Database creation on native WSL2 filesystem
- Proper handling of Windows paths (when available)
- Proper handling of Docker bind mount paths (when available)

## Changes Made

### Modified Files
1. **internal/storage/sqlite/store.go**
   - Added `wslNetworkPathPattern` regex variable
   - Updated `isWSL2WindowsPath()` function
   - Updated documentation with GH#1224 reference

### New Files
1. **internal/storage/sqlite/store_wsl_test.go**
   - Unit tests for WSL2 path detection
   - Tests for journal mode selection
   - Edge case tests

2. **test_issue_gh1224.sh**
   - Integration test script for manual verification

3. **test_wsl2_wal.sh**
   - Diagnostic script for WAL mode testing (requires sqlite3 CLI)

## Verification

### Test Results
- All existing tests pass (13.2s runtime for cmd/bd tests)
- New WSL2 detection tests pass (3/3)
- Docker bind mount detection tests pass
- Journal mode selection tests pass
- Daemon autostart tests confirm bounded recursion (maxRetries=3)

### Build Status
- Successfully builds: `go build ./cmd/bd`
- No syntax errors or regressions

## How the Fix Works

### Before
```
User on WSL2 with Docker bind mount (/mnt/wsl/docker-desktop-bind-mounts/...)
  ↓
bd init / bd ready / bd sync
  ↓
Database initialization (sqlite/store.go:NewWithTimeout)
  ↓
isWSL2WindowsPath() → false (only checked /mnt/[a-zA-Z]/)
  ↓
WAL mode enabled (PRAGMA journal_mode=WAL)
  ↓
Database connection fails: "sqlite3: locking protocol"
  ↓
Daemon retry loop (eventually bounded by maxRetries)
  ↓
Stack overflow / Timeout (user sees warning)
```

### After
```
User on WSL2 with Docker bind mount (/mnt/wsl/docker-desktop-bind-mounts/...)
  ↓
bd init / bd ready / bd sync
  ↓
Database initialization (sqlite/store.go:NewWithTimeout)
  ↓
isWSL2WindowsPath() → true (detects /mnt/wsl/ pattern)
  ↓
WAL mode disabled (PRAGMA journal_mode=DELETE)
  ↓
Database connection succeeds
  ↓
Command executes normally
```

## References
- GH#1224: Stack Overflow in bd on WSL2 with SQLite WAL Locking Error
- GH#920: SQLite WAL mode on Windows filesystem mounts in WSL2
- SQLite WAL Limitations: https://www.sqlite.org/wal.html#nfs

## Testing Recommendations for Review
1. Run tests locally in WSL2: `go test -v ./internal/storage/sqlite -run TestIsWSL2WindowsPath`
2. Test database creation in different paths:
   - Native WSL2: `/home/user/project/.beads/`
   - Windows: `/mnt/c/Users/.beads/` (if available)
   - Docker bind mount: `/mnt/wsl/docker-desktop-bind-mounts/.../` (if available)
3. Verify daemon starts without stack overflow or excessive retries

# ZFC Resurrection Bug Investigation

## Problem
When `bd sync` runs with a stale DB (more issues than JSONL), it should trust JSONL as source of truth. Instead, it re-exports the stale DB back to JSONL, "resurrecting" deleted issues.

## Reproduction
1. Clean JSONL has 74 issues
2. Stale DB has 122 issues (from previous bad sync)
3. Run `bd sync`
4. Expected: JSONL stays at 74
5. Actual: JSONL becomes 122 (polluted)

## Root Cause Analysis

### The Sync Flow
1. **Step 1**: ZFC check - if DB > JSONL by >50%, import first and set `skipExport=true`
2. **Step 1b**: If `!skipExport`, export DB to JSONL
3. **Step 2**: Commit changes
4. **Step 3**: Pull from remote
5. **Step 4**: Import pulled JSONL
6. **Step 4.5**: Check if re-export needed (`skipReexport` variable)
7. **Step 5**: Push

### The Bug
The post-pull re-export check (Step 4.5) has its own `skipReexport` variable that was NOT inheriting from the initial `skipExport` flag.

**Original code (line 306):**
```go
skipReexport := false  // BUG: doesn't inherit skipExport
```

**Fixed code:**
```go
skipReexport := skipExport  // Carry forward initial ZFC detection
```

### Why Fix Didn't Work
Even with the fix, the bug persists because:

1. **Multiple pollution paths**: The initial export (Step 1b) can pollute JSONL before pull
2. **Import doesn't delete**: When 74-line JSONL is imported to 122-issue DB, the 48 extra issues remain
3. **Post-import check fails**: After import, DB still has 122, JSONL has 74 (from pull), but `dbNeedsExport()` sees differences and triggers re-export

### Key Insight
The import operation only **creates/updates** - it never **deletes** issues that exist in DB but not in JSONL. So even after importing a 74-issue JSONL, the DB still has 122 issues.

## Proposed Solutions

### Option 1: Use `--delete-missing` flag during ZFC import
When ZFC is detected, import with deletion of missing issues:
```go
if err := importFromJSONL(ctx, jsonlPath, renameOnImport, true /* deleteMissing */); err != nil {
```

### Option 2: Skip ALL exports after ZFC detection
Make `skipExport` a more powerful flag that blocks all export paths, including post-pull re-export.

### Option 3: Add explicit ZFC mode
Track ZFC state throughout the entire sync operation and prevent any export when in ZFC mode.

## Files Involved
- `cmd/bd/sync.go:126-178` - Initial ZFC check and export
- `cmd/bd/sync.go:303-357` - Post-pull re-export logic
- `cmd/bd/integrity.go:289-301` - `validatePostImport()` function
- `cmd/bd/import.go` - Import logic (needs `--delete-missing` support)

## Current State
- Remote (origin/main) has 74-line JSONL at commit `c6f9f7e`
- Local DB has 122 issues (stale)
- Fix attempt at line 306 (`skipReexport := skipExport`) is in place but insufficient
- Need to also handle the import-without-delete issue

## Next Steps
1. Clean up: `git reset --hard c6f9f7e` (or earlier clean commit)
2. Delete local DB: `rm -f .beads/beads.db`
3. Import with delete-missing to sync DB with JSONL
4. Implement proper fix that either:
   - Uses delete-missing during ZFC import, OR
   - Completely blocks re-export when ZFC is detected

## Version
This should be fixed in v0.24.6 (v0.24.5 had the resurrection bug).

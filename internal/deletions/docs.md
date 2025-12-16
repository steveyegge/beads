# Noridoc: internal/deletions

Path: @/internal/deletions

### Overview

The `deletions` package manages the deletions manifest—an append-only JSONL log that tracks when issues are deleted. This manifest enables deletion propagation across repository clones during git sync operations, ensuring deletions are consistent across distributed workspaces.

### How it fits into the larger codebase

- **Deletion Propagation Pipeline**: When issues are deleted via `@/cmd/bd/delete.go`, deletion records are appended to the manifest via `AppendDeletion()`. During sync operations (`@/cmd/bd/sync.go`), the manifest is loaded and applied to the database to ensure deletions persist across clones.

- **Integration with Importer**: The `@/internal/importer/importer.go` loads the manifest at startup via `LoadDeletions()` to determine which issues should be skipped during initialization and syncing. Deletion records prevent deleted issues from being re-imported from git history.

- **Tombstone System Interaction**: The manifest feeds into the tombstone migration system (`@/cmd/bd/migrate_tombstones.go`). When migration completes, deletion records are converted to inline `Status == "tombstone"` entries in `issues.jsonl`, and the manifest is archived to prevent re-application.

- **Doctor/Fix System**: The doctor fix module (`@/cmd/bd/doctor/fix/deletions.go`) uses `LoadDeletions()` and `WriteDeletions()` to hydrate the manifest with historical deletions detected by comparing JSONL against git history. This ensures the manifest is complete after recovery operations.

- **Database State Consistency**: When loading the manifest (`LoadDeletions()`), corrupt or malformed lines are gracefully skipped with warnings rather than failing. This prevents database initialization from failing due to committed merge conflicts or transient corruption.

- **Integrity Checks**: The `@/cmd/bd/integrity.go` module loads the manifest to validate it contains only valid deletion records with required fields (ID, timestamp, actor).

### Core Implementation

**Data Structure and I/O** (`deletions.go`):

1. **DeletionRecord** (lines 17-24):
   - Represents a single deletion entry in the manifest
   - Fields: `ID` (issue ID), `Timestamp` (RFC3339 formatted deletion time), `Actor` (who deleted it), `Reason` (optional explanation)
   - Serialized as single-line JSON (JSONL format) in the manifest file

2. **LoadResult** (lines 26-31):
   - Container for the results of loading the manifest
   - Contains `Records` (map keyed by issue ID for O(1) lookup), `Skipped` (count of malformed lines), and `Warnings` (list of skip reasons)
   - Allows partial loading success—valid records are loaded even if some lines are corrupt

3. **LoadDeletions()** (lines 55-118):
   - Entry point for reading the manifest file line-by-line using buffered scanning
   - Allows up to 1MB line sizes to accommodate very long deletion reasons
   - **Merge Conflict Marker Handling (GH#590)**: Calls `isMergeConflictMarker()` before JSON parsing to detect and skip git conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`)
   - Skips empty lines (lines 80-82)
   - Attempts JSON parsing; if parsing fails, skips the line and records a warning
   - Validates that `ID` field is non-empty (lines 101-107)
   - Implements "last write wins" semantics (line 110): if an ID appears multiple times, the last record overwrites previous ones
   - Returns gracefully if file doesn't exist (lines 63-65)

4. **isMergeConflictMarker()** (lines 33-50) - NEW:
   - Detects git merge conflict markers that may exist in the manifest
   - Checks if a trimmed line starts with exactly 7 characters of `<<<<<<<`, `=======`, or `>>>>>>>`
   - Validates that characters after the marker are whitespace or reference names (not JSON quotes)
   - Prevents false positives from JSON strings containing these markers
   - Returns `true` if the line is a merge conflict marker, allowing `LoadDeletions()` to skip it

5. **AppendDeletion()** (lines 120-159):
   - Appends a single deletion record to the manifest in append-mode
   - Creates the file and parent directories if they don't exist
   - Validates that `ID` is non-empty (line 125)
   - Calls `Sync()` after writing to ensure durability of the append-only log (line 154)
   - Used by delete commands and batch deletion operations

6. **WriteDeletions()** (lines 161-205):
   - Atomically rewrites the entire manifest (replaces all contents)
   - Creates a temporary file in the same directory, writes records to it, then renames it
   - Used for compaction/deduplication operations (via `PruneDeletions()` and `RemoveDeletions()`)
   - An empty slice creates an empty manifest file (clears all deletions)

7. **Count()** (lines 224-251):
   - Fast operation that counts non-empty lines without parsing JSON
   - Used for diagnostics and logging
   - Returns 0 gracefully if file doesn't exist

**Deletion Lifecycle Operations**:

8. **PruneDeletions()** (lines 260-308):
   - Removes deletion records older than a specified retention period
   - Loads all records, filters by age, and rewrites the manifest atomically
   - Returns counts of kept/pruned records and list of pruned IDs
   - Only rewrites if something was actually pruned (optimization)
   - Sorts records deterministically by ID before rewriting (lines 285-287)

9. **RemoveDeletions()** (lines 317-370):
   - Removes specific issue IDs from the manifest (used when issues are hydrated from git history)
   - Builds a set of IDs to remove for O(1) lookup (lines 340-343)
   - Rewrites manifest atomically with remaining records
   - Sorts records deterministically before rewriting (lines 361-363)

10. **IsTombstoneMigrationComplete()** (lines 213-222):
    - Checks if migration to the tombstone system has completed
    - Looks for a marker file `deletions.jsonl.migrated` (created by `@/cmd/bd/migrate_tombstones.go`)
    - Returns `true` if migration is complete; subsequent operations should NOT write new deletion records

11. **DefaultPath()** (lines 207-211):
    - Returns the default path for the manifest: `.beads/deletions.jsonl`

### Things to Know

**Merge Conflict Marker Handling (GH#590 Fix)**:

The root cause of GH#590 was merge conflicts committed to the `deletions.jsonl` file in git history. When users ran `bd reset --force` followed by `git reset --hard` and then `bd init`, the file with conflict markers was restored. The JSON parser would fail on lines like `<<<<<<< HEAD`, causing initialization to fail and corrupting the database state.

The fix adds `isMergeConflictMarker()` which detects git conflict markers before JSON parsing. When detected, the line is skipped with a warning (similar to how corrupt JSON is handled) rather than causing a parse error. This allows `LoadDeletions()` to gracefully handle committed merge conflicts without failing initialization.

**Why This Matters for State**:

The deletions manifest is critical for database consistency. If `LoadDeletions()` fails entirely, the importer cannot determine which issues should be skipped, leading to:
- Deleted issues being re-added to the database during sync
- Inconsistent deletion state across clones
- Database corruption requiring manual recovery

By skipping merge conflict markers gracefully, initialization succeeds and the database reaches a consistent state, even if some deletion records are lost (acceptable trade-off for availability).

**Append-Only Design**:

The manifest is append-only to support distributed operation:
- Each write is a new line (single-line JSON)
- Enables efficient streaming loads
- Allows merge conflict detection at the line level
- "Last write wins" semantics handle duplicate IDs naturally
- Compaction (`WriteDeletions()`) is an explicit operation, not automatic

**Load Robustness**:

`LoadDeletions()` is designed to be resilient:
- Skips empty lines (no warning)
- Skips lines that are merge conflict markers (warning about line number)
- Skips malformed JSON (warning with parse error)
- Skips valid JSON missing required fields like ID (warning)
- Never fails the entire load—partial success is acceptable

This robustness is critical because the manifest may be corrupted during git operations or manual editing, and initialization must succeed to maintain workspace availability.

**Integration with Tombstone System**:

After `bd migrate-tombstones` completes:
1. All deletion records are converted to inline tombstones in `issues.jsonl` with `Status == "tombstone"`
2. A marker file `deletions.jsonl.migrated` is created
3. `IsTombstoneMigrationComplete()` returns `true`
4. New deletion operations should NOT append to the manifest (as verified by caller logic)
5. The manifest becomes a historical archive of pre-migration deletions

This separation ensures the system doesn't maintain duplicate deletion state between the manifest and tombstones.

**Deterministic Output**:

When rewriting the manifest (`WriteDeletions()`), records are sorted by ID (lines 285-287 in `PruneDeletions()`, lines 361-363 in `RemoveDeletions()`). This ensures:
- Consistent output across runs
- Deterministic git diffs for version control
- Reproducible deletion propagation

Created and maintained by Nori.

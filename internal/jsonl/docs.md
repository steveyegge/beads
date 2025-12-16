# Noridoc: internal/jsonl

Path: @/internal/jsonl

### Overview

The `jsonl` package provides utilities for reading, parsing, and cleaning JSONL (JSON Lines) issue files. Its primary responsibility is normalizing corrupted JSONL data during initialization, removing duplicates, filtering test pollution, repairing broken references, and validating the entire dataset before database import.

### How it fits into the larger codebase

- **Auto-Import Integration** (@/cmd/bd/autoimport.go): When `bd init` detects a fresh clone with issues in git but an empty database, it invokes the cleaning pipeline to normalize JSONL before calling `importIssuesCore()`. This prevents UNIQUE constraint violations and database corruption during fresh initialization.

- **Data Type Dependencies** (@/internal/types): Works with `types.Issue`, `types.Dependency`, and status/type enums. The package preserves issue structure while removing corrupted or redundant entries.

- **Import/Export Flow**: Sits between raw JSONL on disk (`@/cmd/bd/import.go`) and the database import layer (`@/internal/importer`, `@/internal/storage`). Acts as a filter/normalizer before data reaches the database.

- **No Database Connection**: This package operates entirely on in-memory issue slices—it does not interact with the storage layer, enabling use during initialization before the database is fully available.

### Core Implementation

**Reading JSONL** (`reader.go`):

1. **ReadIssuesFromFile()** (lines 14-51):
   - Opens and reads an entire JSONL file into memory
   - Uses buffered scanning with 64MB per-line buffer to handle large descriptions
   - Skips empty lines (line 35-37)
   - Returns error on JSON parse failures with line number context
   - Designed for initial data loading and testing

2. **ReadIssuesFromData()** (lines 54-80):
   - Identical to ReadIssuesFromFile but reads from in-memory byte slice
   - Used by `importFromGit()` to parse git-stored JSONL (line 280 in autoimport.go)
   - Supports the same 64MB line buffer capacity

**Cleaning Pipeline** (`cleaner.go`):

3. **CleanerOptions** (lines 14-26):
   - Controls which cleaning phases are applied
   - `RemoveDuplicates`, `RemoveTestPollution`, `RepairBrokenReferences` can be independently toggled
   - `DefaultCleanerOptions()` enables all three phases

4. **CleanResult** (lines 29-46):
   - Output structure tracking cleaning statistics
   - Records counts for each phase: original, after dedup, after test removal, final
   - Lists removed dependencies for audit trail

5. **CleanIssues()** (lines 59-92) - Entry Point:
   - Applies cleaning phases sequentially: dedup → test removal → reference repair
   - Returns both statistics and cleaned issue list
   - Errors in individual phases are surfaced (currently none since phases don't fail)
   - Called from `importFromGit()` in autoimport.go (line 289)

**Phase 1: Deduplication** (`deduplicateIssues`, lines 101-129):
- Groups issues by ID using a map (line 107-110)
- For duplicate IDs, keeps the newest by `UpdatedAt` timestamp (sorts descending, takes first)
- Returns count of unique issues after dedup and number of duplicates removed
- Preserves insertion order for other issues

**Phase 2: Test Pollution Removal** (`filterTestPollution`, lines 132-181):
- Identifies test/temporary issues using ID-based patterns
- Checks for known pollution prefixes first: `bd-9f86-baseline-`, `bd-da96-baseline-` (lines 144-147)
- Falls back to generic patterns: `-baseline-`, `-test-`, `-tmp-`, `-temp-`, `-scratch-`, `-demo-` (lines 134-141)
- Filters out matching issues, counts removed entries
- Used to eliminate issues from failed quality gate checks that corrupted the JSONL

**Phase 3: Reference Repair** (`repairBrokenReferences`, lines 190-240):
- First builds a set of all valid issue IDs (O(n) preprocessing)
- For each issue's dependencies list, validates each `DependsOnID`
- Removes dependencies with:
  - `deleted:` prefix (deleted parent issues) (lines 212-218)
  - Non-existent target IDs (line 222-228)
- Preserves valid dependencies in their original order
- Returns count and list of removed dependencies with reasons

**Validation Reporting** (`cleaner.go`):

6. **ValidationReport** (lines 243-250):
   - Snapshot of all issues in a dataset
   - Maps duplicate IDs to occurrence counts
   - Maps issue IDs to lists of broken dependency targets
   - Tracks test pollution IDs and invalid issues

7. **ValidateIssues()** (lines 259-332):
   - Comprehensive validation without modification
   - Reports all problems: duplicates, broken refs, test pollution, validation failures
   - Checks each issue structure via `issue.Validate()` (line 323)
   - Used for pre-import diagnostics

8. **Report Output**:
   - `HasIssues()` (line 335-340): Quick check if validation found any problems
   - `Summary()` (lines 343-396): Human-readable text report with symbols (✓, ❌, ⚠️)

### Things to Know

**Why Cleaning Happens During Auto-Import** (GH#590):

The root cause was corrupted JSONL in git history containing:
- Duplicate issue IDs (e.g., `bd-7bbc4e6a` appearing 3 times from merge conflicts)
- Test pollution from failed quality gates (baseline-prefixed issues)
- Broken references to deleted issues (e.g., `deleted:bd-da96-baseline-lint`)

Without cleaning, `importFromGit()` would attempt to insert duplicates, violating the database UNIQUE constraint on ID, causing import to fail and initialization to abort.

**UpdatedAt-Based Deduplication**:

When choosing which duplicate to keep, the implementation sorts by `UpdatedAt` in descending order and keeps the first (newest). This assumes:
- Timestamps are reasonably synchronized across systems
- The latest edit represents the most correct version
- If timestamps are equal or unavailable, order is undefined (acceptable for corrupted data)

**Deleted Issue References**:

Dependencies with `DependsOnID` starting with `deleted:` indicate a parent issue that was permanently removed from git history (not just soft-deleted). These are unconditionally removed because:
- The target issue no longer exists in the repository
- Keeping such dependencies would create dangling references
- This is a data cleanup operation, not a deletion of the depending issue itself

**Test Pollution Patterns**:

The known pollution prefixes (`bd-9f86-baseline-`, `bd-da96-baseline-`) come from a specific failed quality gate check but are hardcoded as an example. Organizations with different test issue naming conventions would need pattern extension (this is acceptable since GH#590 was specific to beads repository).

**64MB Line Buffer**:

Both reader functions allocate a 64MB per-line buffer to handle large issue descriptions or deeply nested dependency lists. This is conservative but necessary because:
- Some issues may have very long descriptions (design docs, code samples)
- Dependencies can accumulate in high-complexity projects
- A single malformed line can't cause the reader to hang

**Phase Independence**:

The three cleaning phases are independent:
- Dedup removes quantity problems (same issue multiple times)
- Test removal removes category problems (non-production issues)
- Reference repair removes structural problems (broken references)

Running all three ensures the dataset is valid for database insertion without creating new problems (e.g., dedup doesn't create broken references).

**Auto-Import Reporting** (lines 295-303 in autoimport.go):

The cleaning results are reported only if significant issues are found (>10 total problems). This threshold prevents noise in typical cases while alerting users to data quality issues. The format reports the total issue reduction and breakdown of problems fixed.

**Integration with Sync Operations**:

Unlike auto-import, manual `bd import` does NOT apply cleaning by default. Users must run `bd import` with explicit flags or clean the JSONL beforehand. This preserves user intent—cleaning is only automatic during fresh initialization when the database is empty.

Created and maintained by Nori.

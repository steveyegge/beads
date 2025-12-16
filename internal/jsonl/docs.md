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

3. **CleanerOptions** (lines 16-29):
    - Controls which cleaning phases are applied
    - `RemoveDuplicates`, `RemoveTestPollution`, `RepairBrokenReferences` can be independently toggled
    - `DefaultCleanerOptions()` enables all three phases

4. **RejectedIssue** (lines 31-35):
    - Tracks a single rejected issue with the specific reason for rejection
    - Used by test pollution and reference repair phases to record why issues were removed
    - Each rejection includes the issue details and a human-readable reason string

5. **DuplicateRemoval** (lines 37-42):
    - Tracks duplicate ID removals during deduplication phase
    - Records the `KeptVersion` (newest by UpdatedAt) and all `RemovedVersions`
    - Enables audit trail showing which issue versions were discarded

6. **CleanResult** (lines 44-67):
    - Output structure tracking cleaning statistics and rejected issues
    - Records counts for each phase: original, after dedup, after test removal, final
    - **New audit trail fields**:
      - `RejectedDuplicates`: list of `DuplicateRemoval` structs tracking all removed duplicate versions
      - `RejectedTestPollution`: list of `RejectedIssue` structs with rejection reasons
      - `RejectedForBrokenRefs`: list of `RejectedIssue` structs for issues that had broken references removed
    - Lists removed dependencies for audit trail

7. **CleanIssues()** (lines 79-120) - Entry Point:
    - Applies cleaning phases sequentially: dedup → test removal → reference repair
    - Returns both statistics and cleaned issue list
    - Collects rejected issues from each phase into the `CleanResult` struct
    - Errors in individual phases are surfaced (currently none since phases don't fail)
    - Called from `importFromGit()` in autoimport.go (line 289)

**Phase 1: Deduplication** (`deduplicateIssues`, lines 129-166):
- Groups issues by ID using a map (line 136-139)
- For duplicate IDs, keeps the newest by `UpdatedAt` timestamp (sorts descending, takes first)
- Returns count of unique issues after dedup and number of duplicates removed
- **New**: Records `DuplicateRemoval` structs with both kept and removed versions for each duplicated ID (lines 154-159)
- Preserves insertion order for other issues

**Phase 2: Test Pollution Removal** (`filterTestPollution`, lines 168-226):
- Identifies test/temporary issues using ID-based patterns
- Checks for known pollution prefixes first: `bd-9f86-baseline-`, `bd-da96-baseline-` (lines 181-200)
- Falls back to generic patterns: `-baseline-`, `-test-`, `-tmp-`, `-temp-`, `-scratch-`, `-demo-` (lines 171-178)
- Filters out matching issues, counts removed entries
- **New**: For each rejected issue, records a `RejectedIssue` with specific reason:
  - `"matches known baseline prefix: <prefix>"` for known pollution prefixes (line 198)
  - `"matches test pattern: <pattern>"` for generic test patterns (line 208)
- Used to eliminate issues from failed quality gate checks that corrupted the JSONL

**Phase 3: Reference Repair** (`repairBrokenReferences`, lines 235-296):
- First builds a set of all valid issue IDs (O(n) preprocessing, lines 238-241)
- For each issue's dependencies list, validates each `DependsOnID`
- Removes dependencies with:
  - `deleted:` prefix (deleted parent issues) (lines 261-267)
  - Non-existent target IDs (lines 270-277)
- Preserves valid dependencies in their original order
- **New**: When any dependencies are removed from an issue, records a `RejectedIssue` with:
  - The original issue (not the removed dependencies—the issue itself)
  - Reason: `"removed <count> broken references: <list of removed deps>"` (lines 284-289)
- Returns count and list of removed dependencies with reasons

**Audit Trail & Rejection Manifest** (`cleaner.go`):

8. **SaveRejectionManifest()** (lines 455-503):
    - Writes all rejected issues to `.beads/cleaning-rejects.jsonl` as JSONL format
    - Called from `importFromGit()` when significant cleaning occurs (>10 problems)
    - Creates one JSON line per rejected issue with three fields:
      - `issue`: the complete issue object
      - `rejection_reason`: human-readable reason for rejection
      - `cleaned_at`: RFC3339 timestamp of when cleaning occurred
    - For duplicates, writes one line per removed version with reason indicating the kept version timestamp
    - For test pollution, writes one line per rejected issue with pattern match reason
    - For broken refs, writes one line per issue that had references removed
    - Used as audit trail to help users review and potentially recover removed issues

9. **marshalIssueWithReason()** (lines 505-519):
    - Helper function that serializes an issue with rejection metadata
    - Creates a wrapper object containing the issue, rejection reason, and cleaning timestamp
    - Returns JSON-encoded string suitable for JSONL line writing
    - Used by SaveRejectionManifest to format each rejection line

**Validation Reporting** (`cleaner.go`):

10. **ValidationReport** (lines 298-306):
    - Snapshot of all issues in a dataset
    - Maps duplicate IDs to occurrence counts
    - Maps issue IDs to lists of broken dependency targets
    - Tracks test pollution IDs and invalid issues

11. **ValidateIssues()** (lines 314-388):
    - Comprehensive validation without modification
    - Reports all problems: duplicates, broken refs, test pollution, validation failures
    - Checks each issue structure via `issue.Validate()` (line 379)
    - Used for pre-import diagnostics

12. **Report Output**:
    - `HasIssues()` (line 391-396): Quick check if validation found any problems
    - `Summary()` (lines 398-453): Human-readable text report with symbols (✓, ❌, ⚠️)

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

**Rejection Manifest for Audit Trail**:

The `SaveRejectionManifest()` function (lines 455-503 in cleaner.go) is called from `importFromGit()` (lines 304-311 in cmd/bd/autoimport.go) when significant cleaning occurs. It writes all rejected issues to `.beads/cleaning-rejects.jsonl` with:
- Complete issue details (serialized as JSON object)
- Specific rejection reason explaining why each issue was removed
- RFC3339 timestamp of when cleaning occurred

The manifest enables users to:
- Review what was discarded during cleaning
- Identify if cleaning was too aggressive (e.g., legitimate issues matching "baseline" pattern)
- Manually recover specific issues if needed
- Understand data quality issues in the JSONL

The manifest is only saved when >10 issues are cleaned (line 298 in autoimport.go) to avoid creating noise for minor fixes. This threshold balances visibility with usability.

**Rejection Reason Specificity**:

Each phase produces rejection reasons that explain the decision:
- **Duplicates**: `"duplicate of <id> (kept version from <timestamp>)"` helps identify which version was kept
- **Test Pollution**: `"matches known baseline prefix: <prefix>"` or `"matches test pattern: <pattern>"` shows the specific pattern that triggered removal
- **Broken References**: `"removed <count> broken references: <list>"` identifies which dependencies were problematic

This specificity enables users to validate the cleaning decisions and adjust patterns if needed.

**Auto-Import Reporting** (lines 295-311 in autoimport.go):

The cleaning results are reported in two ways:
1. Console output only if significant issues are found (>10 total problems), preventing noise while alerting users to data quality issues
2. Rejection manifest saved to `.beads/cleaning-rejects.jsonl` for permanent audit trail

This dual-approach design provides immediate visibility for interactive use while preserving detailed information for later review.

**Integration with Sync Operations**:

Unlike auto-import, manual `bd import` does NOT apply cleaning by default. Users must run `bd import` with explicit flags or clean the JSONL beforehand. This preserves user intent—cleaning is only automatic during fresh initialization when the database is empty.

Created and maintained by Nori.

# GH#590: Clean Database Initialization Plan

## Problem

When running `bd init` on main branch after reset, the auto-import fails with multiple corruption errors:

1. **Duplicate IDs**: `bd-7bbc4e6a` appears 3 times (2 real issues + 1 tombstone)
2. **Broken references**: Issues reference deleted parents (`discovered-from:deleted:bd-da96-baseline-lint`)
3. **Prefix mismatches**: 2 issues with wrong prefix (`bd-9f86-baseline-`, `bd-da96-baseline-`)
4. **Unique constraint violations**: SQLite rejects duplicate ID inserts

## Root Cause

The .beads/issues.jsonl contains corrupted data from:
- Previous failed imports with merge conflicts
- Auto-generated test issues (bd-9f86-baseline, bd-da96-baseline) from quality gate checks
- Unresolved references to deleted parents

## Solution: Clean JSONL During Init

Add validation and cleaning to `bd init` process BEFORE database import:

### Phase 1: JSONL Validation (internal/jsonl/validator.go)

```go
type JSONLValidator struct {
    // Track all seen IDs, detect duplicates
    seenIDs map[string][]int // id -> line numbers
    
    // Track all issues for reference validation
    issues map[string]*Issue
    
    // Errors found
    duplicates []string     // IDs that appear multiple times
    brokenRefs []string     // Issues with non-existent parent refs
    badPrefixes []string    // Issues with wrong prefix
}

func (v *JSONLValidator) ValidateIssue(id string, issue *Issue, lineNum int) error
func (v *JSONLValidator) ResolveBrokenReferences() error
func (v *JSONLValidator) RemoveTestPollution() error
func (v *JSONLValidator) Report() *ValidationReport
```

### Phase 2: Deduplication (remove duplicates, keep newest)

```go
// Remove duplicate IDs, keeping the newest version
func DeduplicateIssues(issues []*Issue) []*Issue {
    // Group by ID
    byID := make(map[string][]*Issue)
    for _, issue := range issues {
        byID[issue.ID] = append(byID[issue.ID], issue)
    }
    
    // Keep only newest per ID
    result := make([]*Issue, 0, len(byID))
    for _, group := range byID {
        sort.Slice(group, func(i, j int) bool {
            return group[i].UpdatedAt.After(group[j].UpdatedAt)
        })
        result = append(result, group[0])
    }
    return result
}
```

### Phase 3: Reference Repair (fix/remove broken references)

```go
func RepairBrokenReferences(issues []*Issue) []*Issue {
    idSet := make(map[string]bool)
    for _, issue := range issues {
        idSet[issue.ID] = true
    }
    
    for _, issue := range issues {
        // Remove deps to non-existent parents
        issue.Dependencies = FilterDeps(issue.Dependencies, func(d *Dependency) bool {
            // Keep if parent exists
            return idSet[d.DependsOnID] || !strings.HasPrefix(d.DependsOnID, "deleted:")
        })
    }
    return issues
}
```

### Phase 4: Test Pollution Removal

```go
func RemoveTestPollution(issues []*Issue) []*Issue {
    // Remove issues with test/baseline prefixes that aren't tracked in git
    filteredPrefixes := []string{
        "bd-9f86-baseline-",
        "bd-da96-baseline-",
        "-baseline-",
        "-test-",
    }
    
    result := make([]*Issue, 0, len(issues))
    for _, issue := range issues {
        isTestPollution := false
        for _, prefix := range filteredPrefixes {
            if strings.HasPrefix(issue.ID, prefix) {
                isTestPollution = true
                break
            }
        }
        if !isTestPollution {
            result = append(result, issue)
        }
    }
    return result
}
```

### Phase 5: Integration into bd init

Modify `cmd/bd/init.go` to use cleaning pipeline:

```go
// In initCmd.Run():

// 1. Read raw JSONL (before database creation)
rawIssues, err := ReadJSONLFile(".beads/issues.jsonl")

// 2. Validate and clean
validator := jsonl.NewValidator()
validator.ValidateAll(rawIssues)
validator.Report().Print(verbose)

// 3. Apply cleaning pipeline
cleaned := rawIssues
cleaned = jsonl.DeduplicateIssues(cleaned)
cleaned = jsonl.RemoveTestPollution(cleaned)
cleaned = jsonl.RepairBrokenReferences(cleaned)

// 4. Report what was cleaned
if verbose {
    fmt.Printf("Cleaning results:\n")
    fmt.Printf("  - Removed %d duplicates\n", len(rawIssues)-len(cleaned))
    fmt.Printf("  - Removed %d test issues\n", testCount)
    fmt.Printf("  - Repaired %d broken references\n", refCount)
}

// 5. Create database and import cleaned issues
db, err := sqlite.New(ctx, dbPath)
for _, issue := range cleaned {
    db.CreateIssue(ctx, issue)
}
```

## Expected Outcome

After running `bd init --prefix bd` on gh-590 branch:

1. ✓ No duplicate ID constraint violations
2. ✓ All dependencies point to existing issues
3. ✓ No test pollution (baseline issues removed)
4. ✓ Database has ~750-900 real issues (not 10,500 corrupted ones)
5. ✓ `bd doctor` reports "healthy" status

## Files to Create/Modify

### New Files
- `internal/jsonl/validator.go` - JSONL validation logic
- `internal/jsonl/cleaner.go` - Deduplication and cleanup
- `tests/jsonl/validator_test.go` - Comprehensive tests

### Modified Files
- `cmd/bd/init.go` - Integrate cleaning pipeline before import
- `cmd/bd/doctor.go` - Add pre-import validation warning

## Testing

1. **Duplicate detection**: Multiple same ID in JSONL
2. **Broken reference removal**: Issues with non-existent parent refs
3. **Test pollution removal**: Baseline-prefixed issues removed
4. **Tombstone handling**: Duplicate tombstones removed keeping newest
5. **End-to-end**: Fresh init on main produces clean 750-issue database

## Success Criteria

- [ ] `bd init --prefix bd` completes without auto-import errors
- [ ] `bd stats` shows correct issue count (750-900, not 10,500)
- [ ] `bd doctor` reports no DB-JSONL sync issues
- [ ] `bd ready` works without stale database warnings
- [ ] Validation is logged in verbose mode (--verbose flag)
- [ ] Cleaning is optional - `--no-clean` flag skips it (for recovery scenarios)

## Rollout

1. Implement validation + deduplication (Phase 1-2)
2. Test on gh-590 branch with main JSONL
3. Verify produces clean database
4. Add to gh-590-init-reset-clean branch
5. Update GH#590 with fix verification

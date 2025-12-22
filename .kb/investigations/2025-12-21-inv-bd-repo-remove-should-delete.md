<!--
D.E.K.N. Summary - 30-second handoff for fresh Claude
Fill this at the END of your investigation, before marking Complete.
-->

## Summary (D.E.K.N.)

**Delta:** `bd repo remove` now deletes hydrated issues from the removed repo as expected.

**Evidence:** Tests pass for `DeleteIssuesBySourceRepo` and `ClearRepoMtime` methods; build succeeds.

**Knowledge:** Issues have a `source_repo` field tracking their origin; deleting by this field removes all related data (deps, labels, comments, events).

**Next:** Close issue - implementation complete with tests.

**Confidence:** High (90%) - unit tests pass; integration testing with real multi-repo workflow not yet done.

---

# Investigation: bd repo remove should delete hydrated issues

**Question:** How should `bd repo remove` clean up hydrated issues from the database?

**Started:** 2025-12-21
**Updated:** 2025-12-21
**Owner:** feature-impl agent
**Phase:** Complete
**Next Step:** None
**Status:** Complete
**Confidence:** High (90%)

---

## Findings

### Finding 1: Issues have source_repo field

**Evidence:** The `types.Issue` struct has a `SourceRepo` field that tracks which repo the issue came from. This is set during multi-repo hydration and used for export grouping.

**Source:** `internal/types/types.go:33`, `internal/storage/sqlite/multirepo.go:178-179`

**Significance:** We can use this field to identify and delete issues from a specific repo.

### Finding 2: repoRemoveCmd only updates config

**Evidence:** The existing `repoRemoveCmd` in `cmd/bd/repo.go` only calls `config.RemoveRepo()` to update the YAML config - it doesn't interact with the database at all.

**Source:** `cmd/bd/repo.go:92-125`

**Significance:** This is the bug - config is updated but issues remain orphaned in the database.

### Finding 3: Related data must be cleaned up

**Evidence:** Issues have related data in multiple tables: dependencies, labels, comments, events, dirty_issues. The existing `DeleteIssue` method shows the pattern for cleaning these up.

**Source:** `internal/storage/sqlite/queries.go:1081-1133`

**Significance:** The new `DeleteIssuesBySourceRepo` must clean up all related data to avoid FK violations and orphaned records.

---

## Synthesis

**Key Insights:**

1. **source_repo enables selective deletion** - The existing field provides exactly what we need to identify issues from a removed repo.

2. **Batch deletion is more efficient** - Rather than calling `DeleteIssue` in a loop, we can batch delete related data for all affected issues.

3. **Mtime cache should also be cleared** - The `repo_mtimes` table caches file modification times to skip unchanged imports; this should be cleared when a repo is removed.

**Answer to Investigation Question:**

The fix is straightforward: add `DeleteIssuesBySourceRepo` and `ClearRepoMtime` methods to SQLiteStorage, then call them from `repoRemoveCmd` before updating the config. The implementation deletes all issues matching the source_repo, along with their dependencies, labels, comments, events, and dirty markers.

---

## Confidence Assessment

**Current Confidence:** High (90%)

**Why this level?**

Strong evidence from code analysis and passing unit tests. Minor uncertainty from lack of integration testing with a real multi-repo setup.

**What's certain:**

- ✅ Issues are stored with source_repo field correctly
- ✅ DeleteIssuesBySourceRepo properly removes issues and related data (tested)
- ✅ ClearRepoMtime removes the mtime cache entry (tested)
- ✅ repoRemoveCmd now calls both methods before updating config

**What's uncertain:**

- ⚠️ Edge cases with tilde expansion in paths (e.g., ~/foo vs /home/user/foo)
- ⚠️ Behavior when daemon is running (should be fine since we call ensureDirectMode)

**What would increase confidence to Very High:**

- Integration test with actual multi-repo setup
- Test edge cases with different path formats

---

## Implementation Summary

**Changes made:**

1. **Added `DeleteIssuesBySourceRepo` method** (`internal/storage/sqlite/multirepo.go`)
   - Queries issues by source_repo
   - Deletes related data (dependencies, events, comments, labels, dirty markers)
   - Deletes the issues themselves
   - Returns count of deleted issues

2. **Added `ClearRepoMtime` method** (`internal/storage/sqlite/multirepo.go`)
   - Expands tilde and converts to absolute path
   - Deletes the mtime cache entry

3. **Updated `repoRemoveCmd`** (`cmd/bd/repo.go`)
   - Calls `ensureDirectMode` for database access
   - Calls `DeleteIssuesBySourceRepo` to remove issues
   - Calls `ClearRepoMtime` to clear cache
   - Then updates config via `config.RemoveRepo`
   - Reports deleted count in output

4. **Added tests** (`internal/storage/sqlite/multirepo_test.go`)
   - Tests for deleting all issues from a repo
   - Tests for handling non-existent repos
   - Tests for cleaning up related data
   - Tests for ClearRepoMtime

---

## References

**Files Modified:**
- `cmd/bd/repo.go` - Added database cleanup to repoRemoveCmd
- `internal/storage/sqlite/multirepo.go` - Added DeleteIssuesBySourceRepo and ClearRepoMtime
- `internal/storage/sqlite/multirepo_test.go` - Added tests for new methods

**Commands Run:**
```bash
# Build check
go build ./cmd/bd/

# Unit tests
go test -v ./internal/storage/sqlite/... -run "TestDeleteIssuesBySourceRepo|TestClearRepoMtime"

# Full test suite
go test ./... -count=1
```

---

## Investigation History

**2025-12-21 22:02:** Investigation started
- Initial question: How to make `bd repo remove` delete hydrated issues?
- Context: Issue bd-5ww2 reports orphaned issues after repo removal

**2025-12-21 22:15:** Implementation complete
- Added DeleteIssuesBySourceRepo and ClearRepoMtime methods
- Updated repoRemoveCmd to call them
- Added tests

**2025-12-21 22:25:** Investigation completed
- Final confidence: High (90%)
- Status: Complete
- Key outcome: bd repo remove now cleans up issues from removed repos

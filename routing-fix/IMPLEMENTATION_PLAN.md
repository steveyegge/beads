# Routing Fix Implementation Plan

## Executive Summary

Investigation of the beads routing functionality revealed:
- **Core routing is WORKING CORRECTLY** - no functional bugs
- **Documentation is SEVERELY BROKEN** - describes non-existent feature
- **Test coverage has GAPS** - error paths untested
- **Error handling is SILENT** - failures hard to debug

**Last Updated**: 2026-01-27 (TASK-003 completed)
**Validation Status**: All specs reviewed against implementation - CONFIRMED

### Independent Verification Summary
- ✓ routes.go:47-48 silent failure FIXED (BD_DEBUG_ROUTING logging added)
- ✓ routing.md confirmed wrong (pattern/target/priority vs actual prefix/path)
- ✓ BD_DEBUG_ROUTING implemented in 7 functions (LoadRoutes added in TASK-002)
- ✓ No dedicated tests for LoadRoutes, ResolveBeadsDirForRig, ResolveBeadsDirForID error paths
- ✓ createInRig at create.go:978-1108 handles all flags correctly

---

## Task Status Overview

| Task | Description | Status | Priority |
|------|-------------|--------|----------|
| TASK-001 | Fix routing documentation | completed | P0 |
| TASK-002 | Add BD_DEBUG_ROUTING to LoadRoutes | completed | P0 |
| TASK-003 | Add unit tests for LoadRoutes error paths | completed | P1 |
| TASK-004 | Add unit tests for ResolveBeadsDirForRig | pending | P1 |
| TASK-005 | Add unit tests for ResolveBeadsDirForID | pending | P1 |
| TASK-006 | Add documentation comments for edge cases | pending | P2 |
| TASK-007 | Add warning for malformed routes.jsonl | pending | P2 |
| TASK-000 | Core routing implementation | completed | - |

---

## Priority Tasks

### P0: Critical (Must Fix)

#### TASK-001: Fix routing documentation
**Status**: completed
**File**: `website/docs/multi-agent/routing.md`
**Spec**: `routing-fix/specs/02-documentation-mismatch.md`
**Completed**: 2026-01-27

**Problem**: Documentation describes pattern-based routing with priority fields that doesn't exist.

**Solution Implemented**:
- Completely rewrote documentation to describe actual prefix-based routing
- Removed all references to non-existent commands (bd routes list/add/remove/test)
- Added correct routes.jsonl format with prefix/path fields
- Documented Gas Town multi-rig setup with directory structure example
- Added --rig flag usage documentation
- Documented symlinked .beads directory handling
- Added redirect file documentation
- Added troubleshooting section with BD_DEBUG_ROUTING usage
- Documented manual configuration workflow

**Acceptance Criteria**:
- [x] Documentation accurately describes prefix-based routing
- [x] Remove all references to non-existent commands (lines 53-67)
- [x] Add examples of actual routes.jsonl format
- [x] Document manual configuration workflow
- [x] Add section on Gas Town multi-rig setup
- [x] Document BD_DEBUG_ROUTING for troubleshooting

---

#### TASK-002: Add BD_DEBUG_ROUTING to LoadRoutes
**Status**: completed
**File**: `internal/routing/routes.go:27-56`
**Spec**: `routing-fix/specs/03-error-handling.md`
**Completed**: 2026-01-27

**Problem**: LoadRoutes() silently skips malformed JSON lines with no indication of failure.

**Solution Implemented**:
- Added `debugRouting` flag at function start to check `BD_DEBUG_ROUTING` env var
- Log file path being loaded at function start
- Log when file does not exist (not an error)
- Log when file fails to open with error
- Track `lineNum` and `skippedLines` counters
- Log malformed JSON lines with line number and parse error
- Log lines with empty prefix or path
- Log summary of parsed routes and skipped lines at function end

**Acceptance Criteria**:
- [x] `BD_DEBUG_ROUTING=1 bd create` shows routes loading details
- [x] Shows file path being loaded
- [x] Shows count of lines parsed/skipped
- [x] Shows specific parse errors per line

---

### P1: Important (Should Fix)

#### TASK-003: Add unit tests for LoadRoutes error paths
**Status**: completed
**File**: `internal/routing/routing_test.go`
**Spec**: `routing-fix/specs/04-test-coverage.md`
**Completed**: 2026-01-27

**Tests Implemented**:
1. `TestLoadRoutes_MalformedJSON` - malformed JSON lines are skipped, valid routes loaded
2. `TestLoadRoutes_EmptyPrefix` - routes with empty prefix are skipped
3. `TestLoadRoutes_EmptyPath` - routes with empty path are skipped
4. `TestLoadRoutes_EmptyFile` - empty file returns nil slice, no error
5. `TestLoadRoutes_CommentsOnly` - comments and blank lines return nil slice
6. `TestLoadRoutes_FileNotExist` - non-existent file returns nil, nil
7. `TestLoadRoutes_MixedContent` - comprehensive test with all edge cases combined

**Acceptance Criteria**:
- [x] Test malformed JSON handling
- [x] Test empty prefix/path field handling
- [x] Test empty file handling
- [x] Test comment line handling

---

#### TASK-004: Add unit tests for ResolveBeadsDirForRig
**Status**: pending
**File**: `internal/routing/routing_test.go`
**Spec**: `routing-fix/specs/04-test-coverage.md`

**Currently Untested Scenarios** (verified - no dedicated tests exist):
1. Non-existent rig name → should return error
2. Rig with non-existent target directory → should return error
3. All three input formats: "gt", "gt-", "gastown" → should all work
4. Route with path="." (town-level beads) → should resolve to town root
5. Redirect file handling → should follow redirect

**Acceptance Criteria**:
- [ ] Test all three input formats (prefix, prefix-, rigname)
- [ ] Test non-existent rig returns error
- [ ] Test non-existent target directory returns error
- [ ] Test path="." handling

---

#### TASK-005: Add unit tests for ResolveBeadsDirForID
**Status**: pending
**File**: `internal/routing/routing_test.go`
**Spec**: `routing-fix/specs/04-test-coverage.md`

**Currently Untested Scenarios** (verified - no dedicated tests exist):
1. ID with unknown prefix → should return local, routed=false
2. ID routing to non-existent directory → should return local
3. ID with no prefix → should return local
4. ID routing to existing directory → should route successfully

**Acceptance Criteria**:
- [ ] Test unknown prefix handling
- [ ] Test no-prefix handling
- [ ] Test successful routing
- [ ] Test non-existent target directory

---

### P2: Nice to Have (Could Fix)

#### TASK-006: Add documentation comments for edge cases
**Status**: pending
**Files**: `internal/routing/routes.go`
**Spec**: `routing-fix/specs/05-code-clarity.md`

**Required Comments** (verified in code):
1. **ExtractProjectFromPath** (lines 80-90): Document that "." path returns "." not empty string
2. **AutoDetectTargetRig** (lines 455-458): Already has comment explaining prefix return ✓
3. **findTownRootFromCWD** (lines 304-324): Document CWD dependency for symlink handling

**Acceptance Criteria**:
- [ ] ExtractProjectFromPath documents "." edge case
- [ ] findTownRootFromCWD documents symlink handling rationale
- [ ] Gas Town terminology explained in package doc

---

#### TASK-007: Add warning for malformed routes.jsonl
**Status**: pending
**File**: `internal/routing/routes.go:47-48`
**Spec**: `routing-fix/specs/03-error-handling.md`

**Rationale**: Silent failures are bad UX. A typo in routes.jsonl causes routes to silently disappear. Users may not know to enable BD_DEBUG_ROUTING.

**Trade-off**: Warning every time could be noisy. Consider:
- Warn only on first invocation per session
- Warn only if routes.jsonl exists but has 0 valid routes
- Warn in verbose mode only (--verbose flag)

**Acceptance Criteria**:
- [ ] User sees warning if routes.jsonl has parse issues
- [ ] Warning is not excessively noisy
- [ ] Can be disabled via config or env var

---

## Completed Tasks

#### TASK-000: Core routing implementation
**Status**: completed (already working)
**Evidence** (verified 2026-01-27):
- All TestAutoDetectTargetRig tests pass (6 scenarios) - routing_test.go:418-598
- TestFindTownRoutes_SymlinkedBeadsDir passes - routing_test.go:315-416
- BD_DEBUG_ROUTING logging implemented in:
  - ResolveBeadsDirForRig (routes.go:194-196)
  - ResolveBeadsDirForID (routes.go:270-276)
  - findTownRoutes (routes.go:346-354, 371-373)
  - AutoDetectTargetRig (routes.go:390-392, 403-405, 409-411, 460-463)
  - findTownRootFromCWD (routes.go:311-322)
  - resolveRedirect (routes.go:474-506)
- createInRig implemented with full flag support (create.go:978-1108)

**Functions Verified Working**:
| Function | Location | Status |
|----------|----------|--------|
| `AutoDetectTargetRig()` | routes.go:378-466 | ✓ |
| `ResolveBeadsDirForRig()` | routes.go:154-199 | ✓ |
| `ResolveBeadsDirForID()` | routes.go:232-284 | ✓ |
| `findTownRoutes()` | routes.go:326-376 | ✓ |
| `createInRig()` | create.go:978-1108 | ✓ |

---

## Dependencies

```
TASK-001 (docs) - standalone, no code dependencies
TASK-002 (debug logging) - standalone
TASK-003 (LoadRoutes tests) - blocked by TASK-002 (need debug output to verify)
TASK-004 (ResolveBeadsDirForRig tests) - standalone
TASK-005 (ResolveBeadsDirForID tests) - standalone
TASK-006 (comments) - standalone
TASK-007 (warnings) - blocked by TASK-002 (use same pattern)
```

**Recommended Execution Order**:
1. TASK-001 (docs) - can do in parallel with code changes
2. TASK-002 (debug logging) - unblocks TASK-003, TASK-007
3. TASK-004, TASK-005 (tests) - independent, can parallelize
4. TASK-003 (LoadRoutes tests) - after TASK-002
5. TASK-006, TASK-007 (polish)

---

## Files to Modify

| File | Tasks | Risk | Notes |
|------|-------|------|-------|
| `website/docs/multi-agent/routing.md` | TASK-001 | LOW | Docs only, complete rewrite needed |
| `internal/routing/routes.go` | TASK-002, TASK-006, TASK-007 | MEDIUM | Core routing, add logging only |
| `internal/routing/routing_test.go` | TASK-003, TASK-004, TASK-005 | LOW | Tests only, follow existing patterns |

---

## Validation Commands

```bash
# Run routing tests
go test ./internal/routing/... -v

# Run specific test
go test ./internal/routing/... -v -run TestAutoDetectTargetRig

# Run with debug logging
BD_DEBUG_ROUTING=1 bd create "test" --dry-run

# Test cross-rig creation
BD_DEBUG_ROUTING=1 bd create "test" --rig gastown --dry-run

# Full test suite
go test ./...

# Lint
golangci-lint run ./internal/routing/...
```

---

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Breaking existing routing | All existing tests pass; only adding logging/tests |
| Documentation misleads users | TASK-001 is P0 for this reason |
| Debug logging too verbose | Use BD_DEBUG_ROUTING env var, off by default |
| Test flakiness with CWD | Use t.TempDir() and t.Chdir() per AGENTS.md |

---

## Implementation Notes

### Key Code Locations (verified)

**LoadRoutes silent failure** (routes.go:47-48):
```go
if err := json.Unmarshal([]byte(line), &route); err != nil {
    continue // ← THIS IS THE GAP - no logging
}
```

**Existing debug logging pattern** (routes.go:194-196):
```go
if os.Getenv("BD_DEBUG_ROUTING") != "" {
    fmt.Fprintf(os.Stderr, "[routing] Rig %q -> prefix %s, path %s (townRoot=%s)\n", ...)
}
```

**Documentation gap** (routing.md:20-27):
```jsonl
{"pattern": "frontend/**", "target": "frontend-repo", "priority": 10}  ← WRONG
```
Should be:
```jsonl
{"prefix": "gt-", "path": "gastown/mayor/rig"}  ← CORRECT
```

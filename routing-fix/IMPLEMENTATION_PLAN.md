# Routing Fix Implementation Plan

## Executive Summary

Investigation of the beads routing functionality revealed:
- **Core routing is WORKING CORRECTLY** - no functional bugs
- **Documentation is SEVERELY BROKEN** - describes non-existent feature
- **Test coverage has GAPS** - error paths untested
- **Error handling is SILENT** - failures hard to debug

**Last Updated**: 2026-01-27 (TASK-008 completed - ALL TASKS COMPLETE)
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
| TASK-004 | Add unit tests for ResolveBeadsDirForRig | completed | P1 |
| TASK-005 | Add unit tests for ResolveBeadsDirForID | completed | P1 |
| TASK-006 | Add documentation comments for edge cases | completed | P2 |
| TASK-007 | Add warning for malformed routes.jsonl | completed | P2 |
| TASK-008 | Integrate auto-routing in create command | completed | P0 |
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

#### TASK-008: Integrate auto-routing in create command
**Status**: completed
**File**: `cmd/bd/create.go:285-340`
**Completed**: 2026-01-27

**Problem**: The `bd create` command doesn't automatically route to the correct rig based on the configured prefix. Users must manually specify `--rig` even when the database has a configured issue-prefix that maps to a route.

**Solution Implemented**:
- Added auto-routing logic in create command before explicit --rig handling
- Detects configured prefix from database config (issue-prefix or issue_prefix keys)
- Falls back to config.yaml if not in database
- Calls `routing.AutoDetectTargetRig()` to determine if routing is needed
- If routing is needed, automatically calls `createInRig()` to create bead in target rig
- Logs routing decision when BD_DEBUG_ROUTING is enabled

**Acceptance Criteria**:
- [x] Create command auto-routes based on configured prefix
- [x] Works with both daemon and direct mode
- [x] Logs routing decisions with BD_DEBUG_ROUTING
- [x] Falls back gracefully if no prefix configured or no matching route

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
**Status**: completed
**File**: `internal/routing/routing_test.go`
**Spec**: `routing-fix/specs/04-test-coverage.md`
**Completed**: 2026-01-27

**Tests Implemented**:
1. `TestResolveBeadsDirForRig_NonExistentRig` - non-existent rig name returns error
2. `TestResolveBeadsDirForRig_NonExistentTargetDir` - rig pointing to non-existent directory returns error
3. `TestResolveBeadsDirForRig_AllInputFormats` - all three formats work (prefix, prefix-, rigname)
4. `TestResolveBeadsDirForRig_DotPath` - path="." resolves to town root beads directory
5. `TestResolveBeadsDirForRig_Redirect` - redirect files are followed correctly

**Acceptance Criteria**:
- [x] Test all three input formats (prefix, prefix-, rigname)
- [x] Test non-existent rig returns error
- [x] Test non-existent target directory returns error
- [x] Test path="." handling

---

#### TASK-005: Add unit tests for ResolveBeadsDirForID
**Status**: completed
**File**: `internal/routing/routing_test.go`
**Spec**: `routing-fix/specs/04-test-coverage.md`
**Completed**: 2026-01-27

**Tests Implemented**:
1. `TestResolveBeadsDirForID_UnknownPrefix` - ID with unknown prefix returns local, routed=false
2. `TestResolveBeadsDirForID_NoPrefix` - ID without any prefix returns local, routed=false
3. `TestResolveBeadsDirForID_SuccessfulRouting` - ID with known prefix routes to target directory
4. `TestResolveBeadsDirForID_NonExistentTargetDir` - ID matching route but non-existent target falls back to local
5. `TestResolveBeadsDirForID_DotPath` - path="." correctly resolves to town root beads directory
6. `TestResolveBeadsDirForID_NoRoutes` - no routes.jsonl returns local, routed=false

**Acceptance Criteria**:
- [x] Test unknown prefix handling
- [x] Test no-prefix handling
- [x] Test successful routing
- [x] Test non-existent target directory

---

### P2: Nice to Have (Could Fix)

#### TASK-006: Add documentation comments for edge cases
**Status**: completed
**Files**: `internal/routing/routes.go`
**Spec**: `routing-fix/specs/05-code-clarity.md`
**Completed**: 2026-01-27

**Documentation Added**:
1. **Package-level documentation**: Added comprehensive package doc explaining Gas Town architecture,
   multi-repository setup terminology, routes.jsonl format, and symlink handling overview
2. **ExtractProjectFromPath**: Documented that "." path returns "." not empty string, with
   explanation of why this matters for town-root-as-rig configurations
3. **findTownRootFromCWD**: Enhanced documentation with concrete example showing why CWD-based
   lookup is required when .beads is symlinked (e.g., ~/gt/.beads -> ~/gt/olympus/.beads)

**Acceptance Criteria**:
- [x] ExtractProjectFromPath documents "." edge case
- [x] findTownRootFromCWD documents symlink handling rationale
- [x] Gas Town terminology explained in package doc

---

#### TASK-007: Add warning for malformed routes.jsonl
**Status**: completed
**File**: `internal/routing/routes.go`
**Spec**: `routing-fix/specs/03-error-handling.md`
**Completed**: 2026-01-27

**Rationale**: Silent failures are bad UX. A typo in routes.jsonl causes routes to silently disappear. Users may not know to enable BD_DEBUG_ROUTING.

**Solution Implemented**:
- Added warning at end of LoadRoutes() that triggers only when:
  - File exists and was opened successfully
  - skippedLines > 0 (malformed lines were encountered)
  - len(routes) == 0 (no valid routes loaded)
  - BD_QUIET_ROUTING env var is not set
- Warning includes helpful hints pointing to BD_DEBUG_ROUTING and BD_QUIET_ROUTING
- Added unit tests for warning behavior (4 test cases)
- Updated routing.md documentation with warning section

**Acceptance Criteria**:
- [x] User sees warning if routes.jsonl has parse issues
- [x] Warning is not excessively noisy (only when completely broken config)
- [x] Can be disabled via env var (BD_QUIET_ROUTING=1)

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

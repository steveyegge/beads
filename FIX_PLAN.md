# Fix Plan: Auto-Route Create with Explicit ID

## Problem Statement

When `bd create --id=pq-xxx` is called from a directory with a different prefix (e.g., `hq-`), the system fails with "prefix mismatch" instead of automatically routing to the correct database based on `routes.jsonl`.

## Solution Approach

Add automatic routing logic when `--id` is specified, before the prefix validation step. Extract the prefix from the ID, look it up in routes.jsonl, and route to the correct database if found.

## Implementation Steps

### Step 1: Add helper function to detect if routing is needed

**Location**: `cmd/bd/create.go` (add near top of file or before `Run` function)

**Function**:
```go
// shouldAutoRouteFromID checks if the explicit ID requires routing to a different database.
// Returns (rigOrPrefix, shouldRoute) where:
// - rigOrPrefix is the rig name or prefix to route to
// - shouldRoute is true if routing is needed
func shouldAutoRouteFromID(explicitID string, currentBeadsDir string) (string, bool) {
    if explicitID == "" {
        return "", false
    }

    // Extract prefix from ID
    prefix := routing.ExtractPrefix(explicitID)
    if prefix == "" {
        return "", false
    }

    // Load routes from town level
    routes, err := routing.LoadTownRoutes(currentBeadsDir)
    if err != nil || len(routes) == 0 {
        return "", false
    }

    // Check if this prefix matches a route
    for _, route := range routes {
        if route.Prefix == prefix {
            // Found matching route
            // Check if it's a different path than current (path "." means current)
            if route.Path != "" && route.Path != "." {
                return route.Path, true
            }
        }
    }

    return "", false
}
```

### Step 2: Call auto-routing logic in create command

**Location**: `cmd/bd/create.go` in the `Run` function

**Insert point**: After parsing `explicitID` flag, before the `--dry-run` check, and before the `--rig`/`--prefix` check (around line 180-200)

**Code to add**:
```go
// Auto-route based on explicit ID prefix
if explicitID != "" && rigOverride == "" && prefixOverride == "" {
    rigName, shouldRoute := shouldAutoRouteFromID(explicitID, beadsDir)
    if shouldRoute {
        // Route to the target rig automatically
        createInRig(cmd, rigName, title, description, issueType, priority,
                    design, acceptance, notes, assignee, labels, externalRef, wisp)
        return
    }
}
```

**Important**: This must be placed:
- **After** `explicitID` is parsed from flags
- **Before** `--dry-run` handling (so dry-run shows the correct routing)
- **Before** explicit `--rig`/`--prefix` handling (which has priority over auto-routing)
- **Before** prefix validation (which currently causes the error)

### Step 3: Update `createInRig` function signature if needed

**Location**: `cmd/bd/create.go` - `createInRig` function (around line 780)

**Current signature**:
```go
func createInRig(cmd *cobra.Command, rigName, title, description, issueType string,
                 priority int, design, acceptance, notes, assignee string,
                 labels []string, externalRef string, wisp bool)
```

**Check**: Verify this signature has all the parameters we need. If any are missing (like `explicitID`, `deps`, `molType`, etc.), update the signature.

**Likely needed additions**:
- `explicitID string` - to pass the explicit ID
- `deps []string` - for dependencies
- `parentID string` - for hierarchical issues
- `molType types.MolType` - for molecule type
- Other flags as needed

### Step 4: Pass `explicitID` to `createInRig`

**Location**: Both the auto-routing call (Step 2) and the explicit `--rig`/`--prefix` call

**Update calls to `createInRig`**:
```go
// Auto-routing call (Step 2)
createInRig(cmd, rigName, explicitID, title, description, issueType, priority, ...)

// Explicit --rig/--prefix call (existing, around line 275)
createInRig(cmd, targetRig, explicitID, title, description, issueType, priority, ...)
```

### Step 5: Update `createInRig` implementation

**Location**: `cmd/bd/create.go` - inside `createInRig` function body

**Changes needed**:
1. Add `explicitID` parameter to function signature
2. Use `explicitID` when creating the issue (currently not passed through)
3. Verify prefix matching logic works correctly with the routed database

**Key section** (around line 820-840):
```go
// Prepare prefix override from routes.jsonl for cross-rig creation
// Strip trailing hyphen - database stores prefix without it (e.g., "aops" not "aops-")
var prefixOverride string
if targetPrefix != "" {
    prefixOverride = strings.TrimSuffix(targetPrefix, "-")
}

// Pass explicitID to Create call
issue := &types.Issue{
    ID:          explicitID,  // ADD THIS LINE
    Title:       title,
    Description: description,
    // ... rest of fields
}
```

### Step 6: Add tests

**Location**: Create new test file `cmd/bd/create_autoroute_test.go`

**Tests to add**:
1. **TestAutoRouteFromID** - Unit test for `shouldAutoRouteFromID` helper
2. **TestCreateWithExplicitIDAutoRoutes** - Integration test verifying the full flow
3. **TestCreateWithExplicitIDNoRoute** - Verify no routing when prefix matches current DB
4. **TestExplicitRigOverridesAutoRoute** - Verify `--rig` takes priority over auto-routing

**Test structure**:
```go
func TestAutoRouteFromID(t *testing.T) {
    tests := []struct {
        name           string
        explicitID     string
        routes         []routing.Route
        wantRig        string
        wantShouldRoute bool
    }{
        {
            name:           "routes pq- prefix to pgqueue",
            explicitID:     "pq-test-123",
            routes:         []routing.Route{{Prefix: "pq-", Path: "pgqueue"}},
            wantRig:        "pgqueue",
            wantShouldRoute: true,
        },
        {
            name:           "no routing for current prefix",
            explicitID:     "hq-test-123",
            routes:         []routing.Route{{Prefix: "hq-", Path: "."}},
            wantRig:        "",
            wantShouldRoute: false,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup temp directory with routes.jsonl
            // Call shouldAutoRouteFromID
            // Verify results
        })
    }
}
```

### Step 7: Update documentation

**Files to update**:
1. **docs/commands/create.md** (if exists) - Document auto-routing behavior
2. **CHANGELOG.md** - Add entry:
   ```markdown
   ### Fixed
   - `bd create` now automatically routes to the correct database when `--id` prefix matches a route in `routes.jsonl`. Previously failed with "prefix mismatch" error. (#1188)
   ```

## Testing Strategy

### Manual Testing

1. **Test in Gas Town environment**:
   ```bash
   cd ~/gt  # Root with hq- prefix
   bd create --id=pq-test-123 --title="Test auto-routing"
   # Should create in pgqueue/.beads/beads.db
   ```

2. **Test with explicit --rig (should still work)**:
   ```bash
   bd create --id=pq-test-456 --title="Test explicit rig" --rig=pgqueue
   # Should create in pgqueue/.beads/beads.db
   ```

3. **Test without routes.jsonl** (standalone repo):
   ```bash
   cd /tmp/standalone-repo
   bd create --id=test-123 --title="Test no routing"
   # Should create in local .beads/beads.db (no routing)
   ```

4. **Test gt doctor --fix**:
   ```bash
   gt doctor --fix
   # Should successfully create missing agent beads
   ```

### Automated Testing

Run existing tests plus new tests:
```bash
go test ./cmd/bd/... -run TestCreate
go test ./internal/routing/... -run TestAutoRoute
```

## Edge Cases to Handle

1. **Empty explicitID** - No routing (already handled by guard clause)
2. **No routes.jsonl** - No routing (LoadTownRoutes returns nil)
3. **Prefix not in routes** - No routing (loop finds no match)
4. **Route path is "."** - No routing (indicates current directory)
5. **Both --id and --rig specified** - Explicit --rig takes priority (handled by order of checks)
6. **Invalid prefix in ID** - No routing (ExtractPrefix returns "")

## Rollback Plan

If the fix causes issues:
1. Revert the commit
2. Use workaround: explicit `--rig` or `--prefix` flags
3. Update `gt doctor --fix` to use `--rig` flag when creating agent beads

## Success Criteria

1. ✅ `bd create --id=pq-xxx` from root directory successfully creates in pgqueue database
2. ✅ `gt doctor --fix` successfully creates agent beads without errors
3. ✅ All existing tests pass
4. ✅ New tests demonstrate the fix works
5. ✅ Manual testing in Gas Town environment succeeds
6. ✅ No regressions in single-repo scenarios

## Timeline Estimate

- **Step 1-2**: Add auto-routing logic - 30 minutes
- **Step 3-5**: Update createInRig - 45 minutes
- **Step 6**: Add tests - 60 minutes
- **Step 7**: Documentation - 15 minutes
- **Testing**: Manual + automated - 30 minutes

**Total**: ~3 hours

## Code Locations Quick Reference

- `cmd/bd/create.go` - Main create command (needs modification)
- `cmd/bd/create.go:780` - `createInRig` function (reference implementation)
- `internal/routing/routes.go:232` - `ResolveBeadsDirForID` (existing routing logic)
- `internal/routing/routes.go:68` - `ExtractPrefix` (helper function)
- `internal/routing/routes.go:58` - `LoadTownRoutes` (load routes.jsonl)
- `internal/validation/bead.go:91` - `ValidatePrefixWithAllowed` (current validation causing error)

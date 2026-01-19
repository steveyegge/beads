# Bug: Multi-Repo Routing with Explicit ID Fails

## Summary

When creating an issue with `--id` flag where the ID's prefix doesn't match the current database prefix, the system fails with a "prefix mismatch" error instead of routing to the correct database based on `routes.jsonl`.

## Reproduction

### Setup

In a Gas Town environment with multiple rigs:

```
~/gt/
  .beads/
    routes.jsonl  # Maps: {"prefix":"hq-","path":"."}, {"prefix":"pq-","path":"pgqueue"}
    beads.db      # Database with prefix "hq"
  pgqueue/
    .beads/
      beads.db    # Database with prefix "pq"
```

### Steps to Reproduce

```bash
cd ~/gt  # In root directory with hq- prefix database
bd create --id=pq-pgqueue-crew-pgq_crew --title="Test" --type=task
```

### Expected Behavior

1. System extracts prefix "pq-" from the ID
2. Looks up "pq-" in routes.jsonl
3. Routes to `pgqueue/.beads/beads.db`
4. Creates issue in pgqueue database
5. Success

### Actual Behavior

```
Error: prefix mismatch: database uses 'hq' but you specified 'pq' (use --force to override)
```

The system validates the ID against the current database's prefix (hq-) instead of routing to the correct database (pq-) based on routes.jsonl.

## Root Cause

In `cmd/bd/create.go`, when `--id` is specified:

1. The system checks if `--rig` or `--prefix` flags are provided
2. If yes → calls `createInRig()` which correctly routes to target database
3. If no → uses current database and validates ID prefix against it

**Missing logic**: When `--id` is provided WITHOUT `--rig`/`--prefix` flags, the system should:
- Extract prefix from the ID
- Look up prefix in routes.jsonl
- Automatically route to the correct database (like `createInRig` does)

## Workaround

Use `--rig` or `--prefix` flag explicitly:

```bash
bd create --id=pq-pgqueue-crew-pgq_crew --title="Test" --type=task --rig=pgqueue
# OR
bd create --id=pq-pgqueue-crew-pgq_crew --title="Test" --type=task --prefix=pq
```

This works because `createInRig()` correctly resolves the target database.

## Test Case

See `internal/routing/create_with_id_test.go`:
- `TestCreateWithExplicitIDShouldRouteToCorrectDatabase` - Demonstrates the expected behavior
- `TestResolveBeadsDirForIDWithMismatchedPrefix` - Tests routing logic for mismatched prefixes

These tests verify that `ResolveBeadsDirForID()` correctly routes IDs to their target databases based on prefix matching in routes.jsonl.

## Proposed Fix

In `cmd/bd/create.go`, before validating the prefix:

```go
// If explicitID is provided, check if it needs routing
if explicitID != "" {
    prefix := routing.ExtractPrefix(explicitID)
    if prefix != "" {
        // Check if this prefix matches a route
        routes, err := routing.LoadTownRoutes(beadsDir)
        if err == nil && len(routes) > 0 {
            for _, route := range routes {
                if route.Prefix == prefix {
                    // Found a matching route - need to route to different database
                    // Extract the rig name from the route path
                    rigName := routing.ExtractProjectFromPath(route.Path)
                    if rigName != "" && rigName != "." {
                        // Call createInRig to create in the target database
                        createInRig(cmd, rigName, title, description, issueType, priority, ...)
                        return
                    }
                }
            }
        }
    }
}
```

This ensures that explicit IDs are automatically routed to the correct database, making the behavior consistent with user expectations.

## Impact

**Severity**: Medium
- Blocks creation of agent beads in Gas Town when `gt doctor --fix` tries to create them
- Workaround exists (`--rig` flag) but is non-obvious
- Violates principle of least surprise (routes.jsonl implies automatic routing)

**Affected Users**:
- Gas Town users creating issues with explicit IDs across rigs
- Automated scripts/tools (like `gt doctor --fix`) that create cross-rig issues
- Any multi-repo setup using routes.jsonl

## Related Code

- `internal/routing/routes.go` - `ResolveBeadsDirForID()` (already works correctly)
- `cmd/bd/create.go` - Needs to call routing logic when `--id` is provided
- `cmd/bd/create.go` - `createInRig()` (reference implementation for cross-rig creation)

# SYNTHESIS: bd repo commands investigation

## TLDR

`bd repo` commands (list, add, remove, sync) fail with "unexpected end of JSON input" because `getRepoConfig()` doesn't handle empty config values. Simple one-line fix needed. Cross-project epic support is already viable via the multi-repo hydration layer.

## Findings

### Bug Root Cause

**Location:** `cmd/bd/repo.go:190-206`

The `getRepoConfig()` function expects `GetConfig()` to return an error for missing keys, but it returns `("", nil)`. When `value == ""`, the code tries `json.Unmarshal([]byte(""), ...)` which fails.

**Fix:**
```go
// After line 197, before JSON parsing:
if value == "" {
    return make(map[string]string), nil
}
```

### Two Config Systems (Architectural Issue)

beads has two incompatible multi-repo config mechanisms:
1. `bd repo add` - Stores JSON map: `{"alias": "/path"}`
2. `bd config set repos.additional` - Stores comma-separated string: `/path1,/path2`

These are not integrated. The docs reference `bd config`, but `bd repo` exists as a separate system.

### Cross-Project Epic Support

Already viable via `internal/storage/sqlite/multirepo.go`:
- `HydrateFromMultiRepo()` loads issues from multiple repos
- `source_repo` field tracks ownership
- Cross-repo dependencies work: `bd dep add impl-42 plan-10`
- Used for OSS contributor workflows (planning repo + fork)

## Recommendation

**Option A (minimal):** Fix the bug only - add empty value check
- Pro: Quickest fix
- Con: Two config systems remain inconsistent

**Option B (clean):** Deprecate `bd repo` in favor of `bd config set repos.additional`
- Pro: Single config mechanism, matches documentation
- Con: Breaking change for anyone using `bd repo`

**Recommend: Option A first** (quick bug fix), then consider Option B in a follow-up issue.

## Evidence

```bash
$ bd repo list
Error: failed to load config: failed to parse repos config: unexpected end of JSON input

$ bd config get repos.additional
repos.additional (not set)  # Works correctly
```

## Deliverables

1. `investigation.md` - Full investigation with D.E.K.N. summary
2. This synthesis document

## Next Steps

1. Create bug fix issue for `getRepoConfig()` empty value handling
2. Consider follow-up issue to unify/deprecate config mechanisms

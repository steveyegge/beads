# SYNTHESIS: Fix bd repo JSON parsing bug

## TLDR

Fixed `getRepoConfig()` in `cmd/bd/repo.go` to handle empty config values. Added empty string check before `json.Unmarshal` to prevent "unexpected end of JSON input" error. Added comprehensive tests.

## Changes Made

### Bug Fix
**Location:** `cmd/bd/repo.go:197-200`

Added empty value check after `GetConfig()` returns:
```go
// Handle empty value (config key exists but no value set)
if value == "" {
    return make(map[string]string), nil
}
```

### Tests Added
**Location:** `cmd/bd/repo_test.go` (new file)

- `TestGetRepoConfig_EmptyValue` - Tests the bug fix (empty string → empty map)
- `TestSetRepoConfig` - Tests round-trip set/get with valid data
- `TestRepoConfigEmptyMap` - Tests setting and reading empty map

## Evidence

```bash
# Tests all pass
$ go test -v -run "TestGetRepoConfig|TestSetRepoConfig|TestRepoConfigEmptyMap" ./cmd/bd/
=== RUN   TestGetRepoConfig_EmptyValue
--- PASS: TestGetRepoConfig_EmptyValue (0.03s)
=== RUN   TestSetRepoConfig
--- PASS: TestSetRepoConfig (0.01s)
=== RUN   TestRepoConfigEmptyMap
--- PASS: TestRepoConfigEmptyMap (0.01s)
PASS

# Build succeeds
$ go build ./cmd/bd/
```

## Recommendation

**close** - Bug fix is complete with tests. The fix is minimal and targeted to the specific issue identified in the investigation.

## Next Steps (Discovered Work)

None for this fix. The investigation already noted the architectural issue of two config systems (`bd repo` vs `bd config set repos.additional`) as a potential follow-up item.

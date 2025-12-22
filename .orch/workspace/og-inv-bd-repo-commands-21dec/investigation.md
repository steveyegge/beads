# Investigation: bd repo commands - Multi-Repo Functionality

## Summary (D.E.K.N.)

**Delta:** `bd repo` commands fail with JSON parsing error when no repos are configured. The bug is in `getRepoConfig()` which doesn't handle empty string from GetConfig. Cross-project epic support is already viable via the multi-repo hydration layer.

**Evidence:** Reproduced `bd repo list` and `bd repo add` failures with "unexpected end of JSON input" error. Root cause: GetConfig returns "" for missing keys, not error, but code tries to JSON-parse empty string.

**Knowledge:** beads has two multi-repo mechanisms: (1) `bd repo` commands store JSON map in SQLite config, (2) `bd config set repos.additional` stores in Viper/YAML. These are not integrated - `bd repo` has the bug, `bd config` works for reading.

**Next:** Fix `getRepoConfig()` to check for empty value before JSON parsing. Consider whether to unify the two config mechanisms or deprecate `bd repo` in favor of `bd config set repos.additional`.

**Confidence:** High (95%) - Bug reproduced and root cause identified in code.

---

## Question

What does `bd repo` do, how is it supposed to work, is the JSON parsing bug in parsing or config format, and is cross-project epic support viable?

**Status:** Complete

## Findings

### 1. bd repo Commands Overview

The `bd repo` command group (`cmd/bd/repo.go`) provides:
- `bd repo add <path> [alias]` - Add an additional repository to sync
- `bd repo remove <key>` - Remove a repository from sync configuration  
- `bd repo list` - List all configured repositories
- `bd repo sync` - Manually trigger multi-repo sync

These commands are intended for the "multi-clone sync" use case where multiple git clones share issues.

### 2. JSON Parsing Bug - Root Cause

**Location:** `cmd/bd/repo.go:190-206`

```go
func getRepoConfig(ctx context.Context, store storage.Storage) (map[string]string, error) {
    value, err := store.GetConfig(ctx, "repos.additional")
    if err != nil {
        if strings.Contains(err.Error(), "not found") {
            return make(map[string]string), nil
        }
        return nil, err
    }

    // Parse JSON map
    repos := make(map[string]string)
    if err := json.Unmarshal([]byte(value), &repos); err != nil {
        return nil, fmt.Errorf("failed to parse repos config: %w", err)
    }
    return repos, nil
}
```

**Problem:** `GetConfig` returns `("", nil)` when the key doesn't exist (see `internal/storage/sqlite/config.go:22-23`). The code checks for `err != nil`, but since `err` is `nil` and `value` is `""`, it proceeds to parse `json.Unmarshal([]byte(""), ...)` which fails with "unexpected end of JSON input".

**Fix:** Add a check for empty value:
```go
if value == "" {
    return make(map[string]string), nil
}
```

### 3. Two Separate Multi-Repo Configuration Systems

beads has **two** ways to configure multi-repo:

1. **`bd repo` commands** - Stores as JSON map in SQLite `config` table with key `repos.additional`
   - Format: `{"alias": "/path/to/repo", ...}`
   - Bug: Doesn't handle empty/missing config

2. **`bd config set repos.additional`** - Stores as comma-separated string in SQLite `config` table
   - Format: `/path1,/path2,/path3`
   - Works correctly (tested: `bd config get repos.additional` returns "(not set)")

Additionally, `internal/config/config.go` reads `repos.additional` as a string slice from Viper (YAML config file).

These systems are NOT integrated - the `bd repo` JSON format and `bd config` string format are incompatible.

### 4. Multi-Repo Hydration Layer (Cross-Project Support)

The `internal/storage/sqlite/multirepo.go` file implements **cross-project issue aggregation**:

- `HydrateFromMultiRepo()` - Loads issues from multiple repo JSONL files into unified SQLite
- Uses `source_repo` field to track which repo owns each issue
- Supports cross-repo dependencies (e.g., `bd dep add impl-42 plan-10 --type blocks`)
- Uses mtime caching to skip unchanged JSONL files

**Cross-project epic support IS viable** - the hydration layer already supports:
- Loading issues from multiple repos
- Tracking `source_repo` on each issue
- Creating dependencies across repos
- Unified querying across all hydrated issues

From `docs/FAQ.md:241`: "Issues cannot reference issues in other projects. Each database is isolated by design. If you need cross-project tracking, initialize bd in a parent directory that contains both projects."

### 5. Documentation Claims vs Reality

The documentation (`docs/MULTI_REPO_AGENTS.md`, `docs/MULTI_REPO_MIGRATION.md`) shows:
```bash
bd config set repos.additional "~/repo1,~/repo2,~/repo3"
bd dep add impl-42 plan-10 --type blocks  # Link across repos
```

But `bd repo add` commands use a different JSON format that's broken.

## Test Performed

**Test 1:** Reproduced bug
```bash
$ bd repo list
Error: failed to load config: failed to parse repos config: unexpected end of JSON input

$ bd repo add /tmp/test-repo test-alias
Error: failed to get existing repos: failed to parse repos config: unexpected end of JSON input
```

**Test 2:** Verified database state
```bash
$ sqlite3 .beads/beads.db "SELECT * FROM config WHERE key LIKE 'repos%';"
# (no output - no repos.additional key exists)
```

**Test 3:** Verified `bd config` works
```bash
$ bd config get repos.additional
repos.additional (not set)
# Works correctly - no error
```

## Conclusion

1. **Bug confirmed:** `bd repo` commands fail because `getRepoConfig()` tries to JSON-parse an empty string when no repos are configured. The fix is a one-line check for empty value.

2. **Two incompatible systems:** `bd repo` uses JSON maps, `bd config` uses comma-separated strings. This is a design inconsistency that should be resolved.

3. **Cross-project epics are viable:** The multi-repo hydration layer (`multirepo.go`) already supports loading issues from multiple repos and creating cross-repo dependencies. The `source_repo` field tracks ownership.

4. **Recommendation:** Either:
   - Fix `bd repo` commands AND unify with `bd config` format, OR
   - Deprecate `bd repo` in favor of `bd config set repos.additional`

## Self-Review

- [x] Real test performed (not code review)
- [x] Conclusion from evidence (not speculation)
- [x] Question answered
- [x] File complete

**Self-Review Status:** PASSED

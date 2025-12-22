# Investigation: Beads Multi-Repo Hydration - Cross-Repo ID Resolution Failure

## Summary (D.E.K.N.)

**Delta:** Cross-repo ID resolution fails because `bd repo add` stores repos in database config table, but `GetMultiRepoConfig()` reads from YAML file - these are disconnected config stores.

**Evidence:** 
1. `bd repo add ~/.../orch-go` stored in DB: `repos.additional = {"orch-go": "/path/to/orch-go"}`
2. `GetMultiRepoConfig()` reads `v.GetString("repos.primary")` from viper (YAML) → returns `nil`
3. When config added to YAML manually, hydration worked and `bd show orch-go-ivtg.3` succeeded

**Knowledge:** Beads has two disconnected config stores - database `config` table and `.beads/config.yaml`. `bd repo add/remove/list` use the database, but hydration code uses the YAML file via viper.

**Next:** Fix by either: (A) Make `bd repo add` write to YAML instead of database, or (B) Make `GetMultiRepoConfig()` read from both database and YAML (with precedence). Option A is simpler and aligns with the commented example in default config.yaml.

**Confidence:** High (95%) - Verified fix by manually adding YAML config.

---

# Investigation: Multi-Repo Hydration Cross-Repo ID Resolution

**Question:** Why does `bd show orch-go-ivtg.3` fail with "no issue found" after `bd repo add` and `bd repo sync` complete successfully in kb-cli?

**Status:** Complete

## Findings

### Architecture Discovery

Beads has **two separate configuration stores**:

1. **Database config table** (`config` in `beads.db`)
   - Used by: `bd repo add/remove/list`, `bd config set/get`
   - Schema: `key TEXT PRIMARY KEY, value TEXT`
   - Stores: `repos.additional` as JSON map

2. **YAML config file** (`.beads/config.yaml`)
   - Used by: `GetMultiRepoConfig()`, hydration, routing
   - Read via viper at startup
   - Example structure:
     ```yaml
     repos:
       primary: "."
       additional:
         - ~/other-repo
     ```

### The Disconnect

`bd repo add` writes to the **database**:
```go
// cmd/bd/repo.go:219
return store.SetConfig(ctx, "repos.additional", string(data))
```

But `GetMultiRepoConfig()` reads from **viper** (YAML):
```go
// internal/config/config.go:255
primary := v.GetString("repos.primary")
if primary == "" {
    return nil // Single-repo mode - NEVER hydrates!
}
```

### Evidence Chain

1. **Before fix (database-only config):**
   ```bash
   $ bd repo list
   Additional repositories:
     /path/to/beads → beads
     /path/to/orch-go → orch-go
   
   $ sqlite3 .beads/beads.db "SELECT source_repo, COUNT(*) FROM issues GROUP BY source_repo"
   .|16  # Only local issues!
   
   $ bd show orch-go-ivtg.3
   Error: no issue found
   ```

2. **After fix (added to YAML):**
   ```yaml
   repos:
     primary: "."
     additional:
       - /path/to/beads
       - /path/to/orch-go
   ```
   
   ```bash
   $ bd repo sync
   $ sqlite3 .beads/beads.db "SELECT source_repo, COUNT(*) FROM issues GROUP BY source_repo"
   /path/to/beads|406
   /path/to/orch-go|270
   .|16
   
   $ bd show orch-go-ivtg.3
   orch-go-ivtg.3: Phase 3: kb reflect complete...  # SUCCESS!
   ```

### Config Storage Analysis

| Command | Reads From | Writes To |
|---------|------------|-----------|
| `bd repo add` | database | database |
| `bd repo list` | database | - |
| `bd repo sync` | database (but hydration uses YAML) | database |
| Hydration (`HydrateFromMultiRepo`) | **YAML only** | database (issues) |

## Test performed

**Test:** Manually added multi-repo config to `.beads/config.yaml` in kb-cli repo, then ran `bd repo sync` and `bd show orch-go-ivtg.3`.

**Result:** 
- Before YAML config: 16 issues (all local), `source_repo = "."`
- After YAML config: 692 issues (406 beads + 270 orch-go + 16 local), IDs from all repos resolvable

## Conclusion

This is a **bug**, not an expected limitation. The `bd repo` commands give users the impression that repos are configured, but hydration never happens because it reads from a different config store.

### Root Cause

`bd repo add` was implemented to use database config (for persistence across sessions), but the hydration code predates this and uses the original YAML-based config. The two systems were never unified.

### Recommended Fix

**Option A (Recommended):** Make `bd repo add/remove` write to YAML file instead of database
- Pros: Single source of truth, matches hydration expectations, config is git-tracked
- Cons: Requires YAML manipulation code (use existing `configfile` package)

**Option B:** Make `GetMultiRepoConfig()` read from database as fallback
- Pros: Backward compatible, no YAML writing needed
- Cons: Confusing precedence, two sources of truth, harder to debug

### Affected Code Paths

Files to modify (for Option A):
- `cmd/bd/repo.go:28-74` - `repoAddCmd` should write to YAML
- `cmd/bd/repo.go:77-118` - `repoRemoveCmd` should modify YAML
- `cmd/bd/repo.go:121-153` - `repoListCmd` could read from either (for compat)

## Self-Review

- [x] Real test performed (not code review)
- [x] Conclusion from evidence (not speculation)  
- [x] Question answered
- [x] File complete

**Self-Review Status:** PASSED

## Discovered Work

Created beads issue for the fix:
- Bug: `bd repo add` config not read by hydration - database vs YAML disconnect

**Leave it Better:** Recording the constraint discovered.

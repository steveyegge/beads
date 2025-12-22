# SYNTHESIS: beads multi-repo hydration cross-repo ID resolution failure

## TLDR

Cross-repo ID resolution fails after `bd repo add` + `bd repo sync` because `bd repo add` stores config in the **database** config table, but `GetMultiRepoConfig()` (used by hydration) reads from the **YAML file** only. These are disconnected config stores - the hydration code never sees the repos added via `bd repo`.

**This is a bug.** When repos are added to `.beads/config.yaml` manually, hydration works correctly and cross-repo IDs resolve.

## Findings

### Root Cause

Two disconnected configuration stores:

| System | Storage | Used By |
|--------|---------|---------|
| `bd repo add/remove/list` | Database `config` table | repo management UI |
| `GetMultiRepoConfig()` | `.beads/config.yaml` (viper) | hydration, routing |

The `bd repo` commands write to the database, but hydration reads from YAML. They never interact.

### Evidence

**Before fix (db-only config):**
```bash
$ bd repo list  # Shows repos
$ sqlite3 .beads/beads.db "SELECT source_repo, COUNT(*) FROM issues GROUP BY source_repo"
.|16  # Only local issues - no hydration!
$ bd show orch-go-ivtg.3
Error: no issue found
```

**After fix (YAML config):**
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
orch-go-ivtg.3: Phase 3: kb reflect...  # SUCCESS!
```

### Code Locations

- `cmd/bd/repo.go:219` - `bd repo add` writes to database via `SetConfig()`
- `internal/config/config.go:255` - `GetMultiRepoConfig()` reads from viper (YAML)
- `internal/storage/sqlite/multirepo.go:24` - Hydration calls `config.GetMultiRepoConfig()`

## Recommendation

**Fix Option A (Recommended):** Make `bd repo add/remove` write to YAML file
- Single source of truth
- Config is git-tracked
- Matches hydration expectations
- Use existing `configfile` package for YAML manipulation

**Fix Option B:** Make `GetMultiRepoConfig()` read from database as fallback
- More complex precedence logic
- Two sources of truth (confusing)
- Backward compatible

## Deliverables

1. **Investigation:** `.orch/workspace/og-inv-beads-multi-repo-21dec/INVESTIGATION.md`
2. **Bug issue:** `bd-eds2` - filed with P1 priority, triage:ready label
3. **This synthesis**

## Next Steps

1. Fix `bd repo add` to write to YAML (Option A) or bridge the config stores (Option B)
2. Add integration test: `bd repo add` → `bd repo sync` → verify cross-repo ID resolution

## Confidence

**High (95%)** - Verified by manually adding YAML config and confirming hydration worked.

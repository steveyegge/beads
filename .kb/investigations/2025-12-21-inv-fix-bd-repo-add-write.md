## Summary (D.E.K.N.)

**Delta:** Fixed `bd repo add/remove` to write to YAML config instead of database, aligning with how `GetMultiRepoConfig()` reads the config.

**Evidence:** Built and tested - `bd repo add` now creates/updates repos section in `.beads/config.yaml`, `bd repo sync` then hydrates correctly.

**Knowledge:** The bug was a simple data store disconnect - write path used DB, read path used YAML. Single source of truth (YAML) is cleaner.

**Next:** Close issue, merge the fix.

**Confidence:** High (95%) - Unit tests pass, manual validation confirms YAML is written and read correctly.

---

# Investigation: Fix bd repo add to write YAML

**Question:** How to fix `bd repo add` to write to config.yaml so `GetMultiRepoConfig()` can read it during hydration?

**Started:** 2025-12-21
**Updated:** 2025-12-21
**Owner:** Agent (og-feat-fix-bd-repo-21dec)
**Phase:** Complete
**Next Step:** None
**Status:** Complete
**Confidence:** High (95%)

---

## Findings

### Finding 1: Root cause was disconnected config stores

**Evidence:** 
- `cmd/bd/repo.go:219` wrote to database via `store.SetConfig(ctx, "repos.additional", ...)`
- `internal/config/config.go:255` read via viper from YAML file only
- These never interacted

**Source:** `cmd/bd/repo.go:190-220`, `internal/config/config.go:247-264`

**Significance:** Explains why `bd repo add` appeared to work but hydration never saw the repos.

---

### Finding 2: YAML is the correct source of truth

**Evidence:** 
- Config.yaml is version-controlled and shared across clones
- Database config table is gitignored (per-clone)
- `GetMultiRepoConfig()` already reads from viper/YAML

**Source:** Analysis of config precedence in `internal/config/config.go:19-134`

**Significance:** Writing to YAML aligns with existing architecture and enables sharing repo config across clones.

---

### Finding 3: gopkg.in/yaml.v3 already available

**Evidence:** Already used in `cmd/bd/autoimport.go` for YAML parsing

**Source:** `go.mod`, `cmd/bd/autoimport.go:20`

**Significance:** No new dependencies needed. Can use existing yaml.Node for preserving comments/formatting.

---

## Synthesis

**Key Insights:**

1. **Single source of truth** - Using YAML exclusively eliminates the disconnect and confusion about which config source is authoritative.

2. **Version-controlled config** - Repos config in YAML means it's shared across all clones of a repository, enabling team-wide multi-repo setups.

3. **Minimal code change** - Just replaced database read/write with YAML read/write in repo.go, plus new helper functions in config/repos.go.

**Answer to Investigation Question:**

Created `internal/config/repos.go` with `AddRepo()`, `RemoveRepo()`, and `ListRepos()` functions that manipulate the `repos` section of `config.yaml`. Updated `cmd/bd/repo.go` to use these instead of the database. This ensures `GetMultiRepoConfig()` sees repos added via `bd repo add`.

---

## Confidence Assessment

**Current Confidence:** High (95%)

**Why this level?**

Unit tests pass, manual testing confirms YAML is correctly written and read. The fix is straightforward with no edge cases that could cause regressions.

**What's certain:**

- ✅ `bd repo add` writes to config.yaml repos section
- ✅ `bd repo remove` removes from config.yaml repos section
- ✅ `bd repo list` reads from config.yaml
- ✅ Existing config is preserved (issue-prefix, sync-branch, etc.)
- ✅ `GetMultiRepoConfig()` now sees the repos added via CLI

**What's uncertain:**

- ⚠️ Migration path for users who have repos in database (minor - they can re-add)

---

## Implementation Complete

**Files created/modified:**
- `internal/config/repos.go` - New YAML manipulation functions
- `internal/config/repos_test.go` - Unit tests
- `cmd/bd/repo.go` - Updated to use YAML functions
- Removed `cmd/bd/repo_test.go` - Obsolete database tests

**Testing:**
- All unit tests pass
- Manual testing confirmed:
  - `bd repo add` creates repos section with primary="." and adds to additional
  - `bd repo list` correctly shows configured repos
  - `bd repo remove` removes repos and cleans up empty section

---

## References

**Files Examined:**
- `cmd/bd/repo.go` - Original database-based implementation
- `internal/config/config.go` - GetMultiRepoConfig() reads from viper/YAML
- `cmd/bd/autoimport.go` - Example of YAML parsing in codebase

**Related Artifacts:**
- **Investigation:** `.orch/workspace/og-inv-beads-multi-repo-21dec/SYNTHESIS.md` - Original investigation that identified the bug
- **Issue:** `bd-eds2` - Beads issue tracking this fix

---

## Investigation History

**2025-12-21 21:15:** Investigation started
- Initial question: How to fix the db vs YAML config disconnect for bd repo add
- Context: Prior investigation identified the root cause

**2025-12-21 21:45:** Implementation complete
- Created internal/config/repos.go with YAML manipulation
- Updated cmd/bd/repo.go to use new functions
- All tests passing

**2025-12-21 21:50:** Investigation completed
- Final confidence: High (95%)
- Status: Complete
- Key outcome: `bd repo add/remove` now writes to YAML, fixing hydration

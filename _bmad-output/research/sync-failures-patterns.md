# Sync Failures Patterns Analysis

## Executive Summary

- **Total issues analyzed:** 16
- **Patterns identified:** 6 distinct patterns
- **Most common severity:** High (8 issues)
- **Critical issues:** 3 (data loss scenarios)

This analysis documents sync failure patterns extracted from the [steveyegge/beads](https://github.com/steveyegge/beads) GitHub repository. Patterns are categorized by failure type, frequency, and severity to inform recovery documentation.

## Methodology

### Search Queries Used
- `is:issue sync` - 36+ results
- `is:issue daemon` - 12 results
- `is:issue worktree` - 12 results
- `is:issue "bd sync"` - 12 results
- `is:issue push fail` - 12 results
- `is:issue import export` - 12 results

### Date Range
- Issues from December 2025 (v0.29.0 - v0.41.0)

### Selection Criteria
- Issues directly related to sync operations, daemon synchronization, or worktree management
- Minimum impact: workflow blocked or data integrity compromised
- Excluded: Feature requests, UI issues, documentation-only issues

### Document Purpose

This is a **research analysis document** for Epic 2 synthesis, not a production recovery runbook. Pattern format includes analytical fields (Frequency, Severity, Root Cause) that will be condensed when migrating to `website/docs/recovery/`.

---

## Quick Reference: Diagnostic Commands

Before diving into specific patterns, use these commands to identify your sync issue:

```bash
# Check daemon status
bd daemons status

# Check sync branch configuration
bd config get sync.branch
bd config get sync.remote

# Check worktree health
git worktree list
ls -la .git/beads-worktrees/

# Check JSONL state
wc -l .beads/issues.jsonl
bd stats

# Force manual sync
bd sync --no-daemon

# Check for stuck auth
ps aux | grep git-credential
```

---

## Pattern Catalog

### Pattern 1: Worktree Management Failures

#### Symptoms
- Error: `fatal: '.git/beads-worktrees/beads-metadata' is a missing but already registered worktree`
- Error: `fatal: 'main' is already used by worktree at '/path/to/project'`
- Error: `fatal: not a git repository (or any of the parent directories): .git`
- Sync push works but pull fails consistently
- Worktree health check errors in daemon.log

#### Root Cause
Multiple worktree lifecycle issues:
1. **Orphaned registration:** Worktree contents deleted but git registration persists
2. **Branch conflict:** Attempting to create worktree for currently checked-out branch
3. **Bare repo detection:** `.git` is a file (not directory) in worktree setups, breaking path resolution
4. **Health check gaps:** `CreateBeadsWorktree` returned success without comprehensive validation

#### Frequency
5 of 16 issues (31%)

#### Severity: High (workflow blocked, requires manual intervention)

#### Solutions
1. **For orphaned worktree registration (fixed in v0.31.0+):**
   ```bash
   # Manual cleanup
   git worktree prune
   rm -rf .git/beads-worktrees/beads-metadata
   bd sync
   ```

2. **For branch conflict:**
   ```bash
   # Configure different sync branch
   bd config set sync.branch beads-sync
   bd sync
   ```

3. **For bare repo issues (fixed in v0.31.0+):**
   ```bash
   # Update to latest version
   git pull origin main
   go install ./cmd/bd
   ```

4. **Force worktree recreation:**
   ```bash
   git worktree remove .git/beads-worktrees/beads-metadata --force
   bd sync
   ```

#### Prevention
- Use latest beads version (v0.40.0+) with consolidated worktree health checks
- Avoid using the same branch name for both work and sync
- In bare repo setups, ensure beads v0.31.0+ which uses `git rev-parse --git-common-dir`

#### References
- [#609](https://github.com/steveyegge/beads/issues/609) - Daemon auto-sync fails with recurring worktree error
- [#639](https://github.com/steveyegge/beads/issues/639) - Worktree creation fails in bare repository
- [#710](https://github.com/steveyegge/beads/issues/710) - Worktree health check error
- [#519](https://github.com/steveyegge/beads/issues/519) - bd sync fails when sync.branch is checked out
- [#785](https://github.com/steveyegge/beads/issues/785) - Sync fails across worktrees in bare repo

---

### Pattern 2: Daemon Push/Pull Race Conditions

#### Symptoms
- Error: `push failed after 5 attempts: git push failed from worktree`
- Issues created in one clone don't appear in other clones
- Daemon logs show push rejection with "fetch first" errors
- Deletions (tombstones) don't propagate to other clones
- Changes visible locally but never reach remote

#### Root Cause
Multiple daemon synchronization issues:
1. **No push retry logic:** `gitPushFromWorktree()` lacked error recovery when remote had newer commits
2. **Event-driven only:** Daemon only watched local file changes, not remote updates
3. **Tombstone exclusion:** `store.SearchIssues()` used default filter excluding deleted issues
4. **Missing copy step:** After pulling in sync worktree, data wasn't copied back to local `.beads/`

#### Frequency
4 of 16 issues (25%)

#### Severity: Critical (can cause data loss)

#### Solutions
1. **For push failures (fixed in v0.35.0+):**
   ```bash
   # Manual sync with retry
   bd sync
   # Or kill daemon and sync manually
   bd daemons killall
   bd sync --no-daemon
   ```

2. **For missing remote updates:**
   ```bash
   # Force pull from remote
   bd sync --import-only
   # Or configure auto_pull
   bd config set daemon.auto_pull true
   bd config set daemon.auto_pull_interval 30
   ```

3. **For tombstone sync issues (fixed in v0.35.0+):**
   ```bash
   # Force full export/import
   bd export -o .beads/issues.jsonl
   bd sync
   ```

4. **For bare repo sync gaps:**
   ```bash
   # Ensure sync worktree path normalization (v0.40.0+)
   git pull origin main && go install ./cmd/bd
   bd sync
   ```

#### Prevention
- Use daemon with `auto_pull` enabled for multi-clone setups
- Update to v0.35.0+ for fetch-rebase-retry push logic
- Run `bd sync` manually after creating issues in multi-user scenarios
- Monitor daemon.log for push/pull failures

#### References
- [#694](https://github.com/steveyegge/beads/issues/694) - Daemon push fails when remote has newer commits
- [#695](https://github.com/steveyegge/beads/issues/695) - Event-driven daemon doesn't pull remote updates
- [#693](https://github.com/steveyegge/beads/issues/693) - Daemon export excludes tombstones
- [#785](https://github.com/steveyegge/beads/issues/785) - Sync fails across worktrees (missing copy step)

---

### Pattern 3: Data Loss During Sync

#### Symptoms
- Issues disappear after running `bd sync`
- `bd stats` shows decreased issue count post-sync
- Code changes reverted after sync commit
- Recently created issues vanish from database
- Error: `DATABASE MISMATCH DETECTED!`

#### Root Cause
Three distinct data loss scenarios:
1. **Overwrite instead of merge:** Local JSONL with fewer issues overwrote remote JSONL
2. **Race condition:** Daemon pulls before unpushed local changes are synced
3. **History backfill:** `git-history-backfill` deleted open issues during repo ID mismatch
4. **Merge conflict resolution:** Incorrect 3-way merge interpreted deletions

#### Frequency
2 of 16 issues (13%)

#### Severity: Critical (permanent data loss possible)

#### Solutions
1. **Immediate recovery from JSONL:**
   ```bash
   # Check git history for issues.jsonl
   git log --oneline -- .beads/issues.jsonl
   git show <commit>:.beads/issues.jsonl > /tmp/recovered.jsonl
   bd import -i /tmp/recovered.jsonl
   ```

2. **For repo ID mismatch:**
   ```bash
   bd migrate --update-repo-id
   ```

3. **For daemon race condition:**
   ```bash
   # Stop daemon during issue creation
   bd daemons killall
   bd create "New issue"
   bd sync
   bd daemons start
   ```

4. **Bootstrap fresh clone safely:**
   ```bash
   # Always import from sync branch first
   bd init --prefix <prefix>
   bd sync --import-only
   ```

#### Prevention
- Push local changes before pulling remote changes
- Use `bd sync --import-only` on fresh clones before creating issues
- Backup `.beads/` directory before major sync operations
- Fixed in v0.30.0+: Merge logic instead of overwrite
- Fixed in PR #564: Bootstrap from sync branch

#### References
- [#464](https://github.com/steveyegge/beads/issues/464) - Beads deletes issues
- [#746](https://github.com/steveyegge/beads/issues/746) - bd sync commits regressing source code changes

---

### Pattern 4: Configuration Not Honored

#### Symptoms
- Error: `prefix mismatch detected: database uses 'X' but found issues with prefixes: [Y, Z]`
- Sync uses `origin` despite `sync.remote` configured differently
- Error: `not in a bd workspace (no .beads directory found)` in no-db mode
- `bd sync --no-pull` works but `bd sync` fails

#### Root Cause
Configuration settings accepted but not utilized in execution paths:
1. **sync.remote ignored:** Daemon sync functions defaulted to `origin` without checking config
2. **Multi-repo prefix validation:** Sync import didn't recognize configured additional repos
3. **No-db mode detection:** Path discovery failed when `dbPath` was empty

#### Frequency
3 of 16 issues (19%)

#### Severity: High (workflow blocked)

#### Solutions
1. **For sync.remote not honored (fixed in v0.37.0+):**
   ```bash
   # Verify config
   bd config get sync.remote
   # Update to latest
   git pull && go install ./cmd/bd
   bd sync
   ```

2. **For multi-repo prefix mismatch:**
   ```bash
   # Workaround: skip import
   bd sync --no-pull
   # Or: verify repos.additional config
   cat .beads/config.yaml | grep -A5 repos
   ```

3. **For no-db mode (fixed in v0.29.1+):**
   ```bash
   # Set explicit JSONL path
   export BEADS_JSONL=.beads/issues.jsonl
   bd sync
   ```

#### Prevention
- Test sync after configuration changes
- Use latest beads version for full config support
- Avoid mixing no-db mode with sync branch features

#### References
- [#736](https://github.com/steveyegge/beads/issues/736) - sync.remote config ignored
- [#686](https://github.com/steveyegge/beads/issues/686) - Prefix mismatch in multi-repo setups
- [#546](https://github.com/steveyegge/beads/issues/546) - bd sync fails in no-db mode

---

### Pattern 5: Hook/Authentication Blocking

#### Symptoms
- Command appears frozen with no user feedback
- Error: `push failed after 5 attempts: git push failed from worktree: context canceled`
- Circular error: bd sync fails recommending running... bd sync
- Error: `Beads JSONL has uncommitted changes` during sync
- Browser waiting for GitHub authorization page

#### Root Cause
External dependencies blocking sync operations:
1. **Credential helper blocking:** Git push waits for browser auth without feedback
2. **Pre-commit hook conflict:** Hooks trigger `bd sync` inside worktree context, causing circular failure
3. **Pre-push hook conflict:** Hook detects uncommitted JSONL during sync push itself

#### Frequency
2 of 16 issues (13%)

#### Severity: Medium (workaround available)

#### Solutions
1. **For frozen auth (fixed in v0.32.0+):**
   ```bash
   # Check for browser auth prompts
   # After 5 seconds, beads now displays timeout message
   # Manual: Ctrl+C and check browser
   ```

2. **For hook conflicts:**
   ```bash
   # Reinstall hooks with fix
   bd hooks install
   # Or temporarily bypass
   bd sync --no-verify
   ```

3. **For circular sync error:**
   ```bash
   # The BD_SYNC_IN_PROGRESS env var prevents recursion
   # Update hooks
   bd hooks install
   ```

4. **For daemon/hook interaction (fixed in v0.33.0+):**
   ```bash
   # Daemon now uses --no-verify for worktree commits
   git pull && go install ./cmd/bd
   bd daemons restart
   ```

#### Prevention
- Keep hooks updated with `bd hooks install` after upgrades
- Configure SSH keys to avoid browser auth prompts
- Use credential caching: `git config --global credential.helper cache`

#### References
- [#647](https://github.com/steveyegge/beads/issues/647) - bd sync frozen waiting for GitHub auth
- [#532](https://github.com/steveyegge/beads/issues/532) - Circular bd sync error
- [#520](https://github.com/steveyegge/beads/issues/520) - Daemon auto-sync fails with pre-commit hooks

---

### Pattern 6: State Reconstruction Failures

#### Symptoms
- Error: `Sync branch commit failed: failed to commit in worktree`
- JSONL file hash mismatch warnings
- Export hashes cleared forcing full re-export
- Inconsistent issue counts between clones

#### Root Cause
State tracking mechanisms become desynchronized:
1. **Hash mismatch:** JSONL content changed externally without updating export_hashes
2. **Worktree state corruption:** Sync worktree in inconsistent state
3. **Database/JSONL drift:** Local database and JSONL file out of sync

#### Frequency
1 of 16 issues (6%)

#### Severity: Medium (recoverable with rebuild)

#### Solutions
1. **For hash mismatch:**
   ```bash
   # Clear hashes and force re-sync
   bd sync --flush-only
   bd sync
   ```

2. **For worktree state corruption:**
   ```bash
   # Remove and recreate worktree
   git worktree remove .git/beads-worktrees/beads-metadata --force
   git worktree prune
   bd sync
   ```

3. **For database/JSONL drift:**
   ```bash
   # Rebuild database from JSONL
   rm .beads/beads.db*
   bd sync --import-only
   ```

#### Prevention
- Don't manually edit `.beads/issues.jsonl`
- Run `bd doctor` periodically to detect drift
- Use `bd sync` as the only interface for JSONL changes

#### References
- [#520](https://github.com/steveyegge/beads/issues/520) - Daemon commit failures
- [#532](https://github.com/steveyegge/beads/issues/532) - Hash mismatch detection

---

## Severity Distribution

| Severity | Count | Percentage | Description |
|----------|-------|------------|-------------|
| Critical | 3 | 21% | Data loss, unrecoverable without backup |
| High | 8 | 57% | Workflow blocked, requires manual intervention |
| Medium | 3 | 21% | Inconvenient, workaround available |

## Pattern Frequency Summary

| Pattern | Count | Percentage |
|---------|-------|------------|
| Worktree Management Failures | 5 | 31% |
| Daemon Push/Pull Race Conditions | 4 | 25% |
| Configuration Not Honored | 3 | 19% |
| Data Loss During Sync | 2 | 13% |
| Hook/Authentication Blocking | 2 | 13% |
| State Reconstruction Failures | 1 | 6% |

*Note: Percentages exceed 100% because some issues map to multiple patterns (e.g., #785 appears in both Pattern 1 and Pattern 2).*

---

## Recommendations for Recovery Documentation

### Priority Patterns for Epic 2 v2.0
1. **Pattern 3: Data Loss During Sync** - Most user-impacting, requires immediate recovery guidance
2. **Pattern 2: Daemon Race Conditions** - Affects multi-user workflows, complex diagnosis
3. **Pattern 1: Worktree Failures** - Most frequent, multiple manifestations
4. **Pattern 4: Config Not Honored** - Confusing user experience, hard to diagnose

### Suggested Documentation Structure
1. **Quick Reference Card:** Common sync errors → immediate solutions
2. **Decision Tree:** "My sync is failing" → branching diagnosis by error message
3. **Recovery Runbook:** Step-by-step procedures for each pattern
4. **Prevention Guide:** Best practices for multi-clone setups

### Cross-references to Other Research
- **Database Corruption (Story 2.1):** Pattern 6 overlaps with database state issues → `database-corruption-patterns.md`
- **Merge Conflicts (Story 2.2):** Pattern 3 shares root causes with merge logic issues → `merge-conflicts-patterns.md`
- **Circular Dependencies (Story 2.3):** Independent patterns, no overlap → `circular-dependencies-patterns.md`

### Key Commands for Recovery Documentation

```bash
# Essential diagnostic commands
bd daemons status
bd config get sync.branch
bd config get sync.remote
git worktree list
bd stats

# Common recovery sequence
bd daemons killall
git worktree prune
bd sync --no-daemon

# Nuclear option (rebuild from JSONL)
rm .beads/beads.db*
bd sync --import-only

# Nuclear option (rebuild from remote)
rm -rf .beads/
bd init --prefix <prefix>
bd sync --import-only
```

---

## Appendix: Issues Analyzed

| Issue # | Title | Severity | Pattern(s) |
|---------|-------|----------|------------|
| [#785](https://github.com/steveyegge/beads/issues/785) | Sync fails across worktrees in bare repo | High | 1, 2 |
| [#694](https://github.com/steveyegge/beads/issues/694) | Daemon push fails when remote newer | Critical | 2 |
| [#746](https://github.com/steveyegge/beads/issues/746) | bd sync commits regressing changes | Critical | 3 |
| [#695](https://github.com/steveyegge/beads/issues/695) | Daemon doesn't pull remote updates | High | 2 |
| [#609](https://github.com/steveyegge/beads/issues/609) | Daemon worktree registration error | High | 1 |
| [#647](https://github.com/steveyegge/beads/issues/647) | bd sync frozen (GitHub auth) | Medium | 5 |
| [#519](https://github.com/steveyegge/beads/issues/519) | Sync fails when sync.branch checked out | High | 1 |
| [#520](https://github.com/steveyegge/beads/issues/520) | Daemon fails with pre-commit hooks | High | 5, 6 |
| [#532](https://github.com/steveyegge/beads/issues/532) | Circular bd sync error | Medium | 5 |
| [#639](https://github.com/steveyegge/beads/issues/639) | Worktree creation fails in bare repo | High | 1 |
| [#686](https://github.com/steveyegge/beads/issues/686) | Prefix mismatch in multi-repo | High | 4 |
| [#693](https://github.com/steveyegge/beads/issues/693) | Daemon excludes tombstones | High | 2 |
| [#736](https://github.com/steveyegge/beads/issues/736) | sync.remote config ignored | Medium | 4 |
| [#464](https://github.com/steveyegge/beads/issues/464) | Beads deletes issues | Critical | 3 |
| [#546](https://github.com/steveyegge/beads/issues/546) | bd sync fails in no-db mode | High | 4 |
| [#710](https://github.com/steveyegge/beads/issues/710) | Worktree health check error | High | 1 |

---

*Analysis Date: 2025-12-30*
*Beads Version Range: v0.29.0 - v0.41.0*
*Total GitHub Issues in Repository: 363+ closed*

# Database Corruption Patterns Analysis

## Executive Summary

- **Total issues analyzed:** 14
- **Patterns identified:** 7 distinct patterns
- **Most common severity:** High (6 issues)
- **Critical issues:** 3 (data loss scenarios)

This analysis documents database corruption patterns extracted from the [steveyegge/beads](https://github.com/steveyegge/beads) GitHub repository. Patterns are categorized by root cause, frequency, and severity to inform recovery documentation.

## Methodology

### Search Queries Used
- `is:issue database` - 12 results
- `is:issue SQLite` - 12 results
- `is:issue corruption` - 5 results
- `is:issue "database locked"` - 2 results
- `is:issue beads.db` - 12 results
- `is:issue .beads` - 12 results
- `is:issue migrate` - 12 results
- `is:issue fail database` - 9 results

### Date Range
- Issues from December 2025 (v0.29.0 - v0.41.0)

### Selection Criteria
- Issues directly related to database state, corruption, data loss, or migration failures
- Minimum impact: workflow blocked or data integrity compromised
- Excluded: Feature requests, UI issues, documentation-only issues

### Format Note

This is a **research analysis document**, not a production recovery runbook. The pattern format includes additional fields (Frequency, Severity, Root Cause) for analytical purposes. When migrating patterns to `website/docs/recovery/`, convert to the Architecture.md Recovery Section Format:

```markdown
## Recovery: [Problem Name]
### Symptoms
### Diagnosis
### Solution
### Prevention
```

### Quick Diagnostic Commands

Before diving into specific patterns, use these commands to identify your issue:

```bash
# Check database health
bd doctor

# Check for daemon issues
bd daemons status

# Check database file exists
ls -la .beads/beads.db*

# Check JSONL integrity
wc -l .beads/issues.jsonl
head -1 .beads/issues.jsonl | jq .

# Check for merge conflict markers
grep -E "^(<<<<<<<|=======|>>>>>>>)" .beads/*.jsonl

# Check deletions manifest
wc -l .beads/deletions.jsonl
```

---

## Pattern Catalog

### Pattern 1: Validation Catch-22 (Database Unopenable)

#### Symptoms
- All `bd` commands fail with validation errors
- Error: `failed to open database: post-migration validation failed`
- Error: `migration invariants failed: found X orphaned dependencies`
- `bd doctor --fix`, `bd migrate`, `bd repair-deps` all fail identically
- No actionable recovery guidance in error messages

#### Root Cause
Every command requires successful database opening before executing. Validation runs unconditionally with no bypass mechanism. When validation fails, users cannot access any recovery tools.

#### Frequency
1 of 14 issues (7%) - but affects ALL users who encounter database corruption

#### Severity: Critical

#### Solutions
1. **Proposed (PR #805):** New flags `--force` and `--source=<auto|jsonl|db>`
   ```bash
   bd doctor --fix --force --source=jsonl
   ```
2. **Current workaround:** Manual database deletion and rebuild from JSONL
   ```bash
   rm .beads/beads.db*
   bd sync --import-only
   ```

#### Prevention
- Regular JSONL backups (JSONL is the recovery source of truth)
- Monitor `bd doctor` output for early warning signs

#### Reference
- [#806](https://github.com/steveyegge/beads/issues/806) - Improve bd doctor to handle unopenable databases

---

### Pattern 2: Migration Schema Mismatch

#### Symptoms
- Error: `table issues_new has X columns but Y values were supplied`
- Error: `no such column: <column_name>`
- Error: `duplicate column name: <column_name>`
- Failures during `bd init` or after version upgrades
- Test failures in `cmd/bd/` module

#### Root Cause
Database migrations contain schema definitions that don't match the current `schema.go`. Missing columns, extra columns, or timing issues during migration cause SQLite errors.

#### Frequency
3 of 14 issues (21%)

#### Severity: High

#### Solutions
1. **For missing column errors after upgrade:**
   ```bash
   bd daemons killall
   bd list  # retry command
   ```

2. **For fresh init failures:**
   ```bash
   # Update to latest version
   git pull origin main
   go install ./cmd/bd
   bd init --prefix <prefix>
   ```

3. **For parallel execution conflicts:**
   ```bash
   # Serialize bd commands, avoid running multiple simultaneously
   bd list --no-daemon
   ```

#### Prevention
- Upgrade beads CLI before running commands on existing databases
- Avoid running multiple `bd` commands in parallel during migrations
- Fixed in v0.35.0+: Migrations wrapped in `BEGIN EXCLUSIVE` transaction

#### References
- [#757](https://github.com/steveyegge/beads/issues/757) - tombstone_closed_at column mismatch
- [#720](https://github.com/steveyegge/beads/issues/720) - Parallel execution migration error
- [#669](https://github.com/steveyegge/beads/issues/669) - no such column: replies_to

---

### Pattern 3: Tombstone/Manifest Corruption

#### Symptoms
- Thousands of issues marked for deletion incorrectly
- Skip messages: `Skipping bd-xxxx (in deletions manifest: deleted by bd-doctor-hydrate)`
- Error: `skipping corrupt line X in deletions manifest: invalid character '<'`
- 3-way merge failures stating database may be inconsistent
- Legitimate issues disappear after sync

#### Root Cause
Two distinct causes:
1. **Migration bug:** `bd migrate-tombstones` excluded tombstones from current ID set, causing migrated issues to appear "missing" and be re-added to deletions manifest
2. **Merge conflict markers:** Git merge conflicts committed to `deletions.jsonl` cause JSON parsing failures

#### Frequency
2 of 14 issues (14%)

#### Severity: Critical (permanent data loss possible)

#### Solutions
1. **For tombstone migration corruption (fixed in v0.30.2+):**
   ```bash
   # Check deletions.jsonl for incorrectly deleted issues
   cat .beads/deletions.jsonl | grep <issue-id>
   # Manual recovery requires editing deletions.jsonl
   ```

2. **For merge conflict markers:**
   ```bash
   # Check for conflict markers
   grep -E "^(<<<<<<<|=======|>>>>>>>)" .beads/deletions.jsonl
   # Remove conflict lines manually, then:
   bd sync --import-only
   ```

#### Prevention
- Never commit merge conflict markers to `.beads/` files
- Add pre-commit hook to detect conflict markers in JSONL files
- Regular backups of `.beads/` directory before major operations

#### References
- [#552](https://github.com/steveyegge/beads/issues/552) - CRITICAL: bd migrate-tombstones corrupts deletions manifest
- [#590](https://github.com/steveyegge/beads/issues/590) - bd reset & init on Main branch is not clean

---

### Pattern 4: Daemon Mode Store Initialization Failures

#### Symptoms
- Error: `database not initialized`
- Error: `no database connection`
- Commands work with `--no-daemon` but fail normally
- `bd doctor` reports healthy database status
- Specific commands fail while others work (e.g., `bd graph` fails, `bd list` works)

#### Root Cause
Some commands bypass daemon RPC and attempt direct store access. In daemon mode, the global `store` variable is `nil` client-side (store management is server-side). Commands that don't implement fallback logic fail.

#### Frequency
3 of 14 issues (21%)

#### Severity: Medium (workflow blocked, workaround available)

#### Solutions
1. **Immediate workaround:**
   ```bash
   bd --no-daemon <command>
   # or
   bd <command> --sandbox
   ```

2. **Kill and restart daemon:**
   ```bash
   bd daemons killall
   bd <command>
   ```

3. **For batch file creation:**
   ```bash
   bd create -f issues.md --no-daemon
   ```

#### Prevention
- Fixed in various versions (v0.35.0+): Commands now implement direct-storage fallback
- Report new daemon mode failures as bugs

#### References
- [#719](https://github.com/steveyegge/beads/issues/719) - bd create -f fails with 'database not initialized'
- [#751](https://github.com/steveyegge/beads/issues/751) - bd graph fails when daemon is running
- [#669](https://github.com/steveyegge/beads/issues/669) - no such column after upgrade (daemon cache)

---

### Pattern 5: Multi-Machine Sync Data Loss

#### Symptoms
- Issues mysteriously disappear after syncing
- Error: `DATABASE MISMATCH DETECTED! This database belongs to a different repository`
- Issues marked as "recovered from git history (pruned from manifest)"
- Local JSONL with fewer issues overwrites remote JSONL
- Daemon pulls and overwrites before local changes are pushed

#### Root Cause
Multiple causes in multi-machine setups:
1. **Fresh clone overwrite:** 3-way merge logic deletes issues when local has fewer
2. **Daemon sync race condition:** Daemon pulls before unpushed changes are synced
3. **Repo ID mismatch:** Triggers `git-history-backfill` which silently deletes open issues

#### Frequency
2 of 14 issues (14%)

#### Severity: Critical (data loss)

#### Solutions
1. **For repo ID mismatch:**
   ```bash
   bd migrate --update-repo-id
   ```

2. **Stop daemon during issue creation:**
   ```bash
   pkill -f "bd daemon"
   bd create "New issue"
   bd sync
   bd daemon --start
   ```

3. **Recover from deletions manifest:**
   ```bash
   # Check deletions.jsonl for accidentally deleted issues
   cat .beads/deletions.jsonl
   # Issues can potentially be recovered from git history
   ```

4. **Bootstrap from sync-branch on fresh clone:**
   ```bash
   # Fixed in v0.30.0+: bd init reads from origin/sync-branch first
   bd init --prefix <prefix>
   ```

#### Prevention
- Push local changes before pulling remote changes
- Avoid creating issues while daemon is syncing
- Use `bd sync --import-only` on fresh clones before creating issues
- Fixed in PR #564: Merge instead of overwrite logic

#### References
- [#464](https://github.com/steveyegge/beads/issues/464) - Beads deletes issues
- [#746](https://github.com/steveyegge/beads/issues/746) - bd sync commits regressing source code changes

---

### Pattern 6: Hierarchical ID Collision/Parsing

#### Symptoms
- `bd create --parent` returns existing child ID instead of new one
- Warning: `UNIQUE constraint failed: dependencies.issue_id`
- Error: `parent issue does not exist` when parent clearly exists
- Child IDs restart at `.1` instead of continuing sequence

#### Root Cause
Two distinct issues:
1. **Counter not backfilled:** `child_counters` table created without backfilling from existing IDs
2. **ID parsing bug:** Dots in repository names (e.g., `example.com-xxx`) parsed as hierarchy delimiters

#### Frequency
2 of 14 issues (14%)

#### Severity: High (data integrity, duplicate IDs)

#### Solutions
1. **For ID collision (fixed in v0.36.0+):**
   ```bash
   # Update to latest version
   git pull && go install ./cmd/bd
   ```

2. **For parent parsing errors:**
   ```bash
   # Workaround: Create child independently, then link
   bd create "Child issue" -t task
   bd dep add <child-id> <parent-id> --type parent-child
   ```

3. **For existing collision:**
   ```bash
   # Manually rename colliding issue
   bd update <id> --id <new-unique-id>
   ```

#### Prevention
- Use latest beads version (v0.36.0+)
- Avoid dots in repository prefixes when possible
- Fixed: Counter updated on explicit child creation

#### References
- [#728](https://github.com/steveyegge/beads/issues/728) - bd create --parent allocates colliding child IDs
- [#664](https://github.com/steveyegge/beads/issues/664) - bd create --parent fails with 'parent issue does not exist'

---

### Pattern 7: JSONL-Only Mode Broken

#### Symptoms
- Error: `no beads database found` when JSONL files exist
- `bd init --no-db` works, subsequent commands fail
- `bd list` fails after successful `bd create`

#### Root Cause
`findDatabaseInBeadsDir()` function exclusively searches for `.db` files. When SQLite database doesn't exist, function returns empty string despite valid JSONL files.

#### Frequency
1 of 14 issues (7%)

#### Severity: High (feature completely unusable)

#### Solutions
1. **Use SQLite mode instead:**
   ```bash
   bd init --prefix <prefix>  # Default SQLite mode
   ```

2. **For existing --no-db projects:**
   ```bash
   # Convert to SQLite
   bd sync --import-only
   ```

#### Prevention
- Avoid `--no-db` mode until fixed
- Fixed in later versions: Database discovery checks for JSONL fallback

#### References
- [#534](https://github.com/steveyegge/beads/issues/534) - CRITICAL: --no-db mode broken

---

## Severity Distribution

| Severity | Count | Percentage | Description |
|----------|-------|------------|-------------|
| Critical | 3 | 21% | Data loss, unrecoverable without backup |
| High | 6 | 43% | Workflow blocked, requires manual intervention |
| Medium | 4 | 29% | Inconvenient, workaround available |
| Low | 1 | 7% | Minor impact, cosmetic |

## Pattern Frequency Summary

| Pattern | Count | Percentage |
|---------|-------|------------|
| Migration Schema Mismatch | 3 | 21% |
| Daemon Mode Failures | 3 | 21% |
| Tombstone/Manifest Corruption | 2 | 14% |
| Multi-Machine Sync Data Loss | 2 | 14% |
| Hierarchical ID Issues | 2 | 14% |
| Validation Catch-22 | 1 | 7% |
| JSONL-Only Mode Broken | 1 | 7% |

---

## Recommendations for Recovery Documentation

### Priority Patterns for Epic 2 v2.0
1. **Pattern 5: Multi-Machine Sync Data Loss** - Most user-impacting, complex recovery
2. **Pattern 3: Tombstone/Manifest Corruption** - Critical severity, manual recovery needed
3. **Pattern 1: Validation Catch-22** - Blocks all recovery attempts
4. **Pattern 2: Migration Schema Mismatch** - Common after upgrades

### Suggested Documentation Structure
1. **Quick Reference Card:** Common errors → immediate solutions
2. **Decision Tree:** "My database won't open" → branching diagnosis
3. **Recovery Runbook:** Step-by-step procedures for each pattern
4. **Prevention Guide:** Best practices to avoid corruption

### Cross-references to Story 2.5 Synthesis
- Patterns 3 and 5 share root cause: sync/merge logic issues
- Patterns 2 and 4 share root cause: daemon/direct mode inconsistencies
- Pattern 1 represents meta-problem: recovery tools themselves fail

### Key Commands for Recovery Documentation
```bash
# Essential diagnostic commands
bd doctor
bd doctor --fix
bd daemons killall
bd sync --import-only

# Emergency recovery
rm .beads/beads.db*
bd sync --import-only

# Multi-machine safety
bd migrate --update-repo-id
bd sync --import-only  # Before creating issues on fresh clone
```

---

## Appendix: Issues Analyzed

| Issue # | Title | Severity | Pattern(s) |
|---------|-------|----------|------------|
| [#806](https://github.com/steveyegge/beads/issues/806) | bd doctor unopenable databases | Critical | 1 |
| [#804](https://github.com/steveyegge/beads/issues/804) | Read operations write to database | Low | - |
| [#757](https://github.com/steveyegge/beads/issues/757) | Migration tombstone_closed_at fails | High | 2 |
| [#720](https://github.com/steveyegge/beads/issues/720) | Parallel execution migration error | High | 2 |
| [#719](https://github.com/steveyegge/beads/issues/719) | bd create -f database not initialized | Medium | 4 |
| [#751](https://github.com/steveyegge/beads/issues/751) | bd graph fails daemon running | Medium | 4 |
| [#552](https://github.com/steveyegge/beads/issues/552) | CRITICAL: migrate-tombstones corrupts | Critical | 3 |
| [#590](https://github.com/steveyegge/beads/issues/590) | bd reset & init not clean | High | 3 |
| [#464](https://github.com/steveyegge/beads/issues/464) | Beads deletes issues | Critical | 5 |
| [#746](https://github.com/steveyegge/beads/issues/746) | bd sync regressing changes | High | 5 |
| [#728](https://github.com/steveyegge/beads/issues/728) | Colliding child IDs | High | 6 |
| [#664](https://github.com/steveyegge/beads/issues/664) | Parent does not exist error | Medium | 6 |
| [#534](https://github.com/steveyegge/beads/issues/534) | CRITICAL: --no-db mode broken | High | 7 |
| [#669](https://github.com/steveyegge/beads/issues/669) | no such column: replies_to | Medium | 2, 4 |

---

*Analysis Date: 2025-12-30*
*Beads Version Range: v0.29.0 - v0.41.0*
*Total GitHub Issues in Repository: 363+ closed*

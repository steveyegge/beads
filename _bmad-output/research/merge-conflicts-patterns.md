# Merge Conflicts Patterns Analysis

## Executive Summary

- **Total issues analyzed:** 16
- **Patterns identified:** 6 major patterns
- **Most common conflict type:** Sync Branch Worktree Conflicts (5 issues)
- **Multi-agent scenarios found:** 4 issues explicitly document multi-agent conflicts
- **Date range:** October 2025 - December 2025
- **Repository:** steveyegge/beads

## Methodology

### Search Queries Used
- `is:issue merge conflict`
- `is:issue JSONL`
- `is:issue sync conflict`
- `is:issue git merge`
- `is:issue rebase`
- `is:issue worktree`
- `is:issue concurrent`
- `is:issue multi-agent`
- `is:issue database locked`

### Selection Criteria
1. Issues mentioning merge conflicts, sync failures, or concurrent access
2. Issues describing JSONL-specific problems
3. Issues involving multi-agent or multi-clone workflows
4. Issues with clear symptoms, causes, and resolutions

## Conflict Type Taxonomy

> **Note:** Issues may belong to multiple conflict types. Total assignments (18) exceeds issue count (16) due to overlapping classifications.

| Type | Description | Count |
|------|-------------|-------|
| Sync Branch Worktree | Worktree path resolution and lifecycle issues | 5 |
| Multi-Agent Concurrent Access | Multiple agents/clones accessing same data | 4 |
| Database Migration Race | Parallel processes triggering migrations | 2 |
| JSONL Merge/Overwrite | JSON Lines file conflicts and overwrites | 3 |
| Daemon Push/Pull Failures | Background sync failures | 2 |
| Configuration Conflicts | Gitignore, excludes, and config mismatches | 2 |

## Pattern Catalog

### Pattern 1: Sync Branch Worktree Path Resolution Failure

**Severity:** High
**Frequency:** 5 of 16 issues (31%)
**Related Issues:** [#785](https://github.com/steveyegge/beads/issues/785), [#639](https://github.com/steveyegge/beads/issues/639), [#609](https://github.com/steveyegge/beads/issues/609), [#807](https://github.com/steveyegge/beads/issues/807), [#570](https://github.com/steveyegge/beads/issues/570)

#### Symptoms & Error Messages
- `ERROR: no issue found matching "<id>"` - Issues invisible across worktrees
- `fatal: '<path>' is a missing but already registered worktree`
- `fatal: not a git repository (or any of the parent directories): .git`
- `fatal: 'main' is already checked out at '/Users/.../.git/beads-worktrees/main'`

#### Triggering Workflow
1. User sets up bare repository with multiple worktrees
2. Creates issues in one worktree
3. Runs `bd sync` to synchronize
4. Attempts to access issues from different worktree
5. Path resolution fails due to non-standard repository structure

#### Root Cause Analysis
- `jsonlRelPath` calculated relative to project root generates paths like `"main/.beads/issues.jsonl"` instead of `".beads/issues.jsonl"`
- Worktree lifecycle management deletes contents but git registration persists
- `.git` becomes a file (pointer) in worktrees rather than directory

#### Diagnosis
```bash
# Check if in worktree vs regular repo
$ git rev-parse --is-inside-work-tree
$ git rev-parse --git-common-dir
# Verify worktree registration
$ git worktree list
# Check beads path resolution
$ bd doctor
```

#### Resolution Strategy
1. Use `git rev-parse --git-common-dir` for dynamic path detection
2. Add `-f` (force) flag to worktree creation commands
3. Implement `normalizeBeadsRelPath()` function for consistent path handling

#### bd sync Options Used
- Standard `bd sync` fails in bare repo setups
- No specific flags resolve this; requires code fixes

#### Prevention
- Use `bd doctor` to validate worktree configuration
- Prefer non-nested worktree layouts with explicit `BEADS_DIR` configuration
- Mount `.beads` directory from sync branch worktree for container agents

---

### Pattern 2: Multi-Clone Push Rejection Race Condition

**Severity:** High
**Frequency:** 2 of 16 issues (12%)
**Related Issues:** [#694](https://github.com/steveyegge/beads/issues/694), [#746](https://github.com/steveyegge/beads/issues/746)

#### Symptoms & Error Messages
- `! [rejected] beads-metadata -> beads-metadata (fetch first)`
- `error: failed to push some refs to 'github.com:user/repo.git'`
- `git push failed from worktree: exit status 1`
- Source code changes regressing after `bd sync`

#### Triggering Workflow
1. Clone A pushes commit X to sync branch
2. Clone B has uncommitted work (commit Y)
3. Clone B's daemon attempts push
4. Push rejected - remote has newer commits
5. Daemon logs error and stops without recovery
6. Clone B's changes remain local, never syncing

#### Root Cause Analysis
- `gitPushFromWorktree()` lacks push-rejection handling
- Code immediately returns error without fetch-rebase-retry logic
- Multi-machine synchronization creates race conditions

#### Diagnosis
```bash
# Check daemon logs for push failures
$ tail -50 ~/.beads/daemon.log | grep -i "push\|reject"
# Verify sync branch status
$ cd .git/beads-worktrees/<branch> && git status
# Check remote vs local
$ git log --oneline origin/<branch>..<branch>
```

#### Resolution Strategy
1. Detect push rejection errors
2. Execute `git fetch` for latest remote sync branch
3. Rebase local commits onto remote branch
4. Retry push operation

```bash
# Manual recovery
cd .git/beads-worktrees/<branch>
git fetch origin
git rebase origin/<branch>
git push
```

#### bd sync Options Used
- `bd sync` alone doesn't recover from push rejections
- Requires manual intervention or daemon restart

#### Prevention
- Run `bd sync` before making changes
- Avoid concurrent pushes from multiple clones
- Implement locking mechanisms for critical operations

---

### Pattern 3: Parallel Database Migration Race

**Severity:** Critical
**Frequency:** 3 of 16 issues (19%)
**Related Issues:** [#720](https://github.com/steveyegge/beads/issues/720), [#607](https://github.com/steveyegge/beads/issues/607), [#536](https://github.com/steveyegge/beads/issues/536)

#### Symptoms & Error Messages
- `Error: failed to open database: migration messaging_fields failed: failed to add relates_to column: sqlite3: SQL logic error: duplicate column name`
- `sql: database is closed`
- Intermittent failures under concurrent load

#### Triggering Workflow
1. Multiple `bd` commands executed simultaneously
2. Both processes check column existence (returns false)
3. First process adds column (succeeds)
4. Second process adds column (fails - duplicate)

**Reproduction:**
```bash
bd list --status=closed --limit=5 & bd list --status=open --limit=5 & bd list --status=pending --limit=5
```

#### Root Cause Analysis
- Race condition in database migrations
- `reconnectMu` mutex guards only reconnection, not concurrent database access
- Window exists where Operation A queries while Operation B closes database

#### Diagnosis
```bash
# Check for parallel bd processes
$ pgrep -a bd
# Verify database lock status
$ sqlite3 .beads/beads.db "PRAGMA locking_mode;"
# Check for migration errors in logs
$ bd doctor 2>&1 | grep -i "migration\|duplicate"
```

#### Resolution Strategy
1. Wrap `RunMigrations` in `BEGIN EXCLUSIVE` transactions
2. Disable foreign keys before transaction initialization
3. Convert nested transactions to SAVEPOINTs
4. Convert mutex to RWLock (read locks for queries, write lock for reconnection)

#### bd sync Options Used
- Not directly applicable; database-level issue

#### Prevention
- Avoid running multiple `bd` commands in parallel
- Use `--no-db` mode for parallel read-only operations
- Configure `--lock-timeout 0` for immediate failure instead of waiting

---

### Pattern 4: Fresh Clone JSONL Overwrite Data Loss

**Severity:** Critical
**Frequency:** 2 of 16 issues (12%)
**Related Issues:** [#464](https://github.com/steveyegge/beads/issues/464), [#158](https://github.com/steveyegge/beads/issues/158)

#### Symptoms & Error Messages
- "I can replicate by running `bd create`...After a `bd sync` however, the issue has disappeared"
- Issues disappearing after daemon sync
- Multiple prefixes appearing in `issues.jsonl` simultaneously
- JSONL corruption from concurrent container access

#### Triggering Workflow
1. Clone repository with configured sync branch
2. Run `bd init` and `bd doctor`
3. Create new issues locally
4. Run `bd sync`
5. Sync overwrites local JSONL with remote version
6. Newly created issues lost

#### Root Cause Analysis
- Syncing JSONL to worktree overwrote files instead of merging
- Fresh clone with empty database loses remote issues during sync
- `git-history-backfill` silently deleted valid open issues
- Daemon sync overwrote unpushed local changes

#### Diagnosis
```bash
# Compare local vs remote issue count
$ bd list --count
$ git show origin/<sync-branch>:.beads/issues.jsonl | wc -l
# Check for recent sync activity
$ git log --oneline -5 origin/<sync-branch>
# Verify database vs JSONL consistency
$ bd doctor
```

#### Resolution Strategy
1. Implement comparison logic: if local has fewer issues than worktree, trigger 3-way merge
2. Bootstrap from sync branch first during initialization
3. Add detection for external database file replacement

```bash
# Recovery if issues lost
git checkout origin/<sync-branch> -- .beads/issues.jsonl
bd sync --force-rebuild
```

#### bd sync Options Used
- `--force-rebuild` - Rebuilds database from JSONL
- Standard sync without options caused data loss

#### Prevention
- Always run `bd sync` before creating new issues in fresh clone
- Verify sync branch contains expected issues before making changes
- Use `bd doctor` to validate database state

---

### Pattern 5: Multi-Agent Compaction Conflicts

**Severity:** Medium
**Frequency:** 3 of 16 issues (19%)
**Related Issues:** [#650](https://github.com/steveyegge/beads/issues/650), [#747](https://github.com/steveyegge/beads/issues/747), [#158](https://github.com/steveyegge/beads/issues/158)

#### Symptoms & Error Messages
- Multiple issues marked `in_progress` simultaneously create conflicts
- "Everything starts to become a mess" after compaction
- Issues closing prematurely before work completion
- Agents "get confused and cautious when they notice other changes"

#### Triggering Workflow
1. Launch multiple subagents to parallelize epic work
2. Each agent marks different issues as in_progress
3. Compaction triggers during active agent operations
4. Data inconsistencies arise from concurrent modifications

#### Root Cause Analysis
- Compaction feature designed for single-agent workflows
- No agent ownership tracking for issues
- Concurrent modifications to shared JSONL files
- JSONL representation poorly suited for conflict resolution

#### Diagnosis
```bash
# Check for multiple in_progress issues
$ bd list --status=in_progress
# Verify compaction state
$ bd compact --analyze
# Check for daemon activity
$ ps aux | grep "bd daemon"
```

#### Resolution Strategy
1. Assign each subagent to separate issues (not concurrent work on shared tasks)
2. Avoid `bd compact` during active agent operations
3. Use worktrees with `BEADS_NO_DAEMON=1` for isolation
4. Consider `owned_by` field to claim issues with unique agent IDs

#### bd sync Options Used
- No specific flags help; architectural limitation

#### Prevention
- Configure each agent in separate worktree
- Set `BEADS_NO_DAEMON=1` for agent containers
- Disable compaction in multi-agent setups
- Wait for "Gastown" multi-agent coordination features

---

### Pattern 6: Gitignore and Exclude Configuration Conflicts

**Severity:** Low
**Frequency:** 2 of 16 issues (12%)
**Related Issues:** [#797](https://github.com/steveyegge/beads/issues/797), [#349](https://github.com/steveyegge/beads/issues/349)

#### Symptoms & Error Messages
- Untracked `issues.jsonl` file appearing unexpectedly
- Distracting unstaged changes notifications
- `bd compact --analyze` showing "Error: compact requires SQLite storage" with daemon mode

#### Triggering Workflow
1. Initialize beads with `--branch <sync-branch>`
2. Create worktrees from original repository
3. Execute `bd sync`
4. `.beads/.gitignore` contains `!issues.jsonl` negation
5. Negation overrides `.git/info/exclude` protection rules

#### Root Cause Analysis
- Conflicting gitignore rules between `.beads/.gitignore` and `.git/info/exclude`
- Daemon mode incompatibility with compact command (misleading error)
- Local-only users see "daemon_unsupported" without understanding context

#### Diagnosis
```bash
# Check gitignore conflicts
$ cat .beads/.gitignore
$ cat .git/info/exclude | grep beads
# Verify untracked files
$ git status --porcelain .beads/
# Check daemon mode
$ bd config get daemon
```

#### Resolution Strategy
1. Make `.gitignore` configuration dependent on sync-branch settings
2. Use different templates for direct vs. sync-branch modes
3. Add `--no-daemon` flag when running compact

#### bd sync Options Used
- Not directly applicable; configuration issue

#### Prevention
- Verify `.beads/.gitignore` matches your workflow (sync-branch vs direct)
- Run `bd doctor` to detect configuration mismatches
- Add `--no-daemon` when using compact command

---

## Multi-Agent Collaboration Scenarios

| Scenario | Frequency | Issues | Resolution |
|----------|-----------|--------|------------|
| Multiple AI agents (Claude Code) editing simultaneously | 3 issues | #158, #650, #747 | Separate worktrees per agent, disable daemon, use `--no-db` mode |
| Human + AI agent conflicts | 2 issues | #158, #650 | Explicit ownership tracking, sequential task assignment |
| Multiple clones across machines | 3 issues | #464, #694, #746 | Fetch-rebase-retry logic, sync before changes |
| CI/CD + Human workflow conflicts | 1 issue | #536 | `--lock-timeout 0` for immediate failure, Docker-aware locking |
| Parallel Docker containers | 2 issues | #536, #570 | Mount `.beads` from sync branch worktree, set `BEADS_DIR` |

### Multi-Agent Best Practices

1. **Isolation Strategy:**
   ```bash
   # Per-agent worktree setup
   git worktree add ../agent-1 branch-1
   cd ../agent-1
   export BEADS_DIR=/path/to/sync-branch/.beads
   export BEADS_NO_DAEMON=1
   bd config set no-db true
   ```

2. **Ownership Tracking (Proposed):**
   - Add `owned_by` field to issues
   - Agents claim issues with unique IDs
   - Query scoped results post-compaction

3. **Waiting for Gastown:**
   - Full multi-agent coordination expected late 2025
   - Will include explicit agent handoff mechanisms
   - Native support for concurrent workflows

---

## bd sync Command Reference (from real usage)

| Flag | Effect | When to Use |
|------|--------|-------------|
| `--force-rebuild` | Rebuilds SQLite database from JSONL files | After JSONL corruption or data loss, fresh clone recovery |
| `--dry-run` | Preview changes without applying | Debugging sync issues, verifying expected behavior |
| `--no-daemon` | Bypass daemon for direct operation | When daemon is causing conflicts, compact operations |
| `--no-db` | Use JSONL directly without SQLite | Multi-agent scenarios, Docker containers with locking issues |

### Additional CLI Flags

| Flag | Effect | When to Use |
|------|--------|-------------|
| `--lock-timeout 0` | Immediate failure on database lock | Parallel execution, detecting resource contention |
| `--sandbox` | Skip background goroutines entirely | Isolated testing, preventing side effects |

---

## Severity Distribution

| Severity | Count | Percentage | Description |
|----------|-------|------------|-------------|
| Critical | 2 | 12% | Data loss, JSONL corruption unrecoverable |
| High | 8 | 50% | Workflow blocked, manual intervention required |
| Medium | 4 | 25% | Inconvenient, workaround available |
| Low | 2 | 13% | Cosmetic, minimal impact |

---

## Recommendations for Recovery Documentation

### Priority Patterns for Epic 2 v2.0

1. **Highest Priority:** Pattern 4 (JSONL Overwrite Data Loss) - causes actual data loss
2. **High Priority:** Pattern 3 (Database Migration Race) - causes corruption
3. **High Priority:** Pattern 1 (Worktree Path Resolution) - blocks multi-agent workflows
4. **Medium Priority:** Pattern 2 (Push Rejection Race) - recoverable with manual intervention
5. **Lower Priority:** Patterns 5-6 - workarounds exist

### Suggested Documentation Structure

1. **Quick Recovery Guide** - Common symptoms and immediate fixes
2. **Multi-Agent Setup Guide** - Worktree, daemon, and isolation configuration
3. **Troubleshooting Decision Tree** - Symptom-based navigation to solutions
4. **bd sync Deep Dive** - All flags with use cases and examples

### Cross-References to Story 2.5 Synthesis

- Conflict Type Taxonomy should align with Story 2.1 (Database Corruption) patterns
- Multi-agent scenarios are unique to this story; highlight for synthesis
- Severity distribution provides quantitative data for prioritization
- Resolution strategies map to specific `bd` commands for runbook creation

---

## Appendix: Issues Analyzed

| Issue # | Title | Conflict Type | Severity | Pattern(s) |
|---------|-------|---------------|----------|------------|
| [#746](https://github.com/steveyegge/beads/issues/746) | bd sync commits regressing source code changes | JSONL Merge | High | 2 |
| [#785](https://github.com/steveyegge/beads/issues/785) | Beads sync fails across worktrees in bare repo | Sync Worktree | High | 1 |
| [#158](https://github.com/steveyegge/beads/issues/158) | Beads approach to merge conflicts + RFC | Multi-Agent | High | 4, 5 |
| [#694](https://github.com/steveyegge/beads/issues/694) | Daemon push fails when remote has newer commits | Daemon Push | High | 2 |
| [#747](https://github.com/steveyegge/beads/issues/747) | Setting up multi-agent workflows with worktree | Multi-Agent | Medium | 5 |
| [#536](https://github.com/steveyegge/beads/issues/536) | Locking between systems | Database Lock | High | 3 |
| [#720](https://github.com/steveyegge/beads/issues/720) | Parallel execution fails with migration error | Database Race | Critical | 3 |
| [#607](https://github.com/steveyegge/beads/issues/607) | Race condition in FreshnessChecker | Database Race | High | 3 |
| [#650](https://github.com/steveyegge/beads/issues/650) | Subagents + compaction causing issues | Multi-Agent | Medium | 5 |
| [#570](https://github.com/steveyegge/beads/issues/570) | Non-nested worktree layout | Sync Worktree | Medium | 1 |
| [#349](https://github.com/steveyegge/beads/issues/349) | Compact/daemon/merge documentation | Config Conflict | Low | 6 |
| [#797](https://github.com/steveyegge/beads/issues/797) | Untracked issues.jsonl with sync-branch | Config Conflict | Low | 6 |
| [#464](https://github.com/steveyegge/beads/issues/464) | Beads deletes issues | JSONL Overwrite | Critical | 4 |
| [#639](https://github.com/steveyegge/beads/issues/639) | Sync branch worktree fails in bare repo | Sync Worktree | High | 1 |
| [#807](https://github.com/steveyegge/beads/issues/807) | Beads still creating worktree in main branch | Sync Worktree | Medium | 1 |
| [#609](https://github.com/steveyegge/beads/issues/609) | Daemon auto-sync fails with missing worktree | Sync Worktree | High | 1 |

---

## Document Metadata

- **Generated:** 2025-12-30
- **Story:** 2.2 - GitHub Issues Mining - Merge Conflicts Patterns
- **Epic:** 2 - Recovery Documentation Analysis & Knowledge Gathering
- **Beads ID:** bd-9g9.2
- **Author:** Dev Agent (Claude Opus 4.5)

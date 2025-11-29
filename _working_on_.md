I created a new worktree branch and was stopped even before getting started. Beads is not happy with this setup. I don't know why.


```
main on  main [$] via 🐹 v1.25.4 
❯ git worktree add ../fix-ci -b fix-ci
Preparing worktree (new branch 'fix-ci')
HEAD is now at 4ef5a28a bd sync: 2025-11-28 22:17:18
🔗 Importing beads issues from JSONL...
✓ Beads issues imported successfully

main on  main [$] via 🐹 v1.25.4 
❯ cd ../fix-ci/

fix-ci on  fix-ci [$] via 🐹 v1.25.4 
❯ bd doctor

Diagnostics
 ├ Installation: .beads/ directory found
 ├ Git Hooks: All recommended hooks installed
 │   Installed: post-merge, pre-push, pre-commit
 ├ Database: Unable to read database version ✗
 │   Storage: SQLite
 ├ Schema Compatibility: All required tables and columns present
 ├ Issue IDs: hash-based ✓
 ├ CLI Version: 0.26.0 (latest)
 ├ Database Files: Single database file
 ├ JSONL Files: Using issues.jsonl
 ├ JSONL Config: Using issues.jsonl
 ├ Database Config: Configuration matches existing files
 ├ Daemon Health: No daemon running (will auto-start on next command)
 ├ DB-JSONL Sync: Database and JSONL are in sync
 ├ Permissions: All permissions OK
 ├ Dependency Cycles: No circular dependencies detected
 ├ Claude Integration: Hooks installed (CLI mode)
 │   Plugin not detected - install for slash commands
 ├ bd in PATH: 'bd' command available
 ├ Documentation bd prime: Documentation references match installed features
 │   Files: AGENTS.md
 ├ Agent Documentation: Documentation found: AGENTS.md
 ├ Documentation: No legacy beads slash commands detected
 ├ Gitignore: Up to date
 ├ Git Merge Driver: Correctly configured
 │   bd merge %A %O %A %B
 ├ Metadata Version Tracking: Version tracking active (version: 0.26.0)
 ├ Sync Branch Config: sync.branch not configured ⚠
 │   Current branch: fix-ci
 ├ Deletions Manifest: Present (2474 entries)
 └ Untracked Files: All .beads/*.jsonl files are tracked

✗ Error: Unable to read database version
  Fix: Database may be corrupted. Try 'bd migrate'

⚠ Warning: sync.branch not configured
  Fix: Run 'bd doctor --fix' to auto-configure to 'fix-ci', or manually: bd config set sync.branch <branch-name>

```

I created a new worktree branch and was stopped even before getting started. Beads is not happy with this setup. I don't know why.


Trial #1:

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

Ampcode and I worked on the CI issues for an hour:

CI failing again ...  35m ago       Private           60  T-9da46a22-ecc7-46ee-b102-1ecf639f104f
Sync fix-ci branc...  43m ago       Private           42  T-077d7cf5-a77c-4575-9246-370c5046e569
Working on markdo...  51m ago       Private          199  T-448d37ab-8cba-4a92-94c3-b5539e3c6a7a
Verify readme.md ...  1h ago        Private            6  T-c126c289-68e5-47fa-ba48-7eb2808fca33
Git worktree conf...  1h ago        Private           10  T-187eb77c-462c-468f-bf57-cfa34cd6e022


Then I shifted back to try and understand what's happening with Beads and worktrees. This time I see a different error. So somehow the work in the 1st tree changed things. But what? counting jsonl issues in init.go?

Trial #2

```
❯ git worktree add ../worktree-test -b worktree-test
Preparing worktree (new branch 'worktree-test')
HEAD is now at c8d0624f bd sync: 2025-11-29 01:33:24
Warning: Failed to sync bd changes after checkout
Error: no beads database found
Hint: run 'bd init' to create a database in the current directory
      or set BEADS_DIR to point to your .beads directory
      or set BEADS_DB to point to your database file (deprecated)

Run 'bd doctor --fix' to diagnose and repair
```

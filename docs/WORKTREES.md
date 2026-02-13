# Git Worktrees Guide

**Enhanced Git worktree compatibility for Beads issue tracking**

## Overview

Beads now provides **enhanced Git worktree support** with a shared database architecture. All worktrees in a repository share the same `.beads` database located in the main repository, enabling seamless issue tracking across multiple working directories.

**Note:** While comprehensively implemented and tested internally, this feature may benefit from real-world usage feedback to identify any remaining edge cases.

---

## Beads-Created Worktrees (Sync Branch)

**Important:** Beads automatically creates git worktrees internally for its sync-branch feature. This is different from user-created worktrees for parallel development.

### Why Beads Creates Worktrees

When you configure a **sync branch** (via `bd init --branch <name>` or `bd config set sync.branch <name>`), beads needs to commit issue updates to that branch without switching your working directory away from your current branch.

**Solution:** Beads creates a lightweight worktree that:
- Contains only the `.beads/` directory (sparse checkout)
- Lives in `.git/beads-worktrees/<sync-branch>/`
- Commits issue changes to the sync branch when you run `bd sync --full`
- Leaves your main working directory untouched

### Where to Find These Worktrees

```
your-project/
├── .git/
│   ├── beads-worktrees/          # Beads-created worktrees live here
│   │   └── beads-sync/           # Default sync branch worktree
│   │       └── .beads/
│   │           └── issues.jsonl  # Issue data committed here
│   └── worktrees/                # Standard git worktrees directory
├── .beads/                       # Your working copy
│   ├── beads.db                  # Local SQLite database
│   └── issues.jsonl              # Local JSONL (may differ from sync branch)
└── src/                          # Your code (untouched by sync)
```

**Note:** You will see `.beads/issues.jsonl` in both your working copy and the sync worktree. The working copy is a local mirror for the database; the sync worktree copy is the one committed to the sync branch.

### Common Confusion: "Beads took over main!"

If you see worktrees pointing to `main` and can't switch branches normally, this is likely because:

1. Your sync branch was created from `main`
2. Beads created a worktree for that branch
3. Git worktrees lock branches they're checked out to

**Symptoms:**
```bash
$ git checkout main
fatal: 'main' is already checked out at '/path/to/.git/beads-worktrees/beads-sync'
```

**Quick Fix:**
```bash
# Remove the beads worktree
rm -rf .git/beads-worktrees

# Prune stale worktree references
git worktree prune

# Also remove any stray worktrees in .git/worktrees (older versions)
rm -rf .git/worktrees/beads-*
git worktree prune
```

### Disabling Sync Branch (Remove Worktrees)

If you don't want beads to use a separate sync branch:

```bash
# Unset the sync branch configuration
bd config set sync.branch ""

# Clean up existing worktrees
rm -rf .git/beads-worktrees
git worktree prune
```

### Checking Your Sync Branch Configuration

```bash
# See current sync branch setting
bd config get sync.branch

# Check if worktrees exist
ls -la .git/beads-worktrees/ 2>/dev/null || echo "No beads worktrees"
ls -la .git/worktrees/ 2>/dev/null || echo "No standard worktrees"

# List all git worktrees
git worktree list
```

### See Also

For complete sync-branch documentation, see [PROTECTED_BRANCHES.md](PROTECTED_BRANCHES.md).

---

## How It Works

### Shared Database Architecture

```
Main Repository
├── .git/                    # Shared git directory
├── .beads/                  # Shared database (main repo)
│   ├── beads.db            # SQLite database
│   ├── issues.jsonl        # Issue data (tracked in sync branch if configured)
│   └── config.yaml         # Configuration
├── feature-branch/         # Worktree 1
│   └── (code files only)
└── bugfix-branch/          # Worktree 2
    └── (code files only)
```

**Key points:**
- ✅ **One database** - All worktrees share the same `.beads` directory in main repo
- ✅ **Automatic discovery** - Database found regardless of which worktree you're in
- ✅ **Concurrent access** - SQLite locking prevents corruption
- ✅ **Git integration** - Issues sync via JSONL (tracked in sync branch when configured)

### Worktree Detection & Sync Behavior

bd automatically detects when you're in a git worktree and resolves the main `.beads` directory. The current CLI runs in direct mode (no background daemon).

**Without sync-branch configured:**
- `bd sync` exports JSONL only (no git operations)
- `bd sync --full` commits to your current branch

**With sync-branch configured:**
- `bd sync --full` commits to the sync branch worktree

## Usage Patterns

### Recommended: Use a Sync Branch for Protected Main

```bash
# Configure sync branch once (in main repo or any worktree)
bd config set sync.branch beads-sync

# Commit/push issues to the sync branch
bd sync --full
```

### Direct Commit (No Sync Branch)

```bash
# Commit issues on your current branch
bd sync --full

# Or export only (no git operations)
bd sync
```

## Worktree-Aware Features

### Database Discovery

bd intelligently finds the correct database:

1. **Priority search**: Main repository `.beads` directory first
2. **Fallback logic**: Searches worktree if main repo doesn't have database
3. **Path resolution**: Handles symlinks and relative paths correctly
4. **Validation**: Ensures `.beads` contains actual project files

### Git Hooks Integration

Pre-commit hooks adapt to worktree context:

```bash
# In main repo: Stages JSONL normally
git add .beads/issues.jsonl

# In worktree: Safely skips staging (files outside working tree)
# Hook detects context and handles appropriately
```

### Sync Operations

Worktree-aware sync operations:

- **Repository root detection**: Uses `git rev-parse --show-toplevel` for main repo
- **Git directory handling**: Distinguishes between `.git` (file) and `.git/` (directory)
- **Path resolution**: Converts between worktree and main repo paths
- **Concurrent safety**: SQLite locking prevents corruption

## Setup Examples

### Basic Worktree Setup

```bash
# Create main worktree
git worktree add main-repo

# Create feature worktree
git worktree add feature-worktree

# Initialize beads in main repo
cd main-repo
bd init

# Worktrees automatically share the database
cd ../feature-worktree
bd ready  # Works immediately - sees same issues
```

### Multi-Feature Development

```bash
# Main development
cd main-repo
bd create "Epic: User authentication" -t epic -p 1
# Returns: bd-a3f8e9

# Feature branch worktree
git worktree add auth-feature
cd auth-feature
bd create "Design login UI" -p 1
# Auto-assigned: bd-a3f8e9.1 (child of epic)

# Bugfix worktree
git worktree add auth-bugfix
cd auth-bugfix
bd create "Fix password validation" -t bug -p 0
# Auto-assigned: bd-f14c3
```

## Troubleshooting

### Issue: "Branch already checked out" error

**Symptoms:**
```bash
$ git checkout main
fatal: 'main' is already checked out at '/path/to/.git/beads-worktrees/beads-sync'
```

**Cause:** Beads created a worktree for its sync branch feature, and that worktree has your target branch checked out. Git doesn't allow the same branch to be checked out in multiple worktrees.

**Solution:**
```bash
# Remove beads worktrees
rm -rf .git/beads-worktrees
rm -rf .git/worktrees/beads-*

# Clean up git's worktree registry
git worktree prune

# Now you can checkout the branch
git checkout main
```

**Prevention:** If you use trunk-based development and don't need a separate sync branch, disable it:
```bash
bd config set sync.branch ""
```

### Issue: Unexpected worktree directories appeared

**Symptoms:** You notice `.git/beads-worktrees/` or entries in `.git/worktrees/` that you didn't create.

**Cause:** Beads automatically creates worktrees when using the sync-branch feature (configured via `bd init --branch` or `bd config set sync.branch`).

**Solution:** See [Beads-Created Worktrees](#beads-created-worktrees-sync-branch) section above for details on what these are and how to remove them if unwanted.

### Issue: Sync changes appear on the wrong branch

**Symptoms:** Issues show up on a branch you didn't expect

**Solution:** Ensure a dedicated sync branch is configured and re-run sync.

```bash
bd config set sync.branch beads-sync
bd sync --full --no-pull
```

### Issue: Database not found in worktree

**Symptoms:** `bd: database not found` error

**Solutions:**
```bash
# Ensure main repo has .beads directory
cd main-repo
ls -la .beads/

# Re-run bd init if needed
bd init

# Check worktree can access main repo
cd ../worktree-name
bd info  # Should show database path in main repo
```

### Issue: Multiple databases detected

**Symptoms:** Warning about multiple `.beads` directories

**Solution:**
```bash
# bd shows warning with database locations
# Typically, the closest database (in main repo) is correct
# Remove extra .beads directories if they're not needed
```

### Issue: Git hooks fail in worktrees

**Symptoms:** Pre-commit hook errors about staging files outside working tree

**Solution:** This is now automatically handled. The hook detects worktree context and adapts its behavior. No manual intervention needed.

## Advanced Configuration

### Environment Variables

```bash
# Force specific database location
export BEADS_DB=/path/to/specific/.beads/beads.db
```

### Configuration Options

```bash
# Configure sync behavior
bd config set sync.branch beads-sync  # Use separate sync branch

# Use a dedicated sync branch for protected main branches
bd config set sync.branch beads-sync
```

## Performance Considerations

### Database Sharing Benefits

- **Reduced overhead**: One database instead of per-worktree copies
- **Instant sync**: Changes visible across all worktrees immediately
- **Memory efficient**: Single SQLite instance vs multiple
- **Git efficient**: One JSONL file to track vs multiple

### Concurrent Access

- **SQLite locking**: Prevents corruption during simultaneous access
- **Git operations**: Safe concurrent commits from different worktrees
- **Sync coordination**: JSONL-based sync prevents conflicts

## Migration from Limited Support

### Before (Limited Worktree Support)

- ❌ Sync branch conflicts in worktrees
- ❌ Manual workarounds required
- ❌ Complex setup procedures
- ❌ Limited documentation

### After (Enhanced Worktree Support)

- ✅ Shared database architecture
- ✅ Automatic worktree detection
- ✅ Clear user guidance and warnings
- ✅ Comprehensive documentation
- ✅ Git hooks work correctly
- ✅ All bd commands function properly

**Note:** Based on comprehensive internal testing. Real-world usage may reveal additional refinements needed.

## Examples in the Wild

### Monorepo Development

```bash
# Monorepo with multiple service worktrees
git worktree add services/auth
git worktree add services/api
git worktree add services/web

# Each service team works in their worktree
cd services/auth
bd create "Add OAuth support" -t feature -p 1

cd ../api
bd create "Implement auth endpoints" -p 1
# Issues automatically linked and visible across worktrees
```

### Feature Branch Workflow

```bash
# Create feature worktree
git worktree add feature/user-profiles
cd feature/user-profiles

# Work on feature with full issue tracking
bd create "Design user profile schema" -t task -p 1
bd create "Implement profile API" -t task -p 1
bd create "Add profile UI components" -t task -p 2

# Issues tracked in shared database
# Code changes isolated to worktree
# Clean merge back to main when ready
```

## Fully Separate Beads Repository

For users who want complete separation between code history and issue tracking, beads supports storing issues in a completely separate git repository.

### Why Use a Separate Repo?

- **Clean code history** - No beads commits polluting your project's git log
- **Shared across worktrees** - All worktrees can use the same BEADS_DIR
- **Platform agnostic** - Works even if your main project isn't git-based
- **Monorepo friendly** - Single beads repo for multiple projects

### Setup

**Option A: Initialize with BEADS_DIR (simplest)**
```bash
# 1. Create the directory structure
mkdir -p ~/my-project-beads/.beads

# 2. Set BEADS_DIR and initialize from anywhere
export BEADS_DIR=~/my-project-beads/.beads
bd init --prefix myproj    # Creates database at $BEADS_DIR

# 3. Initialize git in the beads repo (optional, for sync)
cd ~/my-project-beads && git init
```

**Option B: Traditional approach**
```bash
# 1. Create a dedicated beads repository (one-time)
mkdir ~/my-project-beads
cd ~/my-project-beads
git init
bd init --prefix myproj

# 2. Add a remote for cross-machine sync (optional)
git remote add origin git@github.com:you/my-project-beads.git
git push -u origin main
```

### Usage

Set `BEADS_DIR` to point at your separate beads repository:

```bash
cd ~/my-project
export BEADS_DIR=~/my-project-beads/.beads

# All bd commands now use the separate repo
bd create "My task" -t task
bd list
bd sync --full  # commits to ~/my-project-beads, pushes there
```

### Making It Permanent

**Option 1: Shell profile**
```bash
# Add to ~/.bashrc or ~/.zshrc
export BEADS_DIR=~/my-project-beads/.beads
```

**Option 2: direnv (per-project)**
```bash
# In ~/my-project/.envrc
export BEADS_DIR=~/my-project-beads/.beads
```

**Option 3: Wrapper script**
```bash
# ~/bin/bd-myproj
#!/bin/bash
BEADS_DIR=~/my-project-beads/.beads exec bd "$@"
```

### How It Works

When `BEADS_DIR` points to a different git repository than your current directory:

1. `bd sync --full` detects "External BEADS_DIR"
2. Git operations (add, commit, push, pull) target the beads repo
3. `bd sync` without `--full` only exports JSONL (no git operations)
4. Your code repository is never touched

This was contributed by @dand-oss in [PR #533](https://github.com/steveyegge/beads/pull/533).

### Combining with Worktrees

This approach elegantly solves the worktree isolation problem:

```bash
# All worktrees share the same external beads repo
export BEADS_DIR=~/project-beads/.beads

cd ~/project/main       && bd list  # Same issues
cd ~/project/feature-1  && bd list  # Same issues
cd ~/project/feature-2  && bd list  # Same issues
```

No daemon conflicts, no branch confusion - all worktrees see the same issues because they all use the same external repository.

## See Also

- [REPO_CONTEXT.md](REPO_CONTEXT.md) - RepoContext API for contributors
- [GIT_INTEGRATION.md](GIT_INTEGRATION.md) - General git integration guide
- [AGENTS.md](../AGENTS.md) - Agent usage instructions
- [README.md](../README.md) - Main project documentation
- [MULTI_REPO_MIGRATION.md](MULTI_REPO_MIGRATION.md) - Multi-workspace patterns
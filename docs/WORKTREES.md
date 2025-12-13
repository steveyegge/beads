# Git Worktrees Guide

**Enhanced Git worktree compatibility for Beads issue tracking**

## Overview

Beads now provides **enhanced Git worktree support** with a shared database architecture. All worktrees in a repository share the same `.beads` database located in the main repository, enabling seamless issue tracking across multiple working directories.

**Note:** While comprehensively implemented and tested internally, this feature may benefit from real-world usage feedback to identify any remaining edge cases.

## How It Works

### Shared Database Architecture

```
Main Repository
├── .git/                    # Shared git directory
├── .beads/                  # Shared database (main repo)
│   ├── beads.db            # SQLite database
│   ├── issues.jsonl        # Issue data (git-tracked)
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
- ✅ **Git integration** - Issues sync via JSONL in main repo

### Worktree Detection & Warnings

bd automatically detects when you're in a git worktree and provides appropriate guidance:

```bash
# In a worktree with daemon active
$ bd ready
╔══════════════════════════════════════════════════════════════════════════╗
║ WARNING: Git worktree detected with daemon mode                         ║
╠══════════════════════════════════════════════════════════════════════════╣
║ Git worktrees share the same .beads directory, which can cause the      ║
║ daemon to commit/push to the wrong branch.                               ║
║                                                                          ║
║ Shared database: /path/to/main/.beads                                    ║
║ Worktree git dir: /path/to/shared/.git                                   ║
║                                                                          ║
║ RECOMMENDED SOLUTIONS:                                                   ║
║   1. Use --no-daemon flag:    bd --no-daemon <command>                   ║
║   2. Disable daemon mode:     export BEADS_NO_DAEMON=1                   ║
╚══════════════════════════════════════════════════════════════════════════╝
```

## Usage Patterns

### Recommended: Direct Mode in Worktrees

```bash
# Disable daemon for worktree usage
export BEADS_NO_DAEMON=1

# Work normally - all commands work correctly
cd feature-worktree
bd create "Implement feature X" -t feature -p 1
bd update bd-a1b2 --status in_progress
bd ready
bd sync  # Manual sync when needed
```

### Alternative: Daemon in Main Repo Only

```bash
# Use daemon only in main repository
cd main-repo
bd ready  # Daemon works here

# Use direct mode in worktrees
cd ../feature-worktree
bd --no-daemon ready
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

### Issue: Daemon commits to wrong branch

**Symptoms:** Changes appear on unexpected branch in git history

**Solution:**
```bash
# Disable daemon in worktrees
export BEADS_NO_DAEMON=1
# Or use --no-daemon flag for individual commands
bd --no-daemon sync
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
# Disable daemon globally for worktree usage
export BEADS_NO_DAEMON=1

# Disable auto-start (still warns if manually started)
export BEADS_AUTO_START_DAEMON=false

# Force specific database location
export BEADS_DB=/path/to/specific/.beads/beads.db
```

### Configuration Options

```bash
# Configure sync behavior
bd config set sync.branch beads-metadata  # Use separate sync branch
bd config set sync.auto_commit true       # Auto-commit changes
bd config set sync.auto_push true         # Auto-push changes
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

- ❌ Daemon mode broken in worktrees
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
export BEADS_NO_DAEMON=1
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

## See Also

- [GIT_INTEGRATION.md](GIT_INTEGRATION.md) - General git integration guide
- [AGENTS.md](../AGENTS.md) - Agent usage instructions
- [README.md](../README.md) - Main project documentation
- [MULTI_REPO_MIGRATION.md](MULTI_REPO_MIGRATION.md) - Multi-workspace patterns
# Repository Context

> **Status**: Draft outline for Phase 7 documentation

This document explains how beads resolves repository context when commands run from
different directories than where `.beads/` lives.

## Problem

Git commands must run in the correct repository, but users may invoke `bd` from:

- A different repository (using `BEADS_DIR` environment variable)
- A git worktree (separate working directory, shared `.beads/`)
- A subdirectory within the repository

Without centralized handling, each command must implement its own path resolution,
leading to bugs when assumptions don't match reality.

## Solution: RepoContext API

The `RepoContext` API provides a single source of truth for repository resolution:

```go
import "github.com/steveyegge/beads/internal/beads"

rc, err := beads.GetRepoContext()
if err != nil {
    return err
}

// Run git in beads repository (not CWD)
cmd := rc.GitCmd(ctx, "status")
output, err := cmd.Output()
```

## When to Use Each Method

| Method | Use Case | Example |
|--------|----------|---------|
| `GitCmd()` | Git commands for beads operations | `git add .beads/`, `git push` |
| `GitCmdCWD()` | Git commands for user's working repo | `git status` (show user's changes) |
| `RelPath()` | Convert absolute path to repo-relative | Display paths in output |

## Scenarios

### Normal Repository

CWD is inside the repository containing `.beads/`:

```
/project/
├── .beads/
├── src/
└── README.md

$ cd /project/src
$ bd sync
# GitCmd() runs in /project (correct)
```

### BEADS_DIR Redirect

User is in one repository but managing beads in another:

```
$ cd /repo-a          # Has uncommitted changes
$ export BEADS_DIR=/repo-b/.beads
$ bd sync
# GitCmd() runs in /repo-b (correct, not /repo-a)
```

### Git Worktree

User is in a worktree but `.beads/` lives in main repository:

```
/project/                    # Main repo
├── .beads/
├── .worktrees/
│   └── feature-branch/      # Worktree (CWD)
└── src/

$ cd /project/.worktrees/feature-branch
$ bd sync
# GitCmd() runs in /project (main repo, where .beads lives)
```

### Combined: Worktree + Redirect

Both worktree and BEADS_DIR can be active simultaneously:

```
$ cd /repo-a/.worktrees/branch-x
$ export BEADS_DIR=/repo-b/.beads
$ bd sync
# GitCmd() runs in /repo-b (BEADS_DIR takes precedence)
```

## RepoContext Fields

| Field | Description |
|-------|-------------|
| `BeadsDir` | Actual `.beads/` directory (after following redirects) |
| `RepoRoot` | Repository root containing `BeadsDir` |
| `CWDRepoRoot` | Repository root containing user's CWD (may differ) |
| `IsRedirected` | True if BEADS_DIR points to different repo than CWD |
| `IsWorktree` | True if CWD is in a git worktree |

## Migration Guide

### Before (scattered resolution)

```go
func doGitOperation(ctx context.Context) error {
    // Each function resolved paths differently
    beadsDir := beads.FindBeadsDir()
    redirectInfo := beads.GetRedirectInfo()
    var repoRoot string
    if redirectInfo.IsRedirected {
        repoRoot = filepath.Dir(beadsDir)
    } else {
        repoRoot = getRepoRootForWorktree(ctx)
    }
    cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "status")
    // ...
}
```

### After (centralized)

```go
func doGitOperation(ctx context.Context) error {
    rc, err := beads.GetRepoContext()
    if err != nil {
        return err
    }
    cmd := rc.GitCmd(ctx, "status")
    // ...
}
```

## Testing

Tests use `beads.ResetCaches()` to clear cached context between test cases:

```go
func TestSomething(t *testing.T) {
    t.Cleanup(func() {
        beads.ResetCaches()
        git.ResetCaches()
    })
    // Test code...
}
```

## Related Documentation

- [WORKTREES.md](WORKTREES.md) - Git worktree integration
- [ROUTING.md](ROUTING.md) - Multi-repository routing
- [CONFIG.md](CONFIG.md) - BEADS_DIR and environment variables

## Implementation Notes

- Result is cached via `sync.Once` for efficiency
- CWD and BEADS_DIR don't change during command execution
- Uses `cmd.Dir` pattern (not `-C` flag) for Go-idiomatic execution

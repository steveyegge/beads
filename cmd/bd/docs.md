# Noridoc: cmd/bd

Path: @/cmd/bd

### Overview

The `cmd/bd` directory contains the main CLI command implementation for the `bd` beads command-line tool. It includes all user-facing commands (init, sync, delete, etc.) and their underlying business logic, as well as git hook installation and management functionality.

### How it fits into the larger codebase

- **Entry point**: `main.go` initializes the Cobra CLI framework and sets up the root command
- **Command structure**: Each major feature has a corresponding file (init.go, sync.go, hooks.go, etc.) with both CLI command definitions and business logic
- **Data access**: Interacts with @/internal/storage (SQLite/in-memory database) and @/internal/types (core data structures) to perform operations
- **Git integration**: Uses git commands directly (git rev-parse, git config, git init) via exec.Command to detect repository state and manage hooks
- **Output coordination**: Formats and outputs results to stdout/stderr, with support for JSON output via the `jsonOutput` flag
- **Hook management**: Maintains embedded git hooks (pre-commit, post-merge, pre-push, post-checkout) that synchronize beads state with git operations

### Core Implementation

**Test Organization**:
- Hook tests (`hooks_test.go`, `init_hooks_test.go`) verify hook installation, detection, and management
- Tests initialize real git repositories using `git init` rather than mocking `.git` directories
- Uses `getGitDir()` helper function (from `hooks.go`) to properly resolve git directories in both normal repos and git worktrees
- Tests use `t.TempDir()` for isolation and `t.Chdir()` to change working directory during test execution

**Git Directory Resolution** (`hooks.go`):
- `getGitDir()` function returns the actual .git directory path using `git rev-parse --git-dir`
- Critical for worktree support: in normal repos this returns ".git", but in git worktrees .git is a file containing "gitdir: /path/to/actual/git/dir"
- All hook-related operations (install, uninstall, check) use `getGitDir()` to construct paths to the hooks directory

**Hook Installation** (`hooks.go`):
- `installHooks()` writes embedded hook scripts to the git hooks directory or .beads-hooks/ (for shared mode)
- Creates hooks directory with `os.MkdirAll()` if it doesn't exist
- Backs up existing hooks (unless `--force` flag is set) before overwriting
- Sets execute permissions (0755) on installed hooks

**Hook Detection** (`init.go`):
- `detectExistingHooks()` scans for existing hooks and determines their type (bd, pre-commit framework, custom)
- `hooksInstalled()` checks if bd hooks are already installed by verifying file existence and checking for bd signature comments
- Used by `bd init` to decide whether to chain with existing hooks or overwrite them

### Things to Know

**Worktree Awareness**:
- Git worktrees store `.git` as a file (not a directory) that points to the actual git directory
- This breaks code that assumes `.git` is always a directory and tries to create `.git/hooks/` subdirectories
- The fix replaces hardcoded `filepath.Join(tmpDir, ".git", "hooks")` with `filepath.Join(getGitDir(), "hooks")`
- Tests must use real git repos (via `git init`) to properly test worktree-aware code

**Test Directory Creation** (`hooks_test.go`, `init_hooks_test.go`):
- Some tests explicitly create the hooks directory with `os.MkdirAll()` before writing hook files
- This is necessary because `getGitDir()` resolves the path correctly, but the directory may not exist yet
- Two test variants: those that only read hooks (no MkdirAll needed) and those that write hooks (need MkdirAll)

**Test Execution Order**:
1. Create temporary directory with `t.TempDir()`
2. Change working directory with `t.Chdir()`
3. Initialize real git repo with `exec.Command("git", "init")`
4. Call `getGitDir()` to get the actual git directory path
5. Construct hooks path with `filepath.Join(gitDirPath, "hooks")`
6. Create hooks directory if needed with `os.MkdirAll()`
7. Execute hook operations and verify results

**Hook Signature Detection**:
- bd hooks are identified by checking for "bd (beads) pre-commit hook" comment in the content
- This allows bd to distinguish its own hooks from other custom hooks or pre-commit framework hooks
- Used to prevent re-installation of already-installed hooks and to support hook chaining scenarios

Created and maintained by Nori.

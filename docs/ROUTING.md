# Multi-Repo Auto-Routing

This document describes the auto-routing feature that intelligently directs new issues to the appropriate repository based on user role.

## Overview

Auto-routing solves the OSS contributor problem: contributors want to plan work locally without polluting upstream PRs with planning issues. The routing layer automatically detects whether you're a maintainer or contributor and routes `bd create` to the appropriate repository.

## User Role Detection

### Strategy

The routing system detects user role via (in priority order):

1. **Explicit git config** (highest priority):
   ```bash
   git config beads.role maintainer
   # or
   git config beads.role contributor
   ```

2. **Upstream remote detection** (fork signal):
   - If an `upstream` remote exists → Contributor
   - This correctly identifies fork contributors who use SSH for their fork
   - Overrides the SSH/HTTPS heuristic (see below)

3. **Push URL inspection** (fallback heuristic):
   - SSH URLs (`git@github.com:user/repo.git`) → Maintainer
   - HTTPS with credentials → Maintainer
   - HTTPS without credentials → Contributor
   - No remote → Contributor (fallback)

### Examples

```bash
# Maintainer (SSH access, no upstream)
git remote add origin git@github.com:owner/repo.git
bd create "Fix bug" -p 1
# → Creates in current repo (.)

# Contributor (SSH fork with upstream) - Fixes GH#1174
git remote add origin git@github.com:myuser/fork.git   # SSH to your fork
git remote add upstream https://github.com/owner/repo.git
bd create "Fix bug" -p 1
# → Creates in planning repo (upstream remote signals fork/contributor)

# Contributor (HTTPS fork)
git remote add origin https://github.com/fork/repo.git
git remote add upstream https://github.com/owner/repo.git
bd create "Fix bug" -p 1
# → Creates in planning repo (~/.beads-planning by default)
```

### Upstream Remote Detection

The presence of an `upstream` remote is a strong signal that you are working in a forked repository as a contributor. This detection method was added to fix GH#1174, where fork contributors using SSH were incorrectly detected as maintainers.

**Why it works:**
- Maintainers work directly in the canonical repository (no upstream needed)
- Contributors fork the repo and add the original as `upstream` for syncing
- This pattern is standard in OSS workflows (GitHub's fork documentation recommends it)

**Detection priority:**
1. `git config beads.role` (explicit override) - always wins
2. `upstream` remote exists - contributor (even with SSH origin)
3. SSH/HTTPS URL heuristic - fallback when no upstream

## Configuration

Routing is configured via the database config:

```bash
# Set routing mode (auto = detect role, explicit = always use default)
bd config set routing.mode auto

# Set default planning repo
bd config set routing.default "~/.beads-planning"

# Set repo for maintainers (in auto mode)
bd config set routing.maintainer "."

# Set repo for contributors (in auto mode)
bd config set routing.contributor "~/.beads-planning"
```

## CLI Usage

### Auto-Routing

```bash
# Let bd decide based on role
bd create "Fix authentication bug" -p 1

# Maintainer: creates in current repo (.)
# Contributor: creates in ~/.beads-planning
```

### Explicit Override

```bash
# Force creation in specific repo (overrides auto-routing)
bd create "Fix bug" -p 1 --repo /path/to/repo
bd create "Add feature" -p 1 --repo ~/my-planning
```

## Discovered Issue Inheritance

Issues created with `discovered-from` dependencies automatically inherit the parent's `source_repo`:

```bash
# Parent in current repo
bd create "Implement auth" -p 1
# → Created as bd-abc (source_repo = ".")

# Discovered issue inherits parent's repo
bd create "Found bug in auth" -p 1 --deps discovered-from:bd-abc
# → Created with source_repo = "." (same as parent)
```

This ensures discovered work stays in the same repository as the parent task.

## Backward Compatibility

- **Single-repo workflows unchanged**: If no multi-repo config exists, all issues go to current repo
- **Explicit --repo always wins**: `--repo` flag overrides any auto-routing
- **No schema changes**: Routing is pure config-based, no database migrations

## Implementation

**Key Files:**
- `internal/routing/routing.go` - Role detection and routing logic
- `internal/routing/routing_test.go` - Unit tests
- `cmd/bd/create.go` - Integration with create command
- `routing_integration_test.go` - End-to-end tests

**API:**

```go
// Detect user role based on git configuration
// Checks: config → upstream remote → push URL heuristic
func DetectUserRole(repoPath string) (UserRole, error)

// Check if repository has an "upstream" remote (fork signal)
func HasUpstreamRemote(repoPath string) bool

// Determine target repo based on config and role
func DetermineTargetRepo(config *RoutingConfig, userRole UserRole, repoPath string) string
```

## Testing

```bash
# Run routing tests
go test -v ./internal/routing/...

# Run integration tests (requires git)
go test -v -tags=integration ./cmd/bd/... -run TestUpstreamRemoteDetection

# Tests cover:
# - Maintainer detection (git config)
# - Contributor detection (fork remotes)
# - Upstream remote detection (fork signal)
# - SSH vs HTTPS remote detection
# - Explicit --repo override
# - End-to-end multi-repo workflow
```

## Future Enhancements

See [bd-k58](https://github.com/steveyegge/beads/issues/k58) for proposal workflow:
- `bd propose <id>` - Move issue from planning to upstream
- `bd withdraw <id>` - Un-propose
- `bd accept <id>` - Maintainer accepts proposal

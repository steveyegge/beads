# Multi-Repo Auto-Routing

This document describes the auto-routing feature that intelligently directs new issues to the appropriate repository based on user role.

## Overview

Auto-routing solves the OSS contributor problem: contributors want to plan work locally without polluting upstream PRs with planning issues. The routing layer automatically detects whether you're a maintainer or contributor and routes `bd create` to the appropriate repository.

## User Role Detection

### Strategy

The routing system uses a 4-tier detection strategy (in priority order):

1. **Explicit git config** (highest priority):
   ```bash
   git config beads.role maintainer
   # or
   git config beads.role contributor
   ```
   This always wins. Use it to override any automatic detection.

2. **Cached role** (performance optimization):
   ```bash
   # Stored automatically after detection
   git config beads.role.cache maintainer

   # Clear to force re-detection
   git config --unset beads.role.cache
   ```
   After role is detected via API or heuristic, it's cached to avoid repeated lookups.

3. **Upstream remote detection** (fork signal):
   - If an `upstream` remote exists → Contributor
   - This correctly identifies fork contributors who use SSH for their fork
   - Standard OSS workflow: fork adds `upstream` pointing to original repo

4. **GitHub API** (authoritative, if token available):
   - Checks if repository is a fork via GitHub API
   - Checks if user has push permissions
   - Fork = Contributor, Push access = Maintainer
   - Requires GitHub token (see Token Discovery below)
   - Rate limit errors fall through to heuristic

5. **Push URL inspection** (fallback heuristic):
   - SSH URLs (`git@github.com:user/repo.git`) → Maintainer
   - HTTPS with credentials (`https://token@github.com/...`) → Maintainer
   - HTTPS without credentials → Contributor
   - No remote → Contributor (safest fallback)

### Token Discovery

For GitHub API detection, tokens are discovered in this order:

1. `GITHUB_TOKEN` environment variable
2. `GH_TOKEN` environment variable
3. `gh auth token` (GitHub CLI)
4. `git credential fill` for github.com

If no token is found, detection skips API and uses heuristics.

### Cache Invalidation

The role cache (`beads.role.cache`) should be cleared when:
- You receive push access to a repository
- You change the fork/upstream relationship
- Detection gave an incorrect result

```bash
# Clear cache
git config --unset beads.role.cache

# Next bd command will re-detect and re-cache
bd ready
```

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

**Note:** Upstream detection takes priority over GitHub API because it's faster (no network call) and the fork pattern is unambiguous. If you have an `upstream` remote but want to be treated as maintainer, use explicit config:
```bash
git config beads.role maintainer
```

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
- `internal/routing/github_client.go` - GitHub API integration
- `internal/routing/token_discovery.go` - Token discovery from env/CLI/credential store
- `internal/routing/routing_test.go` - Unit tests
- `internal/routing/detection_test.go` - Detection priority and caching tests
- `internal/beads/context.go` - RepoContext with role fields
- `cmd/bd/create.go` - Integration with create command

**API:**

```go
// Detect user role with full metadata (recommended)
// Uses 4-tier strategy: config → cache → upstream → api → heuristic
func DetectUserRoleWithSource(repoPath string) (*RoleDetectionResult, error)

// RoleDetectionResult contains role and detection metadata
type RoleDetectionResult struct {
    Role        UserRole    // "maintainer" or "contributor"
    Source      RoleSource  // "config", "cache", "upstream", "api", "heuristic"
    IsFork      bool        // True if GitHub API confirmed fork
    OriginURL   string      // Origin remote URL
    UpstreamURL string      // Upstream remote URL (empty if none)
    HasUpstream bool        // True if upstream remote exists
}

// Legacy: simple role detection without metadata
func DetectUserRole(repoPath string) (UserRole, error)

// Check if repository has an "upstream" remote (fork signal)
func HasUpstreamRemote(repoPath string) bool

// Determine target repo based on config and role
func DetermineTargetRepo(config *RoutingConfig, userRole UserRole, repoPath string) string

// RepoContext (from internal/beads) includes role fields:
type RepoContext struct {
    // ... existing fields ...
    UserRole    routing.UserRole  // Detected role
    RoleSource  string            // How role was detected
    IsFork      bool              // From GitHub API
    HasUpstream bool              // Upstream remote exists
}
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

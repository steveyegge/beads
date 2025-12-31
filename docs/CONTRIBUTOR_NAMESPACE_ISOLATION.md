# Contributor Namespace Isolation Design

**Issue**: bd-umbf
**Status**: Design Complete
**Author**: beads/polecats/onyx
**Created**: 2025-12-30

## Problem Statement

When contributors work on beads-the-project using beads-the-tool, their personal
work-tracking issues leak into PRs. The `.beads/issues.jsonl` file is intentionally
git-tracked (it's the project's canonical issue database), but contributors' local
issues pollute the diff.

This is a **recursion problem unique to self-hosting projects**.

### The Recursion

```
beads-the-project/
├── .beads/
│   └── issues.jsonl    ← Project bugs, features, tasks (SHOULD be in PRs)
└── src/
    └── ...

contributor-working-on-beads/
├── .beads/
│   └── issues.jsonl    ← Project issues PLUS personal tracking (POLLUTES PRs)
└── src/
    └── ...
```

When a contributor:
1. Forks/clones the beads repository
2. Uses `bd create "My TODO: fix tests before lunch"` to track their work
3. Creates a PR

The PR diff includes their personal issues in `.beads/issues.jsonl`.

### Why This Matters

- **Noise in diffs**: Reviewers see issue database changes unrelated to the PR
- **Merge conflicts**: Personal issues conflict with upstream issue changes
- **Privacy leakage**: Contributors' work habits and notes become public
- **Git history pollution**: Unrelated metadata in commit history

## Solution Space Analysis

### Approach 1: Contributor Namespaces (Prefix-Based)

Each contributor gets a private prefix (e.g., `bd-steve-xxxx`) that's gitignored.

**Pros:**
- Single database, simple mental model
- Prefix visually distinguishes personal vs project issues

**Cons:**
- Requires `.gitignore` entries per contributor
- Prefix in ID is permanent - can't "promote" to project issue
- Prefix collision risk with project's chosen prefix

**Verdict**: Too fragile for a zero-friction solution.

### Approach 2: Separate Database (BEADS_DIR)

Contributors use `BEADS_DIR` pointing elsewhere for personal tracking.

**Pros:**
- Complete isolation - no pollution possible
- Works today via environment variable
- Clear separation of concerns

**Cons:**
- Manual setup required
- Two separate databases means context switching
- Cross-linking between personal and project issues is awkward

**Verdict**: Viable but requires explicit setup.

### Approach 3: Issue Ownership/Visibility Flags

Mark issues as "local-only" vs "project" with a flag.

**Pros:**
- Single database
- Easy to change visibility
- Could filter during export

**Cons:**
- Easy to forget to set the flag
- Export logic becomes complex
- Default matters (which causes friction?)

**Verdict**: Adds complexity without solving the core problem.

### Approach 4: Auto-Routing Based on User Role ← RECOMMENDED

Automatically detect if user is maintainer or contributor and route new issues
accordingly:
- **Maintainer** (SSH access): Issues go to `./.beads/` (project database)
- **Contributor** (HTTPS fork): Issues go to `~/.beads-planning/` (personal database)

**Pros:**
- Zero-friction for contributors
- Automatic based on git remote inspection
- Clear separation maintained automatically
- Can aggregate both databases for unified view

**Cons:**
- Requires initial setup for personal database
- Role detection has edge cases (CI, work vs personal machines)

**Verdict**: Best balance of automation and isolation.

## Current Implementation Status

### What's Implemented

1. **Role Detection** (`internal/routing/routing.go`):
   ```go
   func DetectUserRole(repoPath string) (UserRole, error)
   ```
   - Checks `git config beads.role` for explicit override
   - Inspects push URL: SSH → Maintainer, HTTPS → Contributor
   - Defaults to Contributor if uncertain

2. **Routing Configuration** (`internal/config/config.go`):
   ```go
   v.SetDefault("routing.mode", "auto")
   v.SetDefault("routing.default", ".")
   v.SetDefault("routing.contributor", "~/.beads-planning")
   ```

3. **Target Repo Calculation** (`internal/routing/routing.go`):
   ```go
   func DetermineTargetRepo(config *RoutingConfig, userRole UserRole, repoPath string)
   ```

4. **Contributor Setup Wizard** (`cmd/bd/init_contributor.go`):
   ```bash
   bd init --contributor
   ```
   Creates `~/.beads-planning/` and configures routing.

5. **Documentation**:
   - `docs/ROUTING.md` - Auto-routing mechanics
   - `docs/MULTI_REPO_MIGRATION.md` - Contributor workflow guide

### What's NOT Implemented (Gaps)

1. **Actual Routing in `bd create`** (bd-6x6g):
   ```go
   // cmd/bd/create.go:181
   // TODO(bd-6x6g): Switch to target repo for multi-repo support
   // For now, we just log the target repo in debug mode
   if repoPath != "." {
       debug.Logf("DEBUG: Target repo: %s\n", repoPath)
   }
   ```
   The routing is calculated but NOT used. Issues still go to `./.beads/`.

2. **Pollution Detection for Preflight** (bd-lfak):
   No way to detect if personal issues are in the PR diff.

3. **First-Time Contributor Warning**:
   No prompt when a contributor first runs `bd create` without setup.

## Recommended Implementation Plan

### Phase 1: Complete Auto-Routing (bd-6x6g)

Make `bd create` actually route to the target repo:

```go
// In cmd/bd/create.go, after DetermineTargetRepo()
if repoPath != "." {
    // Switch store to target repo
    targetBeadsDir := expandPath(repoPath)
    if err := ensureBeadsDir(targetBeadsDir); err != nil {
        return fmt.Errorf("failed to initialize target repo: %w", err)
    }
    store, err = storage.OpenStore(filepath.Join(targetBeadsDir, "beads.db"))
    if err != nil {
        return fmt.Errorf("failed to open target store: %w", err)
    }
    // Continue with issue creation in target store
}
```

### Phase 2: First-Time Setup Prompt

When a contributor runs `bd create` without routing configured:

```
→ Detected fork/contributor setup
→ Personal issues would pollute upstream PRs

Options:
  1. Configure auto-routing (recommended)
     Creates ~/.beads-planning for personal tracking

  2. Continue to current repo
     Issue will appear in .beads/issues.jsonl (affects PRs)

Choice [1]:
```

### Phase 3: Pollution Detection (for bd-lfak)

Add check in `bd preflight --check`:

```go
func checkBeadsPollution(ctx context.Context) (CheckResult, error) {
    // Get git diff of .beads/issues.jsonl
    diff, err := gitDiff(".beads/issues.jsonl")
    if err != nil {
        return CheckResult{}, err
    }

    // Parse added issues from diff
    addedIssues := parseAddedIssues(diff)

    // Check if any added issues have source_repo != "."
    // OR were created by current user (heuristic)
    for _, issue := range addedIssues {
        if issue.SourceRepo != "." {
            // Definite pollution - issue was routed elsewhere but leaked
            return CheckResult{
                Status: Fail,
                Message: fmt.Sprintf("Personal issue %s in diff", issue.ID),
            }, nil
        }
        // Heuristic: check created_by against git author
        if issue.CreatedBy != "" && !isProjectMaintainer(issue.CreatedBy) {
            return CheckResult{
                Status: Warn,
                Message: fmt.Sprintf("Issue %s may be personal (created by %s)",
                    issue.ID, issue.CreatedBy),
            }, nil
        }
    }

    return CheckResult{Status: Pass}, nil
}
```

### Phase 4: Graduating Issues

Allow promoting a personal issue to a project issue:

```bash
# Move from personal to project database
bd migrate plan-42 --to . --dry-run
bd migrate plan-42 --to .
```

This creates a new issue in the target repo with a reference to the original.

## Configuration Reference

### Contributor Setup (Recommended)

```bash
# One-time setup
bd init --contributor

# This configures:
# - Creates ~/.beads-planning/ with its own database
# - Sets routing.mode=auto
# - Sets routing.contributor=~/.beads-planning

# Verify
bd config get routing.mode        # → auto
bd config get routing.contributor # → ~/.beads-planning
```

### Explicit Role Override

```bash
# Force maintainer mode (for CI or shared machines)
git config beads.role maintainer

# Force contributor mode
git config beads.role contributor
```

### Manual BEADS_DIR Override

```bash
# Per-command override
BEADS_DIR=~/.beads-planning bd create "My task" -p 1

# Or per-shell session
export BEADS_DIR=~/.beads-planning
bd create "My task" -p 1
```

## Pollution Detection Heuristics

For `bd preflight`, we can detect pollution by checking:

1. **Source Repo Mismatch**: Issue has `source_repo != "."` but is in `./.beads/`
2. **Creator Check**: Issue `created_by` doesn't match known maintainers
3. **Prefix Mismatch**: Issue prefix doesn't match project prefix
4. **Timing Heuristic**: Issue created recently on contributor's branch

### False Positive Mitigation

Some issues ARE meant to be in PRs:
- Bug reports discovered during implementation
- Documentation issues created while coding
- Test failure tracking

Use `--type` to distinguish:
- `--type=task` or `--type=feature` from contributor → likely personal
- `--type=bug` discovered during work → may be legitimate project issue

## Dependencies

This design enables:
- **bd-lfak**: PR preflight checks (pollution detection)
- **bd-6x6g**: Multi-repo target switching in `bd create`

## Success Criteria

1. Contributors can use beads without polluting upstream PRs
2. Zero-friction default: auto-routing based on role detection
3. Explicit override available when needed
4. `bd preflight` can detect and warn about pollution
5. Clear upgrade path to "graduate" personal issues to project issues

## Open Questions

1. **Should we warn on first `bd create` without setup?**
   - Pro: Prevents accidental pollution
   - Con: Friction for new users who may be maintainers

2. **Should personal database be auto-created?**
   - Pro: True zero-friction
   - Con: Creates files user didn't ask for

3. **How to handle CI environments?**
   - CI typically has HTTPS access even for maintainers
   - Need explicit `beads.role=maintainer` config or skip routing in CI

## Appendix: Role Detection Algorithm

```
1. Check git config beads.role
   - If "maintainer" → Maintainer
   - If "contributor" → Contributor

2. Get push URL: git remote get-url --push origin
   - If starts with git@ or ssh:// → Maintainer (SSH access implies write)
   - If contains @ (credentials) → Maintainer
   - If HTTPS without credentials → Contributor

3. Default → Contributor (safe fallback)
```

This algorithm prioritizes safety: when in doubt, route to personal database
to avoid accidental pollution.

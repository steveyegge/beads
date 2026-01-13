# Proposal: Branch-Based Issue Namespacing

**Status:** Draft  
**Author:** Discussion between mhwilkie and AI assistant  
**Date:** 2026-01-13  
**Related Issues:** Fork workflow friction, PR conflicts on issues.jsonl

## Problem Statement

When contributors fork beads and use `bd` to track their work:

1. `issues.jsonl` diverges from upstream immediately
2. Every PR requires manual exclusion of this file
3. There's no clear way to collaborate on issues across forks
4. The current `--contributor` mode puts all issues in `~/.beads-planning`, which doesn't scale to dozens of projects

### The Irony

Beads is an issue tracker designed for AI agents, but contributors are told NOT to use beads in their forks (or at least, not to commit it). This friction discourages using beads to develop beads.

## Inspiration: Module Systems

Well-tested namespace patterns from package managers:

| System | Pattern | Example |
|--------|---------|---------|
| **Go** | `domain/org/repo/package` | `github.com/steveyegge/beads/internal/types` |
| **Python** | `package.subpackage.module` | `beads.internal.types` |
| **npm** | `@scope/package/path` | `@steveyegge/beads/internal/types` |
| **Rust** | `crate::module::item` | `beads::internal::types` |

**Key insight:** All systems separate **identity** (what) from **source** (where/which version).

## Proposed Solution

### Core Syntax

```
{project}:{branch}-{hash}
└───┬───┘ └──┬───┘ └─┬─┘
    │        │       └── Unique within branch
    │        └────────── Branch namespace (omit for main/default)
    └─────────────────── Project identifier
```

### Examples

```bash
# Main branch (default)
beads:a3f2                 

# Feature branch
beads:fix-auth-a3f2        

# Child issue (hierarchical)
beads:fix-auth-a3f2.1      

# Different project
other-project:main-b7c9    
```

### Source Configuration (Separate from Identity)

Like `go.mod` or `package.json`, source is configured separately:

```yaml
# .beads/sources.yaml
sources:
  beads:
    upstream: github.com/steveyegge/beads
    fork: github.com/mhwilkie/beads  # Optional
    
  other-project:
    upstream: github.com/other/project
```

This mirrors how modules work:
```go
// go.mod
replace github.com/steveyegge/beads => github.com/mhwilkie/beads v0.0.0-fix-auth
```

## Grammar

```
issue_id       := short_form | qualified_form
short_form     := hash | branch "-" hash
qualified_form := project ":" (branch "-")? hash

project        := name                    # Simple project name
branch         := name                    # Git branch name  
hash           := [a-z0-9]{4,8}          # Unique identifier
name           := [a-zA-Z0-9_-]+         # Identifier characters
```

### Delimiter Vocabulary

- `:` = project boundary (like `::` in Rust, `/` in Go imports)
- `-` = separates branch from hash (and within branch names)
- `.` = hierarchical children (like `bd-a3f2.1`)

## Resolution Rules

Given context: Working in `mhwilkie/beads` fork on branch `fix-auth`

| Input | Resolves To | Explanation |
|-------|-------------|-------------|
| `a3f2` | `beads:fix-auth-a3f2` | Current context |
| `fix-auth-a3f2` | `beads:fix-auth-a3f2` | Explicit branch |
| `main-b7c9` | `beads:main-b7c9` | Different branch |
| `beads:c4d8` | `beads:main-c4d8` | Explicit project, default branch |
| `other:f6g7` | `other:main-f6g7` | Different project |

## How This Solves the Fork Problem

### Branch Isolation

Branch-scoped issues never conflict with main:

```
Upstream main:     beads:main-a3f2, beads:main-b7c9
Fork main:         beads:main-x1y2 (different hash, no conflict)
Fork feature:      beads:fix-auth-p5q6 (branch-scoped)

PR from fork to upstream:
- Feature branch issues: NOT in main's issues.jsonl
- Only main branch issues would conflict
- But forks typically don't modify upstream's main issues
```

### The Commit Squashing Analogy

Just as commits flow through curation:

```
Developer work:
  100 messy commits → squash/rebase → 5 clean commits → PR → main

Issues could flow:
  50 working issues → curate/promote → 3 relevant issues → PR → upstream
```

### Proposed Workflow

```bash
# Working on fork feature branch
beads:fix-auth-a1b2   # "Investigate auth bug"
beads:fix-auth-c3d4   # "Try approach A (failed)"
beads:fix-auth-e5f6   # "Try approach B (works!)"

# Before PR: promote relevant issues to main
bd promote e5f6 --to main
# → beads:main-e5f6 (now in main namespace)

# PR includes only main-branch issues
# Feature branch issues stay in branch (like unmerged commits)

# On merge to upstream:
# Maintainer can: bd accept beads:main-e5f6 --as beads:main-xyz
```

## Storage Format

```jsonl
{"id":"a3f2","project":"beads","branch":"fix-auth","title":"Fix widget",...}
{"id":"b7c9","project":"beads","branch":"main","title":"Core feature",...}
```

The `id` field is now JUST the hash. Project and branch are separate fields.

## CLI Experience

```bash
# Initialize (project name auto-derived from git remote)
bd init
# Detected project: beads
# Fork of: github.com/steveyegge/beads

# Create issue (uses current branch automatically)
bd create "Fix the widget" -p 1
# Created: fix-auth-a3f2

# Create in main branch explicitly
bd create "Document the fix" -p 2 --branch main
# Created: main-b7c9

# List issues (current branch by default)
bd list
# fix-auth-a3f2  Fix the widget
# fix-auth-c3d4  Another task

# List all branches
bd list --all-branches
# main-b7c9      Core feature
# fix-auth-a3f2  Fix the widget
# fix-auth-c3d4  Another task

# Reference upstream issue
bd show beads:main-c4d8 --source upstream

# Cross-reference in dependency
bd dep add a3f2 beads:main-c4d8
```

## Display Modes

```bash
# Full qualification (unambiguous)
bd list --full
beads:fix-auth-a3f2  Fix the widget
beads:fix-auth-b7c9  Another task
beads:main-c4d8      Core feature

# Short form (current context)
bd list
fix-auth-a3f2  Fix the widget
fix-auth-b7c9  Another task
main-c4d8      Core feature (different branch, shown with branch)

# Minimal (current branch only)
bd list --branch
a3f2  Fix the widget
b7c9  Another task
```

## Comparison with Module Systems

| Aspect | Go | npm | Beads (proposed) |
|--------|-----|-----|------------------|
| **Identity** | `github.com/org/repo` | `@scope/package` | `project:branch-hash` |
| **Source config** | `go.mod` | `.npmrc` | `.beads/sources.yaml` |
| **Version/Branch** | `v1.2.3` in go.mod | `^1.2.3` in package.json | Branch in ID |
| **Fork handling** | `replace` directive | Registry config | `sources.yaml` fork entry |
| **Short form** | `repo` (with go.mod) | `package` | `hash` (current context) |

## Backward Compatibility

```bash
# Old format still works
bd-a3f2              → beads:main-a3f2 (assumes current project, main branch)
bd-fix-auth/a3f2     → beads:fix-auth-a3f2 (old branch syntax)

# Migration path
bd migrate --to-namespaced-ids
# Rewrites storage format, preserves all data
```

## Open Questions

### 1. Branch Deletion

What happens to `beads:fix-auth-a3f2` when `fix-auth` branch is deleted?

**Options:**
- A: Orphaned (still accessible by full ID)
- B: Auto-promote to main if not closed
- C: Archive/tombstone with branch deletion event

### 2. Project Name Derivation

How to determine project name automatically?

**Options:**
- A: Repository name from git remote (e.g., `beads` from `github.com/steveyegge/beads`)
- B: Explicit config during `bd init`
- C: Directory name (current behavior for prefix)

### 3. Merge Behavior

When feature branch merges to main, what happens to branch-scoped issues?

**Options:**
- A: Keep branch namespace (historical record)
- B: Auto-migrate to main namespace
- C: User choice per-issue via `bd promote`

### 4. Cross-Fork References

How to reference issues in other forks?

**Options:**
- A: Configure fork as additional source: `bd show beads:main-a3f2 --source mhwilkie`
- B: Full URL syntax: `bd show github.com/mhwilkie/beads:main-a3f2`
- C: Named remotes like git: `bd show fork:main-a3f2`

## Benefits Summary

| Problem | Solution |
|---------|----------|
| Fork issues pollute PRs | Branch-scoped issues excluded by default |
| Dozens of projects | Each project has its own namespace |
| Collaboration visibility | Source config enables cross-fork references |
| Issue curation | `bd promote` moves issues between branches |
| Backward compatibility | Old `bd-xxx` format still works |

## Implementation Considerations

### ID Structure

```go
type IssueID struct {
    Project string // "beads", "other-project"
    Branch  string // "main", "fix-auth", "" (defaults to main)
    Hash    string // "a3f2", "b7c9"
}

func (id IssueID) String() string {
    if id.Branch == "" || id.Branch == "main" {
        return fmt.Sprintf("%s:%s", id.Project, id.Hash)
    }
    return fmt.Sprintf("%s:%s-%s", id.Project, id.Branch, id.Hash)
}

func (id IssueID) Short() string {
    if id.Branch == "" || id.Branch == "main" {
        return id.Hash
    }
    return fmt.Sprintf("%s-%s", id.Branch, id.Hash)
}
```

### Storage Schema Changes

```sql
-- Add project and branch columns
ALTER TABLE issues ADD COLUMN project TEXT DEFAULT 'beads';
ALTER TABLE issues ADD COLUMN branch TEXT DEFAULT 'main';

-- Index for efficient branch queries
CREATE INDEX idx_issues_project_branch ON issues(project, branch);
```

### Sources Configuration

```go
type SourceConfig struct {
    Upstream string `yaml:"upstream"` // Canonical source
    Fork     string `yaml:"fork"`     // User's fork (optional)
    Local    string `yaml:"local"`    // Local override (optional)
}

type SourcesConfig struct {
    Sources map[string]SourceConfig `yaml:"sources"`
}
```

## Related Work

- [ADAPTIVE_IDS.md](../ADAPTIVE_IDS.md) - Current hash ID implementation
- [MULTI_REPO_MIGRATION.md](../MULTI_REPO_MIGRATION.md) - Existing multi-repo patterns
- [ROUTING.md](../ROUTING.md) - Auto-routing for contributors
- [GIT_INTEGRATION.md](../GIT_INTEGRATION.md) - Git merge handling

## Next Steps

1. Gather feedback on syntax and semantics
2. Prototype branch detection and ID generation
3. Design migration path from current format
4. Implement source configuration
5. Update CLI commands for new ID format
6. Write comprehensive tests for resolution rules
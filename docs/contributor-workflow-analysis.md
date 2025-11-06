# Beads Contributor Workflow Analysis

**Date**: 2025-11-03
**Context**: Design discussion on how to handle beads issues in PR/OSS contribution workflows

## The Problem (from #207)

When contributing to OSS projects with beads installed:
- Git hooks automatically commit contributor's personal planning to PRs
- Contributor's experimental musings pollute the upstream project's issue tracker
- No clear ownership/permission model for external contributors
- Difficult to keep beads changes out of commits

**Core tension**: Beads is great for team planning (shared namespace), but breaks down for OSS contributions (hierarchical gatekeeping).

## Key Insights from Discussion

### Beads as "Moving Frontier"

Beads is not a traditional issue tracker. It captures the **active working set** - the sliding window of issues currently under attention:

- Work moves fast with AI agents (10x-50x acceleration)
- Completed work fades quickly (95% never revisited, should be pruned aggressively)
- Future work is mostly blocked (small frontier of ready tasks)
- The frontier is bounded by team size (dozens to hundreds of issues, not thousands)

**Design principle**: Beads should focus on the "what's next" cloud, not long-term planning or historical archive.

### The Git Ledger is Fundamental

Beads achieves reliability despite being unreliable (merge conflicts, sync issues, data staleness) through:

**A. Git is the ledger and immutable backstop for forensics**
**B. AI is the ultimate arbiter and problem-solver when things go wrong**

Any solution that removes the git ledger (e.g., gitignored contributor files) breaks this model entirely.

### Requirements for Contributors

Contributors need:
- Git-backed persistence (multi-clone sync, forensics, AI repair)
- Isolated planning space (don't pollute upstream)
- Ability to propose selected issues upstream
- Support for multiple workers across multiple clones of the same repo

## Proposed Solutions

### Idea 1: Fork-Aware Hooks + Two-File System

**Structure**:
```
# Upstream repo
.beads/
  beads.jsonl          # Canonical frontier (committed)
  .gitignore           # Ignores local.jsonl

# Contributor's fork
.beads/
  beads.jsonl          # Synced from upstream (read-only)
  local.jsonl          # Contributor planning (committed to fork)
  beads.db             # Hydrated from both
```

**Detection**: Check for `upstream` remote to distinguish fork from canonical repo

**Workflow**:
```bash
# In fork
$ bd add "Experiment"        # → local.jsonl (committed to fork)
$ bd sync                    # → Pulls upstream's beads.jsonl
$ bd show                    # → Shows both
$ bd propose bd-a3f8e9       # → Moves issue to beads.jsonl for PR
```

**Pros**:
- Git ledger preserved (local.jsonl committed to fork)
- Multi-clone sync works
- Upstream .gitignore prevents pollution

**Cons**:
- Fork detection doesn't help teams using branches (most common workflow)
- Two files to manage
- Requires discipline to use `bd propose`

### Idea 2: Ownership Metadata + Smart PR Filtering

**Structure**:
```jsonl
{"id":"bd-123","owner":"upstream","title":"Canonical issue",...}
{"id":"bd-456","owner":"stevey","title":"My planning",...}
```

**Workflow**:
```bash
$ bd add "Experiment"        # → Creates with owner="stevey"
$ bd propose bd-456          # → Changes owner to "upstream"
$ bd clean-pr                # → Filters commit to only upstream-owned issues
$ git push                   # → PR contains only proposed issues
```

**Pros**:
- Single file (simpler)
- Works with any git workflow (branch, fork, etc)
- Git ledger fully preserved

**Cons**:
- Requires discipline to run `bd clean-pr`
- Clean commit is awkward (temporarily removing data)
- Merge conflicts if upstream and contributor both modify beads.jsonl

### Idea 3: Branch-Scoped Databases

Track which issues belong to which branch, filter at PR time.

**Implementation**: Similar to #2 but uses labels/metadata to track branch instead of owner.

**Challenge**: Complex with multiple feature branches, requires tracking branch scope.

### Idea 4: Separate Planning Repo (Most Isolated)

**Structure**:
```bash
# Main project repos (many)
~/projects/beads/.beads/beads.jsonl
~/projects/foo/.beads/beads.jsonl

# Single planning repo (one)
~/.beads-planning/.beads/beads.jsonl

# Configuration links them
~/projects/beads/.beads/config.toml:
  planning_repo = "~/.beads-planning"
```

**Workflow**:
```bash
$ cd ~/projects/beads
$ bd add "My idea"           # → Commits to ~/.beads-planning/
$ bd show                    # → Shows beads canonical + my planning
$ bd propose bd-a3f8e9       # → Exports to beads repo for PR
```

**Pros**:
- Complete isolation (separate git histories, zero PR pollution risk)
- Git ledger fully preserved (both repos tracked)
- Multi-clone works perfectly (clone both repos)
- No special filtering/detection needed
- **Scales better**: One planning repo for all projects

**Cons**:
- Two repos to manage
- Less obvious for new users (where's my planning?)

## Analysis: Fork vs Clone vs Branch

**Clone**: Local copy of a repo (`git clone <url>`)
- `origin` remote points to source
- Push directly to origin (if you have write access)

**Fork**: Server-side copy on GitHub
- For contributors without write access
- `origin` → your fork, `upstream` → original repo
- Push to fork, then PR from fork → upstream

**Branch**: Feature branches in same repo
- Most common for teams with write access
- Push to same repo, PR from branch → main

**Key insight**: Branches are universal, forks are only for external contributors. Most teams work on branches in a shared repo.

## Current Thinking: Idea 4 is Cleanest

After analysis, **separate planning repo (#4)** is likely the best solution because:

1. **Only solution that truly prevents PR pollution** (separate git histories)
2. **Git ledger fully preserved** (both repos tracked)
3. **Multi-clone works perfectly** (just clone both)
4. **No complex filtering/detection needed** (simple config)
5. **Better scaling**: One planning repo across all projects you contribute to

The "managing two repos" concern is actually an advantage: your planning is centralized and project-agnostic.

## Open Questions

### About the Workflow

1. **Where does PR pollution actually happen?**
   - Scenario A: Feature branch → upstream/main includes all beads changes from that branch?
   - Scenario B: Something else?

2. **Multi-clone usage pattern**:
   - Multiple clones on different machines?
   - All push/pull to same remote?
   - Workers coordinate via git sync?
   - PRs created from feature branches?

### About Implementation

1. **Proposed issue IDs**: When moving issue from planning → canonical, keep same ID? (Hash-based IDs are globally unique)

2. **Upstream acceptance sync**: If upstream accepts/modifies a proposal, how to sync back to contributor?
   - `bd sync` detects accepted proposals
   - Moves from planning repo to project's canonical beads.jsonl

3. **Multiple projects**: One planning repo for all projects you contribute to, or one per project?

4. **Backwards compatibility**: Single-user projects unchanged (single beads.jsonl)

5. **Discovery**: How do users discover this feature? Auto-detect and prompt?

## Next Steps

Need to clarify:
1. User's actual multi-clone workflow (understand the real use case)
2. Where exactly PR pollution occurs (branch vs fork workflow)
3. Which solution best fits the "git ledger + multi-clone" requirements
4. Whether centralized planning repo (#4) or per-project isolation (#1/#2) is preferred

## Design Principles to Preserve

From the conversation, these are non-negotiable:

- **Git as ledger**: Everything must be git-tracked for forensics and AI repair
- **Moving frontier**: Focus on active work, aggressively prune completed work
- **Multi-clone sync**: Workers across clones must coordinate via git
- **Small databases**: Keep beads.jsonl small enough for agents to read (<25k per repo, see below)
- **Simple defaults**: Don't break single-user workflows
- **Explicit over implicit**: Clear boundaries between personal and canonical

### JSONL Size Bounds with Multi-Repo

**Critical clarification**: The <25k limit applies **per-repo**, not to total hydrated size.

#### The Rule

**Per-repo limit**: Each individual JSONL file should stay <25k (roughly 100-200 issues depending on metadata).

**Why per-repo, not total**:
1. **Git operations**: Each repo is independently versioned. Git performance depends on per-file size, not aggregate.
2. **AI readability**: Agents read JSONLs for forensics/repair. Reading one 20k file is easy; reading the union of 10 files is still manageable.
3. **Bounded growth**: Total size naturally bounded by number of repos (typically N=1-3, rarely >10).
4. **Pruning granularity**: Completed work is pruned per-repo, keeping each repo's frontier small.

#### Example Scenarios

| Primary | Planning | Team Shared | Total Hydrated | Valid? |
|---------|----------|-------------|----------------|--------|
| 20k | - | - | 20k | ✅ Single-repo, well under limit |
| 20k | 15k | - | 35k | ✅ Each repo <25k (per-repo rule) |
| 20k | 15k | 18k | 53k | ✅ Each repo <25k (per-repo rule) |
| 30k | 15k | - | 45k | ❌ Primary exceeds 25k |
| 20k | 28k | - | 48k | ❌ Planning exceeds 25k |

#### Rationale: Why 25k?

**Agent context limits**: AI agents have finite context windows. A 25k JSONL file is:
- ~100-200 issues with metadata
- ~500-1000 lines of JSON
- Comfortably fits in GPT-4 context (128k tokens)
- Small enough to read/parse in <500ms

**Moving frontier principle**: Beads tracks **active work**, not historical archive. With aggressive pruning:
- Completed issues get compacted/archived
- Blocked work stays dormant
- Only ready + in-progress issues are "hot"
- Typical frontier: 50-100 issues per repo

#### Monitoring Size with Multi-Repo

**Per-repo monitoring**:
```bash
# Check each repo's JSONL size
$ wc -c .beads/beads.jsonl
20480 .beads/beads.jsonl

$ wc -c ~/.beads-planning/beads.jsonl
15360 ~/.beads-planning/beads.jsonl

# Total hydrated size (informational, not a hard limit)
$ expr 20480 + 15360
35840
```

**Automated check**:
```go
// Check all configured repos
for _, repo := range cfg.Repos.All() {
    jsonlPath := filepath.Join(repo, "beads.jsonl")
    size, _ := getFileSize(jsonlPath)
    if size > 25*1024 {  // 25k
        log.Warnf("Repo %s exceeds 25k limit: %d bytes", repo, size)
    }
}
```

#### Pruning Strategy with Multi-Repo

Each repo should be pruned independently:

```bash
# Prune completed work from primary repo
$ bd compact --repo . --older-than 30d

# Prune experimental planning repo
$ bd compact --repo ~/.beads-planning --older-than 7d

# Shared team planning (longer retention)
$ bd compact --repo ~/team-shared/.beads --older-than 90d
```

Different repos can have different retention policies based on their role.

#### Total Size Soft Limit (Guideline Only)

While per-repo limit is the hard rule, consider total hydrated size for performance:

**Guideline**: Keep total hydrated size <100k for optimal performance.

**Why 100k total**:
- SQLite hydration: Parsing 100k JSON still fast (<1s)
- Agent queries: Dependency graphs with 300-500 total issues remain tractable
- Memory footprint: In-memory SQLite comfortably handles 500 issues

**If total exceeds 100k**:
- Not a hard error, but performance may degrade
- Consider pruning completed work more aggressively
- Evaluate if all repos are still needed
- Check if any repos should be archived/removed

#### Summary

| Limit Type | Value | Enforcement |
|------------|-------|-------------|
| **Per-repo (hard limit)** | <25k | ⚠️ Warn if exceeded, agents may struggle |
| **Total hydrated (guideline)** | <100k | ℹ️ Informational, affects performance |
| **Typical usage** | 20k-50k total | ✅ Expected range for active development |

**Bottom line**: Monitor per-repo size (<25k each). Total size naturally bounded by N repos × 25k.

---

# Decision: Separate Repos (Solution #4)

**Date**: 2025-11-03 (continued discussion)

## Why Separate Repos

After consideration, **Solution #4 (Separate Planning Repos)** is the chosen approach:

### Key Rationale

1. **Beads as a Separate Channel**: Beads is fundamentally a separate communication channel that happens to use git/VCS for persistence, not a git-centric tool. It should work with any VCS (jujutsu, sapling, mercurial, etc.).

2. **VCS-Agnostic Design**: Solution #1 (fork detection) is too git-centric and wouldn't work with other version control systems. Separate repos work regardless of VCS.

3. **Maximum Flexibility**: Supports multiple workflows and personas:
   - OSS contributor with personal planning
   - Multi-phase development (different beads DBs for different stages)
   - Multiple personas (architect, implementer, reviewer)
   - Team vs personal planning separation

4. **Zero PR Pollution Risk**: Completely separate git histories guarantee no accidental pollution of upstream projects.

5. **Proven Pain Point**: Experience shows that accidental bulk commits (100k issues) can be catastrophic and traumatic to recover from. Complete isolation is worth the complexity.

## Core Architecture Principles

### 1. Multi-Repo Support (N ≥ 1)

**Configuration should support N repos, including N=1 for backward compatibility:**

When N=1 (default), this is the current single-repo workflow - no changes needed.
When N≥2, multiple repos are hydrated together.

```toml
# .beads/config.toml

# Default mode: single repo (backwards compatible)
mode = "single"

# Multi-repo mode
[repos]
  # Primary repo: where canonical issues live
  primary = "."

  # Additional repos to hydrate into the database
  additional = [
    "~/.beads-planning",           # Personal planning across all projects
    "~/.beads-work/phase1",        # Architecting phase
    "~/.beads-work/phase2",        # Implementation phase
    "~/team-shared/.beads",        # Shared team planning
  ]

# Routing: where do new issues go?
[routing]
  mode = "auto"  # auto | explicit
  default = "~/.beads-planning"    # Default for `bd add`

  # Auto-detection: based on user permissions
  [routing.auto]
    maintainer = "."                   # If maintainer, use primary
    contributor = "~/.beads-planning"  # Otherwise use planning repo
```

### 2. Hydration Model

On `bd show`, `bd list`, etc., the database hydrates from multiple sources:

```
beads.db ← [
  ./.beads/beads.jsonl           (primary, read-write if maintainer)
  ~/.beads-planning/beads.jsonl   (personal, read-write)
  ~/team-shared/.beads/beads.jsonl (shared, read-write if team member)
]
```

**Metadata tracking**:
```jsonl
{
  "id": "bd-a3f8e9",
  "title": "Add dark mode",
  "source_repo": "~/.beads-planning",  # Which repo owns this issue
  "visibility": "local",               # local | proposed | canonical
  ...
}
```

### 3. Visibility States

Issues can be in different states of visibility:

- **local**: Personal planning, only in planning repo
- **proposed**: Exported for upstream consideration (staged for PR)
- **canonical**: In the primary repo (upstream accepted it)

### 4. VCS-Agnostic Operations

Beads should not assume git. Core operations:

- **Sync**: `bd sync` should work with git, jj, hg, sl, etc.
- **Ledger**: Each repo uses whatever VCS it's under (or none)
- **Transport**: Issues move between repos via export/import, not git-specific operations

## Workflow Examples

### Use Case 1: OSS Contributor

```bash
# One-time setup
$ mkdir ~/.beads-planning
$ cd ~/.beads-planning
$ git init && bd init

# Contributing to upstream project
$ cd ~/projects/some-oss-project
$ bd config --add-repo ~/.beads-planning --routing contributor

# Work
$ bd add "Explore dark mode implementation"
# → Goes to ~/.beads-planning/beads.jsonl
# → Commits to planning repo (git tracked, forensic trail)

$ bd show
# → Shows upstream's canonical issues (read-only)
# → Shows my planning issues (read-write)

$ bd work bd-a3f8e9
$ bd status bd-a3f8e9 in-progress

# Ready to propose
$ bd propose bd-a3f8e9 --target upstream
# → Exports issue from planning repo
# → Creates issue in ./beads/beads.jsonl (staged for PR)
# → Marks as visibility="proposed" in planning repo

$ git add .beads/beads.jsonl
$ git commit -m "Propose: Add dark mode"
$ git push origin feature-branch
# → PR contains only the proposed issue, not all my planning
```

### Use Case 2: Multi-Phase Development

```bash
# Setup phases
$ mkdir -p ~/.beads-work/{architecture,implementation,testing}
$ for dir in ~/.beads-work/*; do (cd $dir && git init && bd init); done

# Configure project
$ cd ~/my-big-project
$ bd config --add-repo ~/.beads-work/architecture
$ bd config --add-repo ~/.beads-work/implementation
$ bd config --add-repo ~/.beads-work/testing

# Architecture phase
$ bd add "Design authentication system" --repo ~/.beads-work/architecture
$ bd show --repo ~/.beads-work/architecture
# → Only architecture issues

# Implementation phase (later)
$ bd add "Implement JWT validation" --repo ~/.beads-work/implementation

# View all phases
$ bd show
# → Shows all issues from all configured repos
```

### Use Case 3: Multiple Contributors on Same Project

```bash
# Team member Alice (maintainer)
$ cd ~/project
$ bd add "Fix bug in parser"
# → Goes to ./beads/beads.jsonl (she's maintainer)
# → Commits to project repo

# Team member Bob (contributor)
$ cd ~/project
$ bd add "Explore performance optimization"
# → Goes to ~/.beads-planning/beads.jsonl (he's contributor)
# → Does NOT pollute project repo

$ bd show
# → Sees Alice's canonical issue
# → Sees his own planning

$ bd propose bd-xyz
# → Proposes to Alice's canonical repo
```

## Implementation Outline

### Phase 1: Core Multi-Repo Support

**Commands**:
```bash
bd config --add-repo <path>         # Add a repo to hydration
bd config --remove-repo <path>      # Remove a repo
bd config --list-repos              # Show all configured repos
bd config --routing <mode>          # Set routing: single|auto|explicit
```

**Config schema**:
```toml
[repos]
primary = "."
additional = ["path1", "path2", ...]

[routing]
default = "path"  # Where `bd add` goes by default
mode = "auto"     # auto | explicit
```

**Database changes**:
- Add `source_repo` field to issues
- Hydration layer reads from multiple JSONLs
- Writes go to correct JSONL based on source_repo

### Phase 2: Proposal Flow

**Commands**:
```bash
bd propose <issue-id> [--target <repo>]   # Move issue to target repo
bd withdraw <issue-id>                    # Un-propose (move back)
bd accept <issue-id>                      # Maintainer accepts proposal
```

**States**:
- `visibility: local` → Personal planning
- `visibility: proposed` → Staged for PR
- `visibility: canonical` → Accepted by upstream

### Phase 3: Routing Rules

**Auto-detection**:
- Detect if user is maintainer (git config, permissions)
- Auto-route to primary vs planning repo

**Config-based routing** (no new schema fields):
```toml
[routing]
mode = "auto"  # auto | explicit
default = "~/.beads-planning"  # Fallback for contributors

# Auto-detection rules
[routing.auto]
maintainer = "."  # If user is maintainer, use primary repo
contributor = "~/.beads-planning"  # Otherwise use planning repo
```

**Explicit routing** via CLI flag:
```bash
# Override auto-detection for specific issues
bd add "Design system" --repo ~/.beads-work/architecture
```

**Discovered issue inheritance**:
- Issues with parent_id automatically inherit parent's source_repo
- Keeps related work co-located

### Phase 4: VCS-Agnostic Sync

**Sync operations**:
- Detect VCS type per repo (git, jj, hg, sl)
- Use appropriate sync commands
- Fall back to manual sync if no VCS

**Example**:
```bash
$ bd sync
# Auto-detects:
# - . is git → runs git pull
# - ~/.beads-planning is jj → runs jj git fetch && jj rebase
# - ~/other is hg → runs hg pull && hg update
```

## Migration Path

### Existing Users (Single Repo)

No changes required. Current workflow continues to work:

```bash
$ bd add "Task"
# → .beads/beads.jsonl (as before)
```

### Library Consumers (Go/TypeScript)

**Critical for projects like VC that use beads as a library.**

#### Backward Compatibility (No Changes Required)

Your existing code continues to work unchanged. The storage layer automatically reads `.beads/config.toml` if present:

```go
// Before multi-repo (v0.17.3)
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")

// After multi-repo (v0.18.0+) - EXACT SAME CODE
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")
// If .beads/config.toml exists, additional repos are auto-hydrated
// If .beads/config.toml doesn't exist, single-repo mode (backward compatible)
```

**What happens automatically**:
1. Storage layer checks for `.beads/config.toml`
2. If found: Reads `repos.additional`, hydrates from all configured repos
3. If not found: Single-repo mode (current behavior)
4. Your code doesn't need to know which mode is active

#### Explicit Multi-Repo Configuration (Optional)

If you need to override config.toml or configure repos programmatically:

```go
// Explicit multi-repo configuration
cfg := beadsLib.Config{
    Primary:    ".beads/vc.db",
    Additional: []string{
        filepath.ExpandUser("~/.beads-planning"),
        filepath.ExpandUser("~/team-shared/.beads"),
    },
}
store, err := beadsLib.NewStorageWithConfig(cfg)
```

**When to use explicit configuration**:
- Testing: Override config for test isolation
- Dynamic repos: Add repos based on runtime conditions
- No config file: Programmatic setup without `.beads/config.toml`

#### When to Use Multi-Repo vs Single-Repo

**Single-repo (default, recommended for most library consumers)**:
```go
// VC executor managing its own database
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")
// Stays single-repo by default, no config.toml needed
```

**Multi-repo (opt-in for specific use cases)**:
- **Team planning**: VC executor needs to see team-wide issues from shared repo
- **Multi-phase dev**: Different repos for architecture, implementation, testing phases
- **Personal planning**: User wants to track personal experiments separate from VC's canonical DB

**Example: VC with team planning**:
```toml
# .beads/config.toml
[repos]
primary = "."
additional = ["~/team-shared/.beads"]

[routing]
default = "."  # VC-generated issues go to primary
```

```go
// VC executor code (unchanged)
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")

// GetReadyWork() now returns issues from:
// - .beads/vc.db (VC-generated issues)
// - ~/team-shared/.beads (team planning)
ready, err := store.GetReadyWork(ctx)
```

#### Migration Checklist for Library Consumers

1. **Test with config.toml**: Create `.beads/config.toml`, verify auto-hydration works
2. **Verify performance**: Ensure multi-repo hydration meets your latency requirements (see Performance section)
3. **Update exclusive locks**: If using locks, decide if you need per-repo or all-repo locking (see Exclusive Lock Protocol section)
4. **Review routing**: Ensure auto-generated issues (e.g., VC's `discovered:blocker`) go to correct repo
5. **Test backward compat**: Verify code works with and without config.toml

#### API Compatibility Matrix

| API Call | v0.17.3 (single-repo) | v0.18.0+ (multi-repo) | Breaking? |
|----------|----------------------|----------------------|-----------|
| `NewSQLiteStorage(path)` | ✅ Single repo | ✅ Auto-detects config | ❌ No |
| `GetReadyWork()` | ✅ Returns from single DB | ✅ Returns from all repos | ❌ No |
| `CreateIssue()` | ✅ Writes to single DB | ✅ Writes to primary (or routing config) | ❌ No |
| `UpdateIssue()` | ✅ Updates in single DB | ✅ Updates in source repo | ❌ No |
| Exclusive locks | ✅ Locks single DB | ✅ Locks per-repo | ❌ No |

**Summary**: Zero breaking changes. Multi-repo is transparent to library consumers.

### Opting Into Multi-Repo (CLI Users)

```bash
# Create planning repo
$ mkdir ~/.beads-planning && cd ~/.beads-planning
$ git init && bd init

# Link to project
$ cd ~/my-project
$ bd config --add-repo ~/.beads-planning
$ bd config --routing auto  # Auto-detect maintainer vs contributor

# Optionally migrate existing issues
$ bd migrate --move-to ~/.beads-planning --filter "author=me"
```

### Teams Adopting Beads

```bash
# Maintainer sets up project
$ cd ~/team-project
$ bd init
$ git add .beads/ && git commit -m "Initialize beads"

# Contributors clone and configure
$ git clone team-project
$ cd team-project
$ mkdir ~/.beads-planning && cd ~/.beads-planning
$ git init && bd init
$ cd ~/team-project
$ bd config --add-repo ~/.beads-planning --routing contributor
```

### Self-Hosting Projects (VC, Internal Tools, Pet Projects)

**Important**: The multi-repo design is primarily for **OSS contributors** making PRs to upstream projects. Self-hosting projects have different needs.

#### What is Self-Hosting?

Projects that use beads to build themselves:
- **VC (VibeCoder)**: Uses beads to track development of VC itself
- **Internal tools**: Company tools that track their own roadmap
- **Pet projects**: Personal projects with beads-based planning

**Key difference from OSS contribution**:
- No upstream/downstream distinction (you ARE the project)
- Direct commit access (no PR workflow)
- Often have automated executors/agents
- Bootstrap/early phase stability matters

#### Default Recommendation: Stay Single-Repo

**For most self-hosting projects, single-repo is the right choice:**

```bash
# Simple, stable, proven
$ cd ~/my-project
$ bd init
$ bd create "Task" -p 1
# → .beads/beads.jsonl (committed to project repo)
```

**Why single-repo for self-hosting**:
- ✅ **Simpler**: No config, no routing decisions, no multi-repo complexity
- ✅ **Proven**: Current architecture, battle-tested
- ✅ **Sufficient**: All issues live with the project they describe
- ✅ **Stable**: No hydration overhead, no cross-repo coordination

#### When to Adopt Multi-Repo

Multi-repo makes sense for self-hosting projects only in specific scenarios:

**Scenario 1: Team Planning Separation**

Your project has multiple developers with different permission levels:

```toml
# .beads/config.toml
[repos]
primary = "."  # Canonical project issues (maintainers only)
additional = ["~/team-shared/.beads"]  # Team planning (all contributors)
```

**Scenario 2: Multi-Phase Development**

Your project uses distinct phases (architecture → implementation → testing):

```toml
# .beads/config.toml
[repos]
primary = "."  # Current active work
additional = [
  "~/.beads-work/architecture",  # Design decisions
  "~/.beads-work/implementation", # Implementation backlog
]
```

**Scenario 3: Experimental Work Isolation**

You want to keep experimental ideas separate from canonical roadmap:

```toml
# .beads/config.toml
[repos]
primary = "."  # Committed roadmap
additional = ["~/.beads-experiments"]  # Experimental ideas
```

#### Automated Executors with Multi-Repo

**Critical for projects like VC with automated agents.**

**Default behavior (recommended)**:
```go
// Executor sees ONLY primary repo (canonical work)
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")
// No config.toml = single-repo mode
ready, err := store.GetReadyWork(ctx)  // Only canonical issues
```

**With multi-repo (opt-in)**:
```toml
# .beads/config.toml
[repos]
primary = "."
additional = ["~/team-shared/.beads"]

[routing]
default = "."  # Executor-created issues stay in primary
```

```go
// Executor code (unchanged)
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")
// Auto-reads config.toml, hydrates from both repos
ready, err := store.GetReadyWork(ctx)
// Returns issues from primary + team-shared

// When executor creates discovered issues:
discovered := &Issue{Title: "Found blocker", ...}
store.CreateIssue(discovered)
// → Goes to primary repo (routing.default = ".")
```

**Recommendation for executors**: Stay single-repo unless you have a clear team coordination need.

#### Bootstrap Phase Considerations

**If your project is in early/bootstrap phase (like VC), extra caution:**

1. **Prioritize stability**: Multi-repo adds complexity. Delay until proven need.
2. **Test thoroughly**: If adopting multi-repo, test with small repos first.
3. **Monitor performance**: Ensure executor polling loops stay sub-second (see Performance section).
4. **Plan rollback**: Keep single-repo workflow working so you can revert if needed.

**Bootstrap-phase checklist**:
- [ ] Do you have multiple developers with different permissions? → Maybe multi-repo
- [ ] Do you have team planning separate from executor roadmap? → Maybe multi-repo
- [ ] Are you solo or small team with unified planning? → Stay single-repo
- [ ] Is executor stability critical right now? → Stay single-repo
- [ ] Can you afford multi-repo testing/debugging time? → If no, stay single-repo

#### Migration Path for Self-Hosting Projects

**From single-repo to multi-repo (when ready)**:

```bash
# Step 1: Create planning repo
$ mkdir ~/.beads-planning && cd ~/.beads-planning
$ git init && bd init

# Step 2: Configure multi-repo (test mode)
$ cd ~/my-project
$ bd config --add-repo ~/.beads-planning --routing auto

# Step 3: Test with small workload
$ bd create "Test issue" --repo ~/.beads-planning
$ bd show  # Verify hydration works
$ bd ready  # Verify queries work

# Step 4: Verify executor compatibility
# - Run executor with multi-repo config
# - Check GetReadyWork() latency (<100ms)
# - Verify discovered issues route correctly

# Step 5: Migrate planning issues (optional)
$ bd migrate --move-to ~/.beads-planning --filter "label=experimental"
```

**Rollback (if needed)**:
```bash
# Remove config.toml to revert to single-repo
$ rm .beads/config.toml
$ bd show  # Back to single-repo mode
```

#### Summary: Self-Hosting Decision Tree

```
Is your project self-hosting? (building itself with beads)
├─ YES
│  ├─ Solo developer or unified team?
│  │  └─ Stay single-repo (simple, stable)
│  ├─ Multiple developers, different permissions?
│  │  └─ Consider multi-repo (team planning separation)
│  ├─ Multi-phase development (arch → impl → test)?
│  │  └─ Consider multi-repo (phase isolation)
│  ├─ Bootstrap/early phase?
│  │  └─ Stay single-repo (stability > flexibility)
│  └─ Automated executor?
│     └─ Stay single-repo unless team coordination needed
└─ NO (OSS contributor)
   └─ Use multi-repo (planning repo separate from upstream)
```

**Bottom line for self-hosting**: Default to single-repo. Only adopt multi-repo when you have a proven, specific need.

## Design Decisions (Resolved)

### 1. Namespace Collisions: **Option B (Global Uniqueness)**

**Decision**: Use globally unique hash-based IDs that include timestamp + random component.

**Rationale** (from VC feedback):
- Option C (allow collisions) breaks dependency references: `bd dep add bd-a3f8e9 bd-b7c2d1` becomes ambiguous
- Need to support cross-repo dependencies without repo-scoped namespacing
- Hash should be: `hash(title + description + timestamp_ms + random_4bytes)`
- Collision probability: ~1 in 10^12 (acceptable)

### 2. Cross-Repo Dependencies: **Yes, Fully Supported**

**Decision**: Dependencies work transparently across all repos.

**Implementation**:
- Hydrated database contains all issues from all repos
- Dependencies stored by ID only (no repo qualifier needed)
- `bd ready` checks dependency graph across all repos
- Writes route back to correct JSONL via `source_repo` metadata

### 3. Routing Mechanism: **Config-Based, No Schema Changes**

**Decision**: Use config-based routing + explicit `--repo` flag. No new schema fields.

**Rationale**:
- `IssueType` already exists and is used semantically (bug, feature, task, epic, chore)
- Labels are used semantically by VC (`discovered:blocker`, `no-auto-claim`)
- Routing is a storage concern, not issue metadata
- Simpler: auto-detect maintainer vs contributor from config
- Discovered issues inherit parent's `source_repo` automatically

### 4. Performance: **Smart Caching with File Mtime Tracking**

**Decision**: SQLite DB is the cache, JSONLs are source of truth.

**Implementation**:
```go
type MultiRepoStorage struct {
    repos      []RepoConfig
    db         *sql.DB
    repoMtimes map[string]time.Time  // Track file modification times
}

func (s *MultiRepoStorage) GetReadyWork(ctx) ([]Issue, error) {
    // Fast path: check if ANY JSONL changed
    needSync := false
    for repo, jsonlPath := range s.jsonlPaths() {
        currentMtime := stat(jsonlPath).ModTime()
        if currentMtime.After(s.repoMtimes[repo]) {
            needSync = true
            s.repoMtimes[repo] = currentMtime
        }
    }

    // Only re-hydrate if something changed
    if needSync {
        s.rehydrate()  // Expensive but rare
    }

    // Query is fast (in-memory SQLite)
    return s.db.Query("SELECT * FROM issues WHERE ...")
}
```

**Rationale**: VC's polling loop (every 5-10 seconds) requires sub-second queries. File stat is microseconds, re-parsing only when needed.

#### Performance Benchmarks and Targets

**Critical for library consumers (VC) with automated polling.**

##### Performance Targets

Based on VC's polling loop requirements (every 5-10 seconds):

| Operation | Target | Rationale |
|-----------|--------|-----------|
| **File stat** (per repo) | <1ms | Checking mtime of N JSONLs must be negligible |
| **Hydration** (full re-parse) | <500ms | Only happens when JSONL changes, rare in polling loop |
| **Query** (from cached DB) | <10ms | Common case: no JSONL changes, pure SQLite query |
| **Total GetReadyWork()** | <100ms | VC's hard requirement for responsive executor |

##### Scale Testing Targets

Test at multiple repo counts to ensure scaling:

| Repo Count | File Stat Total | Hydration (worst case) | Query (cached) | Total (cached) |
|------------|-----------------|------------------------|----------------|----------------|
| **N=1** (baseline) | <1ms | <200ms | <5ms | <10ms |
| **N=3** (typical) | <3ms | <500ms | <10ms | <20ms |
| **N=10** (edge case) | <10ms | <2s | <50ms | <100ms |

**Assumptions**:
- JSONL size: <25k per repo (see Design Principles)
- SQLite: In-memory mode (`:memory:` or `file::memory:?cache=shared`)
- Cached case: No JSONL changes since last hydration (99% of polling loops)

##### Benchmark Suite (To Be Implemented)

```go
// benchmark/multi_repo_test.go

func BenchmarkFileStatOverhead(b *testing.B) {
    // Test: Stat N JSONL files
    // Target: <1ms per repo
}

func BenchmarkHydrationN1(b *testing.B) {
    // Test: Full hydration from 1 JSONL (20k file)
    // Target: <200ms
}

func BenchmarkHydrationN3(b *testing.B) {
    // Test: Full hydration from 3 JSONLs (20k each)
    // Target: <500ms
}

func BenchmarkHydrationN10(b *testing.B) {
    // Test: Full hydration from 10 JSONLs (20k each)
    // Target: <2s
}

func BenchmarkQueryCached(b *testing.B) {
    // Test: GetReadyWork() with no JSONL changes
    // Target: <10ms
}

func BenchmarkGetReadyWorkN3(b *testing.B) {
    // Test: Realistic polling loop (3 repos, cached)
    // Target: <20ms total
}
```

##### Performance Optimization Notes

If benchmarks fail to meet targets, optimization strategies:

1. **Parallel file stats**: Use goroutines to stat N JSONLs concurrently
2. **Incremental hydration**: Only re-parse changed repos, merge into DB
3. **Smarter caching**: Hash-based cache invalidation (mtime + file size)
4. **SQLite tuning**: `PRAGMA synchronous = OFF`, `PRAGMA journal_mode = MEMORY`
5. **Lazy hydration**: Defer hydration until first query after mtime change

##### Status

**Benchmarks**: ⏳ Not implemented yet (tracked in bd-wta)
**Targets**: ✅ Documented above
**Validation**: ⏳ Pending implementation

**Next steps**:
1. Implement benchmark suite in `benchmark/multi_repo_test.go`
2. Run benchmarks on realistic workloads (VC-sized DBs)
3. Document results in this section
4. File optimization issues if targets not met

### 5. Visibility Field: **Optional, Backward Compatible**

**Decision**: Add `visibility` as optional field, defaults to "canonical" if missing.

**Schema**:
```go
type Issue struct {
    // ... existing fields ...
    Visibility *string `json:"visibility,omitempty"`  // nil = canonical
}
```

**States**:
- `local`: Personal planning only
- `proposed`: Staged for upstream PR
- `canonical`: Accepted by upstream (or default for existing issues)

**Orthogonality**: Visibility and Status are independent:
- `status: in_progress, visibility: local` → Working on personal planning
- `status: open, visibility: proposed` → Proposed to upstream, awaiting review

### 6. Library API Stability: **Transparent Hydration**

**Decision**: Hybrid approach - transparent by default, explicit opt-in available.

**Backward Compatible**:
```go
// Existing code keeps working - reads config.toml automatically
store, err := beadsLib.NewSQLiteStorage(".beads/vc.db")
```

**Explicit Override**:
```go
// Library consumers can override config
cfg := beadsLib.Config{
    Primary: ".beads/vc.db",
    Additional: []string{"~/.beads-planning"},
}
store, err := beadsLib.NewStorageWithConfig(cfg)
```

### 7. ACID Guarantees: **Per-Repo File Locking**

**Decision**: Use file-based locks per JSONL, atomic within single repo.

**Implementation**:
```go
func (s *Storage) UpdateIssue(issue Issue) error {
    sourceRepo := issue.SourceRepo

    // Lock that repo's JSONL
    lock := flock(sourceRepo + "/beads.jsonl.lock")
    defer lock.Unlock()

    // Read-modify-write
    issues := s.readJSONL(sourceRepo)
    issues.Update(issue)
    s.writeJSONL(sourceRepo, issues)

    // Update in-memory DB
    s.db.Update(issue)
}
```

**Limitation**: Cross-repo transactions are NOT atomic (acceptable, rare use case).

#### Compatibility with Exclusive Lock Protocol

The per-repo file locking (Decision #7) is **fully compatible** with the existing exclusive lock protocol (see [EXCLUSIVE_LOCK.md](../EXCLUSIVE_LOCK.md)).

**How they work together**:

1. **Exclusive locks are daemon-level**: The `.beads/.exclusive-lock` prevents the bd daemon from operating on a specific database
2. **File locks are operation-level**: Per-JSONL file locks (`flock`) ensure atomic read-modify-write for individual operations
3. **Different scopes, complementary purposes**:
   - Exclusive lock: "This entire database is off-limits to the daemon"
   - File lock: "This specific JSONL is being modified right now"

**Multi-repo behavior**:

With multi-repo configuration, each repo can have its own exclusive lock:

```bash
# VC executor locks its primary database
.beads/.exclusive-lock           # Locks primary repo operations

# Planning repo can be locked independently
~/.beads-planning/.exclusive-lock  # Locks planning repo operations
```

**When both are active**:
- If primary repo is locked: Daemon skips all operations on primary, but can still sync planning repo
- If planning repo is locked: Daemon skips planning repo, but can still sync primary
- If both locked: Daemon skips entire multi-repo workspace

**No migration needed for library consumers**:

Existing VC code (v0.17.3+) using exclusive locks will continue to work:
```go
// VC's existing lock acquisition
lock, err := types.NewExclusiveLock("vc-executor", "1.0.0")
lockPath := filepath.Join(".beads", ".exclusive-lock")
os.WriteFile(lockPath, data, 0644)

// Works the same with multi-repo:
// - Locks .beads/ (primary repo)
// - Daemon skips primary, can still sync ~/.beads-planning if configured
```

**Atomic multi-repo locking**:

If a library consumer needs to lock **all** repos atomically:

```go
// Lock all configured repos
repos := []string{".beads", filepath.ExpandUser("~/.beads-planning")}
for _, repo := range repos {
    lockPath := filepath.Join(repo, ".exclusive-lock")
    os.WriteFile(lockPath, lockData, 0644)
}
defer func() {
    for _, repo := range repos {
        os.Remove(filepath.Join(repo, ".exclusive-lock"))
    }
}()

// Now daemon skips all repos until locks released
```

**Summary**: No breaking changes. Exclusive locks work per-repo in multi-repo configs, preventing daemon interference at repo granularity.

## Key Learnings from VC Feedback

The VC project (VibeCoder) provided critical feedback as a real downstream consumer that uses beads as a library. Key insights:

### 1. Two Consumer Models

Beads has two distinct consumer types:
- **CLI users**: Use `bd` commands directly
- **Library consumers**: Use `beadsLib` in Go/TypeScript/etc. (like VC)

Multi-repo must work transparently for both.

### 2. Performance is Critical for Automation

VC's executor polls `GetReadyWork()` every 5-10 seconds. Multi-repo hydration must:
- Use smart caching (file mtime tracking)
- Avoid re-parsing JSONLs on every query
- Keep queries sub-second (ideally <100ms)

### 3. Special Labels Must Work Across Repos

VC uses semantic labels that must work regardless of repo:
- `discovered:blocker` - Auto-generated blocker issues (priority boost)
- `discovered:related` - Auto-generated related work
- `no-auto-claim` - Prevent executor from claiming
- `baseline-failure` - Self-healing baseline failures

These are **semantic labels**, not routing labels. Don't overload labels for routing.

### 4. Discovered Issues Routing

When VC's analysis phase auto-creates issues with `discovered:blocker` label, they should:
- Inherit parent's `source_repo` automatically
- Stay co-located with related work
- Not require manual routing decisions

### 5. Library API Stability is Non-Negotiable

VC's code uses `beadsLib.NewSQLiteStorage()`. Must not break. Solution:
- Read `.beads/config.toml` automatically (transparent)
- Provide `NewStorageWithConfig()` for explicit override
- Hydration happens at storage layer, invisible to library consumers

## Remaining Open Questions

1. **Sync semantics**: When upstream accepts a proposed issue and modifies it, how to sync back?
   - Option A: Mark as "accepted" in planning repo, keep both copies
   - Option B: Delete from planning repo (it's now canonical)
   - Option C: Keep in planning repo but mark as read-only mirror

2. **Discovery**: How do users learn about this feature?
   - Auto-prompt when detecting fork/contributor status?
   - Docs + examples?
   - `bd init --contributor` wizard?

3. **Metadata fields**: Should `source_repo` be exposed in JSON export, or keep it internal to storage layer?

4. **Proposed issue lifecycle**: What happens to proposed issues after PR is merged/rejected?
   - Auto-delete from planning repo?
   - Mark as "accepted" or "rejected"?
   - Manual cleanup via `bd withdraw`?

## Success Metrics

How we'll know this works:

1. **Zero pollution**: No contributor planning issues accidentally merged upstream
2. **Multi-clone sync**: Workers on different machines see consistent state (via VCS sync)
3. **Flexibility**: Users can configure for their workflow (personas, phases, etc.)
4. **Backwards compat**: Existing single-repo users unaffected
5. **VCS-agnostic**: Works with git, jj, hg, sl, or no VCS

## Next Actions

Suggested epics/issues to create (can be done in follow-up session):

1. **Epic: Multi-repo hydration layer**
   - Design schema for source_repo metadata
   - Implement config parsing for repos.additional
   - Build hydration logic (read from N JSONLs)
   - Build write routing (write to correct JSONL)

2. **Epic: Proposal workflow**
   - Implement `bd propose` command
   - Implement `bd withdraw` command
   - Implement `bd accept` command (maintainer only)
   - Design visibility state machine

3. **Epic: Auto-routing**
   - Detect maintainer vs contributor status
   - Implement routing rules (label, priority, custom)
   - Make `bd add` route to correct repo

4. **Epic: VCS-agnostic sync**
   - Detect VCS type per repo
   - Implement sync adapters (git, jj, hg, sl)
   - Handle mixed-VCS multi-repo configs

5. **Epic: Migration and onboarding**
   - Write migration guide
   - Implement `bd migrate` command
   - Create init wizards for common scenarios
   - Update documentation

---

## Summary and Next Steps

This document represents the design evolution for multi-repo support in beads, driven by:

1. **Original problem** (GitHub #207): Contributors' personal planning pollutes upstream PRs
2. **Core insight**: Beads is a separate communication channel that happens to use VCS
3. **VC feedback**: Real-world library consumer with specific performance and API stability needs

### Final Architecture

**Solution #4 (Separate Repos)** with these refinements:

- **N ≥ 1 repos**: Single repo (N=1) is default, multi-repo is opt-in
- **VCS-agnostic**: Works with git, jj, hg, sapling, or no VCS
- **Config-based routing**: No schema changes, auto-detect maintainer vs contributor
- **Smart caching**: File mtime tracking, SQLite DB as cache layer
- **Transparent hydration**: Library API remains stable, config-driven
- **Global namespace**: Hash-based IDs with timestamp + random for uniqueness
- **Cross-repo dependencies**: Fully supported, transparent to users
- **Discovered issues**: Inherit parent's source_repo automatically

### Why This Design Wins

1. **Zero PR pollution**: Separate git histories = impossible to accidentally merge planning
2. **Git ledger preserved**: All repos are VCS-tracked, full forensic capability
3. **Maximum flexibility**: Supports OSS contributors, multi-phase dev, multi-persona workflows
4. **Backward compatible**: Existing single-repo users unchanged
5. **Performance**: Sub-second queries even with polling loops
6. **Library-friendly**: Transparent to downstream consumers like VC

### Related Documents

- Original issue: GitHub #207
- VC feedback: `./vc-feedback-on-multi-repo.md`
- Implementation tracking: TBD (epics to be created)

### Status

**Design**: ✅ Complete (pending resolution of open questions)
**Implementation**: ⏳ Not started
**Target**: TBD

Last updated: 2025-11-03

# Branch-Based Namespace Implementation Status

This document tracks the implementation of branch-based issue namespacing for Beads (see `docs/proposals/BRANCH_NAMESPACING.md` for the full design).

## Completed

### Core ID Parsing & Resolution
- ✅ Created `internal/namespace/id.go` with `IssueID` struct
- ✅ Implemented `ParseIssueID()` with multi-format resolution
  - Short form: `a3f2` (hash only, uses context)
  - Branch-hash: `fix-auth-a3f2` (uses context project)
  - Qualified: `project:hash` or `project:branch-hash`
- ✅ String output methods
  - `String()` → fully qualified: `beads:fix-auth-a3f2`
  - `Short()` → context-aware: `fix-auth-a3f2` (or `a3f2` for main)
  - `ShortWithBranch()` → always shows branch: `main-a3f2`
- ✅ Comprehensive tests (13 test cases per resolution rules in spec)
- ✅ All namespace tests passing

### Sources Configuration
- ✅ Created `internal/namespace/sources.go` for `.beads/sources.yaml`
- ✅ `SourceConfig` struct with upstream/fork/local precedence
- ✅ Load/save configuration files
- ✅ Project management API (AddProject, SetProjectFork, SetProjectLocal)
- ✅ All sources configuration tests passing

### Data Model Changes
- ✅ Added `Project` and `Branch` fields to `Issue` type
  - Both marked as `omitempty` in JSON for backward compatibility
  - Properly documented in struct field groups

### Database Schema
- ✅ Created migration `041_namespace_project_branch_columns.go`
  - Adds `project TEXT DEFAULT ''`
  - Adds `branch TEXT DEFAULT 'main'`
  - Creates indexes: `(project, branch)` and `(project)`
- ✅ Migration registered in `migrations.go` with description
- ✅ Migration follows existing pattern (idempotent, uses `pragma_table_info`)

## In Progress

### CLI Command Updates
- ⏳ Update `bd create` to accept `--project` and `--branch` flags
- ⏳ Update `bd show` to parse namespaced IDs
- ⏳ Update `bd list` to filter by branch/project
- ⏳ Update `bd promote` to move issues between branches
- ⏳ Add `bd sources` command for configuration management

### Backward Compatibility Layer
- ⏳ Old `bd-xxx` format → new `project:main-xxx` (auto-migration)
- ⏳ Support reading old-format issues.jsonl
- ⏳ Auto-detect project name from git remote on first use

### ID Generation
- ⏳ Update `GenerateIssueID()` to include project/branch
- ⏳ Ensure hash uniqueness within project/branch scope
- ⏳ Support `bd promote` semantic (moving issues between branches)

### Storage Layer Integration
- ⏳ Update `SQLiteStore.CreateIssue()` to populate project/branch
- ⏳ Update issue export (JSONL) to include namespace fields
- ⏳ Update issue import (JSONL) to handle namespace fields

## To Do

### Display & Formatting
- ⏳ Update display logic to show branch context when relevant
  - Short form if in same branch
  - Full form if different branch/project
- ⏳ Add `--full` flag to show fully qualified IDs

### Workflow Support
- ⏳ Implement `bd promote` command
  - `bd promote hash-a3f2 --to main`
  - Moves issue from feature branch to main
- ⏳ PullRequest workflow: auto-exclude branch-scoped issues

### Multi-Repo Coordination
- ⏳ Extend Gas Town routes to understand namespace prefixes
- ⏳ Cross-fork issue references (e.g., `upstream:main-c4d8`)
- ⏳ Federated issue tracking across forks

### Testing
- ⏳ Integration tests with SQLite
- ⏳ Migration tests (verify old data converts correctly)
- ⏳ CLI command tests
- ⏳ Multi-repo tests

### Documentation
- ⏳ User guide for namespace syntax
- ⏳ Migration guide for existing projects
- ⏳ CLI reference updates
- ⏳ Examples and workflow documentation

## Design Decisions

### Delimiter Vocabulary
- `:` = project boundary (semantic: `::` in Rust, `/` in Go imports)
- `-` = separates branch from hash (and within branch names)
- `.` = hierarchical children (existing: `bd-a3f2.1`)

### Field Defaults
- `project`: Empty string (`""`) in database, auto-populated from context
- `branch`: Defaults to `"main"` if not specified
- `hash`: Required, 4-8 base36 characters

### Backward Compatibility Strategy
1. Old IDs: `bd-a3f2` → parse as `project="beads"` (from config), `branch="main"`, `hash="a3f2"`
2. Auto-migration: On first run, populate project/branch fields for existing issues
3. Old format still readable/writable temporarily (deprecation period)

### Sources Configuration
Like `go.mod` or `package.json`:
- Single source of truth for project origins
- Supports: upstream (canonical), fork (user's), local (override)
- Lazy-loaded, optional (assumes defaults if missing)

## Related Files

### Code
- `internal/namespace/` - ID parsing, sources configuration
- `internal/types/types.go` - Issue struct with namespace fields
- `internal/storage/sqlite/migrations/041_*.go` - Schema changes
- `internal/storage/sqlite/migrations.go` - Migration registration

### Documentation
- `docs/proposals/BRANCH_NAMESPACING.md` - Full design specification
- `docs/NAMESPACE_IMPLEMENTATION.md` - This file

## Next Steps

1. **Implement CLI layer** - Update command parsing to handle namespace syntax
2. **Integrate with storage** - Update CreateIssue/UpdateIssue to use namespace fields
3. **Test migrations** - Verify schema migration and data preservation
4. **Implement backwards compatibility** - Handle old format gracefully
5. **Update display logic** - Show namespace context in listings
6. **Document workflow** - Guide users on new namespace syntax

## Open Questions (from spec)

1. **Branch Deletion**: What happens to issues when their branch is deleted?
   - Option A: Orphaned (still accessible by full ID)
   - Option B: Auto-promote to main if not closed
   - Option C: Archive/tombstone with branch deletion event

2. **Project Name Derivation**: How to determine project name automatically?
   - Option A: Repository name from git remote
   - Option B: Explicit config during `bd init`
   - Option C: Directory name (current behavior for prefix)

3. **Merge Behavior**: When feature branch merges to main, what happens to branch-scoped issues?
   - Option A: Keep branch namespace (historical record)
   - Option B: Auto-migrate to main namespace
   - Option C: User choice per-issue via `bd promote`

4. **Cross-Fork References**: How to reference issues in other forks?
   - Option A: Configure fork as additional source
   - Option B: Full URL syntax
   - Option C: Named remotes like git

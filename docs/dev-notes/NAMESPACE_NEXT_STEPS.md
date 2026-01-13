# Namespace Implementation - Next Steps

## Phase 2: CLI Command Integration

**Status:** Complete (2026-01-13)

### Phase 2.1 Completion Summary

✅ **Completed:**
- Config package (`internal/config/config.go`): Added namespace configuration with helper functions
- Configfile package (`internal/configfile/configfile.go`): Added ProjectName and DefaultBranch fields, auto-detection from git remote
- CLI commands (`cmd/bd/create.go`): Added --branch flag with smart defaults (current git branch → config default → main)
- Issue creation: Updated CreateIssue to populate Project and Branch fields from context
- RPC protocol: Extended CreateArgs to support Project and Branch fields
- RPC server: Updated handler to propagate namespace fields to database
- Database queries: Added GetIssuesByBranch() for filtering by project/branch pair
- Storage interface: Updated to include GetIssuesByBranch method
- Memory storage: Implemented GetIssuesByBranch for testing

### Phase 2.2 Completion Summary

✅ **Completed:**
- Display layer updates (bd show, bd list): Show branch context when branch != "main"
- bd init integration: setupNamespaceConfig() prompts for project name and default branch
- bd promote command (`cmd/bd/promote.go`): Move issues between branches with UpdateIssue integration
- bd sources command (`cmd/bd/sources.go`): Manage .beads/sources.yaml for project source configuration
- RPC protocol extension: Added Branch field to UpdateArgs
- RPC server integration: Branch field handled in updatesFromArgs() function

Phase 2 is complete. Namespace CLI support is fully integrated.

### Key Integration Points

1. **Config Package** (`internal/configfile`)
   - Add optional `project_name` and `default_branch` to `Config` struct
   - Auto-detect project from git remote: `git config --get remote.origin.url` → repo name
   - Store in `~/.config/bd/config.yaml`

2. **Database Storage** (`internal/storage/sqlite`)
   - Update `CreateIssue()` to set `project` and `branch` from context
   - Update `UpdateIssue()` to allow branch changes (promote)
   - Add `GetIssuesByBranch()` query for filtering
   - Add `GetProject()` to issues table queries

3. **ID Generation** (`internal/storage/sqlite/ids.go`)
   - `GenerateIssueID()` now returns just the hash (not prefixed)
   - Project/branch stored separately in Issue fields
   - Hash uniqueness scoped to `(project, branch)` pair

4. **CLI Commands** (`cmd/bd/`)
   - `bd init` - Ask for project name, detect from git, or use directory name
   - `bd create` - Accept `--branch` flag, default to current git branch
   - `bd list` - Show branch context, filter by `--branch`
   - `bd show` - Parse namespaced IDs
   - `bd promote` - Move issue from one branch to another
   - `bd sources` - Manage sources.yaml

### Command Changes Detail

```bash
# Initialize with namespace support
bd init
# → "Detect project: beads" (from git remote)
# → "Default branch: main"
# → Creates .beads/sources.yaml with upstream info

# Create issue in current git branch
bd create "Fix bug" --priority 1
# → Creates: beads:fix-bug-a3f2 (if on fix-bug branch)

# Create in main branch explicitly
bd create "Document fix" --branch main --priority 2
# → Creates: beads:main-b7c9

# List issues
bd list
# fix-bug-a3f2   Fix bug
# fix-bug-c3d4   Another task
# main-b7c9      Document fix (different branch shown with branch)

# Show issue
bd show fix-bug-a3f2
# → Same as: bd show beads:fix-bug-a3f2

# Promote issue to main
bd promote fix-bug-a3f2 --to main
# → Moves: beads:fix-bug-a3f2 → beads:main-a3f2

# Manage sources
bd sources add upstream github.com/steveyegge/beads
bd sources set-fork github.com/matt/beads
```

### Migration Strategy

1. **Graceful Degradation**
   - Old code reads new format: ✅ (project/branch fields omitted in JSON)
   - New code reads old format: ✅ (empty project → use config default)
   - Transition period: Both formats work

2. **Data Migration**
   - Populate missing project/branch on first read
   - Background task: `bd migrate --to-namespaced` (optional, explicit)
   - Export includes namespace fields (backward compat: omit if main)

3. **Testing Path**
   - Unit tests: `namespace` package ✅ (done)
   - Integration tests: storage layer + CLI
   - E2E tests: real git repos, branch changes
   - Compatibility tests: old/new format interop

## Phase 3: Workflow Support

Once CLI is integrated, implement semantic workflows.

### Branch Scoping in PR Workflow

```yaml
# Pull request checks
pd create --type pr
  - Excludes branch-scoped issues from main automatically
  - Includes only main-branch issues in PR description
  - Reviewers see: "5 issues on main, 12 on feature branch"
```

### Issue Curation Pipeline

```bash
# During development on feature branch
# → Many working issues: beads:fix-auth-a3f2, ...b7c9, ...c4d8

# Before PR: promote relevant ones
bd promote a3f2 --to main  # "Implement auth"
bd promote b7c9 --to main  # "Add tests"
# Keep c4d8 on feature branch (exploratory)

# PR includes: a3f2, b7c9 (now on main)
# Feature branch issues: c4d8 (stays in fix-auth)

# After merge to upstream:
# → Feature branch issues can be: archived, deleted, or kept
# → Main branch issues synced to upstream
```

## Phase 4: Multi-Repo Coordination

Integrate with Gas Town routes and cross-repo issue tracking.

### Required Changes to Gas Town

1. **Route Prefix Parsing**
   - Current: `beads-hq-91t` (prefix + issue)
   - Future: `beads-beads:main-91t` (prefix + namespaced ID)
   - Or: Auto-derive project from route path

2. **Rig Configuration**
   - Store project name per rig
   - Routes file: extend with `project` field?
   ```jsonl
   {"prefix":"hq-","path":".","project":"gt"}
   {"prefix":"beads-","path":"../beads-rig","project":"beads"}
   ```

3. **Cross-Rig References**
   - Issue in gt: `gt:main-91t` (Beads fork orchestration issue)
   - Issue in beads-rig: `beads:main-xyz` (actual implementation)
   - `bd dep add gt:91t beads:xyz` - Create cross-repo dependency

## Implementation Order

1. **Week 1: CLI Integration**
   - Config package updates (auto-detect project)
   - ID generation in storage layer
   - `bd init` and `bd create` with namespace support

2. **Week 2: Display & Listing**
   - `bd list` with branch filtering
   - `bd show` parsing namespaced IDs
   - Context-aware display formatting

3. **Week 3: Promotion Workflow**
   - `bd promote` command
   - Branch migration logic
   - Tests and documentation

4. **Week 4: Gas Town Integration**
   - Route parsing updates
   - Cross-repo dependencies
   - E2E workflow tests

## Testing Strategy

### Unit Tests
- ✅ Namespace parsing (done)
- ✅ Sources configuration (done)
- ⏳ ID generation with project/branch
- ⏳ Database queries (GetByBranch, GetProject)

### Integration Tests
- ✅ Storage layer + namespace fields (Phase 2.1)
- ✅ CLI command parsing (Phase 2.2: promote, sources)
- ⏳ Migration (old → new format)
- ⏳ End-to-end: bd promote workflow
- ⏳ End-to-end: bd sources workflow

### E2E Tests
- ⏳ Real git repos with multiple branches
- ⏳ Issue creation on different branches
- ⏳ Promotion workflow
- ⏳ PR integration

### Compatibility Tests
- ⏳ Old issues.jsonl files
- ⏳ Mixed format databases
- ⏳ Upgrade paths

## Files to Modify

### Core Storage
- `internal/storage/sqlite/queries.go` - UpdateIssue, GetIssuesByBranch
- `internal/storage/sqlite/ids.go` - GenerateIssueID to handle project/branch scope
- `internal/storage/storage.go` - Interface updates for namespace methods

### CLI Commands
- ✅ `cmd/bd/create.go` - Add --branch flag (Phase 2.1)
- ✅ `cmd/bd/list.go` - Add --branch filtering (Phase 2.1)
- ✅ `cmd/bd/show.go` - Display branch context (Phase 2.2)
- ✅ `cmd/bd/promote.go` - New: promote issues between branches (Phase 2.2)
- ✅ `cmd/bd/sources.go` - New: manage sources.yaml (Phase 2.2)
- ✅ `cmd/bd/init.go` - namespace config setup (Phase 2.2)

### Configuration
- `internal/configfile/config.go` - Add project_name, default_branch
- `internal/config/` - Load/apply namespace defaults

### Display Layer
- `internal/ui/format.go` - Context-aware ID formatting
- `internal/ui/list.go` - Show branch context

## Open Questions to Answer

1. **Current Git Branch Detection**
   - Use `git rev-parse --abbrev-ref HEAD` during `bd create`?
   - Or require explicit `--branch` flag?
   - Option: Auto-detect with override capability

2. **Default Project Name**
   - Auto: Repository name from `git config --get remote.origin.url`
   - Or: Explicit input during `bd init`
   - Or: Config file with `project_name`

3. **Issue ID in Commands**
   - Accept: `a3f2`, `fix-auth-a3f2`, `beads:main-a3f2`
   - Which forms should be supported in each command?
   - When to require explicit project?

4. **Backward Compatibility Duration**
   - How long to support old `bd-xxx` format?
   - Deprecation period: 1 release, 2 releases, indefinite?
   - Migration command: mandatory or optional?

## Success Criteria

- [x] Namespace parsing logic complete and tested (Phase 1)
- [x] Sources configuration structure in place (Phase 1)
- [x] CLI commands accept namespace syntax (Phase 2.2: promote, sources)
- [ ] Old format automatically upgraded on read (Phase 3+)
- [x] All existing tests pass (continuous)
- [ ] New namespace tests >90% coverage (Phase 3+)
- [ ] PR workflow excludes branch-scoped issues (Phase 3)
- [ ] Gas Town routes recognize namespaced IDs (Phase 4)
- [ ] User can work on fork feature branches without conflicts (Phase 3+)

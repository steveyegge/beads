# Instructions for AI Agents Working on Beads

## Project Overview

This is **beads** (command: `bd`), an issue tracker designed for AI-supervised coding workflows. We dogfood our own tool!

## Issue Tracking

We use bd (beads) for issue tracking instead of Markdown TODOs or external tools.

### Quick Reference

```bash
# Find ready work (no blockers)
bd ready --json

# Create new issue
bd create "Issue title" -t bug|feature|task -p 0-4 -d "Description" --json

# Create with explicit ID (for parallel workers)
bd create "Issue title" --id worker1-100 -p 1 --json

# Update issue status
bd update <id> --status in_progress --json

# Link discovered work
bd dep add <discovered-id> <parent-id> --type discovered-from

# Complete work
bd close <id> --reason "Done" --json

# Show dependency tree
bd dep tree <id>

# Get issue details
bd show <id> --json

# Import with collision detection
bd import -i .beads/issues.jsonl --dry-run             # Preview only
bd import -i .beads/issues.jsonl --resolve-collisions  # Auto-resolve
```

### Workflow

1. **Check for ready work**: Run `bd ready` to see what's unblocked
2. **Claim your task**: `bd update <id> --status in_progress`
3. **Work on it**: Implement, test, document
4. **Discover new work**: If you find bugs or TODOs, create issues:
   - `bd create "Found bug in auth" -t bug -p 1 --json`
   - Link it: `bd dep add <new-id> <current-id> --type discovered-from`
5. **Complete**: `bd close <id> --reason "Implemented"`
6. **Export**: Changes auto-sync to `.beads/issues.jsonl` (5-second debounce)

### Issue Types

- `bug` - Something broken that needs fixing
- `feature` - New functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature composed of multiple issues
- `chore` - Maintenance work (dependencies, tooling)

### Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (nice-to-have features, minor bugs)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

### Dependency Types

- `blocks` - Hard dependency (issue X blocks issue Y)
- `related` - Soft relationship (issues are connected)
- `parent-child` - Epic/subtask relationship
- `discovered-from` - Track issues discovered during work

Only `blocks` dependencies affect the ready work queue.

## Development Guidelines

### Code Standards

- **Go version**: 1.21+
- **Linting**: `golangci-lint run ./...` (baseline warnings documented in LINTING.md)
- **Testing**: All new features need tests (`go test ./...`)
- **Documentation**: Update relevant .md files

### File Organization

```
beads/
├── cmd/bd/              # CLI commands
├── internal/
│   ├── types/           # Core data types
│   └── storage/         # Storage layer
│       └── sqlite/      # SQLite implementation
├── examples/            # Integration examples
└── *.md                 # Documentation
```

### Before Committing

1. **Run tests**: `go test ./...`
2. **Run linter**: `golangci-lint run ./...` (ignore baseline warnings)
3. **Update docs**: If you changed behavior, update README.md or other docs
4. **Commit**: Issues auto-sync to `.beads/issues.jsonl` and import after pull

### Git Workflow

**Auto-sync is now automatic!** bd automatically:
- **Exports** to JSONL after any CRUD operation (5-second debounce)
- **Imports** from JSONL when it's newer than DB (e.g., after `git pull`)

```bash
# Make changes and create/update issues
bd create "Fix bug" -p 1
bd update bd-42 --status in_progress

# JSONL is automatically updated after 5 seconds

# Commit (JSONL is already up-to-date)
git add .
git commit -m "Your message"

# After pull - JSONL is automatically imported
git pull  # bd commands will auto-import the updated JSONL
bd ready  # Fresh data from git!
```

**Optional**: Use the git hooks in `examples/git-hooks/` for immediate export (no 5-second wait) and guaranteed import after git operations. Not required with auto-sync enabled.

### Handling Import Collisions

When merging branches or pulling changes, you may encounter ID collisions (same ID, different content). bd detects and safely handles these:

**Check for collisions after merge:**
```bash
# After git merge or pull
bd import -i .beads/issues.jsonl --dry-run

# Output shows:
# === Collision Detection Report ===
# Exact matches (idempotent): 15
# New issues: 5
# COLLISIONS DETECTED: 3
#
# Colliding issues:
#   bd-10: Fix authentication (conflicting fields: [title, priority])
#   bd-12: Add feature (conflicting fields: [description, status])
```

**Resolve collisions automatically:**
```bash
# Let bd resolve collisions by remapping incoming issues to new IDs
bd import -i .beads/issues.jsonl --resolve-collisions

# bd will:
# - Keep existing issues unchanged
# - Assign new IDs to colliding issues (bd-25, bd-26, etc.)
# - Update ALL text references and dependencies automatically
# - Report the remapping with reference counts
```

**Important**: The `--resolve-collisions` flag is safe and recommended for branch merges. It preserves the existing database and only renumbers the incoming colliding issues. All text mentions like "see bd-10" and dependency links are automatically updated to use the new IDs.

**Manual resolution** (alternative):
If you prefer manual control, resolve the Git conflict in `.beads/issues.jsonl` directly, then import normally without `--resolve-collisions`.

## Current Project Status

Run `bd stats` to see overall progress.

### Active Areas

- **Core CLI**: Mature, but always room for polish
- **Examples**: Growing collection of agent integrations
- **Documentation**: Comprehensive but can always improve
- **MCP Server**: Planned (see bd-5)
- **Migration Tools**: Planned (see bd-6)

### 1.0 Milestone

We're working toward 1.0. Key blockers tracked in bd. Run:
```bash
bd dep tree bd-8  # Show 1.0 epic dependencies
```

## Common Tasks

### Adding a New Command

1. Create file in `cmd/bd/`
2. Add to root command in `cmd/bd/main.go`
3. Implement with Cobra framework
4. Add `--json` flag for agent use
5. Add tests in `cmd/bd/*_test.go`
6. Document in README.md

### Adding Storage Features

1. Update schema in `internal/storage/sqlite/schema.go`
2. Add migration if needed
3. Update `internal/types/types.go` if new types
4. Implement in `internal/storage/sqlite/sqlite.go`
5. Add tests
6. Update export/import in `cmd/bd/export.go` and `cmd/bd/import.go`

### Adding Examples

1. Create directory in `examples/`
2. Add README.md explaining the example
3. Include working code
4. Link from `examples/README.md`
5. Mention in main README.md

## Questions?

- Check existing issues: `bd list`
- Look at recent commits: `git log --oneline -20`
- Read the docs: README.md, TEXT_FORMATS.md, EXTENDING.md
- Create an issue if unsure: `bd create "Question: ..." -t task -p 2`

## Important Files

- **README.md** - Main documentation (keep this updated!)
- **EXTENDING.md** - Database extension guide
- **TEXT_FORMATS.md** - JSONL format analysis
- **CONTRIBUTING.md** - Contribution guidelines
- **SECURITY.md** - Security policy

## Pro Tips for Agents

- Always use `--json` flags for programmatic use
- Link discoveries with `discovered-from` to maintain context
- Check `bd ready` before asking "what next?"
- Auto-sync is automatic! JSONL updates after CRUD ops, imports after git pull
- Use `--no-auto-flush` or `--no-auto-import` to disable automatic sync if needed
- Use `bd dep tree` to understand complex dependencies
- Priority 0-1 issues are usually more important than 2-4
- Use `--dry-run` to preview import collisions before resolving
- Use `--resolve-collisions` for safe automatic branch merges
- Use `--id` flag with `bd create` to partition ID space for parallel workers (e.g., `worker1-100`, `worker2-500`)

## Building and Testing

```bash
# Build
go build -o bd ./cmd/bd

# Test
go test ./...

# Test with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run locally
./bd init --prefix test
./bd create "Test issue" -p 1
./bd ready
```

## Release Process (Maintainers)

1. Update version in code (if applicable)
2. Update CHANGELOG.md (if exists)
3. Run full test suite
4. Tag release: `git tag v0.x.0`
5. Push tag: `git push origin v0.x.0`
6. GitHub Actions handles the rest

---

**Remember**: We're building this tool to help AI agents like you! If you find the workflow confusing or have ideas for improvement, create an issue with your feedback.

Happy coding! 🔗

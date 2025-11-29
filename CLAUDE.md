# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**beads** (`bd`) is a git-backed issue tracker designed for AI-supervised coding workflows. The project dogfoods its own tool for all task tracking.

**Key Innovation**: Acts like a centralized database but is actually distributed via git, using SQLite (local cache) + JSONL (git-tracked) with intelligent auto-sync.

## Essential Commands

### Development Workflow

```bash
# Build and test
go build -o bd ./cmd/bd
go test ./...                           # Run all tests
go test -short ./...                    # Skip slow tests
BEADS_DB=/tmp/test.db go test ./...     # Use temp DB for tests

# Using Makefile
make build                              # Build binary
make test                               # Run test suite
make install                            # Install to GOPATH/bin
make clean                              # Clean artifacts

# Linting (baseline warnings documented in docs/LINTING.md)
golangci-lint run ./...

# Version management
./scripts/bump-version.sh 0.9.3          # Update versions, show diff
./scripts/bump-version.sh 0.9.3 --commit # Update and commit

# Testing individual packages
go test ./internal/storage/sqlite/      # Test storage layer
go test ./cmd/bd/                       # Test CLI layer
go test ./internal/rpc/                 # Test RPC layer
```

### Issue Tracking with bd

**CRITICAL**: This project uses `bd` for ALL task tracking. Do NOT create markdown TODO lists.

```bash
# Find work
bd ready --json                          # Unblocked issues
bd stale --days 30 --json                # Forgotten issues

# Create issues (ALWAYS include --description)
bd create "Title" --description="Context" -t bug|feature|task -p 0-4 --json
bd create "Found bug" --description="Details" -p 1 --deps discovered-from:<parent-id> --json

# Update and complete
bd update <id> --status in_progress --json
bd close <id> --reason "Done" --json

# Search and filter
bd list --status open --priority 1 --json
bd show <id> --json

# Sync (CRITICAL at end of session!)
bd sync                                  # Force immediate flush/commit/push
```

**Priority Levels:**
- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (default, nice-to-have)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

### Session-End Protocol

**ALWAYS run before finishing work:**

```bash
# 1. Sync issue tracker
bd sync

# 2. Run tests if code was changed
go test -short ./...

# 3. Commit and push
git add .
git commit -m "Description"
git push
```

## Architecture Overview

### Three-Layer Design

**1. Storage Layer** (`internal/storage/`)
- Interface-based design in `storage.go` (150+ lines defining Storage interface)
- SQLite implementation in `storage/sqlite/` (production backend)
- Memory implementation in `storage/memory/` (testing only)
- Extensible via `UnderlyingDB()` for custom tables

**2. RPC Layer** (`internal/rpc/`)
- Client/server using Unix sockets (Windows named pipes)
- Protocol defined in `protocol.go`
- Server split into focused files:
  - `server_core.go` - Core daemon logic
  - `server_issues_epics.go` - Issue/epic operations
  - `server_labels_deps_comments.go` - Labels/dependencies/comments
- Per-workspace daemons communicate via `.beads/bd.sock`

**3. CLI Layer** (`cmd/bd/`)
- Cobra-based commands (one file per command: `create.go`, `list.go`, etc.)
- Commands try daemon RPC first, fall back to direct database access
- All commands support `--json` for programmatic use
- Main entry point in `main.go`

### Distributed Database Pattern

The "magic" is auto-sync between SQLite and JSONL:

```
SQLite DB (.beads/beads.db, gitignored)
    ↕ auto-sync (5s debounce)
JSONL (.beads/issues.jsonl, git-tracked)
    ↕ git push/pull
Remote JSONL (shared across machines)
```

**Key Implementations:**
- Export: `cmd/bd/export.go`, `cmd/bd/autoflush.go`
- Import: `cmd/bd/import.go`, `cmd/bd/autoimport.go`
- Collision detection: `internal/importer/importer.go`
- Hash-based IDs (v0.20+): Automatic collision prevention across branches/workers

### Key Data Types

See `internal/types/types.go`:

- `Issue` - Core work item (title, description, status, priority, etc.)
- `Dependency` - Four types: blocks, related, parent-child, discovered-from
- `Label` - Flexible tagging system
- `Comment` - Threaded discussions
- `Event` - Full audit trail

### Directory Structure

```
beads/
├── cmd/bd/                    # CLI commands (Cobra-based)
│   ├── main.go               # Entry point
│   ├── create.go             # bd create command
│   ├── list.go               # bd list command
│   ├── daemon.go             # Daemon management
│   ├── doctor/               # Health checks and fixes
│   ├── setup/                # Initialization helpers
│   └── templates/            # Issue templates
├── internal/
│   ├── types/                # Core data types (Issue, Dependency, etc.)
│   ├── storage/              # Storage interface + implementations
│   │   ├── storage.go        # Interface definition
│   │   ├── sqlite/           # SQLite backend (production)
│   │   └── memory/           # In-memory backend (testing)
│   ├── rpc/                  # RPC client/server for daemon
│   ├── importer/             # JSONL import logic with collision detection
│   ├── export/               # JSONL export logic
│   ├── merge/                # Git merge driver for JSONL
│   ├── git/                  # Git operations
│   ├── daemon/               # Daemon lifecycle management
│   └── testutil/             # Testing utilities
├── integrations/
│   └── beads-mcp/            # MCP server (Python) for Claude Desktop
├── examples/
│   ├── python-agent/         # Example Python agent
│   ├── bash-agent/           # Example bash agent
│   └── git-hooks/            # Auto-sync git hooks
├── scripts/
│   ├── bump-version.sh       # Version management
│   ├── release.sh            # Release workflow
│   └── install.sh            # Installation script
└── .beads/
    ├── beads.db              # SQLite cache (gitignored)
    ├── issues.jsonl          # Git-tracked source of truth
    ├── deletions.jsonl       # Deletion manifest for sync
    └── config.yaml           # Repository configuration
```

## Testing Philosophy

- **Unit tests** - Live next to implementation (`*_test.go`)
- **Integration tests** - Use real SQLite (`:memory:` or temp files)
- **Script tests** - In `cmd/bd/testdata/*.txt` (see `scripttest_test.go`)
- **RPC tests** - Extensive isolation and edge case coverage

**Always use temp DB for tests:**
```bash
BEADS_DB=/tmp/test.db go test ./...
```

**Never pollute production database with test data.**

## Common Development Patterns

### Adding a New CLI Command

1. Create `cmd/bd/mycommand.go`
2. Define Cobra command structure
3. Add `--json` flag for programmatic use
4. Try daemon RPC first, fall back to direct DB
5. Add tests in `cmd/bd/mycommand_test.go`
6. Update documentation

See existing commands like `cmd/bd/create.go` for reference.

### Adding Storage Features

1. Update `internal/storage/storage.go` interface
2. Implement in `internal/storage/sqlite/`
3. Implement in `internal/storage/memory/` (for tests)
4. Add tests in both implementations
5. Update RPC protocol if needed (`internal/rpc/protocol.go`)

### Working with Daemon

- Each workspace gets its own daemon process
- Auto-starts on first command (unless `--no-daemon`)
- Socket at `.beads/bd.sock` (or `.beads/bd.pipe` on Windows)
- Version checking prevents mismatches after upgrades
- Manage with `bd daemons` command

**Use `--no-daemon` when:**
- Running in git worktrees (avoid socket conflicts)
- Testing daemon code itself
- Debugging daemon issues

## Important Notes

- Install git hooks for zero-lag sync: `bd hooks install`
- Run `bd sync` at end of agent sessions for immediate flush/commit/push
- Use `--json` flags for all programmatic use
- Hash-based IDs (v0.20+) eliminate collisions across branches/workers
- Auto-sync batches changes with 5-second debounce
- Check `bd info --whats-new` after upgrades
- Run `bd daemons killall` after upgrading bd to restart all daemons

## Key Documentation Files

- **AGENTS.md** - Complete workflow and development guide (primary reference)
- **AGENT_INSTRUCTIONS.md** - Detailed development procedures, testing, releases
- **README.md** - User-facing documentation
- **docs/ADVANCED.md** - Advanced features (rename, merge, compaction)
- **docs/EXTENDING.md** - How to add custom tables to database
- **docs/CONFIG.md** - Configuration system
- **docs/LABELS.md** - Complete label system guide
- **docs/CLI_REFERENCE.md** - Complete command reference
- **.github/copilot-instructions.md** - Concise version for GitHub Copilot

## Development Best Practices

- **Always read AGENTS.md first** for complete workflow context
- Use `bd create` with detailed `--description` for all new work
- Link discovered work with `--deps discovered-from:<parent-id>`
- Run `bd sync` before finishing work to push changes
- Check `bd ready` to find available work
- Use `bd duplicates --auto-merge` proactively to keep database clean
- Run linter before committing: `golangci-lint run ./...`
- Add tests for all new features
- Update docs when changing behavior

## Session Workflow for AI Agents

**At session start:**
```bash
bd info --whats-new              # Check recent changes (if bd upgraded)
bd ready --json                  # Find available work
```

**During work:**
```bash
bd update <id> --status in_progress
# ... implement, test, document ...
bd create "Found issue" --description="Details" --deps discovered-from:<current-id> --json
```

**At session end:**
```bash
bd sync                          # Sync issue tracker
go test -short ./...             # Run tests (if code changed)
git add . && git commit -m "..." && git push
```

## Pro Tips

- Use `bd dep tree <id>` to visualize complex dependencies
- Check for duplicates before creating: `bd list --json | grep -i "keyword"`
- Use `bd --no-auto-flush` to disable automatic sync during batch operations
- For multi-repo workflows, see `docs/MULTI_REPO_AGENTS.md`
- For Agent Mail (multi-agent coordination), see `docs/AGENT_MAIL.md`
- Hash IDs use progressive length (4-6 chars) based on database size
- Use `--dry-run` to preview import changes before applying

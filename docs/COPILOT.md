# Copilot Guide for Beads

This file provides architectural guidance when working with code in this repository.

## Project Overview

**beads** (command: `bd`) is a git-backed issue tracker for AI-supervised coding workflows. We dogfood our own tool.

**IMPORTANT**: See [AGENTS.md](../AGENTS.md) for complete workflow instructions, bd commands, and development guidelines.

## Architecture Overview

### Three-Layer Design

1. **Storage Layer** (`internal/storage/`)
   - Interface-based design in `storage.go`
   - SQLite implementation in `storage/sqlite/`
   - Memory backend in `storage/memory/` for testing
   - Extensions can add custom tables via `UnderlyingDB()`

2. **RPC Layer** (`internal/rpc/`)
   - Client/server architecture using Unix domain sockets (Windows: named pipes)
   - Protocol defined in `protocol.go`
   - Server split into focused files: `server_core.go`, `server_issues_epics.go`, etc.
   - Per-workspace daemons communicate via `.beads/bd.sock`

3. **CLI Layer** (`cmd/bd/`)
   - Cobra-based commands (one file per command: `create.go`, `list.go`, etc.)
   - Commands try daemon RPC first, fall back to direct database access
   - All commands support `--json` for programmatic use
   - Main entry point in `main.go`

### Distributed Database Pattern

The "magic" is in the auto-sync between SQLite and JSONL:

```
SQLite DB (.beads/beads.db, gitignored)
    ↕ auto-sync (5s debounce)
JSONL (.beads/issues.jsonl, git-tracked)
    ↕ git push/pull
Remote JSONL (shared across machines)
```

- **Write path**: CLI → SQLite → JSONL export → git commit
- **Read path**: git pull → JSONL import → SQLite → CLI
- **Hash-based IDs**: Automatic collision prevention (v0.20+)

Core implementation:
- Export: `cmd/bd/export.go`, `cmd/bd/autoflush.go`
- Import: `cmd/bd/import.go`, `cmd/bd/autoimport.go`
- Collision detection: `internal/importer/importer.go`

### Key Data Types

See `internal/types/types.go`:
- `Issue`: Core work item (title, description, status, priority, etc.)
- `Dependency`: Four types (blocks, related, parent-child, discovered-from)
- `Label`: Flexible tagging system
- `Comment`: Threaded discussions
- `Event`: Full audit trail

### Daemon Architecture

Each workspace gets its own daemon process:
- Auto-starts on first command (unless disabled)
- Handles auto-sync, batching, and background operations
- Socket at `.beads/bd.sock` (or `.beads/bd.pipe` on Windows)
- Version checking prevents mismatches after upgrades
- Manage with `bd daemons` command

## Key Files for Navigation

| Area | Files |
|------|-------|
| CLI commands | `cmd/bd/*.go` |
| Core types | `internal/types/types.go` |
| Storage interface | `internal/storage/storage.go` |
| SQLite implementation | `internal/storage/sqlite/*.go` |
| Import/export | `cmd/bd/import.go`, `cmd/bd/export.go` |
| Daemon | `internal/daemon/daemon.go` |
| RPC protocol | `internal/rpc/protocol.go` |

## Common Development Commands

```bash
# Build
go build -o bd ./cmd/bd

# Test
go test ./...
go test -coverprofile=coverage.out ./...

# Lint (baseline warnings in docs/LINTING.md)
golangci-lint run ./...

# Version management
./scripts/bump-version.sh 0.9.3 --commit
```

## Testing with Isolation

Always use a separate database for testing:

```bash
BEADS_DB=/tmp/test.db bd create "Test issue" -p 1
BEADS_DB=/tmp/test.db bd ready
```

## Testing Philosophy

- Unit tests live next to implementation (`*_test.go`)
- Integration tests use real SQLite databases (`:memory:` or temp files)
- Script-based tests in `cmd/bd/testdata/*.txt` (see `scripttest_test.go`)
- RPC layer has extensive isolation and edge case coverage

## Related Documentation

- [AGENTS.md](../AGENTS.md) - Complete AI agent workflows
- [CLI_REFERENCE.md](CLI_REFERENCE.md) - All commands documented
- [ARCHITECTURE.md](ARCHITECTURE.md) - Detailed system design
- [INTERNALS.md](INTERNALS.md) - Implementation deep-dive

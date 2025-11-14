# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**bd (beads)** is a Git-backed issue tracker designed for AI-supervised coding workflows. It provides a dependency-aware task management system that acts like a centralized database but syncs via Git using JSONL files.

## Build and Test Commands

### Build
```bash
go build -o bd ./cmd/bd
```

### Run Tests
```bash
# Quick tests for local development (skip slow integration tests)
go test -short ./...

# Full test suite (runs in CI)
go test ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Run Linter
```bash
golangci-lint run ./...
```
Note: Baseline warnings are documented in `docs/LINTING.md` - ignore these during development.

### Run Single Test
```bash
# Run specific test file
go test ./cmd/bd -run TestSpecificFunction -v

# Run with short flag to skip slow tests
go test -short ./internal/beads -run TestCreate -v
```

### Version Bumps
```bash
# Preview changes (shows diff)
./scripts/bump-version.sh 0.9.3

# Auto-commit version bump
./scripts/bump-version.sh 0.9.3 --commit
```
This updates ALL version files atomically (CLI, plugin, MCP server, docs).

## Architecture Overview

### Core Design Principles

1. **Distributed Database via Git**: SQLite cache (`.beads/*.db`, gitignored) + JSONL source of truth (`.beads/issues.jsonl`, committed). Auto-sync keeps them synchronized.

2. **Dual-Mode Operation**:
   - **Daemon mode** (default): Background daemon handles auto-sync, RPC operations, file watching
   - **Direct mode** (`--no-daemon`): Direct SQLite access for git worktrees or debugging

3. **Hash-Based Issue IDs**: Collision-resistant hash IDs (e.g., `bd-a1b2`, `bd-f14c`) with progressive length scaling based on database size. Enables multi-worker/multi-branch workflows without ID conflicts.

### Directory Structure

```
beads/
├── cmd/bd/                    # CLI commands and main entry point
│   ├── main.go                # Root command, daemon connection logic
│   ├── create.go, update.go   # CRUD commands
│   ├── daemon*.go             # Daemon lifecycle, RPC server, auto-sync
│   ├── export.go, import.go   # JSONL sync operations
│   └── *_test.go              # Command tests (use -short for local dev)
├── internal/
│   ├── beads/                 # Core business logic
│   ├── storage/               # Storage abstraction layer
│   │   ├── sqlite/            # SQLite implementation
│   │   └── memory/            # In-memory for tests
│   ├── rpc/                   # Daemon RPC protocol
│   ├── importer/              # JSONL import logic
│   ├── merge/                 # Git merge driver
│   ├── config/                # Viper-based configuration
│   └── types/                 # Core data types (Issue, Dependency, etc.)
├── integrations/
│   └── beads-mcp/             # Python MCP server for Claude/AI clients
├── examples/                  # Agent integration examples
└── docs/                      # Comprehensive documentation
```

### Key Architectural Patterns

**Storage Abstraction**: `internal/storage/storage.go` defines the interface. Implementations: SQLite (production), memory (tests). Commands use `store` variable initialized in `main.go`.

**Auto-Sync Pipeline**:
1. CRUD command modifies database
2. Daemon debouncer (30s window) batches changes
3. Export to `.beads/issues.jsonl`
4. Optional git commit/push (if `--auto-commit`/`--auto-push` enabled)
5. On `git pull`: JSONL mtime check triggers auto-import

**Daemon Architecture**:
- One daemon per workspace (`.beads/bd.sock`)
- Auto-starts on first command (unless `BEADS_AUTO_START_DAEMON=false`)
- Unix domain socket RPC (JSON protocol)
- Background goroutines: debouncer, file watcher, periodic sync
- Version mismatch detection with auto-restart

**Hash ID System**:
- UUIDv4 → SHA-256 → hex prefix (4-6 chars based on DB size)
- Thresholds: 500 issues → 5 chars, 1500 issues → 6 chars
- Progressive extension on collision (never remapping)
- Hierarchical children: `bd-a3f8e9.1`, `bd-a3f8e9.2` (up to 3 levels)

**JSONL Format**:
- One JSON object per line
- Schema: `{id, title, description, status, priority, type, created_at, updated_at, ...}`
- Soft deletion: Issues are marked as deleted, not removed from JSONL
- Git merge driver: `bd merge` for intelligent conflict resolution

## Development Workflow

### Testing with Isolated Database

**CRITICAL**: Never pollute production `.beads/beads.db` with test data!

```bash
# Manual testing with isolated DB
BEADS_DB=/tmp/test.db ./bd init --quiet --prefix test
BEADS_DB=/tmp/test.db ./bd create "Test issue" -p 1

# Automated tests use t.TempDir()
func TestMyFeature(t *testing.T) {
    tmpDir := t.TempDir()
    testDB := filepath.Join(tmpDir, ".beads", "beads.db")
    s := newTestStore(t, testDB)
    // ... test code
}
```

### Daemon Mode vs Direct Mode

**When to use `--no-daemon`**:
- Git worktrees (daemon mode not supported)
- Debugging sync issues
- Tests requiring deterministic timing
- Commands that modify daemon state (e.g., `bd migrate`)

**Auto-start behavior**:
- Enabled by default (v0.9.11+)
- Disable: `export BEADS_AUTO_START_DAEMON=false`
- Daemon auto-restarts on version mismatch

### Git Integration Points

1. **Git hooks** (`.beads/hooks/`): Embedded in binary, installed via `bd hooks install`
   - `pre-commit`: Immediate flush (bypasses 30s debounce)
   - `post-merge`: Auto-import after pull
   - `pre-push`: Export before push (prevents stale JSONL)
   - `post-checkout`: Import on branch switch

2. **Git merge driver**: `bd merge %A %O %L %R` for intelligent JSONL merging

3. **Protected branches**: Use `bd init --branch beads-metadata` to commit to separate branch

## Common Development Tasks

### Adding a New CLI Command

1. Create `cmd/bd/mycommand.go` with Cobra structure
2. Register in `cmd/bd/main.go` (`rootCmd.AddCommand()`)
3. Add `--json` flag for agent compatibility
4. Add tests in `cmd/bd/mycommand_test.go`
5. Update README.md with usage examples

### Modifying Database Schema

1. Update `internal/storage/sqlite/schema.go`
2. Add migration file in `internal/storage/sqlite/migrations/` (e.g., `015_my_migration.go`)
3. Register it in `internal/storage/sqlite/migrations.go` in the `migrationsList` array
4. Update `internal/types/types.go` if adding new types
5. Implement in `internal/storage/sqlite/sqlite.go`
6. Update export/import in `cmd/bd/export.go` and `cmd/bd/import.go`
7. Add tests covering migration path

### Adding a Daemon RPC Endpoint

1. Define request/response types in `internal/rpc/protocol.go`
2. Implement handler in `cmd/bd/daemon_server.go`
3. Add client method in `internal/rpc/client.go`
4. Update daemon health checks if endpoint is critical
5. Add tests for RPC round-trip

## Important Technical Details

### Database Schema Versioning

Schema version stored in `metadata` table (`bd_version` key) as a version string (e.g., "0.9.11"). Migrations tracked in `internal/storage/sqlite/migrations/`. Use `bd info --schema --json` to inspect.

### Auto-Sync Timings

- **Export debounce**: 30 seconds (batches multiple CRUD ops)
- **Import check**: On every command (if `--auto-import` enabled)
- **Daemon sync**: Every 5 seconds (if `--auto-commit`/`--auto-push`)
- **Force immediate**: `bd sync` bypasses all debouncing

### Dependency Graph

Dependencies stored in `dependencies` table with `type`:
- `blocks`: Hard blocker (affects `bd ready`)
- `related`: Soft relationship
- `parent-child`: Hierarchical (epic → tasks)
- `discovered-from`: Tracks work discovered during task (inherits `source_repo`)

Cycle detection: `bd dep cycles` uses DFS to find circular dependencies.

### Multi-Repo Support

Each `.beads/` directory = isolated workspace with own daemon. MCP server routes by working directory. Use `source_repo` field to track cross-repo dependencies.

### Agent Mail (Optional)

Real-time coordination for multi-agent workflows. Requires:
- Agent Mail server running on `BEADS_AGENT_MAIL_URL`
- Environment: `BEADS_AGENT_NAME`, `BEADS_PROJECT_ID`
- Daemon automatically registers and syncs via WebSocket
- <100ms latency vs 2-5s git-only mode

## Testing Guidelines

### Test Naming Conventions

- `*_test.go`: Go tests (use `go test -short` to skip slow integration tests)
- `testdata/`: Test fixtures (git-committed for reproducibility)
- `t.TempDir()`: Isolated test databases

### Integration Tests

Mark slow tests with:
```go
if testing.Short() {
    t.Skip("Skipping integration test in short mode")
}
```

### Test Helpers

Common helpers in `cmd/bd/test_helpers_test.go`:
- `newTestStore(t, dbPath)`: Create isolated test store
- `createTestIssue(t, s, title)`: Helper for test issue creation
- `assertIssueExists(t, s, id)`: Verify issue presence

## Documentation Structure

- **README.md**: Main user-facing docs, feature overview
- **AGENTS.md**: AI agent workflow guide (this is dogfooded!)
- **docs/**: Comprehensive topic-specific guides
  - `INSTALLING.md`: Platform-specific installation
  - `ADVANCED.md`: Power user features
  - `EXTENDING.md`: Database extension patterns
  - `GIT_INTEGRATION.md`: Git workflow deep dive
  - `MULTI_REPO_AGENTS.md`: Multi-repo patterns
- **examples/**: Working code examples (Python agent, Bash agent, etc.)

## Before Committing

1. Run `go test -short ./...` (CI runs full suite)
2. Run `golangci-lint run ./...` (ignore baseline warnings)
3. Update relevant documentation
4. Use `bd` to track work: `bd create`, `bd update`, `bd close`
5. Run `bd sync` to ensure `.beads/issues.jsonl` is current

## Release Process

Use `./scripts/release.sh <version>` for automated releases (handles version bump, tests, tagging, Homebrew update). See `scripts/README.md` for details.

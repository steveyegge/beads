# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
**Beads** (command: `bd`) is a distributed, git-backed graph issue tracker designed for AI agents. It uses Git as a database, storing issues as JSONL files in a `.beads/` directory while maintaining a local SQLite cache for performance.

## Build and Run
- **Build**: `make build` or `go build -o bd ./cmd/bd`
- **Install**: `make install` (installs to `~/.local/bin`)
- **Run**: `./bd <command>` (or `bd` if installed)
- **Dependencies**: `go mod download`

## Testing
- **Local Test**: `make test` or `go test -short ./...`
- **Full Test with Coverage**: `go test -coverprofile=coverage.out ./...`
- **Manual Testing**: Use `BEADS_DB=/tmp/test.db` to avoid polluting production data.
- **Benchmarks**: `make bench` (10K/20K issue databases) or `make bench-quick`
- **Lint**: `golangci-lint run ./...`

## Project Structure
- `cmd/bd/`: CLI entry point and Cobra command definitions.
- `internal/storage/`: Data persistence layer (`sqlite/` for implementation, `factory/` for provider selection).
- `internal/types/`: Core domain types (e.g., `Resource`, `Issue`).
- `internal/daemon/`: Background process for auto-sync and health monitoring.
- `internal/rpc/`: Communication between CLI and daemon.
- `integrations/beads-mcp/`: Python-based Model Context Protocol (MCP) server.
- `.beads/`: Storage directory containing `issues.jsonl` (git-synced) and `beads.db` (local cache, gitignored).

## Development Guidelines
- **Go Version**: 1.24.0+
- **CLI Design**:
    - Follow Cobra patterns in `cmd/bd/`.
    - Include a `--json` flag for all commands to support programmatic/agent use.
    - Avoid interactive editors; use flags for updates.
- **Git Workflow**:
    - Run `bd sync` at the end of every work session to flush changes to Git and push to remote.
    - Always commit `.beads/issues.jsonl` along with code changes.
    - Do NOT commit `.beads/beads.db`.
- **Issue Tracking**:
    - The project uses `bd` to track its own tasks. Do NOT create markdown TODO lists.
    - Create issues: `bd create "Title" --description="Detailed context" -t bug|feature|task -p 0-4 --json`
    - Commit message convention: `Fix auth validation bug (bd-abc)` (include issue ID in parentheses).

## Key Scripts
- `scripts/bump-version.sh <version> --commit`: Update all version files atomically.
- `scripts/release.sh <version>`: Complete release workflow.
- `scripts/update-homebrew.sh <version>`: Update Homebrew formula.

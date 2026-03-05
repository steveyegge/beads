Docs generated against (beads HEAD): 7f41edd9c4350e5c68ebec6f9571e27833f27133

# Architecture (Current)

This document describes the current Beads architecture as evidenced by repository sources. When sources disagree, this doc records the mismatch as `unknown/conflict` and does not attempt to resolve it.

## Cross-Repo Glossary

- `setup-mode`: Workspace-bootstrap dashboard routing mode used when no workspace is detected.
- `dashboard-mode`: Operational dashboard routing mode used when a workspace is detected.
- `data plane`: Request paths that execute primary workload operations.
- `control plane`: Request paths that perform administrative/configuration control operations.
- `MCP`: Model Context Protocol integration surface and tooling.
- `npm wrapper`: Node-based launcher that forwards CLI arguments/environment to a packaged native binary.
- `Cobra \`Use:\` surface`: The command names/signatures declared in Cobra `Use:` fields that define the observable CLI surface.

## Local Glossary

- DoltStore: The Dolt-backed storage implementation used at runtime (see `beads/internal/storage/dolt/store.go`).
- sync: A family of commands and storage operations that move versioned data between repos/remotes (see `beads/internal/storage/versioned.go`, `beads/internal/storage/dolt/federation.go`).
- federation: Cross-repo sync and peer configuration surfaces (see `beads/cmd/bd/federation.go`, `beads/internal/storage/dolt/federation.go`).
- MCP: The Python stdio server exposing Beads tools for MCP clients (see `beads/integrations/beads-mcp/src/beads_mcp/server.py`).
- npm wrapper: The Node entrypoint that launches the packaged `bd` binary and forwards args/env (see `beads/npm-package/bin/bd.js`).

## Architecture Boundaries

Beads is organized around a CLI surface that wires commands to a storage interface, with integrations and packaging layers acting as launchers or adapters.

- CLI boundary: Root command wiring and global flags live in the Cobra-based CLI entrypoint, which runs persistent pre/post hooks that gate command startup (see `beads/cmd/bd/main.go`).
- Storage boundary: Callers depend on an interface in `internal/storage`, while the concrete Dolt implementation lives under `internal/storage/dolt` (see `beads/internal/storage/storage.go`, `beads/internal/storage/dolt/store.go`).
- Config boundary: Configuration discovery and env mapping are centralized in `internal/config`, and CLI startup reads config and overlays flags (see `beads/internal/config/config.go`, `beads/cmd/bd/main.go`).
- Integration boundary: External launch surfaces (MCP server, npm wrapper) are separate from the Go CLI binary and forward work into it or into storage operations (see `beads/integrations/beads-mcp/src/beads_mcp/server.py`, `beads/npm-package/bin/bd.js`).

### unknown/conflict: CLI surface mismatches (docs vs Cobra `Use:` surface)

The docs surface and the inspected Cobra wiring disagree in a few places. Per the repo's source-precedence contract, this is recorded as `unknown/conflict` and left unresolved here (see `.sisyphus/evidence/task-2-classification-model.md`).

- `unknown/conflict`: secondary docs (website reference pages) still advertise root-level `bd merge ... --into ...`, while inspected Cobra definitions show merge under `vc` (`bd vc merge <branch>`) plus `merge-slot` and duplicate/supersede flows (see `beads/website/docs/cli-reference/issues.md`, `beads/website/docs/reference/advanced.md`, `beads/cmd/bd/vc.go`, `beads/cmd/bd/merge_slot.go`, `beads/cmd/bd/duplicate.go`).
- `unknown/conflict`: secondary docs (website reference pages) still advertise root-level `bd import` and `bd sync`; in reviewed command files there are import helpers, `bd init --from-jsonl`, many nested sync-related commands, and Dolt-native push/pull, but no root `import` or `sync` Cobra `Use:` surface in scope (see `beads/website/docs/cli-reference/sync.md`, `beads/website/docs/intro.md`, `beads/cmd/bd/init.go`, `beads/cmd/bd/import_shared.go`, `beads/cmd/bd/backup_dolt.go`, `beads/cmd/bd/federation.go`, `beads/cmd/bd/migrate.go`, `beads/cmd/bd/repo.go`).

## Storage and Sync

Storage and sync are built around a versioned-data model with a Dolt backend and explicit conflict surfacing.

- Interface-first storage: The primary boundary is `storage.Storage`, with runtime implementations using Dolt-backed types (see `beads/internal/storage/storage.go`, `beads/internal/storage/dolt/store.go`).
- Sync pipeline: Sync operations are implemented as fetch, merge, optional conflict handling, then push, with conflicts surfaced as typed values (see `beads/internal/storage/versioned.go`, `beads/internal/storage/dolt/federation.go`).
- Remote transport split: git-protocol remotes route through `dolt` CLI subprocesses, while other remotes use SQL procedures like `CALL DOLT_PUSH/PULL/FETCH` (see `beads/internal/storage/dolt/store.go`, `beads/internal/storage/dolt/federation.go`).
- Credential scope: Remote credentials are injected into subprocess/env call paths rather than set globally, reducing cross-goroutine credential bleed (see `beads/internal/storage/dolt/federation.go`, `beads/internal/storage/dolt/store.go`).
- Contention and availability: The Dolt storage layer includes retry/backoff, circuit-breaker state, lock error wrapping, and stale noms lock cleanup paths (see `beads/internal/storage/dolt/store.go`, `beads/internal/storage/dolt/circuit.go`, `beads/internal/storage/dolt/noms_lock.go`).

## Integrations

Integrations are separate deliverables that expose Beads functionality via other ecosystems, while preserving workspace scoping and trust boundaries.

- MCP server: The MCP integration is implemented as a Python server that exposes tool APIs over stdio transport (see `beads/integrations/beads-mcp/src/beads_mcp/server.py`).
- MCP write gating: Write operations can require an explicit workspace context before allowing mutations (see `beads/integrations/beads-mcp/src/beads_mcp/server.py`).
- Workspace routing: The MCP server resolves git root and `.beads/*.db` discovery per request or persistent context, scoping tool calls by workspace (see `beads/integrations/beads-mcp/src/beads_mcp/server.py`).
- npm wrapper: The npm package is a thin launcher that checks packaged binaries and forwards argv/env to the native `bd` binary (see `beads/npm-package/bin/bd.js`).
- Federation surfaces: Federation-related CLI and storage code crosses repo and peer domains for sync operations (see `beads/cmd/bd/federation.go`, `beads/internal/storage/versioned.go`, `beads/internal/storage/dolt/federation.go`).

## Packaging

Packaging focuses on distributing the CLI and integration surfaces across multiple ecosystems.

- Documented channels: The README describes distribution via npm, Homebrew, and `go install` for the CLI, and also references a Python package for the MCP integration (see `beads/README.md`).
- npm distribution model: The npm package is a binary-wrapper distribution where the JS entrypoint locates a platform binary under `bin/` and emits diagnostics if postinstall download did not succeed (see `beads/npm-package/bin/bd.js`).
- MCP version identity: The MCP server resolves its identity/version from Python package metadata, with a fallback when metadata is unavailable (see `beads/integrations/beads-mcp/src/beads_mcp/server.py`).

## Config Precedence

Configuration has explicit discovery and precedence rules spanning flags, config files, and environment variables.

- Startup precedence: CLI startup logic applies flags over config-derived values, with remaining values coming from config/env/defaults (see `beads/cmd/bd/main.go`).
- Config discovery order: Config file discovery checks `BEADS_DIR` first, then nearest project `.beads/config.yaml`, then the user config dir, then home config (see `beads/internal/config/config.go`).
- Env mapping: Viper automatic env mapping uses a `BD_` prefix, with legacy `BEADS_` checks used for source detection (see `beads/internal/config/config.go`).
- Dolt-specific precedence: Dolt config output logic documents env vars, then `metadata.json`, then `config.yaml` (see `beads/cmd/bd/dolt.go`).

## Observability

Observability is wired at the command boundary and propagated into the storage layer.

- Command spans: CLI startup initializes OpenTelemetry in the persistent pre-run, starts a root span with command and version attributes, and shuts down on post-run (see `beads/cmd/bd/main.go`).
- Storage traces and metrics: The Dolt storage layer emits OTel traces and metrics around query and exec paths, including retry and lock timing instrumentation (see `beads/internal/storage/dolt/store.go`).
- Profiling hooks: CLI flags include CPU profiling and trace artifacts, and verbose mode can include config override logging (see `beads/cmd/bd/main.go`, `beads/internal/config/config.go`).

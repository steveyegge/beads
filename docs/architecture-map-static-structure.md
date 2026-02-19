# Architecture Map — Static Structure.

## Scope and Method
This map covers all first-class code modules/packages/directories in the repository: Go packages (`go list ./...`), integrations, examples, npm package, website, and scripts.

Status labels:
- `active`: touched recently (2026-01+ in this snapshot) and still in active command/runtime paths.
- `stale`: no recent changes in this snapshot (late 2025) and not central to main runtime.
- `deprecated`: explicitly marked deprecated in code/docs or hidden behind replacement paths.

| Module name & path | Purpose | Internal dependencies | External dependencies | Status | Test coverage |
|---|---|---|---|---|---|
| `github.com/steveyegge/beads` (`.`) | Public Go API for embedding/extending beads storage and core types. | `internal/beads`, `internal/storage/dolt`, `internal/types` | Go stdlib | active | yes |
| `github.com/steveyegge/beads/cmd/bd` (`cmd/bd`) | Main CLI surface and orchestration layer for all user commands. | Most `internal/*`, `cmd/bd/doctor`, `cmd/bd/setup` | `cobra`, `viper`, Dolt/mysql/sqlite drivers, Anthropic SDK, stdlib | active | yes |
| `github.com/steveyegge/beads/cmd/bd/doctor` | Health diagnostics, environment checks, and remediation command wiring. | `cmd/bd/doctor/fix`, `internal/beads`, `internal/config*`, `internal/storage/dolt*`, `internal/syncbranch` | DB drivers (`mysql`, `sqlite`, Dolt), stdlib | active | yes |
| `github.com/steveyegge/beads/cmd/bd/doctor/fix` | Automated fixer routines for doctor findings (locks, config, migration, hooks). | `internal/beads`, `internal/config*`, `internal/lockfile`, `internal/storage/dolt` | DB drivers, stdlib | active | yes |
| `github.com/steveyegge/beads/cmd/bd/setup` | CLI setup/bootstrap helpers (editor integration and hook setup). | `internal/utils` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/audit` | Audit logging and interaction tracking helpers. | `internal/beads` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/beads` | Core beads directory/database discovery and storage-facing API contracts. | `internal/configfile`, `internal/git`, `internal/storage`, `internal/utils` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/compact` | Compaction pipeline and AI-assisted summary generation. | `internal/audit`, `internal/beads`, `internal/config`, `internal/types` | Anthropic SDK, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/config` | Global config load/precedence (flags/env/files). | `internal/debug` | `viper`, YAML libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/configfile` | Metadata/config file read/write and migration helpers. | - | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/debug` | Verbose/quiet/debug rendering toggles and helpers. | - | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/export` | Export policy/config wiring used by sync and portability flows. | `internal/storage/dolt` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/formula` | Formula parser/expander for workflow templates. | - | TOML libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/git` | Git process/worktree helper functions. | `internal/utils` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/gitlab` | GitLab client, mapping, and tracker integration. | `internal/storage`, `internal/tracker`, `internal/types` | HTTP/JSON libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/hooks` | Hook runtime abstractions and command hook execution wiring. | `internal/types` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/idgen` | Hash/ID generation primitives. | - | stdlib | stale | yes |
| `github.com/steveyegge/beads/internal/jira` | Jira integration client and mapping logic. | `internal/storage`, `internal/tracker`, `internal/types` | HTTP/JSON libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/linear` | Linear integration client and mapping logic. | `internal/idgen`, `internal/storage`, `internal/tracker`, `internal/types` | HTTP/JSON libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/lockfile` | Lock-file primitives for daemon/storage coordination. | - | `x/sys`, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/molecules` | Molecule catalog loading and template orchestration helpers. | `internal/debug`, `internal/storage`, `internal/storage/dolt`, `internal/types` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/query` | Query language parser/evaluator for list/query commands. | `internal/timeparsing`, `internal/types` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/recipes` | Setup recipe definitions and helpers. | - | TOML libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/routing` | Rig/workspace routing and repo resolution logic. | `internal/beads`, `internal/storage/dolt` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/storage` | Storage interfaces and shared storage contracts. | `internal/types` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/storage/dolt` | Primary Dolt-backed storage implementation (CRUD, deps, queries, events). | `internal/config*`, `internal/idgen`, `internal/lockfile`, `internal/storage`, `internal/types`, migrations | Dolt/mysql drivers, backoff libs, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/storage/dolt/migrations` | Dolt schema migration routines. | `internal/storage/doltutil` | DB/sql libs | active | yes |
| `github.com/steveyegge/beads/internal/storage/doltutil` | Shared Dolt utility wrappers. | - | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/syncbranch` | Sync branch workflow control and integrity checks. | `internal/beads`, `internal/config`, `internal/git`, `internal/storage/dolt`, `internal/utils` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/testutil` | Shared test scaffolding for packages. | - | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/testutil/fixtures` | Test fixture generation for integration/benchmark scenarios. | `internal/storage/dolt`, `internal/types` | stdlib | active | none |
| `github.com/steveyegge/beads/internal/timeparsing` | Layered natural-language and relative time parsing. | - | `olebedev/when`, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/tracker` | External tracker plugin framework and tracker orchestration. | `internal/storage`, `internal/types` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/types` | Core domain model (`Issue`, deps, labels, events, filters, workflow fields). | - | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/ui` | CLI UI rendering/styling and paging helpers. | - | `glamour`, `lipgloss`, `termenv`, stdlib | active | yes |
| `github.com/steveyegge/beads/internal/utils` | Utility helpers (ID resolution, path/runtime helpers). | `internal/storage`, `internal/types` | stdlib | active | yes |
| `github.com/steveyegge/beads/internal/validation` | Validation rules used by create/update/close flows. | `internal/types`, `internal/utils` | stdlib | active | yes |
| `integrations/beads-mcp` | Python MCP server exposing beads operations for MCP-only clients. | Calls `bd` CLI/daemon; depends on project command surface | `fastmcp`, `pydantic`, pytest ecosystem | active | yes |
| `integrations/claude-code` | Claude Code slash-command integration to convert plans into beads issue graphs. | Uses `bd` CLI and workflow conventions | Claude Code runtime | stale | none |
| `integrations/junie` | Junie integration guidance and MCP config artifacts. | Uses `bd mcp` and setup flows | JetBrains Junie/MCP client | active | none |
| `npm-package` | Node distribution wrapper that downloads/executes native `bd` binary. | Wraps `cmd/bd` binary releases | Node runtime (`child_process`, fs/path/os), npm | active | yes |
| `website` | Docusaurus docs site for user/developer documentation. | Consumes docs and release metadata from repo | Docusaurus/React toolchain | active | partial |
| `scripts` | Install, release, and maintenance automation scripts. | Used by release/docs/npm and setup workflows | bash/curl/git/python toolchain | active | partial |
| `examples/bash-agent` | Bash agent workflow example (`ready -> claim -> close`). | Calls `bd` CLI | bash + `jq` | stale | none |
| `examples/bd-example-extension-go` | Go extension example using beads API/library mode. | Imports `github.com/steveyegge/beads` | Go toolchain | active | none |
| `examples/claude-desktop-mcp` | Legacy reference for Claude Desktop MCP setup flow. | Points at `integrations/beads-mcp` and `bd` | Claude Desktop/MCP | stale | none |
| `examples/compaction` | Manual compaction workflow examples. | Calls `bd admin compact` flows | bash, optional Anthropic key | active | none |
| `examples/contributor-workflow` | Contributor/fork workflow examples and docs. | Uses contributor-routing CLI behavior | git + shell | active | none |
| `examples/github-import` | GitHub issue import to beads JSONL. | Produces imports consumed by `bd import` | GitHub REST API | stale | none |
| `examples/jira-import` | Jira<->beads import/export scripts. | Integrates with `bd` data model/CLI | Jira REST API | stale | none |
| `examples/library-usage` | Minimal library usage example for Go consumers. | Imports public beads package | Go toolchain | active | none |
| `examples/linear-workflow` | Linear workflow integration example docs/scripts. | Uses `bd linear` flow | Linear API | stale | none |
| `examples/markdown-to-jsonl` | Converts markdown plans into beads-compatible JSONL. | Feeds `bd import` workflow | Python stdlib/parsers | stale | none |
| `examples/monitor-webui` | Standalone monitor UI for issue/daemon observation. | Consumes daemon/CLI output | JS/Web tooling | active | none |
| `examples/multi-phase-development` | Multi-phase planning/workflow pattern examples. | Uses epics/deps/gates features | shell/git | active | none |
| `examples/multiple-personas` | Persona-based workflow examples. | Uses labels/issue routing patterns | shell/git | active | none |
| `examples/protected-branch` | Protected branch sync workflow examples. | Uses sync-branch-related commands | git | active | none |
| `examples/python-agent` | Python reference agent using JSON CLI API. | Calls `bd ready/update/create/dep/close` | Python runtime | stale | none |
| `examples/startup-hooks` | Session startup hook examples. | Uses CLI metadata/version checks | shell | stale | none |
| `examples/team-workflow` | Team collaboration workflow examples. | Uses shared repo + sync patterns | git/shell | active | none |

## Deprecated Areas (Within Active Modules)
These are deprecated subpaths inside otherwise active modules:
- `cmd/bd/detect_pollution.go` (hidden deprecated command, superseded by `bd doctor --check=pollution`).
- `cmd/bd/admin_aliases.go` (deprecated aliases to admin subcommands).
- `scripts/bump-version.sh` (prints deprecation notice; replacement scripts are used).
- Legacy config/env compatibility paths (`BEADS_DB`, `beads.jsonl`, contributor legacy keys) retained for migration safety.

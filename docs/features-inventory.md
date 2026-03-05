Docs generated against (beads HEAD): 7f41edd9c4350e5c68ebec6f9571e27833f27133

# Beads Feature Inventory

## Cross-Repo Glossary

- `setup-mode`: Workspace-bootstrap dashboard routing mode used when no workspace is detected.
- `dashboard-mode`: Operational dashboard routing mode used when a workspace is detected.
- `data plane`: Request paths that execute primary workload operations.
- `control plane`: Request paths that perform administrative/configuration control operations.
- `MCP`: Model Context Protocol integration surface and tooling.
- `npm wrapper`: Node-based launcher that forwards CLI arguments/environment to a packaged native binary.
- `Cobra \`Use:\` surface`: The command names/signatures declared in Cobra `Use:` fields that define the observable CLI surface.

## Feature Classification Model

# Feature Classification and Source-Precedence Contract

This document defines the locked status taxonomy, confidence levels, evidence rules, source precedence, and conflict handling used by later feature inventory documents.

This is a schema and governance contract only. It does not claim any specific feature is implemented.

## Status taxonomy (locked)

Only these status tokens are allowed:

- `implemented`
- `partially_implemented`
- `indicated`
- `roadmap`
- `deprecated`
- `unknown/conflict`

### Status definitions

`implemented`
- Meaning: User-visible behavior exists now.
- Hard rule: `implemented` requires non-doc evidence. Docs-only evidence is never sufficient.
- Typical evidence: code/handlers that execute the behavior, CLI wiring that reaches real implementation, HTTP handler code, tests that assert behavior, captured command output that proves behavior (not just help text).

`partially_implemented`
- Meaning: Some user-visible behavior exists, but parts are stubbed, no-op, gated, or explicitly "not yet implemented".
- Typical evidence: code paths present plus TODO/FIXME, placeholder prints, feature-flag off by default, missing side effects.

`indicated`
- Meaning: Strong signals exist that a feature is intended, but implementation is not verified.
- Typical evidence: design docs, README/CLI reference, command or route names with missing backing logic, TODOs that describe near-term behavior.

`roadmap`
- Meaning: Future intent with no usable behavior today.
- Typical evidence: design sections labeled future, TODOs describing new architecture, planned flags or subcommands not present in code.

`deprecated`
- Meaning: Feature exists or existed, but is flagged as deprecated, replaced, hidden, or scheduled for removal.
- Typical evidence: deprecation notes in docs, code comments, hidden commands, warnings in output.

`unknown/conflict`
- Meaning: Evidence is contradictory or incomplete in a way that cannot be resolved with the available sources.
- Typical evidence: code says one thing while docs say another, or two same-precedence sources disagree.

## Confidence levels

Confidence is a separate field from status.

High
- Evidence threshold: At least one high-precedence, non-doc source directly supports the claim.
- Examples of acceptable evidence: code/handler path, failing/passing tests tied to behavior, captured runtime output from executing the surface.

Medium
- Evidence threshold: Evidence supports the claim, but is indirect or missing one critical link.
- Examples: command is wired but side effects are unclear, tests cover only part of the behavior, output exists but inputs and failure modes are unknown.

Low
- Evidence threshold: Claim is based on low-precedence sources or weak signals.
- Examples: docs-only, design-only, TODO-only, or naming that suggests behavior without proof.

## Evidence types and source precedence

When sources conflict, higher precedence wins.

1. Code and handlers (implementation source)
   - Go code that executes behavior, HTTP handlers, routing tables, command wiring that reaches real logic.
2. Executable surfaces (runtime proof)
   - Captured output from actually running commands, listing routes, or hitting endpoints in a reproducible way.
3. Tests
   - Unit/integration tests that assert behavior, including validation and failure modes.
4. Docs (reference)
   - README, CLI reference, reference manuals.
5. Design docs (intent)
   - Design notes, proposals, "future" sections.
6. TODO/FIXME markers (intent signal)
   - TODO comments, stubs, placeholder strings.

## Conflict handling rules

If sources disagree:

- Prefer the highest-precedence source.
- If two sources at the same precedence disagree and you cannot prove which is correct, mark status as `unknown/conflict`.
- Record both claims in the row notes, and include evidence paths for each.
- Do not "average" by choosing `partially_implemented` unless you have direct evidence of partial behavior.

If evidence is missing:

- Use `indicated` or `roadmap` (not `implemented`).
- Use Low confidence unless there is supporting code structure that strongly implies imminent behavior.

## Default classification for common signals

TODO/FIXME
- TODO describing a missing behavior without any working path: default `roadmap` (Low).
- TODO in a code path that otherwise runs and does something user-visible: default `partially_implemented` (Medium or Low).

Design docs
- Design language describing intended behavior without corroborating code: default `indicated` (Low) or `roadmap` (Low) if explicitly future.

Docs and CLI references
- Docs-only description of a feature: default `indicated` (Low).
- Help text alone is not enough for `implemented` unless paired with non-doc evidence.

## Feature inventory row schema (required fields)

Every feature inventory row must include these fields:

- surface: Where the user interacts (CLI command, HTTP route, proxy endpoint, file format, integration tool).
- invocation: Exact example of how it is invoked (command line, request shape, API path).
- inputs: User-controlled inputs and required parameters.
- outputs: User-visible outputs (stdout, JSON, returned data).
- side effects: Persistent changes, network calls, file writes, state mutations.
- evidence path: Concrete pointer(s) to evidence (file paths, test names, or captured command output paths).

Strongly recommended additional fields:

- feature: Human name for the capability.
- status: One of the locked status tokens.
- confidence: High, Medium, Low.
- notes: Constraints, caveats, and conflict details.

## Evidence citation format

- Prefer repo-relative file paths (for example: `gastown/internal/web/api.go`).
- For tests, include the test name when possible.
- For captured runtime output, store it under `.sisyphus/evidence/` and cite that path.
- If a row has multiple evidence items, list them all.

## Feature Inventory

| feature | surface | invocation | inputs | outputs | side effects | status | confidence | evidence path | notes |
|---|---|---|---|---|---|---|---|---|---|
| Create issue | CLI | `bd create "Title" -p 1 --json` | title, type/priority/labels/deps flags | JSON or human confirmation | Writes issue row, labels, deps | implemented | High | `beads/cmd/bd/create.go:27`; `beads/cmd/bd/create.go:577`; `beads/cmd/bd/create.go:594` | Non-doc code path calls `CreateIssue` and dependency writes. |
| Update issue | CLI | `bd update <id> --status blocked --json` | id(s), status/priority/assignee/title/etc | JSON or human confirmation | Mutates issue fields; can add/remove deps; can claim | implemented | High | `beads/cmd/bd/update.go:22`; `beads/cmd/bd/update.go:307`; `beads/cmd/bd/update.go:342`; `beads/cmd/bd/update.go:408` | Includes atomic claim path via `ClaimIssue`. |
| Show issue | CLI | `bd show <id> --json` | id(s), `--current`, view flags | Issue details to stdout/JSON | None (read path) | implemented | High | `beads/cmd/bd/show.go:16`; `beads/cmd/bd/show.go:142` | Read-only retrieval and formatting. |
| Close issue | CLI | `bd close <id> --reason "Done" --json` | id(s), reason | JSON/human close result | Sets status closed; may auto-close molecule | implemented | High | `beads/cmd/bd/close.go:19`; `beads/cmd/bd/close.go:128` | Direct `CloseIssue` write path confirmed. |
| Reopen issue | CLI | `bd reopen <id> --reason "Retry" --json` | id(s), reason | JSON/human reopen result | Sets status open; clears defer; optional comment | implemented | High | `beads/cmd/bd/reopen.go:14`; `beads/cmd/bd/reopen.go:56`; `beads/cmd/bd/reopen.go:60`; `beads/cmd/bd/reopen.go:66` | Reopen implemented via `UpdateIssue` + optional `AddComment`. |
| Add dependency | CLI | `bd dep add <issue> <depends-on>` | two issue ids, dep type | JSON/human relation confirmation | Writes dependency edge | implemented | High | `beads/cmd/bd/dep.go:185`; `beads/cmd/bd/dep.go:157` | Supports cross-rig routing logic in same file. |
| Dependency tree | CLI | `bd dep tree <id>` | issue id, depth/direction flags | Tree output/JSON | None (read path) | implemented | High | `beads/cmd/bd/dep.go:530`; `beads/cmd/bd/dep.go:583`; `beads/cmd/bd/dep.go:589` | Uses `GetDependencyTree` for down/up/both. |
| Dependency cycle detection | CLI | `bd dep cycles` | optional filters | JSON/human cycle list | None (analysis/read) | implemented | High | `beads/cmd/bd/dep.go:653`; `beads/cmd/bd/dep.go:663` | Command wired and emits JSON when requested. |
| Ready work queue | CLI | `bd ready --json` | limit/priority/assignee/labels/type | Ready issue list | None (read path) | implemented | High | `beads/cmd/bd/ready.go:19`; `beads/cmd/bd/ready.go:171` | Explicitly uses dependency-aware ready semantics. |
| Blocked queue | CLI | `bd blocked --json` | parent filter | Blocked issue list plus blockers | None (read path) | implemented | High | `beads/cmd/bd/ready.go:285`; `beads/cmd/bd/ready.go:296`; `beads/cmd/bd/ready.go:300` | `blocked` subcommand is registered from `ready.go`. |
| Stale finder | CLI | `bd stale --days 30 --json` | days/status/limit | Stale issue list | None (read path) | implemented | High | `beads/cmd/bd/stale.go:13`; `beads/docs/CLI_REFERENCE.md:43` | Cobra command exists and is documented. |
| Dolt operations | CLI | `bd dolt status` / `bd dolt push` | subcommand-specific args | Status/operation result | Starts/stops server, commits/pushes/pulls DB history | implemented | High | `beads/cmd/bd/dolt.go:29`; `beads/cmd/bd/dolt.go:39`; `beads/cmd/bd/dolt.go:49` | Dedicated Dolt command group is wired. |
| Backup operations | CLI | `bd backup` / `bd backup status` | optional force and subcommands | JSONL backup state/status | Writes backup artifacts; optional git push | implemented | High | `beads/cmd/bd/backup.go:15`; `beads/cmd/bd/backup.go:38`; `beads/cmd/bd/backup.go:69` | Includes JSONL export plus Dolt backup subcommands. |
| VC operations | CLI | `bd vc status` / `bd vc merge <branch>` | branch, merge strategy, commit message | JSON/human VC status | DB merge/commit operations | implemented | High | `beads/cmd/bd/vc.go:14`; `beads/cmd/bd/vc.go:30`; `beads/cmd/bd/vc.go:47`; `beads/cmd/bd/vc.go:111` | `vc merge` is present under `vc`, not root. |
| Molecule namespace | CLI | `bd mol ...` | molecule subcommands | Help/subcommand outputs | Delegates to molecule workflows | implemented | High | `beads/cmd/bd/mol.go:34`; `beads/cmd/bd/mol.go:93` | Root molecule namespace is wired on root command. |
| Wisp lifecycle | CLI | `bd mol wisp <proto>` / `bd mol wisp gc` | proto id, vars, gc flags | Wisp creation/list/gc output | Creates ephemeral issues; gc deletes stale wisps | implemented | High | `beads/cmd/bd/wisp.go:28`; `beads/cmd/bd/wisp.go:775`; `beads/cmd/bd/wisp.go:797` | Wisp command exists and is attached under `mol`. |
| Pour proto to mol | CLI | `bd mol pour <proto> --var k=v` | proto id, vars, assignee/attach flags | Create/preview output | Spawns persistent issue graph | implemented | High | `beads/cmd/bd/pour.go:20`; `beads/cmd/bd/pour.go:257`; `beads/cmd/bd/pour.go:265` | `Use` omits `mol` prefix because command is mounted under `mol`. |
| Squash wisp | CLI | `bd mol squash <molecule-id>` | molecule id, summary, keep flags | Digest result/JSON | Creates digest and promotes/deletes children | implemented | High | `beads/cmd/bd/mol_squash.go:18`; `beads/cmd/bd/mol_squash.go:299`; `beads/cmd/bd/mol_squash.go:304` | Wisp-to-digest flow is code-backed. |
| Burn wisp/mol | CLI | `bd mol burn <molecule-id> --force` | molecule id(s), dry-run/force | Burn result/JSON | Destructive delete of molecule graph | implemented | High | `beads/cmd/bd/mol_burn.go:16`; `beads/cmd/bd/mol_burn.go:409`; `beads/cmd/bd/mol_burn.go:415` | Explicit destructive path with dry-run support. |
| Bond molecules/protos | CLI | `bd mol bond <A> <B> --type parallel` | operands, type, phase flags, vars | Bond result/JSON | Creates/attaches compound graph | implemented | High | `beads/cmd/bd/mol_bond.go:18`; `beads/cmd/bd/mol_bond.go:601`; `beads/cmd/bd/mol_bond.go:610` | Polymorphic bonding path is implemented in code. |
| MCP issue tools | MCP | `create(...)`, `update(...)`, `show(...)`, `close(...)`, `reopen(...)`, `dep(...)`, `ready(...)`, `blocked(...)`, `stats(...)` | typed MCP tool params incl. workspace | Structured tool return models | Calls bd-backed tool functions; writes on mutating tools | implemented | High | `beads/integrations/beads-mcp/src/beads_mcp/server.py:978`; `beads/integrations/beads-mcp/src/beads_mcp/server.py:1048`; `beads/integrations/beads-mcp/src/beads_mcp/server.py:1103`; `beads/integrations/beads-mcp/src/beads_mcp/server.py:1152`; `beads/integrations/beads-mcp/src/beads_mcp/server.py:1173` | MCP surface is narrower than full CLI but clearly wired. |
| npm wrapper passthrough | npm wrapper | `npx @beads/bin bd <args...>` (via wrapper entrypoint) | command-line args and env | Child process stdout/stderr passthrough | Spawns native `bd` binary; exits with child code | implemented | High | `beads/npm-package/bin/bd.js:19`; `beads/npm-package/bin/bd.js:36`; `beads/npm-package/bin/bd.js:46` | Wrapper is a launcher, not an alternate implementation. |
| `bd import` command availability | CLI docs vs CLI code | Docs: `bd import -i ...`; code surface check for Cobra `Use: "import..."` | JSONL path, orphan mode (per docs) | Docs promise import output | Would write/import issues if command existed | unknown/conflict | High | `beads/docs/CLI_REFERENCE.md:594`; `beads/docs/CLI_REFERENCE.md:595`; `beads/cmd/bd/import_shared.go:69`; `beads/cmd/bd/init.go:528` | Docs advertise top-level `bd import`; code shows shared import helper used from `init`, but no top-level `import` command found in the Cobra `Use:` surface extraction for `beads/cmd/bd/*.go`. |
| `bd sync` command availability | CLI docs vs CLI code | Docs: `bd sync`; code shows only nested sync commands | none (root command in docs) | Docs promise sync behavior | Would commit/pull/push if root command existed | unknown/conflict | High | `beads/docs/CLI_REFERENCE.md:702`; `beads/docs/CLI_REFERENCE.md:887`; `beads/cmd/bd/backup_dolt.go:99`; `beads/cmd/bd/migrate.go:727`; `beads/cmd/bd/federation.go:38`; `beads/cmd/bd/jira.go:42` | `sync` exists under multiple subcommands, but root `bd sync` is not present in the Cobra `Use:` surface inventory. |
| `bd merge` command availability | CLI docs vs CLI code | Docs: `bd merge <src...> --into <target>`; code has `bd vc merge` and `bd merge-slot` | source/target IDs or branch names | Merge result/conflicts | Would mutate issue graph or VC state | unknown/conflict | High | `beads/docs/CLI_REFERENCE.md:383`; `beads/cmd/bd/vc.go:30`; `beads/cmd/bd/merge_slot.go:18` | Direct root `bd merge` claim conflicts with observed command surfaces (`vc merge`, `merge-slot`). |
| `bd stats` command availability | Docs outside CLI reference vs CLI code | Docs: `bd stats`; code has `bd human stats` and MCP `stats` tool | none (root command in docs) | Docs imply aggregate stats output | Read-only stats query | unknown/conflict | High | `beads/docs/QUICKSTART.md:181`; `beads/docs/PLUGIN.md:370`; `beads/cmd/bd/human.go:319`; `beads/integrations/beads-mcp/src/beads_mcp/server.py:1174` | Statistics capability exists, but root CLI spelling in docs conflicts with Cobra surface observed. |
| `--no-auto-flush` global flag | CLI docs/MCP client vs CLI root flags | `bd --no-auto-flush <command>` | boolean flag | Expected behavior: disable auto flush | Expected to change sync/write timing | unknown/conflict | High | `beads/docs/CLI_REFERENCE.md:331`; `beads/integrations/beads-mcp/src/beads_mcp/bd_client.py:278`; `beads/cmd/bd/main.go:183` | Docs and MCP client still reference flag, but current root persistent flags list does not include it. |
| `--no-auto-import` global flag | CLI docs/MCP client vs CLI root flags | `bd --no-auto-import <command>` | boolean flag | Expected behavior: disable auto import | Expected to change import behavior | unknown/conflict | High | `beads/docs/CLI_REFERENCE.md:332`; `beads/integrations/beads-mcp/src/beads_mcp/bd_client.py:280`; `beads/cmd/bd/main.go:183` | Same mismatch pattern as `--no-auto-flush`; likely stale docs/client assumptions vs current CLI flags. |
| Windows sandbox auto-detect parity | Cross-platform CLI implementation | `bd --sandbox <command>` or auto-detect path | platform/runtime environment | Unix auto-detect behavior; Windows manual fallback | Affects sync/autopush behavior in sandbox | partially_implemented | Medium | `beads/docs/CLI_REFERENCE.md:281`; `beads/cmd/bd/sandbox_windows.go:17`; `beads/cmd/bd/main.go:410` | Windows file contains TODO for detection; manual `--sandbox` remains documented fallback. |

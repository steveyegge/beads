# Architectural Issues — Prioritized.

Prioritization rule used here: highest impact + lowest execution risk first.

## Priority List

1. **Unreachable legacy control block in orphaned-issues doctor check**
- Location: `cmd/bd/doctor/git.go` (`CheckOrphanedIssues` after unconditional early return)
- Problem: Function returns `N/A` immediately, leaving a large unreachable legacy block in place. This is dead code and increases maintenance risk/noise.
- Recommended action: **remove** unreachable block and keep a concise TODO/reimplementation marker.
- Risk level: low
- Breaking change: no
- Downstream impact: doctor code readability; no runtime behavior change.

2. **Test-issue heuristic stored in deprecated command module but used by active create flow**
- Location: `cmd/bd/detect_pollution.go` + call sites in `cmd/bd/create.go`
- Problem: Active behavior depends on helper inside a deprecated/hidden command file, causing misplaced ownership and fragile future cleanup.
- Recommended action: **relocate** heuristic to shared validation module and consume from both command paths.
- Risk level: low
- Breaking change: no
- Downstream impact: create warnings, pollution detection consistency.

3. **Scheduling parse/warn logic duplicated across create/update/defer paths**
- Location: `cmd/bd/create.go`, `cmd/bd/update.go`, `cmd/bd/defer.go`
- Problem: Repeated `ParseRelativeTime` flows with slightly different error/warn behavior produce inconsistent handling and extra maintenance cost.
- Recommended action: **refactor/merge** into shared scheduling parser helpers.
- Risk level: low
- Breaking change: no
- Downstream impact: CLI scheduling flags (`--due`, `--defer`, `--until`), docs/examples.

4. **Multi-step update path can leave partially-applied semantic changes if later steps fail**
- Location: `cmd/bd/update.go` (field updates + label updates + parent/dependency operations)
- Problem: Some operations happen in sequence with continue-on-error behavior; per-ID update can become partially successful with unclear reporting.
- Recommended action: **refactor** to stricter transaction grouping or explicit per-step summary status.
- Risk level: medium
- Breaking change: no (if output-compatible)
- Downstream impact: update reliability and operator trust.

5. **Close reason semantics are load-bearing but validation remains mostly procedural**
- Location: close path (`cmd/bd/close.go`) and workflow guidance docs
- Problem: Conditional fallback behavior depends on close reason keyword safety, but enforcement is not centralized in command path.
- Recommended action: **add** centralized close-reason lint utility in close command path.
- Risk level: medium
- Breaking change: potentially yes (stricter close validation may reject previously accepted reasons)
- Downstream impact: close automation, conditional dependency behavior.

6. **Global mutable CLI runtime state remains broad and tightly coupled**
- Location: `cmd/bd/main.go`, `cmd/bd/context.go`
- Problem: Many package globals and transitional sync logic raise fragility around initialization ordering and testing complexity.
- Recommended action: **refactor** toward `CommandContext` ownership; reduce globals incrementally.
- Risk level: high
- Breaking change: no external API break expected, but high regression risk in CLI runtime paths.
- Downstream impact: all commands, hooks, daemon/direct mode behavior.

7. **Deprecated command/alias surface still present and increases cognitive load**
- Location: `cmd/bd/detect_pollution.go`, `cmd/bd/admin_aliases.go`, deprecated flags in several commands
- Problem: Legacy surfaces remain available and can route users through old patterns.
- Recommended action: **deprecate further then remove** in a staged release with migration notes.
- Risk level: medium
- Breaking change: yes (when removed)
- Downstream impact: scripts/users relying on legacy aliases/commands.

8. **Stale examples with overlapping responsibilities dilute onboarding quality**
- Location: several `examples/*` paths marked stale (`python-agent`, `bash-agent`, `startup-hooks`, `claude-desktop-mcp`, `github-import`, etc.)
- Problem: Multiple old examples cover similar workflows with drift from current guidance.
- Recommended action: **merge/replace** into curated maintained example set.
- Risk level: medium
- Breaking change: yes for users referencing removed examples by path.
- Downstream impact: docs links and contributor onboarding.

9. **Legacy compatibility branches still mixed in active routing/config paths**
- Location: `internal/routing/routing.go`, `internal/configfile/configfile.go`, `internal/beads/beads.go`
- Problem: Backward-compat branches are necessary but add complexity and hidden behavior.
- Recommended action: **retain with explicit sunset policy**; remove branches only after migration windows close.
- Risk level: high
- Breaking change: yes if removed prematurely.
- Downstream impact: older environments/configs.

10. **Doctor/orphaned issue feature gap (disabled for Dolt backend)**
- Location: `cmd/bd/doctor/git.go`
- Problem: Feature intentionally disabled while old implementation remains dead and stale.
- Recommended action: **replace** with Dolt-native implementation; until then keep explicit `N/A` behavior and no dead code.
- Risk level: medium
- Breaking change: no
- Downstream impact: doctor quality/completeness.

11. **Runtime side-effects during initialization are broad and command-global**
- Location: `cmd/bd/main.go` pre-run/post-run
- Problem: Opening stores, hooks, and migration checks before command-specific execution can make startup fragile and harder to isolate.
- Recommended action: **refactor** toward narrower initialization where feasible, preserving correctness.
- Risk level: high
- Breaking change: no external interface break, high runtime regression risk.
- Downstream impact: command latency and reliability.

12. **Dependency concern tracking is not codified in one machine-readable report path**
- Location: multiple manifests (`go.mod`, `pyproject.toml`, `package.json`) + docs
- Problem: dependency freshness/vulnerability review exists, but not unified per release gate in this repo snapshot.
- Recommended action: **add** dependency health check aggregation in CI docs/workflow.
- Risk level: medium
- Breaking change: no
- Downstream impact: release hygiene and security visibility.

## Phase 4 execution plan based on this priority
Execution order requested by user and applied to the above list:
1. Dead code removal -> #1.
2. Logic relocation -> #2.
3. Data flow cleanup -> #3.
4. Control flow fixes -> #4 (low/medium-safe subset first).
5. Module merges/splits -> deferred until medium/high-risk confirmation.
6. Dependency updates/removals -> deferred until medium/high-risk confirmation.

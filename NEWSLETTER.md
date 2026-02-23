# Beads v0.55.4 - v0.56.1 — The Great Purge

**February 20 - February 23, 2026**

Beads v0.56 is a structural release. Three major subsystems have been removed entirely, the binary is a quarter of its former size, and everything runs on Dolt natively. If v0.53 deleted the sync pipeline, v0.56 finishes the job by removing the last three legacy subsystems: the embedded Dolt driver, the SQLite ephemeral store, and the remaining JSONL plumbing.

## 168MB to 41MB

The headline number tells the story. The embedded Dolt driver (`dolthub/driver`) pulled in the entire wazero WebAssembly runtime, which added ~127MB of binary weight and a 2-second JIT compilation penalty on every invocation on Linux and Windows. Beads now requires an external Dolt SQL server (`bd dolt start` or `dolt sql-server`), and in exchange, startup is instant and the binary ships at ~41MB across all platforms.

The CGO build-tag bifurcation that split the codebase into `cgo` and `nocgo` variants is also gone. One build path, one binary, everywhere.

## Wisps Move to Dolt

The SQLite ephemeral store was always a workaround — wisps needed fast, uncommitted writes that wouldn't bloat Dolt history. The solution is `dolt_ignore`: a dedicated `wisps` table that Dolt tracks locally but excludes from push/pull. Wisps get the same query engine as regular issues without the sync overhead.

Run `bd migrate wisps` to move existing ephemeral data from SQLite to the new table.

## JSONL Is Fully Gone

The remaining ~500 JSONL references have been purged. `bd sync` is a deprecated no-op. JSONL bootstrap, JSONL recovery in `bd doctor`, JSONL-based restore — all removed. The fork protection code that checked whether you were using JSONL or Dolt is dead code now (commit `9da90394`). Dolt-native push/pull via git remotes is the only sync path.

## New Capabilities

**Metadata is queryable.** `bd list --metadata-field key=value`, `bd search`, and `bd query` all support metadata filters now. `bd show` and `bd list --long` display metadata in human-readable format rather than hiding it in JSON blobs. PRs [#1908](https://github.com/steveyegge/beads/pull/1908) and [#1905](https://github.com/steveyegge/beads/pull/1905).

**OpenTelemetry instrumentation** is available as an opt-in. Hook and storage operations emit OTLP traces for debugging complex molecule execution flows. PR [#1940](https://github.com/steveyegge/beads/pull/1940).

**Transaction infrastructure** wraps Dolt operations in proper transactions with isolation, retry, and batch semantics. `bd mol bond`, `bd mol squash`, and `bd mol cook` are now atomic — no more half-created molecules on failure. Commit messages flow through to Dolt history, making `dolt log` useful for auditing what `bd` did and when.

**Standalone formula execution** lets `bd mol wisp` run expansion formulas directly, without needing a parent molecule. PR [#1903](https://github.com/steveyegge/beads/pull/1903).

## Community Contributions

This release includes work from 15+ contributors. Notable community fixes: Joseph Turian contributed metadata normalization, ready ordering, doctor improvements, and gosec compliance. Xexr fixed cross-expansion dependency propagation in `bd mol cook` and parent-child display in `bd list`. Nelson Melo fixed Dolt comment persistence. Marco Del Pin tackled early CGO detection. EmreEG added backend-aware deep validation to `bd doctor`. ZenchantLive cleaned up stale daemon references. Wenjix fixed the Jira API v3 search endpoint. Mike Macpherson removed stale `--from-main` references.

## 30+ Bug Fixes

The full list is in [CHANGELOG.md](CHANGELOG.md), but highlights: `bd ready` now respects SortPolicy and correctly handles `waits-for` dependencies. The `--limit` flag on `bd list` applies after `--sort` (not before). Doctor no longer false-positives on noms LOCK files — it uses `flock` probes instead of file existence. Hook shim templates use the correct `bd hooks run` command. N+1 query patterns in dependency/label loading are batched with per-invocation caching (PR [#1874](https://github.com/steveyegge/beads/pull/1874)).

## Breaking Changes

**Embedded Dolt mode is removed.** If you were running without an external Dolt server, you now need one: `bd dolt start` launches a local instance. This is the only breaking change, and `bd doctor` will detect the situation and guide you.

**`bd sync` is a no-op.** It prints a deprecation notice. Use `dolt push`/`dolt pull` directly, or configure git remotes for automatic sync.

## Upgrade

```bash
brew upgrade bd
# or
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash
```

After upgrading, run `bd migrate wisps` if you have existing ephemeral data, and ensure you have a Dolt server running (`bd dolt start`).

Full changelog: [CHANGELOG.md](CHANGELOG.md) | GitHub release: [v0.56.1](https://github.com/steveyegge/beads/releases/tag/v0.56.1)

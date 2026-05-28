# CI Cleanup Plan

Last reviewed: 2026-05-28

Freshness source: `docs/CI_TEST_SURFACE_AUDIT.md`, `.github/workflows/*.yml`,
`.buildflags`, `.golangci.yml`, package test manifests, and maintainer decision
review on 2026-05-28.

This document records the agreed target shape for CI cleanup. It is the policy
and roadmap layer; the current inventory remains in
[`CI_TEST_SURFACE_AUDIT.md`](CI_TEST_SURFACE_AUDIT.md).

## Goals

- Make every important CI tier reproducible through a repository-owned command.
- Keep PR checks fast, required, and Linux-only unless risk justifies more.
- Run expensive platform and integration coverage on `main`, manual dispatch, or
  scheduled background jobs after measuring wall-clock cost.
- Make release/package checks rerun release-critical validation before
  publishing, independent of earlier `main` success.
- Preserve current behavior first, then measure and promote additional suites.

## Non-Goals

- Do not make `.test-skip` part of CI. It is a local human optimization file.
- Do not run macOS or Windows checks on PRs by default.
- Do not make Codecov/upload success block PRs or `main`.
- Do not broaden `pull_request_target` usage for package validation.

## Tier Model

| Tier | Trigger | Required | Platform | Purpose |
|---|---|---:|---|---|
| `pr-core` | Every PR and merge queue run | Yes | Linux | Fast baseline Go validation for the shipped default path. |
| `pr-policy` | Every PR and merge queue run | Yes | Linux | Repository policy checks that should fail before expensive tests matter. |
| `pr-lint` | Every PR and merge queue run | Yes | Linux | Required `gofmt` and `golangci-lint` gate. |
| `pr-risk-*` | PRs matching risky paths or maintainer labels | Yes when applicable | Linux | Descriptive risk checks such as embedded, regression, Nix, packages, and release paths. |
| `main-*` | Every push to `main` | Yes for branch health | Linux plus selected macOS/Windows | Detect after-merge issues from direct pushes and platform-specific behavior. |
| `measure-*` | Manual dispatch | No | Per suite | Collect wall-clock and sharding data before promoting suites. |
| `nightly-*` | Scheduled/manual | No, but failures require triage | Linux unless measured otherwise | Expensive background coverage not ready for every `main` push. |
| `release-*` | Tags/manual release | Yes before publish | Per artifact | Re-run release-critical checks and publish only after package gates pass. |

`merge_group` means GitHub Merge Queue. Treat it like a PR event: run Linux
`pr-core`, `pr-policy`, `pr-lint`, and the same risk checks; do not add macOS or
Windows there.

## Required PR Checks

Every PR, including docs-only PRs, should run the required Linux baseline:
`pr-core`, `pr-policy`, and `pr-lint`.

## Wrapper Conventions

Shell scripts under `scripts/ci/` are the source of truth. Make targets should
be aliases for discoverability, not a second implementation of command policy.

Wrapper rules:

- Auto-detect the repository root from the script path.
- Source `.buildflags` except for explicitly special modes such as no-CGO or
  unsupported-install checks.
- Do not require a clean worktree.
- Clean up temporary files that the wrapper creates.
- Use `scripts/ci/lib/timing.sh` for new measured command blocks.
- Keep CI-specific reporting such as `gotestsum`, JUnit, and artifacts in the
  workflow layer unless the wrapper needs to own the behavior.

### `pr-core`

Initial wrapper behavior must preserve the current Linux PR command exactly:

```bash
source ./.buildflags
go test -race -short -skip '^TestEmbedded' ./...
```

CI may wrap that command with `gotestsum` for logs and JUnit, but the underlying
test contract must remain identical during the first migration.

Additional rules:

- Use `.buildflags` so `gms_pure_go` remains the default shipped path.
- Keep `-race`.
- Keep `-short` initially only to avoid behavior drift.
- Do not generate coverage in PRs.
- Do not read `.test-skip`.

### `pr-policy`

`pr-policy` should be a separate wrapper from `pr-core`. It should include:

- Build tag policy: `scripts/check-build-tags.sh`.
- Unsupported `go install` guidance: `scripts/check-go-install-guidance.sh`.
- Version consistency: `scripts/check-versions.sh`.
- CLI flag freshness: `make check-docs` or the underlying doc flag script.
- PR guard for `.beads/issues.jsonl` changes.

### `pr-lint`

`pr-lint` is required. It should stay separate from policy so lint failures are
easy to identify and rerun. It includes:

- `make fmt-check`.
- `golangci-lint run --timeout=5m --build-tags=gms_pure_go ./...`.

Known false positives must be handled in `.golangci.yml` or with targeted
`//nolint` comments. CI should not use a tolerated failing lint baseline.

## Risk Checks

Use separate, descriptive jobs rather than one broad "extra tests" job:

- `pr-risk-embedded`
- `pr-risk-regression`
- `pr-risk-nix`
- `pr-risk-packages`
- `pr-risk-release`

Use the robust path-gated required-check pattern:

1. A detector job always runs and decides which risk checks are applicable.
2. Each required risk check reports success when it is not applicable.
3. Applicable checks fail normally on command failure.

Embedded Dolt coverage is risk-gated on PRs and always runs on `main`. Add a
maintainer `run-embedded` label and a rare maintainer-only `skip-embedded`
override. Regression coverage follows the same pattern with `run-regression`
and `skip-regression`, while still running on every `main` push.

## Main Branch Checks

`main` should run as much as practical after measurement. Direct pushes to
`main` are allowed in this repository, so after-merge detection matters.

Initial main policy:

- Re-run Linux `pr-core`.
- Run Linux coverage collection on `main`, not on PRs or merge queue.
- Run current macOS Go test shape without coverage upload:
  `go test -tags gms_pure_go -v -race -short -skip '^TestEmbedded' ./...`.
- Run Windows smoke only for now: build, `version`, and `help`.
- Run embedded Dolt every `main` push.
- Run regression every `main` push.

Candidate promotions for every `main` push must be measured first. The working
wall-clock target is about 25 minutes total. Suites that exceed that target or
create too much queue pressure should stay manual/scheduled until sharded.

No-short integration is an intended every-main candidate, not nightly-only by
policy; promote it after measurement if wall-clock data supports it.

Coverage collection should block on local coverage generation/test failures, not
on upload service failures. Do not introduce a coverage threshold during the
first promotion step.

The `main` branch may fail after merge as a cost tradeoff, but failures should
be fixed forward or reverted promptly.

## Measurement Workflow

Add a manual-dispatch workflow before changing tier breadth.

Initial implementation lives in `.github/workflows/ci-measurements.yml` on
branch `ci/bd-am3.1-wrapper-commands`. It is manual-only for human operators:
direct `workflow_dispatch` when available, or `workflow_call` from the existing
nightly workflow while the new workflow file is still branch-local. It measures
one selected suite per dispatch so maintainers can control macOS, package, and
integration cost.

Until `.github/workflows/ci-measurements.yml` exists on `main`, dispatch it from
the branch through the existing `Nightly Full Tests` workflow by selecting any
suite other than `full-test`. After the measurement workflow is on `main`, it
can be dispatched directly.

Measurement requirements:

- One sample per suite is enough initially.
- Measure per command, not only per job, so future sharding decisions have data.
- Use a shared `scripts/ci/lib/timing.sh` helper in new wrappers.
- Print timing summaries to logs and `$GITHUB_STEP_SUMMARY`.
- Preserve command exit codes; measurement jobs should fail visibly when the
  measured command fails.
- Retain artifacts for seven days.
- Pin `gotestsum`; replace current `gotestsum@latest` opportunistically when
  touching nearby workflow code.
- Use `gotestsum` for Linux Go measurement outputs. Install it one-off in the
  workflow rather than making wrappers depend on it.

Selectable measurement suites:

- `pr-linux`: PR policy, core, and lint command timings on Linux with JUnit for
  the core Go test command.
- `macos-short`: current macOS short Go test shape.
- `macos-candidates`: macOS no-short and integration-tag candidates.
- `linux-integration`: current nightly-style integration run with
  `BEADS_TEST_SKIP=dolt`.
- `linux-integration-coverage`: same integration shape with coverage generation
  and a coverage summary, but no threshold.
- `cross-version-smoke`: one previous-release smoke sample, optionally pinned
  by workflow input.
- `nix`: full `nix build .#default`.
- `mcp-package`, `npm-package`, and `website`: package and documentation probes
  for measurement. These are not promoted gates yet.

Measure at least:

- Current Linux `pr-core`, policy, and lint timing.
- Current macOS short Go test command.
- macOS no-short/integration candidates.
- Linux no-short integration preserving current nightly shape:
  `BEADS_TEST_SKIP=dolt` with `-tags=integration,gms_pure_go`.
- Linux no-short integration with coverage, matching the current nightly signal.
- A cross-version smoke sample.
- Full `nix build`.
- Package checks for MCP, npm, and website.

### Initial Wrapper Measurement Snapshot

First sample: PR #4211, workflow run 26549957718, commit
`c5fd8fc34b3f28ab5a507b02a8fc9f1faf051d13`.

This sample was collected from additive wrapper jobs on the cleanup branch, not
from `main`. Treat it as a measurement smoke test and first baseline only; do
not make final tiering or sharding decisions from one run.

| Wrapper | Job wall clock | Wrapper command timing | Current long pole |
|---|---:|---:|---|
| `pr-policy` | 91s | 65s | `build bd for docs checks` at 53s |
| `pr-core` | 593s | 577s | `go test -race -short -skip '^TestEmbedded' ./...` |
| `pr-lint` | 211s | 143s, plus 54s tool install | `golangci-lint` at 143s |

Same-run job-level observations:

- Existing Linux short test job took 607s, matching the new `pr-core` wrapper
  shape closely enough for the first no-drift check.
- macOS short test took 383s; keep it off PRs and measure again before deciding
  whether to shard main-platform coverage.
- Windows smoke took 237s; keep it as smoke-only until there is evidence that
  broader Windows tests are worth the queue cost.
- Embedded Dolt had a slow-shard tail: storage 355s, cmd 19/20 403s, cmd 7/20
  387s, and cmd 4/20 334s. Sharding decisions should use repeated samples.

Second sample: branch-dispatched `pr-linux` measurement via `Nightly Full
Tests`, workflow run 26551186971, commit
`f4918fdaf97d659b568068ad2c1cfee88c8f118d`.

| Command | Duration | Notes |
|---|---:|---|
| `install gotestsum` | 10s | Pinned `gotestsum@v1.13.0`. |
| `install golangci-lint` | 41s | Pinned `golangci-lint@v2.9.0`. |
| `pr-policy wrapper` | 46s | Long pole was docs-check binary build at 32s. |
| `pr-core gotestsum` | 461s | Uploaded `pr-core-junit.xml` artifact. |
| `pr-lint wrapper` | 17s | Excludes lint tool install because install is measured separately. |

The full job wall clock was 607s. An earlier branch-dispatched run,
26550681849, failed visibly in `pr-core gotestsum` after 566s with
`TestInitRepairsPermissiveBeadsDir` hitting a `TempDir RemoveAll` cleanup error
for a non-empty `.git` directory. The workflow now continues independent
commands after a failure and exits nonzero at the end, so later samples still
collect lint timing when core flakes.

### Branch Measurement Batch

The first broad batch was dispatched from branch
`ci/bd-am3.1-wrapper-commands`, commit
`35135f235e49051bb777bcbb31d95291766eb190`, through `Nightly Full Tests` while
the reusable measurement workflow was still branch-local.

| Suite | Run | Job wall clock | Result | Command timings |
|---|---:|---:|---|---|
| `website` | 26552244751 | 72s | Pass | `npm ci` 9s; typecheck 1s; `llms-full` 1s; build 45s. |
| `nix` | 26552243745 | 209s | Pass | `nix build .#default` 197s. |
| `macos-short` | 26552238862 | 392s | Pass | macOS short Go test 316s. |
| `macos-candidates` | 26552239802 | 774s | Fail | no-short Go test 370s; integration-tag Go test 341s. |
| `linux-integration` | 26552240869 | 664s | Fail | install `gotestsum` 10s; integration Go test 632s. |
| `linux-integration-coverage` | 26552241818 | 818s | Fail | install `gotestsum` 13s; integration coverage Go test 790s; manual coverage summary from artifact was 37.9%. |
| `cross-version-smoke` | 26552242789 | 163s | Fail | candidate binary build 150s; previous-release smoke failed before running because no release tag was resolved. |
| `npm-package` | 26552245608 | 201s | Fail | package binary build 161s; install 0s; `npm run test:all` 23s. |
| `mcp-package` | 26552246442 | 173s | Fail | install `uv` 8s; package binary build 150s; `uv sync` 2s; Ruff failed before later checks ran. |

Failure modes from this batch:

- `macos-candidates`: no-short passed, but integration-tag tests failed in
  `internal/beads` on symlink deduplication, in `internal/doltserver` with
  repeated `fatal: empty ident name not allowed`, and in `internal/storage/dolt`
  on missing `depends_on_external`.
- `linux-integration` and `linux-integration-coverage`: both reported
  `cmd/bd TestAutoMigrateOnVersionBump_NoDatabase` through gotestsum and failed
  three `internal/storage/dolt` routing tests because `depends_on_external` was
  missing from the queried schema.
- `cross-version-smoke`: the workflow did not pass an explicit release tag and
  the script could not infer one. The measurement workflow now resolves the
  latest GitHub release tag when the input is empty.
- `npm-package`: the Claude Code for Web simulation failed because the expected
  JSONL file was not created. The measurement workflow now continues to the
  package dry-run after `test:all` failures.
- `mcp-package`: Ruff found 130 errors. The measurement workflow now continues
  through mypy, pytest, and build after independent package check failures.

Initial tiering read:

- `website` and `nix` are cheap enough to be good PR-risk or main candidates,
  pending path-gating policy.
- `macos-short` is feasible for `main` but still too expensive for default PRs.
- Linux integration and coverage are close to 11-14 minutes per unsharded job
  and currently fail, so measure sharding and fix failures before promotion.
- Package gates need cleanup before they can protect releases or risk paths.

Follow-up robustness reruns used commit
`d6b1314d8a7ab2c85656cfa440ca2c4cd8620087`, after the measurement workflow was
updated to continue independent package commands and resolve the default smoke
release tag:

| Suite | Run | Job wall clock | Result | Added signal |
|---|---:|---:|---|---|
| `cross-version-smoke` | 26552895263 | 171s | Pass | Auto-resolved `v1.0.4`; candidate build 149s; upgrade smoke 7s. |
| `npm-package` | 26552895239 | 212s | Fail | Binary build 154s; install 2s; `test:all` still failed after 23s; pack dry-run succeeded in 13s with 7 files, 79.9 MB package size, and 189.5 MB unpacked size. |
| `mcp-package` | 26552895273 | 264s | Fail | Install `uv` 2s; binary build 156s; `uv sync` 2s; Ruff failed with 130 errors; mypy failed with 4 errors in 2 files after 13s; pytest failed after 72s with 5 failed, 190 passed, 5 skipped, and 15 errors; build still succeeded. |
| `linux-integration-coverage` | 26552895222 | 856s | Fail | Install `gotestsum` 13s; coverage Go test failed after 821s with the same four failures as the first sample; coverage summary still ran and reported 37.9%. |

Roadmap updates from the measurement evidence:

- Keep the wrapper timing jobs additive until branch protection can switch to
  `pr-core`, `pr-policy`, and `pr-lint` by name.
- Promote `cross-version-smoke` to use explicit or auto-resolved release tags;
  the candidate build dominates its runtime, while the smoke scenarios are
  cheap once the binary exists.
- Treat `website` and `nix` as good first PR-risk candidates because they are
  relatively cheap and already green in the measurement workflow.
- Do not promote `npm-package` or `mcp-package` as blocking gates until the
  measured package failures are fixed. They are still release-critical checks,
  so cleanup should happen before release workflow hardening.
- Do not promote Linux integration coverage yet. It is currently red and near
  the upper end of the 25-minute total main-branch budget before sharding.
- Use the collected per-command timings to shard around the long poles: Go
  package tests, candidate binary builds for package/smoke jobs, and macOS
  integration-tag tests.

## Package Gates

Package checks should be reusable from PR risk jobs, measurement jobs, `main`,
and release workflows.

### MCP Python Package

Build a candidate `bd` once and put it on `PATH`. Test only the `bd` binary
name, not the `beads` alias.

Wrapper command sequence:

```bash
go build -tags gms_pure_go -o /tmp/bd-mcp-test ./cmd/bd
cd integrations/beads-mcp
uv sync --all-groups
uv run ruff check src/beads_mcp tests
uv run mypy src/beads_mcp
uv run pytest --durations=50
uv build
```

### npm Package

`npm-package` currently has no lockfile, so use `npm install` until a separate
packaging cleanup adds one. Build the native binary expected by
`npm-package/bin/bd.js`, and clean it up on exit by default.

Wrapper command sequence:

```bash
go build -tags gms_pure_go -o npm-package/bin/bd ./cmd/bd
cd npm-package
npm install
npm run test:all
npm pack --dry-run
```

The existing integration test already exercises a real `npm pack`; keep both
that real pack and the explicit dry-run file-list check.

### Website

Classify `scripts/generate-llms-full.sh` as a docs/website check, not generic
policy.

Wrapper command sequence:

```bash
cd website
npm ci
npm run typecheck
cd ..
./scripts/generate-llms-full.sh
cd website
npm run build
```

Keep the internal link check in Actions through the existing Lychee workflow
step. External link checking remains non-blocking.

## Mandatory `testing.Short()` Cleanup

`-short` currently does double duty: it suppresses true slow tests and also acts
as an implicit integration/e2e tier boundary. That is mandatory cleanup before
the tier policy is considered complete.

Plan:

1. Audit every `testing.Short()` use.
2. Keep `testing.Short()` only for runtime, stress, or large-fixture skips.
3. Move integration, e2e, API, Docker, and external dependency boundaries to
   explicit build tags, environment checks, or named wrappers.
4. Update `docs/TESTING.md` after wrapper commands exist.

## Release Policy

Release/tag workflows must independently re-run release-critical checks even
when `main` was green. Publishing should happen only after package-specific
checks pass.

- Reuse `scripts/ci/package-*` wrappers in release jobs.
- MCP release may upload `dist/*` produced by the same validated `uv build`.
- npm release should explicitly build/copy the native binary needed for
  publishing; the validation wrapper should clean temporary binaries by default.
- Keep privileged automation isolated and audited. Do not broaden normal
  validation to `pull_request_target`.

## Implementation Order

1. Add `scripts/ci/*` wrappers and `scripts/ci/lib/timing.sh`; add Make aliases.
   Initial PR wrappers exist on branch `ci/bd-am3.1-wrapper-commands`.
2. Wire existing workflows to wrappers with no intentional behavior drift.
   Additive PR timing jobs exist on branch `ci/bd-am3.1-wrapper-commands`; the
   existing direct jobs remain in place until the wrapper jobs are promoted.
3. Add the manual measurement workflow and pinned `gotestsum`.
   Initial workflow exists on branch `ci/bd-am3.1-wrapper-commands`; the legacy
   Linux coverage install has been pinned from `gotestsum@latest` to
   `gotestsum@v1.13.0`.
4. Add package wrappers and risk/measurement usage for MCP, npm, and website.
5. Perform the mandatory `testing.Short()` audit and cleanup.
6. Promote measured suites to `main` or scheduled jobs based on wall-clock data.
7. Harden release workflows to reuse package wrappers before publishing.
8. Split workflows by tier/domain once wrappers are stable:
   `pr.yml`, `pr-risk.yml`, `main.yml`, `release.yml`, and `nightly.yml`.

## Deferred Decisions

- Exact branch-protection required-check names after workflow split.
- Whether no-CGO should become a full all-package gate or remain focused.
- Coverage thresholds for promoted main suites.
- Final sharding strategy for macOS, integration, and embedded jobs.
- npm lockfile policy for `npm-package`.

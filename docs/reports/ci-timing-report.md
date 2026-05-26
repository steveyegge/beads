# CI Timing Report

Generated: 2026-05-26 22:39 UTC

Repository: `gastownhall/beads`

Command: `python3 scripts/ci/ci_timing_report.py --repo gastownhall/beads --runs 120 --out-dir docs/reports`

## Data Set

- Recent workflow runs requested: 120
- Workflow runs returned: 120
- Completed runs analyzed: 114
- Jobs analyzed: 1466
- Incomplete runs at collection time: 6
- Cancelled runs: 5
- Failed/non-success non-cancelled runs: 8
- Successful PR CI wall-clock P50/P95: 11.6m / 14.1m
- All successful workflow wall-clock P50/P95: 9.4m / 12.8m
- Run queue P50/P95: 0s / 0s
- Job queue P50/P95: 2s / 49s

GitHub `run_started_at` is currently equal to `created_at` for most runs, so run-level queue time is less useful than per-job queue time. Job queue time uses `job.created_at` to `job.started_at` when present.

## Checked Workflow Files

| Workflow | Local file |
|---|---|
| CI | `.github/workflows/ci.yml` |
| Cross-Version Smoke Tests | `.github/workflows/cross-version-smoke.yml` |
| Regression Tests | `.github/workflows/regression.yml` |
| nix build | `.github/workflows/nix-build.yml` |
| Nightly Full Tests | `.github/workflows/nightly.yml` |
| Migration Test Harness | `.github/workflows/migration-test.yml` |

## Wall-Clock by Workflow and Event

| Workflow | Event | Runs | Success | P50 | P95 | Max |
|---|---:|---:|---:|---:|---:|---:|
| CI | pull_request | 29 | 23 | 11.4m | 15.7m | 16.2m |
| CI | push | 4 | 4 | 11.7m | 13.5m | 13.5m |
| Copilot | dynamic | 1 | 1 | 46s | 46s | 46s |
| Cross-Version Smoke Tests | pull_request | 29 | 29 | 3.2m | 4.5m | 4.7m |
| Deploy Documentation | push | 3 | 3 | 2.3m | 2.5m | 2.5m |
| Nightly Full Tests | schedule | 1 | 0 | 10.6m | 10.6m | 10.6m |
| Regression Tests | pull_request | 31 | 30 | 9.5m | 10.9m | 11.3m |
| Regression Tests | push | 5 | 5 | 9.6m | 9.8m | 9.8m |
| Update vendorHash for dependabot Go bumps | pull_request_target | 3 | 0 | 1s | 1s | 1s |
| nix build | pull_request | 3 | 3 | 3.5m | 3.6m | 3.6m |

## Slowest Jobs

| Job | Runs | Workflow | P50 | P95 | Max | Failure rate | Queue P95 |
|---|---:|---|---:|---:|---:|---:|---:|
| Test (macos-latest) | 37 | CI | 9.0m | 11.9m | 15.6m | 16% | 17s |
| Test (ubuntu-latest) | 37 | CI | 10.3m | 10.6m | 10.7m | 16% | 44s |
| Full Test Suite | 1 | Nightly Full Tests | 10.5m | 10.5m | 10.5m | 100% | 3s |
| Differential Regression (v0.49.6 baseline) | 36 | Regression Tests | 9.2m | 9.6m | 10.6m | 3% | 16s |
| Test (Embedded Dolt Storage) | 37 | CI | 6.1m | 6.5m | 7.6m | 11% | 30s |
| Test (Embedded Dolt Cmd shard) | 640 | CI | 2.6m | 6.1m | 8.1m | 3% | 1.4m |
| Test (Windows - smoke) | 37 | CI | 2.7m | 5.6m | 7.2m | 3% | 8s |
| Build (Embedded Dolt) | 37 | CI | 4.5m | 4.7m | 4.7m | 11% | 34s |
| Test (storage domain + uow) | 37 | CI | 3.7m | 4.0m | 4.0m | 5% | 3s |
| Test Nix Flake | 37 | CI | 3.5m | 3.8m | 3.9m | 5% | 1.0m |
| nix build .#default | 3 | nix build | 3.5m | 3.5m | 3.5m | 0% | 2s |
| Upgrade smoke (version shard) | 150 | Cross-Version Smoke Tests | 2.9m | 3.1m | 3.2m | 3% | 33s |
| Lint | 37 | CI | 2.8m | 3.0m | 3.1m | 8% | 38s |
| build | 3 | Deploy Documentation | 1.9m | 2.0m | 2.0m | 0% | 3s |
| Check cmd/bd pure-Go tests compile (CGO_ENABLED=0) | 37 | CI | 1.4m | 1.5m | 1.5m | 3% | 3s |
| Check doc flags freshness | 37 | CI | 1.2m | 1.3m | 1.6m | 5% | 35s |
| copilot-pull-request-reviewer | 1 | Copilot | 40s | 40s | 40s | 0% | 2s |
| Check formatting | 37 | CI | 16s | 22s | 35s | 8% | 1.1m |
| Detect CI tier | 37 | CI | 13s | 21s | 35s | 3% | 38s |
| Check for .beads changes | 37 | CI | 12s | 19s | 35s | 3% | 51s |

## Critical Path

For successful PR CI runs, the tail job is the last successful job to complete in the workflow run. This is the best available proxy for the Actions critical path from public run/job metadata.

| Tail job | Runs ending here | Median job duration |
|---|---:|---:|
| Test (Embedded Dolt Cmd shard) | 19 | 6.6m |
| Test (Embedded Dolt Storage) | 2 | 6.2m |
| Test (macos-latest) | 1 | 11.9m |
| Test (ubuntu-latest) | 1 | 10.0m |

## Failure and Flakiness Suspects

Failures are stronger flake suspects than cancellations; cancellations often come from concurrency replacing older PR runs.

| Job | Failures | Cancels | Runs | Failure rate | Non-success rate | P95 duration |
|---|---:|---:|---:|---:|---:|---:|
| Test (Embedded Dolt Cmd shard) | 2 | 20 | 640 | 0% | 3% | 6.1m |
| Test (macos-latest) | 2 | 4 | 37 | 5% | 16% | 11.9m |
| Test (ubuntu-latest) | 2 | 4 | 37 | 5% | 16% | 10.6m |
| Check formatting | 2 | 1 | 37 | 5% | 8% | 22s |
| Build (Embedded Dolt) | 1 | 3 | 37 | 3% | 11% | 4.7m |
| Lint | 1 | 2 | 37 | 3% | 8% | 3.0m |
| Check doc flags freshness | 1 | 1 | 37 | 3% | 5% | 1.3m |
| Full Test Suite | 1 | 0 | 1 | 100% | 100% | 10.5m |
| Differential Regression (v0.49.6 baseline) | 1 | 0 | 36 | 3% | 3% | 9.6m |
| Upgrade smoke (version shard) | 0 | 5 | 150 | 0% | 3% | 3.1m |
| Test (Embedded Dolt Storage) | 0 | 4 | 37 | 0% | 11% | 6.5m |
| Test (Embedded Dolt Cmd ${{ matrix.shard }}/${{ strategy.job-total }}) | 0 | 3 | 5 | 0% | 60% | 0s |
| Test (storage domain + uow) | 0 | 2 | 37 | 0% | 5% | 4.0m |
| Test Nix Flake | 0 | 2 | 37 | 0% | 5% | 3.8m |
| Test (Windows - smoke) | 0 | 1 | 37 | 0% | 3% | 5.6m |

## Skipped and Pending Patterns

| Job | Skipped | Observations |
|---|---:|---|
| Check for .beads changes | 6 | Mostly expected conditional gating or cancelled superseded runs. |
| Update default.nix vendorHash | 3 | Mostly expected conditional gating or cancelled superseded runs. |
| Test (Embedded Dolt Storage) | 2 | Mostly expected conditional gating or cancelled superseded runs. |
| Test (Embedded Dolt Cmd ${{ matrix.shard }}/${{ strategy.job-total }}) | 2 | Mostly expected conditional gating or cancelled superseded runs. |
| Differential Regression (v0.49.6 baseline) | 2 | Mostly expected conditional gating or cancelled superseded runs. |
| Build (Embedded Dolt) | 1 | Mostly expected conditional gating or cancelled superseded runs. |
| Regression Tests run 26479093922 | 0 | `in_progress` at collection time. |
| CI run 26479093903 | 0 | `in_progress` at collection time. |
| Regression Tests run 26479089036 | 0 | `in_progress` at collection time. |
| CI run 26479089035 | 0 | `in_progress` at collection time. |
| Cross-Version Smoke Tests run 26479089033 | 0 | `in_progress` at collection time. |
| Regression Tests run 26478907498 | 0 | `in_progress` at collection time. |

## Cost and Noise Hot Spots

- Successful job runner time in sample: 4164.3m.
- macOS successful job time: 323.3m. This is a premium hosted runner lane and should stay risk-gated.
- Windows successful job time: 110.8m. It is currently a smoke lane, which is appropriate.
- Embedded Dolt successful job time: 2264.5m across build/storage/cmd shard jobs. The shard fan-out is the dominant PR noise source when enabled.
- Codecov uploads are steps inside the Linux test job rather than separate jobs in the public Jobs API. They are `continue-on-error`, so they add log/check noise and possible tail latency but usually should not block.

## Tier Recommendations

| Tier | Recommended contents | Why |
|---|---|---|
| Fast PR | build-tag policy, version consistency, no-beads check, doc flags, fmt, lint, Linux pure-Go short test, Windows smoke | Keeps default PR feedback focused on broad regressions and low queue/cost jobs. |
| Risk PR | storage domain/uow, macOS race test, Cross-Version Smoke Tests, Regression Tests, Nix path workflow | Run when touched paths or labels indicate storage, packaging, release compatibility, Nix, or CLI behavior risk. |
| Merge queue | Fast PR plus Linux short test, Windows smoke, regression detector, selected risk jobs for touched paths | Protects `main` without forcing every contributor edit through the full embedded matrix. |
| Main push | Full CI as currently defined, docs deploy, regression, Nix when inputs changed | Main can absorb broader coverage after merge while preserving signal for regressions introduced by integration. |
| Nightly | Nightly Full Tests, full embedded Dolt matrix, migration fidelity, longer cross-version count | Expensive and compatibility-heavy jobs belong here unless a PR explicitly changes the covered surface. |
| Manual | Release, Test PyPI Publish, update flake/vendor hash workflows | Operator-triggered or bot-maintenance workflows should remain outside default PR critical path. |

## Key Findings

- Recent successful PR CI P50/P95 wall-clock is 11.6m / 14.1m.
- CI critical path is typically the longest Linux/macOS short test lane, storage/uow, or embedded cmd shard when the embedded tier is enabled.
- Regression Tests are quick when skipped by detector, but become a meaningful PR lane when path risk or labels enable the differential suite.
- Cross-Version Smoke Tests are parallel version shards; they are bounded, but they create check noise on routine PRs.
- The embedded Dolt build plus 20 cmd shards has high aggregate runner-minute cost and should remain risk-gated, main/nightly, or manually requested.

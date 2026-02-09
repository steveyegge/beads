# OJ Integration Assessment (2026-02-08)

> **Assessed by**: 5 parallel agents examining architecture, feasibility, codebase alignment, prioritization, and gaps
> **Epics reviewed**: od-vq6 (hardening), od-ki9 (convergence), od-k3o (bus maturation)
> **Codebases inspected**: beads (Go), gastown (Go), oddjobs (Rust)

## Scorecard

| Dimension | Score | Summary |
|-----------|:-----:|---------|
| Architectural Soundness | 7/10 | Three-layer split (OJ=sessions, Bus=dispatch, GT=policy) is clean and maps to real code boundaries |
| Implementation Feasibility | 6/10 | Cross-lang subprocess overhead, 34 tasks = 10-12 weeks not 4, untested foundations |
| Codebase Alignment | 7/10 | 10/13 plan assumptions confirmed against code; NotifyAdapter swap is real; OJ event types don't exist yet |
| Prioritization & Sequencing | 6/10 | Wrong epic ordering (k3o should start first); vq6.1 urgency is false (GT_SLING_OJ not enabled); over-planned |
| Gaps & Blind Spots | 6/10 | No latency benchmark, no handler timeouts, no state reconciliation, flat NATS security |
| **Overall** | **6.4/10** | **Architecturally sound; execution plan needs significant revision** |

## Consensus Findings (all 5 agents agree)

1. **The three-layer split is correct.** OJ owns sessions, BD Bus owns dispatch, GT owns policy. This maps to real code boundaries and each system's genuine strengths.

2. **`bd bus register` and handler persistence DON'T EXIST YET.** od-k3o.2 and od-k3o.4 are the single most blocking prerequisite. Everything past Phase 1 is gated on these. Every handler registration shown in the research docs is aspirational.

3. **Phase 4 (beads as authoritative OJ state) is unrealistic.** OJ's `MaterializedState` is 900+ lines of deeply nested operational state (timers, inflight items, step history, session reconnection, worker concurrency). Beads' Issue model is fundamentally flat. OJ WAL must remain authoritative; beads should be a readable projection.

4. **Subprocess overhead (`bd bus emit` from Rust) is fine for fire-and-forget, problematic for blocking decisions.** Phase 3 needs direct NATS from Rust (async-nats crate) instead of 4-process-spawn chains.

5. **The BusNotifyAdapter swap is the highest-confidence win.** Single trait, single constructor site, zero runtime changes needed. Start here to validate the pattern.

## Critical Issues (P0 -- must fix before implementation)

| Issue | Evidence | Action |
|-------|----------|--------|
| No handler persistence or registration CLI | `Bus.Register()` is in-memory only; `OpBusRegister` only in design doc | Build od-k3o.2 + k3o.4 first |
| No latency benchmark for `bd bus emit` | Deep research line 549: "currently unknown"; Go startup alone is 15-30ms | Promote k3o.9 to Phase 0 |
| No handler timeout enforcement | `Bus.Dispatch` runs handlers sequentially with no per-handler timeout; StopDecisionHandler blocks 1hr | Add `context.WithTimeout` per handler + `Unregister()` kill switch |
| Phase 4 scope unrealistic | OJ WAL has timers, inflight HashSets, step history, session reconnect state; beads has flat Issues | Reclassify: OJ WAL authoritative, beads = projection |

## High Priority Issues (P1)

| Issue | Evidence | Action |
|-------|----------|--------|
| `oj_job_id` is text convention, not schema field | Stored as key-value line in bead description; parsed by GT `AttachmentFields`; invisible to beads | Document fragility; consider first-class metadata |
| NotifyAdapter `(title, message)` too narrow | Structured events (job ID, state, metadata) must serialize into message string | Create dedicated `BusEmitAdapter` trait alongside NotifyAdapter |
| `GT_SLING_OJ` not enabled anywhere | No deployment config, Helm values, or K8s config sets it; vq6.1 urgency is false | Gate the flag flip on safety guards, don't block all work |
| Zero tests for sling_oj.go (272 lines) | 6 functions with no test file; `dispatchToOj()` does name alloc, base64, subprocess, parsing | Write tests before building integration on top |
| NATS binds `0.0.0.0` with flat token auth | `nats.go:73` Host: "0.0.0.0", `nats.go:84` single shared token, no ACLs | Restrict to `127.0.0.1`; add accounts before cross-pod |
| GT backend tests entirely stubbed | `gastown/internal/beads/backend_test.go` has 15 `TODO` stubs, 0 real tests | Write tests before building on the bridge |
| No handler rollback mechanism | No `Unregister()` on Bus struct; no way to disable handler at runtime | Add disable/unregister capability |

## Codebase Verification Results

| # | Plan Assumption | Verdict | Location |
|---|----------------|---------|----------|
| 1 | `spawn_runtime_event_forwarder` as tap point | **CONFIRMED** (line 529) | `/tmp/oddjobs/crates/daemon/src/lifecycle/mod.rs` |
| 2 | `NotifyAdapter` trait swappable | **CONFIRMED** | `/tmp/oddjobs/crates/adapters/src/notify/mod.rs:30-33` |
| 3 | Runtime handler files exist | **CONFIRMED** | `crates/engine/src/runtime/handlers/{job_create,lifecycle,agent,worker/dispatch}.rs` |
| 4 | Effect enum extensible | **CONFIRMED** (14 variants, Serialize/Deserialize) | `/tmp/oddjobs/crates/core/src/effect.rs:20` |
| 5 | OJ daemon socket/IPC real | **CONFIRMED** (UDS, 4-byte length-prefix JSON) | `/tmp/oddjobs/crates/daemon/src/protocol_wire.rs` |
| 6 | `sling_oj.go` bridge exists | **CONFIRMED** | `/Users/matthewbaker/gastown/internal/cmd/sling_oj.go:38` |
| 7 | `AutoNukeIfClean()` exists | **CONFIRMED** (line 706) | `/Users/matthewbaker/gastown/internal/witness/handlers.go` |
| 8 | `oj_job_id` on beads | **PARTIAL** -- GT convention in description text, not beads schema | `gastown/internal/beads/fields.go:27` |
| 9 | Bus supports register/block/inject | **CONFIRMED** | `beads/internal/eventbus/{bus,handler,types}.go` |
| 10 | OJ event types in bus | **NOT FOUND** -- only Claude Code hooks, advice, decision types | `beads/internal/eventbus/types.go` |
| 11 | Bus RPC protocol | **CONFIRMED** (bus_emit, bus_status, bus_handlers) | `beads/internal/rpc/protocol.go:139-141` |
| 12 | OJ sessions prefixed `oj-` | **CONFIRMED** -- GT uses bare names | `adapters/src/session/tmux.rs` line 39 |
| 13 | SIGHUP handler reload | **NOT FOUND** -- design fiction, no `LoadPersisted()` exists | `beads/cmd/bd/daemon_unix.go` |

## Simplified Plan: 10 Tasks, 4 Weeks

Replaces the 34-task, 3-epic structure. Achieves ~80% of the value.

| Wk | Task | Origin | Track | Effort |
|----|------|--------|-------|--------|
| 1 | Handler persistence + registration CLI | k3o.2 + k3o.4 | **beads** | 2d |
| 1 | Guard witness + flag merge.hcl | vq6.1 + vq6.2 | **gastown** | 3h |
| 1 | OJ health endpoint | ki9.9 | **oddjobs** | 2d |
| 2 | OJ event types in bd bus | k3o.6 | **beads** | 1d |
| 2 | Benchmark `bd bus emit` latency | k3o.9 | **beads** | 1d |
| 2 | Version headers + auto-close beads | vq6.3 + vq6.4 | **gastown** | 2d |
| 3 | OJ runtime bus emit (event forwarder tap) | k3o.7 | **oddjobs** | 3d |
| 3 | Default OJ event handlers | k3o.8 | **beads** | 2d |
| 3 | Integration tests for OJ dispatch | vq6.5 + k3o.5 | **cross-repo** | 2d |
| 4 | Enable `GT_SLING_OJ=1` on one rig, validate E2E | new | **gastown-uat** | 2d |

### What this drops (and why)

| Dropped | Reason |
|---------|--------|
| ki9.4 (nudge via OJ) | Already partially implemented in `nudge_oj.go` |
| ki9.5 (PGID kill extract) | Pure refactoring, no user-facing value |
| ki9.6 (env injection unification) | Already working via `env_json` base64 var |
| ki9.7 (bypass-perms consolidation) | Both approaches work, no urgency |
| ki9.8 (formulas to runbooks) | Highest effort, most speculative value |
| ki9.10 (queue polling consolidation) | Polling works fine at current scale |
| k3o Phases 4-5 (GT unification, advanced) | Speculative; depends on Phases 1-3 succeeding |
| Deep Integration Phases 2-4 | Defer until Phase 1 is proven |

### What's missing from ALL plans (add before go-live)

- **Per-rig rollout strategy** for `GT_SLING_OJ` (not just global binary flag)
- **Monitoring/alerting** for bus emit failures, handler latency, state divergence
- **Rollback plan** for in-flight OJ jobs if integration breaks
- **Dead-letter queue** for failed handler invocations
- **Event ordering guarantees** across the async OJ-to-bus path

## Architectural Recommendations

| Priority | Recommendation | Rationale |
|----------|---------------|-----------|
| P0 | Build k3o.2 + k3o.4 before any OJ bus work | 8 downstream tasks blocked; no integration possible without this |
| P0 | Benchmark `bd bus emit` latency under load | If >100ms, subprocess approach needs replacement |
| P0 | Rescope Phase 4: OJ WAL stays authoritative | Beads cannot represent OJ operational state richly enough for recovery |
| P1 | Add per-handler `context.WithTimeout` | Prevents slow handler from blocking entire Claude Code session |
| P1 | Plan async-nats direct connection for Phase 3 | Subprocess chains unacceptable for blocking decisions (200-500ms) |
| P1 | Add distinct exit code for local-fallback-no-handlers | OJ cannot currently detect silent event loss when daemon is down |
| P2 | Batch OJ events on Rust side (100ms window) | Prevents event storm under concurrency (5 jobs = 750+ subprocess spawns) |
| P2 | Document GT -> OJ -> bus -> GT runtime cycle | Circular runtime dependency needs explicit failure isolation boundaries |
| P2 | Build reconciliation mechanism for bead drift | Catch up bead projections from OJ state after bus failures |
| P3 | Restrict NATS to 127.0.0.1; add per-consumer ACLs | Current flat auth with 0.0.0.0 binding is insufficient for cross-pod |

## Relationship to Existing Beads

This assessment supersedes and consolidates:
- **od-vq6** (Integration Hardening) -- 6 tasks, folded into simplified plan tasks 2, 6, 9
- **od-ki9** (Convergence Deduplication) -- 10 tasks, partially implemented, remainder folded or dropped
- **od-k3o** (BD Bus Maturation) -- 18 tasks (3/18 done), critical path items (k3o.2/4/5/6/7/8) retained

The 34 original tasks are superseded by the 10-task simplified plan above. Remaining od-ki9 and od-k3o Phase 4-5 tasks proceed only after Week 4 validates the architecture works.

# AGENTS Control-Flow Coverage Matrix (Pass 4, 2026-02-19)

Canonical spec: `/Users/gwizz/.codex/AGENTS.md`  
Runtime binary: `/tmp/beads-bd-new2`  
Repo: `/private/tmp/beads`

Legend:
- owner `cli` = implemented in CLI/source (category 1)
- owner `split` = owned by split-agent docs (category 2)
- owner `task` = tracked as task (category 3)
- status `missing` = deterministic gap requiring task creation (category 4)

| AGENTS requirement | deterministic? | owner (cli\|split\|task) | implementation evidence | task id | status | notes |
|---|---|---|---|---|---|---|
| State-order graph BOOT->...->END | yes | cli | `cmd/bd/state.go:320`, `cmd/bd/state.go:361` |  | implemented | Explicit transition validator command + graph. |
| BOOT hardening + resume guard primitives | yes | cli | `cmd/bd/preflight.go:114`, `cmd/bd/preflight.go:339`, `cmd/bd/resume.go:17` |  | implemented | Preflight/remediation and resume snapshot exist. |
| PLANNING complexity classification (Light/Standard/Deep) | no | split | `docs/agents/planner.md:1` |  | implemented | Judgment policy remains docs-owned. |
| INTAKE state + claim hard-gate | yes | cli | `cmd/bd/flow.go:296`, `cmd/bd/flow.go:383`, `cmd/bd/intake.go:46` |  | implemented | `claim-next` blocks when intake audit proof absent. |
| EXECUTING state lifecycle wrappers | yes | cli | `cmd/bd/flow.go:81`, `cmd/bd/flow.go:3423` |  | implemented | Full deterministic `flow` surface present. |
| RECOVERING state phase orchestration | yes | cli | `cmd/bd/recover.go:194`, `cmd/bd/recover.go:421` |  | implemented | Phases 1-4 emitted deterministically. |
| ABORT state via session_abort | yes | cli | `cmd/bd/flow.go:1513`, `cmd/bd/flow.go:1533` |  | implemented | Writes `ABORT_HANDOFF.md` and optional issue block note. |
| LANDING state deterministic gate engine | yes | cli | `cmd/bd/land.go:34`, `cmd/bd/land.go:365` |  | implemented | Gate 1-4 checks + Gate 3 choreography command path. |
| Execute step 1 scoped ready | yes | cli | `cmd/bd/flow.go:248`, `cmd/bd/flow.go:353` |  | implemented | Uses ready-work filters + scoped claim candidate scan. |
| Execute step 2 pre-claim quality check | yes | cli | `cmd/bd/flow.go:422`, `cmd/bd/flow.go:2537` |  | implemented | `flow preclaim-lint` enforces description/acceptance/verify/labels/deps. |
| Execute step 3 WIP-gated claim | yes | cli | `cmd/bd/flow.go:198`, `cmd/bd/flow.go:215` |  | implemented | Deterministic single-WIP gate in claim path. |
| Execute step 3a post-claim viability + deferred blockers | yes | cli | `cmd/bd/flow.go:318`, `cmd/bd/flow.go:350`, `cmd/bd/flow.go:2741` |  | implemented | Immediate blocker + deferred blocker scan surfaced. |
| Execute step 3b baseline verify | yes | cli | `cmd/bd/flow.go:515`, `cmd/bd/flow.go:575` |  | implemented | Baseline command execution + PASS/FAIL note append. |
| Execute step 4 change strategy (characterize/refactor/change) | no | split | `docs/agents/executor.md:1` |  | implemented | Strategy remains judgment-owned. |
| Execute steps 5-7 discovery tiers | no | split | `docs/agents/executor.md:9` |  | implemented | Tiering policy is agent judgment; CLI provides primitives. |
| Tier-4 discovered task creation/linking | yes | cli | `cmd/bd/flow.go:1777`, `cmd/bd/flow.go:1905` |  | implemented | `create-discovered` enforces discovered-from dependency. |
| Execute step 8 close-safe checklist core | yes | cli | `cmd/bd/flow.go:2110`, `cmd/bd/flow.go:2239`, `cmd/bd/flow.go:2324` |  | implemented | Reason/verified/blocker/gate checks enforced. |
| Pre-close traceability check | yes | cli | `cmd/bd/flow.go:2252`, `cmd/bd/flow.go:3133` |  | implemented | Deterministic check when `--require-traceability` is used. |
| Pre-close spec-drift proof check | yes | cli | `cmd/bd/flow.go:2399`, `cmd/bd/flow.go:3129` |  | implemented | Deterministic check when `--require-spec-drift-proof` is used. |
| Pre-close secret scan | yes | cli | `cmd/bd/flow.go:2381`, `cmd/bd/flow.go:3101` |  | implemented | Secret marker detection enforced unless explicit override. |
| Parent-close cascade check | yes | cli | `cmd/bd/flow.go:2281`, `cmd/bd/flow.go:3174` |  | implemented | Deterministic check in strict/default-strict control mode. |
| Force-close audit schema | yes | cli | `cmd/bd/flow.go:2429`, `cmd/bd/close.go:189` |  | implemented | Force-close requires audit note fields. |
| Living state digest on close | yes | cli | `cmd/bd/flow.go:2472`, `cmd/bd/flow.go:3190` |  | implemented | Digest updated after successful close-safe. |
| Recover Phase 1 quick diagnosis sequence | yes | cli | `cmd/bd/recover.go:232`, `cmd/bd/recover.go:253`, `cmd/bd/recover.go:283` |  | implemented | Gate check facts + scoped ready + blocked snapshot. |
| Recover Phase 2 structural diagnosis | yes | cli | `cmd/bd/recover.go:301`, `cmd/bd/recover.go:314`, `cmd/bd/recover.go:642` |  | implemented | Cycles, stale WIP, hooked, root blockers, gate sets. |
| Recover Phase 3 limbo detection | yes | cli | `cmd/bd/recover.go:353`, `cmd/bd/recover.go:517` |  | implemented | Limbo candidates computed from open-ready-blocked deltas. |
| Recover Phase 4 widen scope | yes | cli | `cmd/bd/recover.go:363`, `cmd/bd/recover.go:412`, `cmd/bd/recover.go:601` |  | implemented | Module-only/unscoped/unassigned widening encoded. |
| Recover Phase 5 convergence signature + escalation | yes | cli | `cmd/bd/recover.go:47`, `cmd/bd/recover.go:103`, `cmd/bd/recover.go:157` |  | implemented | Signature compare, escalation threshold, optional anchor note write. |
| Transition 1 claim_failed | yes | cli | `cmd/bd/flow.go:1099` |  | implemented | Deterministic requeue result. |
| Transition 2 claim_became_blocked | yes | cli | `cmd/bd/flow.go:1370`, `cmd/bd/flow.go:1623` |  | implemented | Block + optional blocker dependency. |
| Transition 3 exec_blocked | yes | cli | `cmd/bd/flow.go:1375`, `cmd/bd/flow.go:1623` |  | implemented | Block with context pack. |
| Transition 4 test_failed | yes | cli | `cmd/bd/flow.go:1393`, `cmd/bd/flow.go:1623` |  | implemented | FAIL note path is deterministic. |
| Transition 5 conditional_fallback_activate | yes | cli | `cmd/bd/flow.go:1411`, `cmd/bd/flow.go:1473` |  | implemented | Decision note + failure-close enforced. |
| Transition 6 priority_poll | yes | cli | `cmd/bd/flow.go:626`, `cmd/bd/flow.go:2354` |  | implemented | Priority poll command + freshness checks for close-safe. |
| Transition 7 transient_failure retry/backoff | yes | cli | `cmd/bd/flow.go:1114`, `cmd/bd/flow.go:1157`, `cmd/bd/flow.go:1258` |  | implemented | Attempt/backoff/escalation deterministic. |
| Transition 8 priority_preempt | no | split | `docs/agents/planner.md:1`, `docs/agents/reviewer.md:1` |  | implemented | Preemption decision remains judgment-owned. |
| Transition 9 ambiguity | no | split | `docs/agents/planner.md:1` |  | implemented | Clarification decision remains judgment-owned. |
| Transition 10 decomposition_invalid (+damper) | yes | cli | `cmd/bd/flow.go:1272`, `cmd/bd/flow.go:1324`, `cmd/bd/flow.go:1342` |  | implemented | Attempt tracking + threshold escalation implemented. |
| Transition 11 supersede_coarse_tasks | yes | cli | `cmd/bd/flow.go:720`, `cmd/bd/flow.go:861` |  | implemented | Deterministic supersession protocol command. |
| Transition 12 execution_rollback | yes | cli | `cmd/bd/flow.go:918`, `cmd/bd/flow.go:985` |  | implemented | Deterministic corrective task creation and linking. |
| Transition 13 session_abort | yes | cli | `cmd/bd/flow.go:1513`, `cmd/bd/flow.go:1605` |  | implemented | ABORT handoff + optional issue mutation. |
| SP1 WIP policy | yes | cli | `cmd/bd/flow.go:198`, `cmd/bd/flow.go:215` |  | implemented | WIP=1 gate in claim-next. |
| SP2 parallel rule | no | split | `docs/agents/planner.md:1` |  | implemented | Parallelization constraints are judgment policy. |
| SP3 anchors/pinned stickiness | yes | cli | `cmd/bd/flow.go:129`, `cmd/bd/flow.go:176`, `cmd/bd/resume.go:81` |  | implemented | Anchor cardinality + digest surfaced. |
| SP4 defer policy | no | split | `docs/agents/executor.md:1` |  | implemented | Defer strategy is policy; CLI supports defer primitives. |
| SP5 external blocker caveat | no | split | `docs/agents/reviewer.md:1` |  | implemented | Manual reliability caveat, not machine-lintable. |
| SP6 context-pack format | no | split | `docs/agents/executor.md:1`, `claude-plugin/agents/beads-cleanup-agent.md:1` |  | implemented | Format guidance remains docs-owned. |
| SP7 parent-close check | yes | cli | `cmd/bd/flow.go:2281`, `cmd/bd/flow.go:3174` |  | implemented | Enforced via close-safe parent cascade checks. |
| SP8 context freshness thresholds | yes | cli | `cmd/bd/resume.go:117`, `cmd/bd/resume.go:174` |  | implemented | Deterministic signal emission from counters. |
| SP9 living state digest | yes | cli | `cmd/bd/flow.go:2472`, `cmd/bd/flow.go:3307` |  | implemented | Updated on close-safe path. |
| SP10 overlap protocol | no | split | `docs/agents/reviewer.md:1` |  | implemented | Advisory coordination remains docs-owned. |
| SP11 commit granularity | no | split | `docs/agents/reviewer.md:1` |  | implemented | Advisory workflow policy, not CLI-enforced. |
| Hardening invariant config keys | yes | cli | `cmd/bd/preflight.go:339`, `cmd/bd/preflight.go:354`, `cmd/bd/preflight.go:361` |  | implemented | Auto-remediates `validation.on-create` + `create.require-description`. |
| Doctor fail/error and critical warnings as blockers | yes | cli | `cmd/bd/preflight.go:51`, `cmd/bd/preflight.go:370`, `cmd/bd/land.go:178` |  | implemented | Preflight + landing promotion path implemented. |
| Pre-write/pre-claim WIP gate | yes | cli | `cmd/bd/preflight.go:157`, `cmd/bd/preflight.go:398` |  | implemented | Deterministic blocker when actor holds WIP. |
| Runtime binary capability parity check | yes | cli | `cmd/bd/preflight.go:251`, `cmd/bd/preflight.go:295` |  | implemented | Manifest-based capability probe command present. |
| Unknown help subcommand capability-probe guard | yes | cli | `cmd/bd/capability_probe_guard.go:10`, `cmd/bd/main.go:739` |  | implemented | Fails bad probes deterministically. |
| Intake canonical block extraction | yes | cli | `cmd/bd/intake.go:724` |  | implemented | Exactly one `INTAKE-MAP` block required. |
| Intake PLAN/FINDING cardinality and shape checks | yes | cli | `cmd/bd/intake.go:758`, `cmd/bd/intake.go:831` |  | implemented | Contiguity/uniqueness/cardinality checks enforced. |
| Intake child count and membership checks | yes | cli | `cmd/bd/intake.go:172`, `cmd/bd/intake.go:183` |  | implemented | Parent-child reconciliation enforced. |
| Intake child lint (`description`, `acceptance`, `## Verify`) | yes | cli | `cmd/bd/intake.go:197`, `cmd/bd/intake.go:475` |  | implemented | Deterministic lint in audit + planning-exit. |
| Intake ready-set exact equality | yes | cli | `cmd/bd/intake.go:237`, `cmd/bd/intake.go:260` |  | implemented | Expected vs actual ready wave equality enforced. |
| Intake proof write (`INTAKE_AUDIT=PASS`) | yes | cli | `cmd/bd/intake.go:286`, `cmd/bd/intake.go:1113` |  | implemented | Proof block emitted with `--write-proof`. |
| Intake map rewrite helper | yes | cli | `cmd/bd/intake.go:587`, `cmd/bd/intake.go:899` |  | implemented | Canonical map rewrite command exists. |
| Intake bulk-write guard | yes | cli | `cmd/bd/intake.go:321`, `cmd/bd/intake.go:1034` |  | implemented | Deterministic cycle/ready guard command exists. |
| Planning exit deterministic audit | yes | cli | `cmd/bd/intake.go:410`, `cmd/bd/intake.go:546` |  | implemented | Structure/readiness checks implemented. |
| Atomicity lint (`<=180` or split marker, one verify path) | yes | cli | `cmd/bd/flow.go:2551`, `cmd/bd/flow.go:2566`, `cmd/bd/flow.go:2577` |  | implemented | Deterministic violations emitted by preclaim-lint. |
| Close reason safety (success verbs + failure keywords) | yes | cli | `cmd/bd/control_plane_helpers.go:17`, `cmd/bd/control_plane_helpers.go:101`, `cmd/bd/close.go:86` |  | implemented | Enforced in `close` and `flow close-safe`. |
| Failure reason allow path (`failed:` + allow flag) | yes | cli | `cmd/bd/control_plane_helpers.go:108`, `cmd/bd/close.go:49`, `cmd/bd/flow.go:3410` |  | implemented | Explicit allow flag required. |
| Standalone reason lint primitive | yes | cli | `cmd/bd/reason.go:22` |  | implemented | `bd reason lint` present. |
| Landing Gate 1 issue hygiene | yes | cli | `cmd/bd/land.go:77`, `cmd/bd/land.go:105`, `cmd/bd/land.go:133` |  | implemented | Checks in-progress, hooked, open-under-epic. |
| Landing Gate 2 code quality evidence | no | split | `cmd/bd/land.go:315`, `docs/agents/reviewer.md:1` |  | implemented | CLI supports deterministic enforcement when requested; trigger policy is judgment-owned. |
| Landing Gate 3 sync/push choreography | yes | cli | `cmd/bd/land.go:365`, `cmd/bd/land.go:388`, `cmd/bd/land.go:418` |  | implemented | Pull/rebase, sync status/merge/sync, push path encoded. |
| Landing Gate 4 handoff fields | no | split | `cmd/bd/land.go:325`, `claude-plugin/agents/beads-cleanup-agent.md:1` |  | implemented | CLI can require fields; handoff content quality remains judgment-owned. |
| Split-agent boundary (deterministic in CLI, judgment in docs) | no | split | `docs/agents/README.md:1`, `docs/PLUGIN.md:108`, `claude-plugin/agents/task-agent.md:1` |  | implemented | Ownership boundary documented across repo/plugin docs. |
| Runtime parity against source command surface | yes | cli | `/tmp/beads-bd-new2 preflight runtime-parity --binary /tmp/beads-bd-new2 --json` |  | implemented | Capability parity check passed in this pass. |

## Gap Summary
- deterministic missing (`owner=task` + `status=missing`): **0**
- deterministic tracked-as-task (`owner=task` + non-implemented): **0**
- deterministic implemented in CLI/source (`owner=cli` + implemented): **62**
- judgment/split-owned (`owner=split`): **14**

## Notes
- `bd-bmf` and `bd-c0q` are closed and contain the migration/remediation history for previously missing deterministic controls.
- `bd intake audit --epic bd-bmf --write-proof --json` now passes in `closed_epic` mode (ready-set check marked `N/A`), so closed-epic audits no longer produce false contract violations.
- `bd preflight gate --action claim --json` now returns deterministically in this environment (verified runtime parity and bounded diagnostics path).

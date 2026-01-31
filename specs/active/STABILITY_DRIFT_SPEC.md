# Stability Drift Detection: Volatility-Aware Issue Management

> **Status**: READY FOR ENGINEERING | Priority: P1 | Last Updated: 2026-01-31
> **Repo**: github.com/anupamchugh/shadowbook (local fork)
> **Epic**: Complete the "Four Drifts" ecosystem
> **Impact**: Prevents cascading rework from unstable specs

---

## Problem Statement

### The Missing Fourth Drift

Shadowbook tracks three drifts but misses the most dangerous one:

| Drift | Signal | Command | Status |
|-------|--------|---------|--------|
| Spec Drift | Hash changed | `bd spec scan` | ✓ Shipped |
| Skill Drift | Agent mismatch | `bd preflight` | ✓ Shipped |
| Visibility Drift | What's active | `bd recent` | Phase 2 |
| **Stability Drift** | **Specs churning** | **`bd spec volatility`** | ◐ **HIDDEN** |

### Why This Matters

**Volatile specs cause cascading failures:**

```
Timeline:
- Day 1: Create issue bd-42 linked to specs/auth.md
- Day 2: Team updates specs/auth.md (major refactor)
- Day 3: bd-42 still in-progress, building against old spec
- Day 5: bd-42 stuck in code review (doesn't match current spec)
- Day 6: Realize bd-42 needs complete rework

Cost: 5 days of wasted work
```

**Current state:** Feature exists as `bd spec volatility` and:
- Is documented in README and the "Four Drifts" narrative
- Renamed from `bd spec risk` for clearer positioning
- Still lacks integration with other commands
- Has no warnings or automation in workflow yet

### What Volatility Signals

- **High volatility + open issues** = cascading rework incoming
- **High volatility + blocked dependents** = team coordination failure
- **Zero volatility + no issues** = safe to archive (inverse signal)
- **Decreasing volatility** = spec stabilizing, safe to resume work

---

## Solution: Stability Drift Detection

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│           Stability Drift Detection System                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  DATA COLLECTION LAYER                                           │
│  ├─ spec_scan_events: Track every spec change with timestamp    │
│  ├─ beads table: Count open issues per spec                     │
│  ├─ dependency graph: Find blocked/downstream issues            │
│  └─ git log: Correlate with code churn (optional)               │
│                                                                   │
│  VOLATILITY SCORING LAYER                                        │
│  ├─ Change frequency: changes in last N days                    │
│  ├─ Open issue count: active work on this spec                  │
│  ├─ Dependent count: issues blocked by spec-linked issues       │
│  └─ Trend direction: increasing, stable, decreasing             │
│                                                                   │
│  CLASSIFICATION LAYER                                            │
│  ├─ ● HIGH: 5+ changes/30d OR 3+ changes + 3+ open issues     │
│  ├─ ◐  MEDIUM: 2-4 changes/30d with open issues                │
│  ├─ ○ LOW: 1 change/30d OR only open issues, no changes        │
│  └─ ✓ STABLE: 0 changes/30d AND 0 open issues                  │
│                                                                   │
│  INTEGRATION LAYER                                               │
│  ├─ bd spec volatility: Standalone command                      │
│  ├─ bd ready: Warn about volatile spec-linked issues            │
│  ├─ bd create --spec: Warn if spec is volatile                  │
│  ├─ bd preflight: Show top volatile specs                       │
│  ├─ bd recent --all: Color-code by volatility                   │
│  └─ bd list: Add volatility badge                               │
│                                                                   │
│  ACTION LAYER                                                    │
│  ├─ Recommendations: stabilize, split, archive                  │
│  ├─ Auto-pause: Move beads to blocked when spec volatile        │
│  ├─ Cascade analysis: Show downstream impact                    │
│  └─ Trend prediction: Estimate stabilization date               │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Specifications

### Status Update (2026-01-31)

Completed:
- Phase 1: rename/expose volatility command and docs
- Phase 2.1: create-time volatility warning
- Phase 2.2: list badges + flags + watch mode
- Phase 2.3: ready partitioning
- Phase 3.1: preflight volatility check
- Phase 3.2: volatility CI gates
- Phase 5.1: recent activity volatility badges
- Phase 4.1: `--with-dependents` cascade analysis
- Phase 4.2: `--recommendations` guidance
- Phase 6.1: trend analysis (`--trend`)
- Phase 6.2: auto-pause + resume flow
- Phase 6.3: skill propagation in `bd skills audit`

Next:
- None. Phase 6 delivered.

### Phase 1: Expose & Rename (2-3 hours)

**Rename command and add to README:**

| Task | Description | File |
|------|-------------|------|
| Rename command | `bd spec risk` → `bd spec volatility` | `cmd/bd/spec_volatility.go` |
| Update help text | Clear use case and examples | `cmd/bd/spec_volatility.go` |
| Add to README | "Four Drifts" section | `README.md` |
| Add docs | Use cases and examples | `docs/SHADOWBOOK_MANUAL.md` |

**Output format (current, improved):**

```bash
$ bd spec volatility

SPEC                      VOLATILITY  CHANGES  LAST CHANGE   OPEN  STATUS
specs/auth.md             ● HIGH     7        2h ago        3     ◐ Stabilize first
specs/api.md              ◐  MEDIUM   3        3d ago        2     Review before work
specs/ui.md               ○ LOW      1        14d ago       1     Likely stable
specs/old-feature.md      ✓ STABLE   0        45d ago       0     Archive candidate

Summary: 4 specs tracked, 2 volatile, 1 archive candidate
```

**Acceptance criteria:**
- [ ] Command renamed to `bd spec volatility`
- [ ] Help text includes clear use case
- [ ] README updated with "Four Drifts" table
- [ ] Output shows volatility classification with emoji indicators

---

### Phase 2: Bead Integration (4-6 hours)

**2.1: Warning on `bd create --spec`**

```bash
$ bd create "Implement OAuth" --spec specs/auth.md

◐  WARNING: specs/auth.md is volatile
    • 5 changes in last 7 days
    • 3 open issues already linked

    Recommendation: Stabilize spec before starting new work.

Create anyway? [y/N] y

Created bd-xyz "Implement OAuth" (linked to volatile spec)
```

**Implementation:**
- Query volatility before creating bead
- Show warning if HIGH or MEDIUM volatility
- Require confirmation (or `--force` flag to skip)

| Task | Description | File | Status |
|------|-------------|------|--------|
| Add volatility check | Query spec volatility on create | `cmd/bd/create.go` | ✅ Done |
| Add warning prompt | Interactive confirmation | `cmd/bd/create.go` | ✅ Done |
| Add `--force` flag | Skip warning | `cmd/bd/create.go` | ✅ Done |

---

**2.2: Volatility badge in `bd list`** ✅ COMPLETE

```bash
$ bd list --spec specs/auth.md

  bd-42  ● Implement login      in_progress  specs/auth.md (volatile)
  bd-43  ● Add 2FA              pending      specs/auth.md (volatile)
  bd-44  ○ Update README        pending      specs/docs.md (stable)
```

**Implementation:**
- Join with volatility data on list
- Add `--show-volatility` flag (default: on when filtering by spec)

| Task | Description | File | Status |
|------|-------------|------|--------|
| Add volatility join | Include volatility in list query | `cmd/bd/list.go` | ✅ Done |
| Add badge rendering | Show emoji + "(volatile)" suffix | `cmd/bd/list.go` | ✅ Done |
| Add flag | `--show-volatility` / `--hide-volatility` | `cmd/bd/list.go` | ✅ Done |
| Thread through outputs | Pretty/compact/long all show badges | `cmd/bd/list.go` | ✅ Done |
| Update watch mode | Watch mode includes volatility | `cmd/bd/list.go` | ✅ Done |
| Update docs | README and manual updated | `README.md`, `docs/SHADOWBOOK_MANUAL.md` | ✅ Done |

---

**2.3: Smart `bd ready` with volatility awareness**

```bash
$ bd ready

READY TO WORK (stable specs):
  bd-12  Update docs              specs/docs.md (stable)
  bd-15  Fix typo                 (no spec)

◐ CAUTION (volatile specs):
  bd-42  Implement OAuth          specs/auth.md ● (5 changes/week)
  bd-43  Add 2FA                  specs/auth.md ●

Recommendation: Stabilize specs/auth.md before continuing 2 issues

Total: 4 ready, 2 on volatile specs
```

**Implementation:**
- Partition ready issues by spec volatility
- Show stable first, then volatile with warning
- Add recommendation when volatile count > 0

| Task | Description | File | Status |
|------|-------------|------|--------|
| Partition by volatility | Separate stable vs volatile | `cmd/bd/ready.go` | ✅ Done |
| Add caution section | Render volatile issues separately | `cmd/bd/ready.go` | ✅ Done |
| Add recommendation | Suggest stabilization | `cmd/bd/ready.go` | ✅ Done |

---

### Phase 3: Preflight Integration (3-4 hours)

**3.1: Volatility in `bd preflight --check`**

```bash
$ bd preflight --check

✓ Skills: 47/47 synced
✓ Specs: 12 tracked, 0 unacknowledged changes
◐ Volatility: 2 specs have high churn
  • specs/auth.md     (5 changes, 3 open issues)
  • specs/api.md      (3 changes, 2 open issues)

Run `bd spec volatility --recommendations` for guidance

Overall: PASS with warnings
```

**Implementation:**
- Add volatility check to preflight sequence
- Show top 3-5 volatile specs
- Warning status (doesn't fail preflight, just warns)

| Task | Description | File | Status |
|------|-------------|------|--------|
| Add volatility check | Query volatile specs | `cmd/bd/preflight.go` | ✅ Done |
| Add warning output | Show in preflight results | `cmd/bd/preflight.go` | ✅ Done |
| Add JSON support | Include in `--json` output | `cmd/bd/preflight.go` | ✅ Done |

---

**3.2: CI-friendly volatility gate**

```bash
$ bd spec volatility --fail-on-high

◐ HIGH volatility specs detected:
  • specs/auth.md (7 changes in 30 days)

Exit code: 1 (would block PR)
```

**Use case:** Block PRs that modify volatile specs without acknowledgment.

| Task | Description | File | Status |
|------|-------------|------|--------|
| Add `--fail-on-high` | Exit code 1 if high volatility | `cmd/bd/spec_volatility.go` | ✅ Done |
| Add `--fail-on-medium` | Exit code 1 if medium+ | `cmd/bd/spec_volatility.go` | ✅ Done |

---

### Phase 4: Cascade Analysis (6-8 hours)

**4.1: `--with-dependents` flag**

```bash
$ bd spec volatility --with-dependents specs/auth.md

specs/auth.md (● HIGH volatility: 5 changes, 3 open)
│
├── bd-42: Implement login (in_progress) ← DRIFTED
│   ├── bd-43: Add 2FA (blocked by bd-42)
│   │   └── bd-45: SSO integration (blocked by bd-43)
│   └── bd-46: Session management (blocked by bd-42)
│
└── bd-44: RBAC redesign (pending)
    └── bd-47: Admin dashboard (blocked by bd-44)

IMPACT SUMMARY:
  • 2 issues directly affected
  • 4 issues blocked downstream
  • Total cascade: 6 issues at risk

RECOMMENDATION: Lock down spec, complete bd-42 first, then unblock cascade
```

**Implementation:**
- Walk dependency graph from spec-linked issues
- Count direct + transitive dependents
- Generate recommendation based on cascade size

| Task | Description | File |
|------|-------------|------|
| Add dependency walker | Traverse blocks/blockedBy graph | `internal/cascade/walker.go` |
| Add tree renderer | ASCII tree output | `internal/cascade/renderer.go` |
| Add impact calculator | Count affected issues | `internal/cascade/impact.go` |
| Add recommendations | Generate action items | `internal/cascade/recommendations.go` |

---

**4.2: `--recommendations` flag**

```bash
$ bd spec volatility --recommendations

RECOMMENDATIONS BY SPEC:

specs/auth.md (● HIGH)
  Action: STABILIZE
  Reason: 5 changes + 3 open issues + 4 blocked downstream
  Steps:
    1. Freeze spec changes (notify team)
    2. Complete bd-42 (in-progress, most blocking)
    3. Review spec with stakeholders
    4. Resume dependent work after spec locked

specs/old-feature.md (✓ STABLE)
  Action: ARCHIVE
  Reason: 0 changes + 0 open issues for 45 days
  Steps:
    1. Run: bd spec compact specs/old-feature.md
```

| Task | Description | File |
|------|-------------|------|
| Add recommendation engine | Generate actions by volatility | `internal/volatility/recommendations.go` |
| Add step generator | Concrete action items | `internal/volatility/recommendations.go` |

---

### Phase 5: Activity Dashboard Integration (4-6 hours)

**5.1: Volatility in `bd recent --all`**

```bash
$ bd recent --all

ACTIVITY (last 7 days):

SPECS
  specs/auth.md      ● VOLATILE  5 changes  3 open issues
  specs/api.md       ◐  MEDIUM    2 changes  1 open issue
  specs/docs.md      ✓ STABLE    0 changes  0 open issues

BEADS
  bd-42 → specs/auth.md    ◐ building on volatile spec
  bd-12 → specs/docs.md    ✓ stable foundation
  bd-15 → (no spec)        ✓ independent

SKILLS
  auth-validator     ◐ linked to volatile spec
  data-processor     ✓ stable

Summary: 3 specs (1 volatile), 3 beads (1 at risk), 2 skills
```

**Implementation:**
- Add volatility column to spec section
- Add warning indicator to beads linked to volatile specs
- Propagate volatility to linked skills

| Task | Description | File |
|------|-------------|------|
| Add volatility to recent | Include in spec output | `cmd/bd/recent.go` |
| Add bead risk indicator | Flag beads on volatile specs | `cmd/bd/recent.go` |
| Add skill propagation | Mark skills linked to volatile specs | `cmd/bd/recent.go` |

---

### Phase 6: Advanced Features (8-12 hours)

**6.1: Trend Analysis**

```bash
$ bd spec volatility --trend specs/auth.md

VOLATILITY TREND (specs/auth.md):

  Week 1: ████████░░  8 changes  (peak)
  Week 2: █████░░░░░  5 changes  (decreasing)
  Week 3: ██░░░░░░░░  2 changes  (stabilizing)
  Week 4: ░░░░░░░░░░  0 changes  (stable)

Status: STABILIZING
Prediction: Safe to resume work in ~5 days
Confidence: 72% (based on trend direction)
```

| Task | Description | File |
|------|-------------|------|
| Add trend query | Group changes by week | `internal/volatility/trend.go` |
| Add bar renderer | ASCII sparkline | `internal/volatility/trend.go` |
| Add prediction | Simple linear regression | `internal/volatility/predict.go` |

---

**6.2: Auto-Pause on Volatility**

```bash
$ bd config set auto-pause-on-volatility true

# Later, when spec becomes volatile:
$ bd spec scan

● SPEC CHANGED: specs/auth.md (now HIGH volatility)
  ↳ Auto-pausing 3 linked issues:
    • bd-42: Implement login → status: blocked (was: in_progress)
    • bd-43: Add 2FA → status: blocked (was: pending)
    • bd-44: RBAC → status: blocked (was: pending)

Reason: Spec volatility threshold exceeded
Action: Stabilize spec, then run `bd resume --spec specs/auth.md`
```

```bash
$ bd resume --spec specs/auth.md

Resuming 3 issues linked to specs/auth.md:
  • bd-42: blocked → in_progress
  • bd-43: blocked → pending
  • bd-44: blocked → pending

Done. Run `bd ready` to see available work.
```

| Task | Description | File |
|------|-------------|------|
| Add config option | `auto-pause-on-volatility` | `internal/config/config.go` |
| Add auto-pause logic | Pause beads when spec volatile | `cmd/bd/spec_scan.go` |
| Add `bd resume --spec` | Unblock paused beads | `cmd/bd/resume.go` |

---

**6.3: Skill Volatility Propagation**

```bash
$ bd skills audit

SKILLS HEALTH:

  auth-validator      ◐ HIGH RISK
    Reason: Linked to specs/auth.md (volatile)
    Action: Don't update until spec stabilizes

  data-processor      ✓ STABLE
    Reason: Linked to specs/data.md (stable)

  ui-helpers          ✓ STABLE
    Reason: No linked spec

Summary: 3 skills, 1 at risk
```

| Task | Description | File |
|------|-------------|------|
| Add skill-spec links | Track which skills implement specs | `internal/skills/links.go` |
| Propagate volatility | Inherit spec volatility to skills | `internal/skills/audit.go` |
| Add to audit output | Show risk in skills audit | `cmd/bd/skills.go` |

---

## Data Model

### Existing Tables (Already in Place)

```sql
-- specs_manifest (tracks spec changes)
CREATE TABLE specs_manifest (
    spec_id TEXT PRIMARY KEY,
    path TEXT UNIQUE,
    sha256 TEXT,
    discovered_at DATETIME,
    last_scanned_at DATETIME
);

-- spec_scan_events (records each scan)
CREATE TABLE spec_scan_events (
    id INTEGER PRIMARY KEY,
    spec_id TEXT,
    scanned_at DATETIME,
    sha256 TEXT,
    changed BOOLEAN
);

-- beads (linked issues)
CREATE TABLE beads (
    id TEXT PRIMARY KEY,
    spec_id TEXT,
    status TEXT,
    created_at DATETIME
);
```

### Core Volatility Query

```sql
SELECT
    s.spec_id,
    s.path,
    COUNT(CASE WHEN e.changed = 1 THEN 1 END) as change_count,
    MAX(CASE WHEN e.changed = 1 THEN e.scanned_at END) as last_changed_at,
    COUNT(DISTINCT b.id) as open_issues,
    CASE
        WHEN COUNT(CASE WHEN e.changed = 1 THEN 1 END) >= 5
             OR (COUNT(CASE WHEN e.changed = 1 THEN 1 END) >= 3
                 AND COUNT(DISTINCT b.id) >= 3) THEN 'HIGH'
        WHEN COUNT(CASE WHEN e.changed = 1 THEN 1 END) >= 2
             AND COUNT(DISTINCT b.id) > 0 THEN 'MEDIUM'
        WHEN COUNT(CASE WHEN e.changed = 1 THEN 1 END) >= 1
             OR COUNT(DISTINCT b.id) > 0 THEN 'LOW'
        ELSE 'STABLE'
    END as volatility
FROM specs_manifest s
LEFT JOIN spec_scan_events e ON s.spec_id = e.spec_id
    AND e.scanned_at > datetime('now', '-30 days')
LEFT JOIN beads b ON s.spec_id = b.spec_id
    AND b.status NOT IN ('closed', 'tombstone')
GROUP BY s.spec_id
ORDER BY change_count DESC, open_issues DESC;
```

### New Table: Volatility History (Phase 6)

```sql
CREATE TABLE volatility_snapshots (
    id INTEGER PRIMARY KEY,
    spec_id TEXT NOT NULL,
    snapshot_date DATE NOT NULL,
    change_count INTEGER,
    open_issues INTEGER,
    volatility_level TEXT,  -- 'HIGH', 'MEDIUM', 'LOW', 'STABLE'
    UNIQUE(spec_id, snapshot_date)
);

-- Populated daily by daemon or on-demand
```

---

## CLI Reference

### New Commands

| Command | Description | Phase |
|---------|-------------|-------|
| `bd spec volatility` | Show all specs by volatility | 1 |
| `bd spec volatility <path>` | Show volatility for specific spec | 1 |
| `bd spec volatility --since 7d` | Change window (default: 30d) | 1 |
| `bd spec volatility --fail-on-high` | CI gate (exit 1 if high) | 3 |
| `bd spec volatility --with-dependents` | Show cascade impact | 4 |
| `bd spec volatility --recommendations` | Action items per spec | 4 |
| `bd spec volatility --trend <path>` | Show volatility over time | 6 |
| `bd resume --spec <path>` | Unblock auto-paused beads | 6 |

### Modified Commands

| Command | Modification | Phase |
|---------|--------------|-------|
| `bd create --spec <path>` | Warn if spec volatile | 2 |
| `bd list` | Add volatility badge | 2 |
| `bd ready` | Partition by spec volatility | 2 |
| `bd preflight --check` | Include volatility warnings | 3 |
| `bd recent --all` | Color-code by volatility | 5 |
| `bd skills audit` | Show risk from volatile specs | 6 |

### New Config Options

| Option | Default | Description | Phase |
|--------|---------|-------------|-------|
| `volatility.window` | `30d` | Time window for change counting | 1 |
| `volatility.high_threshold` | `5` | Changes to trigger HIGH | 1 |
| `volatility.medium_threshold` | `2` | Changes to trigger MEDIUM | 1 |
| `volatility.auto_pause` | `false` | Auto-pause beads on volatile specs | 6 |
| `volatility.show_in_list` | `true` | Show badges in bd list | 2 |

---

## Implementation Roadmap

### Summary

| Phase | Description | Effort | Dependencies |
|-------|-------------|--------|--------------|
| 1 | Expose & Rename | 2-3 hrs | None |
| 2 | Bead Integration | 4-6 hrs | Phase 1 |
| 3 | Preflight Integration | 3-4 hrs | Phase 1 |
| 4 | Cascade Analysis | 6-8 hrs | Phase 2 |
| 5 | Dashboard Integration | 4-6 hrs | Phase 2, `bd recent` done |
| 6 | Advanced Features | 8-12 hrs | All above |

**Total: 27-39 hours (~4-5 days)**

### Recommended Order

```
Phase 1 (Day 1)
    ↓
Phase 2 + Phase 3 (Days 2-3, parallel)
    ↓
Phase 4 (Day 4)
    ↓
Phase 5 (Day 5, after bd recent ships)
    ↓
Phase 6 (Done)
```

---

## Success Metrics

### Before Implementation

- `bd spec volatility` exists but 0% discoverability
- Users don't know volatility feature exists
- No integration with bead workflow
- Cascading failures go undetected

### After Implementation

| Metric | Target |
|--------|--------|
| Volatility command in README | ✓ |
| Four Drifts narrative complete | ✓ |
| Warnings on volatile spec work | 100% coverage |
| Preflight includes volatility | ✓ |
| Cascade analysis available | ✓ |
| Zero hidden features | ✓ |

### User Stories Enabled

1. *"As a dev, I want to know if I'm building on quicksand before I start."*
2. *"As a lead, I want to see which specs need stabilization before sprint planning."*
3. *"As a reviewer, I want PRs blocked if they touch volatile specs without acknowledgment."*
4. *"As anyone, I want the activity dashboard to show what's risky, not just what's recent."*

---

## Open Questions

1. **Thresholds**: Are 5/3/2 the right defaults for HIGH/MEDIUM/LOW?
2. **Auto-pause**: Should this be opt-in or opt-out?
3. **CI integration**: Should `--fail-on-high` be the default for CI?
4. **Skill propagation**: How to handle skills linked to multiple specs with different volatility?
5. **Notifications**: Should volatile specs trigger Slack/webhook alerts?

---

## Related Specs

- [Auto-Compaction Spec](../archive/SHADOWBOOK_AUTO_COMPACT_SPEC.md) - Uses inverse volatility signal
- [Activity Dashboard](../reference/SHADOWBOOK_FEATURE_OPPORTUNITIES.md#7-recent-activity-dashboard) - Phase 5 dependency
- [Skills Manifest](../local/skills-manifest-spec.md) - Phase 6 skill-spec linking

---

## Appendix: Volatility Classification Logic

```go
func ClassifyVolatility(changes, openIssues int) VolatilityLevel {
    // HIGH: Lots of changes OR changes + many open issues
    if changes >= 5 || (changes >= 3 && openIssues >= 3) {
        return HIGH
    }
    // MEDIUM: Some changes with open work
    if changes >= 2 && openIssues > 0 {
        return MEDIUM
    }
    // LOW: Minor activity
    if changes >= 1 || openIssues > 0 {
        return LOW
    }
    // STABLE: No activity
    return STABLE
}
```

```go
func GetRecommendation(level VolatilityLevel, openIssues, dependents int) string {
    switch level {
    case HIGH:
        return "STABILIZE: Freeze spec, complete in-flight work, then resume"
    case MEDIUM:
        return "REVIEW: Check spec before starting new work"
    case LOW:
        return "MONITOR: Likely stable, proceed with caution"
    case STABLE:
        if openIssues == 0 {
            return "ARCHIVE: Safe to compact"
        }
        return "CONTINUE: Stable foundation"
    }
}
```

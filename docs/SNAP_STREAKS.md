# Snap Streaks: Track Spec Stability Over Time

Specs change. Sometimes too much.

A spec that changes five times in a week while three engineers build against it creates chaos. Work drifts. Reviews fail. Rework multiplies.

Shadowbook now tracks these changes visually.

## The Problem

You create an issue linked to `specs/auth.md`. You start building. The spec changes at 3am. You don't notice. Your code review fails because your implementation doesn't match the current spec.

This happens constantly. Specs change. Teams don't communicate. Work compounds on unstable foundations.

## The Solution

```bash
$ bd spec volatility --trend specs/auth.md

VOLATILITY TREND (specs/auth.md):

  Week 1: ████████░░  8 changes
  Week 2: █████░░░░░  5 changes
  Week 3: ██░░░░░░░░  2 changes
  Week 4: ░░░░░░░░░░  0 changes

Status: DECREASING
Prediction: Safe to resume work in ~5 days
```

Four weeks. One bar per week. Filled blocks show changes.

A declining streak means stability is returning. Wait a few days, then resume work.

A flat streak at zero means the spec is locked down. Build confidently.

An increasing streak means chaos is growing. Stop. Stabilize the spec first.

## Reading Streaks

**Declining streak**: The spec is settling. Changes are slowing. Stability approaches.

```
Week 1: ████████░░  8
Week 2: █████░░░░░  5
Week 3: ██░░░░░░░░  2
Week 4: ░░░░░░░░░░  0
```

**Flat streak at zero**: Stable. No changes in weeks. Safe to build.

```
Week 1: ░░░░░░░░░░  0
Week 2: ░░░░░░░░░░  0
Week 3: ░░░░░░░░░░  0
Week 4: ░░░░░░░░░░  0
```

**Increasing streak**: Danger. The spec is in flux. Don't start new work.

```
Week 1: ░░░░░░░░░░  0
Week 2: ██░░░░░░░░  2
Week 3: █████░░░░░  5
Week 4: ████████░░  8
```

**Erratic streak**: Unstable requirements. The team hasn't agreed on direction.

```
Week 1: ██░░░░░░░░  2
Week 2: ████████░░  8
Week 3: █░░░░░░░░░  1
Week 4: ██████░░░░  6
```

## Volatility Badges

Every bead shows its spec's stability at a glance:

```bash
$ bd list --spec specs/auth.md

  bd-42  [🔥 volatile] Implement login    in_progress
  bd-43  [🔥 volatile] Add 2FA           pending
  bd-44  [⚡ stable]    Update README     pending
```

🔥 means danger. ⚡ means safe.

## Cascade Impact

One unstable spec can block many issues.

```bash
$ bd spec volatility --with-dependents specs/auth.md

specs/auth.md (🔥 HIGH volatility: 5 changes, 3 open)
├── bd-42: Implement login (in_progress) ← DRIFTED
│   └── bd-43: Add 2FA (blocked by bd-42)
└── bd-44: RBAC redesign (pending)

IMPACT SUMMARY:
  • 2 issues directly affected
  • 1 issue blocked downstream
  • Total cascade: 3 issues at risk

RECOMMENDATION: STABILIZE: lock spec and unblock dependents
```

Three issues at risk. One spec change propagates through the dependency graph.

## Recommendations

Let Shadowbook analyze all specs and suggest actions:

```bash
$ bd spec volatility --recommendations

RECOMMENDATIONS BY SPEC:

specs/auth.md (🔥 HIGH)
  Action: STABILIZE: lock spec and unblock dependents
  Reason: 5 changes, 3 open issues, 1 dependents

specs/old-feature.md (⚡ STABLE)
  Action: ARCHIVE: safe to compact
  Reason: 0 changes, 0 open issues
```

Four actions exist:

- **STABILIZE**: Freeze spec changes. Complete in-flight work. Then unblock dependents.
- **REVIEW**: Check the spec before starting new work.
- **MONITOR**: Low activity. Proceed with caution.
- **ARCHIVE**: No changes, no open issues. Safe to compact.

## Auto-Pause

Enable automatic pausing of issues when their linked spec becomes volatile:

```bash
bd config set volatility.auto_pause true
```

When a spec hits HIGH volatility, Shadowbook moves linked issues to `blocked` status. Resume them after the spec stabilizes:

```bash
bd resume --spec specs/auth.md
```

## CI Integration

Block PRs that touch volatile specs:

```bash
bd spec volatility --fail-on-high
```

Exit code 1 if any spec is HIGH volatility. Add to your CI pipeline to prevent building on quicksand.

## The Name

Snapchat streaks track daily activity. Maintain the streak or lose it.

Spec streaks track weekly stability. A declining streak is good. It means the chaos is ending.

Unlike Snapchat, you want your streak to die.

---

## Commands

| Command | Purpose |
|---------|---------|
| `bd spec volatility --trend <path>` | Show 4-week streak |
| `bd spec volatility --with-dependents <path>` | Show cascade impact |
| `bd spec volatility --recommendations` | Get action items |
| `bd spec volatility --fail-on-high` | CI gate |
| `bd resume --spec <path>` | Unblock paused issues |

## Configuration

| Option | Default | Description |
|--------|---------|-------------|
| `volatility.auto_pause` | `false` | Auto-pause on HIGH volatility |
| `volatility.window` | `30d` | Time window for change counting |
| `volatility.high_changes` | `5` | Changes to trigger HIGH |
| `volatility.high_mixed_changes` | `3` | Changes required when open issues are high |
| `volatility.high_open_issues` | `3` | Open issues required for mixed HIGH |
| `volatility.medium_changes` | `2` | Changes to trigger MEDIUM (with open issues) |
| `volatility.low_changes` | `1` | Changes to trigger LOW |
| `volatility.decay.half_life` | `""` | Decay window for weighted changes (e.g., `7d`) |

---

*Stability is a feature. Track it.*

# Shadowbook

### `bd` — see your chaos, catch the drift

```
$ bd recent --all

test-f2y [P1] Implement OAuth login 🔥 volatile  ○ open  just now
└─ ● specs/auth.md  ✓ active 🔥 volatile  just now
test-sgo [P3] Update README ⚡ stable  ○ open  just now
└─ ● specs/docs.md  ✓ active ⚡ stable  1m ago

Summary: 2 beads, 2 specs | Active: 2 pending | Momentum: 4 items today
```

One command. Beads, specs, skills—nested by relationship. Volatility flagged. Orphans called out.

[![License](https://img.shields.io/github/license/anupamchugh/shadowbook)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/anupamchugh/shadowbook)](https://goreportcard.com/report/github.com/anupamchugh/shadowbook)

Built on [beads](https://github.com/steveyegge/beads).

---

## Four Drifts, One Tool

| Drift | Problem | Solution |
|-------|---------|----------|
| **Spec Drift** | Spec changes, code builds old version | `bd spec scan` |
| **Skill Drift** | Claude has skills Codex lacks | `bd preflight --check` |
| **Visibility Drift** | Can't see what's active | `bd recent --all` |
| **Stability Drift** | Specs churning while work in flight | `bd spec volatility` |

---

## Quick Start

```bash
curl -fsSL https://raw.githubusercontent.com/anupamchugh/shadowbook/main/scripts/install.sh | bash
cd your-project && bd init && mkdir -p specs
bd recent --all
```

---

## Snap Streaks

Track spec stability over time. Like Snapchat streaks, but for specs.

```bash
$ bd spec volatility --trend specs/auth.md

  Week 1: ████████░░  8 changes
  Week 2: █████░░░░░  5 changes
  Week 3: ██░░░░░░░░  2 changes
  Week 4: ░░░░░░░░░░  0 changes

Status: DECREASING
Prediction: Safe to resume work in ~5 days
```

Declining = stabilizing. Flat at zero = locked down. Increasing = chaos growing.

**Badges everywhere:**

```bash
$ bd list --show-volatility
  bd-42  [🔥 volatile] Implement login    in_progress
  bd-44  [⚡ stable]    Update README     pending

$ bd ready
○ Ready (stable): 1. Update README
🔥 Caution (volatile): 1. Implement login (5 changes/30d, 3 open)
```

**Cascade impact:**

```bash
$ bd spec volatility --with-dependents specs/auth.md

specs/auth.md (🔥 HIGH: 5 changes, 3 open)
├── bd-42: Implement login ← DRIFTED
│   └── bd-43: Add 2FA (blocked)
└── bd-44: RBAC redesign

Impact: 3 issues at risk
Recommendation: STABILIZE
```

**CI gate:**

```bash
bd spec volatility --fail-on-high  # Exit 1 if HIGH volatility
```

**Auto-pause:**

```bash
bd config set volatility.auto_pause true
bd resume --spec specs/auth.md  # Unblock after stabilization
```

---

## Spec Drift Detection

```bash
bd create "Implement login" --spec-id specs/login.md
# ... spec changes ...
bd spec scan
● SPEC CHANGED: specs/login.md → bd-a1b2 unaware

bd list --spec-changed    # Find drifted issues
bd update bd-a1b2 --ack-spec  # Acknowledge
```

---

## Skill Sync

```bash
bd preflight --check
✓ Skills: 47/47 synced
✓ Specs: 12 tracked
🔥 Volatility: 2 specs have high churn

bd preflight --check --auto-sync  # Fix drift
```

---

## Auto-Compaction

```bash
bd spec candidates        # Score specs for archival
bd spec compact specs/old.md --summary "Done. 3 endpoints."
bd close bd-xyz --compact-spec --compact-skills
```

---

## Commands

| Command | Action |
|---------|--------|
| `bd recent --all` | Activity dashboard with volatility |
| `bd ready` | Work queue, partitioned by volatility |
| `bd list --show-volatility` | Badges: 🔥 volatile / ⚡ stable |
| `bd spec scan` | Detect spec changes |
| `bd spec volatility` | List specs by stability |
| `bd spec volatility --trend <spec>` | 4-week visual trend |
| `bd spec volatility --with-dependents <spec>` | Cascade impact |
| `bd spec volatility --recommendations` | Action items |
| `bd spec volatility --fail-on-high` | CI gate |
| `bd preflight --check` | Skills + specs + volatility |
| `bd resume --spec <path>` | Unblock paused issues |
| `bd pacman` | Pacman mode: dots (ready work), ghosts (blockers), leaderboard |
| `bd pacman --eat <id>` | Close bead and increment score |

---

## Documentation

- [Snap Streaks](docs/SNAP_STREAKS.md) — Volatility tracking guide
- [User Manual](docs/SHADOWBOOK_MANUAL.md) — Full usage
- [Architecture](docs/SHADOWBOOK_ARCHITECTURE.md) — How it works
- [AGENTS.md](AGENTS.md) — Agent workflow

---

## Why "Shadowbook"?

Every spec casts a shadow over code. When the spec moves, the shadow should move too.

---

MIT License · Built on [beads](https://github.com/steveyegge/beads)

# Shadowbook

![Pacman Score](https://img.shields.io/badge/pacman%20score-3%20dots-yellow)

### `bd` — see your chaos, catch the drift

```
$ bd pacman

╭──────────────────────────────────────────────────────────╮
│  ᗧ····○ bd-abc····○ bd-xyz····○ bd-123 ····◐            │
╰──────────────────────────────────────────────────────────╯

YOU: claude | SCORE: 3 dots | #1 codex (5 pts)
```

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

## Five Drifts, One Tool

| Drift | Problem | Solution |
|-------|---------|----------|
| **Spec Drift** | Spec changes, code builds old version | `bd spec scan` |
| **Skill Drift** | Claude has skills Codex lacks | `bd preflight --check` |
| **Visibility Drift** | Can't see what's active | `bd recent --all` |
| **Stability Drift** | Specs churning while work in flight | `bd spec volatility` |
| **Behavioral Drift** | Claude "helpfully" deviates from instructions | `bd wobble scan` |

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

## Spec Radar Flow

Treat it like a daily weather report for specs.

```bash
# Morning: see what moved
bd spec delta

# Midday: clean up ideas
bd spec triage --sort status

# Weekly: generate a briefing
bd spec report --out .beads/reports

# Cleanup day: align lifecycle with reality (confirm before apply)
bd spec sync --apply
```

Quick reads:
- `bd spec stale` shows age buckets.
- `bd spec duplicates` surfaces overlap.
- `bd spec report` combines summary, triage, staleness, duplicates, delta, and volatility.

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

## Wobble Detection

```
     You write the recipe. Claude "improves" it.

     Expected:  bd list --created-after=$(date -v-1d) --sort=created
     Actual:    bd list --status=in_progress  ← "I thought this would help"

                    ᗧ····~····~····~····
                         wobble →
```

Based on Anthropic's ["Hot Mess of AI"](https://alignment.anthropic.com/2026/hot-mess-of-ai/) paper: extended reasoning amplifies incoherence. Wobble catches it.

```bash
$ bd wobble scan --from-sessions --days 7

┌─ WOBBLE SCAN: REAL SESSION DATA ───────────────────────┐
│ Analyzed 18 skills with REAL session data             │
└────────────────────────────────────────────────────────┘

┌─ WOBBLE REPORT: my-skill (REAL DATA) ──────────────────┐
│ Invocations: 6                                         │
│ Exact Match Rate: 33%                                  │
│ Variants Found: 5                                      │
│ Wobble Score: 0.85                                     │
│                                                        │
│ VERDICT: ● UNSTABLE                                    │
└────────────────────────────────────────────────────────┘
```

**The formula** (from the paper):
```
Wobble = Variance / (Bias² + Variance)

High wobble = Claude does something different every time
High bias   = Claude consistently does the wrong thing
```

**Structural risk factors** that predict high wobble:
- No `EXECUTE NOW` section with explicit command
- Multiple options without `(default)` marker
- Content > 4000 chars (Claude overthinks)
- Missing "DO NOT IMPROVISE" constraint
- Numbered steps without clear default

**Two modes:**

```bash
# Simulated analysis (fast, no history needed)
bd wobble scan my-skill

# Real session analysis (parses actual Claude behavior)
bd wobble scan --from-sessions --days 14

# Rank all skills by risk
bd wobble scan --all --top 10

# Project health audit
bd wobble inspect . --fix
```

**Drift dashboard:**

```bash
bd drift
```

Shows last wobble scan, stable/wobbly/unstable counts, skills fixed since last scan, and spec/bead drift summary.

**Cascade impact:**

```bash
bd cascade beads
```

Lists known dependents from the wobble store (`.beads/wobble/skills.json`).

**Fixing wobbly skills:**

```markdown
## EXECUTE NOW

**Run this immediately:**
```bash
your-exact-command --with-flags
```

**Do NOT improvise.** Run the command above first.
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
| `bd ready --mine` | Work queue filtered to your assignments |
| `bd list --show-volatility` | Badges: 🔥 volatile / ⚡ stable |
| `bd spec scan` | Detect spec changes |
| `bd spec stale` | Show specs by staleness bucket |
| `bd spec triage` | Triage specs/ideas by age and git status |
| `bd spec duplicates` | Find duplicate or overlapping specs |
| `bd spec delta` | Show spec changes since last scan |
| `bd spec report` | Generate full spec radar report |
| `bd spec align` | Spec ↔ bead ↔ code alignment report |
| `bd spec sync` | Sync spec lifecycle from linked beads |
| `bd spec volatility` | List specs by stability |
| `bd spec volatility --trend <spec>` | 4-week visual trend |
| `bd spec volatility --with-dependents <spec>` | Cascade impact |
| `bd spec volatility --recommendations` | Action items |
| `bd spec volatility --fail-on-high` | CI gate |
| `bd preflight --check` | Skills + specs + volatility |
| `bd resume --spec <path>` | Unblock paused issues |
| `bd assign <id> --to <agent>` | Assign a bead to someone |
| `bd wobble scan <skill>` | Analyze skill for drift risk |
| `bd wobble scan --all` | Rank all skills by wobble risk |
| `bd wobble scan --from-sessions` | Use REAL session data |
| `bd wobble inspect .` | Project skill health audit |
| `bd drift` | Wobble + spec/bead drift summary |
| `bd cascade <skill>` | Wobble cascade impact from stored dependents |
| `bd pacman` | Pacman mode: dots (ready work), blockers, leaderboard |
| `bd pacman --pause "reason"` | Pause signal for other agents (file-based) |
| `bd pacman --resume` | Clear pause signal |
| `bd pacman --join` | Register agent in .beads/agents.json |
| `bd pacman --eat <id>` | Close task + increment score (hidden flag) |
| `bd pacman --global` | Workspace-wide view across all projects |
| `bd pacman --badge` | Generate GitHub profile badge |

---

## Pacman Mode (Multi-Agent)

Gamified task management for coordinating multiple agents. No server required.

```bash
$ bd pacman

╭──────────────────────────────────────────────────────────╮
│  ᗧ····○ bd-abc····○ bd-xyz····○ bd-123 ····◐            │
╰──────────────────────────────────────────────────────────╯

YOU: claude
SCORE: 3 dots

DOTS NEARBY:
  ○ bd-abc ● P1 "Implement login flow"
  ○ bd-xyz ● P2 "Add retry logic"

ACHIEVEMENTS:
  ✓ First Blood
  ✓ Streak 5
  ✓ Ghost Buster

Tip: `bd pacman --global` aggregates dots and scores across your workspace.

BLOCKERS:
  ● bd-456 blocked by bd-789

LEADERBOARD:
  #1 codex   5 pts
  #2 claude  3 pts
```

All tasks done? Pacman clears the maze:

```bash
╭──────────────────────────────────────────────────────────╮
│  ᗧ····················✓ CLEAR!                            │
╰──────────────────────────────────────────────────────────╯
```

### Multi-Agent Scenarios

**Two agents, same project:**
```bash
# Codex joins and works
AGENT_NAME=codex bd pacman --join
bd pacman --eat bd-123              # Close + score

# You check progress
bd pacman                           # See leaderboard
```

**Session handoff (day → night):**
```bash
# End of day
git push

# Codex overnight
git pull && AGENT_NAME=codex bd pacman --join
bd pacman --eat bd-456
git push

# Next morning
git pull && bd pacman               # See overnight work
```

**Emergency stop all agents:**
```bash
bd pacman --pause "PRODUCTION DOWN"
# Every agent's next bd command shows warning

bd pacman --resume                  # After incident
```

### Workspace-Wide View

```bash
$ bd pacman --global

╭──────────────────────────────────────────────────────────╮
│  GLOBAL PACMAN · 5 projects · 42 dots · 8 ghosts        │
╰──────────────────────────────────────────────────────────╯

YOU: claude
TOTAL SCORE: 15 dots across all projects

PROJECTS:
  18○ project-alpha              (5 pts) ◐3
  12○ project-beta               (3 pts) ◐5
  8○  api-backend                (2 pts)
  4○  mobile-app                 (5 pts)
  ✓   my-tool                    (10 pts)
```

### Files (All Git-Tracked)

```
.beads/
├── agents.json       # Who's playing
├── scoreboard.json   # Points per agent
└── pause.json        # Pause signal (when active)
```

### Why Files, Not Server?

| Aspect | Server | Files |
|--------|--------|-------|
| Agent dies | Inbox stuck | Files persist |
| 10 projects | 10 registrations | 0 registrations |
| Sync | MCP calls | Git pull/push |

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

### Wobble Drift

```bash
bd drift
bd cascade <skill>
```

Drift shows the last wobble scan summary plus spec/bead drift counts. Cascade prints the dependents recorded in `.beads/wobble/skills.json`.

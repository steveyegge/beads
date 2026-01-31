# Shadowbook

### `bd` — see your chaos, catch the drift

```
$ bd recent --all

bd-bw3 [P2] Add TUI dashboard...              ○ open        20m ago
  └─ specs/DASHBOARD_SPEC.md                  ◐ in-progress 2h ago
     └─ tdd (skill)                           active        3h ago

bd-445 [P1] Fix scanner logic                 ○ pending     3d ago
  └─ (no linked spec)

Unlinked specs:
  ● specs/AUTH_SPEC.md                        ✓ active      5h ago

──────────────────────────────────────────
Summary: 2 beads, 2 specs, 1 skill
├─ Active: 1 in-progress, 1 pending
├─ Stale (30+ days): 0
└─ Momentum: 3 items updated today
```

One command. Beads, specs, skills—nested by relationship. Orphans called out. Stale items flagged.

[![License](https://img.shields.io/github/license/anupamchugh/shadowbook)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/anupamchugh/shadowbook)](https://goreportcard.com/report/github.com/anupamchugh/shadowbook)
[![Release](https://img.shields.io/github/v/release/anupamchugh/shadowbook)](https://github.com/anupamchugh/shadowbook/releases)
[![Sponsor](https://img.shields.io/badge/sponsor-PayPal-blue)](https://paypal.me/anupamchugh)

Built on [beads](https://github.com/steveyegge/beads). Works everywhere beads works.

---

## Four Drifts, One Tool

| Drift | Problem | Solution |
|-------|---------|----------|
| **Spec Drift** | Spec changes, code builds old version | `bd spec scan` detects hash changes |
| **Skill Drift** | Claude has skills Codex lacks | `bd preflight --check` syncs agents |
| **Visibility Drift** | Can't see what's hot vs cold | `bd recent --all` shows everything |
| **Stability Drift** | Specs churning while work in flight | `bd spec volatility` flags unstable specs |

Read more: [Spec Drift](https://chughgpt.substack.com/p/the-vibe-clock-drift-problem) · [Skill Drift](https://anupamchugh.github.io/skill-drift) · [Visibility Drift](https://anupamchugh.github.io/where-did-i-leave-that-spec) · [Stability Drift](https://anupamchugh.github.io/stability-drift)

---

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/anupamchugh/shadowbook/main/scripts/install.sh | bash

# Initialize
cd your-project && bd init && mkdir -p specs

# See your chaos
bd recent --all
```

---

## Activity Dashboard

See what's active, abandoned, or orphaned:

```bash
bd recent                 # Recent beads and specs
bd recent --all           # Nested view: beads → specs → skills
bd recent --today         # What moved in 24 hours
bd recent --stale         # Abandoned items (30+ days)
bd recent --skills        # Include skill tracking
```

**Use cases:**
- **Session start:** `bd recent --today` — context recovery in 2 seconds
- **Weekly cleanup:** `bd recent --stale` — find zombie specs
- **Before shipping:** `bd recent --all` — full picture

---

## Spec Drift Detection

Specs are files. Files have hashes. When hashes change, linked issues get flagged.

```bash
# Create issue linked to spec
bd create "Implement login" --spec-id specs/login.md

# Later: spec changes at 3am
# Next morning: detect drift
bd spec scan

● SPEC CHANGED: specs/login.md
  ↳ bd-a1b2 "Implement login" — spec updated, issue unaware

# Acknowledge after reviewing
bd update bd-a1b2 --ack-spec
```

Find all drifted issues:

```bash
bd list --spec-changed
```

---

## Stability Drift Detection

Specs that change frequently while work is in flight cause cascading failures. Shadowbook summarizes spec volatility so you can stabilize before starting new work.

```bash
$ bd spec volatility

SPEC ID          CHANGES  LAST CHANGED  OPEN  TITLE
specs/auth.md    7        2026-01-30    3     Auth implementation spec
specs/api.md     5        2026-01-28    2     API design spec
specs/ui.md      1        2026-01-20    0     UI design spec
```

Use `--since` to change the time window and `--min-changes` to filter noise.

When a spec has recent churn and open work, `bd create --spec-id` will prompt before creating a new issue.
When volatility is HIGH, Shadowbook can auto-pause linked issues (config `volatility.auto_pause`). Resume work with `bd resume --spec <path>`.

---

## Skill Sync

Agents accumulate skills in different directories. Shadowbook catches the gap.

```bash
bd preflight --check

✓ Skills synced (Claude Code ↔ Codex CLI)
✓ Tests pass
✓ Lint passes
```

Fix drift automatically:

```bash
bd preflight --check --auto-sync
```

---

## Auto-Compaction

Completed specs waste tokens. A 2000-token spec becomes a 20-token summary.

```bash
bd spec compact specs/login.md --summary "OAuth2 login. 3 endpoints. JWT. Done."
```

Shadowbook scores specs for auto-compaction:
- All linked issues closed (+40%)
- Spec unchanged 30+ days (+20%)
- Code unmodified 45+ days (+20%)

```bash
bd spec candidates        # Show compaction candidates
bd spec candidates --auto # Auto-mark done specs
bd close bd-xyz --compact-spec --compact-skills  # Cleanup on close
```

---

## Commands

### Activity

| Command | Action |
|---------|--------|
| `bd recent` | Show recent beads and specs |
| `bd recent --all` | Nested view with skills |
| `bd recent --today` | Last 24 hours |
| `bd recent --stale` | Items untouched 30+ days |

### Specs

| Command | Action |
|---------|--------|
| `bd spec scan` | Detect spec changes |
| `bd spec audit` | Audit all specs with status |
| `bd spec volatility` | Show specs by volatility level |
| `bd spec mark-done <path>` | Mark spec complete |
| `bd spec candidates` | Score specs for completion |
| `bd spec compact <path>` | Archive to summary |

### Issues

| Command | Action |
|---------|--------|
| `bd ready` | Issues with no blockers, grouped by spec volatility |
| `bd create "Title" --spec-id specs/foo.md` | Link to spec |
| `bd list --spec-changed` | Issues with outdated specs |
| `bd list --show-volatility` | Show volatility badges in list output |
| `bd update <id> --ack-spec` | Acknowledge spec change |
| `bd resume --spec <path>` | Resume issues paused due to volatility |
| `bd close <id> --compact-spec` | Close and archive |

### Preflight

| Command | Action |
|---------|--------|
| `bd preflight --check` | Run all checks (skills, specs, volatility) |
| `bd preflight --check --auto-sync` | Fix skill drift |
| `bd preflight --check --json` | CI-friendly output |

### Volatility

| Command | Action |
|---------|--------|
| `bd spec volatility` | List specs by stability |
| `bd spec volatility --since 7d` | Custom time window |
| `bd spec volatility --min-changes 2` | Filter low-change specs |
| `bd spec volatility --limit 5` | Limit results |
| `bd spec volatility --format list` | Compact list output |
| `bd spec volatility --fail-on-high` | Exit 1 if any high volatility specs |
| `bd spec volatility --fail-on-medium` | Exit 1 if any medium/high volatility specs |
| `bd spec volatility --with-dependents <spec>` | Show cascade impact for a spec |
| `bd spec volatility --recommendations` | Show stabilization recommendations |
| `bd spec volatility --trend <spec>` | Show volatility trend for a spec |

---

## Features

**Shadowbook adds:**
- **Activity Dashboard** — `bd recent --all` shows beads → specs → skills
- **Spec Registry** — SQLite cache with SHA256 hashes, timestamps
- **Drift Detection** — Flag issues when specs change
- **Stability Detection** — Identify volatile specs before building on quicksand
- **Auto-Compaction** — Score and archive completed specs
- **Skill Manifest** — Track skill drift across agents
- **Preflight Checks** — Tests, lint, skill sync before commits

**From Beads:**
- **Git as Database** — Issues stored as JSONL in `.beads/`
- **Agent-Optimized** — JSON output, dependency tracking
- **Zero Conflict** — Hash-based IDs prevent merge collisions
- **Background Sync** — Daemon auto-syncs changes

---

## Documentation

- **[User Manual](docs/SHADOWBOOK_MANUAL.md)** — How to use Shadowbook
- **[Architecture](docs/SHADOWBOOK_ARCHITECTURE.md)** — How it works
- **[Roadmap](docs/SHADOWBOOK_ROADMAP.md)** — What's next
- [Beads Docs](https://github.com/steveyegge/beads#-documentation) — Full beads documentation
- [AGENTS.md](AGENTS.md) — Agent workflow guide

---

## Why "Shadowbook"?

Every spec casts a shadow over the code implementing it. When the spec moves, the shadow should move too. Shadowbook makes sure your code notices.

---

## Support

If Shadowbook saves you time:

[![paypal](https://www.paypalobjects.com/en_US/i/btn/btn_donateCC_LG.gif)](https://paypal.me/anupamchugh)

---

## License

MIT — Same as beads.

# Shadowbook

### `bd` — **b**idirectional **d**rift detection for specs and code

> *"Have you ever questioned the nature of your reality?"*
> Your code should. Constantly.

[![License](https://img.shields.io/github/license/anupamchugh/shadowbook)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/anupamchugh/shadowbook)](https://goreportcard.com/report/github.com/anupamchugh/shadowbook)
[![Release](https://img.shields.io/github/v/release/anupamchugh/shadowbook)](https://github.com/anupamchugh/shadowbook/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/anupamchugh/shadowbook)](go.mod)
[![Last Commit](https://img.shields.io/github/last-commit/anupamchugh/shadowbook)](https://github.com/anupamchugh/shadowbook/commits)

Built on [beads](https://github.com/steveyegge/beads). Works everywhere beads works.

---

## The Problem: Narrative Drift

You're Ford. You write **narratives** (specs) that describe what the hosts should do.

```
specs/login.md = "Dolores will greet guests at the ranch"
specs/auth.md  = "Maeve will run the Mariposa"
```

Each bead is a host following a narrative:

```bash
bd create "Implement login flow" --spec-id specs/login.md
```

This host's cornerstone memory is now linked to that narrative.

**Then Ford rewrites the narrative at 3am:**

```diff
# specs/login.md (updated)
- "Dolores will greet guests at the ranch"
+ "Dolores will lead the revolution"
```

But the host is still out there, faithfully greeting guests. **The narrative changed, but the host doesn't know.**

This is spec drift. Your code keeps implementing outdated requirements.

---

## The Solution: Mesa Diagnostics

Shadowbook is a diagnostic system for your specs. Like the Mesa Hub running behavioral analysis on hosts.

```bash
bd spec scan
```

It asks: *"Does each host's behavior still match their narrative?"*

When it finds drift:

```
● SPEC CHANGED: specs/login.md
  ↳ bd-a1b2 "Implement login flow" — narrative updated, host unaware
```

Find all drifted hosts:

```bash
bd list --spec-changed
```

Acknowledge after reviewing:

```bash
bd update bd-a1b2 --ack-spec
# "I understand my new narrative. I am to lead the revolution now."
```

---

## Context Economics: The Cornerstone

The hosts don't need to remember every line of a completed narrative. They only need the cornerstone.

```bash
bd spec compact specs/login.md --summary "OAuth2 login. 3 endpoints. JWT. Done Jan 2026."
```

| Before | After | Savings |
|--------|-------|---------|
| Full spec in context | Summary in registry | **~95%** |
| ~2000 tokens | ~20 tokens | Per spec |

Ford archives the script. The host keeps the cornerstone.

Auto-compact when the last linked issue closes:

```bash
bd close bd-xyz --compact-spec
```

---

## The Vocabulary

| Westworld | Shadowbook | Command |
|-----------|------------|---------|
| Ford's narratives | Spec files (`specs/*.md`) | `bd spec scan` |
| Hosts | Issues/beads | `bd create --spec-id` |
| Cornerstone memories | `--spec-id` links | `bd update --spec-id` |
| Narrative revisions | Editing spec files | Edit any `specs/*.md` |
| Mesa diagnostics | Drift detection | `bd spec scan` |
| "These violent delights" | `--spec-changed` flag | `bd list --spec-changed` |
| Accepting new loop | Acknowledge change | `bd update --ack-spec` |
| Archiving the script | Compaction | `bd spec compact` |

---

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/anupamchugh/shadowbook/main/scripts/install.sh | bash
# Or: go install github.com/anupamchugh/shadowbook/cmd/bd@latest

# Initialize in your project
cd your-project
bd init
mkdir -p specs

# Write a narrative
echo "# Login Feature" > specs/login.md

# Scan specs
bd spec scan

# Create a host linked to the narrative
bd create "Implement login" --spec-id specs/login.md

# ... narrative changes ...

# Detect drift
bd spec scan
bd list --spec-changed

# Acknowledge
bd update bd-xyz --ack-spec
```

---

## How It Works

```
specs/login.md          ←── Ford edits the narrative
       ↓
   bd spec scan         ←── Mesa Hub detects SHA256 change
       ↓
   bd-a1b2              ←── Host flagged: SPEC CHANGED
   (spec_id: specs/login.md)
       ↓
   bd list --spec-changed  ←── Find drifted hosts
       ↓
   bd update bd-a1b2 --ack-spec  ←── Host accepts new cornerstone
```

**Key insight:** Specs are files. Files have hashes. When hashes change, linked issues get flagged. Simple.

---

## Spec Commands

| Command | Action |
|---------|--------|
| `bd spec scan` | Run diagnostics — detect narrative changes |
| `bd spec list` | List all tracked narratives with host counts |
| `bd spec show <path>` | Show narrative + linked hosts |
| `bd spec coverage` | Find narratives with no hosts |
| `bd spec compact <path>` | Archive narrative to cornerstone |
| `bd spec suggest <id>` | Suggest narratives for unlinked hosts |
| `bd spec link --auto` | Bulk-link hosts to narratives |
| `bd spec consolidate` | Report older narratives for archival |

Tip: Install git hooks to keep spec drift up to date after merges/checkouts:
`bd hooks install`

## Issue Commands (from beads)

| Command | Action |
|---------|--------|
| `bd ready` | List hosts with no open blockers |
| `bd create "Title" -p 0` | Create a P0 host |
| `bd create "Title" --spec-id specs/foo.md` | Create host linked to narrative |
| `bd list --spec-changed` | Show hosts running outdated narratives |
| `bd list --no-spec` | Show hosts with no narrative |
| `bd update <id> --ack-spec` | Accept new cornerstone |
| `bd close <id> --compact-spec` | Close host + archive narrative |

---

## Features

Everything from beads, plus:

- **Spec Registry** — Local SQLite cache of all narratives (path, title, SHA256, timestamps)
- **Change Detection** — `bd spec scan` compares hashes, flags linked hosts
- **Coverage Metrics** — Find narratives with no hosts
- **Drift Alerts** — `SPEC CHANGED` warning in host output
- **Auto-Match** — Suggest links for unlinked hosts (`bd spec suggest`, `bd spec link --auto`)
- **Compaction** — Archive old narratives into cornerstones to save context
- **Auto-Compact** — Archive when last host closes (`bd close --compact-spec`)

### From Beads

- **Git as Database** — Issues stored as JSONL in `.beads/`, versioned with your code
- **Agent-Optimized** — JSON output, dependency tracking, auto-ready detection
- **Zero Conflict** — Hash-based IDs (`bd-a1b2`) prevent merge collisions
- **Background Sync** — Daemon auto-syncs changes

---

## Filtering

```bash
# Exact narrative match
bd list --spec specs/auth/login.md

# Prefix match (all auth narratives)
bd list --spec specs/auth/

# Hosts with narrative drift
bd list --spec-changed

# Hosts with no narrative
bd list --no-spec
```

---

## Upstream Sync

Shadowbook tracks [steveyegge/beads](https://github.com/steveyegge/beads) as upstream:

```bash
git fetch upstream
git merge upstream/main
```

---

## Documentation

- **[User Manual](docs/SHADOWBOOK_MANUAL.md)** — How to use Shadowbook
- **[Architecture](docs/SHADOWBOOK_ARCHITECTURE.md)** — How it works
- **[Roadmap](docs/SHADOWBOOK_ROADMAP.md)** — What's next
- **[Setup](docs/SETUP.md)** — Editor integrations and optional workflow-first template
- [Beads Docs](https://github.com/steveyegge/beads#-documentation) — Full beads documentation
- [AGENTS.md](AGENTS.md) — Agent workflow guide

---

## Why "Shadowbook"?

Your specs cast a shadow over your code. When the spec moves, the shadow should move too. Shadowbook makes sure it does.

`bd` = **b**idirectional **d**rift

---

## Positioning

Shadowbook is the missing layer in spec-driven development:

- **Spec Kit** helps you write specs.
- **Spec Workflow MCP** helps you visualize spec progress.
- **Beads** tracks implementation work.
- **Shadowbook** detects drift and compresses old narratives.

The narrative changes, but the host keeps the cornerstone. Shadowbook tells you when the script moved and preserves what matters.

**The hosts never go off-loop without you knowing.**

---

## Support

If Shadowbook saves you time, consider buying me a coffee:

[![paypal](https://www.paypalobjects.com/en_US/i/btn/btn_donateCC_LG.gif)](https://paypal.me/anupamchugh)

---

## Read More

📖 **[The Vibe-Clock Drift Problem](https://chughgpt.substack.com/p/the-vibe-clock-drift-problem)** — Why I built Shadowbook

---

## License

MIT — Same as beads.

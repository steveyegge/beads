# Shadowbook

### `bd` — **b**idirectional **d**rift detection for specs and code

> *Keep your specs and code in sync. Always.*

[![License](https://img.shields.io/github/license/anupamchugh/shadowbook)](LICENSE)

Shadowbook is a git-backed issue tracker with **spec intelligence**. It detects when your spec files change and flags linked issues—so your code never drifts from requirements.

Built on [beads](https://github.com/steveyegge/beads). Works everywhere beads works.

---

## The Problem

You write a spec. You create issues to implement it. The spec changes. **Nobody notices.**

Weeks later, QA finds the mismatch. Sound familiar?

## The Solution

```bash
# Link an issue to a spec
bd create "Implement login" --spec-id specs/auth/login.md

# Later, someone edits the spec...

# Shadowbook detects the drift
bd spec scan
# ● SPEC CHANGED: specs/auth/login.md
#   ↳ bd-a1b2 "Implement login" — spec updated, issue may be stale

# Find all drifted issues
bd list --spec-changed

# After reviewing & updating implementation
bd update bd-a1b2 --ack-spec
```

**Bidirectional drift detection:**
- Spec changes → Issues flagged
- Issues link back → Spec coverage visible

---

## Quick Start

```bash
# Install (same as beads)
curl -fsSL https://raw.githubusercontent.com/anupamchugh/shadowbook/main/scripts/install.sh | bash

# Or via Go
go install github.com/anupamchugh/shadowbook/cmd/bd@latest

# Initialize in your project
cd your-project
bd init
mkdir -p specs

# Create a spec
echo "# Login Feature" > specs/login.md

# Scan specs
bd spec scan

# Link issues to specs
bd create "Implement login" --spec-id specs/login.md
```

---

## Spec Commands

| Command | Action |
|---------|--------|
| `bd spec scan` | Scan specs directory, detect changes, flag linked issues |
| `bd spec list` | List all tracked specs with issue counts |
| `bd spec show <path>` | Show spec details + linked issues |
| `bd spec coverage` | Show specs without linked issues |
| `bd spec compact <path>` | Archive a spec with a summary |
| `bd spec suggest <id>` | Suggest specs for an issue by title match |
| `bd spec link --auto` | Preview auto-links for unlinked issues |
| `bd spec consolidate` | Generate a report of older specs for consolidation |

## Issue Commands (from beads)

| Command | Action |
|---------|--------|
| `bd ready` | List issues with no open blockers |
| `bd create "Title" -p 0` | Create a P0 issue |
| `bd create "Title" --spec-id specs/foo.md` | Create issue linked to spec |
| `bd list --spec-changed` | Show issues with changed specs |
| `bd update <id> --ack-spec` | Acknowledge spec change |
| `bd dep add <child> <parent>` | Add dependency |
| `bd show <id>` | View issue details |

---

## How It Works

```
specs/login.md          ←── You edit this
       ↓
   bd spec scan         ←── Detects SHA256 change
       ↓
   bd-a1b2              ←── Issue flagged: SPEC CHANGED
   (spec_id: specs/login.md)
       ↓
   bd list --spec-changed  ←── Find all drifted issues
       ↓
   bd update bd-a1b2 --ack-spec  ←── Clear flag after review
```

**Key insight:** Specs are files. Files have hashes. When hashes change, linked issues get flagged. Simple.

---

## Features

Everything from beads, plus:

- **Spec Registry** — Local SQLite cache of all spec files (path, title, SHA256, timestamps)
- **Change Detection** — `bd spec scan` compares hashes, flags linked issues
- **Coverage Metrics** — Find specs with no linked issues
- **Drift Alerts** — `SPEC CHANGED` warning in issue output
- **Spec Auto-Match** — Suggest links for unlinked issues (`bd spec suggest`, `bd spec link --auto`)
- **Spec Compaction** — Archive old specs into summaries to save context

### From Beads

- **Git as Database** — Issues stored as JSONL in `.beads/`, versioned with your code
- **Agent-Optimized** — JSON output, dependency tracking, auto-ready detection
- **Zero Conflict** — Hash-based IDs (`bd-a1b2`) prevent merge collisions
- **Background Sync** — Daemon auto-syncs changes
- **Compaction** — Summarize old issues to save context

---

## Filtering Issues by Spec

```bash
# Exact spec match
bd list --spec specs/auth/login.md

# Prefix match (all auth specs)
bd list --spec specs/auth/

# Issues with spec drift
bd list --spec-changed
```

---

## Spec Compaction (Token Savings)

When a spec is done, you can archive it with a short summary:

```bash
bd spec compact specs/auth.md --summary "OAuth2 login. 3 endpoints. JWT. Done Jan 2026."
```

This keeps the essential memory while cutting context size dramatically. You still have the full spec on disk, but the registry surfaces the summary first.

You can also auto-compact when closing the last linked issue:

```bash
bd close bd-xyz --compact-spec
```

To see the size impact during auto-linking:

```bash
bd spec link --auto --threshold 80 --show-size
```

---

## Upstream Sync

Shadowbook tracks [steveyegge/beads](https://github.com/steveyegge/beads) as upstream:

```bash
# Pull latest beads updates
git fetch upstream
git merge upstream/main
```

---

## Documentation

- **[Docs Index](docs/SHADOWBOOK_DOCS_INDEX.md)** — Complete documentation map
- **[User Manual](docs/SHADOWBOOK_MANUAL.md)** — The Host's Awakening (Westworld style guide)
- **[Engineering Spec](docs/SHADOWBOOK_ENG_SPEC.md)** — Technical implementation details
- **[Compaction & Lifecycle](docs/SHADOWBOOK_COMPACTION_LIFECYCLE.md)** — Memory decay, archival, semantic compression (6 advanced ideas)
- [Spec Sync Guide](docs/SPEC_SYNC.md) — How spec intelligence works
- [Beads Docs](https://github.com/steveyegge/beads#-documentation) — Full beads documentation
- [AGENTS.md](AGENTS.md) — Agent workflow guide

---

## Why "Shadowbook"?

Your specs cast a shadow over your code. When the spec moves, the shadow should move too. Shadowbook makes sure it does.

`bd` = **b**idirectional **d**rift

---

## License

MIT — Same as beads.

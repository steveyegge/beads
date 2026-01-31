# Shadowbook

### `bd` — detect when specs change but code doesn't know

> Specs evolve. Tasks sprint. Shadowbook catches the drift.

You edit `specs/login.md` at 3am. The issue implementing it should know. Shadowbook watches spec files for changes and flags linked issues before your code drifts from reality.

[![License](https://img.shields.io/github/license/anupamchugh/shadowbook)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/anupamchugh/shadowbook)](https://goreportcard.com/report/github.com/anupamchugh/shadowbook)
[![Release](https://img.shields.io/github/v/release/anupamchugh/shadowbook)](https://github.com/anupamchugh/shadowbook/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/anupamchugh/shadowbook)](go.mod)
[![Last Commit](https://img.shields.io/github/last-commit/anupamchugh/shadowbook)](https://github.com/anupamchugh/shadowbook/commits)

Built on [beads](https://github.com/steveyegge/beads). Works everywhere beads works.

---

## The Problem: Spec Drift

You write a spec. You create an issue to implement it. Then you update the spec—but the issue keeps building the old version.

```bash
bd create "Implement login flow" --spec-id specs/login.md
```

Later, you edit the spec:

```diff
# specs/login.md (updated at 3am)
- "OAuth2 with Google"
+ "OAuth2 with Google AND Apple"
```

The issue `bd-a1b2` is still building Google-only auth. **The spec changed, but the code doesn't know.**

This is spec drift.

---

## The Solution: Drift Detection

Shadowbook compares spec file hashes against linked issues. When a spec changes, linked issues get flagged.

```bash
bd spec scan

● SPEC CHANGED: specs/login.md
  ↳ bd-a1b2 "Implement login flow" — spec updated, issue unaware
```

Find all drifted issues:

```bash
bd list --spec-changed
```

Acknowledge after reviewing:

```bash
bd update bd-a1b2 --ack-spec
```

**Key insight:** Specs are files. Files have hashes. When hashes change, linked issues get flagged.

---

## Context Economics: Auto-Compaction

Completed specs waste tokens. A 2000-token spec that's done should become a 20-token summary.

```bash
bd spec compact specs/login.md --summary "OAuth2 login. 3 endpoints. JWT. Done Jan 2026."
```

| Before | After | Savings |
|--------|-------|---------|
| Full spec in context | Summary in registry | **~95%** |
| ~2000 tokens | ~20 tokens | Per spec |

Shadowbook scores specs for auto-compaction using multiple factors:
- All linked issues closed (+40%)
- Spec unchanged 30+ days (+20%)
- Code unmodified 45+ days (+20%)
- Marked SUPERSEDED (+20%)

```bash
bd spec candidates        # Show compaction candidates with scores
bd spec auto-compact      # Compact specs scoring above threshold
bd close bd-xyz --compact-spec  # Compact on issue close
```

---

## Core Concepts

| Concept | What it means | Command |
|---------|---------------|---------|
| Spec files | Markdown files defining requirements | `specs/*.md` |
| Drift | Spec changed, issue doesn't know | `bd spec scan` |
| Link | Issue tracks a spec | `bd create --spec-id` |
| Acknowledge | Mark issue as aware of spec change | `bd update --ack-spec` |
| Compact | Archive completed spec to summary | `bd spec compact` |
| Auto-compact | Score and archive stale specs | `bd spec auto-compact` |

<details>
<summary>🎬 Westworld vocabulary (for fans)</summary>

| Westworld | Shadowbook |
|-----------|------------|
| Ford's narratives | Spec files |
| Hosts | Issues/beads |
| Cornerstone memories | `--spec-id` links |
| Mesa diagnostics | Drift detection |
| "These violent delights" | `--spec-changed` flag |
| Accepting new loop | `--ack-spec` |
| Archiving the script | Compaction |

</details>

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

# Write a spec
echo "# Login Feature" > specs/login.md

# Scan specs
bd spec scan

# Create an issue linked to the spec
bd create "Implement login" --spec-id specs/login.md

# ... spec changes ...

# Detect drift
bd spec scan
bd list --spec-changed

# Acknowledge
bd update bd-xyz --ack-spec
```

---

## How It Works

```
specs/login.md             ←── You edit the spec
       ↓
   bd spec scan            ←── Shadowbook detects SHA256 change
       ↓
   bd-a1b2                 ←── Issue flagged: SPEC CHANGED
   (spec_id: specs/login.md)
       ↓
   bd list --spec-changed  ←── Find drifted issues
       ↓
   bd update bd-a1b2 --ack-spec  ←── Acknowledge new spec
```

---

## Spec Commands

| Command | Action |
|---------|--------|
| `bd spec scan` | Detect spec changes, flag linked issues |
| `bd spec list` | List all tracked specs with issue counts |
| `bd spec show <path>` | Show spec details + linked issues |
| `bd spec coverage` | Find specs with no linked issues |
| `bd spec audit` | **NEW:** Audit all specs with completion status |
| `bd spec mark-done <path>` | **NEW:** Mark spec as complete |
| `bd spec candidates` | Score specs for completion (auto-detect done specs) |
| `bd spec candidates --auto` | **NEW:** Auto-mark specs with score >= 0.8 |
| `bd spec compact <path>` | Archive spec to summary |
| `bd spec auto-compact` | Compact specs above threshold |
| `bd spec suggest <id>` | Suggest specs for unlinked issues |
| `bd spec link --auto` | Bulk-link issues to specs |
| `bd spec consolidate` | Report stale specs for archival |

Tip: Install git hooks to detect drift after merges/checkouts:
`bd hooks install`

## Issue Commands (from beads)

| Command | Action |
|---------|--------|
| `bd ready` | List issues with no open blockers |
| `bd create "Title" -p 0` | Create a P0 issue |
| `bd create "Title" --spec-id specs/foo.md` | Create issue linked to spec |
| `bd list --spec-changed` | Show issues with outdated specs |
| `bd list --no-spec` | Show issues with no spec |
| `bd update <id> --ack-spec` | Acknowledge spec change |
| `bd close <id> --compact-spec` | Close issue + archive spec |
| `bd close <id> --compact-skills` | Close issue + archive unused skills |
| `bd preflight --check` | Run all pre-commit checks (tests, lint, skills) |
| `bd preflight --check --auto-sync` | Run checks and auto-fix skill drift |

## Activity Dashboard

See recent activity across beads, specs, and skills in one view:

```bash
bd recent                 # Show recent beads and specs
bd recent --all           # Nested view: beads → specs → skills
bd recent --skills        # Include skills in output
bd recent --today         # Items modified today
bd recent --stale         # Show stale items (30+ days old)
```

Example nested output with `bd recent --all`:

```
bd-456 [P1] Implement auth endpoints          ◐ in-progress  Today
  └─ specs/auth/LOGIN_SPEC.md                 ◐ in-progress  2h ago
     └─ tdd (skill)                           active         3h ago

bd-445 [P2] Fix scanner logic                 ○ pending      3d ago
  └─ (no linked spec)

──────────────────────────────────────────
Summary: 5 beads, 3 specs, 2 skills
├─ Active: 4 in-progress, 2 pending
├─ Stale (30+ days): 2
└─ Momentum: 3 items updated today
```

## Skills Tracking

Track which skills are used by which issues:

```bash
bd create "Fix bug" --skills=debugging,tdd    # Link skills when creating
bd skills audit                                # See skill drift across agents
bd skills sync                                 # Sync Claude → Codex skills
bd skills cleanup-candidates                   # Find unused skills
bd close <id> --compact-skills                 # Archive skills no longer used
```

When all issues for a spec are closed, Shadowbook suggests:

```bash
✓ Closed bd-123: Fixed auth bug

● All issues for spec specs/auth.md are now closed.
  Run: bd spec mark-done specs/auth.md
```

---

## Features

Everything from beads, plus:

- **Spec Registry** — SQLite cache of specs (path, title, SHA256, timestamps)
- **Drift Detection** — `bd spec scan` compares hashes, flags linked issues
- **Coverage Metrics** — Find specs with no linked issues
- **Drift Alerts** — `SPEC CHANGED` warning in issue output
- **Multi-Factor Compaction** — Score specs by staleness (closed issues, age, activity)
- **Auto-Match** — Suggest links using Jaccard similarity (`bd spec suggest`)
- **Skills Manifest** — Track skill drift across Claude/Codex agents (`specs/skills/manifest.json`)
- **Skill Sync** — Preflight checks for skill synchronization between Claude Code and Codex CLI
- **Preflight Checks** — Validate tests, lint, nix hash, version sync, and skill sync before commits

### From Beads

- **Git as Database** — Issues stored as JSONL in `.beads/`, versioned with your code
- **Agent-Optimized** — JSON output, dependency tracking, auto-ready detection
- **Zero Conflict** — Hash-based IDs (`bd-a1b2`) prevent merge collisions
- **Background Sync** — Daemon auto-syncs changes

---

## Preflight Checks & Skill Sync

Run pre-commit checks to catch issues before they hit CI:

```bash
bd preflight              # Show checklist
bd preflight --check      # Run checks automatically
bd preflight --check --json  # JSON output for CI
bd preflight --check --auto-sync  # Auto-fix skill drift
```

**Checks included:**
- Skills synced (Claude Code ↔ Codex CLI)
- Tests pass (`go test -short ./...`)
- Lint passes (`golangci-lint run ./...`)
- Nix hash current (go.sum unchanged)
- Version sync (version.go matches default.nix)

**Skill sync integration:**
- Shadowbook detects when skills drift between Claude Code and Codex CLI
- Use `--auto-sync` to automatically sync missing skills
- Use `--compact-skills` on `bd close` to clean up unused skills

See [SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md](specs/SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md) for details.

---

## Filtering

```bash
# Exact spec match
bd list --spec specs/auth/login.md

# Prefix match (all auth specs)
bd list --spec specs/auth/

# Issues with spec drift
bd list --spec-changed

# Issues with no spec
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

## Testing in Any Codebase (packnplay)

Use [packnplay](https://github.com/obra/packnplay) to test Shadowbook in isolated Docker containers:

```bash
# Install packnplay
brew install obra/tap/packnplay

# Run Claude Code in container with bd mounted
cd /path/to/any/project
packnplay run claude

# Inside container:
bd init
bd spec scan specs/
bd recent --all
```

Or add to your project's `.devcontainer/devcontainer.json`:

```json
{
  "postCreateCommand": "curl -L https://github.com/anupamchugh/shadowbook/releases/latest/download/bd-linux-amd64 -o /usr/local/bin/bd && chmod +x /usr/local/bin/bd"
}
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

Every spec casts a shadow over the code implementing it. When the spec moves, the shadow should move too. Shadowbook makes sure your code notices.

---

## Positioning

Shadowbook answers a question other tools don't:

| Tool | Question it answers |
|------|---------------------|
| Spec Kit | How do I write specs? |
| Beads | What work needs doing? |
| **Shadowbook** | Is the work still aligned with the spec? |

Specs evolve. Shadowbook detects the drift and compacts what's done.

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

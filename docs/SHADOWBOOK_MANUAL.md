# Shadowbook Manual

Shadowbook keeps specs and implementation aligned by linking issues to spec files and detecting drift when specs change.

---

## Install

```bash
go install github.com/anupamchugh/shadowbook/cmd/bd@latest
bd init
```

---

## Core Workflow

```bash
# Scan specs (default: specs/)
bd spec scan

# Link an issue to a spec
bd create "Implement login" --spec-id specs/login.md

# Detect drift
bd list --spec-changed

# Acknowledge after review
bd update bd-xxx --ack-spec
```

---

## Spec Commands

```bash
bd spec scan                 # Update registry and detect changes
bd spec list                 # Show all specs and linked counts
bd spec show <spec_id>       # Show spec + linked issues
bd spec coverage             # Coverage metrics
bd spec candidates           # Score specs for auto-compaction
bd spec auto-compact         # Dry-run auto-compaction
bd spec volatility           # Summarize spec volatility
```

---

## Spec Volatility

Use volatility to spot specs that are changing frequently while work is in flight.

```bash
bd spec volatility --since 14d --min-changes 2
```

When you link a new issue to a volatile spec, `bd create --spec-id` prompts before creating work.
`bd ready` groups ready work into stable and volatile sections.
Use `bd list --show-volatility` to add volatility badges in list output.
`bd list --json` includes a `volatility` object per issue when spec data is available.
If `volatility.auto_pause` is enabled, HIGH volatility specs will auto-pause linked issues. Resume with `bd resume --spec <path>`.

Flags:
- `--since` controls the lookback window (default: 30d)
- `--min-changes` filters low-activity specs
- `--limit` caps the number of rows returned
- `--format list` switches to a compact list view
- `--fail-on-high` exits 1 if any HIGH volatility specs are detected
- `--fail-on-medium` exits 1 if any MEDIUM or HIGH volatility specs are detected
- `--with-dependents <spec>` shows dependent issue cascade for a spec
- `--recommendations` prints stabilization recommendations per spec
- `--trend <spec>` shows volatility trend history for a spec

---

## Auto‑Linking (Preview‑First)

```bash
# Suggest matches for one issue
bd spec suggest bd-xxx

# Preview bulk matches
bd spec link --auto --threshold 80

# Apply matches explicitly
bd spec link --auto --threshold 80 --confirm

# Table view for review
bd spec link --auto --format table

# Show size impact
bd spec link --auto --show-size
```

Notes:
- Preview is default; nothing is written without `--confirm`.
- `--threshold` controls strictness.
- `--format table` is easier to review.

---

## Compaction (Token Savings)

```bash
# Manual compaction
bd spec compact specs/auth.md --summary "OAuth2 login. 3 endpoints. Done Jan 2026."

# Auto‑compact when closing the last linked issue
bd close bd-xxx --compact-spec
```

Compaction keeps the full spec on disk but stores a short summary in the registry to reduce context.

## Auto-Compaction (Optional)

```bash
# Preview candidates (default threshold: 0.7)
bd spec candidates

# Dry run (default threshold: 0.8)
bd spec auto-compact

# Execute (writes summaries + archives)
bd spec auto-compact --execute
```

Auto-compaction uses multiple signals (linked issues closed, spec staleness, git activity, superseded markers)
to suggest or archive specs safely.

---

## Consolidation Report (Safe, Report‑Only)

```bash
bd spec consolidate --older-than 180 --report docs/SHADOWBOOK_CONSOLIDATION_REPORT.md
```

This does not modify specs; it only lists candidates.

---

## Spec IDs (What is Scannable)

Scannable spec IDs are **repo‑relative paths** (e.g., `specs/auth.md`).

Not scannable:
- URLs (`https://...`)
- Absolute paths (`/Users/...`)
- External IDs (`SPEC-001`, `JIRA-123`)

Only scannable IDs are eligible for drift detection and compaction.

---

## Registry Notes

- The spec registry is **local‑only**.
- Run `bd spec scan` on each machine to update it.
- The registry lives in `.beads/beads.db` (SQLite).
- Install git hooks to auto-scan after merges/checkouts: `bd hooks install` (runs only if `specs/` exists).

# Shadowbook Product Hierarchy

Shadowbook is a fork of Beads that adds spec, skill, and stability primitives to make project drift visible and actionable.

## Core Objects

- **Beads**: Issues and tasks stored in `.beads/` (the system of record).
- **Specs**: Markdown specs tracked by hash and status in the spec registry.
- **Skills**: Agent capabilities tracked for drift across environments.
- **Drifts**: Four lenses that explain where work and reality diverge.

## The Four Drifts

1. **Spec Drift** — Specs changed, code or issues lag. Command: `bd spec scan`.
2. **Skill Drift** — Agents are out of sync. Command: `bd preflight --check` and `bd skills`.
3. **Visibility Drift** — Work exists but is invisible. Command: `bd recent --all`.
4. **Stability Drift** — Specs churn while work is in flight. Command: `bd spec volatility`.

## Command Hierarchy (What a user sees)

- **Daily loop**: `bd recent --all` → `bd ready` → `bd close`.
- **Spec loop**: `bd spec scan` → `bd spec audit` → `bd spec compact`.
- **Skill loop**: `bd skills audit` → `bd skills sync` → `bd preflight --check`.
- **Stability loop**: `bd spec volatility` → pause/resume decisions.
- **Gamified flow**: `bd pacman` for streaks and scorekeeping.

## How it Fits Together

- Beads reference specs. Specs are scanned into the registry.
- Skills are compared across agent environments.
- Drift commands reveal mismatches and suggest actions.
- Pacman adds motivation without changing the core data model.

## Workspace-Level View (Optional)

- `bd workspace scan` summarizes content across repos and writes triage notes to `workspace-hub`.
- Beads remain project-scoped; the hub is a front door, not a database.

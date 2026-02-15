# Beads Hooks & Export Integration Plan

**Status:** Planning
**Priority:** P1
**Date:** 2026-02-14

## Overview

Add a hooks system and structured export to beads, enabling automatic integration
with external tools (bobbin vector search, Telegram notifications, dashboards).
Inspired by Jira webhooks and git hooks but adapted for the beads/Dolt model.

## Motivation

Beads is the central issue tracker for Gas Town. Currently it's a CLI tool with
Dolt storage. To become a true integration hub, it needs:
1. **Hooks** — trigger external actions on bead events (like Jira webhooks)
2. **Export** — structured data output for external consumers
3. **Feed** — change stream leveraging Dolt's version control

This unlocks:
- Bobbin auto-indexing (hook fires on bead write → bobbin re-embeds)
- Telegram/ntfy alerts (hook fires on P0 create → notification)
- Dashboard updates (export/feed → web UI)
- External tool integration without modifying bd core

## Architecture

```
bd create/update/close
        │
        ▼
┌───────────────────┐
│   Write to Dolt   │
│   (existing path) │
└───────┬───────────┘
        │
        ▼
┌───────────────────┐     ┌─────────────────┐
│   Hook Dispatcher │────→│ bobbin index-bead│ (vector search)
│   (NEW)           │────→│ ntfy/Telegram   │ (notifications)
│                   │────→│ custom scripts  │ (extensible)
└───────────────────┘     └─────────────────┘
```

## Design: Hook System

### Configuration

```toml
# .beads/config.toml (or beads.toml)

[[hooks]]
event = "post-write"           # fires after any write (create, update, close)
command = "bobbin index-bead --id ${BEAD_ID} --rig ${BEAD_RIG}"
async = true                   # don't block bd command on hook completion

[[hooks]]
event = "post-create"          # fires only on new bead creation
filter = "priority:P0,P1"      # only for high-priority beads
command = "curl -s -d '${BEAD_TITLE}' http://ntfy.svc/aegis"
async = true

[[hooks]]
event = "post-close"
command = "echo 'Closed: ${BEAD_ID} ${BEAD_TITLE}' >> /tmp/beads-activity.log"
```

### Event Types

| Event | Fires when | Variables available |
|-------|-----------|-------------------|
| `post-create` | New bead created | BEAD_ID, BEAD_RIG, BEAD_TITLE, BEAD_PRIORITY, BEAD_TYPE |
| `post-update` | Bead modified | BEAD_ID, BEAD_RIG, BEAD_FIELD, BEAD_OLD_VALUE, BEAD_NEW_VALUE |
| `post-close` | Bead closed | BEAD_ID, BEAD_RIG, BEAD_TITLE, BEAD_REASON |
| `post-comment` | Comment added | BEAD_ID, BEAD_RIG, COMMENT_AUTHOR, COMMENT_BODY |
| `post-write` | Any write op | BEAD_ID, BEAD_RIG, BEAD_EVENT (create/update/close/comment) |

### Filter Syntax

```toml
filter = "priority:P0,P1"           # priority is P0 or P1
filter = "status:open"              # only open beads
filter = "type:bug"                 # only bugs
filter = "rig:aegis"                # only aegis rig
filter = "priority:P0 AND type:bug" # compound (stretch goal)
```

### Implementation Plan

1. **Config parsing** — add `[[hooks]]` section to config.toml parser
2. **Hook dispatcher** — fire after write operations in crud.go
3. **Variable expansion** — substitute ${BEAD_*} vars in command string
4. **Async execution** — spawn hook commands without blocking bd
5. **Error handling** — log failures, never fail the bd command due to hook
6. **CLI management** — `bd hooks list`, `bd hooks test <event>`

### Code Location

```
cmd/hooks.go           # CLI: bd hooks list/test
internal/hooks/        # Hook engine
  config.go            # Parse [[hooks]] from config
  dispatcher.go        # Fire hooks on events
  expansion.go         # Variable substitution
  runner.go            # Async command execution
```

## Design: Export System

### Commands

```bash
# Full export
bd export --format json > beads.json
bd export --format csv > beads.csv
bd export --format yaml > beads.yaml

# Filtered export
bd export --format json --status open --priority P0,P1
bd export --format json --since "2026-02-01"

# Single bead
bd export aegis-0a9 --format json
```

### Output Format (JSON)

```json
{
  "version": 1,
  "exported_at": "2026-02-14T20:00:00Z",
  "rig": "aegis",
  "beads": [
    {
      "id": "aegis-0a9",
      "title": "Standing Directive",
      "type": "task",
      "priority": "P1",
      "status": "in_progress",
      "assignee": "aegis/crew/goldblum",
      "created_at": "2026-02-14T...",
      "updated_at": "2026-02-14T...",
      "description": "...",
      "comments": [
        {
          "author": "aegis/crew/goldblum",
          "body": "...",
          "created_at": "2026-02-14T..."
        }
      ],
      "dependencies": {
        "blocks": ["aegis-xyz"],
        "blocked_by": []
      },
      "labels": ["standing-directive"]
    }
  ]
}
```

## Design: Change Feed

Leverages Dolt's built-in version control:

```bash
# What changed since timestamp
bd feed --since "2026-02-14T12:00:00Z"

# What changed in last N hours
bd feed --last 24h

# Watch mode (poll every 30s)
bd feed --watch --interval 30s
```

Output: structured list of changes (creates, updates, closes, comments) with
before/after values. Uses Dolt diff under the hood.

## Jira Comparison

| Jira Feature | Beads Equivalent | Notes |
|-------------|-----------------|-------|
| Webhooks | `bd hooks` | Local command execution (more flexible than HTTP-only) |
| REST API | Dolt SQL + `bd export` | SQL is more powerful than REST for queries |
| JQL | SQL via Dolt | Already more expressive |
| Activity Stream | `bd feed` | Leverages Dolt diff (git-like history) |
| Connect Apps | Hooks + export | Simpler, no app marketplace needed |
| Dashboards | Export → web UI | TBD (Phase 3 intelligence layer) |

**Beads advantages over Jira:**
- Full version history via Dolt (time travel to any state)
- SQL access without API wrappers
- Local-first, works offline
- Hook system fires local commands (no HTTP callback infrastructure)
- Dolt replication for cross-host sync (no cloud dependency)

## Implementation Order

1. **Hooks (core)** — config parsing + dispatcher + variable expansion
2. **Export (json)** — single format first, expand later
3. **Feed** — wrap Dolt diff with UX-friendly output
4. **Hooks (filters)** — priority/status/type filtering
5. **Export (csv/yaml)** — additional output formats
6. **Feed (watch)** — polling mode

## Risks

- Hook command injection — must sanitize bead titles/descriptions before shell expansion
- Dolt server availability — hooks should degrade gracefully if Dolt is down
- Hook storms — rapid bead updates could trigger many hooks (add debouncing?)

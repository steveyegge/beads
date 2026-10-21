---
title: Fizzy Integration Configuration
description: Configuration reference for bd fizzy sync, which bidirectionally syncs beads issues with the cards of a Fizzy board
---

Last reviewed: 2026-10-21

Freshness source: `cmd/bd/fizzy*.go` and `internal/fizzy/`.

This guide covers the configuration of the `bd fizzy sync` command. The command synchronizes beads issues with the cards of one [Fizzy](https://github.com/basecamp/fizzy) board. Fizzy is the kanban tracker of 37signals. You can use the hosted service (fizzy.do) or a self-hosted instance.

**Proxied-server mode:** `bd fizzy sync` and `bd fizzy status` are not
supported when bd is connected to a proxied server (error:
`fizzy sync is not supported in proxied-server mode`). Run them from a
workspace with direct database access.

## Quick Start

1. In Fizzy, open your profile. In the **Developer** section, click **personal access tokens**.
2. Make a token with the **Read + Write** permission.
3. Set the configuration:

   ```bash
   export FIZZY_TOKEN="your-personal-access-token"   # or: bd config set fizzy.token ...
   bd config set fizzy.account "897362094"
   bd config set fizzy.board "03f5v9zkft4hj9qq0lsn9ohcm"
   bd config set fizzy.url "https://fizzy.example.com"  # only for a self-hosted instance
   ```

4. Do a dry run, then sync:

   ```bash
   bd fizzy sync --dry-run
   bd fizzy sync
   ```

The account slug is the number in the Fizzy URLs (`https://app.fizzy.do/897362094/...`). The board ID is in the URL of the board (`.../boards/<board-id>`).

## Configuration Keys

| Key | Environment variable | Required | Use |
|---|---|---|---|
| `fizzy.token` | `FIZZY_TOKEN` | Yes | Personal access token. The key is stored in `config.yaml`, not in the Dolt database. |
| `fizzy.account` | `FIZZY_ACCOUNT` | Yes | Account slug |
| `fizzy.board` | `FIZZY_BOARD` | Yes | ID of the board to sync |
| `fizzy.url` | `FIZZY_URL` | No | URL of the Fizzy instance. The default is `https://app.fizzy.do`. `http://` is accepted only for `localhost`. |
| `fizzy.status_map.<status>` | none | No | Lane of a beads status (see [Status Mapping](#status-mapping)) |

Config keys take priority over environment variables, except for `fizzy.token`, which is read from `config.yaml` first.

## Field Mapping

| beads | Fizzy |
|---|---|
| title | card title |
| description | card description. A push converts the plain text to paragraphs. A pull reads the plain text of the card. |
| status | lane of the card (see [Status Mapping](#status-mapping)) |
| priority | tag `p0` to `p4` |
| issue type | tag `type::<type>`, for example `type::bug` |
| labels | the other tags. Fizzy stores tags in lowercase. |
| `external_ref` | URL of the card, under `fizzy.url` |

These fields are not synchronized:

- **Assignee.** Fizzy users do not map to beads assignees. A pull keeps the local assignee.
- **Dependencies.** Fizzy has no dependency model. beads keeps the dependency graph.
- **Comments, steps and attachments.** The [integration charter](https://github.com/gastownhall/beads/blob/main/engdocs/INTEGRATION_CHARTER.md) puts them out of scope.

A push makes the tags of the card equal to the tags of the issue. Thus, a push removes a tag that a person adds in Fizzy, unless a pull imports it first as a label. A bidirectional sync pulls before it pushes.

## Status Mapping

A Fizzy board has workflow columns and three fixed lanes: `Maybe?` (triage), `Not Now` and `Done`. The status of an issue maps to one lane.

### Default mapping

Without `fizzy.status_map.*` keys, the mapping uses the category of the status:

| beads status | Push (lane) | Pull (status) |
|---|---|---|
| `open`, and the statuses of the `active` category | `Maybe?` | `Maybe?` gives `open` |
| `in_progress`, `blocked`, `hooked`, and the `wip` category | first workflow column | a workflow column gives `in_progress` |
| `deferred`, `pinned`, and the `frozen` category | `Not Now` | `Not Now` gives `deferred` |
| `closed`, and the `done` category | `Done` | `Done` gives `closed` |

The custom statuses and their categories come from `status.custom`.

### Custom mapping

Set `fizzy.status_map.<status>` to the name of a workflow column, or to `Maybe?`, `Not Now` or `Done`:

```bash
bd config set fizzy.status_map.open "Ready"
bd config set fizzy.status_map.in_progress "In Progress"
bd config set fizzy.status_map.blocked "Blocked"
```

The column names are not case-sensitive. If a mapped column does not exist on the board, the push of an issue with this status fails with an error.

On pull, a lane gives the first status that maps to it, in this order: `open`, `in_progress`, `blocked`, `deferred`, `closed`, `pinned`, `hooked`, then the custom statuses in alphabetical order. If the card stays in the lane of the local status, the pull keeps the local status. As a result, two statuses in the same column do not overwrite each other.

## Sync Behavior

`bd fizzy sync` uses the shared tracker sync engine:

- A pull reads the cards of the board. Fizzy has no filter on the update time, so bd reads all the cards and keeps the cards with a `last_active_at` after the last sync. A draft card is not pulled.
- A pull creates an issue for a new card, and gives it an ID with the issue prefix.
- A push creates a card for an issue without `external_ref`, then sets its tags and its lane.
- A push skips an issue that did not change since its last push.
- By default, `molecule`, `message` and `event` issues and ephemeral issues are not pushed.

| Flag | Use |
|---|---|
| `--dry-run` | Show the changes, but do not apply them |
| `--pull-only`, `--push-only` | Sync in one direction |
| `--prefer-local`, `--prefer-fizzy`, `--prefer-newer` | Conflict resolution. The default is `--prefer-newer`. |
| `--type`, `--exclude-type` | Select the issue types to sync |
| `--no-ephemeral` | Exclude the ephemeral issues from push (default: true) |
| `--issues`, `--parent` | Sync only some issues |

## Auto-postpone

Fizzy moves a card to `Not Now` after a period without activity. The default period is 30 days. A pull then changes the status of the issue to `deferred`. If you do not want this, set the auto-postpone period of the board to 365 days, the longest period.

## Rate Limits and Errors

The client retries a request after a `429` or a `5xx` response, with exponential backoff and jitter. It obeys the `Retry-After` header. After 3 retries, the sync stops with an error. The client limits a response to 50 MB and a list to 1,000 pages.

If a step after the creation of a card does not complete (tags or lane), the card stays, and the sync gives a warning.

## Status Command

`bd fizzy status` shows the configuration (with a masked token), the status map, the time of the last sync and the result of the configuration check.

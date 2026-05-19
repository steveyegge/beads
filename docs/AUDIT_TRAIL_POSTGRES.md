# Audit Trail on Postgres

> **Status: planned for v2.** This document is a placeholder. The
> implementation details below describe the intended design; the feature
> is not fully implemented in v1.

---

## Overview

When using the Postgres backend, beads writes application-level audit
events to two tables in the beads database:

- **`events`** — one row per issue mutation (create, update, close,
  label, comment, dependency add/remove, etc.). Populated on every
  write from v1 onwards.
- **`wisp_events`** — same structure, scoped to wisp (dependency
  graph) mutations.

These tables serve as the Postgres equivalent of Dolt's native commit
history. Because Postgres has no built-in row-level versioning, bd
is responsible for appending event rows on every write.

---

## What is recorded (v1)

Every `bd` write command appends at minimum:

| Field | Description |
|---|---|
| `event_type` | Verb: `create`, `update`, `close`, `label_add`, `label_remove`, `comment`, `dep_add`, `dep_remove`, etc. |
| `issue_id` | The affected issue or wisp ID. |
| `actor` | The `bd` user or agent identity (from config or `$USER`). |
| `occurred_at` | UTC timestamp. |
| `payload` | JSON snapshot of the changed fields. |

---

## What is NOT recorded (v1 limitations)

- **Pre-migration Dolt history** — audit events from before a
  `bd migrate --to=postgres` are not backfilled. Only events that
  occur after migration are recorded in the Postgres events table.
  The `--include-events` flag on `bd migrate` is reserved as a v1
  placeholder and returns `migration: feature not implemented in v1`.
- **`bd_commits` grouping** — Dolt records grouped operations under
  a single commit message. On Postgres, each write is a separate event
  row; grouping under a shared `commit_id` is deferred to v2.

---

## Querying the audit log

Events are plain SQL rows. Query them directly via `psql` or any
Postgres client:

```sql
-- All events for a specific issue
SELECT event_type, actor, occurred_at, payload
FROM events
WHERE issue_id = 'be-abc123'
ORDER BY occurred_at;

-- Last 50 writes by any actor
SELECT issue_id, event_type, actor, occurred_at
FROM events
ORDER BY occurred_at DESC
LIMIT 50;
```

---

## See Also

- [POSTGRES-BACKEND.md](POSTGRES-BACKEND.md) — operator reference for
  the Postgres backend.
- [DOLT.md](DOLT.md) — Dolt's native commit history, which this
  feature is designed to approximate on Postgres.

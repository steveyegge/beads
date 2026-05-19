# PostgreSQL Storage Backend

- [Overview](#overview)
- [When to choose Postgres vs. Dolt](#when-to-choose-postgres-vs-dolt)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Connection Strings (DSN forms)](#connection-strings-dsn-forms)
- [Authentication: how the password flows](#authentication-how-the-password-flows)
- [Backup and Restore](#backup-and-restore)
- [Migration from Dolt](#migration-from-dolt)
- [Operational Gotchas](#operational-gotchas)
- [Troubleshooting](#troubleshooting)
- [Configuration Reference](#configuration-reference)
- [See Also](#see-also)

---

## Overview

Beads ships Postgres as an opt-in storage backend alongside the default Dolt
backend. Postgres is suited for multi-writer deployments and teams that already
operate a Postgres server. Dolt remains the default and is recommended for
solo developers and small teams. For background on the architecture and
audit-trail strategy, see
[AUDIT_TRAIL_POSTGRES.md](AUDIT_TRAIL_POSTGRES.md).

| Feature | Postgres |
|---|---|
| Storage | External server (you operate it) |
| Version control | Application-level event log (no native commit history) |
| Branching | No |
| Time travel | No (events-table queries only) |
| Multi-user concurrent | Native (MVCC) |
| Sync | Native pg replication (logical, physical), or `pg_dump` |

---

## When to choose Postgres vs. Dolt

Dolt is the default backend and works for the vast majority of
beads users. Postgres exists for cases where you need
production-grade concurrency on a server you already operate, or
where you want a single database host to back many bd instances.

| Choose **Dolt** when… | Choose **Postgres** when… |
|---|---|
| You want zero-setup: `bd init` and go. | You already run Postgres in your infrastructure. |
| You're a solo developer or small team. | You need many concurrent writers (a fleet of agents, an orchestrator) on one bd database. |
| You want native time-travel / diff / blame on your issues. | You're consolidating many per-rig dolt instances onto one server. |
| You want a Git-style branching workflow for your issues. | You need a hosted-pg flavor (RDS, Cloud SQL, Supabase) for backup/HA/observability already paid for. |
| You want `bd backup` / `bd dolt push` / DoltHub federation. | You're OK using pg-native tooling (`pg_dump`, `pg_restore`, replication) for backup and HA. |

**What you give up by choosing Postgres:**

- **No native commit history on issues.** Dolt records every
  write as a commit hash; on pg, bd writes its own
  application-level audit trail to an `events` table (see
  [AUDIT_TRAIL_POSTGRES.md](AUDIT_TRAIL_POSTGRES.md)). `bd history`,
  `bd diff`, and `bd restore --as-of=<commit>` work on Dolt but are
  reduced to event-log queries on pg. Full commit-message grouping is
  deferred to a post-v1 release.
- **No `bd backup` subcommand.** Use `pg_dump` / `pg_restore`
  (see [Backup and Restore](#backup-and-restore)).
- **No `bd dolt push` / `bd dolt pull`.** Use pg-native
  replication or `pg_dump` to share data across machines.
- **No branching / time travel.** The dolt-specific
  `bd dolt remote add`, branching workflows, and
  `dolt_diff_<table>` queries do not exist on pg.

**What you give up by choosing Dolt:**

- **Embedded mode is single-writer.** Concurrent agents must use
  `bd init --server` (sql-server mode) or a shared dolt server.
  Postgres handles concurrency natively.
- **One database per rig.** Dolt's embedded mode lives in
  `.beads/embeddeddolt/` per project. Sharing requires either
  shared-server mode (one port per machine) or a remote.
  Postgres lets one server host many bd databases trivially —
  one `CREATE DATABASE` per bd instance.
- **Operational tooling parity.** Postgres has 30 years of
  monitoring, replication, and HA tooling; Dolt's equivalents
  are newer.

**Heuristic:** if you're not sure, start with Dolt. Migration
from Dolt to Postgres is supported via `bd migrate --to=postgres`
(see [Migration from Dolt](#migration-from-dolt)); going the
other direction is not.

---

## Prerequisites

- **Postgres 14 or later.** The CI matrix uses `postgres:14-alpine`
  (via testcontainers-go). Earlier versions are not tested and may
  encounter schema or driver incompatibilities.
- **A database the bd user can create tables in.** Either own the
  database outright or have `CREATE` privileges granted by a
  superuser:
  ```sql
  GRANT CREATE ON DATABASE beads_proj TO bduser;
  ```
- **No required extensions.** The bd schema migrations do not use
  `CREATE EXTENSION`. No additional Postgres extensions need to be
  installed before `bd init`.
- **`psql` recommended for diagnostics, but not required.** bd
  connects via its own driver (`pgx/v5`). `psql` is useful for
  verifying connectivity and running manual queries when
  troubleshooting.

---

## Quick Start

The steps below walk from a fresh Postgres install to a working bd
instance. Replace steps 1–2 with your cloud provider's
database-creation flow (RDS, Cloud SQL, Supabase, etc.) if you are
using a hosted Postgres service.

```bash
# 1. Install Postgres (example: Ubuntu/Debian)
sudo apt install postgresql

# 2. Create a database and user
sudo -u postgres createuser --pwprompt bduser
sudo -u postgres createdb -O bduser beads_proj

# 3. Initialize bd with the Postgres backend
cd ~/my-project
bd init --backend=postgres \
  --dsn='postgres://bduser:mypassword@127.0.0.1:5432/beads_proj'

# 4. Set the password for all subsequent bd commands
export BEADS_POSTGRES_PASSWORD='mypassword'

# 5. Verify
bd list
bd doctor
```

After step 3, bd strips the password from the DSN before writing it
to `.beads/metadata.json`. Every subsequent invocation reads the
stripped DSN and re-injects the password from
`BEADS_POSTGRES_PASSWORD`. If the env var is unset, bd connects
without a password and Postgres returns `SQLSTATE 28P01`. See
[Authentication: how the password flows](#authentication-how-the-password-flows)
for details and alternative mechanisms (`.pgpass`, `PGPASSWORD`,
IAM auth).

---

## Connection Strings (DSN forms)

### URI form

```
postgres://user:password@host:port/database?sslmode=require
```

The URI form is recommended. `bd init --dsn=` parses it directly.
The password is stripped before persistence (see
[Authentication: how the password flows](#authentication-how-the-password-flows));
the `postgres_dsn` entry in `.beads/metadata.json` will have the
URI without a password component.

### Keyword form

```
host=db.example.com port=5432 user=bduser dbname=beads_proj sslmode=require
```

The keyword form is also accepted by `bd init --dsn=` — bd passes
it to `pgconn.ParseConfig` before stripping the password. The
stripped form written to `metadata.json` is always URI-form
regardless of which form you provide at init time.

### `sslmode` values

| Value | Behavior |
|---|---|
| `disable` | No TLS; plaintext connection only. |
| `allow` | Prefer plaintext; use TLS only if the server requires it. |
| `prefer` | Prefer TLS; fall back to plaintext. pgconn default when `sslmode` is unspecified. |
| `require` | TLS required; server certificate not verified. |
| `verify-ca` | TLS required; CA of server certificate verified. |
| `verify-full` | TLS required; CA and hostname of server certificate verified. |

bd preserves the operator's explicit `sslmode` value verbatim across
`bd init`'s strip-and-compose cycle. When `sslmode` is not present
in the DSN, pgconn's default (`prefer`) applies.

### `.pgpass` file

The `.pgpass` file (`~/.pgpass` on Linux/macOS,
`%APPDATA%\postgresql\pgpass.conf` on Windows) lets you store
per-host credentials so you do not need `BEADS_POSTGRES_PASSWORD` in
the environment. Format (one entry per line):

```
hostname:port:database:username:password
```

The file must have permissions `0600`; pgx ignores it otherwise.
This is the recommended approach for operators managing several bd
databases on different hosts.

---

## Authentication: how the password flows

Postgres asks for a password every time bd connects. bd does
**not** store the password to disk. Understanding which
mechanism supplies the password at each step keeps you out of
the most common pitfall — getting `SQLSTATE 28P01: password
authentication failed` and reaching for `pg_hba trust` as a
workaround. Trust auth is **not** the answer; an environment
variable is.

### What bd does at `bd init` time

When you run:

```bash
bd init --backend=postgres \
  --dsn='postgres://bduser:mypassword@db.example.com/beads_proj'
```

bd:

1. Parses the DSN.
2. Connects once using the raw DSN (password included) to run
   schema migrations and seed the issue prefix.
3. **Strips the password** from the DSN and writes the
   password-less form to `.beads/metadata.json`:
   ```json
   { "backend": "postgres",
     "postgres_dsn": "postgres://bduser@db.example.com/beads_proj" }
   ```
4. Prints a reminder that subsequent `bd` invocations need the
   password from the environment.

```mermaid
sequenceDiagram
    autonumber
    actor U as Operator
    participant bd as bd init
    participant md as metadata.json
    participant pg as PostgreSQL
    U->>bd: --dsn=postgres://user:PW@host/db
    bd->>pg: connect(user:PW@host/db)
    pg-->>bd: OK, run migrations
    bd->>md: write { "postgres_dsn": "postgres://user@host/db" }
    Note over md: password stripped
    bd-->>U: "Set BEADS_POSTGRES_PASSWORD before next bd command"
```

### What bd does on every subsequent invocation

Every later `bd` command (`bd list`, `bd ready`, `bd create`,
…):

1. Reads `metadata.json` and finds the stripped DSN.
2. Reads `BEADS_POSTGRES_PASSWORD` from the environment.
3. Composes a complete DSN by injecting the env-var password
   back into the stripped form.
4. Connects to pg with the composed DSN.

If `BEADS_POSTGRES_PASSWORD` is **unset**, bd connects with
**no password**. On a default Postgres install (which uses
`scram-sha-256` for non-local users), pg responds with
`SQLSTATE 28P01: password authentication failed for user
"bduser"`. This is the failure operators reach for `pg_hba
trust` to dodge — but the actual fix is one of:

```mermaid
sequenceDiagram
    autonumber
    actor U as Operator
    participant env as $BEADS_POSTGRES_PASSWORD
    participant bd as bd list (or any cmd)
    participant md as metadata.json
    participant pg as PostgreSQL
    U->>bd: bd list
    bd->>md: read postgres_dsn (no password)
    bd->>env: read BEADS_POSTGRES_PASSWORD
    bd->>pg: connect(user:env-pw@host/db)
    alt env var set & correct
        pg-->>bd: OK
        bd-->>U: issue list
    else env var unset
        bd->>pg: connect(user@host/db) (no password)
        pg-->>bd: SQLSTATE 28P01
        bd-->>U: auth error
    end
```

### How to supply the password

| Mechanism | When to use | Persistence |
|---|---|---|
| `export BEADS_POSTGRES_PASSWORD='…'` | Day-to-day local dev. | Process env; usually goes in `.envrc` / `~/.zshrc`. |
| `BEADS_POSTGRES_PASSWORD='…' bd list` | One-off / CI step. | Single invocation. |
| `PGPASSWORD='…' bd list` | If you already use libpq's standard env var. | Honored by pgx (the driver bd uses). |
| `~/.pgpass` file (libpq standard) | Per-host credentials, multi-database setups. | File-based; pgx honors `.pgpass` automatically. |
| Peer / IAM auth (`pg_hba.conf`) | Managed pg (RDS IAM, GCP Cloud SQL IAM, unix-socket peer auth). | Server-side; bd connects without a password and pg authenticates the OS / IAM identity instead. |

**Do not edit `pg_hba.conf` to `trust`** unless you're on an
isolated dev machine. Trust auth disables password checking
for the entire host/database — including future tools you
haven't installed yet.

### Verifying it works

```bash
export BEADS_POSTGRES_PASSWORD='mypassword'
bd list           # should print "No issues found." (not an auth error)
bd doctor         # reports backend health
```

If `bd list` still fails with `28P01`, the password value is
wrong for that user. If it fails with `connection refused`,
the pg server is not reachable at the host/port in the DSN —
see [Troubleshooting](#troubleshooting).

---

## Backup and Restore

`bd backup` does not support the Postgres backend. Use pg-native
tooling.

```bash
# Backup: custom format (-F c), portable across pg versions
pg_dump \
  -h db.example.com \
  -U bduser \
  -F c \
  -f beads.dump \
  beads_proj

# Restore into a fresh database
createdb -O bduser beads_restored
pg_restore \
  -h db.example.com \
  -U bduser \
  -d beads_restored \
  beads.dump

# Point bd at the restored database
bd init --backend=postgres \
  --dsn='postgres://bduser@db.example.com/beads_restored'
export BEADS_POSTGRES_PASSWORD='mypassword'
bd list
```

### Application-level export (`bd export`)

`bd export -o issues.jsonl` works on the Postgres backend the same way
it works on Dolt — it writes a portable JSONL dump of all issues,
labels, and dependencies. Use it for a lightweight pre-maintenance
snapshot or to move data between projects. Note that `bd export` does
not preserve audit history (events table rows); for a full data
backup including audit records use `pg_dump`.

---

## Migration from Dolt

To migrate an existing Dolt-backed bd workspace to Postgres:

```bash
# From the project directory currently using Dolt:
bd migrate --to=postgres \
  --dsn='postgres://bduser:mypassword@db.example.com/beads_proj'
```

Before migrating, take a safety export of your current data:

```bash
bd export -o pre-migrate.jsonl
```

The `--to=postgres` path is implemented in `handleCrossBackendMigrate`
(`cmd/bd/migrate.go:287`).

**What carries to Postgres:**

- Issues and wisps
- Dependencies (all four types)
- Labels and comments
- Config keys (issue prefix, custom statuses, custom types)
- Issue counters and snapshots

**What does NOT carry:**

- **Dolt commit history.** The Dolt backend stores every write as a
  commit with a hash; Postgres has no equivalent. The events table
  is populated going forward from the point of migration, not
  backfilled from Dolt history.
- **Audit-trail events from before migration.** The `--include-events`
  flag exists as a v1 placeholder; passing it returns an error
  (`migration: feature not implemented in v1`). Audit copying ships
  post-v1 (see [AUDIT_TRAIL_POSTGRES.md](AUDIT_TRAIL_POSTGRES.md)).

**Reverse direction (Postgres to Dolt)** is not supported in v1. If
you want to evaluate Postgres and keep the option to go back, use
`bd export -o snapshot.jsonl` on the Postgres instance and
`bd import` on a fresh Dolt workspace.

---

## Operational Gotchas

### Connection pool sizing

bd's default pgx pool size is 10 connections (`pool_max_conns`).
For concurrent-agent deployments where many `bd` processes run in
parallel, raise this in the DSN query string:

```
postgres://bduser@db.example.com/beads_proj?pool_max_conns=50
```

Keep the value below the pg server's `max_connections` setting,
shared across all clients (agents, human users, monitoring). A
common pg default is `max_connections=100`; if bd is not the only
consumer, stay well under that ceiling.

### Schema upgrades

bd runs schema migrations automatically on first connect. There is
no manual `bd migrate --schema` step. When you upgrade bd, the next
`bd` invocation (any command) applies any new migration files in
`internal/storage/postgres/migrations/`. If a future migration
requires manual intervention, the release notes will say so.

### Sharing one Postgres database across multiple rigs

Multiple bd instances can share one Postgres database — this is the
primary motivation for the pg backend. Each rig runs `bd init
--backend=postgres --dsn=...` pointing at the same database.
`bd init` detects an existing schema and adopts the database's
`_project_id` instead of generating a new one.

Issue IDs are globally unique within the pg database (using
shared atomic counters). Issue prefixes are per-rig, stored in
`.beads/metadata.json` and the rig's local `config` table — two
rigs on the same pg database may have different prefixes (e.g.,
`be-` vs `bd-`). Issue IDs do not collide across rigs.

### Audit trail status

The `events` and `wisp_events` tables exist in the pg schema and
are populated on every write. `bd_commits` grouping (the equivalent
of Dolt commit messages for grouped operations) is deferred to v2.
See [AUDIT_TRAIL_POSTGRES.md](AUDIT_TRAIL_POSTGRES.md) for the
implementation strategy.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `SQLSTATE 28P01: password authentication failed` | `BEADS_POSTGRES_PASSWORD` is unset, wrong, or does not match the `pg_hba.conf` auth method | See [Authentication: how the password flows](#authentication-how-the-password-flows) |
| `connection refused` | Postgres not running, wrong host/port, or firewall blocking the connection | Run `pg_isready -h <host> -p <port>`; check pg server logs |
| `database "beads_proj" does not exist` | The database was not created before `bd init` (step 2 of Quick Start) | `createdb -O bduser beads_proj`, then retry `bd init` |
| `permission denied for schema public` | Postgres 15+ revoked default schema `CREATE` privileges from non-superusers | `GRANT CREATE ON SCHEMA public TO bduser` |
| `ERROR: relation "issues" already exists` on `bd init` | Re-running `bd init` against a database that already has bd tables | `bd init` is idempotent for re-runs against the same project; use a fresh database to start over, or run `bd doctor` to inspect existing state |
| `SSL connection required by server` | The pg server requires TLS but the DSN specifies `sslmode=disable` | Change to `sslmode=require` or stronger; supply a CA cert via `sslrootcert=<path>` in the DSN if needed |

For errors not specific to the Postgres backend, see
[TROUBLESHOOTING.md](TROUBLESHOOTING.md).

---

## Configuration Reference

### `metadata.json` fields

The file lives at `.beads/metadata.json` in the project directory.
Fields used by the Postgres backend:

| Field | Value | Notes |
|---|---|---|
| `backend` | `"postgres"` | Tells bd which storage driver to load. |
| `postgres_dsn` | `"postgres://user@host/db"` | Password-stripped URI. bd injects `BEADS_POSTGRES_PASSWORD` at runtime before connecting. |
| `_project_id` | UUID string | Set by `bd init`. Shared across all rigs that point at the same pg database; lets bd detect whether it is the first initializer or a later joiner. |

### Environment variables

| Variable | Purpose |
|---|---|
| `BEADS_POSTGRES_PASSWORD` | Password injected into the stripped DSN at runtime. Set this before any `bd` command. |
| `PGPASSWORD` | Standard libpq password variable. Honored by the pgx driver bd uses; alternative to `BEADS_POSTGRES_PASSWORD`. |
| `PGSSLMODE` | Sets `sslmode` at the environment level. Overrides any `sslmode` in the DSN. |
| `PGSSLROOTCERT` | Path to a CA certificate file for `verify-ca` / `verify-full` SSL modes. |

### DSN query parameters

These parameters are accepted in the DSN URI query string
(`postgres://…?key=value&key=value`):

| Parameter | Default | Notes |
|---|---|---|
| `sslmode` | `prefer` | TLS mode (see [Connection Strings](#connection-strings-dsn-forms)). |
| `pool_max_conns` | `10` | Maximum connections in the pgx pool. Raise this for concurrent-agent setups; keep below the pg server's `max_connections` ceiling shared across all clients. |
| `pool_min_conns` | `0` | Minimum connections held open. Set to a small positive value if you want persistent warm connections. |
| `application_name` | _(unset)_ | Shown in `pg_stat_activity`. Recommended: `application_name=bd` for visibility when sharing a pg server with other tools. |

---

## See Also

- [DOLT-BACKEND.md](DOLT-BACKEND.md) — the Dolt backend reference,
  including features the Postgres backend does not offer (branching,
  time travel, `bd backup`, DoltHub federation).
- [AUDIT_TRAIL_POSTGRES.md](AUDIT_TRAIL_POSTGRES.md) — the
  events-table and `bd_commits` grouping strategy for Postgres's
  audit trail.
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — general bd
  troubleshooting, not Postgres-specific.
- [CONFIG.md](CONFIG.md) — global bd configuration (sync modes,
  federation, labels, custom types).
- [pgx documentation](https://pkg.go.dev/github.com/jackc/pgx/v5) —
  the underlying Postgres driver; reference for advanced DSN
  parameters and pool tuning.

<!-- B1: §2 (pg vs Dolt) + §6 (Auth) — assembled from design §2 and §3 by builder be-c9qj.1 -->

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
  [Audit Trail](#audit-trail) below). `bd history`, `bd diff`,
  and `bd restore --as-of=<commit>` work on Dolt but are reduced
  to event-log queries on pg. Full
  commit-message grouping is deferred to a post-v1 release
  (see [AUDIT_TRAIL_POSTGRES.md](AUDIT_TRAIL_POSTGRES.md)).
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

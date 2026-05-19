<!-- B4: §8 (Migration) + §9 (Operational Gotchas) + §10 (Troubleshooting) + §12 (See Also) — written from design outlines by builder be-c9qj.4 -->

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

**Reverse direction (Postgres → Dolt)** is not supported in v1. If
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

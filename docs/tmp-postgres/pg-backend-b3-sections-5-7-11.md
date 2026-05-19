<!-- B3: §5 (DSN forms) + §7 (Backup/Restore) + §11 (Config Reference) — written from design outlines by builder be-c9qj.3 -->

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

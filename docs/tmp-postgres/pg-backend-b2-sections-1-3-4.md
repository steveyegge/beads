<!-- B2: §1 (Overview) + §3 (Prerequisites) + §4 (Quick Start) — written from design outlines by builder be-c9qj.2 -->

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

# RFC: UUID Primary Keys for Federation-Safe Events

**Status**: Proposed
**Date**: 2026-03-13
**Branch**: `uuid-primary-keys`

## Problem

Dolt's `AUTO_INCREMENT` counter is **per-server-instance** with no cross-clone
reconciliation. When multiple Dolt clones independently write to the same table
and then sync via `dolt_push`/`dolt_pull`, INSERT operations fail with:

```
Error 1062: duplicate primary key given: [4144]
```

This is not a bug in Dolt — it is [documented behavior][dolthub-uuid-blog]:

> "Dolt branches and clones do not play well with AUTO_INCREMENT primary keys."

Six tables in beads use `BIGINT AUTO_INCREMENT PRIMARY KEY` and are all
vulnerable to this collision in any multi-clone deployment.

### How it happens

```
1. Clone A inserts events → gets IDs 4183, 4184, 4185...
2. Sync: dolt_push from Clone A → Clone B (rows arrive on Clone B)
3. Clone B's AUTO_INCREMENT counter does NOT advance past the pushed rows
4. Client writes to Clone B → AUTO_INCREMENT assigns 4183 → COLLISION
```

### Observed state at time of discovery

In a 3-node federation (one primary, one satellite, one thin-client), the
event counters had diverged significantly:

| Node | Role | MAX(id) | COUNT(*) | AUTO_INCREMENT |
|------|------|---------|----------|----------------|
| Node A | thin client | 4090 | 2477 | 4091 |
| Node B | primary | 4198 | 3104 | 4199 |
| Node C | satellite | 4217 | 3123 | 4218 |

Rows pushed from the satellite to the primary occupied ID ranges that the
primary's counter didn't know about. The thin client was even further behind.

### Impact

- **ALL write operations blocked**: `bd create`, `bd update`, `bd close` fail
  on any node whose counter is behind
- Every sync cycle re-introduces the gap — not a one-time fix
- Agents writing to one clone create events that collide on another clone
- Operations that create events (archival, comments, status changes) all fail
- The only workaround was "burning through" — retrying failed INSERTs ~24 times
  to advance the counter past the gap, then repeating after every sync

See [federation-uuid-evidence.md](federation-uuid-evidence.md) for the full
incident report.

### Prior mitigations (insufficient)

**beads#2133** added `ALTER TABLE <tbl> AUTO_INCREMENT = MAX(id)+1` after every
`DOLT_PULL` in `bd`'s pull code path (`resetAutoIncrements`). This works when
`bd` does the pull, but fails when:

- External sync scripts use raw `dolt_pull`/`dolt_push` (e.g., cron-based
  satellite sync via Docker exec)
- Other tools write independently of `bd`
- Two nodes write concurrently between syncs
- Even within a single server, [dolthub/dolt#7702] documents AUTO_INCREMENT
  race conditions in concurrent transactions

The band-aid treats symptoms, not the root cause.

## Solution

Replace `BIGINT AUTO_INCREMENT PRIMARY KEY` with `CHAR(36) NOT NULL PRIMARY KEY
DEFAULT (UUID())` on all six affected tables.

### Why UUIDs

- **DoltHub's official recommendation** for multi-clone scenarios
  ([blog post][dolthub-uuid-blog])
- Collision-free across any number of independent clones
- `DEFAULT(UUID())` lets Dolt generate IDs server-side (no application changes
  for most INSERT paths)
- `last_insert_uuid()` available since Dolt 2024 for callers that need the
  generated value

### Why UUID v7

Where the application generates IDs explicitly (e.g., `ImportIssueComment`),
we use [UUID v7][rfc9562] via `github.com/google/uuid`:

- **Time-sorted**: embeds a Unix millisecond timestamp in the high bits
- Preserves chronological ordering within UUIDs (`ORDER BY id` ≈ creation order)
- Used as a tiebreaker in queries: `ORDER BY created_at ASC, id ASC`

### Tables affected

| Table | Writes from |
|-------|-------------|
| `events` | Every create, update, close, reopen, label, rename |
| `comments` | `bd comment` |
| `issue_snapshots` | Compaction |
| `compaction_snapshots` | Compaction |
| `wisp_events` | Wisp lifecycle events |
| `wisp_comments` | Wisp annotations |

Tables **NOT** changed: `issues`, `wisps`, `dependencies`, `labels`, `config`,
`metadata`, etc. — these already use VARCHAR or composite PKs that are
federation-safe.

## Schema change

### Before

```sql
CREATE TABLE events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    ...
);
```

### After

```sql
CREATE TABLE events (
    id CHAR(36) NOT NULL PRIMARY KEY DEFAULT (UUID()),
    ...
);
```

Schema version bumped from `6` → `7`.

## Migration strategy

Dolt cannot `ALTER COLUMN` to change a PK's type in place. The migration
(`migrations/010_uuid_primary_keys.go`) uses a 4-step add-column / backfill /
drop / rename pattern:

### For each of the 6 tables:

1. **Idempotency check**: Query `information_schema.COLUMNS` — if `id` is
   already `char(36)`, skip. If the table doesn't exist, skip (fresh schema
   will create it with UUID PKs).

2. **Add UUID column**:
   ```sql
   ALTER TABLE <table> ADD COLUMN uuid_id CHAR(36) NOT NULL DEFAULT (UUID())
   ```

3. **Backfill existing rows**:
   ```sql
   UPDATE <table> SET uuid_id = UUID() WHERE uuid_id = '' OR uuid_id IS NULL
   ```

4. **Drop old PK** (3 sub-steps — Dolt requires removing AUTO_INCREMENT before
   DROP PRIMARY KEY):
   ```sql
   ALTER TABLE <table> MODIFY id BIGINT NOT NULL    -- remove AUTO_INCREMENT
   ALTER TABLE <table> DROP PRIMARY KEY
   ALTER TABLE <table> DROP COLUMN id
   ```

5. **Rename and promote**:
   ```sql
   ALTER TABLE <table> RENAME COLUMN uuid_id TO id
   ALTER TABLE <table> ADD PRIMARY KEY (id)
   ```

6. **Commit**: `DOLT_COMMIT('-Am', 'migration: UUID primary keys for ...')`

### Dolt DDL quirk

`ALTER TABLE ... DROP PRIMARY KEY` fails on Dolt when the column has
`AUTO_INCREMENT`. The workaround is to `MODIFY` the column to remove
`AUTO_INCREMENT` first (step 4a above). This is not documented in Dolt's
migration docs.

## Breaking changes

### 1. `GetAllEventsSince` signature

```go
// Old — used integer event ID as monotonic cursor
GetAllEventsSince(ctx context.Context, sinceID int64) ([]*types.Event, error)

// New — uses timestamp (UUIDs are not sequential across clones)
GetAllEventsSince(ctx context.Context, since time.Time) ([]*types.Event, error)
```

The query changed from `WHERE id > ?` to `WHERE created_at > ?`. With UUIDs,
integer high-water marks are meaningless. Timestamps are the natural ordering
for events.

### 2. Type changes

```go
// types.Event.ID: int64 → string
// types.Comment.ID: int64 → string
```

Any code comparing event/comment IDs as integers must be updated.

### 3. `resetAutoIncrements` removed

The post-pull AUTO_INCREMENT fixup band-aid is deleted. With UUID PKs, there
are no auto-increment counters to reset. Callers that depended on this method
(it was called in 3 places within `Pull()`) no longer need it.

### 4. Credential CLI routing removed

The `shouldUseCLIForCredentials` / `shouldUseCLIForPeerCredentials` guards and
their CLI subprocess routing paths were removed from federation operations.
This simplifies the push/pull code paths.

### 5. `CommitWithConfig` removed

The separate `CommitWithConfig` method was replaced by defense-in-depth logic
in `Commit()` that snapshots and verifies `issue_prefix` integrity. Callers
use `Commit()` directly.

## Code changes summary

```
70 files changed, +1,182 / -5,831 lines (net -4,649 lines)
```

Key files:

| File | Change |
|------|--------|
| `internal/storage/dolt/schema.go` | Schema version 6→7, UUID PK definitions |
| `internal/storage/dolt/migrations/010_uuid_primary_keys.go` | New migration |
| `internal/storage/dolt/events.go` | `GetAllEventsSince(time.Time)`, UUID comment IDs |
| `internal/storage/dolt/store.go` | Remove `resetAutoIncrements`, simplify `Commit()` |
| `internal/storage/dolt/wisps.go` | Inline `createWisp`, UUID PKs |
| `internal/storage/dolt/federation.go` | Remove credential CLI routing |
| `internal/types/types.go` | `Event.ID`, `Comment.ID` → `string` |
| `internal/storage/dolt/issues.go` | Inline `CreateIssue` from deleted `issueops` |
| `internal/storage/dolt/adaptive_length.go` | New: birthday-paradox ID length scaling |

### Deleted packages

| Package | Reason |
|---------|--------|
| `internal/storage/issueops` | Inlined into `DoltStore` directly |
| `internal/storage/embeddeddolt` (6 files) | Simplified to stubs; server-mode is primary |
| `cmd/bd/backup_export_git.go` | Git export backup removed |
| `cmd/bd/doctor/tracked_runtime.go` | Tracked runtime doctor check removed |

## Rollback plan

1. **Schema rollback**: Migration 010 is idempotent and checks column types
   before acting. Rolling back to a pre-migration binary will NOT re-run the
   migration. However, the old binary cannot read UUID `id` values as `int64` —
   it will fail at the Go type level.

2. **Data rollback**: Dolt supports `dolt_reset('--hard', '<commit-hash>')` to
   revert to any prior commit. The migration creates a Dolt commit, so rolling
   back to the commit before migration restores the old schema and data.

3. **Binary rollback**: Deploy the previous `bd` binary. It will see `CHAR(36)`
   columns and fail on type assertions. A full rollback requires both binary
   AND data reversion.

4. **Forward-only recommendation**: Given the fundamental architectural flaw in
   AUTO_INCREMENT + federation, forward-only migration is strongly recommended.
   The rollback path exists for emergencies but should not be exercised in
   production.

## Testing

- All existing tests pass with the UUID schema
- Migration tested against 18 live databases across 3 nodes
- E2E federation test: create on Clone A → sync → write on Clone B → no
  Error 1062
- Comment ordering verified: `ORDER BY created_at ASC, id ASC` provides
  deterministic output

## References

- [DoltHub: UUID Keys blog post][dolthub-uuid-blog]
- [dolthub/dolt#7702]: AUTO_INCREMENT race condition in concurrent transactions
- [beads#2133]: Prior AUTO_INCREMENT reset band-aid (closed, insufficient)
- [RFC 9562][rfc9562]: UUID v7 specification
- [federation-uuid-evidence.md](federation-uuid-evidence.md): Incident report

[dolthub-uuid-blog]: https://www.dolthub.com/blog/2023-10-27-uuid-keys/
[rfc9562]: https://www.rfc-editor.org/rfc/rfc9562
[dolthub/dolt#7702]: https://github.com/dolthub/dolt/issues/7702
[beads#2133]: https://github.com/steveyegge/beads/pull/2133

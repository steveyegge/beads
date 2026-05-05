# Audit-trail strategy for the Postgres backend

**Bead:** be-l7t.7 (architect ADR) / be-6fk.7 (builder bead)
**Memo lives at:** `docs/AUDIT_TRAIL_POSTGRES.md` — referenced from
`bd migrate` stderr note (P5 §7) and from `bd init --backend=postgres --help`
(P4 §2.2).
**Status:** writeup only. v1 ships **without** the implementation.

---

## 0. Recommendation summary (TL;DR)

**Recommended approach: application-level append-only event log + new `bd_commits` table.**

Concretely:
1. **Extend the existing `events` / `wisp_events` tables.** PG already has
   these (P3 §3, §A) with byte-for-byte parity with Dolt's schema
   (`event_type`, `actor`, `old_value`, `new_value`, `comment`,
   `created_at`). The PG storage methods that mutate issues
   (`CreateIssue`, `UpdateIssue`, `CloseIssue`, `AddDependency`,
   `AddLabel`, `AddIssueComment`, …) write a row to `events` from inside
   the same transaction that mutated the issue. **This is what Dolt does
   today at the application layer; PG inherits the pattern.**
2. **Add a `bd_commits` table** that captures the `commitMsg` parameter
   passed to `RunInTransaction(ctx, commitMsg, fn)`. One row per
   non-empty `commitMsg`, joined to the events emitted within that
   transaction by a generated `commit_id`. This recovers the Dolt-style
   "commit message + grouped diff" semantic.
3. **Reject the alternatives** — triggers, logical replication, CDC —
   for the reasons enumerated in §4.

The key invariant: **the audit trail is bd's responsibility, not
Postgres's**. PG provides the storage; bd provides the events. This
matches the Storage Boundary direction set in `AGENTS.md` (P1 §1) — bd
doesn't reach into PG-engine internals.

**v1 ships:**
- The `events` / `wisp_events` tables exist (P3).
- The PG storage methods write to them on mutations (P3 §6 capability set).
- `bd_commits` is **not** implemented in v1.
- `bd migrate --to=postgres` does NOT carry events (P5 §7).

**Future work (P7+1, post-v1):**
- Land `bd_commits` in a new migration `0002_audit_commits.up.sql`.
- Land the `--include-events` flag on `bd migrate` (the placeholder is
  reserved at P5 §2.1).
- Land any HistoryViewer-equivalent helpers on PG.

---

## 1. The problem (FR-9 framing)

Dolt provides a **native event/commit-history audit trail**:

- Every transactional change creates a Dolt commit (hash, author,
  timestamp, optional message).
- `dolt_log`, `dolt_diff_<table>`, `dolt_history_<table>` system tables
  let bd's `HistoryViewer` capability execute time-travel queries.
- `bd history <id>`, `bd diff`, `bd restore <id> --as-of=<commit>` all
  lean on this.

Postgres has **no commit-graph analog**:

- PG's WAL is a write-ahead log, not a versioned history. WAL is replayed
  for crash recovery, not for time-travel queries.
- PG has no `AS OF` clause for SELECT (timetravel is a third-party
  extension; not standard).
- PG does have row-level history when extensions like `temporal_tables`
  or `pgaudit` are installed — both require ops-tier setup and don't
  ship in the standard `postgres:14-alpine` image.

bd's existing audit surface — the `events` table — is **already** at the
application layer. Each row in `events` is an `*types.Event`:

```go
// internal/types/types.go:958-986
type Event struct {
    ID        string    // UUID
    IssueID   string    // FK to issues.id
    EventType EventType // "created", "updated", "status_changed", "commented",
                        // "closed", "reopened", "dependency_added", "dependency_removed",
                        // "label_added", "label_removed", "compacted"
    Actor     string
    OldValue  *string
    NewValue  *string
    Comment   *string
    CreatedAt time.Time
}
```

The `Storage` interface (P1 §3) exposes:

```go
// internal/storage/storage.go:81-82
GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error)
GetAllEventsSince(ctx context.Context, since time.Time) ([]*types.Event, error)
```

Consumers (`bd export`, `bd export-auto`, `bd history` for some queries)
read from these. The PG impl of Storage already satisfies these methods
(per P1's MUST capability list: `Storage` core is a MUST, and
`GetEvents`/`GetAllEventsSince` are on it).

**What's missing on PG vs Dolt:**

| Dolt provides | PG provides today | PG could provide |
|---|---|---|
| Per-row mutation audit (issues table) | events table (one row per mutation) | same — already at parity |
| Commit-grouping ("these 5 events happened together") | the `events` table has `created_at` but no transaction key | a `bd_commits` table |
| Commit message (the `commitMsg` of `RunInTransaction`) | dropped (P3 §5.4) | a `bd_commits.message` column |
| Time-travel queries (`AS OF`) | none | non-trivial — out of v1+future scope |
| Diff between two arbitrary points (`bd diff`) | none | follow-on if `bd_commits` lands |

The memo's question: which mechanism delivers the "commit grouping +
commit message" gap?

---

## 2. Strategies evaluated

The bead asks for at-minimum-four strategies. They are evaluated below:

- **A.** Triggers + `bd_events` mirror table.
- **B.** Logical replication.
- **C.** CDC (change data capture) / change tables.
- **D.** Application-level append-only event log written from the storage layer.

### 2.1 Strategy A — triggers + `bd_events` mirror table

**Mechanism:** PG triggers fire `BEFORE` or `AFTER` `INSERT/UPDATE/DELETE`
on `issues`, `dependencies`, `labels`, `comments`. The trigger function
inspects `OLD.*` and `NEW.*` and inserts an audit row into a new
`bd_events` table that mirrors the column shape of the existing `events`
table.

```sql
CREATE TABLE IF NOT EXISTS bd_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    table_name VARCHAR(64) NOT NULL,
    issue_id VARCHAR(255),
    event_type VARCHAR(32) NOT NULL,
    actor VARCHAR(255),
    old_row JSONB,
    new_row JSONB,
    txid BIGINT NOT NULL DEFAULT txid_current(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION bd_audit_issues() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO bd_events (table_name, issue_id, event_type, old_row, new_row)
    VALUES ('issues', COALESCE(NEW.id, OLD.id), TG_OP, row_to_json(OLD)::jsonb, row_to_json(NEW)::jsonb);
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER issues_audit
    AFTER INSERT OR UPDATE OR DELETE ON issues
    FOR EACH ROW EXECUTE FUNCTION bd_audit_issues();
```

**Pros:**
- Comprehensive: catches every row mutation, including ones from outside
  bd's Go code (e.g., `bd sql` — the deferred RawDBAccessor — or the user
  running `psql` directly).
- Decoupled from application code; survives bd-version skew.
- `txid_current()` provides natural transaction grouping.

**Cons:**
- **Diverges from the Dolt impl.** Dolt's `events` table is written at
  the application layer (the storage methods write the row when they
  understand the high-level event type — "status_changed",
  "label_added"). A trigger sees only column-level diffs and would
  produce events with a different shape (full row JSON, not the
  `OldValue`/`NewValue` field-level diff bd uses today).
- **Event-type fidelity loss.** A trigger sees
  `UPDATE issues SET status='closed', closed_at=NOW() WHERE id='X'` and
  writes a single audit row marked `UPDATE`. The application sees the
  same call as `CloseIssue(X, reason, actor, session)` — emitting a
  logical `EventClosed` event with `Reason` in `Comment`,
  `OldValue=open`, `NewValue=closed`. The trigger can't recover the
  high-level event type without parsing column diffs (fragile).
- **Performance.** Triggers add latency to every write. PG's plpgsql is
  fast but not free; ~10-50µs per row at the issue scale.
- **Debugging.** Trigger errors surface as opaque PG errors; debugging
  requires DBA-tier knowledge. Application-level logging is better for
  bd's developer audience.
- **Test coverage difficulty.** Triggers are hard to unit-test;
  integration tests must hit a real PG.
- **Mirror table redundancy.** Today's `events` table already exists.
  Adding `bd_events` doubles the storage cost and creates a consistency
  burden ("which is the source of truth?").

**Verdict:** Rejected. The high-level event type is bd's domain
knowledge; expressing it via triggers loses fidelity and adds an
architectural seam.

### 2.2 Strategy B — logical replication

**Mechanism:** PG's logical replication
(`pg_create_logical_replication_slot('bd_audit', 'pgoutput')`) streams
every committed change from a WAL slot to a downstream consumer. The
consumer (a separate process) consumes the stream and writes an audit
log to wherever it likes — a file, another database, a message queue.

**Pros:**
- Authoritative: every change captured, including out-of-band ones.
- Transactionally consistent: the consumer sees commits as units.
- Standard PG feature; no extension required (built-in since PG10).

**Cons:**
- **Out of bd's process scope.** bd is a single-binary CLI; logical
  replication needs a long-running consumer process. Adding a sidecar
  daemon to every `bd init --backend=postgres` install is a step-change
  in operational complexity.
- **DBA-tier permissions required.** `CREATE PUBLICATION` and
  `CREATE SUBSCRIPTION` need superuser / replication role. bd's
  user-tier role is unlikely to have it.
- **Asynchronous.** The audit trail lags writes; queries against it
  return slightly-stale data. bd's `events` semantics are synchronous
  (the row is visible inside the transaction's commit).
- **No commit-message capture.** WAL records carry transactional
  grouping but not bd's `commitMsg`. Solving this still requires
  application-level addition.
- **Operationally fragile.** Replication slots accumulate WAL segments
  if the consumer falls behind; a stuck consumer can fill the disk.
  Production readiness needs monitoring.

**Verdict:** Rejected for v1+near-future. logical replication is the
right answer for *streaming bd events to an external observability
platform*, not for bd's local audit-trail need. It's an opt-in
deployment topology, not a built-in feature.

### 2.3 Strategy C — CDC / change tables (Debezium-class)

**Mechanism:** A change-data-capture tool reads PG's WAL (typically via
`wal2json` or `pgoutput` plugins) and produces a structured event
stream. Debezium is the reference impl. The stream is consumed by
Kafka/RabbitMQ/ElasticSearch/whatever the user has stood up.

**Pros:**
- Industry-standard pattern for cross-system event sourcing.
- Tool ecosystem is mature.
- Supports schema evolution out of the box.

**Cons:**
- **External tooling.** Debezium needs Kafka. Kafka needs Zookeeper or
  KRaft. The dependency tree is a multi-VM operation.
- **Same fundamental issues as B** (out-of-process, async, DBA-tier, no
  commitMsg capture).
- **Massive overkill for bd.** bd is a developer-machine CLI; `bd init`
  should not require provisioning a Kafka cluster.

**Verdict:** Rejected. CDC is the answer to "stream bd events to your
enterprise observability stack" — that's an integration concern, not
bd's audit-trail concern.

### 2.4 Strategy D — application-level append-only event log

**Mechanism:** bd's storage methods write to the `events` table (and
`wisp_events` for ephemeral) from inside the same transaction that
mutates the issue. The events table already exists (P3 §A); the writes
already exist on the Dolt side. The PG impl writes the same way.

For commit-level grouping (the gap vs. Dolt), add a `bd_commits` table:

```sql
CREATE TABLE IF NOT EXISTS bd_commits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor VARCHAR(255) NOT NULL,
    message TEXT,
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    committed_at TIMESTAMP
);

ALTER TABLE events ADD COLUMN commit_id UUID REFERENCES bd_commits(id);
ALTER TABLE wisp_events ADD COLUMN commit_id UUID;  -- no FK; wisp tables use loose relations
```

`RunInTransaction(ctx, commitMsg, fn)` on PG:
1. INSERT INTO bd_commits (actor, message, started_at) VALUES (current_actor, commitMsg, NOW()) RETURNING id;
2. Stash the returned `commit_id` in the transaction context.
3. Every event row inserted within the transaction sets `commit_id = <stashed>`.
4. On COMMIT: UPDATE bd_commits SET committed_at = NOW() WHERE id = <stashed>.

`bd_commits.message` captures the previously-dropped `commitMsg`
(P1 §6, P3 §5.4). `bd history` and friends gain a new query path:
enumerate commits, then events per commit.

**Pros:**
- **Symmetric with Dolt's behavior.** Dolt writes the same `events`
  rows; the only delta is the commit-grouping bookkeeping that Dolt
  gets for free (Dolt commits are commits).
- **In-transaction.** `commit_id` and the event rows commit atomically
  — no replication lag, no consistency gap.
- **No new ops.** No daemon, no replication slot, no extension.
- **Debuggable.** All audit logic is in Go; standard tracing applies.
- **Existing `events` shape preserved.** `EventType`, `Actor`,
  `OldValue`, `NewValue`, `Comment`, `CreatedAt` — same fields, same
  semantics. Consumers (`bd export`, `bd history`) work unchanged.
- **`bd migrate --include-events`** is a clean feature: copy rows from
  Dolt's `events` to PG's `events`, generating a single `bd_commits`
  row per Dolt commit (best effort; Dolt commits without bd events are
  skipped).

**Cons:**
- **Out-of-band writes are not audited.** Someone who runs
  `psql -c "UPDATE issues SET ..."` outside bd does not produce
  `events` rows. Triggers (Strategy A) would. The mitigation: bd
  documents that the `events` table reflects bd-mediated changes only;
  out-of-band writes are at the user's risk. Same as today on Dolt.
- **`bd_commits` adds a new table** — but only one, with a small row
  count (one per `RunInTransaction`). Storage cost negligible.
- **Migration:** existing PG instances (v1 ships without `bd_commits`)
  need `0002_audit_commits.up.sql` to add the table and the `commit_id`
  column. Idempotent; runs at next `bd` connect via P3's migration
  runner.

**Verdict:** **Recommended.** Combines today's pattern (which works on
Dolt) with the small additional table that recovers commit-grouping. No
new ops surface. Same Storage interface. Forward-additive.

### 2.5 Side note: bd's existing `interactions.jsonl`

bd already has a *separate* audit log for AI/agent interactions:
`internal/audit/audit.go` writes `.beads/interactions.jsonl` (file-based,
not in any storage backend). This is orthogonal — it's a developer-time
log of LLM calls, not a database audit trail. It works identically on
both backends; it's not subject to FR-9.

The audit-trail memo is about **the database-level events** that
`events` and `bd_history`-style queries cover. interactions.jsonl is
unaffected.

---

## 3. The recommended approach in detail

### 3.1 What v1 ships (already)

- `events` and `wisp_events` tables in PG schema (P3 §A).
- PG storage methods (`CreateIssue`, `UpdateIssue`, `CloseIssue`,
  `AddDependency`, `RemoveDependency`, `AddLabel`, `RemoveLabel`,
  `AddIssueComment`, `ReopenIssue`, `UpdateIssueType`) write event rows
  from inside their own transactions. **Same pattern as the Dolt impl.**
- `GetEvents(ctx, issueID, limit)` and
  `GetAllEventsSince(ctx, since)` work on PG.
- `bd export` (which doesn't include events) works identically.
- `bd migrate --to=postgres` drops events with a stderr note (P5 §7).

### 3.2 What the future implementation adds

When P7+1 (a new bead post-v1) lands:

1. **Migration `internal/storage/postgres/migrations/0002_audit_commits.up.sql`:**

   ```sql
   CREATE TABLE IF NOT EXISTS bd_commits (
       id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
       actor        VARCHAR(255) NOT NULL DEFAULT '',
       message      TEXT,
       started_at   TIMESTAMP NOT NULL DEFAULT NOW(),
       committed_at TIMESTAMP
   );
   CREATE INDEX IF NOT EXISTS idx_bd_commits_started_at ON bd_commits (started_at);

   ALTER TABLE events     ADD COLUMN IF NOT EXISTS commit_id UUID;
   ALTER TABLE wisp_events ADD COLUMN IF NOT EXISTS commit_id UUID;

   CREATE INDEX IF NOT EXISTS idx_events_commit_id     ON events     (commit_id);
   CREATE INDEX IF NOT EXISTS idx_wisp_events_commit_id ON wisp_events (commit_id);
   ```

   No FK on `wisp_events.commit_id` because wisp tables historically have
   no FKs (parallel to the no-FK-on-wisp-deps decision in P3 §11).

2. **PG storage method change.** `RunInTransaction` in
   `internal/storage/postgres/transaction.go` becomes:

   ```go
   func (s *PostgresStore) RunInTransaction(ctx context.Context, commitMsg string, fn func(tx storage.Transaction) error) error {
       pgxTx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
       if err != nil { return s.wrapErr("begin tx", err) }

       actor := actorFromContext(ctx)
       commitID, err := s.insertCommitRow(ctx, pgxTx, actor, commitMsg)
       if err != nil { _ = pgxTx.Rollback(ctx); return s.wrapErr("create commit row", err) }

       pgTx := &pgTransaction{tx: pgxTx, store: s, commitID: commitID}
       if err := fn(pgTx); err != nil {
           _ = pgxTx.Rollback(ctx)
           return err
       }
       if err := s.markCommitCommitted(ctx, pgxTx, commitID); err != nil {
           _ = pgxTx.Rollback(ctx)
           return s.wrapErr("mark commit", err)
       }
       return pgxTx.Commit(ctx)
   }
   ```

   Every event-writing method on the `pgTransaction` includes
   `commit_id` in the INSERT.

3. **`bd migrate --include-events` flag** activates (P5 §2.1's
   reservation). When set, `bd migrate` iterates Dolt's events:
   - For each Dolt commit, INSERT INTO bd_commits
     (actor=commit.author, message=commit.message,
     started_at=commit.timestamp, committed_at=commit.timestamp) and
     capture the new commit_id.
   - For each event whose Dolt commit is the source, INSERT INTO events
     with commit_id = the new commit_id.
   - For events not associated with a Dolt commit (rare; possible for
     old bd versions), INSERT with commit_id = NULL.

4. **Storage interface extensions** (additive):

   ```go
   // CommitsViewer (new capability sub-interface). PG implements; Dolt may or may not.
   type CommitsViewer interface {
       ListCommits(ctx context.Context, since time.Time, limit int) ([]*Commit, error)
       GetCommit(ctx context.Context, id string) (*Commit, error)
       GetEventsForCommit(ctx context.Context, id string) ([]*types.Event, error)
   }

   type Commit struct {
       ID          string
       Actor       string
       Message     string
       StartedAt   time.Time
       CommittedAt time.Time // zero value if uncommitted (mid-transaction; rare to observe)
   }
   ```

5. **`bd history` and `bd diff` extensions** (out of P7 scope; future bead).

### 3.3 What this does NOT address

- **Time-travel queries** (PG `AS OF`). Out of scope; would require
  rewriting bd queries to use `bd_commits`-keyed history instead of
  `dolt_history_*`. A separate, much larger bead.
- **Diffs between arbitrary points** (`bd diff`). Same.
- **The `HistoryViewer` capability on PG.** Stays Dolt-only by P1's
  matrix. The recommended approach gives a *different* shape of history
  (event log) than HistoryViewer (table snapshots).
- **Federation / replication.** RemoteStore is Dolt-only by P1.

The recommendation is **commit grouping**, not full HistoryViewer
parity. That distinction is intentional: HistoryViewer is a Dolt strength
(cell-level merge, time-travel SELECT) that PG can't match without an
extension. The audit trail is bridgeable; the time-machine isn't.

---

## 4. Why not the alternatives — direct comparison

| Property | Triggers (A) | Logical Repl (B) | CDC (C) | App-level (D) **recommended** |
|---|---|---|---|---|
| Catches out-of-band writes | yes | yes | yes | no |
| In-transaction (synchronous) | yes | no | no | yes |
| New ops surface (daemons/extensions) | none | replication slot + consumer | full Kafka stack | none |
| DBA-tier permissions required | partial | yes | yes | no |
| Captures bd's high-level EventType | no | no | no | **yes** |
| Captures `commitMsg` | no | no | no | **yes** |
| Symmetric with Dolt impl | no | no | no | **yes** |
| `bd migrate` carries events naturally | needs trigger replay | needs WAL replay | needs CDC replay | **yes (row copy)** |
| Performance overhead per write | ~10-50µs/row | bg + storage | bg + storage | ~5-10µs/row (one INSERT) |
| Test coverage in Go unit tests | hard | very hard | very hard | **easy** |

The application-level pattern wins on six dimensions: in-transaction, no
new ops, no DBA permissions, captures EventType, captures commitMsg,
symmetric with Dolt, easy tests. It loses on out-of-band capture — bd
documents this trade-off (PG users who run `psql` outside bd accept the
risk; the same is true on Dolt today via `dolt sql`).

---

## 5. Storage Boundary alignment

The Storage Boundary section in `AGENTS.md` (PR#3617, ratified
2026-05-01) reads:

> Beads talks to storage through a driver interface. Beads code should
> not reach across that boundary — no flocks, no engine introspection,
> no storage-engine-specific retry or crash-recovery logic in beads
> packages. … Roadmap target: all storage interaction lives behind the
> driver. Beads stays storage-agnostic.

**Triggers (A)** push audit semantics into the storage engine — exactly
what the boundary forbids.
**Logical replication (B)** and **CDC (C)** require ops infrastructure
outside bd — outside the boundary entirely; beyond bd's reach.
**App-level (D)** keeps audit semantics inside bd's storage layer code,
behind the `Storage` interface. The boundary is preserved.

---

## 6. Cost / benefit summary

Implementation cost (post-v1):

- **0002_audit_commits.up.sql:** ~30 lines of SQL.
- **`RunInTransaction` change in PG impl:** ~50 lines of Go.
- **Per-event-writing method (10 methods on `pgTransaction`):** ~5 lines
  each = 50 lines, mostly mechanical (add `commit_id` to the INSERT).
- **`CommitsViewer` capability impl:** ~100 lines of Go (3 methods on
  PG; placeholder NotImplementedError on Dolt initially).
- **`bd migrate --include-events`:** ~80 lines of Go (Dolt history
  iteration + INSERT).
- **Tests:** ~200 lines.

**Total: ~500 lines of Go + 30 lines of SQL.** Lands in one PR, gated by
a feature bead. Reversible (the column and table are additive; rolling
back requires only dropping `commit_id` columns and `bd_commits`).

Benefit:

- Recovers Dolt's commit-message audit semantic on PG.
- Enables `bd migrate --include-events` (the FR-9 deferred path).
- Forward path to a richer history surface if needed.

---

## 7. v1 explicit non-implementation

**Per FR-9: v1 ships without the implementation.**

What this means concretely:

- `bd_commits` table does NOT exist in `0001_initial.up.sql` (P3's
  schema).
- `events.commit_id` and `wisp_events.commit_id` columns do NOT exist.
- `RunInTransaction(ctx, commitMsg, fn)` on PG **drops** the `commitMsg`
  parameter — same as P3 §5.4 and P1 §6 stipulated.
- `bd migrate --to=postgres` does NOT carry events. The
  `--include-events` flag is reserved (P5 §2.1) but is a no-op.

What v1 DOES ship:

- The `events` and `wisp_events` tables (P3 §A) — schema-ready for the
  future addition.
- Per-mutation event row writes from PG storage methods (P3 §6
  capability set requires `Storage` core, which includes `GetEvents` /
  `GetAllEventsSince` reads — and the corresponding writes are also in
  scope as part of the mutation methods).
- This memo at `docs/AUDIT_TRAIL_POSTGRES.md`.

The bead description's exact language: "Writeup only — no
implementation. … v1 ships without the implementation." This memo is
the writeup. P7's PR commits the file at `docs/AUDIT_TRAIL_POSTGRES.md`
and **nothing else** — no schema changes, no Go code.

---

## 8. The `bd migrate` interaction (cross-reference)

Per P5 §7:

- v1 `bd migrate --to=postgres` emits a stderr note:
  `note: <N> audit-trail events not migrated; see docs/AUDIT_TRAIL_POSTGRES.md`.
- The note's link points at this memo.
- The `--include-events` flag is reserved but no-op.

When the future implementation lands:

- `--include-events` becomes meaningful: when set, `bd migrate` walks
  Dolt's commits, creates corresponding `bd_commits` rows on PG, and
  copies events with `commit_id` populated.
- Dolt commits without bd events (e.g., a `bd dolt commit -m "manual fix"`
  with no preceding `bd update`) are skipped — they have nothing to
  mirror at the events-table level.
- Events without a Dolt commit (very old bd versions, or events emitted
  outside `RunInTransaction`) get `commit_id = NULL` — they're still
  searchable by `event_type` / `created_at`, just unbound to a commit.

---

## 9. Acceptance check (against be-l7t.7)

- [x] **Memo lives under `docs/`.** → Path
  `docs/AUDIT_TRAIL_POSTGRES.md`. P7's PR commits the file.
- [x] **All four strategies named are evaluated.** → §2 (A: triggers,
  B: logical replication, C: CDC, D: application-level event log).
- [x] **A single recommendation is made and justified.** → §0, §3, §4
  (recommended: D, with `bd_commits` extension).
- [x] **The memo cites the existing `Event` type from `internal/types/`
  and the `GetEvents` / `GetAllEventsSince` methods on `Storage`.** →
  §1 (`internal/types/types.go:958-986` for the type;
  `internal/storage/storage.go:81-82` for the methods).
- [x] **States explicitly that v1 ships without the implementation.** →
  §7.
- [x] **Writeup only; no code changes.** → P7's PR commits this file at
  the suggested path; no SQL, no Go.

Constraints honored:
- [x] PR only — implementer ships under PR.
- [x] Independent of P1-P6 — this memo doesn't bind upstream phases.
  (It does cross-reference P3 §A for the existing `events` table shape,
  P5 §7 for the migrate note, and P1 §6 / P3 §5.4 for the `commitMsg`
  ignore — those are read-only references, not new requirements.)

---

## 10. Implementer's checklist for P7's PR

The PR is small:

1. Create the file at `docs/AUDIT_TRAIL_POSTGRES.md` with the contents
   of this memo (sections §0 through §10).
2. (Optional) Add a paragraph to `README.md`'s backend table referencing
   the memo.
3. (Optional) Update `bd init --backend=postgres --help`'s long text to
   point at the memo. The pointer already lives in P4's post-init note
   (§8).

No other files. No tests (memo only). PR review is limited to the memo's
accuracy and prose.

---

## 11. Guardrails

- **The recommended approach is "application-level event log + `bd_commits`".**
  A future implementer who picks A/B/C without justification needs an
  architect re-review.
- **`commitMsg` lives in `bd_commits.message`, not in `events.comment`.**
  Don't shoehorn it into the existing column.
- **The `bd_commits` table prefix is `bd_`.** Per P3 §19 guardrail:
  bd-owned bookkeeping tables use this prefix to avoid collisions with
  user tables.
- **Events without a `commit_id` are valid.** Future code must handle
  `commit_id IS NULL` as the normal case for events that predate the
  `bd_commits` migration. Don't add a NOT NULL constraint in
  `0002_audit_commits.up.sql`.
- **Out-of-band writes are documented as not-audited.** Don't add
  triggers as a defense-in-depth measure — they break the Storage
  Boundary and conflict with the application-level pattern.
- **HistoryViewer parity is NOT the goal.** This memo provides
  commit-grouping. Time-travel SELECT remains Dolt-only.
- **Logical replication / CDC are user-deployment concerns.** If a
  future bd user wants to stream events to Kafka, they configure PG's
  logical replication outside bd; bd doesn't manage it.

---

*End of P7 audit-trail strategy memo.*

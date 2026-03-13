# Evidence: Error 1062 Duplicate Primary Key Collisions

Supporting evidence for
[federation-uuid-primary-keys.md](federation-uuid-primary-keys.md).

All incidents occurred on **2026-03-13** across a 3-node Dolt federation
(thin-client → primary server, satellite server).

## Incident Timeline

### 12:22 UTC — Discovery: ALL writes blocked

**Operation**: `bd update` (closing an issue after a routine deployment)

```
Error 1062: duplicate primary key given: [4144]
```

**Impact**: ALL write operations blocked — `bd create`, `bd update`, `bd close`.
The thin client's event counter was at ~4136 while the events table already
contained IDs up to ~4167 (pushed from the satellite).

**Root cause**: `bd` relied on Dolt's `AUTO_INCREMENT` for event IDs. After
federation sync pushed rows from the satellite → primary, the primary's counter
was stale and assigned IDs that already existed.

**Workaround**: "Burning through" — repeated failed INSERT attempts incremented
the counter by 1 each time. ~24 failed attempts were needed to advance past
the gap.

While issue IDs are hash-based and federation-safe by design, the `events`
table was still using sequential integer IDs with auto_increment — the weak
link in an otherwise collision-resistant system.

---

### 12:27 UTC — Cascading failure: archival blocked

**Operation**: Archiving old messages (35 items)

```
Archive failed due to duplicate primary key errors in the events table
```

Archive operations create events internally, hitting the same stale counter.
Items could be marked read but NOT archived.

---

### 12:46 UTC — Recurrence after sync

**Operation**: Closing work items after a remote agent completed work

```
The event counter desync is back — remote agent created events up to 4195
but our local counter is at 4172. Need to burn through again.
```

**Key insight**: This is not a one-time problem. Every time a remote node
creates events and syncs, the local counter falls behind again. The "burn
through" workaround must be repeated after every sync cycle.

---

### 13:00 UTC — Root cause investigation

Full investigation of the auto-increment architecture across all 3 nodes:

| Node | Role | MAX(id) | COUNT(*) | AUTO_INCREMENT |
|------|------|---------|----------|----------------|
| Node A | thin client | 4090 | 2477 | 4091 |
| Node B | primary | 4198 | 3104 | 4199 |
| Node C | satellite | 4217 | 3123 | 4218 |

**Diagnosis confirmed**: `id BIGINT AUTO_INCREMENT PRIMARY KEY` in the events
table. The satellite cron sync uses raw `dolt_pull`/`dolt_push`, bypassing
`bd`'s post-pull `ALTER TABLE AUTO_INCREMENT` fixup (beads#2133).

The existing band-aid only works in `bd`'s own pull path — any external sync
mechanism (cron scripts, Docker exec, direct Dolt CLI) bypasses it entirely.

**Upstream references**:
- **beads#2133**: Post-pull AUTO_INCREMENT reset (only covers `bd`'s pull path)
- **dolthub/dolt#7702**: Even single-server concurrent transactions can collide
- **DoltHub blog**: Officially recommends UUIDs for multi-clone scenarios

---

### 13:41–14:03 UTC — Implementation

UUID v7 primary keys implemented across 6 tables. Migration 010 created with
the add-column/backfill/drop/rename pattern. All tests pass.

DDL quirk discovered: Dolt requires `MODIFY` to remove `AUTO_INCREMENT` before
`DROP PRIMARY KEY` will succeed.

---

### 14:19–14:38 UTC — Live deployment and validation

- Backed up 581 issues, 1794 events via JSONL export
- Migrated all 18 databases across 3 nodes
- 4 databases required manual fixup (stale host metadata)
- E2E test: create on satellite → sync → write on primary → **no Error 1062**

**Result**: All 18 databases migrated. Error 1062 duplicate PK collisions
eliminated.

---

## Cost Summary

| Category | Impact |
|----------|--------|
| Debugging sessions | 4 sessions (~3-4 hours) |
| "Burn through" workarounds | 2+ episodes (~24 failed INSERTs each) |
| Blocked write operations | ~1 hour, all nodes affected |
| Cascading failures (archival, agent work) | Multiple operations blocked |
| Root cause investigation | 1 full session |
| Implementation + testing + deployment | 2 full sessions + satellite deployment |

## Conclusion

The AUTO_INCREMENT collision is a **fundamental architectural mismatch** between
sequential counters and distributed federation. The existing `resetAutoIncrements`
band-aid is fragile — it only covers one sync path and must be called after
every pull on every node. UUID primary keys eliminate the entire class of
problem permanently, which is why DoltHub recommends them for multi-clone
deployments.

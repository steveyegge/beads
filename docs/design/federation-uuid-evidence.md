# Evidence: Error 1062 Duplicate Primary Key Collisions

Supporting evidence for
[federation-uuid-primary-keys.md](federation-uuid-primary-keys.md).

All incidents occurred on **2026-03-13** across a 3-node Dolt federation
(macbook → mini2 primary, mini3 satellite).

## Incident Timeline

### 12:22 UTC — Discovery: ALL writes blocked

**Session**: `e38120ea-2160-4f6e-80b4-dd457ce9fa3e`
**Operation**: `bd update me-yoi7` (closing a bead after deploying Gas Town)

```
Error 1062: duplicate primary key given: [4144]
```

**Impact**: ALL write operations blocked — `bd create`, `bd update`, `bd close`.
The macbook's event counter was at ~4136 while the events table already
contained IDs up to ~4167 (pushed from mini3 satellite).

**Root cause**: `bd` computed new event IDs client-side as `MAX(events.id) + 1`.
After federation sync pushed rows from mini3 → mini2, the macbook's counter
was stale and assigned IDs that already existed.

**Workaround**: "Burning through" — repeated failed INSERT attempts incremented
the counter by 1 each time. ~24 failed attempts were needed to advance past
the gap.

**User (Trillium) insight**:
> "this feels like some sort of configuration flaw in that we should be able to
> squish our beads together without needing to worry about entry numbers. I
> thought that was the whole point of the system, different agents could create
> different beads that would not conflict because of using a hash instead of a
> incrementing id"

**Agent confirmation**:
> The issue IDs are hash-based (`me-yoi7`, `gt-jy4`) — that's the whole point.
> But the `events` table is still using sequential integer IDs with
> auto_increment, which breaks in exactly this federation scenario.

---

### 12:27 UTC — Mail archive failure

**Session**: `07fe372b-4759-4ec8-81fb-cd2db2fe1fb0`
**Operation**: Archiving 35 old mail messages

```
Archive failed due to duplicate primary key errors in the events table
```

**Impact**: 35 mail items could be marked read but NOT archived. Archive
operations create events, which collide with the same stale counter.

---

### 12:38 UTC — Wasted agent session

**Session**: `07fe372b-4759-4ec8-81fb-cd2db2fe1fb0`
**Operation**: Slung bead `gt-1zw` to polecat furiosa to research the collision

Furiosa closed `gt-1zw` without producing findings — cherry-picked unrelated
work and submitted it as an MR. Full polecat session wasted due to the
cascading effects of the Error 1062 blocking normal operations.

---

### 12:46 UTC — Recurrence after sync

**Session**: `e38120ea-2160-4f6e-80b4-dd457ce9fa3e`
**Operation**: Closing work items after furiosa completed

```
The event counter desync is back — furiosa created events up to 4195 but
our local counter is at 4172. Need to burn through again.
```

**Key insight**: This is not a one-time problem. Every time a remote agent
creates events on the server, the macbook's counter falls behind again. The
"burn through" workaround must be repeated after every sync cycle.

---

### 13:00 UTC — Root cause investigation

**Session**: `d2271505-051f-4972-b163-b410619228a7`
**Operation**: Deep investigation of the auto-increment architecture

State table captured:

```
| Node             | MAX(id) | COUNT(*) | AUTO_INCREMENT |
|------------------|---------|----------|----------------|
| macbook local    | 4090    | 2477     | 4091           |
| mini2 (primary)  | 4198    | 3104     | 4199           |
| mini3 satellite  | 4217    | 3123     | 4218           |
```

Diagnosis confirmed: `id BIGINT AUTO_INCREMENT PRIMARY KEY` in the events
table. `bd` never explicitly sets the ID — it relies on Dolt's auto_increment.
The satellite cron sync uses raw `dolt_pull`/`dolt_push` via Docker exec,
bypassing `bd`'s post-pull `ALTER TABLE AUTO_INCREMENT` fixup (beads#2133).

**Upstream references found**:
- **beads#2133**: Post-pull AUTO_INCREMENT reset (only works in `bd`'s own
  pull path)
- **dolthub/dolt#7702**: Even single-server concurrent transactions can collide
- **DoltHub blog**: Officially recommends UUIDs over AUTO_INCREMENT for
  multi-clone scenarios

---

### 13:24 UTC — Decision to patch

**User (Trillium)**: "We dont care about quick unblock [...] in order for us to
properly handle this we need to make an architectural change to the dolt db"

**User (Trillium)**: "all right let's patch beads for our need then"

---

### 13:41–14:03 UTC — Implementation

**Session**: `c15305f2-58af-44c4-9c87-7e390c124a04`

UUID v7 primary keys implemented across 6 tables. Migration 010 created.
All tests pass. DDL quirk discovered: Dolt requires `MODIFY` to remove
`AUTO_INCREMENT` before `DROP PRIMARY KEY`.

---

### 14:19–14:38 UTC — Live deployment and validation

**Session**: `3318ef2c-e1ce-4944-a063-916d80661836`

- Backed up 581 issues, 1794 events via JSONL export
- Migrated all 18 databases across 3 nodes
- 4 databases had holdouts due to stale `dolt_server_host` metadata (fixed)
- E2E test: create on mini3 → sync → write on mini2 → **no Error 1062**

Final result:
> 18/18 databases migrated from BIGINT AUTO_INCREMENT → CHAR(36) UUID primary
> keys. Error 1062 (duplicate PK collisions) is resolved.

---

## Related Gas Town Escalations

The broader Dolt federation instability pattern from `gt mail inbox`:

| Date | Bead | Escalation |
|------|------|-----------|
| 2026-03-03 | me-0hj | CRITICAL: Dolt server unreachable at 127.0.0.1:3307 |
| 2026-03-03 | me-ki4 | CRITICAL: GT_DOLT_HOST unreachable, blocks patrol |
| 2026-03-03 | me-1yl | CRITICAL: Remote server unreachable |
| 2026-03-03 | me-bcb | HIGH: bd/beads commands failing |
| 2026-03-03 | me-026w | CRITICAL: Remote server unreachable, config mismatch |
| 2026-03-04 | me-xe45 | CRITICAL: 142 blocked issues from outage |
| 2026-03-13 | me-f70 | HIGH: Dolt server down ~20 min, code-sig deadlock |
| 2026-03-13 | me-gyrw | Federation handoff — E2E tests next |

## Cost Summary

| Category | Impact |
|----------|--------|
| Mayor sessions debugging Error 1062 | 4 sessions (~3-4 hours) |
| Polecat run wasted (wrong work) | 1 full polecat cycle |
| "Burn through" workarounds | 2+ episodes (~24 failed INSERTs each) |
| Mail archive blocked | 35 messages unarchivable |
| Write operations blocked | ~1 hour on macbook |
| Root cause investigation | 1 full mayor session |
| Implementation + testing | 2 full mayor sessions |
| Satellite deployment | SSH to 2 nodes, binary + migration |
| **Total sessions** | **7+ sessions across 5 conversation IDs** |

## Session References

For full transcripts, use `ch session <id>`:

| Session ID | Phase |
|-----------|-------|
| `e38120ea-2160-4f6e-80b4-dd457ce9fa3e` | Discovery, first Error 1062 |
| `07fe372b-4759-4ec8-81fb-cd2db2fe1fb0` | Mail archive failure, wasted polecat |
| `d2271505-051f-4972-b163-b410619228a7` | Root cause analysis, design doc |
| `c15305f2-58af-44c4-9c87-7e390c124a04` | Implementation, tests passing |
| `3318ef2c-e1ce-4944-a063-916d80661836` | Live migration of 18 DBs, E2E validation |

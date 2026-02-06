# Spec Hygiene + Team Coordination Design

> **Status:** Draft | **Phase 1:** Features 1-3 (spec hygiene) | **Phase 2:** Feature 4 (team)

**Context:** Production run of `sbd` on a 683-spec project revealed that scan detects missing files but never purges ghost entries from SQLite, duplicates are reported but require manual bash one-liners to fix, and stale spec cleanup requires composing `find -mtime` with registry awareness. All three problems had the same shape: sbd reports the issue, then the operator has to write ad-hoc SQL or shell to fix it.

**Production numbers (2026-02-06):**
- 75 exact duplicate spec pairs (active/ <-> reference/, similarity 1.00)
- 393 ghost rows in `spec_registry` after files were deleted
- 318 specs older than 7 days with no linked beads
- Manual cleanup: 683 -> 365 specs, 110K lines removed
- Required: raw SQL `DELETE FROM spec_registry WHERE path NOT IN (...)` and bash `find -mtime`

**Architecture:** All three features extend existing Cobra commands under `sbd spec`. No new storage backends. One new store interface method (`DeleteSpecRegistryByIDs`). Features 1-3 share a `--dry-run` / `--apply` safety pattern. Feature 4 is a separate `bd team` command tree using existing agent/slot/gate primitives.

**Tech Stack:** Go 1.24+, Cobra CLI, SQLite (`.beads/beads.db`), `internal/spec` package, `internal/storage/sqlite/spec_registry.go`.

---

## Data Model

### Current `spec_registry` Schema

```sql
CREATE TABLE spec_registry (
    spec_id         TEXT PRIMARY KEY,
    path            TEXT NOT NULL,
    title           TEXT DEFAULT '',
    sha256          TEXT DEFAULT '',
    mtime           DATETIME,
    git_status      TEXT DEFAULT 'tracked',
    discovered_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_scanned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    missing_at      DATETIME,
    -- migration 044:
    lifecycle       TEXT DEFAULT 'active',
    completed_at    DATETIME,
    summary         TEXT DEFAULT '',
    summary_tokens  INTEGER DEFAULT 0,
    archived_at     DATETIME
);

CREATE TABLE spec_scan_events (
    spec_id    TEXT NOT NULL,
    scanned_at DATETIME NOT NULL,
    sha256     TEXT NOT NULL,
    changed    INTEGER DEFAULT 0,
    PRIMARY KEY (spec_id, scanned_at)
);
```

### Schema Changes Required

**None for Features 1-3.** All operations use existing columns:
- Feature 1 reads `missing_at IS NOT NULL` and DELETEs rows
- Feature 2 reads `spec_id`, `path`, `sha256` and DELETEs the non-canonical row
- Feature 3 reads `mtime` and DELETEs rows + files

### New Store Interface Method

Add to `SpecRegistryStore` in `internal/spec/store.go`:

```go
// DeleteSpecRegistryByIDs removes spec_registry rows and their scan events.
DeleteSpecRegistryByIDs(ctx context.Context, specIDs []string) (int, error)
```

Implementation in `internal/storage/sqlite/spec_registry.go`:

```go
func (s *SQLiteStorage) DeleteSpecRegistryByIDs(ctx context.Context, specIDs []string) (int, error) {
    if len(specIDs) == 0 {
        return 0, nil
    }
    s.reconnectMu.RLock()
    defer s.reconnectMu.RUnlock()

    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return 0, fmt.Errorf("begin purge tx: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    placeholders := strings.Repeat("?,", len(specIDs))
    placeholders = strings.TrimSuffix(placeholders, ",")
    args := make([]interface{}, len(specIDs))
    for i, id := range specIDs {
        args[i] = id
    }

    // Delete scan events first (child rows)
    eventsQuery := fmt.Sprintf(
        `DELETE FROM spec_scan_events WHERE spec_id IN (%s)`, placeholders)
    if _, err := tx.ExecContext(ctx, eventsQuery, args...); err != nil {
        return 0, fmt.Errorf("delete scan events: %w", err)
    }

    // Delete registry rows
    regQuery := fmt.Sprintf(
        `DELETE FROM spec_registry WHERE spec_id IN (%s)`, placeholders)
    res, err := tx.ExecContext(ctx, regQuery, args...)
    if err != nil {
        return 0, fmt.Errorf("delete spec registry: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return 0, fmt.Errorf("commit purge: %w", err)
    }
    affected, _ := res.RowsAffected()
    return int(affected), nil
}
```

Must also implement in `internal/storage/dolt/spec_registry.go` and `internal/storage/memory/spec_registry.go` to satisfy the interface.

---

## Feature 1: `sbd spec scan --purge-missing`

**Classification:** SLAM DUNK -- the scan already identifies missing entries, just needs a delete path.

### CLI Contract

```
sbd spec scan --purge-missing    # Scan + delete ghost entries from registry
sbd spec scan                    # Existing behavior (marks missing_at, no delete)
```

### Behavior

1. Run normal `spec.Scan()` and `spec.UpdateRegistry()` (existing flow in `registry.go`)
2. After `UpdateRegistry` returns, query: `SELECT spec_id FROM spec_registry WHERE missing_at IS NOT NULL`
3. If `--purge-missing` flag is set, call `DeleteSpecRegistryByIDs()` for those IDs
4. Report: `purged=N ghost entries removed`

### Where to Modify

| File | Change |
|------|--------|
| `cmd/bd/spec.go` (the scan subcommand init) | Add `--purge-missing` bool flag |
| `internal/spec/registry.go` | Add `PurgeMissing()` function after `UpdateRegistry()` |
| `internal/spec/store.go` | Add `DeleteSpecRegistryByIDs` to interface |
| `internal/storage/sqlite/spec_registry.go` | Implement `DeleteSpecRegistryByIDs` |
| `internal/storage/dolt/spec_registry.go` | Implement (or stub) |
| `internal/storage/memory/spec_registry.go` | Implement |

### Design Decision: Should purge be default?

**Recommendation: Yes, make purge-on-scan the default.** Ghost entries cause false positives in `spec duplicates` and inflate `spec stale` counts. The `missing_at` field is still set during the scan pass for debugging, but the delete follows immediately. Add `--keep-missing` flag to opt out.

Rationale: In the production session, 393 of 683 entries were ghosts. That is 57% noise. No user wants ghosts by default.

### Edge Cases

| Case | Behavior |
|------|----------|
| No missing entries | No-op, report `purged=0` |
| Spec linked to beads but file deleted | Still purge registry row (bead's `spec_id` field is a soft reference, not FK) |
| Scan events for purged spec | Deleted in same transaction via `DeleteSpecRegistryByIDs` |
| Concurrent scan from daemon | `reconnectMu.RLock` already held; transaction is atomic |
| spec_id appears in issues table | `issues.spec_id` is informational -- no FK constraint in SQLite schema |

---

## Feature 2: `sbd spec duplicates --fix`

**Classification:** NEEDS JUDGMENT -- requires canonical resolution rules and safe deletion.

### CLI Contract

```
sbd spec duplicates --fix                     # Dry-run by default: show what would be deleted
sbd spec duplicates --fix --apply             # Actually delete non-canonical copies
sbd spec duplicates --fix --threshold 0.95    # Only fix near-exact dupes (existing flag)
sbd spec duplicates                           # Existing behavior (report only)
```

### Canonical Resolution Rules

Given a duplicate pair `(specA, specB)` with `similarity >= threshold`:

```
dir(specA)     dir(specB)     Keep        Delete      Rationale
-----------    -----------    --------    --------    --------------------------------
active/        reference/     active/     reference/  active is the working copy
active/        archive/       archive/    active/     archive is intentionally preserved
archive/       reference/     archive/    reference/  archive > reference
same dir       same dir       SKIP        SKIP        Ambiguous -- warn and skip
```

Directory detection: extract the first path component from `spec_id` (e.g., `specs/active/foo.md` -> `active`, `specs/reference/bar.md` -> `reference`).

```go
func canonicalDir(specID string) string {
    parts := strings.Split(specID, "/")
    for _, p := range parts {
        switch p {
        case "active", "archive", "reference":
            return p
        }
    }
    return "unknown"
}
```

### Behavior

1. Run existing `spec.FindDuplicates(entries, threshold)` to get pairs
2. For each pair, apply canonical resolution rules
3. Collect the "delete" side into a list
4. In dry-run mode (default): print table of `KEEP | DELETE | SCORE`
5. In `--apply` mode: call `os.Remove()` on the file, then `DeleteSpecRegistryByIDs()` for the registry row

### Where to Modify

| File | Change |
|------|--------|
| `cmd/bd/spec_duplicates.go` | Add `--fix`, `--apply` flags; canonical resolution logic |
| `internal/spec/similarity.go` | Add `ResolveCanonical(pair DuplicatePair) (keep, delete string, skip bool)` |
| `internal/spec/similarity_test.go` | Test all resolution rules |

### Edge Cases

| Case | Behavior |
|------|----------|
| Both specs in same directory | Skip with warning: `SKIP: both in active/, manual review needed` |
| Neither in active/archive/reference | Skip with warning: `SKIP: unknown directories` |
| File already deleted on disk | Delete registry row only, no error on `os.Remove` |
| Spec linked to beads | Delete anyway (soft reference). Print advisory: `note: N beads reference deleted spec` |
| Circular dupes (A=B, B=C, A=C) | Process pairs in order; once a spec is in the delete set, skip pairs involving it |
| SHA256 match (true identical) | Could add fast-path: if `sha256` matches, similarity is 1.0 regardless of title |

---

## Feature 3: `sbd spec cleanup --older-than <duration>`

**Classification:** SLAM DUNK -- time-based cleanup with protection rules.

### CLI Contract

```
sbd spec cleanup --older-than 7d                # Dry-run: show what would be deleted
sbd spec cleanup --older-than 7d --apply        # Delete files + registry rows
sbd spec cleanup --older-than 30d --protect "*.template.md"
sbd spec cleanup --older-than 7d --apply --include-linked  # Override bead protection
```

### Duration Parsing

Support Go-style durations plus day shorthand:
- `7d` -> 7 * 24h
- `30d` -> 30 * 24h
- `2h` -> 2h (Go native)

```go
func parseDuration(s string) (time.Duration, error) {
    if strings.HasSuffix(s, "d") {
        days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
        if err != nil {
            return 0, fmt.Errorf("invalid duration: %s", s)
        }
        return time.Duration(days) * 24 * time.Hour, nil
    }
    return time.ParseDuration(s)
}
```

### Protection Rules

Files protected from deletion (skipped silently):

| Pattern | Reason |
|---------|--------|
| Specs linked to beads (`BeadCount > 0`) | Active work reference |
| `README.md` | Convention file |
| `*.template.md` | Reusable template |
| User-specified `--protect <glob>` patterns | Custom protection |
| `lifecycle = 'completed'` with `archived_at IS NOT NULL` | Already properly archived |

### Behavior

1. Query `ListSpecRegistryWithCounts()` to get entries with bead counts
2. Filter to entries where `mtime < now - duration`
3. Apply protection rules (skip protected)
4. In dry-run mode (default): print table of `SPEC | AGE | BEADS | ACTION`
5. In `--apply` mode: `os.Remove()` each file, then `DeleteSpecRegistryByIDs()` for batch

### Where to Modify

| File | Change |
|------|--------|
| `cmd/bd/spec_cleanup.go` | New file: Cobra command, flag parsing, protection logic |
| `cmd/bd/spec.go` | Register `specCleanupCmd` in init() |
| `internal/spec/store.go` | Uses existing `ListSpecRegistryWithCounts` + new `DeleteSpecRegistryByIDs` |

### Edge Cases

| Case | Behavior |
|------|----------|
| File already deleted | Remove registry row, skip `os.Remove`, no error |
| `--older-than 0d` | Error: `duration must be > 0` |
| All specs protected | Report: `0 specs to clean (N protected)` |
| Spec in `archive/` dir | Still eligible for cleanup if older than threshold and no beads |
| Permission denied on file delete | Warn and continue, do NOT remove registry row |
| `--protect` glob syntax error | Fatal error before any deletions |

---

## Feature 4: `bd team` (Phase 2 -- Future)

**Classification:** NEEDS JUDGMENT. The primitives exist but composition is significant work.

### Existing Primitives

From `cmd/bd/agent.go` and related files:

| Primitive | Location | Purpose |
|-----------|----------|---------|
| `bd agent state <id> <state>` | `agent.go` | State machine: idle/spawning/running/working/stuck/done/stopped/dead |
| `bd agent heartbeat <id>` | `agent.go` | Liveness signal (updates `last_activity`) |
| `bd agent show <id>` | `agent.go` | Agent metadata (role_type, rig, slots) |
| Slot (hook_bead, role_bead) | `types.Issue` fields | Current work assignment |
| Labels (gt:agent, role_type:X, rig:Y) | Label system | Filtering and classification |
| Spec volatility | `spec_volatility_trend.go` | Change frequency for gate decisions |

### Proposed `bd team` Subcommands

#### `bd team plan <epic-id>`

Analyze an epic bead's dependency graph and output parallel execution waves.

```
bd team plan EPIC-dashboard-v2

Wave 1 (parallel):
  [spec-a.md] -> gt-emma (no file conflicts)
  [spec-b.md] -> gt-boris (no file conflicts)

Wave 2 (after wave 1):
  [spec-c.md] -> gt-emma (depends on spec-a)

File disjointness: OK (no overlapping file sets)
Estimated: 2 waves, 3 specs, 2 agents
```

**Algorithm:**
1. Load epic bead and its `dep` links (existing `bd dep` system)
2. Topological sort dependencies into waves
3. For each wave, check file disjointness by reading spec `## Files` sections
4. Assign agents round-robin from available agents (`bd agent` with state=idle)

#### `bd team watch`

Live dashboard reading agent state from `.beads/`.

```
bd team watch

AGENT          STATE    SLOT              LAST ACTIVE   UPTIME
gt-emma        working  spec-a.md         2s ago        45m
gt-boris       idle     (empty)           30s ago       45m
gt-mayor       running  witness-check     1s ago        2h

Waves: 1/2 complete | Specs: 2/3 done | Blockers: 0
```

**Implementation:** Poll `bd agent show` for each agent in the rig, render with `tabwriter`, refresh every 2s. Use `bd list --label=gt:agent --label=rig:<rig>` to discover agents.

#### `bd team score`

Pacman leaderboard for a team session.

```
bd team score

SESSION: 2026-02-06T10:00Z -> now (2h 15m)

AGENT          DOTS   STREAKS   GHOSTS   SCORE
gt-emma        12     3         1        42
gt-boris       8      1         0        24
gt-mayor       2      0         0        6

Team total: 22 dots, 72 points
```

**Implementation:** Reuse existing `computeAchievements()` from pacman, scoped to agent beads in the session time window.

#### `bd team wobble`

Post-session drift check across all agent workspaces.

```
bd team wobble

AGENT       DRIFT-FILES   GIT-STATUS   UNCOMMITTED   VERDICT
gt-emma     0             clean        0             OK
gt-boris    2             dirty        3             DRIFT
```

**Implementation:** For each agent's rig workspace, run `git status --porcelain` and `sbd spec scan` diff.

#### `bd team gate <spec>`

Volatility gate check before assigning a spec to an agent.

```
bd team gate specs/active/auth-refactor.md

Volatility:  HIGH (12 changes in 7d)
Dependents:  3 specs blocked
Disjoint:    NO (overlaps with spec-b.md on auth.go)
Verdict:     BLOCK - too volatile for parallel work
```

**Implementation:** Compose existing `spec volatility` + `spec duplicates` + file overlap checks.

#### `bd team report`

Full post-mortem for a team session.

```
bd team report --session 2026-02-06T10:00Z

Team Report: 2h 15m session
  Agents: 3 (emma, boris, mayor)
  Specs completed: 5/7
  Beads created: 22
  Drift incidents: 1 (boris - auth.go conflict)
  Blockers hit: 2 (resolved)
  Score: 72 points
```

### State Transition Diagram

```
                        +-----------+
                        |           |
            +---------->|   idle    |<----------+
            |           |           |           |
            |           +-----+-----+           |
            |                 |                 |
            |          assign |                 |
            |                 v                 |
            |           +-----------+           |
            |           |           |           |
            |           | spawning  |           |
            |           |           |           |
            |           +-----+-----+           |
            |                 |                 |
            |           ready |                 |
            |                 v                 |
       done |           +-----------+           | timeout
            |           |           |           | (witness)
            |           |  running  +---------->+
            |           |           |           |
            |           +-----+-----+       +---+---+
            |                 |             |       |
            |         working |             | dead  |
            |                 v             |       |
            |           +-----------+       +-------+
            |           |           |
            +<----------+  working  +------+
                        |           |      |
                        +-----------+      |
                                           |
                                     stuck |
                                           v
                                    +-----------+
                                    |           |
                                    |  stuck    |
                                    |           |
                                    +-----+-----+
                                          |
                                    help  |
                                          v
                                    +-----------+
                                    |           |
                                    | running   |
                                    |           |
                                    +-----------+

Transitions triggered by:
  assign       -> bd team plan (auto-assigns idle agents)
  ready        -> agent self-reports after setup
  working      -> agent picks up slot work
  done         -> agent completes slot, returns to idle
  stuck        -> agent self-reports blocker
  help         -> leader or peer unblocks
  timeout      -> witness detects last_activity > threshold
  stopped      -> graceful shutdown (bd agent state X stopped)
```

### Data Model for Team

No new tables. Team coordination uses existing primitives:

| Concept | Storage | How |
|---------|---------|-----|
| Team membership | Labels | `gt:agent` + `rig:<team-name>` |
| Agent state | `agent_state` field on issue | State machine via `bd agent state` |
| Work assignment | `hook_bead` field on issue | Points to current spec/bead |
| Session boundary | Bead with `gt:session` label | Created at session start with timestamp |
| Wave plan | Bead with `gt:wave-plan` label | JSON in description: `{waves: [[spec-a, spec-b], [spec-c]]}` |
| Score | Existing pacman aggregation | Filtered by session time + agent label |

### Phase 2 Implementation Order

1. `bd team plan` -- highest value, enables parallel work
2. `bd team watch` -- visibility into running team
3. `bd team gate` -- safety before assignment
4. `bd team score` -- motivation/gamification
5. `bd team wobble` -- post-session hygiene
6. `bd team report` -- post-mortem aggregation

---

## Testing Strategy

### Feature 1 Tests

```go
// internal/spec/registry_test.go
func TestPurgeMissingDeletesGhostEntries(t *testing.T) {
    // Setup: 3 specs in registry, only 2 on disk
    // Run: UpdateRegistry + PurgeMissing
    // Assert: registry has 2 entries, scan_events for ghost are gone
}

func TestPurgeMissingNoOpWhenAllPresent(t *testing.T) {
    // Setup: 3 specs in registry, all 3 on disk
    // Run: UpdateRegistry + PurgeMissing
    // Assert: registry still has 3 entries
}
```

### Feature 2 Tests

```go
// internal/spec/similarity_test.go
func TestCanonicalResolution(t *testing.T) {
    tests := []struct {
        specA, specB string
        wantKeep     string
        wantDelete   string
        wantSkip     bool
    }{
        {"specs/active/a.md", "specs/reference/a.md", "specs/active/a.md", "specs/reference/a.md", false},
        {"specs/active/a.md", "specs/archive/a.md", "specs/archive/a.md", "specs/active/a.md", false},
        {"specs/archive/a.md", "specs/reference/a.md", "specs/archive/a.md", "specs/reference/a.md", false},
        {"specs/active/a.md", "specs/active/b.md", "", "", true}, // same dir = skip
        {"docs/a.md", "notes/b.md", "", "", true},                // unknown dirs = skip
    }
    for _, tt := range tests {
        keep, del, skip := ResolveCanonical(DuplicatePair{SpecA: tt.specA, SpecB: tt.specB})
        // assert keep, del, skip
    }
}

func TestDuplicatesFixSkipsAlreadyDeleted(t *testing.T) {
    // If spec A appears in multiple pairs and was already marked for deletion,
    // subsequent pairs involving A should be skipped
}
```

### Feature 3 Tests

```go
// cmd/bd/spec_cleanup_test.go
func TestCleanupProtectsLinkedSpecs(t *testing.T) {
    // Setup: old spec with BeadCount=2
    // Run: cleanup --older-than 1d --apply
    // Assert: spec NOT deleted
}

func TestCleanupDeletesUnlinkedOldSpecs(t *testing.T) {
    // Setup: old spec with BeadCount=0
    // Run: cleanup --older-than 1d --apply
    // Assert: file deleted, registry row deleted
}

func TestCleanupProtectGlob(t *testing.T) {
    // Setup: old spec matching "*.template.md"
    // Run: cleanup --older-than 1d --apply --protect "*.template.md"
    // Assert: spec NOT deleted
}

func TestCleanupDryRunDoesNotDelete(t *testing.T) {
    // Run without --apply
    // Assert: no files or rows deleted
}
```

---

## Hashimoto Scorecard

| Criterion | F1: purge-missing | F2: duplicates --fix | F3: cleanup | F4: team |
|-----------|:-:|:-:|:-:|:-:|
| **Manual walkthrough done** | Y (393 ghosts found) | Y (75 dupes found) | Y (318 stale found) | N (future) |
| **Edge cases listed** | 5 | 6 | 6 | -- |
| **Design doc exists** | This doc | This doc | This doc | This doc |
| **Task classification** | SLAM DUNK | NEEDS JUDGMENT | SLAM DUNK | FUTURE |
| **New store methods** | 1 (shared) | 0 (reuses F1) | 0 (reuses F1) | 0 |
| **New files** | 0 | 0 | 1 (`spec_cleanup.go`) | 1+ (`team.go`) |
| **Modified files** | 4-6 | 3 | 2 | TBD |
| **Test cases** | 2+ | 3+ | 4+ | TBD |
| **Risk** | Very low | Medium (wrong canonical pick) | Low | High (composition) |
| **Estimated effort** | 2h | 4h | 3h | 2-3 weeks |

### Implementation Priority

1. **Feature 1** first -- unblocks accurate results for Features 2 and 3
2. **Feature 3** second -- simple, high-value cleanup
3. **Feature 2** third -- requires judgment on canonical rules, benefits from clean registry
4. **Feature 4** last -- Phase 2, depends on Features 1-3 being stable

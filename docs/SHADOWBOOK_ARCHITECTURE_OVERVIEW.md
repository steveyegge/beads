# Shadowbook Architecture Overview

Visual map of how all pieces fit together.

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Developer Workflow                                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. Write specs (markdown)                                  │
│     specs/auth.md, specs/payments.md, specs/api.md         │
│                                                             │
│  2. Link issues to specs                                    │
│     bd create "Task" --spec-id specs/auth.md               │
│                                                             │
│  3. Implement features                                      │
│     (normal beads workflow)                                 │
│                                                             │
│  4. Edit spec (requirements change)                         │
│     specs/auth.md now has "Add Apple Sign-In"              │
│                                                             │
│  5. Scan for drift                                          │
│     bd spec scan                                            │
│                                                             │
│  6. Review changed issues                                   │
│     bd list --spec-changed                                 │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ Shadowbook Core (Scanner + Registry)                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Spec Scanner (internal/spec/scanner.go)                    │
│  ├─ Walk specs/ directory                                  │
│  ├─ Extract H1 title                                       │
│  ├─ Compute SHA256 hash                                    │
│  └─ Return: spec_id, title, hash, mtime                    │
│                                                             │
│  Registry Logic (internal/spec/registry.go)                │
│  ├─ UpdateRegistry(scanned, db) → compare hashes           │
│  ├─ Detect: added, updated, missing                        │
│  ├─ Mark changed specs in SQLite                           │
│  └─ Return: result with change counts                      │
│                                                             │
│  Storage Layer (internal/storage/sqlite/spec_registry.go)  │
│  ├─ UpsertSpecRegistry() — insert/update spec rows         │
│  ├─ GetSpecRegistry() — fetch single spec                  │
│  ├─ ListSpecRegistry() — all specs                         │
│  ├─ MarkSpecChangedBySpecIDs() — flag linked issues        │
│  └─ ListSpecRegistryWithCounts() — spec + issue counts     │
│                                                             │
│  RPC Layer (internal/rpc/server_spec.go)                   │
│  ├─ handleSpecScan() — RPC endpoint                        │
│  ├─ handleSpecList()                                       │
│  ├─ handleSpecShow()                                       │
│  └─ handleSpecCoverage()                                   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ Data Layer (Git-Backed)                                     │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  .beads/beads.db (SQLite)                                   │
│  ├─ spec_registry table                                    │
│  │  ├─ spec_id: "specs/auth.md"                            │
│  │  ├─ title: "Authentication System"                      │
│  │  ├─ sha256: "d44494c82..."                              │
│  │  ├─ last_scanned_at: 2026-01-28 14:41                   │
│  │  └─ missing_at: NULL (file exists)                      │
│  │                                                         │
│  │  [NEW] For future compaction:                          │
│  │  ├─ lifecycle: "active" | "complete" | "archived"       │
│  │  ├─ summary: "AI-generated summary text"                │
│  │  ├─ summary_tokens: 150                                 │
│  │  └─ archived_at: 2026-01-28                             │
│  │                                                         │
│  ├─ issues table (EXISTING)                                │
│  │  ├─ spec_id: "specs/auth.md"                            │
│  │  ├─ spec_changed_at: 2026-01-28 14:41 ← flagged!       │
│  │  └─ [SPEC CHANGED] ← shown to user                      │
│  │                                                         │
│  └─ Other tables (from beads)                              │
│     ├─ events (audit trail)                                │
│     ├─ comments                                            │
│     ├─ labels                                              │
│     ├─ dependencies                                        │
│     └─ ...                                                 │
│                                                             │
│  .beads/issues.jsonl (Git-tracked)                         │
│  ├─ Immutable event log                                    │
│  ├─ Event: IssueCreated (bd-vol, spec_id: specs/auth.md)   │
│  ├─ Event: IssueTitleChanged                               │
│  ├─ Event: SpecChanged (← marked here)                     │
│  ├─ Event: IssueAcknowledgedSpec (← cleared here)          │
│  └─ Syncs via git (team sees changes)                      │
│                                                             │
│  .beads/specs-archive.jsonl (PROPOSED - for Phase 2)       │
│  ├─ Archived specs (to keep issues.jsonl lean)             │
│  ├─ Can be gitignored if too large                         │
│  └─ Reduces git operations overhead                        │
│                                                             │
│  specs/*.md (Your spec files)                              │
│  ├─ Regular markdown files                                 │
│  ├─ In git repo                                            │
│  ├─ Hashed by shadowbook                                   │
│  └─ NOT gitignored                                         │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ CLI Layer (cmd/bd/spec.go)                                  │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Commands:                                                  │
│  ├─ bd spec scan         → UpdateRegistry()                │
│  ├─ bd spec list         → ListSpecRegistry()              │
│  ├─ bd spec show <id>    → GetSpecRegistry() + linked      │
│  ├─ bd spec coverage     → coverage metrics                │
│  ├─ bd spec status       → show lifecycle (FUTURE)         │
│  ├─ bd spec compact      → generate summary (FUTURE)       │
│  ├─ bd spec consolidate  → merge specs (FUTURE)            │
│  └─ bd spec impact       → dependency analysis (FUTURE)    │
│                                                             │
│  Flags:                                                     │
│  ├─ --json               → output as JSON                  │
│  ├─ --spec "specs/auth/" → filter by spec path             │
│  ├─ --spec-changed       → only changed specs              │
│  ├─ --full               → show full detail (FUTURE)       │
│  ├─ --history            → show change history (FUTURE)    │
│  └─ --recommend          → compaction suggestions (FUTURE) │
│                                                             │
│  Integration with beads:                                    │
│  ├─ bd create --spec-id  → links issue to spec             │
│  ├─ bd list --spec-changed → show flagged issues           │
│  ├─ bd show              → display SPEC CHANGED warning    │
│  └─ bd update --ack-spec → acknowledge change              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ Advanced Features (FUTURE PHASES)                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Phase 1 (Implemented):                                     │
│  ✅ Scanner, Registry, CLI, Database, Change detection    │
│                                                             │
│  Phase 2 (Proposed - docs/SHADOWBOOK_COMPACTION_LIFECYCLE.md):
│  📋 Spec Lifecycle Tracking                                │
│     ├─ States: active → complete → archived → retired      │
│     └─ Track completion timestamps                         │
│                                                             │
│  📋 AI-Generated Summaries                                 │
│     ├─ Use Claude API for semantic compression             │
│     ├─ 150 lines → 2 lines (summary)                       │
│     └─ Save tokens from context window                     │
│                                                             │
│  📋 Deduplication                                          │
│     ├─ Find overlapping specs                              │
│     ├─ Suggest consolidation                               │
│     └─ Merge with history preserved                        │
│                                                             │
│  📋 Archive JSONL Separation                               │
│     ├─ Move old specs to specs-archive.jsonl               │
│     ├─ Keep issues.jsonl lean for git operations           │
│     └─ Can gitignore archive if needed                     │
│                                                             │
│  📋 Context Window Awareness                               │
│     ├─ Track token usage                                   │
│     ├─ Warn when approaching limits                        │
│     └─ Recommend compaction                                │
│                                                             │
│  📋 Dependency Analysis                                    │
│     ├─ Build spec dependency graph                         │
│     ├─ Show: "If auth.md changes, test these 5 specs"     │
│     └─ Impact scope calculation                            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## Data Flow: Spec Change Detection

```
┌─────────────────────────┐
│  specs/auth.md (v1)     │
│                         │
│  # Auth System          │
│  - OAuth 2.0            │
│  - Google/GitHub        │
│                         │
│  SHA256 = a1b2c3...     │
└─────────────────────────┘
           │
           │ Developer edits
           ↓
┌─────────────────────────┐
│  specs/auth.md (v2)     │
│                         │
│  # Auth System          │
│  - OAuth 2.0            │
│  - Google/GitHub        │
│  - [NEW] Apple Sign-In  │
│                         │
│  SHA256 = d4e5f6...     │
└─────────────────────────┘
           │
           │ bd spec scan
           ↓
┌────────────────────────────────────┐
│  Scanner.Scan("specs/")            │
│  ├─ Read specs/auth.md (v2)        │
│  └─ Return hash = d4e5f6...        │
└────────────────────────────────────┘
           │
           │ Compare with registry
           ↓
┌────────────────────────────────────┐
│  SQLite spec_registry              │
│  spec_id = "specs/auth.md"         │
│  sha256 (old) = "a1b2c3..."        │
│  sha256 (new) = "d4e5f6..."        │
│                                    │
│  Hash mismatch! ✓ Update           │
└────────────────────────────────────┘
           │
           │ Registry.UpdateRegistry()
           ↓
┌────────────────────────────────────┐
│  SQLite issues table               │
│  WHERE spec_id = "specs/auth.md"   │
│                                    │
│  bd-vol: "Implement OAuth"         │
│  bd-vol.1: "Add Google Provider"   │
│  bd-vol.2: "Add GitHub Provider"   │
│                                    │
│  SET spec_changed_at = NOW()       │
└────────────────────────────────────┘
           │
           │ Export to JSONL
           ↓
┌────────────────────────────────────┐
│  .beads/issues.jsonl               │
│  Event: SpecChanged {              │
│    issue_id: bd-vol                │
│    spec_id: specs/auth.md          │
│    timestamp: 2026-01-28           │
│  }                                 │
└────────────────────────────────────┘
           │
           │ bd list --spec-changed
           ↓
┌────────────────────────────────────┐
│  CLI Output                        │
│                                    │
│  ● bd-vol [SPEC CHANGED]           │
│    Implement OAuth                 │
│    Issue needs review!             │
└────────────────────────────────────┘
           │
           │ Developer reviews
           │ & acknowledges
           ↓
┌────────────────────────────────────┐
│  bd update bd-vol --ack-spec       │
│                                    │
│  SQLite: SET spec_changed_at = NULL│
│  JSONL: Event: SpecAcknowledged    │
└────────────────────────────────────┘
           │
           │ bd list --spec-changed
           ↓
┌────────────────────────────────────┐
│  (empty list - all reviewed)       │
└────────────────────────────────────┘
```

---

## Beads Integration Points

Shadowbook extends beads in 3 ways:

### 1. Storage Layer
```
beads/internal/storage/
├─ sqlite/
│  └─ spec_registry.go        ← NEW
│     ├─ UpsertSpecRegistry()
│     ├─ ListSpecRegistry()
│     ├─ GetSpecRegistry()
│     └─ MarkSpecChangedBySpecIDs()
├─ dolt/
│  └─ spec_registry.go        ← NEW (same interface)
└─ memory/
   └─ spec_registry.go        ← NEW (for testing)
```

### 2. RPC Layer
```
beads/internal/rpc/
├─ server_spec.go             ← NEW
│  ├─ handleSpecScan()
│  ├─ handleSpecList()
│  ├─ handleSpecShow()
│  └─ handleSpecCoverage()
├─ client.go
│  └─ SpecScan() method       ← NEW
└─ protocol.go                ← NEW message types
```

### 3. CLI Layer
```
beads/cmd/bd/
├─ spec.go                    ← NEW
│  ├─ cmdSpecScan()
│  ├─ cmdSpecList()
│  ├─ cmdSpecShow()
│  └─ cmdSpecCoverage()
├─ create.go
│  └─ --spec-id flag          ← MODIFIED
├─ list.go
│  └─ --spec-changed flag     ← MODIFIED
└─ show.go
   └─ Display [SPEC CHANGED]  ← MODIFIED
```

### 4. Data Model
```
beads/internal/types/
├─ types.go
│  ├─ Issue{}
│  │  └─ SpecID: string              ← ADDED
│  │  └─ SpecChangedAt: *time.Time   ← ADDED
│  └─ [NEW Types]
│     ├─ SpecRegistryEntry{}
│     ├─ ScannedSpec{}
│     └─ SpecScanResult{}
```

---

## Files Created/Modified

### Phase 1 (MVP - Done)

**New Files:**
- `internal/spec/scanner.go` — Walk dir, hash files
- `internal/spec/registry.go` — Compare hashes, mark issues
- `internal/spec/store.go` — Interface definition
- `internal/spec/types.go` — Data structures
- `internal/storage/sqlite/spec_registry.go` — CRUD
- `internal/storage/dolt/spec_registry.go` — CRUD (alternative backend)
- `internal/storage/memory/spec_registry.go` — Testing backend
- `internal/rpc/server_spec.go` — RPC handlers
- `cmd/bd/spec.go` — CLI commands

**Modified Files:**
- `internal/types/types.go` — Added spec_id, spec_changed_at to Issue
- `internal/rpc/client.go` — Added SpecScan() method
- `internal/rpc/protocol.go` — Added SpecScan message types
- `cmd/bd/create.go` — Added --spec-id flag
- `cmd/bd/list.go` — Added --spec-changed filter
- `cmd/bd/show.go` — Display [SPEC CHANGED] warning

### Phase 2 (Proposed - docs/SHADOWBOOK_COMPACTION_LIFECYCLE.md)

**New Files:**
- `internal/spec/compactor.go` — AI summaries, lifecycle tracking
- `internal/spec/deduplicator.go` — Find overlapping specs
- `internal/spec/archiver.go` — Archive/restore from cold storage
- `internal/spec/dependency_analyzer.go` — Build spec graph
- `cmd/bd/spec_compact.go` — Compaction commands

**Modified Files:**
- `internal/types/types.go` — Add lifecycle, summary, archived_at
- `cmd/bd/spec.go` — Add: status, compact, consolidate, impact commands
- Database migrations

---

## Keep Beads Vision Intact

✅ **Git-backed:** Specs in JSONL, registry in SQLite cache (synced via export)
✅ **Distributed:** Each dev maintains their own registry, but issue flags sync
✅ **Offline-first:** Scan works locally, no network needed
✅ **Transparent:** Inspect `.beads/` directory, see all data
✅ **Reversible:** Git history preserved, can undo anything
✅ **Optional:** Compaction is feature, not mandatory
✅ **Simple:** No magic, just hashes and timestamps
✅ **No vendor lock-in:** Using Claude is optional, could use any LLM

---

## What's Different from Beads?

| Feature | Beads | Shadowbook |
|---------|-------|-----------|
| Issue tracking | ✅ Core | ✅ Inherited |
| Dependency tracking | ✅ Yes (blocks, related) | ✅ Yes + spec links |
| Spec awareness | ❌ No | ✅ Yes (scanner + registry) |
| Change detection | ❌ No | ✅ Yes (hash-based) |
| Automatic flagging | ❌ No | ✅ Yes (spec_changed_at) |
| Bidirectional links | ❌ No | ✅ Yes (spec ↔ issues) |
| Lifecycle management | ❌ No | ✅ Yes (proposed) |
| Compaction | ⚠️ Planned | ✅ Proposed |

---

## Philosophy

**Beads:** Git-backed issue tracker for AI agents
**Shadowbook:** + Spec intelligence to keep code aligned with design

**Core insight:** When specs change, code should know—not through enforcement, but through **awareness**.

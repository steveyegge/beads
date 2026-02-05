# TypeFormula: Formulas as First-Class Beads

**Epic**: gt-pozvwr.24
**Date**: 2026-02-05
**Author**: beads/crew/rpc_ops

## Overview

Formulas are beads' workflow templating engine — they define reusable sequences of
steps that cook (compile) into proto beads and pour/wisp (instantiate) into live
molecules. Today they live on the filesystem as `.formula.toml` / `.formula.json`
files under `.beads/formulas/`. This design promotes formulas to first-class beads:
database-stored issues with `issue_type = "formula"` and formula content in the
`Metadata` JSON field.

## Motivation

**Problem**: Formulas are the only beads concept that lives outside the beads database.

| Concern | Issues | Formulas (today) |
|---------|--------|-------------------|
| Storage | Database (SQLite/Dolt) | Filesystem |
| Sync | JSONL + git (automatic) | Manual file management |
| Discovery | `bd search`, `bd list` | `bd formula list` (directory walk) |
| Versioning | Event log + JSONL history | Git only |
| Federation | Cross-rig via routes.jsonl | Per-directory, no federation |
| Audit trail | Full event log | None |
| Daemon access | RPC API | Requires filesystem access |

Making formulas beads gives them all the same capabilities: federation sync, RPC
access, event history, labels, dependencies, cross-rig discovery.

**Trigger**: The `cook` and `pour` RPC operations (bd-wj80) are currently stubs
returning "not yet supported via daemon RPC" because the daemon has no way to load
formulas — it doesn't walk the filesystem. Database-stored formulas unblock full
daemon support.

## Design Decisions

### Decision 1: Built-in Type vs Custom Type

**Chosen**: Add `TypeFormula` as a built-in type in `types.go`.

**Alternatives**:
1. **Custom type** via `types.custom` config (like molecule, gate, agent, etc.)
   - Pro: No code change to types.go
   - Con: Requires config on every rig; if missing, formulas silently rejected on import
   - Con: No special validation or behavior hooks

2. **Built-in type** with `TypeFormula IssueType = "formula"` constant
   - Pro: Always available, no config needed
   - Pro: Can add formula-specific validation in `IsValid()`
   - Pro: Content hash, export, import all work automatically
   - Con: Adds to the core type list (acceptable — skills, gates, events set precedent)

**Rationale**: Formulas are a core beads concept (cook/pour/wisp). Unlike molecule or
gate (which are Gas Town workflow concerns), formulas are part of beads' own templating
system. A built-in type ensures they work everywhere without configuration.

### Decision 2: Storage Approach — Metadata Field

**Chosen**: Store the full formula definition in `Issue.Metadata` (json.RawMessage).

**Alternatives**:
1. **Metadata field** (json.RawMessage on Issue)
   - Pro: No schema changes, no migrations, no new tables
   - Pro: Content hash already includes Metadata — formula changes auto-propagate
   - Pro: JSONL export/import works automatically
   - Con: No SQL indexing on formula internals (vars, steps)
   - Con: Must deserialize to query formula contents

2. **Dedicated fields on Issue struct** (like Skill, Gate, Agent field groups)
   - Pro: Type-safe, SQL-queryable, indexable
   - Con: Formula has ~13 top-level fields with deep nesting (Steps, ComposeRules,
     AdviceRules) — would add 50+ fields to an already large struct
   - Con: Schema migration required for every formula feature addition

3. **Separate `formulas` table** (like `decision_points`)
   - Pro: Clean separation, dedicated schema
   - Con: Must update both storage backends (SQLite + Dolt)
   - Con: Must add FK constraints, migrations, Transaction methods
   - Con: JSONL export/import needs custom handling
   - Con: Over-engineered — formula is read as a unit, never queried field-by-field

**Rationale**: Formulas are opaque blobs that are read whole and parsed in memory. The
Metadata field is designed exactly for this use case (GH#1406). Skills took the
dedicated-fields approach but skills have flat structure; formulas have deep nesting
(Steps contain Loops contain Bodies contain Steps). Metadata keeps the schema clean.

### Decision 3: Issue-to-Formula Field Mapping

The Issue struct's existing fields carry formula identity; Metadata carries content:

```
Issue.Title              ← Formula.Formula (name, e.g. "mol-feature")
Issue.Description        ← Formula.Description
Issue.IssueType          = "formula"
Issue.Metadata           ← { full formula JSON }
Issue.IsTemplate         = true (formulas are read-only templates)
Issue.SourceFormula      ← Formula.Source (filesystem path, if imported)
Issue.Labels             ← ["formula:workflow"] or ["formula:expansion"] or ["formula:aspect"]
```

The Metadata JSON is the formula.Formula struct serialized verbatim:

```json
{
  "formula": "mol-feature",
  "version": 1,
  "type": "workflow",
  "extends": ["base-workflow"],
  "vars": {
    "component": { "description": "Component name", "required": true },
    "framework": { "default": "react", "enum": ["react", "vue"] }
  },
  "steps": [
    { "id": "design", "title": "Design {{component}}", "type": "task" },
    { "id": "implement", "title": "Implement {{component}}", "depends_on": ["design"] }
  ],
  "compose": { ... },
  "phase": "liquid"
}
```

**Why duplicate Formula.Formula in Issue.Title?**: So `bd search` and `bd list` work
naturally. You search by title, not by parsing metadata.

**Why labels for formula type?**: So you can `bd list --label formula:workflow` to
find all workflow formulas. The `formula:` label prefix is a convention, not enforced.

### Decision 4: Parser Integration — Dual-Read Mode

**Chosen**: Extend `Parser` with optional `storage.Storage` backend; try DB first,
fall back to filesystem.

```go
type Parser struct {
    searchPaths    []string
    store          storage.Storage  // NEW: optional DB backend
    cache          map[string]*Formula
    resolvingSet   map[string]bool
    resolvingChain []string
}
```

`LoadByName()` behavior with storage backend:

```
1. Check in-memory cache → hit? return cached
2. Query DB: SearchIssues(ctx, "", {IssueType: "formula", TitleSearch: name})
3. If found → deserialize Metadata → cache → return
4. If not found → fall back to filesystem search (existing behavior)
5. If filesystem hit → cache → return
6. Return "formula not found" error
```

**Rationale**: This is the minimum-invasive change. All existing code calls
`parser.LoadByName()` — adding a DB path inside that method means cook, pour, wisp,
extends resolution, and expansion all work transparently with DB-stored formulas.
Filesystem fallback ensures backward compatibility during migration.

### Decision 5: Formula Name Uniqueness

**Chosen**: Formula names (Issue.Title for formula beads) are unique per rig. If
multiple formulas share a name, the most recently created one wins (consistent with
filesystem shadowing where project-level shadows town-level).

**Enforcement**: Soft — no unique constraint in DB. The parser returns the first
match from its search order (DB first, then filesystem tiers). A future
`bd formula lint` could warn about name collisions.

**Cross-rig**: Different rigs can have formulas with the same name. When resolving
`extends` across rigs, the local rig's formula takes precedence (same as today's
project-tier-shadows-town-tier behavior).

### Decision 6: No New Issue Struct Fields

**Chosen**: Do NOT add formula-specific fields (FormulaType, FormulaPhase, etc.)
to the Issue struct.

**Rationale**: The Issue struct already has 70+ fields across 25 domain groups.
Formula type is carried in `Metadata.type` and discoverable via
`formula:workflow` / `formula:expansion` / `formula:aspect` labels. Phase is in
`Metadata.phase`. Adding dedicated fields provides marginal query benefit at the
cost of further struct bloat. If formula-specific SQL queries become critical
later, fields can be added then.

## Schema

### Type Registration

```go
// internal/types/types.go

const TypeFormula IssueType = "formula"

func (t IssueType) IsValid() bool {
    switch t {
    case TypeBug, TypeFeature, TypeTask, TypeEpic, TypeChore, TypeAdvice, TypeFormula:
        return true
    }
    return false
}
```

### Metadata Schema (v1)

The Metadata JSON is the `formula.Formula` struct, with one addition — a schema
version wrapper for future evolution:

```json
{
  "schema_version": 1,
  "formula": "mol-feature",
  "description": "Standard feature development workflow",
  "version": 1,
  "type": "workflow",
  "extends": [],
  "vars": { ... },
  "steps": [ ... ],
  "template": null,
  "compose": null,
  "advice": null,
  "pointcuts": null,
  "phase": "liquid",
  "requires_skills": []
}
```

`schema_version` is the envelope version — how to parse this metadata blob. It is
distinct from `version` which is the formula's own semantic version. This allows us
to change the storage format (e.g., normalize steps into a different shape) without
conflating it with formula authoring versions.

### Serialization Functions

```go
// internal/formula/serialization.go

// FormulaToIssue converts a Formula to an Issue with metadata.
func FormulaToIssue(f *Formula) (*types.Issue, error)

// IssueToFormula extracts a Formula from an Issue's metadata.
func IssueToFormula(issue *types.Issue) (*Formula, error)
```

Round-trip property: `IssueToFormula(FormulaToIssue(f))` equals `f` for all
valid formulas (modulo Source field which maps to SourceFormula).

### Content Hash

No changes needed. `ComputeContentHash()` already includes `Metadata` in its hash:

```go
w.str(string(i.Metadata)) // Include metadata in content hash
```

Formula content changes → metadata changes → hash changes → JSONL re-export.

## Parser Changes

### New Constructor

```go
// NewParserWithStorage creates a parser that queries the database for formulas.
// Falls back to filesystem search paths when formulas are not found in DB.
func NewParserWithStorage(store storage.Storage, searchPaths ...string) *Parser
```

### LoadByName Flow

```
LoadByName("mol-feature")
  │
  ├─ cache hit? → return cached
  │
  ├─ store != nil?
  │   ├─ query: SearchIssues("", {IssueType: formula, TitleSearch: "mol-feature"})
  │   ├─ found? → IssueToFormula() → cache → return
  │   └─ not found? → fall through to filesystem
  │
  ├─ for dir in searchPaths:
  │   ├─ try dir/mol-feature.formula.toml
  │   └─ try dir/mol-feature.formula.json
  │
  └─ not found → error
```

### Extends Resolution

No changes. The `Resolve()` method calls `LoadByName()` for each parent formula
in the `extends` list. Since `LoadByName()` now checks DB first, parent formulas
stored as beads are resolved automatically. Mixed inheritance (parent in DB, child
on filesystem, or vice versa) works because both paths go through the same
`LoadByName()`.

Cycle detection via `resolvingSet` works identically — it tracks formula names,
not storage locations.

## CLI Changes

### `bd formula import`

```bash
# Import single formula file
bd formula import mol-feature.formula.toml

# Import all formulas from search paths
bd formula import --all

# Import from specific directory
bd formula import --dir .beads/formulas/

# Force overwrite existing (by name match)
bd formula import --force mol-feature.formula.toml
```

Process:
1. Parse formula file with existing parser
2. Call `FormulaToIssue()` to create Issue
3. Check for existing formula bead with same title
4. If exists and not `--force`: skip (print "already exists: bd-xxxx")
5. If exists and `--force`: update metadata via `UpdateIssue()`
6. If new: `CreateIssue()` with type=formula
7. Print created/updated bead ID

### `bd formula list` (updated)

Add `--source` flag:
- `--source=all` (default): show DB formulas + filesystem formulas
- `--source=db`: show only database-stored formulas
- `--source=files`: show only filesystem formulas (current behavior)

Implementation: query `SearchIssues("", {IssueType: formula})` for DB source,
existing `scanFormulaDir()` for filesystem source.

### `bd formula show` (updated)

Accept bead IDs in addition to formula names:
```bash
bd formula show mol-feature      # Lookup by name (DB first, then files)
bd formula show bd-a1b2c         # Lookup by bead ID (direct DB query)
```

## RPC Integration

### Cook/Pour Unblocking

The daemon creates a `NewParserWithStorage(server.storage)` for each cook/pour
request. This gives the parser direct DB access without filesystem dependency.

```go
func (s *Server) handleCook(req *Request) *Response {
    parser := formula.NewParserWithStorage(s.storage)
    f, err := parser.LoadByName(args.FormulaName)
    // ... existing cook pipeline ...
}
```

### Formula CRUD Operations (gt-pozvwr.24.9)

Four new RPC operations for remote formula management:
- `OpFormulaList` — list formulas (filtered by type, labels)
- `OpFormulaGet` — get formula by ID or name
- `OpFormulaSave` — create or update formula bead
- `OpFormulaDelete` — soft-delete (tombstone) formula bead

These are standard CRUD and follow the existing RPC pattern (protocol.go types,
server handler, client method, HTTP route mapping).

## Federation & Sync

### JSONL Export/Import

No changes needed. Formula beads are regular issues — they participate in the
existing JSONL pipeline:

- **Export**: `bd export` includes formula beads in `issues.jsonl`
- **Import**: `bd import` creates/updates formula beads from JSONL
- **Content hash**: Metadata changes trigger re-export (already implemented)
- **Multi-repo hydration**: Formula beads from remote repos are imported with
  `SourceRepo` set, enabling cross-rig formula discovery

### Cross-Rig Discovery

`bd formula list --all` already scans all rigs via `routes.jsonl`. With DB-stored
formulas, this becomes a federated SearchIssues query instead of a directory walk.
The routing layer handles this transparently.

## Migration Path

### Phase 1: Foundation (gt-pozvwr.24.1, .24.2, .24.3)

1. Add `TypeFormula` constant and update `IsValid()`
2. Define metadata schema (this doc)
3. Implement `FormulaToIssue()` / `IssueToFormula()` with tests
4. No user-visible changes yet

### Phase 2: Parser + CLI (gt-pozvwr.24.4, .24.5)

1. Add `storage.Storage` to `Parser`; `LoadByName()` checks DB first
2. Add `bd formula import` command
3. Update `bd formula list` and `bd formula show` for DB source
4. Users can start importing formulas; cook/pour work from DB

### Phase 3: Daemon (gt-pozvwr.24.6, .24.7, .24.8, .24.9)

1. Refactor cook/pour transformation pipeline to `internal/formula/` package
2. Implement full cook RPC (no longer a stub)
3. Implement full pour/wisp RPC (no longer a stub)
4. Add formula CRUD RPC operations

### Phase 4: Polish (gt-pozvwr.24.10, .24.11)

1. Verify JSONL federation works with formula beads
2. Add deprecation warnings for filesystem formulas
3. Keep filesystem fallback indefinitely

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Large metadata blobs | Slow queries for complex formulas | Lazy deserialization; metadata is opaque to DB |
| Name collisions across rigs | Wrong formula loaded | Local rig takes precedence (same as today) |
| Circular extends | Infinite loop | Existing `resolvingSet` cycle detection — no change |
| Migration breaks existing workflows | Users lose formula access | Filesystem fallback is permanent, not just transitional |
| Schema evolution | Old metadata format unreadable | `schema_version` field in envelope; parser handles all versions |

## Non-Goals

- **Formula editor UI**: Not in scope. Formulas are authored as TOML/JSON files
  and imported, or created via `bd create --type formula --metadata '...'`.
- **Partial formula queries**: We don't need to query "all formulas with a var
  named X" via SQL. If needed later, add an index.
- **Formula inheritance in DB**: The `extends` field stores formula names, not
  bead IDs. Resolution goes through `LoadByName()`. This keeps formulas portable.
- **Removing filesystem support**: The filesystem path remains as permanent
  fallback. Deprecation warnings are advisory only.

## Testing

- **Unit**: `FormulaToIssue`/`IssueToFormula` round-trip for all formula types
- **Unit**: `Parser.LoadByName()` with storage backend — DB hit, DB miss + file hit, both miss
- **Unit**: Extends resolution with mixed sources (parent in DB, child on filesystem)
- **Integration**: `bd formula import` + `bd cook` from imported formula
- **Integration**: Cook/pour RPC with DB-stored formula via daemon
- **Integration**: JSONL export includes formula beads, import restores them

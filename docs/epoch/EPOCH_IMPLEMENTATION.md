# Epoch Implementation Plan

## Summary

Add `epoch` as a first-class issue type in beads, enabling strategic organization of work above the epic level.

## Implementation Phases

### Phase 1: Schema Extension (Priority 0)

**Goal:** Add epoch as a recognized issue type with appropriate fields.

#### 1.1 Type Registration

Update `internal/types/issue.go`:

```go
const (
    TypeBug     IssueType = "bug"
    TypeFeature IssueType = "feature"
    TypeTask    IssueType = "task"
    TypeEpic    IssueType = "epic"
    TypeChore   IssueType = "chore"
    TypeEpoch   IssueType = "epoch"  // NEW
)

var ValidTypes = []IssueType{
    TypeBug, TypeFeature, TypeTask, TypeEpic, TypeChore, TypeEpoch,
}
```

#### 1.2 Epoch-Specific Fields

Add optional fields for epoch metadata:

```go
type Issue struct {
    // ... existing fields ...
    
    // Epoch-specific fields
    EpochPhase      string    `json:"epoch_phase,omitempty"`      // planned|active|completed
    EpochStartDate  time.Time `json:"epoch_start,omitempty"`
    EpochTargetDate time.Time `json:"epoch_target,omitempty"`
    EpochExitCriteria []string `json:"epoch_exit_criteria,omitempty"`
}
```

#### 1.3 Dependency Type

Add `belongs-to-epoch` as a recognized dependency relationship:

```go
const (
    DepBlocks        DependencyType = "blocks"
    DepBlockedBy     DependencyType = "blocked-by"
    DepRelates       DependencyType = "relates"
    DepDiscoveredFrom DependencyType = "discovered-from"
    DepBelongsToEpoch DependencyType = "belongs-to-epoch"  // NEW
)
```

**Deliverables:**
- [ ] Update type constants
- [ ] Add epoch fields to Issue struct
- [ ] Add belongs-to-epoch dependency type
- [ ] Update JSON schema
- [ ] Add validation rules (epochs cannot belong to epochs)

---

### Phase 2: CLI Commands (Priority 1)

**Goal:** Enable epoch management through bd commands.

#### 2.1 Create Epoch

```bash
# Basic creation
bd create "Foundation Phase" -t epoch --json

# With metadata
bd create "Trading Intelligence" -t epoch \
  --epoch-phase planned \
  --epoch-target "2026-06-30" \
  --json
```

#### 2.2 Link Epics to Epochs

```bash
# Create epic under epoch
bd create "Schwab RTD Integration" -t epic \
  --deps belongs-to-epoch:bd-1 \
  --json

# Update existing epic
bd update bd-5 --deps belongs-to-epoch:bd-1 --json
```

#### 2.3 Epoch-Specific Queries

```bash
# List all epochs
bd list --type epoch --json

# List epics in an epoch
bd list --type epic --belongs-to-epoch bd-1 --json

# Show epoch with children
bd show bd-1 --include-children --json

# Epoch progress summary
bd epoch-status bd-1 --json
```

#### 2.4 Epoch Lifecycle Commands

```bash
# Activate epoch
bd epoch-activate bd-1 --json

# Complete epoch  
bd epoch-complete bd-1 --reason "All exit criteria met" --json

# Archive epoch
bd epoch-archive bd-1 --json
```

**Deliverables:**
- [ ] Update `bd create` to handle epoch type
- [ ] Add `--belongs-to-epoch` filter to `bd list`
- [ ] Add `--include-children` flag to `bd show`
- [ ] Implement `bd epoch-status` command
- [ ] Implement epoch lifecycle commands
- [ ] Update CLI help text

---

### Phase 3: Validation & Constraints (Priority 1)

**Goal:** Enforce epoch hierarchy rules.

#### 3.1 Hierarchy Rules

```
Epoch
├── Can contain: epic
├── Cannot contain: epoch, feature, task, bug, chore
├── Cannot belong to: epoch
└── Can have dependencies: relates, blocks, blocked-by

Epic  
├── Can contain: feature, task, bug, chore
├── Cannot contain: epoch, epic
├── Can belong to: epoch (via belongs-to-epoch)
└── Can have dependencies: all types
```

#### 3.2 Validation Implementation

```go
func ValidateEpochRelationship(parent, child *Issue) error {
    if parent.Type != TypeEpoch {
        return ErrParentNotEpoch
    }
    if child.Type != TypeEpic {
        return ErrOnlyEpicsInEpochs
    }
    return nil
}
```

#### 3.3 Closure Rules

- Epoch cannot close until all child epics are closed
- Warning if closing epoch with open epics (force flag required)
- Epoch status auto-updates based on child epic status

**Deliverables:**
- [ ] Implement hierarchy validation
- [ ] Add closure constraints
- [ ] Add status propagation logic
- [ ] Update error messages

---

### Phase 4: Reporting & Visualization (Priority 2)

**Goal:** Surface epoch information in reports and dashboards.

#### 4.1 Epoch Status Report

```bash
bd epoch-status bd-1 --json
```

Output:
```json
{
  "epoch": {
    "id": "bd-1",
    "title": "Foundation Phase",
    "phase": "active",
    "progress": {
      "total_epics": 4,
      "completed_epics": 2,
      "in_progress_epics": 1,
      "open_epics": 1,
      "percent_complete": 50
    },
    "epics": [
      {"id": "bd-2", "title": "Beads Harness Pattern", "status": "closed"},
      {"id": "bd-3", "title": "Credential Management", "status": "closed"},
      {"id": "bd-4", "title": "VSCode IDE Control", "status": "in_progress"},
      {"id": "bd-5", "title": "Event Logging", "status": "open"}
    ],
    "exit_criteria": [
      {"criterion": "All infrastructure epics complete", "met": false}
    ]
  }
}
```

#### 4.2 Roadmap View

```bash
bd roadmap --json
```

Output shows epochs on timeline with nested epics.

#### 4.3 VSCode Integration

Update beads_viewer and VSCode extension to display epoch hierarchy in tree view.

**Deliverables:**
- [ ] Implement `bd epoch-status` command
- [ ] Implement `bd roadmap` command
- [ ] Update VSCode extension tree view
- [ ] Add epoch status to dashboard

---

### Phase 5: Agent Integration (Priority 2)

**Goal:** Enable agents to work within epoch context.

#### 5.1 Session Context

Agent sessions can specify active epoch:

```bash
bd session start --epoch bd-1 --json
```

#### 5.2 Work Discovery

`bd ready` prioritizes work from active epoch:

```bash
bd ready --epoch bd-1 --json
```

#### 5.3 Auto-Assignment

New issues discovered during session inherit epoch from parent:

```bash
# If working on epic bd-4 (belongs to epoch bd-1)
bd create "Found bug in IDE control" -t bug --deps discovered-from:bd-4
# Bug inherits epoch context through epic relationship
```

**Deliverables:**
- [ ] Add `--epoch` filter to session commands
- [ ] Update `bd ready` to support epoch filtering
- [ ] Implement epoch context inheritance
- [ ] Update AGENTS.md with epoch workflow guidance

---

### Phase 6: MCP Server Extension (Priority 3)

**Goal:** Expose epoch operations via MCP for Claude integration.

#### 6.1 New MCP Functions

```python
# beads-mcp additions
mcp__beads__create_epoch(title, phase, target_date, exit_criteria)
mcp__beads__list_epochs(phase_filter)
mcp__beads__epoch_status(epoch_id)
mcp__beads__epoch_activate(epoch_id)
mcp__beads__epoch_complete(epoch_id, reason)
mcp__beads__link_epic_to_epoch(epic_id, epoch_id)
```

**Deliverables:**
- [ ] Update beads-mcp with epoch functions
- [ ] Add epoch operations to MCP schema
- [ ] Update MCP documentation

---

## Migration Strategy

### Existing Repositories

For repos already using beads:

1. **No breaking changes** — Epoch is additive
2. **Optional adoption** — Teams choose to add epochs
3. **Bulk linking** — Command to link existing epics to new epochs

```bash
# Create epoch for existing epics
bd create "Legacy Work" -t epoch --json

# Bulk link epics
bd bulk-link --type epic --to-epoch bd-100 --json
```

### Schema Versioning

Add schema version to `.beads/metadata.json`:

```json
{
  "schema_version": "2.0.0",
  "features": ["epochs"]
}
```

---

## Testing Strategy

### Unit Tests

- Epoch type validation
- Hierarchy constraint enforcement
- Closure rule validation
- Status propagation

### Integration Tests

- Full lifecycle: create epoch → add epics → complete epics → close epoch
- Cross-epoch dependency handling
- Git sync with epoch data

### Acceptance Criteria

1. `bd create -t epoch` creates valid epoch issue
2. Epics can be linked to epochs via `--deps belongs-to-epoch:<id>`
3. `bd list --type epoch` returns only epochs
4. `bd epoch-status` shows accurate progress
5. Epoch cannot close with open child epics (without force)
6. MCP functions work in Claude sessions

---

## Timeline Estimate

| Phase | Effort | Dependencies |
|-------|--------|--------------|
| Phase 1: Schema | 2-3 days | None |
| Phase 2: CLI | 3-4 days | Phase 1 |
| Phase 3: Validation | 2-3 days | Phase 1, 2 |
| Phase 4: Reporting | 3-4 days | Phase 1, 2, 3 |
| Phase 5: Agent Integration | 2-3 days | Phase 1, 2 |
| Phase 6: MCP Extension | 2-3 days | Phase 1, 2 |

**Total: ~15-20 days of development work**

---

## Open Questions

1. **Epoch Nesting?** — Should epochs be nestable (epoch contains sub-epochs)? Initial design says no, but may revisit.

2. **Cross-Repo Epochs?** — How do epochs work with multi-repo hydration? Epoch defined in one repo, epics in others?

3. **Epoch Templates?** — Pre-defined epoch structures for common patterns (e.g., "MVP Phase", "Scaling Phase")?

4. **Epoch Transitions?** — Automatic epic migration when epoch completes? Carry-over rules?

---

## References

- [EPOCH_CONCEPT.md](./EPOCH_CONCEPT.md) — Conceptual foundation
- [ARCHITECTURE.md](../ARCHITECTURE.md) — Beads system architecture
- [CLI_REFERENCE.md](../CLI_REFERENCE.md) — Existing command reference
- [AGENTS.md](../../AGENTS.md) — Agent workflow documentation

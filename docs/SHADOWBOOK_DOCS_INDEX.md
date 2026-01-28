# Shadowbook Documentation Index

Complete guide to Shadowbook specs + code drift detection.

---

## Core Concepts

### For Users
- **[SHADOWBOOK_MANUAL.md](SHADOWBOOK_MANUAL.md)** — The Host's Awakening
  - Narrative guide in Westworld style
  - Complete workflow examples
  - Commands reference
  - Troubleshooting

### For Developers
- **[SHADOWBOOK_ENG_SPEC.md](SHADOWBOOK_ENG_SPEC.md)** — Engineering Specification
  - Architecture and design
  - Task breakdown for implementation
  - Phase 1 & 2 scope

- **[SHADOWBOOK_ARCHITECTURE_OVERVIEW.md](SHADOWBOOK_ARCHITECTURE_OVERVIEW.md)** — System Design
  - Visual system architecture
  - Data flow diagrams
  - Beads integration points
  - File structure and modifications
  - Comparison with beads

---

## Advanced Features

### Compaction & Lifecycle Management
- **[SHADOWBOOK_COMPACTION_LIFECYCLE.md](SHADOWBOOK_COMPACTION_LIFECYCLE.md)** — Memory Decay
  - Spec lifecycle states (active → complete → archived → retired)
  - Semantic compression with AI summaries
  - Deduplication and consolidation
  - Archive JSONL separation
  - Context window awareness
  - Dependency analysis and impact tracking
  - Keeps beads vision intact
  - **6 ideas for intelligent compression**

### Planning & Design
- **[SHADOWBOOK_PITCH.md](SHADOWBOOK_PITCH.md)** — Product Vision
  - The Westworld analogy
  - Business value proposition
  - Feature roadmap

- **[SHADOWBOOK_NEXT_SESSION.md](SHADOWBOOK_NEXT_SESSION.md)** — Session Handoff
  - Current implementation status
  - Pending code quality fixes
  - Repository creation steps
  - Homebrew tap setup

---

## Related Specs

### Beads Integration
- **[SPEC_ID.md](SPEC_ID.md)** — Spec ID field in issues
  - How `--spec-id` works
  - Format validation
  - Linking issues to specs

- **[SPEC_SYNC.md](SPEC_SYNC.md)** — Registry synchronization
  - How spec scanner works
  - Change detection logic
  - Database updates

- **[PR_BEADS_SPEC_ID.md](PR_BEADS_SPEC_ID.md)** — Beads PR Reference
  - Phase 1 implementation details
  - Files modified in beads upstream

---

## Testing & Quality

- **[TESTING.md](TESTING.md)** — Test suite
  - Unit tests for scanner
  - Integration tests
  - End-to-end scenarios

---

## Quick Reference

### Spec Lifecycle
```
ACTIVE (in development)
  ↓
COMPLETE (all linked issues closed)
  ↓
ARCHIVED (summarized, takes less space)
  ↓
RETIRED (historical reference only)
```

### Key Commands
```bash
# Scanning
bd spec scan                    # Find changes
bd spec list                    # View all specs
bd spec show specs/auth.md      # Show spec + linked issues

# Management
bd spec compact specs/auth.md   # Compress old spec
bd spec status                  # View lifecycle states
bd spec impact specs/auth.md    # What depends on this?

# Discovery
bd spec coverage                # Specs without issues
bd list --spec-changed          # Issues with drifted specs
```

### Integration
```bash
# Link issues to specs
bd create "Task" --spec-id specs/auth.md

# Acknowledge changes
bd update bd-xxx --ack-spec

# View dependencies
bd spec impact specs/auth.md
```

---

## Implementation Status

| Feature | Status | Doc |
|---------|--------|-----|
| Spec scanning | ✅ Working | SHADOWBOOK_ENG_SPEC.md |
| Change detection | ✅ Working | SPEC_SYNC.md |
| Issue flagging | ✅ Working | SHADOWBOOK_MANUAL.md |
| CLI commands | ✅ Working | SHADOWBOOK_MANUAL.md |
| Database schema | ✅ Working | SHADOWBOOK_ENG_SPEC.md |
| Lifecycle tracking | 📋 Proposed | SHADOWBOOK_COMPACTION_LIFECYCLE.md |
| AI summaries | 📋 Proposed | SHADOWBOOK_COMPACTION_LIFECYCLE.md |
| Deduplication | 📋 Proposed | SHADOWBOOK_COMPACTION_LIFECYCLE.md |
| Archive JSONL | 📋 Proposed | SHADOWBOOK_COMPACTION_LIFECYCLE.md |
| Impact analysis | 📋 Proposed | SHADOWBOOK_COMPACTION_LIFECYCLE.md |
| Consolidation | 📋 Proposed | SHADOWBOOK_COMPACTION_LIFECYCLE.md |

---

## Getting Started

1. **New to Shadowbook?** Start with [SHADOWBOOK_MANUAL.md](SHADOWBOOK_MANUAL.md)
2. **Want to implement it?** Read [SHADOWBOOK_ENG_SPEC.md](SHADOWBOOK_ENG_SPEC.md)
3. **Ready to optimize?** Check [SHADOWBOOK_COMPACTION_LIFECYCLE.md](SHADOWBOOK_COMPACTION_LIFECYCLE.md)
4. **Need specific info?** Use the commands reference in SHADOWBOOK_MANUAL.md

---

## Philosophy

Shadowbook respects **beads vision**:
- ✅ Git-backed (JSONL + SQLite)
- ✅ Distributed (sync like code)
- ✅ Offline-first (no cloud needed)
- ✅ Transparent (inspect .beads/)
- ✅ Reversible (git history intact)
- ✅ Optional (feature, not mandatory)
- ✅ Simple (no magic, just hashes + timestamps)

**Core idea:** When specs change, your code should know. Not through enforcement, but through **awareness**.

---

## Examples by Use Case

### Use Case: Startup MVP
- Link specs to issues: `bd create "..." --spec-id specs/feature.md`
- Scan for changes: `bd spec scan` (daily)
- Review drift: `bd list --spec-changed` (when changes detected)
- Acknowledge: `bd update --ack-spec` (after fixing)

### Use Case: Long-Running Project
- Active specs for current development
- Archive old specs when complete: `bd spec compact`
- Consolidate similar specs: `bd spec consolidate`
- Track dependencies: `bd spec impact specs/auth.md`

### Use Case: Trading Platform
- Spec per market regime/strategy
- Track spec dependencies (if signal spec changes, test dependent specs)
- Archive old regimes to save context window
- Impact analysis for risk assessment

---

## See Also

- **Beads:** https://github.com/steveyegge/beads
- **Van Eck Phreaking:** Specs broadcast their changes, code listens
- **Time Station Emulator:** Related exploit of unintended signals

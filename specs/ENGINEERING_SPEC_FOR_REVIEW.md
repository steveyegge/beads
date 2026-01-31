# Beads Enhancement: Spec & Skill Tracking

**For:** Engineering Design Team Review
**Status:** Phase 1 DONE, Phase 2-4 TODO
**Date:** 2026-01-31
**Repo:** github.com/anupamchugh/shadowbook (fork of steveyegge/beads)
**Language:** Go (bd CLI is Go + SQLite)

---

## Executive Summary

**Problem 1: Spec Drift** - Specs evolve while tasks are in progress
**Problem 2: Skill Drift** - Agents have different skills, causing silent failures

**Solution:** Shadowbook - beads enhancement with hash-based drift detection

---

## Implementation Status

### Phase 1: Spec Registry ✅ COMPLETE

| Feature | Status | Location |
|---------|--------|----------|
| `bd spec scan` | ✅ Done | `cmd/bd/spec.go` |
| `bd spec list` | ✅ Done | `cmd/bd/spec.go` |
| `bd spec show` | ✅ Done | `cmd/bd/spec.go` |
| `bd spec coverage` | ✅ Done | `cmd/bd/spec.go` |
| `bd spec compact` | ✅ Done | `cmd/bd/spec.go` |
| `bd create --spec` | ✅ Done | `cmd/bd/create.go:903` |
| `bd list --spec-changed` | ✅ Done | `cmd/bd/list.go:1385` |
| `bd list --spec` | ✅ Done | `cmd/bd/list.go` |
| `bd update --ack-spec` | ✅ Done | `cmd/bd/update.go:698` |
| `bd close --compact-spec` | ✅ Done | `cmd/bd/close.go:421` |
| `spec_registry` table | ✅ Done | `schema.go:109` |
| SHA256 drift detection | ✅ Done | In schema |
| `spec_id` on issues | ✅ Done | `types.go:53` |
| `spec_changed_at` field | ✅ Done | `types.go:55` |

**Schema (EXISTS in schema.go:109):**
```sql
CREATE TABLE IF NOT EXISTS spec_registry (
    spec_id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    title TEXT DEFAULT '',
    sha256 TEXT DEFAULT '',
    mtime DATETIME,
    discovered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_scanned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    missing_at DATETIME,
    lifecycle TEXT DEFAULT 'active',
    completed_at DATETIME,
    summary TEXT DEFAULT '',
    summary_tokens INTEGER DEFAULT 0,
    archived_at DATETIME
);
```

---

### Phase 2: Activity Dashboard ❌ TODO

| Feature | Status | Effort |
|---------|--------|--------|
| `bd recent` command | ❌ Not built | 1-2 days |
| `bd recent --stale` | ❌ Not built | Part of above |
| `bd recent --today` | ❌ Not built | Part of above |

**To Build:**
```bash
# Show recent activity
bd recent [--today|--this-week|--stale]

# Output:
# ┌─────────────────────────────────────┬───────────┬──────────┐
# │ Item                                │ Status    │ Modified │
# ├─────────────────────────────────────┼───────────┼──────────┤
# │ beads-456: Implement auth           │ ◐ active  │ Today    │
# │   └─ specs/AUTH_SPEC.md             │ ◐ started │ 2h ago   │
# ├─────────────────────────────────────┼───────────┼──────────┤
# │ specs/OLD_ABANDONED.md              │ ❄ stale   │ 60d ago  │
# └─────────────────────────────────────┴───────────┴──────────┘
```

**Files to create:**
```
cmd/bd/recent.go                  # New command
internal/activity/
├── dashboard.go                  # Query + format logic
└── staleness.go                  # Stale detection
```

---

### Phase 3: Extended Preflight ❌ TODO

Current `bd preflight` is for PR readiness (tests, lint, nix hash).
Need to extend for spec/skill drift checks.

| Feature | Status | Effort |
|---------|--------|--------|
| Spec drift check in preflight | ❌ Not built | 0.5 day |
| Skill sync check in preflight | ❌ Not built | 1 day (needs Phase 4) |
| `bd preflight --auto-sync` | ❌ Not built | 0.5 day |

**To Add to preflight.go:**
```go
// Add these checks:
// - Spec drift: any issues with spec_changed_at set?
// - Skill sync: skills match across agents?
```

---

### Phase 4: Skills Manifest ❌ TODO (Main Work)

| Feature | Status | Effort |
|---------|--------|--------|
| `skills_manifest` table | ❌ Not built | 1 day |
| `skill_bead_links` table | ❌ Not built | 0.5 day |
| `bd create --skills` flag | ❌ Not built | 0.5 day |
| `bd close --compact-skills` | ❌ Not built | 0.5 day |
| `skill-manifest audit` | ❌ Not built | 1 day |
| `skill-manifest sync` | ❌ Not built | 1 day |
| `skill-manifest cleanup-candidates` | ❌ Not built | 0.5 day |

**Schema to Add:**
```sql
CREATE TABLE IF NOT EXISTS skills_manifest (
    id TEXT PRIMARY KEY,                -- skill-name
    name TEXT NOT NULL,
    source TEXT NOT NULL,               -- claude | codex | opencode
    path TEXT,
    tier TEXT NOT NULL DEFAULT 'optional', -- must-have | optional
    sha256 TEXT NOT NULL,
    bytes INTEGER,
    status TEXT DEFAULT 'active',       -- active | deprecated | archived
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME,
    archived_at DATETIME
);

CREATE TABLE IF NOT EXISTS skill_bead_links (
    skill_id TEXT NOT NULL,
    bead_id TEXT NOT NULL,
    linked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (skill_id, bead_id),
    FOREIGN KEY (skill_id) REFERENCES skills_manifest(id),
    FOREIGN KEY (bead_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_skills_status ON skills_manifest(status);
CREATE INDEX IF NOT EXISTS idx_skills_tier ON skills_manifest(tier);
CREATE INDEX IF NOT EXISTS idx_skill_bead_links_bead ON skill_bead_links(bead_id);
```

**Files to create:**
```
cmd/bd/skills.go                  # bd skills audit, bd skills sync
internal/skills/
├── manifest.go                   # Skill scanning + manifest logic
├── sync.go                       # Sync between agents
└── db.go                         # SQLite operations
internal/storage/sqlite/skills.go # Storage layer
```

---

## Summary: What's Left

| Phase | Status | Effort |
|-------|--------|--------|
| Phase 1: Spec Registry | ✅ DONE | - |
| Phase 2: Activity Dashboard | ❌ TODO | 1-2 days |
| Phase 3: Extended Preflight | ❌ TODO | 1 day |
| Phase 4: Skills Manifest | ❌ TODO | 4-5 days |

**Total remaining: ~7 days of work**

---

## References

- Shadowbook repo: `/Users/anupamchugh/Desktop/workspace/shadowbook-local`
- Existing spec code: `cmd/bd/spec.go`, `internal/spec/`
- Schema: `internal/storage/sqlite/schema.go`
- Blog posts: Skill Drift, Vibe-Clock Drift Problem

---

**Document Version:** 3.0 (Accurate to codebase)
**Last Verified:** 2026-01-31

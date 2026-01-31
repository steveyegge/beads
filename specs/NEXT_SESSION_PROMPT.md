# Next Session Prompt

Copy this to start your next session:

---

## Context

I'm building **Shadowbook** - a beads fork that tracks specs and skills with drift detection.

**Repo:** `/Users/anupamchugh/Desktop/workspace/shadowbook-local` (github.com/anupamchugh/shadowbook)

**What's Done (Phase 1):**
- `bd spec scan/list/show/coverage/compact` - all working
- `bd create --spec`, `bd list --spec-changed`, `bd update --ack-spec`
- `bd close --compact-spec`
- `spec_registry` table with SHA256 drift detection

**What's Left:**
1. **Phase 2:** `bd recent` command (activity dashboard) - 1-2 days
2. **Phase 3:** Extend `bd preflight` for spec/skill drift - 1 day
3. **Phase 4:** Skills manifest (tables + commands) - 4-5 days

## Task

Pick ONE to implement:

### Option A: Build `bd recent` (Activity Dashboard)
```bash
bd recent [--today|--this-week|--stale]
# Shows beads + specs sorted by last modified
# Flags stale items (30+ days untouched)
```

Files to create:
- `cmd/bd/recent.go`
- `internal/activity/dashboard.go`

### Option B: Build Skills Manifest
Add tables to `internal/storage/sqlite/schema.go`:
```sql
CREATE TABLE skills_manifest (...)
CREATE TABLE skill_bead_links (...)
```

Add commands:
- `bd skills audit` - list skills across agents
- `bd skills sync` - sync missing skills
- `bd create --skills tdd,debug` - link skills to beads

Files to create:
- `cmd/bd/skills.go`
- `internal/skills/manifest.go`
- `internal/skills/sync.go`

### Option C: Extend `bd preflight`
Add spec drift check to existing `cmd/bd/preflight.go`:
- Check if any open issues have `spec_changed_at` set
- Warn about unacknowledged spec changes

## Reference

- Spec: `specbeads/specs/ENGINEERING_SPEC_FOR_REVIEW.md`
- Existing code patterns: `cmd/bd/spec.go`, `internal/spec/`
- Schema: `internal/storage/sqlite/schema.go`

---

**Start with:** "I want to implement Option [A/B/C]"

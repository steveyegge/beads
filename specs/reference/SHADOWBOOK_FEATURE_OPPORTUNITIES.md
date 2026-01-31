# Shadowbook Feature Opportunities (PR to steveyegge/beads)

**Status:** Backlog (post-Jesse-Philosophy-Implementation)
**Owner:** TBD
**Timeline:** After refactoring complete (Week 6+)
**Scope:** Extension to upstream beads (anupamchugh/shadowbook fork)

---

## Executive Summary

Shadowbook currently detects spec drift (design doc ↔ code mismatch) and auto-compacts old specs. The 7 opportunities below expand this to create **end-to-end traceability**: from design doc → tasks → tools → code.

**Key insight:** Your specs, beads, and skills need to talk to each other. Today they're separate systems. After these features, they'll be a unified knowledge graph showing what was designed, what needs to be built, what tools are required, and what's been completed.

**Token impact:** Full implementation saves 30-100k+ tokens/month through auto-compaction + smart cleanup.

---

## The Problem We Discovered

You have 76 specs (29 active + 47 workflows), no systematic way to track:
- Which specs are pending vs in-progress vs done
- Which skills were created from which specs
- Which tasks use which skills
- Implementation status without manual checking
- Spec → Skill → Bead linking (missing the Skill ↔ Bead connection)

Note: This spec uses "bead" for a task/issue tracked in Beads.

---

## Opportunity 1: Spec Status Registry

### Current State
Shadowbook tracks:
- Spec file path
- SHA256 hash (for drift detection)
- Linked issues

### Missing
- **Status field** — pending | in-progress | done
- **Completion tracking** — % of linked issues closed
- **Timestamps** — created_at, completed_at, last_modified_at
- **Skill mapping** — which skills created from this spec

### PR Addition

**Expand spec registry (beads/internal/spec/registry.go):**

```go
type SpecEntry struct {
    Path              string
    Title             string
    SHA256            string
    Status            string    // pending | in-progress | done
    CreatedAt         time.Time
    CompletedAt       *time.Time
    LastModifiedAt    time.Time
    LinkedIssues      []string  // bd-xxx, bd-yyy
    LinkedSkills      []string  // skill-name, skill-name-2
    CompletionPercent float64   // % of linked issues closed
}
```

**SQLite schema addition:**

```sql
ALTER TABLE spec_registry ADD COLUMN status TEXT DEFAULT 'pending';
ALTER TABLE spec_registry ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE spec_registry ADD COLUMN completed_at TIMESTAMP;
ALTER TABLE spec_registry ADD COLUMN linked_skills TEXT;
ALTER TABLE spec_registry ADD COLUMN completion_percent REAL DEFAULT 0;
```

---

## Opportunity 2: Spec Lifecycle Commands

### New CLI Commands

```bash
# Audit all specs with status
bd spec audit [--format json|table] [--status pending|in-progress|done]

# Show spec details + completion
bd spec status <path>

# Mark spec as implemented
bd spec mark-done <path> [--reason "Implementation complete"]

# Link a skill to a spec (when creating skills from specs)
bd spec track-skill <path> --skill <skill-name>

# Show completion progress
bd spec progress [--all|--pending|--in-progress]
```

### Example Output

```
bd spec audit

┌─────────────────────────────────────────────────┬──────────────┬───────────┬──────────┐
│ Path                                            │ Status       │ Issues    │ Skills   │
├─────────────────────────────────────────────────┼──────────────┼───────────┼──────────┤
│ specs/active/JESSE_PHILOSOPHY_...              │ in-progress  │ 2/3 ○    │ 2        │
│ specs/active/CORE_WORKFLOWS_NON_NEGOTIABLES... │ done         │ 12/12 ✓  │ 6        │
│ specs/workflows/SERVICE_REFACTORING_PLAN.md    │ pending      │ 0/5 ○    │ 0        │
│ specs/workflows/PAPER_SIM_WORKFLOW.md           │ in-progress  │ 4/8 ○    │ 3        │
└─────────────────────────────────────────────────┴──────────────┴───────────┴──────────┘

Summary: 76 specs total
  - Pending:     18
  - In-Progress: 34
  - Done:        24
  - Completion:  31.6%
```

---

## Opportunity 3: Auto-Linking in Beads Commands

### Integration Points

**When creating a bead with `--spec-id`:**

```bash
bd create "Implement feature" --spec-id specs/foo.md
```

Shadowbook auto-updates:
- Adds issue to spec's linked_issues
- Sets spec status to "in-progress" (if pending)
- Records creation timestamp

**When closing a bead:**

```bash
bd close bd-xyz --spec specs/foo.md
```

Shadowbook auto-updates:
- Removes from linked_issues or marks as done
- Recalculates completion_percent
- If all issues closed: suggests `bd spec mark-done`

**When creating a skill from a spec:**

```bash
# Via spec-lifecycle skill (already created)
spec-lifecycle create --spec specs/foo.md
```

Shadowbook auto-tracks:
- New skill name added to spec's linked_skills
- Creates bead linking skill to spec

---

## Opportunity 4: Spec Completion Auto-Detection

### Algorithm

Mark spec DONE automatically when:

```
Score = 0.0 to 1.0

Criteria (each adds to score):
  ✓ All linked issues closed         (+0.4)
  ✓ All linked skills implemented    (+0.3)
  ✓ Spec unchanged 30+ days          (+0.2)
  ✓ Marked completed_at              (+0.1)

Thresholds:
  Auto-mark DONE if score >= 0.8
  Suggest DONE if score >= 0.6
```

**New command:**

```bash
bd spec candidates [--action auto-mark|suggest]

Example output:
┌─────────────────────────────────────┬───────┬─────────┐
│ Spec Path                           │ Score │ Action  │
├─────────────────────────────────────┼───────┼─────────┤
│ specs/finished-feature.md           │ 0.95  │ MARK    │
│ specs/almost-done.md                │ 0.72  │ SUGGEST │
└─────────────────────────────────────┴───────┴─────────┘

bd spec mark-done specs/finished-feature.md --auto
```

---

## Opportunity 5: Spec → Skill → Bead Sync (Skill Manifest & Tracking)

### What This Solves

**The Problem (In Layman's Terms):**

Think of your toolbox:
- **Beads** = Issues/tasks database. Each task gets logged (bd-123), tracked, and you can see status. All stored in `.beads/beads.db`.
- **Skills** = Tools/skills (like hammers, screwdrivers). You have them installed but nowhere to track: which task uses which tool, when was it added, is it still needed?

Today: You manually remember which skill belongs to which spec. When a spec finishes, you don't know which skills to keep or delete.

### Solution: Skill Manifest (Like Beads for Skills)

**Skill Manifest = A database that tracks skills exactly like beads track issues.**

Create `.skills/skills.db` (similar to `.beads/beads.db`) that records:
- Every skill installed (name, version, when added)
- Which agent has it (Claude Code, Codex, OpenCode)
- Which beads/specs use it
- Status (active, deprecated, archived)
- Last used date

### Currently
- Beads track issues (in `.beads/beads.db`)
- Specs track files (in `specs/` folder)
- Skills tracked separately (scattered in `.claude/skills`, `~/.codex/skills`, etc.)
- No link between them

### After This Opportunity

**Three layers working together:**
```
Spec (design doc)
  ↓
Beads (tasks to implement the spec)
  ↓
Skills (tools needed to implement)
  ↓
Code (actual implementation)
```

When you:
1. Create a bead linked to a spec → auto-records which skills it uses
2. Close a bead → knows it's no longer using those skills
3. Finish a spec → auto-archives skills not used by other specs
4. View spec status → shows which skills are active/archived

### New Commands

```bash
# List all skills with usage tracking
skill-manifest audit

# Link a skill to a bead (record: "this task uses this skill")
skill-manifest link <bead-id> --skill <skill-name>

# Show which beads/specs use a skill
skill-manifest used-by <skill-name>

# Auto-cleanup when closing a spec
bd close <bead-id> --spec specs/foo.md --cleanup-skills

# Sync skill manifest across agents
skill-manifest sync [--check-only|--auto-sync]

# Show what skills can be deleted (no longer used by any active spec)
skill-manifest cleanup-candidates
```

### Example Output

```
$ skill-manifest audit

┌──────────────────────────┬────────┬──────────────┬─────────────┐
│ Skill                    │ Status │ Used By Beads│ Last Used   │
├──────────────────────────┼────────┼──────────────┼─────────────┤
│ test-driven-dev          │ active │ bd-45, bd-67 │ Jan 29 2026 │
│ writing-skills           │ active │ bd-12        │ Jan 28 2026 │
│ validation-gates         │ active │ bd-88        │ Jan 31 2026 │
│ old-deprecated-skill     │ archived│ (none)      │ Dec 15 2025 │
│ spec-lifecycle           │ active │ bd-123       │ Jan 31 2026 │
└──────────────────────────┴────────┴──────────────┴─────────────┘

$ skill-manifest used-by test-driven-dev

Linked to beads:
├─ bd-45: Implement auth module
├─ bd-67: Add tests for scanner

Linked to specs:
├─ specs/active/AUTH_SPEC.md
└─ specs/workflows/TESTING_WORKFLOW.md

Safe to delete: NO (used by 2 beads)

$ skill-manifest cleanup-candidates

Skills with zero active users (safe to delete):
├─ old-deprecated-skill (archived Jan 30)
└─ legacy-validation (last used Dec 1 2025)

Delete these? (y/n)
```

### Database Schema (`.skills/skills.db`)

```sql
-- Skills manifest table
CREATE TABLE skills_manifest (
    id TEXT PRIMARY KEY,                    -- skill-name
    name TEXT NOT NULL,
    version TEXT,
    source TEXT,                           -- claude-code | codex | opencode
    status TEXT DEFAULT 'active',          -- active | deprecated | archived
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    archived_at TIMESTAMP
);

-- Skill-to-bead linkage
CREATE TABLE skill_bead_links (
    skill_id TEXT NOT NULL,
    bead_id TEXT NOT NULL,
    linked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (skill_id, bead_id),
    FOREIGN KEY (skill_id) REFERENCES skills_manifest(id)
);

-- Skill-to-spec linkage
CREATE TABLE skill_spec_links (
    skill_id TEXT NOT NULL,
    spec_path TEXT NOT NULL,
    linked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (skill_id, spec_path),
    FOREIGN KEY (skill_id) REFERENCES skills_manifest(id)
);

-- Spec-to-bead linkage
CREATE TABLE spec_bead_links (
    spec_path TEXT NOT NULL,
    bead_id TEXT NOT NULL,
    linked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (spec_path, bead_id)
);
```

### Integration with Beads

**When you create a bead:**
```bash
bd create "Implement login" --spec specs/auth.md --skills test-driven-dev,writing-skills
# Auto-creates records in skill_bead_links
```

**When you close a bead:**
```bash
bd close bd-45 --spec specs/auth.md
# Auto-updates last_used_at for linked skills
# Auto-archives skills if no other beads use them
```

**When you finish a spec:**
```bash
bd spec mark-done specs/auth.md --cleanup-skills
# Auto-archives skills only used by this spec
```

### Bidirectional Sync Commands

```bash
bd sync --specs-to-beads    # Create beads for specs with no linked issues
bd sync --beads-to-skills   # Link orphaned skills to beads
bd sync --check             # Audit gaps (specs/skills/beads disconnected)
```

Example output after `bd sync --check`:
```
SYNC CHECK RESULTS
├─ Orphaned skills (not linked to any bead/spec): 3
│  ├─ legacy-tool
│  ├─ old-pattern
│  └─ deprecated-helper
├─ Beads with no linked skills: 5
│  └─ (run: bd sync --beads-to-skills to auto-link)
├─ Specs with unused skills: 2
│  └─ (run: bd sync --cleanup to archive)
└─ Status: 10 issues found (run with --auto-sync to fix)
```

---

## Opportunity 6: Spec Compaction with Skill Cleanup

### Extend Auto-Compaction

Current `bd spec compact` only summarizes spec content.

**Enhanced compaction:**

```bash
bd spec compact specs/done-feature.md \
  --summary "OAuth2 login. 3 endpoints. JWT. Done Jan 2026." \
  --archive-skills   # Mark linked skills for removal

# Or auto-cleanup on spec close
bd close bd-xyz --compact-spec --cleanup-skills
```

Removes:
- Completed spec from active context
- Unused skills linked only to this spec
- Soft-deletes (archive, don't hard-delete)

---

## Implementation Phases

### Phase 1: Spec Status Registry (Week 1-2)

Files to modify:
- `internal/spec/registry.go` — Add status fields
- `internal/spec/db.go` — SQLite schema updates
- `cmd/bd/spec.go` — New audit/status commands

**PR size:** ~300 lines

---

### Phase 2: Auto-Linking (Week 2-3)

Files to modify:
- `cmd/bd/create.go` — Link spec on `bd create --spec-id`
- `cmd/bd/close.go` — Update spec status on `bd close`
- `internal/spec/lifecycle.go` — New lifecycle logic

**PR size:** ~200 lines

---

### Phase 3: Skill Sync Integration (Week 3-4)

Files to modify:
- `cmd/bd/sync.go` — New sync commands
- `internal/skill/manifest.go` — Skill linking
- `internal/spec/registry.go` — Skill tracking

**PR size:** ~250 lines

---

### Phase 4: Auto-Detection (Week 4-5)

Files to modify:
- `internal/spec/completion.go` — Scoring algorithm
- `cmd/bd/spec.go` — `candidates` and `mark-done` commands
- `internal/spec/db.go` — Query optimizations

**PR size:** ~200 lines

---

### Phase 5: Skill Manifest & Tracking (Week 5-6)

Files to modify:
- `internal/skill/manifest.go` — Skill database + queries
- `internal/skill/db.go` — SQLite schema for `.skills/skills.db`
- `cmd/bd/skill.go` — New skill-manifest commands
- `internal/beads/links.go` — Skill-to-bead linking

**PR size:** ~300 lines

---

### Phase 6: Recent Activity Dashboard (Week 6-7)

Files to modify:
- `internal/activity/dashboard.go` — Query builder for recent activity
- `cmd/bd/recent.go` — New recent/activity command
- `internal/format/table.go` — Nested view rendering

**PR size:** ~250 lines

This phase implements Opportunity 7.

---

## Opportunity 7: Recent Activity Dashboard (Unified Visibility)

### What This Solves

**The Problem (In Layman's Terms):**

You have hundreds of specs and beads scattered across folders. When you sit down to work, you ask:
- What have I actually been working on recently?
- What's been abandoned for months?
- Which specs are active right now vs done?
- Which beads are stuck and need help?

Today: You manually look through folders. You can't tell at a glance what's hot vs cold.

### Solution: Activity Dashboard

**A unified view that shows everything ranked by "when was it last touched."**

Think of it like:
- Your phone's "Recently Used Apps" — shows what you've actually been using
- GitHub's activity feed — newest commits, PRs, issues sorted by time
- Slack's sidebar — unread messages flagged, active channels highlighted

This dashboard shows:
  - All specs by last-modified date
  - All beads by last-updated date
  - All skills by last-used date
  - Status icons: pending = ○, in-progress = ◐, done = ✓, archived = ❄
- Nested view: which beads are attached to which specs, which skills they use

### New Commands

```bash
# Show recent activity (default: last 30 days)
bd recent [--top 10|--last 30d|--today]

# Show activity for a specific type
bd recent --specs             # Only specs
bd recent --beads             # Only beads
bd recent --skills            # Only skills
bd recent --all               # Everything (specs + beads + skills)

# Show only active/in-progress (filter out done/archived)
bd recent --active

# Show by time range
bd recent --today             # Last 24 hours
bd recent --this-week         # Last 7 days
bd recent --this-month        # Last 30 days
bd recent --abandoned         # Not touched in 90+ days

# Export for reporting
bd recent --format json       # Machine-readable
bd recent --format csv        # Spreadsheet-friendly
```

### Example Output

```
$ bd recent --all

┌─────────────────────────────────────────────────────┬───────────┬──────────┐
│ Item                                                │ Status    │ Modified │
├─────────────────────────────────────────────────────┼───────────┼──────────┤
│ bd-456: Implement auth endpoints                    │ ◐ active  │ Today    │
│  └─ specs/auth/LOGIN_SPEC.md                        │ ◐ started │ 2h ago   │
│     ├─ skill: test-driven-dev (active)              │           │ 3h ago   │
│     └─ skill: writing-skills (active)               │           │ 1d ago   │
├─────────────────────────────────────────────────────┼───────────┼──────────┤
│ bd-445: Fix scanner logic                           │ ○ pending │ 3d ago   │
│  └─ specs/scanner/RRS_CALC_FIX.md                   │ ○ pending │ 5d ago   │
│     └─ skill: indicator-calculator (active)         │           │ Jan 25   │
├─────────────────────────────────────────────────────┼───────────┼──────────┤
│ specs/active/OLD_ABANDONED_SPEC.md                  │ ❄ stale   │ 60d ago  │
│ (no beads linked • suggest: archive or close)       │           │          │
├─────────────────────────────────────────────────────┼───────────┼──────────┤
│ skill: legacy-validator                             │ ❄ orphan  │ 45d ago  │
│ (used by 0 beads • suggest: archive)                │           │          │
└─────────────────────────────────────────────────────┴───────────┴──────────┘

Summary:
├─ Total active: 12 (in-progress or pending)
├─ Total done: 24 (completed)
├─ Stale (90+ days): 8 (suggest review)
├─ Orphaned skills: 3 (suggest cleanup)
└─ Momentum: 4 items updated today ✓
```

### Nested View (Context-Aware)

When you look at a bead, you see its whole story:

```
$ bd recent --bead bd-456

bd-456: Implement auth endpoints
├─ Status: ◐ in-progress
├─ Created: Jan 28 2026
├─ Last Updated: Today 2:34 PM
├─ Completion: 60% (3/5 sub-tasks done)
├─ Linked Spec:
│  └─ specs/auth/LOGIN_SPEC.md
│     ├─ Status: ◐ in-progress
│     ├─ Linked Issues: 5 (3 closed, 2 open)
│     └─ Completion: 60%
├─ Required Skills:
│  ├─ test-driven-dev (active, last used 3h ago)
│  └─ writing-skills (active, last used 1d ago)
└─ Blockers: None
```

### Integration with Other Opportunities

**This is the capstone:** Shows results of all other features working together.

- Opportunity 1 (Status Registry) → Supplies status field
- Opportunity 2 (Lifecycle Commands) → Supplies timestamps
- Opportunity 3 (Auto-Linking) → Supplies bead-to-spec links
- Opportunity 4 (Auto-Detection) → Surfaces completion %
- Opportunity 5 (Skill Sync) → Shows skill usage
- Opportunity 6 (Smart Cleanup) → Highlights orphaned items

**Activity Dashboard = The unified view of the entire ecosystem.**

### Database Queries

```sql
-- Find all specs modified in last 7 days
SELECT path, title, status, last_modified_at
FROM spec_registry
WHERE last_modified_at > datetime('now', '-7 days')
ORDER BY last_modified_at DESC;

-- Find beads for a spec with their skills
SELECT b.id, b.title, b.status, b.updated_at,
       GROUP_CONCAT(s.name) as skills
FROM beads b
LEFT JOIN spec_bead_links sbl ON b.id = sbl.bead_id
LEFT JOIN skill_bead_links skbl ON b.id = skbl.bead_id
LEFT JOIN skills_manifest s ON skbl.skill_id = s.id
WHERE sbl.spec_path = ?
ORDER BY b.updated_at DESC;

-- Find stale/abandoned items (suggest cleanup)
SELECT path, 'spec' as type, last_modified_at
FROM spec_registry
WHERE last_modified_at < datetime('now', '-90 days')
AND status != 'done'
UNION ALL
SELECT id, 'bead', updated_at
FROM beads
WHERE updated_at < datetime('now', '-90 days')
AND status != 'closed';
```

### Use Cases

**1. Session Recovery**
```bash
# Start of session: Show what you've been working on
bd recent --today
# Helps you context-switch back to active work
```

**2. Weekly Review**
```bash
bd recent --this-week --active
# See momentum: what shipped this week?
```

**3. Cleanup Workflow**
```bash
bd recent --abandoned --all
# Find specs/beads/skills not touched in 90+ days
# Suggest: archive or close
```

**4. Team Standup**
```bash
bd recent --format json > /tmp/activity.json
# Share with team: "Here's what I shipped"
```

**5. Handoff**
```bash
# At end of session
bd recent --all > /tmp/handoff.txt
# Next person reads: "Here's what's active, here's what's stuck"
```

---

## Deliverables for PR

### Documentation

- `docs/SPEC_LIFECYCLE_GUIDE.md` — How to use new commands
- `docs/SPEC_SKILL_LINKING.md` — Spec → Skill mapping explained
- `internal/spec/README.md` — Implementation notes
- Update main README with new commands

### Tests

- `internal/spec/registry_test.go` — Status field tests
- `internal/spec/completion_test.go` — Auto-detection tests
- `cmd/bd/spec_test.go` — CLI command tests
- Integration tests for sync flow

### Examples

- Example project in `test_project/` showing:
  - Creating specs with status
  - Linking skills to specs
  - Tracking completion
  - Auto-compaction workflow

---

## Upstream Compatibility

**Target:** `steveyegge/beads` (upstream)

**Compatibility:**
- ✓ Backward compatible (new fields, no breaking changes)
- ✓ Opt-in (new commands, existing ones unchanged)
- ✓ Works with existing beads installations
- ✓ Graceful migration (auto-creates new fields if missing)

**Review strategy:**
- Small PRs (Phase 1 + 2 first, then 3 + 4)
- Clear separation of concerns
- Well-documented with examples

---

## Success Metrics

### Before This PR

```
76 specs total
├─ No status tracking
├─ No skill linking
├─ No completion tracking
├─ Manual spreadsheet to track progress
└─ Gap unknown
```

### After All 7 Opportunities

```
76 specs with full ecosystem tracking
├─ Status: pending|in-progress|done (Opportunity 1)
├─ Completion: 31.6% (24/76 done) (Opportunity 4)
├─ Skills: 42 linked to specs, tracked in manifest (Opportunity 5)
├─ Beads: 127 linked to specs + skills (Opportunity 3)
├─ Auto-detection: Suggests DONE when ready (Opportunity 4)
├─ Activity Dashboard: See what's hot vs cold (Opportunity 7)
├─ Recent activity: Top 20 items by last-modified
├─ Stale detection: Finds abandoned specs/beads/skills (Opportunity 7)
└─ Gap: Zero (full visibility + automation)
```

---

## Related Work

This PR supports:
- `.claude/skills/spec-lifecycle/SKILL.md` (created Jan 30)
- `.claude/skills/spec-tracker/SKILL.md` (existing)
- `specs/active/JESSE_PHILOSOPHY_IMPLEMENTATION_SPEC.md` (current)

After PR merges upstream:
- Shadowbook becomes complete spec-tracking solution
- spec-lifecycle skill becomes thin wrapper around `bd spec` commands
- All tools (beads, specs, skills, implementation) connected

---

## Decision Gate

**When:** After Jesse Philosophy refactoring complete (Week 6+)
**Owner:** Assign PR champion
**Action:** 
- [ ] Have 50+ specs with status data to validate against
- [ ] Document learnings from refactoring
- [ ] Draft PR with Phase 1 (spec registry)
- [ ] Submit to steveyegge/beads upstream

---

## Notes

This is a **high-impact PR** because:
1. Solves real problem (76 specs, no tracking)
2. Reusable (any codebase with specs/skills/beads)
3. Upstream-friendly (clean, well-tested, documented)
4. Post-MVP (low risk, proven by your use case)

Worth doing after refactoring is complete.

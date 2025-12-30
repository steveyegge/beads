# Sprint Change Proposal: Epic 2 Phase Reclassification

**Date:** 2025-12-30
**Status:** Approved
**Change Scope:** Moderate
**Triggered By:** Epic 1 Retrospective

---

## Section 1: Issue Summary

### Problem Statement

During Epic 1 Retrospective, it was discovered that Epic 2 stories (2.1-2.5) assume writing recovery documentation without hands-on knowledge of actual beads failure scenarios. The current approach is based on guesswork rather than data-driven analysis.

### Context

- **Discovery Timeline:** Post-Epic 1 completion retrospective (2025-12-30)
- **Evidence:** GitHub repository contains 363 closed issues + 29 open issues with real failure scenarios, solutions, and user discussions
- **Root Cause:** Original Epic 2 stories were designed as Implementation (Phase 4) without first gathering domain knowledge

### Evidence

| Evidence Type | Finding |
|---------------|---------|
| GitHub Issues | 363+ closed issues with real failure patterns |
| Issue Content | Contains symptoms, solutions, and prevention strategies |
| Current Stories | Written as "create docs" without data foundation |
| Gap | No knowledge base exists for recovery documentation |

---

## Section 2: Impact Analysis

### Epic Impact

| Epic | Impact Level | Details |
|------|--------------|---------|
| **Epic 2 [bd-9g9]** | Major | Phase reclassification + complete story transformation |
| **Epic 2 v2.0** | New | Required for Phase 4 implementation after analysis |
| **Epic 3-5** | Minor | Dependency chain updated (depends on Epic 2 v2.0) |

### Story Impact

**Current Stories (Implementation-focused) → New Stories (Analysis-focused):**

| Old Story | New Story | Change Type |
|-----------|-----------|-------------|
| 2.1: Recovery Overview Page | 2.1: GitHub Issues Mining - Database Corruption Patterns | Complete rewrite |
| 2.2: Database Corruption Recovery | 2.2: GitHub Issues Mining - Merge Conflicts Patterns | Complete rewrite |
| 2.3: Merge Conflicts Recovery | 2.3: GitHub Issues Mining - Circular Dependencies Patterns | Complete rewrite |
| 2.4: Circular Dependencies Recovery | 2.4: GitHub Issues Mining - Sync Failures Patterns | Complete rewrite |
| 2.5: Sync Failures Recovery | 2.5: Analysis Synthesis + Recovery Framework Design | Complete rewrite |

### Artifact Conflicts

| Artifact | Conflict Level | Action Required |
|----------|----------------|-----------------|
| `epics.md` | Major | Update Epic 2 section completely |
| `workflow-guide.md` | None | Already updated with discovery |
| PRD | None | FRs remain valid |
| Architecture | None | Patterns remain valid |
| Beads DB | Moderate | Update bd-9g9 and sub-story statuses |

### Technical Impact

- No code changes required
- No infrastructure changes
- New output folder: `_bmad-output/research/` for analysis artifacts
- Beads status updates via `bd` commands

---

## Section 3: Recommended Approach

### Selected Path: Direct Adjustment via Phase Reclassification

**Approach Details:**
1. Reclassify Epic 2 from Phase 4 (Implementation) to Phase 2 (Analysis/Solutioning)
2. Transform all 5 stories from "create docs" to "mine and analyze issues"
3. Create Epic 2 v2.0 plan for future Phase 4 implementation
4. Update dependency chain for Epics 3-5

### Rationale

| Factor | Assessment |
|--------|------------|
| Implementation Effort | **Low** - Only documentation and beads updates |
| Timeline Impact | **Minimal** - Analysis may be faster than blind implementation |
| Technical Risk | **Very Low** - No code changes |
| Quality Improvement | **High** - Data-driven documentation |
| Team Momentum | **Positive** - Clear direction forward |

### Alternatives Considered

| Option | Assessment | Reason Not Selected |
|--------|------------|---------------------|
| Write docs anyway | Not viable | Would produce guesswork documentation |
| Defer Epic 2 entirely | Viable | Delays value delivery unnecessarily |
| Hire domain expert | Not viable | Out of scope for this project |

---

## Section 4: Detailed Change Proposals

### Epic 2 Header Change

**OLD:**
```markdown
## Epic 2: Recovery Documentation [bd-9g9] OPEN (READY)
```

**NEW:**
```markdown
## Epic 2: Recovery Documentation Analysis & Knowledge Gathering [bd-9g9] PHASE 2 (ANALYSIS)

> **Phase Reclassification Notice:** Epic 2 moved from Phase 4 (Implementation) to Phase 2 (Analysis/Solutioning).
```

---

### Story 2.1 Transformation

**OLD:** Story 2.1: Recovery Overview Page [bd-9g9.1]
- User story about creating `docs/recovery/index.md`

**NEW:** Story 2.1: GitHub Issues Mining - Database Corruption Patterns [bd-9g9.1]
- Analysis story extracting database corruption patterns from GitHub issues
- Output: `_bmad-output/research/database-corruption-patterns.md`
- Acceptance: Minimum 10 real issues analyzed

---

### Story 2.2 Transformation

**OLD:** Story 2.2: Database Corruption Recovery [bd-9g9.2]
- User story about creating database corruption runbook

**NEW:** Story 2.2: GitHub Issues Mining - Merge Conflicts Patterns [bd-9g9.2]
- Analysis story extracting merge conflict patterns from GitHub issues
- Output: `_bmad-output/research/merge-conflicts-patterns.md`
- Acceptance: Minimum 10 real issues analyzed

---

### Story 2.3 Transformation

**OLD:** Story 2.3: Merge Conflicts Recovery [bd-9g9.3]
- User story about creating merge conflicts runbook

**NEW:** Story 2.3: GitHub Issues Mining - Circular Dependencies Patterns [bd-9g9.3]
- Analysis story extracting circular dependency patterns from GitHub issues
- Output: `_bmad-output/research/circular-dependencies-patterns.md`
- Acceptance: Minimum 5 real issues analyzed (may be fewer if not available)

---

### Story 2.4 Transformation

**OLD:** Story 2.4: Circular Dependencies Recovery [bd-9g9.4]
- User story about creating circular dependencies runbook

**NEW:** Story 2.4: GitHub Issues Mining - Sync Failures Patterns [bd-9g9.4]
- Analysis story extracting sync failure patterns from GitHub issues
- Output: `_bmad-output/research/sync-failures-patterns.md`
- Acceptance: Minimum 10 real issues analyzed

---

### Story 2.5 Transformation

**OLD:** Story 2.5: Sync Failures Recovery [bd-9g9.5]
- User story about creating sync failures runbook

**NEW:** Story 2.5: Analysis Synthesis + Recovery Framework Design [bd-9g9.5]
- Synthesis story combining all pattern analysis and designing documentation framework
- Output: `_bmad-output/research/recovery-framework-design.md`
- Acceptance: Framework enables Epic 2 v2.0 story creation

---

## Section 5: Implementation Handoff

### Change Scope Classification

**Classification:** Moderate
- Requires epic restructuring (not just story edits)
- Affects beads database and multiple documentation artifacts
- No code changes required

### Handoff Responsibilities

| Role | Responsibility | Actions |
|------|----------------|---------|
| **Current Agent** | Execute changes | Update `epics.md`, run beads commands |
| **SM Agent** | Verify sync | Confirm `sprint-status.yaml` reflects changes |
| **Dev Agent** | Execute Phase 2 | Perform GitHub issues analysis per new stories |

### Implementation Steps

1. **Immediate:** Update `epics.md` with approved changes
2. **Immediate:** Execute beads commands to update bd-9g9 status
3. **Immediate:** Create `_bmad-output/research/` directory
4. **Next Sprint:** Begin Story 2.1 (Database Corruption Mining)
5. **Future:** After Phase 2 complete, create Epic 2 v2.0 stories

### Success Criteria

- [ ] `epics.md` reflects Phase 2 analysis structure
- [ ] Beads DB shows bd-9g9 with updated status
- [ ] Research folder exists for analysis outputs
- [ ] Sprint status synchronized
- [ ] Team aligned on Phase 2 approach

---

## Approval

**Proposal Status:** Approved
**Approved By:** Serhii
**Approval Date:** 2025-12-30

**Next Action:** Execute changes per Implementation Handoff section

---

*Generated by BMad Master via correct-course workflow*

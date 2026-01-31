# Skill-Sync Project: Complete Specification Map

**Project Goal:** Contribute skill-sync infrastructure to superpowers marketplace and integrate it into shadowbook workflows.

**Created:** 2026-01-30  
**Total Specs:** 5 documents + this map

---

## Document Overview

### Core Proposal Documents

#### 1. SKILL_SYNC_OSS_PROPOSAL.md (5.4K)
**Purpose:** Original proposal for contributing skill-sync to superpowers  
**Content:**
- Problem statement (skill fragmentation in multi-agent development)
- 3 options for distribution (PR to superpowers, standalone plugin, homebrew)
- Recommendation: Option A (PR to superpowers marketplace)
- MCP sync considerations

**Status:** ✅ Complete  
**Next:** Foundation for other specs

**Use When:** Understanding the original rationale for the project

---

### Implementation Specs

#### 2. MARKETPLACE_PR_EXECUTION_SPEC.md (16K) ⭐ START HERE
**Purpose:** Step-by-step guide to PR skill-sync to superpowers  
**Content:**
- Pre-work: Validate locally, study conventions
- Adapt skill-sync to superpowers format
- Complete SKILL.md with all sections
- 3 RESOURCES/ files (Validation Gates, MCP Sync, Troubleshooting)
- Test locally
- Submit PR with complete description
- Respond to feedback

**Timeline:** 3-4 sessions  
**Key Sections:**
- Part 1: Pre-Work (Session 1) — Validate + Study
- Part 2: Adapt Skill-Sync (Session 2) — Write SKILL.md + RESOURCES/
- Part 3: Test Locally (Session 2) — Verify everything works
- Part 4: Submit PR (Session 3) — Push and open PR
- Part 5: Iterate (Session 4) — Address feedback

**Status:** 🟢 Ready to Execute  
**Next:** Follow this spec step-by-step

**Use When:** Actively working on the PR submission

---

#### 3. SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md (15K)
**Purpose:** Integrate skill-sync into shadowbook workflows  
**Content:**
- Architecture: Where skill-sync fits in shadowbook layers
- New commands: `bd preflight`, modified `bd cook`, `bd close --compact-skills`
- Code changes: 5 files to modify in shadowbook
- Skill-sync modes: `--check-only`, `--auto-sync`, `--json`
- Pre-session setup and workflow examples
- Implementation order (4 phases)

**Timeline:** 2-3 sessions (after PR is submitted)  
**Key Sections:**
- Part 1: Architecture — How it fits
- Part 2: Command Changes — New/modified commands
- Part 3: Shadowbook File Changes — Code modifications
- Part 4: Skill-Sync Modifications — New modes
- Part 5: Workflow Integration — Examples
- Part 8: Implementation Order (4 phases)

**Status:** 📋 Pending (submit to shadowbook after superpowers PR is merged)  
**Next:** Reference for shadowbook integration

**Use When:** Integrating skill-sync into shadowbook after marketplace PR

---

#### 4. SKILL_SYNC_MARKETPLACE_PR_SPEC.md (16K)
**Purpose:** Comprehensive guide covering marketplace contribution + shadowbook integration  
**Content:**
- Skills with validation-gates (gates explained)
- Shadowbook + skill-sync architecture diagram
- Alternative use cases (multi-agent CI/CD, team skill distribution, marketplace curation)
- Full timeline and success criteria
- Extended integration points

**Status:** ✅ Complete (reference document)  
**Next:** Reference for understanding broader context

**Use When:** Understanding how all pieces fit together

---

### This Document

#### 5. SKILL_SYNC_PROJECT_MAP.md (this file)
**Purpose:** Navigation guide for all specs  
**Content:**
- Overview of all documents
- Reading order for different use cases
- Quick reference for what's in each spec
- Decision tree for which spec to use

**Status:** ✅ Created  
**Next:** You are here

---

## Reading Order by Use Case

### Use Case 1: "I want to submit the PR right now"

1. Read: **MARKETPLACE_PR_EXECUTION_SPEC.md** (Part 1-5)
   - This is the complete step-by-step guide
2. Reference: **SKILL_SYNC_MARKETPLACE_PR_SPEC.md** (validation-gates section)
   - For understanding why validation gates matter
3. Execute: Follow Part 1 → Part 2 → Part 3 → Part 4 → Part 5

**Time:** 3-4 sessions

---

### Use Case 2: "I want to understand the full picture first"

1. Read: **SKILL_SYNC_OSS_PROPOSAL.md** (all sections)
   - Understand the original problem and options
2. Read: **SKILL_SYNC_MARKETPLACE_PR_SPEC.md** (Part 1-2)
   - See architecture and how it fits
3. Read: **MARKETPLACE_PR_EXECUTION_SPEC.md** (skimmable intro)
   - See the execution plan
4. Then: Pick a use case above and dive in

**Time:** 1 session to understand, 3-4 to execute

---

### Use Case 3: "I'm integrating into shadowbook, not just marketplace"

1. Read: **MARKETPLACE_PR_EXECUTION_SPEC.md** (Part 1-5)
   - Submit skill-sync to marketplace first
2. Wait: PR merges (1-2 weeks typical)
3. Read: **SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md** (all parts)
   - Plan shadowbook integration
4. Execute: Part 8 (4 phases of integration)

**Time:** 3-4 sessions for marketplace, then 2-3 for shadowbook

---

### Use Case 4: "I just want to know what was decided"

Read: **SKILL_SYNC_PROJECT_MAP.md** (this file) + Part 1 of **MARKETPLACE_PR_EXECUTION_SPEC.md**

**Time:** 20 minutes

---

## Quick Reference: What's in Each Spec

| Aspect | Marketplace PR | Shadowbook Integration | OSS Proposal |
|--------|---|---|---|
| **Problem Statement** | ✅ Yes (Part 1) | ✅ Yes (Part 1) | ✅ Yes |
| **Solution Overview** | ✅ Yes (Part 2) | ✅ Yes (Part 1) | ✅ Yes |
| **Code to Write** | ✅ Yes (complete SKILL.md) | ✅ Yes (5 files) | ❌ No |
| **Commands to Add** | ❌ No | ✅ Yes (bd preflight, etc.) | ❌ No |
| **Timeline** | ✅ Yes (3-4 sessions) | ✅ Yes (2-3 sessions) | ❌ No |
| **Use Cases** | ✅ Yes (implied) | ✅ Yes (detailed) | ✅ Yes (3 options) |
| **Success Criteria** | ✅ Yes | ✅ Yes | ❌ No |
| **Integration Points** | ✅ Yes | ✅ Yes (detailed) | ❌ No |

---

## Dependencies & Order

```
SKILL_SYNC_OSS_PROPOSAL.md
    ↓
    ├─→ MARKETPLACE_PR_EXECUTION_SPEC.md (Session 1-4)
    │       ↓
    │       [Wait for PR approval/merge]
    │       ↓
    └─→ SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md (Session 5-7)
            ↓
            [Integrate into shadowbook repo]
```

**Critical Path:**
1. Submit marketplace PR (3-4 sessions)
2. Get approval (1-2 weeks typical)
3. Merge PR
4. Then integrate into shadowbook (2-3 sessions)

---

## File Locations

All specs are in: `/Users/anupamchugh/Desktop/workspace/apple-westworld/specs/`

```
specs/
├── SKILL_SYNC_OSS_PROPOSAL.md                      (Original proposal)
├── SKILL_SYNC_MARKETPLACE_PR_SPEC.md               (Comprehensive guide)
├── MARKETPLACE_PR_EXECUTION_SPEC.md                (Step-by-step, START HERE)
├── SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md       (Shadowbook integration)
├── SKILL_SYNC_PROJECT_MAP.md                       (This file)
└── [Other specs...]
```

---

## Next Immediate Steps

### If you want to execute:

**Option A: Just submit to marketplace**
1. Read: `MARKETPLACE_PR_EXECUTION_SPEC.md` (entire document)
2. Start: Part 1 (Validate locally)
3. Follow: Parts 2-5 in order

**Option B: Plan full project (marketplace + shadowbook)**
1. Read: `SKILL_SYNC_OSS_PROPOSAL.md` (quick context)
2. Read: `MARKETPLACE_PR_EXECUTION_SPEC.md` (how to submit)
3. Bookmark: `SHADOWBOOK_SKILL_SYNC_INTEGRATION_SPEC.md` (for later)
4. Start: Part 1 of MARKETPLACE spec

---

## Key Dates & Milestones

| Milestone | Sessions | Target Date |
|-----------|----------|------------|
| Validate locally | 1 | This week |
| Study conventions | 0.5 | This week |
| Write SKILL.md | 1 | Next session |
| Test locally | 0.5 | Next session |
| Submit PR | 0.5 | Next session |
| PR approved | — | +1-2 weeks |
| Integrate shadowbook | 2-3 | After approval |

---

## Success Criteria (Overall Project)

- [ ] Marketplace PR submitted to `obra/superpowers`
- [ ] PR approved and merged
- [ ] Skill available via `/plugin marketplace`
- [ ] Shadowbook integration complete
- [ ] Both projects documented in AGENTS.md files

---

## Questions?

Refer back to this map:
- "Which spec do I read?" → See Reading Order by Use Case
- "What's in this spec?" → See Quick Reference table
- "What comes next?" → See Dependencies & Order

---

**Document Version:** 1.0  
**Status:** Active project  
**Last Updated:** 2026-01-30

**Created by:** Claude (Amp Agent)  
**Project:** Skill-Sync Infrastructure for Multi-Agent Development

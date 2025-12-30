# Workflow Guide: Beads Documentation Project

_Створено: 2025-12-30_
_Оновлено: 2025-12-30 (Epic 1 Retrospective + Epic 2 Phase Reclassification)_

---

## Поточний статус

```
✅ Phase 1-2: Discovery & Solutioning (COMPLETE)
✅ Phase 3: Epic/Story Creation (COMPLETE)
✅ Phase 4.1: Epic 1 Implementation (COMPLETE)
⏳ Phase 2.1: Epic 2 Analysis/Knowledge Gathering (NEXT)
```

### Epic Completion Status

```
✅ Epic 1: Foundation & Deployment [bd-fyy] - COMPLETE (Phase 4)
   ├── ✅ Story 1.1: Fix Deployment URLs (bd-fyy.1) - CLOSED
   ├── ✅ Story 1.2: Environment-Based URL Config (bd-fyy.2) - CLOSED
   └── ✅ Story 1.3: Update Sidebar Navigation (bd-fyy.3) - CLOSED

🔄 Epic 2: Recovery Documentation Analysis [bd-9g9] - PHASE 2 RECLASSIFICATION
   └── 📊 Scope: GitHub issues data mining + knowledge base creation
```

---

## RETROSPECTIVE FINDINGS (Epic 1)

### ✅ Epic 1 Successes

1. **Architecture-driven approach працює**
   - Кожна story мала clear reference до architecture.md
   - Zero confusion про requirements
   - Same-day completion для всіх stories

2. **Proactive implementation strategy effective**
   - Story 1.3: створено 7 documentation files vs minimum requirements
   - Placeholder structure для Epic 2 готова
   - Foundation для next epics встановлена

3. **Build process reliable**
   - No major blockers у жодній story
   - Docusaurus build process validated end-to-end
   - Environment configuration working

### 🚨 Significant Discovery: Epic 2 Phase Reclassification

**Problem Identified:**
- Epic 2 stories передбачають writing technical content про beads failures
- Team не має hands-on expertise з beads failure scenarios
- Current stories approach: guesswork vs data-driven

**Solution Discovered:**
- GitHub repository має 363 closed issues + 29 open issues
- Real failure scenarios і solutions є в issues
- Approach: data mining → pattern analysis → structured recovery docs

**Resolution:**
- Epic 2 RECLASSIFIED з Phase 4 (Implementation) → Phase 2 (Analysis/Solutioning)
- New scope: Knowledge gathering + analysis для informed implementation
- Future Epic 2 v2.0 в Phase 4 з knowledge base

---

## EPIC 2 PHASE RECLASSIFICATION PROTOCOL

### Phase 2 Initialization Commands

```bash
# 1. Mark Epic 2 for phase reclassification
_bmad/bin/bd-stage bd-9g9 backlog

# 2. Add reclassification comments
bd comments add bd-9g9 "PHASE RECLASSIFICATION: Epic 2 moved from Phase 4 (Implementation) to Phase 2 (Analysis/Solutioning). Scope: GitHub issues data mining + knowledge base creation."

# 3. Update epic description для Phase 2 scope
bd update bd-9g9 --title "Epic 2: Recovery Documentation Analysis & Knowledge Gathering"
```

### Phase 2 Analysis Workflow

```bash
# 1. Launch Analysis/Research workflow
/bmad:bmm:workflows:research

# Research focus:
# - Type: Technical Research
# - Scope: GitHub issues analysis для beads failure patterns
# - Output: Knowledge base для recovery documentation
```

### Phase 2 Epic Creation Commands

```bash
# 1. Create new Epic 2 (Phase 2) stories
/bmad:bmm:workflows:create-story
# Stories for Phase 2:
# - GitHub Issues Data Mining (database corruption patterns)
# - GitHub Issues Data Mining (merge conflicts patterns)
# - GitHub Issues Data Mining (circular dependencies patterns)
# - GitHub Issues Data Mining (sync failures patterns)
# - Analysis Synthesis + Recovery Framework Design

# 2. Future: Epic 2 v2.0 creation для Phase 4
# After Phase 2 completion → redesign Epic 2 для implementation
```

### Phase 2 Completion Commands

```bash
# 1. Mark Phase 2 Epic 2 complete
_bmad/bin/bd-stage bd-9g9 done

# 2. Create Epic 2 v2.0 для Phase 4 implementation
/bmad:bmm:workflows:create-epics-and-stories
# Based on Phase 2 analysis results

# 3. Update dependencies
# Epic 3-5 depend on Epic 2 v2.0 (Phase 4), not Epic 2 (Phase 2)
```

---

## EPIC PLANNING REVIEW CHECKLIST

### Pre-Review Validation

- [ ] Retrospective completed і documented
- [ ] Significant discoveries identified
- [ ] Current epic status verified в beads
- [ ] Impact assessment completed

### Review Session Requirements

- [ ] PRD available і current
- [ ] Architecture decisions documented
- [ ] Previous epic lessons learned доступні
- [ ] Data sources identified (GitHub issues for Epic 2)

### Post-Review Actions

- [ ] Epic approach updated
- [ ] New stories created if needed
- [ ] Existing stories modified з new approach
- [ ] Dependencies verified і updated
- [ ] Sprint status synchronized

---

## BMAD + Beads: Два джерела правди

### Проблема яку вирішили

```
BMAD workflows використовують:     Beads використовує:
├── bmad:stage:backlog            ├── status: open
├── bmad:stage:ready-for-dev      ├── status: in_progress
├── bmad:stage:in-progress        └── status: closed
├── bmad:stage:review
├── bmad:stage:planning-review    <- NEW for epic changes
└── bmad:stage:done

+ sprint-status.yaml (derived view)
```

### Рішення: bd-stage helper

```bash
# Одна команда для ексклюзивної зміни stage
_bmad/bin/bd-stage <issue-id> <stage>

# Приклад:
_bmad/bin/bd-stage bd-9g9 planning-review
```

### NEW: Significant Discovery Protocol

```bash
# 1. Mark epic for planning review
_bmad/bin/bd-stage <epic-id> planning-review

# 2. Document discovery
bd comments add <epic-id> "SIGNIFICANT DISCOVERY: [detailed description]"

# 3. Schedule planning review session
/bmad:bmm:workflows:create-epics-and-stories
```

---

## Stage Lifecycle (UPDATED)

```
backlog → ready-for-dev → in-progress → review → done
   │           │              │           │        │
   │      bd-stage        bd-stage    bd-stage  bd close
   │           │              │
   │           └──── planning-review (for epic changes)
   │                       │
   │              Epic Planning Review
   │                       │
   │                  ready-for-dev
```

### NEW Commands для Epic Management

| Перехід | Команда |
|---------|---------|
| → planning-review | `_bmad/bin/bd-stage <epic-id> planning-review` |
| Epic planning review | `/bmad:bmm:workflows:create-epics-and-stories` |
| Review complete → ready | `_bmad/bin/bd-stage <epic-id> ready-for-dev` |

---

## Повний Development Workflow (UPDATED)

### 1. Знайти роботу

```bash
bd ready                           # Показує issues без блокерів
bd show <id>                       # Деталі конкретного issue
bd list --status open              # Всі відкриті issues
```

### 2. Epic/Story Development Flow

```bash
# A. Story Implementation (normal flow)
/bmad:bmm:workflows:dev-story

# B. Epic Planning Review (for significant changes)
/bmad:bmm:workflows:create-epics-and-stories

# C. Retrospective (after epic completion)
/bmad:bmm:workflows:retrospective
```

### 3. Взяти в роботу

```bash
_bmad/bin/bd-stage <id> in-progress
bd update <id> --status in_progress
```

### 4. Code Review

```bash
_bmad/bin/bd-stage <id> review
/bmad:bmm:workflows:code-review
```

### 5. Завершити

```bash
bd close <id> "Story completed - description"
_bmad/bin/bd-stage <id> done
```

---

## Current Epic Status (POST-RETROSPECTIVE)

### ✅ Epic 1: Foundation & Deployment [bd-fyy] - COMPLETED

**Epic Summary:**
- **Delivery**: 3/3 stories completed (100%)
- **Quality**: All acceptance criteria met + additional value
- **Timeline**: Same-day completion (2025-12-30)
- **Impact**: PR #784 foundation ready + deployment infrastructure

| Story | ID | Status | Notes |
|-------|-----|--------|-------|
| 1.1 Fix Deployment URLs | bd-fyy.1 | ✅ CLOSED | joyshmitz → steveyegge URLs fixed |
| 1.2 Environment-Based URL Config | bd-fyy.2 | ✅ CLOSED | SITE_URL env var added |
| 1.3 Update Sidebar Navigation | bd-fyy.3 | ✅ CLOSED | 7 doc files created + navigation |

**Epic 1 Lessons Learned:**
- Architecture-driven approach highly effective
- Proactive implementation strategy works
- Build process reliable and validated

### 🔄 Epic 2: Recovery Documentation Analysis [bd-9g9] - PHASE 2 RECLASSIFICATION

**Phase Change:**
- Original: Phase 4 (Implementation) - write recovery docs
- NEW: Phase 2 (Analysis/Solutioning) - gather knowledge + patterns
- Future: Epic 2 v2.0 (Phase 4) - implement docs з knowledge base

**Current Status:** `backlog` (Phase 2 scope)

| Phase 2 Analysis Scope | Status | Approach |
|----------------------|--------|----------|
| **GitHub Issues Data Mining** | **RECLASSIFIED** | **363 closed + 29 open issues** |
| Database Corruption Patterns | Phase 2 | Analysis → pattern extraction |
| Merge Conflicts Patterns | Phase 2 | Analysis → solution synthesis |
| Circular Dependencies Patterns | Phase 2 | Analysis → detection methods |
| Sync Failures Patterns | Phase 2 | Analysis → recovery procedures |
| Recovery Framework Design | Phase 2 | Structure для Phase 4 implementation |

**Phase 2 Actions:**
1. Execute GitHub issues data mining workflow
2. Pattern analysis і categorization
3. Extract real solutions від users
4. Design recovery documentation framework
5. Prepare Epic 2 v2.0 для Phase 4

---

## CRITICAL PATH FOR EPIC 2

### 🚨 Epic Dependencies (PHASE 2 RESTRUCTURE)

```bash
# NEW Epic Flow:
Epic 1 (Phase 4) ✅ → Epic 2 (Phase 2) 🔄 → Epic 2 v2.0 (Phase 4) → Epic 3 → Epic 4 → Epic 5

# Phase 2 Epic 2 Dependencies:
1. Epic 1 infrastructure ready          # COMPLETED ✅
2. GitHub repository access            # AVAILABLE ✅
3. Research workflow access             # BMAD ✅

# Phase 4 Epic 2 v2.0 Dependencies:
1. Phase 2 Epic 2 analysis complete    # FUTURE
2. Knowledge base established          # FROM Phase 2
3. Recovery framework designed         # FROM Phase 2
```

### Commands для Epic 2 (Phase 2) Execution

```bash
# 1. Initialize Phase 2 Epic 2
_bmad/bin/bd-stage bd-9g9 backlog
bd comments add bd-9g9 "PHASE 2: GitHub issues analysis + knowledge gathering for recovery docs"

# 2. Start Phase 2 analysis work
/bmad:bmm:workflows:research
# Scope: GitHub issues data mining

# 3. Create Phase 2 analysis stories
/bmad:bmm:workflows:create-story
# Focus: Data mining + pattern analysis

# 4. Execute Phase 2 analysis
bd ready --parent bd-9g9               # Find ready analysis tasks
/bmad:bmm:workflows:dev-story           # Execute analysis work
```

---

## RETROSPECTIVE COMPLETION TRACKING

### Epic 1 Retrospective Status

- [x] Epic discovery і validation
- [x] Deep story analysis completed
- [x] Team discussion facilitated
- [x] Significant discoveries identified
- [x] Epic 2 planning review flagged
- [x] Action items captured
- [x] Retrospective document saved

**Retrospective File:** `_bmad-output/retrospectives/epic-1-retro-2025-12-30.md`

### Next Retrospective

**Epic 2 retrospective:** After Epic 2 completion
**Epic 1 lessons:** Apply to Epic 2 planning і execution

---

## Tools and Commands Quick Reference

### BMAD Workflows (Core)

| Workflow | Command | Usage |
|----------|---------|-------|
| Story implementation | `/bmad:bmm:workflows:dev-story` | Execute story tasks |
| Epic/Story planning | `/bmad:bmm:workflows:create-epics-and-stories` | Epic restructuring |
| Code review | `/bmad:bmm:workflows:code-review` | Adversarial review |
| Retrospective | `/bmad:bmm:workflows:retrospective` | Epic completion review |
| Sprint status | `/bmad:bmm:workflows:sprint-status` | Current status check |

### Beads Commands (Essential)

```bash
# Work discovery
bd ready                           # Ready work without blockers
bd ready --parent <epic-id>        # Ready work for specific epic
bd show <id>                       # Issue details
bd list --status <status>          # Filter by status

# Work management
bd update <id> --status <status>   # Update issue status
bd close <id> "reason"             # Close with reason
bd sync                            # Sync beads changes to git

# Stage management (BMAD integration)
_bmad/bin/bd-stage <id> <stage>    # Set exclusive stage
bd comments add <id> "comment"     # Add comments
```

### Epic Management Commands

```bash
# Epic planning review
_bmad/bin/bd-stage <epic-id> planning-review
/bmad:bmm:workflows:create-epics-and-stories

# Epic execution
_bmad/bin/bd-stage <epic-id> ready-for-dev
bd ready --parent <epic-id>
/bmad:bmm:workflows:dev-story

# Epic completion
/bmad:bmm:workflows:retrospective
_bmad/bin/bd-stage <epic-id> done
```

---

## SESSION CLOSE PROTOCOL

```bash
# 1. Verify work status
bd list --status in_progress      # Check in-progress work
bd show <current-work-id>          # Verify completion

# 2. Update stages if needed
_bmad/bin/bd-stage <id> <final-stage>

# 3. Sync all changes
bd sync                            # Beads → git
git status                         # Check working directory
git add <files>                    # Stage code changes
git commit -m "Epic 1 retrospective + Epic 2 planning protocol"
git push                           # Push to remote

# 4. Final validation
/bmad:bmm:workflows:sprint-status  # Confirm current state
```

---

## Project Files Reference

| File | Purpose | Updated |
|------|---------|---------|
| `_bmad-output/prd.md` | Product Requirements | ✅ Complete |
| `_bmad-output/architecture.md` | Architecture Decisions | ✅ Complete |
| `_bmad-output/epics.md` | Epic & Story definitions | ✅ Complete |
| `_bmad-output/sprint-status.yaml` | Sprint tracking (derived) | 🔄 Sync needed |
| `_bmad-output/workflow-guide.md` | **This file** | ✅ Updated |
| `_bmad-output/retrospectives/epic-1-retro-2025-12-30.md` | Epic 1 retrospective | ✅ Generated |

---

## Environment Context

- **Location:** `/data/projects/beads-llm-human`
- **Branch:** `beads-llm-human` (working) → PR to `docs/docusaurus-site`
- **BMAD:** ✅ v6.0.0-alpha.19
- **Beads:** ✅ v0.41.0
- **Upstream:** `joyshmitz/beads` (fork of `steveyegge/beads`)
- **Epic 1:** ✅ COMPLETED (2025-12-30)
- **Epic 2:** 🔄 PHASE 2 RECLASSIFICATION

**Next Actions:**
1. Complete Epic 1 retrospective documentation ✅
2. Execute Epic 2 phase reclassification 🎯
3. Begin Epic 2 (Phase 2) analysis workflow 🔄
4. Create Epic 2 v2.0 (Phase 4) після analysis completion 📋

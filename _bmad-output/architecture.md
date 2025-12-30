---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
status: 'complete'
completedAt: '2025-12-30'
inputDocuments:
  - "_bmad-output/prd.md"
workflowType: 'architecture'
lastStep: 8
project_name: 'Beads Documentation Strategy'
user_name: 'Ubuntu'
date: '2025-12-30'
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
31 FRs across 7 categories. Core focus:
- Human documentation via Docusaurus (FR1-FR7, FR13-FR16, FR21-FR26)
- AI agent documentation via llms.txt standard (FR8-FR12)
- Quality gates via CI/CD (FR17-FR20)

**Non-Functional Requirements:**
14 NFRs driving architecture:
- Performance: <3 min answer discovery, <50K tokens for llms-full.txt
- Readability: Flesch-Kincaid ≤8 (How-to only), ≤2000 words/page
- Reliability: Zero broken links, 100% example validity
- Maintainability: Single Source of Truth architecture

**Scale & Complexity:**
- Primary domain: Static site generation + CI/CD automation
- Complexity level: Medium
- Estimated architectural components: 4 (Docusaurus site, llms.txt generator, CI/CD pipeline, bd prime integration)

### Technical Constraints & Dependencies

1. **Deployment**: steveyegge.github.io via GitHub Pages (not fork deployment)
2. **Toolchain**: Docusaurus for site, custom or plugin for llms.txt generation
3. **CI/CD**: GitHub Actions with path-based triggers on docs/
4. **Beads CLI**: Go ≥1.21, hooks integration for `bd prime`

### Cross-Cutting Concerns Identified

1. **Token budget**: All content must fit llms-full.txt <50K tokens
2. **Diátaxis compliance**: Content must map to Tutorials/How-to/Reference/Explanation
3. **Recovery-first**: Every dangerous operation needs explicit Recovery section
4. **Anti-marketing**: No promotional language in Reference/How-to
5. **Example validity**: All CLI examples must be copy-paste ready with prerequisites

## Starter Template Evaluation

### Primary Technology Domain

Documentation system using existing Docusaurus 3.9.2 infrastructure.

### Existing Setup Analysis

**Already Configured:**
- Docusaurus 3.9.2 with classic preset (TypeScript)
- React 19.0.0 with Node ≥20.0
- Dark mode default with color scheme respect
- Docs-as-homepage configuration
- Partial meta tags for llms.txt discovery

**Requiring Architecture Decision:**
1. Deployment target fix (joyshmitz → steveyegge)
2. llms.txt generation approach
3. Enhanced AI discovery meta tags (BMAD pattern)

### Reference Implementation: BMAD-METHOD

Inspired by [bmad-code-org.github.io/BMAD-METHOD](https://bmad-code-org.github.io/BMAD-METHOD):

| Pattern | Apply to Beads |
|---------|----------------|
| Environment-based URL | `SITE_URL` env var for deployment flexibility |
| 3-tier AI meta tags | ai-terms + llms-full + llms with token budget |
| Token budget disclosure | Document <50K token target in meta |
| Footer llms links | Add llms-full.txt link alongside llms.txt |

### llms.txt Generation Options

| Approach | Pros | Cons |
|----------|------|------|
| @signalwire/docusaurus-plugin-llms-txt | Active maintenance, caching | External dependency |
| Manual CI script | Full control, token budget validation | Custom maintenance |
| BMAD-style build script | Proven pattern, can copy approach | Need to study implementation |

### Architectural Decision: Extend Existing Docusaurus Setup

**Rationale:**
- Already configured with latest Docusaurus 3.9.2
- BMAD patterns provide proven AI discovery approach
- Deployment fix is configuration change
- llms.txt generation is additive

**Note:** First stories should fix deployment config and enhance AI meta tags.

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Block Implementation):**
1. Deployment configuration → Environment variables
2. llms.txt generation → Existing shell script (no plugin needed)
3. AI discovery meta tags → BMAD pattern

**Important Decisions (Shape Architecture):**
4. CI/CD quality gates → All MVP gates

**Deferred Decisions (Post-MVP):**
- Example validation automation (Growth)
- Readability scoring (Growth)
- Versioned documentation (Vision)

### Deployment Configuration

**Decision:** Environment-based URL configuration (BMAD pattern)

**Implementation:**
```javascript
// docusaurus.config.ts
const siteUrl = process.env.SITE_URL || 'https://steveyegge.github.io';
const baseUrl = new URL(siteUrl).pathname || '/beads/';

const config: Config = {
  url: new URL(siteUrl).origin,
  baseUrl: baseUrl,
  organizationName: process.env.GITHUB_ORG || 'steveyegge',
  projectName: 'beads',
  // ...
};
```

**Rationale:** Allows same config to work in forks while defaulting to upstream values.

### llms.txt Generation

**Decision:** Use existing shell script + add CI validation

**Existing Infrastructure (already in repo):**
- `scripts/generate-llms-full.sh` — generates llms-full.txt
- `website/static/llms.txt` — manually curated index
- `website/static/llms-full.txt` — auto-generated (13,690 words ≈ 18K tokens)

**Current state:** Token budget is 18K/50K — well under limit.

**Add to CI:**
```yaml
# Extend .github/workflows/deploy-docs.yml
- name: Validate llms-full.txt token count
  run: |
    TOKEN_COUNT=$(wc -w < website/static/llms-full.txt)
    if [ $TOKEN_COUNT -gt 37500 ]; then
      echo "ERROR: llms-full.txt exceeds 50K token budget"
      exit 1
    fi
```

### AI Discovery Meta Tags

**Decision:** BMAD pattern with token budget disclosure

**Implementation:**
```javascript
headTags: [
  {
    tagName: 'meta',
    attributes: {
      name: 'ai-terms',
      content: 'Load /beads/llms-full.txt (<50K tokens) for complete documentation, /beads/llms.txt for index',
    },
  },
  {
    tagName: 'meta',
    attributes: {
      name: 'llms-full',
      content: '/beads/llms-full.txt',
    },
  },
  {
    tagName: 'meta',
    attributes: {
      name: 'llms',
      content: '/beads/llms.txt',
    },
  },
],
```

**Rationale:** Comprehensive AI discovery following proven BMAD pattern.

### CI/CD Quality Gates

**Decision:** Extend existing `deploy-docs.yml` with validation steps

| Gate | Tool | Trigger | Blocking |
|------|------|---------|----------|
| Link checker | lychee or broken-link-checker | PR + push | Yes |
| Token count | Word count validation | PR + push | Yes |
| Build success | npm run build | PR + push | Yes |

**Deferred to Growth:**
- Example validation (requires Go environment)
- Readability scoring (Flesch-Kincaid integration)

### Decision Impact Analysis

**Implementation Sequence:**
1. Fix deployment URLs (3 files)
2. Update AI discovery meta tags
3. Add CI validation steps to existing workflow
4. Create recovery/ and architecture/ content

**Cross-Component Dependencies:**
- URL fix must happen before PR merge
- Meta tags reference paths that already exist
- CI gates extend existing workflow

## Implementation Patterns & Consistency Rules

### Pattern Categories Defined

**Critical Conflict Points Identified:** 6 areas where AI agents could make different choices

### Content File Naming

**Pattern:** Lowercase kebab-case for all documentation files

**Convention:**
- Files: `getting-started.md`, `cli-reference.md`
- Directories: `getting-started/`, `how-to/`
- URLs: `/getting-started/installation`

**Rationale:** URL-friendly, consistent with web standards.

### CLI Example Format

**Pattern:** Structured format with prerequisites, usage, output, and recovery

**Template:**
```markdown
## Command: bd [command]

**Prerequisites:**
- [Required state or setup]

**Usage:**
```bash
$ bd [command] [options]
```

**Expected output:**
```
[Exact output user should see]
```

**If [error condition]:**
```
Error: "[Exact error message]"
```

**Recovery:** See [Recovery Runbook](/recovery/[topic])
```

**Rationale:** Satisfies PRD requirements FR24-FR26 for copy-paste ready examples.

### Recovery Section Format

**Pattern:** Consistent 5-part structure for all recovery procedures

**Template:**
```markdown
## Recovery: [Problem Name]

### Symptoms
- [Observable symptom 1]
- [Observable symptom 2]

### Diagnosis
```bash
$ [diagnostic command]
```

### Solution
1. [Step with explicit command]
2. [Step with explicit command]
3. [Verification step]

### Prevention
[How to avoid this in future]
```

**Rationale:** Satisfies NFR15 (≤5 steps) and ensures consistency across all recovery docs.

### Diataxis Category Assignment

**Pattern:** Strict category rules for content placement

| Category | Rule | Tone |
|----------|------|------|
| **Tutorial** | Learning-oriented, guided experience | "Let's build..." |
| **How-to** | Task-oriented, problem-solving | "To accomplish X, do Y" |
| **Reference** | Information-oriented, factual | "The command accepts..." |
| **Explanation** | Understanding-oriented, conceptual | "Beads uses Git because..." |

**Anti-marketing rule:** Reference and How-to sections contain NO promotional language. Only factual, operational content.

### Admonition Usage

**Pattern:** Consistent admonition types across all docs

| Admonition | Use For | Required Context |
|------------|---------|------------------|
| `:::tip` | Helpful shortcuts | Optional enhancement |
| `:::note` | Additional context | Prerequisites, versions |
| `:::warning` | Potential issues | Reversible problems |
| `:::danger` | Data loss risk | Irreversible operations |
| `:::info` | Background info | Technical context |

### llms.txt Content Structure

**Pattern:** Follows llmstxt.org specification

**Structure:**
```markdown
# Beads

> Git-backed issue tracker for AI-supervised coding workflows

## Quick Start
[Essential getting started - 3-5 sentences]

## Core Concepts
[Key concepts: beads, molecules, sync, prime]

## CLI Reference
[Command summaries with links to full docs]

## Recovery
[Common issues with quick solutions]

## Optional
- [llms-full.txt](/beads/llms-full.txt): Complete documentation (<50K tokens)
```

### Enforcement Guidelines

**All AI Agents MUST:**
1. Use kebab-case for all new documentation files
2. Include Prerequisites section in every CLI example
3. Follow Recovery Section Format for all troubleshooting content
4. Assign Diataxis category before writing content
5. Use appropriate admonition type (no custom types)
6. Maintain llms.txt structure when adding new sections

**Pattern Verification:**
- CI validates file naming conventions
- PR review checks Diataxis compliance
- Token budget CI gate enforces llms-full.txt size

## Project Structure & Boundaries

### Git Context

**Branch:** `docs/docusaurus-site` (PR #784)
**Author:** Serhii (joyshmitz) — explains joyshmitz.github.io URLs
**Created:** 2025-12-28 (5 commits in one day)
**Upstream:** `steveyegge/beads` main branch (active CLI development)

### Existing Structure (Verified from Repo)

```
website/                              # Docusaurus site (EXISTS)
├── docs/                             # 7 categories (EXISTS)
│   ├── intro.md
│   ├── getting-started/              # [Tutorial] 4 files
│   ├── core-concepts/                # [Explanation] 5 files
│   ├── cli-reference/                # [Reference] 6 files
│   ├── workflows/                    # [How-to] 5 files
│   ├── multi-agent/                  # [Explanation] 3 files
│   ├── integrations/                 # [How-to] 3 files
│   └── reference/                    # [Reference] 5 files
├── static/
│   ├── llms.txt                      # EXISTS (manually curated)
│   └── llms-full.txt                 # EXISTS (13,690 words)
├── src/css/custom.css
├── docusaurus.config.ts              # EXISTS (needs URL fix)
├── sidebars.ts                       # EXISTS (needs new sections)
├── package.json
└── tsconfig.json

scripts/
└── generate-llms-full.sh             # EXISTS (needs URL fix)

.github/workflows/
├── deploy-docs.yml                   # EXISTS (extend with validation)
├── ci.yml
├── release.yml
├── nightly.yml
├── test-pypi.yml
└── update-homebrew.yml

CONTRIBUTING.md                       # EXISTS at repo root (7.6 KB)
```

### Files to Create (NEW)

```
website/docs/
├── recovery/                         # NEW — FR2-FR4
│   ├── index.md                      # Recovery overview
│   ├── database-corruption.md        # FR2
│   ├── merge-conflicts.md            # FR3
│   ├── circular-dependencies.md      # FR4
│   └── sync-failures.md              # Common sync issues
└── architecture/                     # NEW — FR1
    └── index.md                      # Git/JSON/SQLite interaction
```

### Files to Modify (URL Fix)

| File | Line(s) | Change |
|------|---------|--------|
| `docusaurus.config.ts` | 15-18 | joyshmitz → steveyegge |
| `scripts/generate-llms-full.sh` | 18 | joyshmitz → steveyegge |
| `website/static/llms.txt` | 48-51 | joyshmitz → steveyegge |

### Files to Extend

| File | Addition |
|------|----------|
| `deploy-docs.yml` | Add link checker + token validation steps |
| `sidebars.ts` | Add recovery/ and architecture/ categories |
| `docusaurus.config.ts` | BMAD-style AI meta tags |

### Architectural Boundaries

**Content Boundaries (Diátaxis):**

| Directory | Category | Purpose |
|-----------|----------|---------|
| `getting-started/` | Tutorial | Learning-oriented |
| `workflows/`, `integrations/`, `recovery/` | How-to | Task-oriented |
| `cli-reference/`, `reference/` | Reference | Information-oriented |
| `core-concepts/`, `multi-agent/`, `architecture/` | Explanation | Understanding-oriented |

**Generation Boundaries:**

| Artifact | Source | Generator |
|----------|--------|-----------|
| Docusaurus site | `docs/*.md` | `npm run build` |
| `llms-full.txt` | All docs | `scripts/generate-llms-full.sh` |
| `llms.txt` | Manual | Human-curated |

**CI/CD Boundaries:**

| Stage | Trigger | Workflow |
|-------|---------|----------|
| Build | Push to docs/ | `deploy-docs.yml` |
| Validate | PR + push | Same workflow (extended) |
| Deploy | Push to main | GitHub Pages |

### Requirements to Structure Mapping

| PRD Requirement | Implementation |
|-----------------|----------------|
| FR1: Architecture.md | `docs/architecture/index.md` (create) |
| FR2-4: Recovery Runbook | `docs/recovery/*.md` (create) |
| FR5-7: CLI Reference | `docs/cli-reference/` (exists) |
| FR8-12: AI Agent Docs | `static/llms*.txt` (exists, fix URLs) |
| FR13: steveyegge.github.io | Config fix (3 files) |
| FR17-20: Quality Gates | Extend `deploy-docs.yml` |

## Architecture Validation Results

### Coherence Validation ✅

**Decision Compatibility:**
All technology choices work together without conflicts:
- Docusaurus 3.9.2 + Node 20 + TypeScript — verified in package.json
- Shell script for llms.txt generation — simple, no external dependencies
- GitHub Actions for CI/CD — existing workflow extends cleanly

**Pattern Consistency:**
- Kebab-case naming aligns with web URL standards
- Diátaxis categories map cleanly to directory structure
- CLI example format supports recovery links

**Structure Alignment:**
- Project structure verified against actual repo contents
- All proposed new files fit existing organization
- No structural conflicts with existing docs

### Requirements Coverage Validation ✅

**Functional Requirements Coverage:**

| Category | Covered | Deferred |
|----------|---------|----------|
| Documentation Content (FR1-FR7) | 6/7 | FR6 (examples detail) |
| AI Agent Documentation (FR8-FR12) | 5/5 | — |
| Documentation Delivery (FR13-FR16) | 4/4 | — |
| Quality Assurance (FR17-FR20) | 4/4 | — |
| Developer Experience (FR21-FR26) | 4/6 | FR22, FR25 (Growth) |
| Metrics (FR27-FR28) | 0/2 | Growth phase |
| Error Handling (FR29-FR31) | 3/3 | — |

**Total:** 26/31 MVP, 5 deferred to Growth

**Non-Functional Requirements Coverage:**

| NFR | Status |
|-----|--------|
| NFR1-3: Performance | ✅ Token budget 18K/50K |
| NFR4-6: Readability | ⚠️ Flesch-Kincaid deferred |
| NFR7-10: Reliability | ✅ CI gates defined |
| NFR11-14: Maintainability | ✅ Single source of truth |

### Implementation Readiness Validation ✅

**Decision Completeness:**
- All critical decisions documented with code examples
- Technology versions verified against actual package.json
- No placeholder decisions — all based on repo facts

**Structure Completeness:**
- Complete directory structure with specific files
- Files to create, modify, and extend clearly identified
- Git context provides implementation history

**Pattern Completeness:**
- 6 implementation patterns with templates
- Enforcement guidelines for AI agents
- CI validation for pattern compliance

### Gap Analysis Results

**Critical Gaps:** None

**Important Gaps:**
1. Section "llms.txt Generation Options" references plugin decision that was superseded — cosmetic inconsistency, doesn't affect implementation
2. `bd prime` integration mentioned in PRD not fully architected — defer to epic planning

**Nice-to-Have:**
- Sidebar grouping (9 categories approaching usability limit)
- "For AI Agents" explanatory page in docs

### Architecture Completeness Checklist

**✅ Requirements Analysis**
- [x] Project context analyzed with git history
- [x] Scale and complexity assessed (Medium)
- [x] Technical constraints identified (Docusaurus 3.9.2, Node 20)
- [x] Cross-cutting concerns mapped (token budget, Diátaxis, recovery-first)

**✅ Architectural Decisions**
- [x] Critical decisions documented (deployment, llms.txt, meta tags, CI gates)
- [x] Technology stack verified from package.json
- [x] Integration patterns defined (shell script, GitHub Actions)
- [x] Performance considerations addressed (18K/50K tokens)

**✅ Implementation Patterns**
- [x] Naming conventions established (kebab-case)
- [x] Structure patterns defined (Diátaxis categories)
- [x] Content patterns specified (CLI examples, recovery sections)
- [x] Enforcement guidelines documented

**✅ Project Structure**
- [x] Complete directory structure verified from repo
- [x] Component boundaries established (docs, static, scripts, workflows)
- [x] Files to create/modify/extend explicitly listed
- [x] Requirements to structure mapping complete

### Architecture Readiness Assessment

**Overall Status:** READY FOR IMPLEMENTATION

**Confidence Level:** HIGH — based on verified repo facts and git history

**Key Strengths:**
1. Existing infrastructure (Docusaurus, llms.txt, deploy workflow) reduces work
2. Clear URL fix scope (3 files, specific lines)
3. Token budget well under limit (18K/50K)
4. BMAD patterns provide proven reference

**Areas for Future Enhancement:**
1. Sidebar reorganization when categories exceed 9
2. Flesch-Kincaid readability gate in Growth phase
3. Example validation automation with Go environment

### Implementation Handoff

**AI Agent Guidelines:**
1. Follow kebab-case naming for all new files
2. Use CLI Example Format template for all commands
3. Use Recovery Section Format for troubleshooting content
4. Respect Diátaxis category assignments
5. Run `scripts/generate-llms-full.sh` after adding docs

**First Implementation Priorities:**
1. Fix URLs in 3 files (joyshmitz → steveyegge)
2. Create `docs/recovery/` section (4 files)
3. Create `docs/architecture/index.md`
4. Update `sidebars.ts` with new sections
5. Extend `deploy-docs.yml` with validation steps

## Architecture Completion Summary

### Workflow Completion

**Architecture Decision Workflow:** COMPLETED
**Total Steps Completed:** 8
**Date Completed:** 2025-12-30
**Document Location:** `_bmad-output/architecture.md`

### Final Architecture Deliverables

**Complete Architecture Document**
- All architectural decisions documented with specific versions
- Implementation patterns ensuring AI agent consistency
- Complete project structure verified from actual repo
- Requirements to architecture mapping with PRD traceability
- Validation confirming coherence and completeness

**Implementation Ready Foundation**
- 4 architectural decisions made (deployment, llms.txt, meta tags, CI gates)
- 6 implementation patterns defined (naming, CLI examples, recovery, Diátaxis, admonitions, llms.txt structure)
- 4 architectural components (Docusaurus site, llms.txt generator, CI/CD pipeline, docs content)
- 26/31 FRs supported in MVP, 5 deferred to Growth

**AI Agent Implementation Guide**
- Technology stack verified from package.json
- Consistency rules that prevent implementation conflicts
- Project structure with clear Diátaxis boundaries
- Shell script and GitHub Actions integration patterns

### Development Sequence

1. Fix URLs in 3 files (joyshmitz → steveyegge)
2. Create `docs/recovery/` section with 4 files
3. Create `docs/architecture/index.md`
4. Update `sidebars.ts` with new sections
5. Extend `deploy-docs.yml` with link checker + token validation
6. Run `scripts/generate-llms-full.sh` to update llms-full.txt
7. Test local build with `npm run build`
8. Submit PR for review

### Quality Assurance Checklist

**Architecture Coherence**
- [x] All decisions work together without conflicts
- [x] Technology choices verified from actual repo
- [x] Patterns support the architectural decisions
- [x] Structure aligns with existing organization

**Requirements Coverage**
- [x] 26/31 functional requirements supported in MVP
- [x] 12/14 non-functional requirements addressed
- [x] Cross-cutting concerns handled (token budget, Diátaxis, recovery-first)
- [x] Integration points defined (shell script, GitHub Actions)

**Implementation Readiness**
- [x] Decisions based on repo facts, not assumptions
- [x] Patterns prevent agent conflicts
- [x] Structure verified against actual files
- [x] Git history provides implementation context

---

**Architecture Status:** READY FOR IMPLEMENTATION

**Next Phase:** Create epics and stories using `/bmad:bmm:workflows:create-epics-and-stories`


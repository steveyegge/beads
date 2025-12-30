---
stepsCompleted: [1, 2, 3]
inputDocuments:
  - "_bmad-output/prd.md"
  - "_bmad-output/architecture.md"
  - "_bmad-output/project-context.md"
project_name: 'Beads Documentation Strategy'
user_name: 'Ubuntu'
date: '2025-12-30'
---

# Beads Documentation Strategy - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for Beads Documentation Strategy, decomposing the requirements from the PRD, Architecture, and Project Context into implementable stories.

## Requirements Inventory

### Functional Requirements

**Documentation Content:**
- FR1: Developer can read Architecture.md for Git/JSON/SQLite interaction understanding
- FR2: Developer can find Recovery Runbook for database corruption
- FR3: Developer can find Recovery Runbook for merge conflicts
- FR4: Developer can find Recovery Runbook for circular dependencies
- FR5: Developer can view CLI command reference with all bd commands
- FR6: Developer can copy working example for each CLI command
- FR7: Contributor can read CONTRIBUTING.md for contribution process understanding

**AI Agent Documentation:**
- FR8: AI agent can get llms.txt for conceptual understanding of Beads
- FR9: AI agent can get llms-full.txt for full context (<50K tokens)
- FR10: AI agent can get bd prime output for operational context
- FR11: AI agent can find Recovery section in llms.txt for troubleshooting
- FR12: AI agent can identify SESSION CLOSE PROTOCOL from bd prime

**Documentation Delivery:**
- FR13: User can view documentation on steveyegge.github.io
- FR14: User can navigate documentation in ≤3 clicks to critical information
- FR15: User can search content in documentation
- FR16: System automatically generates llms-full.txt on commit to docs/

**Quality Assurance:**
- FR17: CI checks all links for validity (zero broken links)
- FR18: CI validates llms.txt format (lint check)
- FR19: CI checks llms-full.txt size (<50K tokens)
- FR20: Maintainer can view PR preview before merge

**Developer Experience:**
- FR21: Developer can install Beads via `go install`
- FR22: Developer can download binary release from GitHub
- FR23: IDE user can get AI context by calling bd prime via hook/integration
- FR24: Developer sees prerequisites for each code example
- FR25: Developer sees expected output for each code example
- FR26: Developer sees failure scenarios and recovery for each command

**Metrics & Measurement:**
- FR27: System stores baseline measurement of GitHub issues with 'documentation' label
- FR28: Maintainer can compare current issues with baseline

**Error Handling & Resilience:**
- FR29: System shows clear error message when .beads/ doesn't exist
- FR30: IDE gracefully degrades when bd prime fails
- FR31: Each documentation page doesn't exceed 2000 words

### NonFunctional Requirements

**Performance:**
- NFR1: Answer search time <3 minutes for typical question
- NFR2: llms-full.txt size <50K tokens
- NFR3: llms.txt size <10KB
- NFR4: Navigation to critical information ≤3 clicks

**Accessibility & Readability:**
- NFR5: Readability score Flesch-Kincaid ≤8 (How-to docs only)
- NFR6: Cognitive load ≤2000 words per page (How-to, Tutorials, Recovery)
- NFR7: Exemptions for Reference, Architecture, Explanation (may exceed limits)

**Integration & Deployment:**
- NFR8: End-to-end deployment Commit → live site <10 minutes
- NFR9: llms-full.txt generation automatic on commit to docs/

**Reliability:**
- NFR10: Link validity 100% links working
- NFR11: Example validity 100% code examples work
- NFR12: Build reproducibility Same commit = same output

**Maintainability:**
- NFR13: Single Source of Truth one Markdown → all output formats
- NFR14: CI failure notification maintainer alert on failed build

**Usability:**
- NFR15: Recovery procedure length ≤5 steps for each failure scenario

### Additional Requirements

**From Architecture - Deployment Fix (Critical):**
- 3 files contain wrong URLs (joyshmitz → steveyegge):
  - `website/docusaurus.config.ts` lines 15-18
  - `scripts/generate-llms-full.sh` line 18
  - `website/static/llms.txt` lines 48-51

**From Architecture - Environment-based Configuration:**
- SITE_URL environment variable for deployment flexibility
- Same config works in forks while defaulting to upstream values

**From Architecture - AI Discovery Meta Tags (BMAD Pattern):**
- `ai-terms` meta tag with token budget disclosure
- `llms-full` meta tag pointing to /beads/llms-full.txt
- `llms` meta tag pointing to /beads/llms.txt

**From Architecture - CI/CD Quality Gates:**
- Link checker (lychee or broken-link-checker) - blocking
- Token count validation for llms-full.txt - blocking
- Build success validation - blocking

**From Architecture - Files to Create:**
- `docs/recovery/index.md` - Recovery overview
- `docs/recovery/database-corruption.md` - FR2
- `docs/recovery/merge-conflicts.md` - FR3
- `docs/recovery/circular-dependencies.md` - FR4
- `docs/recovery/sync-failures.md` - Common sync issues
- `docs/architecture/index.md` - Git/JSON/SQLite interaction (FR1)

**From Architecture - Files to Extend:**
- `deploy-docs.yml` - Add link checker + token validation steps
- `sidebars.ts` - Add recovery/ and architecture/ categories

**From Architecture - Implementation Patterns:**
- Kebab-case file naming for all documentation files
- CLI example format: Prerequisites → Usage → Expected output → Error handling → Recovery link
- Recovery section format: Symptoms → Diagnosis → Solution (≤5 steps) → Prevention
- Diátaxis category assignment before writing content
- Admonition usage: tip, note, warning, danger, info (no custom types)
- llms.txt structure following llmstxt.org spec

**From Project Context - Technical Constraints:**
- Go 1.24+ required for CLI
- Node.js ≥20.0 required for website
- Docusaurus 3.9.2 with classic preset
- llms-full.txt must stay under 50K tokens (~37,500 words)
- Current token usage: ~18K tokens (well under limit)

**From Project Context - Workflow:**
- PR-based workflow: `docs/docusaurus-site` branch → PR to `main`
- Files NOT committed: `_bmad/`, `_bmad-output/`, `node_modules/`

### FR Coverage Map

| FR | Epic | Description |
|----|------|-------------|
| FR1 | Epic 3 | Architecture.md for Git/JSON/SQLite |
| FR2 | Epic 2 | Recovery Runbook - database corruption |
| FR3 | Epic 2 | Recovery Runbook - merge conflicts |
| FR4 | Epic 2 | Recovery Runbook - circular dependencies |
| FR5 | — | CLI reference exists (enhancement deferred) |
| FR6 | — | Working examples exist (enhancement deferred) |
| FR7 | — | CONTRIBUTING.md exists |
| FR8 | Epic 4 | llms.txt for conceptual understanding |
| FR9 | Epic 4 | llms-full.txt for full context |
| FR10 | Epic 4 | bd prime output (docs reference) |
| FR11 | Epic 4 | Recovery section in llms.txt |
| FR12 | Epic 4 (Story 4.2) | SESSION CLOSE PROTOCOL reference in llms.txt |
| FR13 | Epic 1 | steveyegge.github.io deployment |
| FR14 | — | Navigation ≤3 clicks (Diátaxis deferred) |
| FR15 | — | Search (Docusaurus default) |
| FR16 | Epic 5 | Auto-generate llms-full.txt |
| FR17 | Epic 5 | CI link checker |
| FR18 | Epic 5 | llms.txt lint check |
| FR19 | Epic 5 | Token count validation |
| FR20 | Epic 1 | PR preview before merge |
| FR21-FR26 | — | Developer experience (exists/deferred) |
| FR27-FR28 | — | Metrics (Growth phase) |
| FR29-FR31 | — | Error handling (exists in CLI) |

**MVP Coverage:** 16 FRs directly addressed across 5 epics
**Deferred:** 15 FRs (existing functionality or Growth phase)

## Epic List

### Epic 1: Foundation & Deployment
**User Outcome:** Documentation is accessible on steveyegge.github.io with correct configuration

**FRs covered:** FR13, FR20
**NFRs covered:** NFR8, NFR12
**Additional:** URL fix (3 files), environment-based config, sidebar updates

### Epic 2: Recovery Documentation
**User Outcome:** Developers can diagnose and resolve common Beads issues in ≤5 steps

**FRs covered:** FR2, FR3, FR4, FR26
**NFRs covered:** NFR15

### Epic 3: Architecture Documentation
**User Outcome:** Developers understand Git/JSON/SQLite interaction and can make informed decisions

**FRs covered:** FR1
**NFRs covered:** NFR7 (exempt from word limit)

### Epic 4: AI Agent Documentation
**User Outcome:** AI agents can get full project context in one request (<50K tokens)

**FRs covered:** FR8, FR9, FR10, FR11, FR12
**NFRs covered:** NFR2, NFR3

### Epic 5: Quality Assurance Pipeline
**User Outcome:** Maintainers can trust documentation quality with automated validation

**FRs covered:** FR16, FR17, FR18, FR19
**NFRs covered:** NFR10, NFR14

### Epic Dependencies

```
Epic 1 (Foundation) ← Epic 2 (Recovery) ← Epic 3 (Architecture) ← Epic 4 (AI Docs) ← Epic 5 (QA)
```

| Epic | Depends On | Rationale |
|------|------------|-----------|
| Epic 1 | — | Foundation, no dependencies |
| Epic 2 | Epic 1 | Recovery docs need deployed site |
| Epic 3 | Epic 2 | Architecture refs recovery procedures |
| Epic 4 | Epic 3 | llms.txt includes recovery + architecture content |
| Epic 5 | Epic 4 | QA gates validate all generated content |

**Note:** Beads issues (`bd-fyy`, `bd-9g9`, `bd-gg5`, `bd-907`, `bd-yip`) encode these dependencies.

---

## Epic 1: Foundation & Deployment [bd-fyy] ✅ CLOSED

Documentation is accessible on steveyegge.github.io with correct configuration, enabling PR #784 merge.

### Story 1.1: Fix Deployment URLs [bd-fyy.1] ✅ CLOSED

As a **maintainer**,
I want **correct deployment URLs in configuration files**,
So that **documentation deploys to steveyegge.github.io instead of joyshmitz.github.io**.

**Acceptance Criteria:**

**Given** the current configuration has joyshmitz URLs
**When** I update the 3 files with steveyegge URLs
**Then** `website/docusaurus.config.ts` lines 15-18 reference steveyegge
**And** `scripts/generate-llms-full.sh` line 18 references steveyegge
**And** `website/static/llms.txt` lines 48-51 reference steveyegge
**And** `npm run build` succeeds without errors

---

### Story 1.2: Add Environment-Based URL Configuration [bd-fyy.2] ✅ CLOSED

As a **fork maintainer**,
I want **environment-based URL configuration**,
So that **the same config works in forks while defaulting to upstream values**.

**Acceptance Criteria:**

**Given** the docusaurus.config.ts uses hardcoded URLs
**When** I add SITE_URL environment variable support
**Then** `SITE_URL` env var overrides the default URL
**And** default URL is `https://steveyegge.github.io`
**And** baseUrl is derived from SITE_URL pathname or defaults to `/beads/`
**And** organizationName defaults to `steveyegge` but can be overridden
**And** build succeeds with and without SITE_URL set

---

### Story 1.3: Update Sidebar Navigation [bd-fyy.3] ✅ CLOSED

As a **documentation reader**,
I want **recovery and architecture sections in navigation**,
So that **I can find troubleshooting and architectural information easily**.

**Acceptance Criteria:**

**Given** sidebars.ts has current navigation structure
**When** I add recovery/ and architecture/ categories
**Then** sidebar shows "Recovery" section under How-to category
**And** sidebar shows "Architecture" section under Explanation category
**And** navigation follows Diátaxis category rules
**And** build succeeds with updated sidebar

---

## Epic 2: Recovery Documentation Analysis & Knowledge Gathering [bd-9g9] 🔄 PHASE 2 (ANALYSIS)

> **Phase Reclassification Notice:** Epic 2 moved from Phase 4 (Implementation) to Phase 2 (Analysis/Solutioning).
> Stories focus on GitHub issues data mining and pattern extraction to build knowledge base for future implementation.

Analyze GitHub issues to extract real failure patterns and solutions, creating a knowledge base for data-driven recovery documentation in Epic 2 v2.0.

### Story 2.1: GitHub Issues Mining - Database Corruption Patterns [bd-9g9.1]

As a **documentation author**,
I want **extracted database corruption patterns from GitHub issues**,
So that **recovery documentation is based on real user experiences, not guesswork**.

**Acceptance Criteria:**

**Given** access to beads GitHub repository (363+ closed issues)
**When** I analyze issues related to database corruption, SQLite errors, and .beads/beads.db problems
**Then** pattern document captures:
- Common symptoms reported by users
- Root causes identified in issue discussions
- Solutions that worked (with bd commands used)
- Prevention strategies mentioned
**And** minimum 10 real issues analyzed and documented
**And** patterns categorized by severity and frequency
**And** output saved to `_bmad-output/research/database-corruption-patterns.md`

---

### Story 2.2: GitHub Issues Mining - Merge Conflicts Patterns [bd-9g9.2]

As a **documentation author**,
I want **extracted merge conflict patterns from GitHub issues**,
So that **recovery documentation addresses real JSONL and sync merge scenarios**.

**Acceptance Criteria:**

**Given** access to beads GitHub repository issues
**When** I analyze issues related to merge conflicts, JSONL conflicts, and git sync problems
**Then** pattern document captures:
- Conflict symptoms and error messages
- Workflow scenarios that trigger conflicts
- Resolution strategies that worked
- `bd sync` options and their effects
**And** minimum 10 real issues analyzed and documented
**And** patterns include multi-agent and team collaboration scenarios
**And** output saved to `_bmad-output/research/merge-conflicts-patterns.md`

---

### Story 2.3: GitHub Issues Mining - Circular Dependencies Patterns [bd-9g9.3]

As a **documentation author**,
I want **extracted circular dependency patterns from GitHub issues**,
So that **recovery documentation helps users detect and break dependency cycles**.

**Acceptance Criteria:**

**Given** access to beads GitHub repository issues
**When** I analyze issues related to circular dependencies, blocked issues, and dependency errors
**Then** pattern document captures:
- Cycle detection error messages
- Scenarios that create circular dependencies
- `bd blocked` and `bd dep` command usage patterns
- Cycle-breaking strategies that worked
**And** minimum 5 real issues analyzed (if available, may be fewer)
**And** patterns include prevention best practices
**And** output saved to `_bmad-output/research/circular-dependencies-patterns.md`

---

### Story 2.4: GitHub Issues Mining - Sync Failures Patterns [bd-9g9.4]

As a **documentation author**,
I want **extracted sync failure patterns from GitHub issues**,
So that **recovery documentation covers daemon issues, network problems, and state recovery**.

**Acceptance Criteria:**

**Given** access to beads GitHub repository issues
**When** I analyze issues related to `bd sync` failures, daemon crashes, and state corruption
**Then** pattern document captures:
- Common sync error messages
- Network vs state vs daemon failure categories
- `--force-rebuild` and restart procedures
- Permission and configuration issues
**And** minimum 10 real issues analyzed and documented
**And** patterns include daemon management best practices
**And** output saved to `_bmad-output/research/sync-failures-patterns.md`

---

### Story 2.5: Analysis Synthesis + Recovery Framework Design [bd-9g9.5]

As a **documentation architect**,
I want **synthesized analysis results and a recovery documentation framework**,
So that **Epic 2 v2.0 implementation has clear structure and data-driven content**.

**Acceptance Criteria:**

**Given** completed pattern analysis from Stories 2.1-2.4
**When** I synthesize findings and design documentation framework
**Then** synthesis document includes:
- Cross-pattern insights and common themes
- Severity ranking of issue categories
- Solution effectiveness analysis
- Recommended documentation structure for Epic 2 v2.0
**And** recovery framework defines:
- Consistent format for all recovery runbooks
- Symptom → Diagnosis → Solution flow template
- Prevention checklist template
**And** output saved to `_bmad-output/research/recovery-framework-design.md`
**And** Epic 2 v2.0 stories can be derived from this framework

---

## Epic 3: Architecture Documentation [bd-gg5]

Developers understand Git/JSON/SQLite interaction and can make informed decisions about beads usage.

### Story 3.1: Architecture Overview Document [bd-gg5.1]

As a **developer evaluating beads**,
I want **clear architecture documentation**,
So that **I understand how Git, JSONL, and SQLite work together**.

**Acceptance Criteria:**

**Given** no architecture documentation exists
**When** I create `docs/architecture/index.md`
**Then** document explains the three-layer data model (Git → JSONL → SQLite)
**And** explains why each layer exists and its tradeoffs
**And** includes data flow diagram or clear explanation
**And** covers sync mechanism between layers
**And** explains daemon role and when it's used
**And** follows Diátaxis Explanation category (understanding-oriented)
**And** may exceed 2000 words (NFR7 exemption)

---

## Epic 4: AI Agent Documentation [bd-907]

AI agents can get full project context in one request (<50K tokens) following llmstxt.org standard.

### Story 4.1: Add BMAD-Style AI Meta Tags [bd-907.1]

As an **AI agent discovering beads documentation**,
I want **proper meta tags for AI discovery**,
So that **I can find llms.txt and llms-full.txt automatically**.

**Acceptance Criteria:**

**Given** docusaurus.config.ts has minimal meta tags
**When** I add BMAD-style AI meta tags
**Then** `ai-terms` meta tag includes token budget disclosure
**And** `llms-full` meta tag points to `/beads/llms-full.txt`
**And** `llms` meta tag points to `/beads/llms.txt`
**And** meta content follows BMAD pattern from architecture.md "AI Discovery Meta Tags" section
**And** build succeeds with new meta tags

---

### Story 4.2: Add Recovery Section to llms.txt [bd-907.2]

As an **AI agent troubleshooting beads issues**,
I want **a Recovery section in llms.txt**,
So that **I can help users resolve common problems**.

**Acceptance Criteria:**

**Given** llms.txt has no Recovery section
**When** I update `website/static/llms.txt`
**Then** Recovery section lists common issues with quick solutions
**And** links to full recovery runbooks
**And** SESSION CLOSE PROTOCOL reference included for AI agent session management (FR12)
**And** follows llmstxt.org specification structure
**And** llms.txt remains under 10KB (NFR3)

---

### Story 4.3: Regenerate llms-full.txt [bd-907.3]

As a **documentation maintainer**,
I want **updated llms-full.txt with new content**,
So that **AI agents have complete documentation context**.

**Acceptance Criteria:**

**Given** new recovery and architecture docs exist
**When** I run `scripts/generate-llms-full.sh`
**Then** llms-full.txt includes all new documentation
**And** recovery section is included
**And** architecture section is included
**And** file is under 50K tokens (~37,500 words) (NFR2)
**And** URL references use steveyegge.github.io

---

## Epic 5: Quality Assurance Pipeline [bd-yip]

Maintainers can trust documentation quality with automated validation in CI.

### Story 5.1: Add Link Checker to CI [bd-yip.1]

As a **maintainer reviewing PRs**,
I want **automated link checking in CI**,
So that **broken links are caught before merge**.

**Acceptance Criteria:**

**Given** deploy-docs.yml has no link validation
**When** I add lychee link checker step
**Then** CI fails on broken internal links
**And** CI warns on broken external links
**And** check runs on PR and push to docs/
**And** results are visible in PR checks

---

### Story 5.2: Add Token Count Validation [bd-yip.2]

As a **maintainer protecting token budget**,
I want **automated token count validation**,
So that **llms-full.txt stays under 50K tokens**.

**Acceptance Criteria:**

**Given** deploy-docs.yml has no token validation
**When** I add word count check step
**Then** CI fails if llms-full.txt exceeds 37,500 words
**And** current word count is reported in CI output
**And** check runs after llms-full.txt generation

---

### Story 5.3: Add llms.txt Lint Check [bd-yip.3]

As a **maintainer ensuring llms.txt quality**,
I want **automated llms.txt validation**,
So that **format and size requirements are enforced**.

**Acceptance Criteria:**

**Given** deploy-docs.yml has no llms.txt validation
**When** I add llms.txt lint step
**Then** CI fails if llms.txt exceeds 10KB (NFR3)
**And** CI validates required sections exist (Quick Start, Core Concepts, etc.)
**And** check runs on PR and push to docs/

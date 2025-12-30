# Requirements Inventory

## Functional Requirements

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

## NonFunctional Requirements

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

## Additional Requirements

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

## FR Coverage Map

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

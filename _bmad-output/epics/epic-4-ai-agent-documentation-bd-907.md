# Epic 4: AI Agent Documentation [bd-907]

AI agents can get full project context in one request (<50K tokens) following llmstxt.org standard.

## Story 4.1: Add BMAD-Style AI Meta Tags [bd-907.1]

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

## Story 4.2: Add Recovery Section to llms.txt [bd-907.2]

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

## Story 4.3: Regenerate llms-full.txt [bd-907.3]

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

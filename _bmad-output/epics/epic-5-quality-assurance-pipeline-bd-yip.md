# Epic 5: Quality Assurance Pipeline [bd-yip]

Maintainers can trust documentation quality with automated validation in CI.

## Story 5.1: Add Link Checker to CI [bd-yip.1]

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

## Story 5.2: Add Token Count Validation [bd-yip.2]

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

## Story 5.3: Add llms.txt Lint Check [bd-yip.3]

As a **maintainer ensuring llms.txt quality**,
I want **automated llms.txt validation**,
So that **format and size requirements are enforced**.

**Acceptance Criteria:**

**Given** deploy-docs.yml has no llms.txt validation
**When** I add llms.txt lint step
**Then** CI fails if llms.txt exceeds 10KB (NFR3)
**And** CI validates required sections exist (Quick Start, Core Concepts, etc.)
**And** check runs on PR and push to docs/

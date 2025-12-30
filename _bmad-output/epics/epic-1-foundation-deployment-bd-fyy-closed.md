# Epic 1: Foundation & Deployment [bd-fyy] ✅ CLOSED

Documentation is accessible on steveyegge.github.io with correct configuration, enabling PR #784 merge.

## Story 1.1: Fix Deployment URLs [bd-fyy.1] ✅ CLOSED

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

## Story 1.2: Add Environment-Based URL Configuration [bd-fyy.2] ✅ CLOSED

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

## Story 1.3: Update Sidebar Navigation [bd-fyy.3] ✅ CLOSED

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

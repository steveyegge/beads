# Story 4.1: Add BMAD-Style AI Meta Tags

Status: review

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- Beads is the source of truth for task status. Update via bd commands. -->

**Beads IDs:**

- Epic: `bd-907`
- Story: `bd-907.1`

**Quick Commands:**

- View tasks: `bd list --parent bd-907.1`
- Find ready work: `bd ready --parent bd-907.1`
- Mark task done: `bd close <task_id>`

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an **AI agent discovering beads documentation**,
I want **proper meta tags for AI discovery**,
So that **I can find llms.txt and llms-full.txt automatically**.

## Acceptance Criteria

1. **Given** docusaurus.config.ts has minimal meta tags
   **When** I add BMAD-style AI meta tags
   **Then** `ai-terms` meta tag includes token budget disclosure

2. **And** `llms-full` meta tag points to `${baseUrl}llms-full.txt`

3. **And** `llms` meta tag points to `${baseUrl}llms.txt` (**CURRENTLY MISSING**)

4. **And** meta content follows BMAD pattern from architecture.md "AI Discovery Meta Tags" section

5. **And** build succeeds with new meta tags (`npm run build`)

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- The checkboxes below are for reference only. Use bd commands to update.    -->
<!-- To view current status: bd list --parent bd-907.1                          -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

- [x] **Task 1: Analyze current meta tags implementation** (AC: 1, 4) `bd-907.1.1`
  - [x] Subtask 1.1: Read current headTags in docusaurus.config.ts (lines 58-73)
  - [x] Subtask 1.2: Compare with BMAD pattern from architecture.md
  - [x] Subtask 1.3: Identify gaps (missing `llms` tag, token budget format)
  - [x] Subtask 1.4: **CRITICAL**: Note current WRONG order (llms-full first, ai-terms second) - must reorder

- [x] **Task 2: Add missing `llms` meta tag** (AC: 3) `bd-907.1.2`
  - [x] Subtask 2.1: Add `llms` meta tag pointing to `${baseUrl}llms.txt`
  - [x] Subtask 2.2: Verify format matches BMAD pattern

- [x] **Task 3: Enhance `ai-terms` meta tag** (AC: 1, 4) `bd-907.1.3`
  - [x] Subtask 3.1: Update content to include token budget (<50K tokens)
  - [x] Subtask 3.2: Add reference to both llms.txt and llms-full.txt
  - [x] Subtask 3.3: Match exact BMAD pattern format

- [x] **Task 4: Validate build and test** (AC: 5) `bd-907.1.4`
  - [x] Subtask 4.1: Run `npm run build` in website/ directory
  - [x] Subtask 4.2: Verify meta tags appear in generated HTML
  - [x] Subtask 4.3: Test with `npm run serve` locally

## Dev Notes

### Architecture Adaptation Note

Architecture.md shows hardcoded paths (`/beads/llms-full.txt`). This implementation uses `${baseUrl}` for fork flexibility, following the existing pattern in docusaurus.config.ts (lines 5-25). This maintains environment configurability per project-context.md rules and is the CORRECT approach.

### Gap Analysis

| Meta Tag | Current State | Required State | Action |
|----------|---------------|----------------|--------|
| `ai-terms` | Exists (no token budget, WRONG position) | Token budget + both files, FIRST position | Update content + reorder |
| `llms-full` | Exists (FIRST position) | Exists (SECOND position) | Reorder only |
| `llms` | **MISSING** | Points to llms.txt (THIRD position) | Add new tag |

### Implementation Approach

1. **Keep environment flexibility** - use `${baseUrl}` instead of hardcoded `/beads/`
2. **Add `llms` meta tag** - point to llms.txt for index
3. **Enhance `ai-terms`** - include token budget disclosure and reference both files
4. **CRITICAL: Reorder tags** - ai-terms MUST come first (logical AI discovery order)
5. Final order: ai-terms → llms-full → llms

### Project Structure

- **File to modify**: `website/docusaurus.config.ts`
- **Lines to change**: 58-73 (headTags section)
- **No new files required**

### Expected Transformation

**File**: `website/docusaurus.config.ts` lines 58-73

**BEFORE** (current - WRONG order):
```typescript
headTags: [
  { tagName: 'meta', attributes: { name: 'llms-full', content: `${baseUrl}llms-full.txt` } },
  { tagName: 'meta', attributes: { name: 'ai-terms', content: `Load ${baseUrl}llms-full.txt for complete documentation` } },
],
```

**AFTER** (required - CORRECT order + new tag):
```typescript
headTags: [
  {
    tagName: 'meta',
    attributes: {
      name: 'ai-terms',
      content: `Load ${baseUrl}llms-full.txt (<50K tokens) for complete documentation, ${baseUrl}llms.txt for index`,
    },
  },
  {
    tagName: 'meta',
    attributes: {
      name: 'llms-full',
      content: `${baseUrl}llms-full.txt`,
    },
  },
  {
    tagName: 'meta',
    attributes: {
      name: 'llms',
      content: `${baseUrl}llms.txt`,
    },
  },
],
```

### Testing Requirements

```bash
# Build validation
cd website && npm run build

# Verify meta tags in generated HTML (check order: ai-terms, llms-full, llms)
grep -E "ai-terms|llms-full|llms" website/build/index.html

# Local test (optional)
cd website && npm run serve
```

### References

- [architecture.md#AI Discovery Meta Tags] - BMAD pattern specification
- [epic-4-ai-agent-documentation-bd-907.md#Story 4.1] - Acceptance criteria
- [project-context.md#Docusaurus Framework] - Framework rules

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

N/A

### Completion Notes List

- ✅ Analyzed current headTags: found wrong order (llms-full → ai-terms) and missing llms tag
- ✅ Reordered tags: ai-terms (first) → llms-full → llms (discovery order)
- ✅ Enhanced ai-terms: added token budget (<50K tokens) and reference to both files
- ✅ Added new llms meta tag pointing to llms.txt
- ✅ Build successful with new meta tags
- ✅ Verified meta tags in generated HTML (website/build/index.html)

### File List

- `website/docusaurus.config.ts` - Modified headTags section (lines 57-81)

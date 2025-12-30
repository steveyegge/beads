# Story 3.2: Architecture Documentation Refinements

Status: backlog

**Beads IDs:**
- Epic: `bd-gg5`
- Story: `bd-gg5.2`

## Story

As a **documentation maintainer**,
I want **refinements to the architecture documentation based on code review feedback**,
So that **the document better follows Diátaxis principles and has complete audit trails**.

## Background

This story addresses non-blocking issues identified during Tech Writer and Dev code reviews of Story 3.1.

**Source Reviews:**
- `_bmad-output/stories/review-tech-writer-3-1-2025-12-30.md`
- Code Review output from `/code-review` workflow

## Acceptance Criteria

1. **Given** llms-full.txt regeneration claim in Story 3.1
   **When** I verify the file status
   **Then** either confirm it was regenerated or update story File List accurately

2. **Given** Recovery Model section contains command sequences
   **When** I evaluate against Diátaxis Explanation category
   **Then** either move how-to content to `/recovery` or document rationale for keeping it

3. **Given** Design Decisions section has only 3 brief Q&As
   **When** I expand the section
   **Then** include trade-offs, limitations, and when Beads architecture is NOT suitable

4. **Given** "Two Sources of Truth" title may confuse readers
   **When** I evaluate alternatives
   **Then** either rename to clearer title or document why current title is appropriate

5. **Given** "70%+ of reported issues" statistic lacks source
   **When** I add attribution
   **Then** link to Epic 2 research or soften language to "majority of issues"

## Tasks / Subtasks

- [ ] **Task 1: Verify llms-full.txt Status** (AC: #1)
  - [ ] 1.1 Check if llms-full.txt was actually regenerated
  - [ ] 1.2 If not in git, regenerate: `./scripts/generate-llms-full.sh`
  - [ ] 1.3 Update Story 3.1 File List if claim was inaccurate

- [ ] **Task 2: Evaluate Diátaxis Compliance** (AC: #2)
  - [ ] 2.1 Review Recovery Model section for how-to vs explanation content
  - [ ] 2.2 Decision: Move commands to /recovery OR document rationale for keeping
  - [ ] 2.3 Implement chosen approach

- [ ] **Task 3: Expand Design Decisions** (AC: #3)
  - [ ] 3.1 Add "Why append-only JSONL?" section
  - [ ] 3.2 Add "Trade-offs of this architecture" section
  - [ ] 3.3 Add "When NOT to use Beads" section with limitations

- [ ] **Task 4: Evaluate "Two Sources of Truth" Title** (AC: #4)
  - [ ] 4.1 Consider alternatives: "Layered Truth Model", "Recovery-Oriented Design"
  - [ ] 4.2 Decision: Rename OR document why current title is appropriate
  - [ ] 4.3 Implement chosen approach

- [ ] **Task 5: Add Source Attribution** (AC: #5)
  - [ ] 5.1 Link "70%+" claim to Epic 2 research (`_bmad-output/research/recovery-framework-design.md`)
  - [ ] 5.2 Or soften to "majority of reported issues"

## Dev Notes

### Review Sources

**Tech Writer Issues (non-blocking):**
- "Two Sources of Truth" confusing title
- Recovery Model mixes How-To into Explanation
- Design Decisions too brief
- "70%" statistic without source
- Danger admonition too long (13 lines)

**Dev Review Issues (non-blocking):**
- llms-full.txt listed but not in git changes
- Line count discrepancy (242 vs 241)

### Decision Points

This story requires decisions on:
1. Is Recovery Model content acceptable in Explanation doc? (pragmatism vs purity)
2. Is "Two Sources of Truth" title worth changing? (familiarity vs clarity)
3. How much to expand Design Decisions? (scope control)

### Priority

**Low** - These are refinements, not blockers. Story 3.1 passed AC validation.

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List

## Change Log

| Date | Change | Author |
|------|--------|--------|
| 2025-12-30 | Story created from Tech Writer + Dev review consolidation | Paige (Tech Writer) + Serhii |

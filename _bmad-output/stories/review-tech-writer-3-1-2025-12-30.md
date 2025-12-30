# Tech Writer Review: Story 3.1 Architecture Overview Enhancement

**Reviewer:** Paige (Tech Writer Agent)
**Date:** 2025-12-30
**Document:** `website/docs/architecture/index.md`
**Story:** bd-gg5.1

---

## Review Summary

| Criterion | Score | Comment |
|-----------|-------|---------|
| CommonMark compliance | 7/10 | Code blocks without language tags |
| Diátaxis alignment | 6/10 | Mixes Explanation with How-To |
| Completeness | 6/10 | Missing ADRs, Mermaid, limitations |
| Clarity | 8/10 | Reads well, but has duplication |
| Actionability | 7/10 | Good examples, but belong in Recovery |

**Overall Score: 6.8/10** — Good foundation, needs refinement.

---

## Critical Issues

### 1. ASCII Diagrams Without Language Identifier
**Lines 15-21, 69-75, 77-82, 84-91**

Code blocks have no language tag. CommonMark requires identifier. Should be:
````markdown
```text
Git Repository (Historical Source of Truth)
...
```
````

**Severity:** High
**Fix:** Add `text` language identifier to all ASCII diagram code blocks

### 2. Missing Mermaid Diagram
Per documentation standards:
> Architecture Docs: System overview diagram (Mermaid)

Document has only ASCII art. A proper Mermaid diagram of the three-layer architecture is **required** for architecture documentation.

**Severity:** High
**Fix:** Create Mermaid flowchart showing Git → JSONL → SQLite layers with data flow

### 3. "Two Sources of Truth" — Confusing Title

The term "Two Sources of Truth" sounds like a **problem**, not a feature. Counter-intuitive — usually "single source of truth" is desired. Better alternatives:
- "Layered Truth Model"
- "Historical vs Operational Data"
- "Recovery-Oriented Design"

**Severity:** Medium
**Fix:** Rename info box title to less conflict-sounding term

### 4. Triple Repetition of Source of Truth Concept

| Location | Lines |
|----------|-------|
| Info box | 23-29 |
| Layer 1 | 33 |
| Layer 2 | 44 |

Reader sees the same concept three times. This is duplication.

**Severity:** Medium
**Fix:** Keep only in info box, reference from Layer sections

---

## Structural Issues

### 5. Recovery Model is How-To, Not Explanation

Document is positioned as **Diátaxis Explanation** (understanding-oriented), but Recovery Model section contains step-by-step commands:

```bash
bd daemons killall           # Stop daemons
git worktree prune           # Clean orphaned worktrees
rm .beads/beads.db*          # Remove corrupted database
bd sync --import-only        # Rebuild from JSONL
```

This is **how-to guide** content! Should be in `/recovery`, here — only conceptual explanation of WHY this works.

**Severity:** Medium
**Fix:** Move command sequences to Recovery docs, keep conceptual explanation here

### 6. Design Decisions — Too Brief

Only 3 questions with 1-sentence answers. For architecture documentation this is **insufficient**. Missing:

- Why append-only JSONL specifically? (alternatives?)
- Why SQLite vs LevelDB/RocksDB/BoltDB?
- Trade-offs of this architecture
- When this design is **NOT suitable**
- ADR (Architecture Decision Records)

**Severity:** Medium
**Fix:** Expand with trade-offs, limitations, and architectural rationale

### 7. Statistical Claim Without Source

**Line 197:**
> This sequence resolves 70%+ of reported issues

Where does 70% come from? Should have link to source or remove specific number.

**Severity:** Low
**Fix:** Add source reference (Epic 2 research) or soften to "majority of issues"

---

## Stylistic Issues

### 8. Passive Voice Instead of Active

| Current | Should be |
|---------|-----------|
| "This distinction matters" | "Remember this distinction when..." |
| "This is the safest option" | "Use this as your safest option" |
| "conflicts may occur" | "you may encounter conflicts" |

**Severity:** Low

### 9. Inconsistent List Formatting

**Layer 1-3** use "Why X?" format, but **Design Decisions** uses different format. Should be consistent.

**Severity:** Low

### 10. Danger Admonition Too Long

13 lines of text in one admonition. Rule: admonitions should be **concise**, otherwise readers skip them.

**Severity:** Low
**Fix:** Condense to 5-7 lines max, move details to linked recovery doc

---

## Missing Content

Per Architecture Docs standards:

| Requirement | Status |
|-------------|--------|
| System overview diagram (Mermaid) | Missing |
| Component descriptions | Partial |
| Data flow | Present |
| Technology decisions (ADRs) | Missing |
| Deployment architecture | Missing |

---

## Recommended Actions (Prioritized)

| Priority | Action | Effort |
|----------|--------|--------|
| P1 | Add Mermaid diagram of three-layer architecture | Medium |
| P1 | Add `text` language tag to ASCII code blocks | Low |
| P2 | Rename "Two Sources of Truth" to less confusing title | Low |
| P2 | Move recovery commands to `/recovery`, keep concepts here | Medium |
| P2 | Expand Design Decisions with trade-offs and limitations | Medium |
| P3 | Add source for "70%" statistic or soften language | Low |
| P3 | Fix passive voice instances | Low |
| P3 | Condense danger admonition | Low |

---

## Positive Aspects

- Clear explanation of the three-layer model
- Good use of admonitions for warnings
- Practical sync mode documentation
- Excellent `bd doctor --fix` warning (Priority 0 compliance)
- Recovery cross-references present
- Follows Docusaurus frontmatter conventions

---

**Review Status:** Changes Requested
**Blocking Issues:** 2 (Missing Mermaid, Code block language tags)
**Non-Blocking Issues:** 8

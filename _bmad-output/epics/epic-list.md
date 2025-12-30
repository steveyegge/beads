# Epic List

## Epic 1: Foundation & Deployment
**User Outcome:** Documentation is accessible on steveyegge.github.io with correct configuration

**FRs covered:** FR13, FR20
**NFRs covered:** NFR8, NFR12
**Additional:** URL fix (3 files), environment-based config, sidebar updates

## Epic 2: Recovery Documentation
**User Outcome:** Developers can diagnose and resolve common Beads issues in ≤5 steps

**FRs covered:** FR2, FR3, FR4, FR26
**NFRs covered:** NFR15

## Epic 3: Architecture Documentation
**User Outcome:** Developers understand Git/JSON/SQLite interaction and can make informed decisions

**FRs covered:** FR1
**NFRs covered:** NFR7 (exempt from word limit)

## Epic 4: AI Agent Documentation
**User Outcome:** AI agents can get full project context in one request (<50K tokens)

**FRs covered:** FR8, FR9, FR10, FR11, FR12
**NFRs covered:** NFR2, NFR3

## Epic 5: Quality Assurance Pipeline
**User Outcome:** Maintainers can trust documentation quality with automated validation

**FRs covered:** FR16, FR17, FR18, FR19
**NFRs covered:** NFR10, NFR14

## Epic Dependencies

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

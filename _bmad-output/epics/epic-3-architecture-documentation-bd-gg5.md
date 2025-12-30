# Epic 3: Architecture Documentation [bd-gg5]

Developers understand Git/JSON/SQLite interaction and can make informed decisions about beads usage.

## Story 3.1: Architecture Overview Document [bd-gg5.1]

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

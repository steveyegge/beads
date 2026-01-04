# Beads SKILL Architecture Brief

> **Comparing Upstream vs Fork Implementations with Multi-Project Considerations**

---

## Executive Summary

**Core Finding**: The upstream Beads SKILL excels at **how-to mechanics** (command syntax, decision trees, reference documentation), while the fork's SKILLs focus on **pattern enforcement** (discipline compliance, mandatory rituals, event logging).

**Recommendation**: Adopt a **layered architecture** where:
1. Upstream owns the reference layer (defer to it for command documentation)
2. Fork owns the enforcement layer (maintain discipline skills)
3. Multi-project coordination is a fork-specific extension opportunity

---

## Background

### Anthropic's SKILL Standard

SKILLs are markdown files with YAML frontmatter that define capabilities, trigger phrases, and allowed tools. They provide structured guidance for Claude agents, replacing ad-hoc prompting with documented, version-controlled workflows.

### Beads Evolution

Beads originally relied on an **MCP server** for Claude integration. The transition to SKILLs brought:
- Declarative capability definitions
- Trigger phrase activation
- Tool permission scoping
- Version tracking

### The Upstream SKILL

The upstream repo (`steveyegge/beads`) now includes `skills/beads/SKILL.md` - a comprehensive ~5000-line reference authored by Steve Yegge covering all `bd` commands, session protocols, and decision guidance.

---

## Comparative Analysis

| Aspect | Upstream SKILL | Fork SKILLs |
|--------|---------------|-------------|
| **Author** | Steve Yegge | justSteve |
| **Focus** | Command reference & when-to-use guidance | Pattern enforcement & ritual compliance |
| **Strength** | Explains mechanics thoroughly | Forces discipline adherence |
| **Structure** | Single comprehensive document | 8 specialized skills |
| **Scope** | Single project | Multi-project aware |
| **Event Logging** | Minimal | Extensive (sk.*, ss.*, gt.* events) |
| **Test Gates** | Mentioned | Enforced (blocks push on failure) |

### Example: Session End Protocol

**Upstream** (informational):
```markdown
#### Session End Protocol
When finishing work:
1. Update task with completion notes
2. Run `bd sync` to export
3. Commit and push changes
```

**Fork** (enforcement):
```markdown
# beads-landing
> **THIS RITUAL IS NON-NEGOTIABLE.**

### Phase 3: Quality Gates (MANDATORY)
If tests FAIL:
- DO NOT close the issue
- Fix tests before pushing

### Phase 5: Sync and Push (MANDATORY - MUST SUCCEED)
LANDING FAILED - WORK NOT PUSHED
Do NOT end session until work is pushed.
```

---

## Skill Inventory

### Upstream (skills/beads/)

| File | Type | Purpose |
|------|------|---------|
| `SKILL.md` | Reference | Complete command documentation |
| `references/CLI_REFERENCE.md` | Reference | Command-by-command syntax |
| `references/DEPENDENCIES.md` | Reference | Dependency types explained |
| `references/WORKFLOWS.md` | Reference | Multi-issue patterns |
| `references/BOUNDARIES.md` | Reference | bd vs TodoWrite decision |
| `references/RESUMABILITY.md` | Reference | Compaction survival |
| *(10 reference files total)* | | |

### Fork (vscode/skills/)

**Pattern Enforcement (4 skills)**:

| Skill | Purpose | Key Enforcement |
|-------|---------|-----------------|
| `beads-scope` | ONE ISSUE discipline | Logs violations, files discoveries |
| `beads-landing` | Session end ritual | Blocks push on test failure |
| `beads-initializer` | Epic decomposition | Structured parent-child creation |
| `beads-init-app` | Bootstrap ceremony | Guards all work until foundation complete |

**Ritual Mechanics (4 skills)**:

| Skill | Purpose | Differentiator |
|-------|---------|----------------|
| `beads-bootup` | Session start | Creates session markers, health checks |
| `beads-handoff` | Prompt generation | Structured continuation context |
| `beads-circuit-breaker` | Issue deferral | Abandon stuck work cleanly |
| `beads-recovery` | Error recovery | Scenario-specific guidance |

---

## Recommendations

### 1. Defer to Upstream for Reference

**Remove duplication**: The fork's `skills/beads/SKILL.md` mirrors upstream's command documentation. Since the fork tracks upstream (4,738 commits synchronized), this creates maintenance burden.

**Action**: Fork skills should **reference** upstream SKILL for command syntax:
```markdown
## Command Reference
For complete `bd` command documentation, see the upstream SKILL:
- `skills/beads/SKILL.md`
- `skills/beads/references/CLI_REFERENCE.md`
```

### 2. Maintain Fork Enforcement Skills

**Keep all 8 vscode/skills/** - they provide unique value:
- Event logging infrastructure (sk.*, ss.*, gt.* events)
- Mandatory test gates before push
- Scope violation tracking
- Session marker management

These cannot be upstream because they reflect **local workflow philosophy**, not bd mechanics.

### 3. Reduce Mechanics Duplication

The fork's ritual skills (bootup, landing, handoff) duplicate some upstream content. Consider:
- Keep enforcement logic (markers, gates, logging)
- Reference upstream for command examples
- Remove redundant command syntax explanations

### 4. Multi-Project Extensions

The `c:\myStuff` umbrella structure has unique requirements not addressed by either implementation:

| Requirement | Current State | Opportunity |
|-------------|---------------|-------------|
| Cross-project discovery | Manual CLAUDE.md reference | `beads-sibling` skill |
| Workspace-wide view | Independent .beads/ per repo | `beads-workspace` skill |
| Shared infrastructure | _infra/agents plugin ecosystem | Formalize integration |

**Proposed new skills**:

```markdown
# beads-workspace
Provides visibility across all beads-enabled projects in c:\myStuff.
Commands:
- List all ready issues across workspace
- Show cross-project dependencies
- Aggregate event logs
```

```markdown
# beads-sibling
Handles discovered work that belongs to a different project.
When: "Found while working on Project A, this belongs in Project B"
Action: Creates issue in target project's .beads/
```

---

## Multi-Project Architecture

### Current Structure

```
c:\myStuff\                          # Umbrella root
├── CLAUDE.md                        # Cross-project conventions
├── _infra\
│   ├── beads\                       # Beads core (225 issues)
│   │   └── .beads\issues.jsonl
│   ├── agents\                      # Plugin ecosystem (68 plugins)
│   │   └── .beads\
│   └── ActionableLogLines\          # Log viewer
│       └── .beads\
├── ParseClipmate\                   # Data migration
│   └── .beads\
├── _tooling\Judge0\                 # Code execution
│   └── .beads\
├── Bitwarden\                       # No beads yet
├── IDEasPlatform\                   # No beads yet
└── myDSPy\                          # No beads yet
```

### Cross-Project Coordination Gap

Each beads instance is **isolated**:
- `bd ready` only shows issues from current project
- Dependencies can't span projects
- No workspace-level orchestration

**Future enhancement**: A `beads-workspace` skill could:
1. Aggregate `bd ready` across all projects
2. Enable cross-project dependency types
3. Provide human orchestrator with unified view

---

## Proposed Layered Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    LAYER 3: MULTI-PROJECT                   │
│  beads-workspace (proposed) - Cross-project visibility      │
│  beads-sibling (proposed) - Cross-project discoveries       │
│  CLAUDE.md conventions - Documentation coordination         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               LAYER 2: ENFORCEMENT (Fork-owned)             │
│  beads-scope - ONE ISSUE discipline                         │
│  beads-landing - Mandatory session end ritual               │
│  beads-bootup - Session start with markers                  │
│  beads-initializer - Structured decomposition               │
│  beads-init-app - Bootstrap ceremony                        │
│  beads-circuit-breaker - Clean deferral                     │
│  beads-handoff - Continuation context                       │
│  beads-recovery - Error recovery                            │
└─────────────────────────────────────────────────────────────┘
                              │
                         References
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               LAYER 1: REFERENCE (Upstream-owned)           │
│  skills/beads/SKILL.md - Command documentation              │
│  skills/beads/references/* - Deep dive guides               │
│  bd CLI - Implementation source of truth                    │
└─────────────────────────────────────────────────────────────┘
```

---

## Action Items

### Immediate (Maintenance)

1. **Audit fork skills for upstream duplication** - Mark sections that should reference upstream instead of duplicating
2. **Add upstream reference pointers** - Each fork skill should link to relevant upstream docs
3. **Keep merge strategy** - Continue "accept upstream, preserve local CLAUDE.md" approach

### Medium-term (Enhancement)

4. **Design beads-workspace skill** - Specification for cross-project visibility
5. **Design beads-sibling skill** - Specification for cross-project discoveries
6. **Formalize event aggregation** - Structured logging across projects

### Long-term (Upstream Contribution)

7. **Consider PR to upstream** - Enforcement patterns could benefit community
8. **Multi-project support** - Propose cross-repo dependency types to upstream

---

## Conclusion

The fork's SKILL implementations correctly focus on **enforcement and discipline** rather than duplicating upstream's **reference documentation**. The recommended architecture:

- **Trust upstream** for command mechanics and "how-to" guidance
- **Maintain fork skills** for discipline enforcement and event logging
- **Extend with multi-project skills** to address the `c:\myStuff` umbrella structure

The moat isn't the documentation - it's the harness that turns LLM calls into disciplined, observable, recoverable work sessions across a coordinated project ecosystem.

---

*Generated: 2026-01-02*
*Analysis: Deep sequential thinking with codebase exploration*

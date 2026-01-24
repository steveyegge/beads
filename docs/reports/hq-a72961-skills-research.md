# Skills as First-Class Beads Concept

**Bead**: hq-a72961
**Author**: beads/crew/skills
**Date**: 2026-01-23

## Executive Summary

This document explores adding **skills** as a first-class concept in the beads system. Skills represent trackable capabilities that enable intelligent work routing, formula requirements declaration, and agent capability matching.

**Key recommendation**: Skills should be beads themselves (issue_type=skill), stored in the existing issues table with skill-specific fields, and attached via the unified dependency edge system.

---

## 1. What is a Skill?

### Definition

A **skill** is a named, versionable capability that describes what an agent can do or what work requires. Skills bridge the gap between:
- **Work requirements**: "This issue needs someone who can write Go tests"
- **Agent capabilities**: "This agent knows Go testing patterns"
- **Formula specifications**: "This workflow requires database migration expertise"

### Skill vs Formula

| Aspect | Skill | Formula |
|--------|-------|---------|
| **What it is** | A capability/expertise | A workflow template |
| **Granularity** | Atomic ability | Composed workflow |
| **Examples** | "go-testing", "sql-migrations", "pr-review" | "beads-release", "e2e-test-fix" |
| **Attachment** | To agents, issues, formulas | To molecules/wisps |
| **Versioned** | Yes (semver) | Yes (schema version) |

**Key insight**: Formulas may *require* skills. A formula like `database-migration` might require skills `sql-ddl` and `backup-restore`.

### Skill vs Label

| Aspect | Skill | Label |
|--------|-------|-------|
| **Structure** | Rich metadata (version, description, inputs/outputs) | Simple string tag |
| **Semantics** | Capability matching | Categorization/filtering |
| **Attachment** | Bidirectional (issues require, agents provide) | Unidirectional |
| **Examples** | `skill:go-testing@1.0` | `area:backend`, `priority:high` |

**Recommendation**: Skills are NOT just labels with a `skill:` prefix. They need their own identity for versioning, descriptions, and cross-rig sharing.

---

## 2. Existing Concepts Analysis

### Current Beads Data Model

The beads system already has rich primitives we can leverage:

1. **Issues as entities**: Everything is an issue with `issue_type` discriminator
   - Already supports: `bug`, `feature`, `task`, `epic`, `agent`, `molecule`, `gate`, `slot`, `role`, `rig`
   - Skills would be `issue_type = "skill"`

2. **Unified edge schema** (Decision 004): All relationships via `dependencies` table
   - Types: `blocks`, `parent-child`, `waits-for`, `tracks`, `validates`, etc.
   - Skills would use new edge types: `requires-skill`, `provides-skill`

3. **Labels**: Simple string tags in separate `labels` table
   - Skills could auto-generate labels like `skill:go-testing` for discoverability

4. **Agent beads**: Agents are issues with special fields
   - `RoleType`, `AgentState`, `HookBead`, `Rig`
   - Would add: `Skills[]` via edges

### Current Claude Code Skills

The codebase already has a `claude-plugin/skills/` directory with:
- `SKILL.md` - Frontmatter metadata + markdown instructions
- `resources/` - Supporting documentation files
- `README.md` - Human-readable guide

This is **documentation for Claude** (similar to Mintlify's skill.md). The proposed beads skills are **capability metadata for work routing**. These are complementary:

| Claude Code Skills | Beads Skills |
|--------------------|--------------|
| How to use a tool | What an agent can do |
| Documentation | Metadata |
| Read by Claude | Queried for routing |
| Markdown files | Database records |

**Integration opportunity**: A beads skill could reference a Claude Code skill file for detailed instructions.

---

## 3. Skill Metadata Schema

### Core Fields

```go
// Skill-specific fields (added to Issue struct)
type SkillFields struct {
    // Identity
    SkillName    string   `json:"skill_name"`    // Canonical name: "go-testing"
    SkillVersion string   `json:"skill_version"` // Semver: "1.0.0"

    // Documentation
    SkillInputs  []string `json:"skill_inputs"`  // What the skill needs
    SkillOutputs []string `json:"skill_outputs"` // What the skill produces
    Examples     []string `json:"examples"`      // Usage examples

    // Classification
    SkillCategory string  `json:"skill_category"` // "testing", "devops", "docs"

    // Integration
    ClaudeSkillPath string `json:"claude_skill_path"` // Path to SKILL.md if exists
}
```

### Skill Issue Example

```json
{
  "id": "skill-go-testing",
  "title": "Go Testing",
  "description": "Ability to write and run Go tests using testing package, testify, and table-driven patterns",
  "issue_type": "skill",
  "status": "pinned",

  "skill_name": "go-testing",
  "skill_version": "1.0.0",
  "skill_category": "testing",
  "skill_inputs": ["Go source code", "Test requirements"],
  "skill_outputs": ["Test files", "Coverage reports"],
  "examples": ["Unit tests", "Integration tests", "Benchmark tests"],

  "labels": ["skill:go-testing", "category:testing"]
}
```

---

## 4. Attachment Points

### 4.1 Skills on Agents

**Use case**: "This agent knows Go testing"

```bash
# Declare agent has a skill
bd skill add beads/crew/skills go-testing

# Query agents with a skill
bd skill agents go-testing
```

**Storage**: Dependency edge with type `provides-skill`
```
agent-bead --[provides-skill]--> skill-go-testing
```

### 4.2 Skills on Issues

**Use case**: "This issue needs someone with Go testing skills"

```bash
# Require skill for issue
bd skill require bd-123 go-testing

# Find issues matching agent's skills
bd ready --with-skills  # Only shows issues agent can handle
```

**Storage**: Dependency edge with type `requires-skill`
```
issue-bd-123 --[requires-skill]--> skill-go-testing
```

### 4.3 Skills on Formulas

**Use case**: "This workflow requires database migration expertise"

Formula TOML:
```toml
formula = "database-migration"
requires_skills = ["sql-ddl", "backup-restore"]

[[steps]]
id = "run-migration"
requires_skills = ["sql-ddl"]  # Step-level requirement
```

When cooking a molecule, validate that assigned agent has required skills.

### 4.4 Skills on Molecules

**Use case**: "This specific workflow instance needs these skills"

When a formula is cooked into a molecule, skill requirements are copied to the molecule bead. This allows runtime skill matching even if the formula changes.

---

## 5. Skill Hierarchies

### Category Hierarchy

Skills can have parent categories for coarse-grained matching:

```
testing/
  ├── go-testing
  ├── python-testing
  └── e2e-testing
      ├── cypress
      └── playwright
```

**Storage**: Parent-child edges between skill beads
```
skill-testing --[parent-child]--> skill-go-testing
```

### Skill Implication

Having a specific skill might imply having broader skills:

```bash
# Agent with "cypress" implicitly has "e2e-testing" and "testing"
bd skill add agent cypress --implies="e2e-testing,testing"
```

---

## 6. Cross-Rig Skill Sharing

### The Problem

Skills need to be consistent across rigs for routing to work. If `gastown` defines `go-testing` differently than `beads`, routing breaks.

### Solution: Town-Level Skills Registry

Skills are defined at the **town level** (HQ beads) with prefix `skill-`:
- `skill-go-testing` lives in `~/gt/.beads/`
- All rigs reference the same skill definitions
- Rig-local skills use rig prefix: `beads-skill-custom-thing`

### Skill Federation

For multi-town setups, skills can be federated via Dolt remotes:
1. Define canonical skills in a "skills registry" repo
2. Other towns pull skill definitions
3. External skill references use `external:registry:skill-name`

---

## 7. CLI Command Proposals

### Skill Management

```bash
# Create a skill
bd skill create go-testing \
  --description "Write and run Go tests" \
  --category testing \
  --version 1.0.0

# List skills
bd skill list
bd skill list --category testing

# Show skill details
bd skill show go-testing

# Update skill
bd skill update go-testing --version 1.1.0
```

### Skill Attachment

```bash
# Agent declares skill
bd skill add <agent-id> <skill-name>
bd skill add beads/crew/skills go-testing

# Issue requires skill
bd skill require <issue-id> <skill-name>
bd skill require bd-123 go-testing

# Remove skill
bd skill remove <agent-id> <skill-name>
```

### Skill Queries

```bash
# Find agents with skill
bd skill agents <skill-name>
bd skill agents go-testing

# Find issues requiring skill
bd skill issues <skill-name>
bd skill issues go-testing

# Check if agent can handle issue
bd skill match <agent-id> <issue-id>
bd skill match beads/crew/skills bd-123

# Enhanced ready command
bd ready --with-skills  # Filter by current agent's skills
bd ready --skills go-testing,sql  # Require specific skills
```

---

## 8. Integration with Work Routing

### Current Routing (Without Skills)

```
gt sling <bead-id> <rig>  # Manual assignment
bd ready                   # All unblocked work
```

### Enhanced Routing (With Skills)

```bash
# Sling validates skill match
gt sling bd-123 beads/polecats/p1
# Warning: Agent beads/polecats/p1 missing required skill: go-testing

# Ready filters by skills
bd ready --with-skills
# Only shows issues the current agent can handle

# Automatic skill-based assignment
gt sling bd-123 beads --auto
# Picks polecat with matching skills
```

### Formula Skill Validation

When cooking a formula:
```bash
bd mol pour database-migration
# Error: Formula requires skills [sql-ddl, backup-restore]
# Run: bd skill add <your-agent-id> sql-ddl backup-restore
```

---

## 9. Storage Model Options

### Option A: Skills as Issues (Recommended)

**Add skill fields to Issue struct, use `issue_type = "skill"`**

Pros:
- Leverages existing infrastructure (CRUD, sync, JSONL export)
- Skills are trackable, versionable, queryable
- Unified edge schema for attachments
- Already supports cross-rig references

Cons:
- Adds fields to already large Issue struct
- Skills mixed with work items in queries

**Mitigation**: Add `bd skill` subcommand that filters by `issue_type=skill`

### Option B: Separate Skills Table

**New `skills` table with dedicated schema**

Pros:
- Clean separation from issues
- Dedicated indexes and queries

Cons:
- Duplicates infrastructure (CRUD, sync, export)
- New migrations, new code paths
- Skill→Issue edges still need dependency table

### Option C: Skills as Labels with Metadata

**Extend labels table with optional metadata**

Pros:
- Minimal schema change
- Labels already attached to issues

Cons:
- Labels are simple strings, adding metadata is awkward
- No versioning, no rich descriptions
- Doesn't fit the mental model

### Recommendation: Option A

Skills as issues with `issue_type = "skill"` provides:
- Full CRUD with existing code
- JSONL sync for cross-rig sharing
- Unified edge system for attachments
- Status tracking (`pinned` for active skills, `tombstone` for deprecated)
- Built-in versioning via `skill_version` field

---

## 10. Implementation Plan

### Phase 1: Core Schema (MVP)

1. Add skill fields to Issue struct
2. Add `skill` to IssueType enum
3. Add `requires-skill` and `provides-skill` edge types
4. Basic `bd skill create/show/list` commands

### Phase 2: Attachment Commands

1. `bd skill add <agent> <skill>` - Agent declares skill
2. `bd skill require <issue> <skill>` - Issue requires skill
3. Skill validation on `bd update --assignee`

### Phase 3: Routing Integration

1. `bd ready --with-skills` filter
2. `gt sling` skill validation/warnings
3. Formula `requires_skills` field

### Phase 4: Advanced Features

1. Skill hierarchies (parent-child)
2. Skill implications
3. Town-level skill registry
4. Cross-rig skill federation

---

## 11. Open Questions

1. **Skill granularity**: How fine-grained should skills be?
   - Too broad: "programming" (useless for routing)
   - Too narrow: "go-1.21-generics-testing" (maintenance burden)
   - Sweet spot: "go-testing", "sql-migrations", "pr-review"

2. **Skill proficiency levels**: Should agents declare skill levels?
   - Simple: binary (has/doesn't have)
   - Complex: levels (novice, intermediate, expert)
   - Recommendation: Start simple, add levels if needed

3. **Skill decay**: Do skills expire if unused?
   - Probably not needed for agent skills
   - Maybe useful for tracking which skills are actively used

4. **Skill discovery**: How do agents learn new skills?
   - Manual declaration (`bd skill add`)
   - Inferred from completed work (future ML feature)

5. **Conflict resolution**: What if issue requires skill A but only agents with skill B are available?
   - Warning and proceed
   - Block assignment
   - Recommend training

---

## 12. Relationship to Mintlify skill.md

The Mintlify skill.md concept (agent-readable documentation) is complementary:

| Aspect | Mintlify skill.md | Beads Skills |
|--------|-------------------|--------------|
| Purpose | Teach agent how to use product | Track agent capabilities |
| Format | Markdown | Database record |
| Location | `/.well-known/skills/` | Beads database |
| Audience | LLM reading instructions | Work routing system |

**Integration**: A beads skill can reference a skill.md file via `claude_skill_path` field. When an agent is assigned work requiring a skill, they can load the corresponding skill.md for detailed instructions.

```toml
# Skill bead
id = "skill-beads-usage"
skill_name = "beads-usage"
claude_skill_path = "claude-plugin/skills/beads/SKILL.md"
```

---

## 13. Summary

**Recommendation**: Implement skills as beads (issues with `issue_type=skill`), attached via the unified dependency edge system.

This approach:
- Maximizes reuse of existing infrastructure
- Provides rich metadata for capability matching
- Enables intelligent work routing
- Supports cross-rig skill sharing
- Complements existing Claude Code skills (documentation)

Next step: Review this document and decide on Phase 1 scope.

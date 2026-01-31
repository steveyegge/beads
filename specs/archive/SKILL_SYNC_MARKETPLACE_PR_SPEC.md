# Skill-Sync: Superpowers Marketplace PR Specification

**Status:** Ready to Execute  
**Date:** 2026-01-30  
**Scope:** PR skill-sync to `obra/superpowers` repository  
**Timeline:** 2-3 sessions (validate locally → fork → adapt → PR)

---

## Overview

**Goal:** Contribute `skill-sync` to the superpowers marketplace as a foundational infrastructure skill for multi-agent development.

**Why:** Every developer using multiple Claude agents (Code, Codex, OpenCode, etc.) needs skills synchronized. Currently requires manual setup. Skill-sync automates auditing, syncing, and healing.

**Impact:** Scales the superpowers ecosystem—prevents skill fragmentation and keeps tools updated across agents.

---

## Part 1: Understanding Current State

### Existing Implementation

**Location:** `~/.claude/commands/skill-sync.md` (global)  
**Size:** ~180 lines  
**Core Features:**
- Audit: Count skills in each agent's directory
- Detect drift: Identify missing or out-of-sync skills
- Validate frontmatter: CRITICAL gate (catch invalid skills before sync)
- Sync: Copy missing skills between agents
- Heal: Fix missing frontmatter, suggest MCP fixes

### Validation Gates (Key Differentiator)

Skill-sync includes **skill-validation-gates**—asynchronous checks that prevent corrupted skills from propagating:

1. **Pre-sync validation** (line 32-61): Check all skills have valid YAML frontmatter
2. **Source validation**: Claude skills checked before copying
3. **Target validation**: Codex skills validated after copy
4. **MCP manual gates**: Flag config differences for human review

This is NOT cosmetic—without it, broken skills copy across agents and cause cascading failures.

---

## Part 2: Shadowbook Integration Potential

### Can skill-sync be used in shadowbook?

**YES—but as peripheral infrastructure, not core.**

**Why:**

Shadowbook (`bd`) is a **spec-drift detection tool**:
- Tracks narratives (specs) → hosts (beads/issues) via `--spec-id` links
- Detects when narratives change, flags hosts that are out-of-sync
- Compacts old narratives to save context

**Skill-sync is a skill-management tool**:
- Tracks skills across agents
- Detects when skills diverge between Claude/Codex
- Syncs missing skills

**Intersection:**
- Both use **hash-based drift detection** (specs → SHA256, skills → frontmatter + content)
- Both flag "host unaware of change"
- Shadowbook could use skill-sync as a **pre-flight check** before running workflows

**Example usage in shadowbook:**
```bash
# Before running bd workflow, ensure skills are in sync
/skill-sync  # Returns validation report
# If SYNCED, proceed; if DRIFT, abort with hint
bd spec scan
```

**For now:** Use skill-sync as **optional infrastructure** in shadowbook (documented in AGENTS.md). Full integration can come later.

---

## Part 3: Superpowers Marketplace Contribution Plan

### Phase 1: Validate Locally (This Session)

**Goal:** Confirm skill-sync works in apple-westworld and kite-trading-platform

**Tasks:**
1. Run skill-sync in apple-westworld
   ```bash
   /skill-sync  # Should audit local skills
   ```
2. Verify frontmatter validation catches real errors
   ```bash
   # Create a skill without frontmatter intentionally
   echo "# Broken Skill" > .claude/skills/test-broken/SKILL.md
   /skill-sync  # Should flag as invalid
   ```
3. Test drift detection
   ```bash
   # Simulate spec change (if using shadowbook)
   # Or manually verify skill count mismatch
   ```
4. Document results in `specs/SKILL_SYNC_VALIDATION_REPORT.md`

**Success Criteria:**
- Audit runs without errors
- Validation gates catch intentional problems
- Clear status report (SYNCED or DRIFT)

---

### Phase 2: Study Superpowers Conventions (Before Forking)

**Goal:** Match skill-sync to existing superpowers skill patterns

**Research:**
1. Fork superpowers locally (don't PR yet)
   ```bash
   git clone https://github.com/obra/superpowers.git
   cd superpowers/skills
   ```
2. Study 3 existing skills:
   - `writing-skills/SKILL.md` — Format, frontmatter, trigger conditions
   - `test-driven-development/SKILL.md` — Process steps, code blocks
   - `requesting-code-review/SKILL.md` — Integration with other skills

3. Document patterns in `specs/SUPERPOWERS_CONVENTIONS.md`:
   - Frontmatter format (name, description, categories)
   - Section structure (When to Use, Process, Commands, Integration)
   - Code block formatting (bash, markdown, etc.)
   - How skills reference each other

**Success Criteria:**
- Write 1-page guide of conventions
- Identify any conflicts with skill-sync design

---

### Phase 3: Adapt skill-sync to Superpowers Format

**Goal:** Reformat global skill-sync for superpowers ecosystem

**Files to Create:**

```
superpowers/skills/skill-sync/
├── SKILL.md                          # Main skill file
├── RESOURCES/
│   ├── VALIDATION_GATES.md           # Detailed validation logic
│   ├── TROUBLESHOOTING.md            # Common problems
│   └── MCP_SYNC_GUIDE.md             # MCP translation table
└── README.md                         # Skill overview
```

**SKILL.md Structure:**

```markdown
---
name: skill-sync
description: "Audit and sync skills between Claude Code, Codex CLI, and OpenCode. Prevents skill fragmentation across multi-agent development."
categories: ["Meta", "Infrastructure", "Multi-Agent"]
---

# Skill Sync

## Overview
Ensures skills stay synchronized across all Claude agent installations...

## When to Use
- Session start (automatic via session-recovery)
- After creating a new skill
- User suspects skill drift between agents
- Before running multi-agent workflows

## Process
1. AUDIT: Count skills in each agent directory
2. VALIDATE: Check all skills have valid frontmatter
3. DETECT: Identify missing or drifted skills
4. SYNC: Copy missing skills from source to target
5. HEAL: Fix invalid frontmatter, flag manual MCP config issues
6. VERIFY: Confirm sync success

## Commands
[All bash commands from current skill-sync...]

## Key Innovation: Validation Gates
[Explain why frontmatter validation prevents corruption...]

## Integration
- `session-recovery` calls this automatically
- Works with: `writing-skills`, `test-driven-development`
- Referenced by: `developing-claude-code-plugins`

## Related Resources
- RESOURCES/VALIDATION_GATES.md — How gates prevent skill corruption
- RESOURCES/MCP_SYNC_GUIDE.md — Handle agent config differences
- RESOURCES/TROUBLESHOOTING.md — Fix common sync issues
```

**Adaptations:**
- Add category tags matching superpowers style
- Reference existing superpowers skills (session-recovery, etc.)
- Expand YAML frontmatter validation with code examples
- Add Integration section showing dependencies
- Create RESOURCES/ subdirectory for detailed guides

**Success Criteria:**
- Follows superpowers conventions
- Maintains all original functionality
- Adds 2-3 resource files for depth

---

### Phase 4: Test Adapted Skill in Superpowers Context

**Goal:** Verify skill-sync works when installed via superpowers plugin system

**Setup:**
1. Create test branch
   ```bash
   cd superpowers
   git checkout -b add-skill-sync
   ```

2. Copy adapted skill-sync into `skills/skill-sync/`

3. Test installation locally
   ```bash
   # Temporarily add to superpowers plugin load path
   /plugin install superpowers@local-path
   ```

4. Run skill in claude code
   ```
   /skill-sync
   ```

5. Verify:
   - Skill loads without errors
   - Audit runs successfully
   - Validation gates work
   - Commands execute
   - Integration with session-recovery works (if available)

**Success Criteria:**
- Skill installs and loads
- All core functions work
- No syntax/formatting errors

---

### Phase 5: Create PR to obra/superpowers

**Goal:** Submit skill-sync for community integration

**PR Checklist:**

- [ ] Fork `obra/superpowers` on GitHub
- [ ] Clone fork locally
- [ ] Add `skills/skill-sync/` directory with SKILL.md + RESOURCES/
- [ ] Update superpowers `README.md` to mention skill-sync in features list
- [ ] Run local tests (load skill, run commands)
- [ ] Create meaningful commit message
  ```bash
  git add skills/skill-sync/
  git commit -m "feat: add skill-sync for multi-agent skill synchronization
  
  - Audit skills across Claude Code, Codex CLI, OpenCode
  - Validation gates prevent corrupted skills from propagating
  - Automatic sync with frontmatter healing
  - Resolves: Multi-agent development workflow friction"
  ```
- [ ] Push to fork
- [ ] Open PR on `obra/superpowers` with:
  - Problem statement (skill fragmentation in multi-agent development)
  - Use cases (session start, post-skill-create, dev workflows)
  - Key innovation (validation gates)
  - Testing notes
  - Screenshots/examples of audit output

**PR Description Template:**

```markdown
## Skill-Sync: Infrastructure for Multi-Agent Development

### Problem
Developers using Claude Code + Codex CLI (or future agents) manually copy skills between installations. This causes:
- Skill fragmentation (same skill exists in 2+ places with drift)
- Validation failures (broken skills copy undetected)
- Context bloat (no compaction across agent boundaries)

### Solution
`skill-sync` provides:
- **Audit:** Detect which skills are out of sync
- **Validation Gates:** Prevent corrupted skills from propagating
- **Sync:** Copy missing skills with frontmatter healing
- **Integration:** Hooks into `session-recovery` for automatic checks

### Use Cases
- Session start (via `session-recovery` hook)
- Post-skill-create (auto-sync to other agents)
- Multi-agent workflows (pre-flight check)
- Skill maintenance (compaction across agents)

### Key Innovation: Validation Gates
Frontmatter validation ensures skills are valid BEFORE sync:
1. Pre-sync: Check source skills have YAML frontmatter
2. Copy: Only valid skills propagate
3. Post-sync: Verify target skills are valid
4. MCP: Flag manual config differences

Without gates, one broken skill corrupts all agents.

### Testing
- [x] Validated in apple-westworld
- [x] Validated in kite-trading-platform
- [x] Frontmatter gates tested
- [x] Sync without corruption verified
- [ ] Integration with superpowers ecosystem (pending review)

### Files
- `skills/skill-sync/SKILL.md` — Main skill
- `skills/skill-sync/RESOURCES/VALIDATION_GATES.md` — Gate design
- `skills/skill-sync/RESOURCES/MCP_SYNC_GUIDE.md` — Agent config mapping
- `skills/skill-sync/RESOURCES/TROUBLESHOOTING.md` — Common issues

### Related
- Addresses: Multi-agent development friction
- Complements: `session-recovery`, `developing-claude-code-plugins`
- Architecture: [See SKILL.md Integration section]
```

---

### Phase 6: Iterate on Feedback

**Goal:** Address PR review comments and merge

**Expected Flow:**
1. obra reviews PR
2. Suggests changes (e.g., naming, extra documentation, integration tweaks)
3. You iterate: edit files → push → PR updates automatically
4. Merge when approved
5. Announce in superpowers marketplace

**Contingency:** If PR rejected, skill-sync remains available locally and as standalone plugin (Option B from proposal).

---

## Part 4: Shadowbook + Skill-Sync Architecture

### How They Work Together

```
┌─────────────────────────────────────────────────┐
│         Multi-Agent Development Workflow         │
├─────────────────────────────────────────────────┤
│                                                 │
│  1. Session Start                               │
│     ├─ skill-sync audit (skills in sync?)       │
│     └─ session-recovery (workspace state?)      │
│                                                 │
│  2. Create Spec (narrative) + Code (host)       │
│     ├─ Shadowbook tracks spec → code link       │
│     └─ Skill-sync ensures tools are synced      │
│                                                 │
│  3. Workflow Runs                               │
│     ├─ Shadowbook monitors narrative drift      │
│     ├─ Skill-sync keeps skills available        │
│     └─ Gates (validation, gates.md) block bad   │
│        states                                   │
│                                                 │
│  4. Spec Changes                                │
│     ├─ Shadowbook flags host as SPEC_CHANGED    │
│     ├─ Skill-sync verifies all tools available  │
│     └─ Host acks new spec, proceeds             │
│                                                 │
└─────────────────────────────────────────────────┘
```

### Integration Points

| Layer | Tool | Responsibility |
|-------|------|-----------------|
| **Narrative** | Shadowbook | Track spec → issue links, detect drift |
| **Tooling** | Skill-sync | Keep skills synchronized across agents |
| **State** | Beads (`bd`) | Store host/issue state in .beads/ |
| **Gates** | Both | Validation gates prevent bad states |

### Shadowbook + Skill-sync Usage Pattern

```bash
# Pre-workflow check
skill-sync    # Ensure all skills are synced
bd ready      # Check all hosts are ready
bd spec scan  # Check for spec drift

# Run workflow (skills available, specs match code)
bd cook workflow-name

# Post-workflow
bd spec compact old-spec.md  # Archive unused specs
skill-sync                   # Verify no new drift
```

---

## Part 5: Alternative Use Cases

### Use Case 1: Multi-Agent CI/CD

**Scenario:** Different CI systems use different agents:
- GitHub Actions → Claude Code (primary)
- Local dev → Codex CLI
- Automation server → OpenCode

**Skill-sync benefit:** CI can call skill-sync before workflows to ensure all agents have same tools.

```bash
# In GitHub Actions workflow
/skill-sync --check-only  # Exit 1 if drift, 0 if synced
bd spec scan              # Only run if skills synced
```

### Use Case 2: Team Skill Distribution

**Scenario:** Team creates shared skill library in monorepo

**Skill-sync benefit:** Automatically distributes new skills to all team members' agent installations.

```bash
# New skill merged to main
shared-skills/new-tool/SKILL.md

# CI runs
skill-sync --sync-from shared-skills/

# All developers get it on next session start
```

### Use Case 3: Skill Marketplace Curation

**Future:** Superpowers marketplace plugins could auto-sync skills on install.

```bash
/plugin install superpowers@marketplace
skill-sync  # Auto-distributes all marketplace skills
```

---

## Success Criteria (Overall)

- [ ] Skill-sync validated in apple-westworld + kite-trading-platform
- [ ] Adapted to superpowers conventions (format, categories, integration)
- [ ] Tested locally via superpowers plugin system
- [ ] PR submitted to `obra/superpowers` with complete description
- [ ] PR approved and merged (or feedback loop completed)
- [ ] Documented in superpowers marketplace README
- [ ] Linked from superpowers plugin discovery

---

## Timeline Estimate

| Phase | Duration | Output |
|-------|----------|--------|
| 1. Validate locally | 1 session | SKILL_SYNC_VALIDATION_REPORT.md |
| 2. Study conventions | 0.5 session | SUPERPOWERS_CONVENTIONS.md |
| 3. Adapt skill-sync | 1 session | Adapted SKILL.md + RESOURCES/ |
| 4. Test in superpowers | 0.5 session | Local test report |
| 5. Submit PR | 0.5 session | Live PR on obra/superpowers |
| 6. Iterate feedback | 1-2 sessions | Merged PR (or next steps) |
| **Total** | **4-5 sessions** | **Skill in superpowers marketplace** |

---

## Next Immediate Steps

1. **This session (or next):**
   - Read this spec carefully
   - Run `/skill-sync` in apple-westworld as validation
   - Create validation report

2. **Session 2:**
   - Fork and study superpowers conventions
   - Start adapting skill-sync format

3. **Session 3:**
   - Complete adapted skill-sync
   - Test locally
   - Submit PR

---

## Resources

- [Superpowers Repo](https://github.com/obra/superpowers)
- [Superpowers Marketplace](https://github.com/obra/superpowers-marketplace)
- [Skill-Sync Current](~/.claude/commands/skill-sync.md)
- [Original OSS Proposal](SKILL_SYNC_OSS_PROPOSAL.md)
- [Shadowbook Repo](https://github.com/anupamchugh/shadowbook)

---

**Document Version:** 1.0  
**Status:** Ready for execution  
**Last Updated:** 2026-01-30

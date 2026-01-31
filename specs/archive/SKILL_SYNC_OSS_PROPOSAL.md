# Skill-Sync OSS Contribution Proposal

> Contributing `skill-sync` to the superpowers repo as a universal skill.

---

## Current State

**Location:** Originally in `kite-trading-platform/.claude/skills/skill-sync/`
**Now also:** `~/.claude/commands/skill-sync.md` (global)

**Problem:** Every project that uses both Claude Code and Codex needs this skill. Currently requires manual copy.

---

## Proposal: Add to Superpowers

### Why Superpowers?

1. **Already multi-agent aware** — Superpowers has skills for Claude Code, Codex, OpenCode
2. **Global by design** — Installed as plugin, available everywhere
3. **Maintained** — obra's repo has active development
4. **Precedent** — `writing-skills` already mentions agent-specific directories

### Skill Scope

```
┌─────────────────────────────────────────────────────────────┐
│                     skill-sync                               │
├─────────────────────────────────────────────────────────────┤
│  1. AUDIT                                                    │
│     - Count skills in each agent's directory                │
│     - Detect drift (missing skills)                         │
│     - Validate frontmatter                                  │
│                                                             │
│  2. SYNC                                                     │
│     - Copy missing skills between agents                    │
│     - Respect source-of-truth hierarchy                     │
│     - Handle MCP config differences                         │
│                                                             │
│  3. HEAL                                                     │
│     - Fix missing frontmatter                               │
│     - Report invalid skills                                 │
│     - Suggest manual MCP fixes                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Implementation Options

### Option A: PR to Superpowers (Recommended)

```bash
# Fork and clone
git clone https://github.com/obra/superpowers.git
cd superpowers

# Add skill
mkdir -p skills/skill-sync
# Copy SKILL.md with superpowers conventions

# PR
git checkout -b add-skill-sync
git add skills/skill-sync/
git commit -m "feat: add skill-sync for multi-agent skill synchronization"
git push origin add-skill-sync
# Open PR on GitHub
```

**Pros:**
- Integrated with superpowers ecosystem
- Automatic updates via plugin system
- Community maintenance

**Cons:**
- Requires PR approval
- Must follow superpowers conventions

### Option B: Standalone Plugin

Create `~/.claude/plugins/skill-sync/`:

```
skill-sync/
├── .claude-plugin/
│   └── manifest.json
├── skills/
│   └── skill-sync/
│       └── skill.md
└── README.md
```

**Pros:**
- Full control
- Immediate availability

**Cons:**
- No automatic updates
- Manual installation

### Option C: Homebrew Formula

```ruby
class SkillSync < Formula
  desc "Sync skills between Claude Code and Codex"
  homepage "https://github.com/anupamchugh/skill-sync"
  url "..."

  def install
    # Install to ~/.claude/commands/
  end
end
```

**Pros:**
- Easy installation (`brew install skill-sync`)
- Version management

**Cons:**
- Overkill for a single skill
- Separate from agent ecosystem

---

## Recommended Path

### Phase 1: Validate Locally
- [x] Skill works in kite-trading-platform
- [x] Copied to global commands
- [ ] Test in apple-westworld
- [ ] Test in 2+ other projects

### Phase 2: PR to Superpowers
1. Fork `obra/superpowers`
2. Study existing skill structure
3. Adapt `skill-sync` to superpowers conventions
4. Add tests if required
5. Submit PR with:
   - Problem statement
   - Use cases (multi-agent development)
   - Testing notes

### Phase 3: Document
- Add to superpowers README
- Create usage examples
- Link from CLAUDE.md templates

---

## Superpowers Skill Structure

Based on existing superpowers skills:

```markdown
---
name: skill-sync
description: Audit and sync skills between Claude Code, Codex, and OpenCode
---

# Skill Sync

## Overview
[Brief description]

## When to Use
[Trigger conditions]

## Process
[Step-by-step]

## Commands
[Bash snippets]

## Integration
[How it connects to other superpowers skills]
```

---

## MCP Sync Consideration

Current `skill-sync` handles skills but notes MCP sync is manual.

**Future enhancement:** Add MCP config translation:

```
Claude (.claude.json)  ←→  Codex (config.toml)  ←→  OpenCode (?)
```

This is complex due to format differences but valuable.

---

## Next Steps

1. **Test** skill-sync in apple-westworld (this session or next)
2. **Fork** superpowers repo
3. **Study** existing skill patterns
4. **Adapt** and PR
5. **Iterate** based on feedback

---

## Links

- [Superpowers Repo](https://github.com/obra/superpowers)
- [Skill-Sync Source](kite-trading-platform/.claude/skills/skill-sync/SKILL.md)
- [Superpowers Plugin System](~/.claude/plugins/superpowers/)

---

**Created:** 2026-01-30
**Status:** Proposal

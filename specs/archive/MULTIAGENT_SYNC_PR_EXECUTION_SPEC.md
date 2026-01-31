# Multi-Agent-Sync to Superpowers Marketplace: PR Execution Spec

**Status:** Ready to Execute  
**Date:** 2026-01-30  
**Target:** PR `multi-agent-sync` to `obra/superpowers` repository  
**Timeline:** 3-4 sessions

---

## Executive Summary

**What:** Contribute multi-agent-sync as an infrastructure skill to superpowers marketplace  
**Why:** Every multi-agent developer needs this; prevents skill fragmentation across Claude Code, Codex, OpenCode  
**How:** Fork → Adapt → Test → PR → Iterate  
**Result:** Skill available in `/plugin marketplace` as `multi-agent-sync`

---

## Part 1: Pre-Work (Session 1)

### 1.1 Validate Locally

Run multi-agent-sync in current projects to confirm it works:

```bash
# In specbeads (or apple-westworld)
/multi-agent-sync audit
# Should show: "Claude: 42 skills, Codex: 42 skills, Status: SYNCED"

# In kite-trading-platform
/multi-agent-sync audit
# Should show same or similar results

# Test drift detection
echo "# Broken" > .claude/skills/test/SKILL.md
/multi-agent-sync validate
# Should show: "Invalid: 1 (missing frontmatter)"
rm .claude/skills/test/SKILL.md
```

**Deliverable:** Validation passes locally. Document in `VALIDATION_REPORT.md`.

---

### 1.2 Study Superpowers Structure

Clone and examine existing skills:

```bash
git clone https://github.com/obra/superpowers.git
cd superpowers/skills
```

**Study these 3 skills:**

1. `writing-skills/SKILL.md` — Format, categories, description length
2. `test-driven-development/SKILL.md` — Process steps, integration points, command documentation
3. `requesting-code-review/SKILL.md` — External tool integration, error handling

**Deliverable:** 1-page conventions guide.

---

## Part 2: Adapt Multi-Agent-Sync (Session 2)

### 2.1 Create Directory Structure

```bash
cd superpowers
git checkout -b add-multi-agent-sync

mkdir -p skills/multi-agent-sync/RESOURCES
touch skills/multi-agent-sync/{SKILL.md,README.md}
touch skills/multi-agent-sync/RESOURCES/{VALIDATION_GATES.md,MCP_SYNC_GUIDE.md,TROUBLESHOOTING.md}
```

### 2.2 Write SKILL.md

**Frontmatter & Overview:**

```markdown
---
name: multi-agent-sync
description: "Audit and sync skills between Claude Code, Codex CLI, and OpenCode. Prevents skill fragmentation in multi-agent development."
categories: ["Infrastructure", "Multi-Agent", "Meta"]
---

# Multi-Agent-Sync

## Overview

Multi-agent-sync ensures your Claude agent skills stay synchronized across all installations. When you add a skill to Claude Code, multi-agent-sync automatically distributes it to Codex, OpenCode, and other agents—preventing silent failures when agents can't find required tools.

**Key Innovation:** Validation gates catch broken skills BEFORE they propagate to other agents.

## When to Use

- Session start (automatic via `session-recovery`)
- After creating a new skill
- Before running multi-agent workflows
- When switching between Claude Code and Codex
- When debugging "skill not found" errors

## Process

### 1. AUDIT: Count skills in each location

```bash
/multi-agent-sync audit
```

Output:
```
Claude Code:   42 skills
Codex CLI:     42 skills
OpenCode:      0 (not installed)
Status:        ✅ SYNCED
```

### 2. VALIDATE: Check all skills have valid frontmatter

```bash
/multi-agent-sync validate
```

This catches broken skills before they spread:
- Checks each skill has YAML frontmatter (`---` headers)
- Verifies `name:` and `description:` fields exist
- Flags skills with syntax errors

### 3. DETECT: Identify missing or drifted skills

```bash
/multi-agent-sync detect
```

If skills count mismatch found:
```
⚠️  Drift detected:
Claude Code:  42 skills
Codex CLI:    40 skills
Missing:      skill-validation-gates, defense-in-depth
```

### 4. SYNC: Copy missing skills from source to target

```bash
/multi-agent-sync sync --auto
```

Copies missing skills from Claude Code to Codex, OpenCode, etc. Only syncs valid skills (due to validation gates).

### 5. HEAL: Fix broken skills in place

```bash
/multi-agent-sync heal
```

Automatically repairs:
- Missing frontmatter (adds minimal template)
- Malformed YAML (attempts to fix)
- Reports unfixable issues for manual review

### 6. VERIFY: Confirm sync completed

```bash
/multi-agent-sync verify
```

Re-runs audit and validate to confirm everything is now in sync.

## Commands

### Quick Check

```bash
# One-line status
/multi-agent-sync status
# Returns: SYNCED ✅ or DRIFT DETECTED ❌
```

### Full Audit

```bash
# Show detailed counts per agent
/multi-agent-sync audit [--json]
```

Add `--json` for machine parsing.

### Sync with Options

```bash
# Interactive (ask before each action)
/multi-agent-sync sync

# Automatic (no prompts)
/multi-agent-sync sync --auto

# Dry run (show what would happen, don't change)
/multi-agent-sync sync --dry-run

# Sync specific skills only
/multi-agent-sync sync skill-validation-gates defense-in-depth
```

### Validation Gates (Key Feature)

```bash
# Validate before sync
/multi-agent-sync validate
```

Output shows:
```
Valid skills:    42/42 ✅
Invalid:         0 ✅
Cannot fix:      0
Status:          READY TO SYNC
```

Validation gates prevent this scenario:
```
# Before gates:
❌ Broken skill in Claude Code
→ Copied to Codex
→ Both agents broken
→ No way to detect

# With gates:
❌ Broken skill in Claude Code
→ Validation gate blocks it
→ User fixes it first
→ Then syncs when valid
→ Both agents safe
```

### MCP Sync (Manual)

```bash
# Show MCP config mapping between agents
/multi-agent-sync mcp-guide
```

This shows differences between Claude (`.mcp.json`) and Codex (`config.toml`) format so you can manually sync MCPs.

## Full Workflow Example

```bash
# 1. Check status
/multi-agent-sync status
# Output: DRIFT DETECTED ❌

# 2. See what's missing
/multi-agent-sync audit
# Output: Codex missing: skill-validation-gates, defense-in-depth

# 3. Validate source
/multi-agent-sync validate
# Output: All Claude Code skills valid ✅

# 4. Sync with validation gates active
/multi-agent-sync sync --auto
# Output: Synced 2 skills, validation passed

# 5. Verify
/multi-agent-sync status
# Output: SYNCED ✅
```

## Integration

**Works with:**
- `session-recovery` — Auto-calls multi-agent-sync on session start
- `writing-skills` — Synced automatically
- `test-driven-development` — Synced automatically
- `developing-claude-code-plugins` — Uses multi-agent-sync to distribute new plugins
- `shadowbook` — Pre-flight checks before spec workflows execute

**Hooks into:**
- Session start: Auto-audit via session-recovery
- Skill creation: Optional auto-sync after new skill
- Workflow execution: Pre-flight check in shadowbook (`bd preflight`)

## Advanced: Validation Gates Architecture

Multi-agent-sync's core innovation is **validation gates**—multi-layer checks that prevent corrupted skills from propagating:

### Layer 1: Frontmatter Check
```
Every .md file in skills/ must have YAML header:
---
name: skill-name
description: description
---
```
**Why:** Without this, Claude can't load the skill. If it's missing in source, don't copy to target.

### Layer 2: Content Validation
```
Checks that skill definitions are structurally sound:
- Has "When to Use" section
- Has "Process" or "Steps" section
- No circular references to skills
```

### Layer 3: Target Verification
```
After syncing, re-validates target location:
- File exists in target agent directory
- Frontmatter intact after copy
- No corruption during transfer
```

See [RESOURCES/VALIDATION_GATES.md](RESOURCES/VALIDATION_GATES.md) for technical details.

## Troubleshooting

**Problem:** "Skill not found in Codex"
```bash
# Check if it's missing:
/multi-agent-sync audit
# If missing, sync it:
/multi-agent-sync sync --auto
```

**Problem:** "Skill validation failed"
```bash
# See which skills are broken:
/multi-agent-sync validate
# Manually fix or auto-heal:
/multi-agent-sync heal
```

**Problem:** "Sync incomplete"
```bash
# Verify nothing was corrupted:
/multi-agent-sync verify
```

See [RESOURCES/TROUBLESHOOTING.md](RESOURCES/TROUBLESHOOTING.md) for more.

## Related

- [RESOURCES/VALIDATION_GATES.md](RESOURCES/VALIDATION_GATES.md) — Technical deep dive on gates
- [RESOURCES/MCP_SYNC_GUIDE.md](RESOURCES/MCP_SYNC_GUIDE.md) — Handle MCPs across agents
- [RESOURCES/TROUBLESHOOTING.md](RESOURCES/TROUBLESHOOTING.md) — Common issues and fixes
- [Shadowbook Integration](https://github.com/anupamchugh/shadowbook) — Using multi-agent-sync in spec-driven workflows

---

**Version:** 1.0  
**Status:** Superpowers Marketplace Contribution  
**Categories:** Infrastructure, Multi-Agent, Meta
```

**Deliverable:** Complete, formatted SKILL.md ready for superpowers.

---

### 2.3 Create RESOURCES/ Files

**File: RESOURCES/VALIDATION_GATES.md**

```markdown
# Multi-Agent-Sync Validation Gates

## What Are Validation Gates?

Validation gates are **multi-layer checks** that prevent broken skills from propagating between agents.

### Problem They Solve

Without gates:
```
1. Developer creates broken skill in Claude Code
   (missing frontmatter, YAML syntax error, etc.)

2. multi-agent-sync copies it to Codex (no validation)

3. Both agents now broken

4. No way to detect what happened
```

With gates:
```
1. Developer creates broken skill in Claude Code

2. multi-agent-sync validate checks it → FAILS

3. User must fix before sync

4. Both agents stay healthy
```

## Gate Types

### Gate 1: Frontmatter Validation
Checks YAML header exists and has required fields:
```
---
name: skill-name        ← REQUIRED
description: "..."      ← REQUIRED
---
```

**Failure:** Missing `---` or missing fields → skill not synced

### Gate 2: Structure Validation
Checks skill has minimum required sections:
- "When to Use" (what triggers this skill)
- "Process" or "Steps" (how it works)

**Failure:** Missing sections → skill flagged as incomplete

### Gate 3: Reference Validation
Checks skill doesn't reference non-existent skills:
```
This skill mentions: `writing-skills`, `test-driven-development`
Are these skills available? YES ✅
```

**Failure:** References missing skill → warning (doesn't block)

### Gate 4: Post-Sync Verification
After copying, re-validates target:
```
1. File copied to Codex
2. Revalidate in Codex location
3. Confirm no corruption during transfer
```

**Failure:** Corruption detected → sync marked failed

## Implementation

```bash
# Validate gates are part of /multi-agent-sync validate
/multi-agent-sync validate

# Output example:
Validating skills...
├─ writing-skills: ✅ PASS (all gates)
├─ test-driven-development: ✅ PASS
├─ custom-skill-v1: ❌ FAIL
│   └─ Gate 1 (Frontmatter): FAIL - missing 'description'
└─ another-skill: ⚠️  WARN
    └─ Gate 3 (References): WARNING - references unknown skill
```

## Why This Matters

**For developers:**
- Broken skills are caught at source, not after propagation
- Saves debugging time when agents have diverged

**For multi-agent workflows:**
- Guarantees all agents have same, valid set of skills
- Enables confident skill compaction (remove unused skills safely)

**For superpowers ecosystem:**
- Prevents skill corruption from spreading across users
- Makes multi-agent-sync reliable enough for automation (pre-flight checks, CI/CD)

See main [SKILL.md](../SKILL.md) for usage.
```

**File: RESOURCES/MCP_SYNC_GUIDE.md**

```markdown
# MCP Configuration Sync

Multi-agent-sync handles SKILLS automatically. MCPs (Model Context Protocol servers) require manual sync.

## The Problem

Skills are files. Skills sync easily:
```
.claude/skills/ → rsync → ~/.codex/skills/
```

MCPs are configuration. Different agents use different config formats:

| Agent | Config File | Format | MCP Example |
|-------|-------------|--------|-------------|
| Claude Code | `.mcp.json` | JSON | `"superpowers-chrome": {...}` |
| Codex | `~/.codex/config.toml` | TOML | `chrome = {...}` |
| OpenCode | `config.json` | JSON | `"MCPs": {"chrome": {...}}` |

## Solution: Manual Mapping

After syncing skills, MCPs must be manually configured to match.

### Step 1: Find MCP Name Differences

Claude `.mcp.json`:
```json
{
  "superpowers-chrome": {
    "command": "npx",
    "args": ["@modelcontextprotocol/server-chrome"]
  }
}
```

Codex `~/.codex/config.toml`:
```toml
[mcp]
chrome = "npx @modelcontextprotocol/server-chrome"
# Note: Name changed from "superpowers-chrome" to "chrome"
```

### Step 2: Update Target Config

If you sync skills that use MCPs:

```bash
# Skill synced to Codex, but it references "superpowers-chrome" MCP
# You need to:
# 1. Check if Codex has this MCP
# 2. If yes but different name: update skill to use "chrome" instead
# 3. If no: add to Codex config.toml
```

### Common MCP Renamings

| Claude Name | Codex Name | 
|-------------|------------|
| `superpowers-chrome` | `chrome` |
| `xcodebuild` | `xcodebuild` (same) |
| `filesystem` | `fs` (sometimes) |

## Automated MCP Sync (Future)

For now, manual. Future version of multi-agent-sync might include:
```bash
/multi-agent-sync mcp-sync --from claude --to codex
# Translates .mcp.json to config.toml format
```

See main [SKILL.md](../SKILL.md) for the `mcp-guide` command.
```

**File: RESOURCES/TROUBLESHOOTING.md**

```markdown
# Multi-Agent-Sync Troubleshooting

## Common Issues

### Issue: Skill Shows as Missing But I Know I Created It

**Cause:** Skill is in Claude Code but not in Codex directory

**Solution:**
```bash
/multi-agent-sync audit
# If shows gap, sync it:
/multi-agent-sync sync --auto
```

### Issue: Validation Gate Blocks My Skill

**Cause:** Your skill is missing frontmatter or required sections

**Example:**
```
❌ my-skill: FAIL - Gate 1 (Frontmatter)
   Missing field: 'description'
```

**Solution:** Add missing field to skill frontmatter

```markdown
---
name: my-skill
description: "What this skill does"    ← ADD THIS
---

# My Skill
...
```

### Issue: Sync Says "Drift Detected" But Counts Look Same

**Cause:** Skill counts are equal but skill NAMES differ

**Example:**
```
Claude:  42 skills
Codex:   42 skills
Status:  DRIFT (different skills)
```

**Solution:**
```bash
/multi-agent-sync audit --verbose
# Shows which skills differ by name
```

### Issue: Post-Sync, Codex Still Can't Find Skill

**Cause:** File copied but Codex restarted before loading

**Solution:**
```bash
# Restart Codex agent
# Or reload skills
codex reload-skills
# Then verify
/multi-agent-sync verify
```

### Issue: Validation Gate Too Strict

**Cause:** Your skill is valid but gate rejects it

**Example:** You have a custom section structure

**Workaround:** Use `--skip-validation` (dangerous)
```bash
/multi-agent-sync sync --auto --skip-validation
# ⚠️  Only use if you're sure skill is valid
```

**Better:** Fix gate logic or contact maintainer

See main [SKILL.md](../SKILL.md) for command reference.
```

---

## Part 3: Test Locally (Session 2)

### 3.1 Load Skill in Superpowers

```bash
# In superpowers directory
cd superpowers

# Test that skill loads
cat skills/multi-agent-sync/SKILL.md | head -20
# Should show valid frontmatter
```

### 3.2 Run Commands Manually

```bash
# Copy skill into a test project
cp -r skills/multi-agent-sync ~/test-project/.claude/skills/

# Test the skill
cd ~/test-project
/multi-agent-sync audit
# Should work without errors
```

### 3.3 Verify Integration References

```bash
# Check that SKILL.md references other skills correctly
grep -n "writing-skills\|test-driven-development\|session-recovery" \
  skills/multi-agent-sync/SKILL.md
# Should show valid references
```

**Deliverable:** All tests pass. Document in `TEST_REPORT.md`.

---

## Part 4: Submit PR (Session 3)

### 4.1 Prepare Commit

```bash
cd superpowers
git status
# Should show:
# new file:   skills/multi-agent-sync/SKILL.md
# new file:   skills/multi-agent-sync/README.md
# new file:   skills/multi-agent-sync/RESOURCES/VALIDATION_GATES.md
# new file:   skills/multi-agent-sync/RESOURCES/MCP_SYNC_GUIDE.md
# new file:   skills/multi-agent-sync/RESOURCES/TROUBLESHOOTING.md

git add skills/multi-agent-sync/

git commit -m "feat: add multi-agent-sync for skill synchronization across agents

- Audit skills across Claude Code, Codex CLI, OpenCode
- Validation gates prevent broken skills from propagating
- Automatic sync with frontmatter healing
- Resolves multi-agent development workflow friction

Includes:
- SKILL.md: Complete skill documentation
- RESOURCES/VALIDATION_GATES.md: Technical deep dive
- RESOURCES/MCP_SYNC_GUIDE.md: Agent config mapping
- RESOURCES/TROUBLESHOOTING.md: Common issues and fixes

Tested in specbeads and kite-trading-platform."
```

### 4.2 Push to Fork

```bash
git push origin add-multi-agent-sync
```

### 4.3 Open PR on GitHub

Go to: https://github.com/obra/superpowers

Click: "Compare & pull request" (or create new PR)

**PR Title:**
```
feat: add multi-agent-sync for skill synchronization across agents
```

**PR Description:**

```markdown
## Multi-Agent-Sync: Infrastructure for Multi-Agent Development

### Problem
Developers using multiple Claude agents (Code, Codex CLI, OpenCode, etc.) manually copy skills between installations. This causes:
- **Skill fragmentation:** Same skill exists in 2+ places with divergence
- **Validation failures:** Broken skills copy undetected to other agents
- **Silent failures:** Workflows fail mid-run because agents have different tools

### Solution
`multi-agent-sync` provides automated skill management:

- **Audit:** Detect which skills are out of sync across agents
- **Validation Gates:** Prevent corrupted skills from propagating
- **Sync:** Copy missing skills with automatic frontmatter healing
- **Verify:** Confirm sync completed successfully

### Key Innovation: Validation Gates

Validation gates ensure skills are valid BEFORE they sync:

```
Before gates:
❌ Broken skill (missing frontmatter)
  → Copied to Codex
  → Both agents broken
  → No detection

With gates:
❌ Broken skill
  → Validation gate blocks it
  → User fixes first
  → Then syncs
  → Both agents safe
```

### Use Cases

1. **Session Start** — Auto-called via `session-recovery` 
2. **Multi-Agent Workflows** — Pre-flight check before `bd cook` in shadowbook
3. **Skill Maintenance** — Compact unused skills across agents
4. **CI/CD Integration** — Auto-sync in automated workflows

### Testing

- [x] Validated in specbeads (42 skills, SYNCED)
- [x] Validated in kite-trading-platform (40+ skills, SYNCED)
- [x] Frontmatter validation catches real errors
- [x] Sync runs without corruption
- [x] All gate layers pass

### Files in This PR

- `skills/multi-agent-sync/SKILL.md` — Main skill (400 lines)
- `skills/multi-agent-sync/RESOURCES/VALIDATION_GATES.md` — Gate architecture
- `skills/multi-agent-sync/RESOURCES/MCP_SYNC_GUIDE.md` — Agent config mapping
- `skills/multi-agent-sync/RESOURCES/TROUBLESHOOTING.md` — Common issues

### Integration

Works with:
- `session-recovery` — Auto-calls multi-agent-sync on startup
- `writing-skills` — Synced automatically
- `test-driven-development` — Synced automatically
- Future: `shadowbook` — Pre-flight check in spec workflows

### Related Issues

Addresses: Multi-agent development workflow friction (prevents "skill not found" errors)

### How to Test

```bash
# Install this skill
/plugin install superpowers
/multi-agent-sync audit
# Should show skill counts and sync status
```

### Questions for Reviewers

1. Should validation gates be stricter or more lenient?
2. Should we add `--auto-sync` flag for CI/CD use?
3. Should MCP sync be automated in future versions?

Thanks for reviewing!
```

---

## Part 5: Respond to Feedback (Session 4)

### 5.1 Monitor PR

Watch for comments from @obra (maintainer).

**Common feedback:**
- "Rename X to match conventions"
- "Add more examples"
- "Move this to RESOURCES/"
- "Reference this other skill"

### 5.2 Iterate

Make requested changes:

```bash
# Edit SKILL.md based on feedback
vim skills/multi-agent-sync/SKILL.md

# Push changes (PR auto-updates)
git add skills/multi-agent-sync/
git commit -m "refactor: multi-agent-sync per review feedback

- Updated X per suggestion
- Added Y example
- Clarified Z wording"
git push origin add-multi-agent-sync
```

### 5.3 Merge

Once approved, obra merges or you merge if you have access.

---

## Success Criteria

- [ ] Validation Report complete (locally works)
- [ ] Study Notes document conventions
- [ ] SKILL.md matches superpowers style
- [ ] RESOURCES/ files are comprehensive
- [ ] Tests pass locally
- [ ] Commit message is clear
- [ ] PR description explains problem + solution
- [ ] PR submitted to obra/superpowers
- [ ] Feedback addressed (if any)
- [ ] PR merged

---

## Timeline

| Task | Duration | Session |
|------|----------|---------|
| Validate + Study | 1 session | 1 |
| Write + Test | 1 session | 2 |
| Submit PR | 0.5 session | 3 |
| Iterate feedback | 0.5-1 session | 3-4 |
| **Total** | **3-4 sessions** | — |

---

## Deliverables

```
specbeads/specs/
├── VALIDATION_REPORT.md          (Session 1)
├── STUDY_NOTES.md                (Session 1)
└── [Merged PR on GitHub]          (Session 3)

superpowers/ (on GitHub)
└── skills/multi-agent-sync/
    ├── SKILL.md
    ├── README.md
    └── RESOURCES/
        ├── VALIDATION_GATES.md
        ├── MCP_SYNC_GUIDE.md
        └── TROUBLESHOOTING.md
```

---

**Document Version:** 2.0  
**Status:** Ready to execute  
**Updated:** 2026-01-30
**Name Change:** skill-sync → multi-agent-sync

# Skill-Sync to Superpowers Marketplace: PR Execution Spec

**Status:** Ready to Execute  
**Date:** 2026-01-30  
**Target:** PR `skill-sync` to `obra/superpowers` repository  
**Timeline:** 3-4 sessions

---

## Executive Summary

**What:** Contribute skill-sync as an infrastructure skill to superpowers marketplace  
**Why:** Every multi-agent developer needs this; prevents skill fragmentation  
**How:** Fork → Adapt → Test → PR → Iterate  
**Result:** Skill available in `/plugin marketplace`

---

## Part 1: Pre-Work (Session 1)

### 1.1 Validate Locally

Run skill-sync in current projects to confirm it works:

```bash
# In apple-westworld
/skill-sync audit
# Should show: "Claude: 42 skills, Codex: 42 skills, Status: SYNCED"

# In kite-trading-platform
/skill-sync audit
# Should show same or similar results

# Test drift detection
echo "# Broken" > .claude/skills/test/SKILL.md
/skill-sync validate
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

1. `writing-skills/SKILL.md`
   - Note: Frontmatter format, categories, description length
   - Check: How it references other skills
   - Pattern: Section structure

2. `test-driven-development/SKILL.md`
   - Note: When to use, Process steps, integration points
   - Check: Code block styling, command documentation
   - Pattern: How it ties to other superpowers skills

3. `requesting-code-review/SKILL.md`
   - Note: Integration points, cross-skill references
   - Check: How it handles external tools (superpowers:code-reviewer)
   - Pattern: Error handling, failure modes

**Document in `STUDY_NOTES.md`:**
```markdown
# Superpowers Skill Conventions

## Frontmatter
---
name: skill-name
description: "Single line, 80 chars max"
categories: ["Category1", "Category2"]
---

## Sections
1. Overview (2-3 sentences)
2. When to Use (bullet list)
3. Process (numbered steps or flowchart)
4. Commands (code blocks with bash)
5. Integration (references to other skills)
6. Related (links to documentation)

## Code Blocks
- Use triple backticks with language: ```bash, ```markdown
- Commands should be copy-paste ready
- Include example output

## References
- Other skills: `SKILL_NAME` or [SKILL_NAME](file:///...SKILL.md)
- External: Full GitHub URLs
```

**Deliverable:** 1-page conventions guide.

---

## Part 2: Adapt Skill-Sync (Session 2)

### 2.1 Create Directory Structure

```bash
cd superpowers
git checkout -b add-skill-sync

mkdir -p skills/skill-sync/RESOURCES
touch skills/skill-sync/{SKILL.md,README.md}
touch skills/skill-sync/RESOURCES/{VALIDATION_GATES.md,MCP_SYNC_GUIDE.md,TROUBLESHOOTING.md}
```

### 2.2 Write SKILL.md

**Template:**

```markdown
---
name: skill-sync
description: "Audit and sync skills between Claude Code, Codex CLI, and OpenCode. Prevents skill fragmentation in multi-agent development."
categories: ["Infrastructure", "Multi-Agent", "Meta"]
---

# Skill Sync

## Overview

Skill-sync ensures your Claude agent skills stay synchronized across all installations. When you add a skill to Claude Code, skill-sync automatically distributes it to Codex, OpenCode, and other agents—preventing silent failures when agents can't find required tools.

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
/skill-sync audit
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
/skill-sync validate
```

This catches broken skills before they spread:
- Checks each skill has YAML frontmatter (`---` headers)
- Verifies `name:` and `description:` fields exist
- Flags skills with syntax errors

### 3. DETECT: Identify missing or drifted skills

```bash
/skill-sync detect
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
/skill-sync sync --auto
```

Copies missing skills from Claude Code to Codex, OpenCode, etc. Only syncs valid skills (due to validation gates).

### 5. HEAL: Fix broken skills in place

```bash
/skill-sync heal
```

Automatically repairs:
- Missing frontmatter (adds minimal template)
- Malformed YAML (attempts to fix)
- Reports unfixable issues for manual review

### 6. VERIFY: Confirm sync completed

```bash
/skill-sync verify
```

Re-runs audit and validate to confirm everything is now in sync.

## Commands

### Quick Check

```bash
# One-line status
/skill-sync status
# Returns: SYNCED ✅ or DRIFT DETECTED ❌
```

### Full Audit

```bash
/skill-sync audit [--json]
```

Shows detailed counts per agent. Add `--json` for machine parsing.

### Sync with Options

```bash
# Interactive (ask before each action)
/skill-sync sync

# Automatic (no prompts)
/skill-sync sync --auto

# Dry run (show what would happen, don't change)
/skill-sync sync --dry-run

# Sync specific skills only
/skill-sync sync skill-validation-gates defense-in-depth
```

### Validation Gates (Key Feature)

```bash
# Validate before sync
/skill-sync validate

# Output shows:
# Valid skills:    42/42 ✅
# Invalid:         0 ✅
# Cannot fix:      0
# Status:          READY TO SYNC
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
/skill-sync mcp-guide
```

This shows differences between Claude (`.mcp.json`) and Codex (`config.toml`) format so you can manually sync MCPs.

## Full Workflow Example

```bash
# 1. Check status
/skill-sync status
# Output: DRIFT DETECTED ❌

# 2. See what's missing
/skill-sync audit
# Output: Codex missing: skill-validation-gates, defense-in-depth

# 3. Validate source
/skill-sync validate
# Output: All Claude Code skills valid ✅

# 4. Sync with validation gates active
/skill-sync sync --auto
# Output: Synced 2 skills, validation passed

# 5. Verify
/skill-sync status
# Output: SYNCED ✅
```

## Integration

**Works with:**
- `session-recovery` — Auto-calls skill-sync on session start
- `writing-skills` — Synced automatically
- `test-driven-development` — Synced automatically
- `developing-claude-code-plugins` — Uses skill-sync to distribute new plugins

**Hooks into:**
- Session start: Auto-audit via session-recovery
- Skill creation: Optional auto-sync after new skill
- Workflow execution: Pre-flight check in shadowbook

## Advanced: Validation Gates Architecture

Skill-sync's core innovation is **validation gates**—multi-layer checks that prevent corrupted skills from propagating:

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
/skill-sync audit
# If missing, sync it:
/skill-sync sync --auto
```

**Problem:** "Skill validation failed"
```bash
# See which skills are broken:
/skill-sync validate
# Manually fix or auto-heal:
/skill-sync heal
```

**Problem:** "Sync incomplete"
```bash
# Verify nothing was corrupted:
/skill-sync verify
```

See [RESOURCES/TROUBLESHOOTING.md](RESOURCES/TROUBLESHOOTING.md) for more.

## Related

- [RESOURCES/VALIDATION_GATES.md](RESOURCES/VALIDATION_GATES.md) — Technical deep dive on gates
- [RESOURCES/MCP_SYNC_GUIDE.md](RESOURCES/MCP_SYNC_GUIDE.md) — Handle MCPs across agents
- [RESOURCES/TROUBLESHOOTING.md](RESOURCES/TROUBLESHOOTING.md) — Common issues and fixes
- [Shadowbook Integration](https://github.com/anupamchugh/shadowbook) — Using skill-sync in spec-driven workflows

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
# Skill-Sync Validation Gates

## What Are Validation Gates?

Validation gates are **multi-layer checks** that prevent broken skills from propagating between agents.

### Problem They Solve

Without gates:
```
1. Developer creates broken skill in Claude Code
   (missing frontmatter, YAML syntax error, etc.)

2. skill-sync copies it to Codex (no validation)

3. Both agents now broken

4. No way to detect what happened
```

With gates:
```
1. Developer creates broken skill in Claude Code

2. skill-sync validate checks it → FAILS

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
# Validate gates are part of /skill-sync validate
/skill-sync validate

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
- Makes skill-sync reliable enough for automation (pre-flight checks, CI/CD)

See main [SKILL.md](../SKILL.md) for usage.
```

**File: RESOURCES/MCP_SYNC_GUIDE.md**

```markdown
# MCP Configuration Sync

Skill-sync handles SKILLS automatically. MCPs (Model Context Protocol servers) require manual sync.

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

For now, manual. Future version of skill-sync might include:
```bash
/skill-sync mcp-sync --from claude --to codex
# Translates .mcp.json to config.toml format
```

See main [SKILL.md](../SKILL.md) for the `mcp-guide` command.
```

**File: RESOURCES/TROUBLESHOOTING.md**

```markdown
# Skill-Sync Troubleshooting

## Common Issues

### Issue: Skill Shows as Missing But I Know I Created It

**Cause:** Skill is in Claude Code but not in Codex directory

**Solution:**
```bash
/skill-sync audit
# If shows gap, sync it:
/skill-sync sync --auto
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
/skill-sync audit --verbose
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
/skill-sync verify
```

### Issue: Validation Gate Too Strict

**Cause:** Your skill is valid but gate rejects it

**Example:** You have a custom section structure

**Workaround:** Use `--skip-validation` (dangerous)
```bash
/skill-sync sync --auto --skip-validation
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
# (simulate how Claude Code would load it)
cat skills/skill-sync/SKILL.md | head -20
# Should show valid frontmatter
```

### 3.2 Run Commands Manually

```bash
# Copy skill-sync into a test project
cp -r skills/skill-sync ~/test-project/.claude/skills/

# Test the skill
cd ~/test-project
/skill-sync audit
# Should work without errors
```

### 3.3 Verify Integration References

```bash
# Check that SKILL.md references other skills correctly
grep -n "writing-skills\|test-driven-development\|session-recovery" \
  skills/skill-sync/SKILL.md
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
# new file:   skills/skill-sync/SKILL.md
# new file:   skills/skill-sync/README.md
# new file:   skills/skill-sync/RESOURCES/VALIDATION_GATES.md
# new file:   skills/skill-sync/RESOURCES/MCP_SYNC_GUIDE.md
# new file:   skills/skill-sync/RESOURCES/TROUBLESHOOTING.md

git add skills/skill-sync/

git commit -m "feat: add skill-sync for multi-agent skill synchronization

- Audit skills across Claude Code, Codex CLI, OpenCode
- Validation gates prevent broken skills from propagating
- Automatic sync with frontmatter healing
- Resolves multi-agent development workflow friction

Includes:
- SKILL.md: Complete skill documentation
- RESOURCES/VALIDATION_GATES.md: Technical deep dive
- RESOURCES/MCP_SYNC_GUIDE.md: Agent config mapping
- RESOURCES/TROUBLESHOOTING.md: Common issues and fixes

Tested in apple-westworld and kite-trading-platform."
```

### 4.2 Push to Fork

```bash
git push origin add-skill-sync
```

### 4.3 Open PR on GitHub

Go to: https://github.com/obra/superpowers

Click: "Compare & pull request" (or create new PR)

**PR Title:**
```
feat: add skill-sync for multi-agent skill synchronization
```

**PR Description:**

```markdown
## Skill-Sync: Infrastructure for Multi-Agent Development

### Problem
Developers using multiple Claude agents (Code, Codex CLI, OpenCode, etc.) manually copy skills between installations. This causes:
- **Skill fragmentation:** Same skill exists in 2+ places with divergence
- **Validation failures:** Broken skills copy undetected to other agents
- **Silent failures:** Workflows fail mid-run because agents have different tools

### Solution
`skill-sync` provides automated skill management:

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
2. **Multi-Agent Workflows** — Pre-flight check before `bd cook`
3. **Skill Maintenance** — Compact unused skills across agents
4. **CI/CD Integration** — Auto-sync in automated workflows

### Testing

- [x] Validated in apple-westworld (42 skills, SYNCED)
- [x] Validated in kite-trading-platform (40+ skills, SYNCED)
- [x] Frontmatter validation catches real errors
- [x] Sync runs without corruption
- [x] All gate layers pass

### Files in This PR

- `skills/skill-sync/SKILL.md` — Main skill (400 lines)
- `skills/skill-sync/RESOURCES/VALIDATION_GATES.md` — Gate architecture
- `skills/skill-sync/RESOURCES/MCP_SYNC_GUIDE.md` — Agent config mapping
- `skills/skill-sync/RESOURCES/TROUBLESHOOTING.md` — Common issues

### Integration

Works with:
- `session-recovery` — Auto-calls skill-sync on startup
- `writing-skills` — Synced automatically
- `test-driven-development` — Synced automatically
- Future: `shadowbook` — Pre-flight check in spec workflows

### Related Issues

Addresses: Multi-agent development workflow friction (prevents "skill not found" errors)

### How to Test

```bash
# Install this skill
/plugin install superpowers
/skill-sync audit
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
vim skills/skill-sync/SKILL.md

# Push changes (PR auto-updates)
git add skills/skill-sync/
git commit -m "refactor: skill-sync per review feedback

- Updated X per suggestion
- Added Y example
- Clarified Z wording"
git push origin add-skill-sync
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
apple-westworld/specs/
├── VALIDATION_REPORT.md          (Session 1)
├── STUDY_NOTES.md                (Session 1)
└── [Merged PR on GitHub]          (Session 3)

superpowers/ (on GitHub)
└── skills/skill-sync/
    ├── SKILL.md
    ├── README.md
    └── RESOURCES/
        ├── VALIDATION_GATES.md
        ├── MCP_SYNC_GUIDE.md
        └── TROUBLESHOOTING.md
```

---

**Document Version:** 1.0  
**Status:** Ready to execute  
**Last Updated:** 2026-01-30

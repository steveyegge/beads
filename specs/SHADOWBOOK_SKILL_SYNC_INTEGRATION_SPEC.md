# Shadowbook + Skill-Sync Integration Specification

**Status:** Implementation Ready  
**Date:** 2026-01-30  
**Scope:** Integrate skill-sync as infrastructure layer in shadowbook workflows  
**Target:** Ensure skills are available before specs are executed

---

## The Problem

**Current State:** Shadowbook tracks narrative drift (specs ↔ code links) but assumes skills exist.

**Failure Mode:**
```
bd cook workflow-name
  → Spec references skill-validation-gates
  → Skill missing in Codex (only in Claude Code)
  → Workflow fails mid-execution
  → User doesn't know why
```

**Solution:** Pre-flight skill validation before any workflow runs.

---

## Part 1: Architecture

### Shadowbook Layers (With Skill-Sync)

```
┌──────────────────────────────────────────┐
│  Session Start                           │
├──────────────────────────────────────────┤
│  1. skill-sync audit (NEW)               │
│     └─ Detect skill drift across agents  │
│                                          │
│  2. skill-sync validate (NEW)            │
│     └─ Check all skills have frontmatter │
│                                          │
│  3. bd ready (EXISTING)                  │
│     └─ Check hosts have no blockers      │
│                                          │
│  4. bd spec scan (EXISTING)              │
│     └─ Check narrative drift             │
├──────────────────────────────────────────┤
│  Workflow Execution                      │
├──────────────────────────────────────────┤
│  5. bd cook [workflow] (EXISTING)        │
│     └─ Safe to run: skills synced        │
│                                          │
│  6. Host completes                       │
│     └─ skill-sync --compact (NEW)        │
│        └─ Archive unused skills          │
└──────────────────────────────────────────┘
```

---

## Part 2: Command Changes

### New Command: `bd preflight`

**Purpose:** Run all pre-flight checks before workflows

```bash
bd preflight [--auto-sync]
```

**Checks (in order):**
1. Skill sync audit (count skills per agent)
2. Skill validation (check frontmatter)
3. Host readiness (bd ready)
4. Narrative drift (bd spec scan)

**Output:**
```
┌─ PREFLIGHT ─────────────────────────┐
│ Skills: 42/42 ✅ SYNCED             │
│ Valid:  42/42 ✅ NO ERRORS          │
│ Hosts:  12/15 ⚠️  3 BLOCKED         │
│ Specs:  8/8   ✅ NO DRIFT           │
├─────────────────────────────────────┤
│ Status: ✅ READY TO COOK            │
└─────────────────────────────────────┘
```

**Exit codes:**
- `0`: All pass, safe to proceed
- `1`: Skill drift (fix with `--auto-sync`)
- `2`: Host/spec issues (manual review required)

**With `--auto-sync`:**
```bash
bd preflight --auto-sync
# If skill drift detected:
#   → Prompts: "Sync [N] missing skills?"
#   → If yes: runs skill-sync
#   → If no: exits with error
```

---

### Modified: `bd cook [workflow]`

**Default:** Always run `bd preflight` before executing

```bash
bd cook login-workflow

# Automatically runs:
# 1. bd preflight
# 2. If preflight passes: execute workflow
# 3. If preflight fails: abort, show error
```

**To skip (dangerous):**
```bash
bd cook login-workflow --skip-preflight
```

**Rationale:** Workflows depend on skills. Better to fail early than mid-execution.

---

### New: `bd close [id] --compact-skills`

**Purpose:** Compact unused skills when closing hosts

```bash
bd close bd-a1b2 --compact-skills

# Shadowbook checks:
# - Which skills did bd-a1b2 use?
# - Are those skills used by other hosts?
# - If not: remove from all agents
```

**Output:**
```
✅ Closed: bd-a1b2

Skills used by this host:
├─ skill-validation-gates (keep: used by bd-x2y3)
├─ testing-skills (compact: no other users)
└─ writing-skills (keep: used by bd-z9w8)

Compact 1 skill? (y/n)
```

---

## Part 3: Shadowbook File Changes

### File: `.beads/config.json` (NEW)

Add skill-sync configuration:

```json
{
  "version": 1,
  "agent_directories": {
    "claude-code": ".claude/skills",
    "codex": "~/.codex/skills",
    "opencode": "~/.opencode/skills"
  },
  "preflight": {
    "enabled": true,
    "check_skills": true,
    "check_validation": true,
    "check_hosts": true,
    "check_drift": true
  },
  "skill_sync": {
    "auto_sync_on_workflow": false,
    "compact_on_close": true,
    "validation_gates_required": true
  }
}
```

### File: `cmd/bd/cook.go` (MODIFIED)

Add preflight step:

```go
// Before executing workflow
func (c *cookCmd) Run(ctx context.Context, args []string) error {
    // NEW: Run preflight checks
    if !c.skipPreflight {
        result, err := preflight.Check(ctx, c.config)
        if err != nil {
            return fmt.Errorf("preflight failed: %w", err)
        }
        if !result.Pass {
            return fmt.Errorf("not ready: %s", result.Message)
        }
    }
    
    // EXISTING: Cook workflow
    return c.cookWorkflow(ctx, args)
}
```

### File: `cmd/bd/preflight.go` (NEW)

```go
package bd

import (
    "context"
    "fmt"
)

type PreflightResult struct {
    Pass     bool
    Message  string
    Details  map[string]string
}

func (c *cookCmd) runPreflight(ctx context.Context) (*PreflightResult, error) {
    result := &PreflightResult{
        Details: make(map[string]string),
    }

    // 1. Skill sync audit
    syncStatus := c.skillSync.Audit(ctx)
    result.Details["skills"] = syncStatus.String()
    
    // 2. Skill validation
    validStatus := c.skillSync.Validate(ctx)
    result.Details["validation"] = validStatus.String()
    
    // 3. Host readiness
    readyHosts := c.ready(ctx)
    result.Details["hosts"] = fmt.Sprintf("%d/%d ready", readyHosts.Ready, readyHosts.Total)
    
    // 4. Spec drift
    specStatus := c.scanSpecs(ctx)
    result.Details["specs"] = specStatus.String()
    
    // All pass?
    result.Pass = syncStatus.Passed && validStatus.Passed && readyHosts.AllReady && specStatus.Passed
    
    if result.Pass {
        result.Message = "✅ READY TO COOK"
    } else {
        result.Message = "❌ PREFLIGHT FAILED"
    }
    
    return result, nil
}
```

### File: `cmd/bd/close.go` (MODIFIED)

Add `--compact-skills` flag:

```go
func (c *closeCmd) Run(ctx context.Context, args []string) error {
    // EXISTING: Close host
    err := c.closeHost(ctx, c.hostID)
    if err != nil {
        return err
    }
    
    // NEW: Compact skills if requested
    if c.compactSkills {
        err := c.compactUnusedSkills(ctx, c.hostID)
        if err != nil {
            return fmt.Errorf("compact failed (host closed anyway): %w", err)
        }
    }
    
    return nil
}
```

---

## Part 4: Skill-Sync Modifications

### New Skill-Sync Modes

#### Mode: `--check-only`

Returns exit code without making changes:

```bash
/skill-sync audit --check-only
# exit 0 if synced, exit 1 if drift
# No output, suitable for scripts
```

#### Mode: `--auto-sync`

Auto-fix without prompting:

```bash
/skill-sync sync --auto-sync
# If drift: immediately sync, don't ask
# Useful in CI or automated workflows
```

#### Mode: `--json`

Machine-readable output:

```bash
/skill-sync audit --json
# Returns JSON instead of human text
# Suitable for parsing in shadowbook code
```

**Example output:**
```json
{
  "status": "drift_detected",
  "agents": {
    "claude_code": 42,
    "codex": 40,
    "opencode": 0
  },
  "missing": ["skill-validation-gates", "defense-in-depth"],
  "invalid": []
}
```

---

## Part 5: Workflow: Pre-Session Setup

### Automatic on Session Start

Add to `~/.claude/commands/session-recovery.md`:

```bash
# Step 3: Skill synchronization (NEW)
echo "Checking skill synchronization..."
/skill-sync audit --json > /tmp/skill-audit.json

# Parse result
SKILL_STATUS=$(jq -r '.status' /tmp/skill-audit.json)

if [ "$SKILL_STATUS" = "drift_detected" ]; then
    echo "⚠️  Skill drift detected"
    MISSING=$(jq -r '.missing | join(", ")' /tmp/skill-audit.json)
    echo "Missing in secondary agents: $MISSING"
    
    # Auto-sync if enabled
    if [ "$AUTO_SYNC_ON_SESSION" = "true" ]; then
        echo "Auto-syncing skills..."
        /skill-sync sync --auto-sync
    else
        echo "Run: /skill-sync sync"
    fi
else
    echo "✅ Skills synchronized"
fi
```

---

## Part 6: Workflow: Recipe Example

### Example Workflow Spec

File: `specs/workflows/login-feature.md`

```markdown
# Login Feature Workflow

## Narrative
Implement OAuth2 login with JWT tokens.

## Required Skills
- skill-validation-gates (ensures specs match code)
- writing-skills (documentation)
- testing-anti-patterns (quality gates)

## Steps
1. Create spec in `specs/auth/oauth2.md`
2. Link beads to spec: `bd create --spec-id specs/auth/oauth2.md`
3. Implement OAuth2 endpoints
4. Document API
5. Create tests
6. Close when done: `bd close <id> --compact-skills`

## Gates
- Human review before merge: `bd gate --human`
- CI pipeline passes: `bd gate --gh:run`
```

### Execution

```bash
# User runs:
bd cook login-feature

# Shadowbook does:
# 1. bd preflight
#    ├─ Check skills available (oauth2-skill, testing-skills, etc.)
#    ├─ Validate frontmatter
#    └─ Check hosts ready
# 2. Execute workflow
# 3. Show ready hosts from this workflow
```

---

## Part 7: Integration Points

| Component | Integration |
|-----------|-------------|
| **Session Recovery** | Auto-call skill-sync audit on start |
| **bd cook** | Implicit preflight before every workflow |
| **bd close** | Optional skill compaction on host close |
| **bd preflight** | New command to check all layers |
| **Skill-Sync** | New flags: `--check-only`, `--auto-sync`, `--json` |
| **AGENTS.md** | Document skill-sync in shadowbook workflows |

---

## Part 8: Implementation Order

### Phase 1: Skill-Sync Foundation ✅
- [x] Add `--check-only`, `--auto-sync`, `--json` flags to skill-sync
- [ ] Test in isolation
- [x] Document in skill-sync SKILL.md

### Phase 2: Shadowbook Integration ✅
- [x] Create `cmd/bd/preflight.go` (already existed, modified)
- [ ] Add `.beads/config.json` schema (optional - config hardcoded for now)
- [ ] Modify `cmd/bd/cook.go` to call preflight (optional - can be done separately)
- [x] Add `--compact-skills` to close.go
- [x] Add skill sync check to `bd preflight --check`
- [x] Add `--auto-sync` flag to `bd preflight`

### Phase 3: Documentation
- [ ] Update AGENTS.md with workflow examples
- [ ] Create tutorial: "Pre-Flight Checks in Shadowbook"
- [ ] Add to shadowbook README

### Phase 4: Testing
- [ ] Test preflight in apple-westworld
- [ ] Test preflight in kite-trading-platform
- [ ] Verify auto-sync works
- [ ] Verify skill compaction works

---

## Part 9: Success Criteria

- [ ] `bd preflight` runs and reports all 4 checks
- [ ] `bd cook` automatically calls preflight
- [ ] Skill drift is caught before workflow execution
- [ ] Skills auto-sync on demand with `--auto-sync`
- [ ] Session start includes skill audit
- [ ] `bd close --compact-skills` removes unused skills
- [ ] All modes work: `--check-only`, `--auto-sync`, `--json`
- [ ] Documentation complete in AGENTS.md

---

## Example Output: Successful Preflight

```
$ bd preflight

╔════════════════════════════════════════════════════════╗
║              SHADOWBOOK PREFLIGHT CHECK                ║
╚════════════════════════════════════════════════════════╝

SKILL SYNCHRONIZATION
├─ Claude Code:        42 skills ✅
├─ Codex CLI:          42 skills ✅
├─ OpenCode:           Not installed
└─ Status:             SYNCED ✅

SKILL VALIDATION
├─ Total skills:       42
├─ Valid frontmatter:  42 ✅
├─ Invalid:            0 ✅
└─ Status:             VALID ✅

HOST READINESS
├─ Total hosts:        15
├─ Ready:              12 ✅
├─ Blocked:            3 (awaiting human approval)
└─ Status:             PARTIALLY READY ⚠️

NARRATIVE DRIFT
├─ Specs checked:      8
├─ In sync:            8 ✅
├─ Drifted:            0 ✅
└─ Status:             NO DRIFT ✅

╔════════════════════════════════════════════════════════╗
║  STATUS: ✅ READY TO COOK                             ║
║                                                        ║
║  Next: bd cook login-workflow                         ║
╚════════════════════════════════════════════════════════╝
```

---

## Example Output: Skill Drift Detected

```
$ bd preflight

╔════════════════════════════════════════════════════════╗
║              SHADOWBOOK PREFLIGHT CHECK                ║
╚════════════════════════════════════════════════════════╝

SKILL SYNCHRONIZATION
├─ Claude Code:        42 skills ✅
├─ Codex CLI:          40 skills ❌ DRIFT
├─ OpenCode:           Not installed
└─ Status:             DRIFT DETECTED ❌

Missing in Codex:
├─ skill-validation-gates
└─ defense-in-depth

╔════════════════════════════════════════════════════════╗
║  STATUS: ❌ PREFLIGHT FAILED                          ║
║                                                        ║
║  Fix: bd preflight --auto-sync                        ║
║  Or:  /skill-sync sync                                ║
╚════════════════════════════════════════════════════════╝
```

---

## Related Specs

- [SKILL_SYNC_MARKETPLACE_PR_SPEC.md](SKILL_SYNC_MARKETPLACE_PR_SPEC.md) — Contributing skill-sync to superpowers marketplace
- [SKILL_SYNC_OSS_PROPOSAL.md](SKILL_SYNC_OSS_PROPOSAL.md) — Original proposal

---

**Document Version:** 1.0  
**Status:** Ready for implementation  
**Last Updated:** 2026-01-30

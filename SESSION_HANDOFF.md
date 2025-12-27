# Session Handoff: CI Failure Analysis & Prevention

**Date**: 2025-12-27  
**Branch**: `fix/ci-failure-analysis` (pushed to origin)  
**Status**: Investigation complete → Ready for sub-agent execution  
**Next Actions**: Delegate 3 parallel work items to sub-agents  

---

## What Was Accomplished This Session

### 1. Root Cause Analysis ✓
Investigated why steveyegge/beads:main is failing CI on every push.

**Finding**: All 10 recent pushes (2025-12-27 08:06:57Z → 09:11:26Z) failed CI.

**Failures**:
- 5 golangci-lint errors blocking builds (errcheck: 2, gosec: 2, unparam: 1)
- 1 Nix flake build failure (bd help command fails in Nix sandbox)

**Root Cause**: Agents are committing code without pre-push verification
- No local linting before push
- No pre-commit hooks configured
- No pre-push quality gates

### 2. Analysis Documentation ✓
Created comprehensive analysis:
- `history/CI_FAILURE_ANALYSIS.md` - Detailed root cause, patterns, prevention strategy
- `WORK_DELEGATION.md` - Clear sub-agent assignments with verification steps
- This document - Handoff summary

### 3. Issue Tracking ✓
Created 4 bd issues to track work:

| Issue | Title | Priority | Status | Purpose |
|-------|-------|----------|--------|---------|
| bd-ala4 | CI Investigation: Main branch failures | P1 | open | Parent issue tracking investigation |
| bd-cbg6 | Fix lint errors: errcheck, gosec, unparam | P0 | in_progress | Fix 5 lint violations |
| bd-40cb | Fix Nix flake build failure | P0 | in_progress | Debug & fix Nix sandbox issue |
| bd-v1h6 | Add pre-push verification in agent instructions | P1 | in_progress | Update AGENTS.md + CLAUDE.md |

### 4. Feature Branch Established ✓
- Created branch: `fix/ci-failure-analysis`
- Committed analysis and delegation documents
- Pushed to origin (ready for PR)

---

## What Needs to Be Done (Sub-Agent Work)

### Agent 1: Fix Lint Errors (bd-cbg6)
**Priority**: P0 (Critical - blocks all releases)  
**Time**: ~30 minutes  
**Status**: Ready to start

**5 Errors to Fix**:
1. `cmd/bd/create.go:489` - Add error check to `targetStore.Close()`
2. `cmd/bd/wisp.go:86` - Add error check to `cmd.Help()`
3. `internal/storage/sqlite/migrations/028_tombstone_closed_at.go:111` - Check `rows.Close()`
4. `internal/storage/sqlite/migrations/028_tombstone_closed_at.go:119` - Check `rows.Close()`
5. `cmd/bd/doctor/sqlite_open.go:10` - Remove unused `readOnly` parameter

**Verification**:
```bash
golangci-lint run      # Should report 0 errors
go test ./...          # All tests pass
```

**When Done**: Run `bd close bd-cbg6 --reason "5 lint errors fixed"`

---

### Agent 2: Fix Nix Build (bd-40cb)
**Priority**: P0 (Critical - blocks all releases)  
**Time**: ~1 hour  
**Status**: Ready to start

**Investigation**:
1. Review `flake.nix` for recent breaking changes
2. Check Go version pins (compatible with current environment?)
3. Investigate environment assumptions in Nix sandbox
4. Debug why `bd help` fails in Nix develop shell

**Testing**:
```bash
nix flake check
nix develop
bd help  # Should work in Nix environment
```

**When Done**: Run `bd close bd-40cb --reason "Nix build verified working"`

---

### Agent 3: Update Agent Instructions (bd-v1h6)
**Priority**: P1 (High - prevents future failures)  
**Time**: ~1 hour  
**Status**: Ready to start (can proceed in parallel with Agents 1 & 2)

**Files to Update**:

1. **AGENTS.md** - Add section after "Agent Instructions":
```markdown
## Pre-Push Quality Gates

Before pushing ANY branch, verify:
- golangci-lint run → 0 errors required
- go test ./... → all must pass
- nix flake check → must succeed

NEVER push if linting, tests, or build checks fail.
```

2. **CLAUDE.md** - Add before "Landing the Plane":
```markdown
## Quality Gates (Pre-Push Verification)

Before calling "Landing the Plane" workflow:
1. Run: golangci-lint run (0 errors)
2. Run: go test ./... (all pass)
3. Run: nix flake check (must succeed)

These gates prevent broken code from reaching main.
```

3. **scripts/preflight.sh** (new file):
```bash
#!/bin/bash
# Quick pre-push verification script
set -e
echo "🔍 Running pre-push verification..."
golangci-lint run && echo "✓ Lint passed"
go test ./... && echo "✓ Tests passed"
nix flake check && echo "✓ Nix build passed"
echo "✅ All pre-push checks passed - safe to push!"
```

**When Done**: Run `bd close bd-v1h6 --reason "Agent instructions updated with pre-push verification"`

---

## Execution Plan

### Timing
```
T+0:    Delegate to 3 agents (work items ready - see WORK_DELEGATION.md)
T+30m:  Agent 1 should complete lint fixes
T+1h:   Agent 2 should complete Nix debugging
T+1.5h: Agent 3 updates instructions (now that fixes are verified)
T+2h:   All tasks complete, ready to merge
```

### Workflow
1. Each agent works on their assigned issue (bd-cbg6, bd-40cb, or bd-v1h6)
2. Changes go on branch: `fix/ci-failure-analysis` (already created and pushed)
3. After all 3 agents complete:
   - Pull request: merge `fix/ci-failure-analysis` → `main`
   - Verify all CI jobs pass
   - Merge to main

### Branch Management
```bash
# Agents should use the existing feature branch:
git checkout fix/ci-failure-analysis
git pull origin fix/ci-failure-analysis

# Make their fixes/updates
# Commit with clear messages
# Push to origin fix/ci-failure-analysis
# Once all agents done: Create PR for review & merge
```

---

## Success Criteria

The work is complete when:

✅ **All 5 lint errors fixed**
- golangci-lint run reports 0 errors
- Go tests all pass
- Verified in CI

✅ **Nix flake build working**
- nix flake check passes locally
- bd help works in nix develop shell
- Test Nix Flake CI job passes

✅ **Agent instructions updated**
- AGENTS.md has "Pre-Push Quality Gates" section
- CLAUDE.md has "Quality Gates" before landing
- scripts/preflight.sh exists and is executable
- bd onboard has been run to publish

✅ **CI passes on feature branch**
- All CI jobs green on fix/ci-failure-analysis
- No failures, no warnings

✅ **Main branch restored**
- Feature branch merged to main
- CI passes on main
- No broken commits going forward

---

## Prevention for the Future

Once this work is complete, the following mitigations are in place:

### Short-term (Next 2 hours)
✓ All CI-blocking errors fixed
✓ Agent instructions updated with pre-push checks
✓ Agents trained on new workflow

### Medium-term (This week - future work)
- [ ] Add .pre-commit-config.yaml with golangci-lint hook
- [ ] Create .husky/pre-push hooks for automated verification
- [ ] Document in CONTRIBUTING.md

### Long-term (This month - future work)
- [ ] Implement `bd preflight` command for readiness checks
- [ ] Add GitHub branch protection (require CI before merge)
- [ ] Create contributor onboarding with CI verification steps

---

## Resources

**Analysis Documents**:
- `history/CI_FAILURE_ANALYSIS.md` - Detailed root cause analysis
- `WORK_DELEGATION.md` - Sub-agent assignments with verification steps
- `SESSION_HANDOFF.md` - This file

**Related Issues**:
- bd-ala4 - Parent issue (investigation tracking)
- bd-cbg6 - Lint error fixes
- bd-40cb - Nix build fixes
- bd-v1h6 - Agent instruction updates

**Current Branch**:
- `fix/ci-failure-analysis` (pushed to origin, ready for work)

**GitHub Links**:
- Upstream repo: https://github.com/steveyegge/beads
- CI logs: `gh run view <RUN_ID> --repo steveyegge/beads`
- Recent failures visible in GitHub Actions

---

## Questions & Troubleshooting

**Q: What if an agent gets stuck?**
- Review the specific error messages in WORK_DELEGATION.md
- Check GitHub Actions logs for CI failures
- Add comments to the bd issue with blockers

**Q: Should we merge if only 2/3 agents finish?**
- NO - All 3 issues should be resolved before merging
- The fixes are interdependent (lint → verified, Nix → verified, instructions → documented)

**Q: What if we need to abort?**
- Simply close the feature branch and issues
- Go back to main (this branch doesn't affect it)
- Can pick up the work later if needed

**Q: How do we verify the fixes work?**
- Each task has explicit verification steps in WORK_DELEGATION.md
- CI must pass before merge (automated check)
- Follow the "When Done" instructions for each issue

---

## Summary for Next Session Owner

You are taking over work that requires:
1. **3 parallel work items** - Each agent takes one task
2. **Clear scope** - Each task has specific files to fix
3. **Verification steps** - Each task knows how to verify success
4. **Time-boxed** - Expected 2 hours total to complete

The hard part (root cause analysis) is already done. This session is focused on **execution** by delegated agents.

**Key files to reference**:
- WORK_DELEGATION.md (primary reference for agents)
- history/CI_FAILURE_ANALYSIS.md (context/understanding)

**Command to check progress**:
```bash
bd list --id bd-cbg6,bd-40cb,bd-v1h6 --long
```

---

**Ready to hand off! 🎯**

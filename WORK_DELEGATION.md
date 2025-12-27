# Work Delegation: CI Failure Analysis & Prevention

**Branch**: `fix/ci-failure-analysis`
**Parent Issue**: bd-ala4
**Status**: Investigation Complete → Ready for sub-agent delegation

## Summary

The steveyegge/beads:main branch is failing CI on every push. Root cause: **agents commit code without pre-commit linting and build verification**.

- **10 consecutive main pushes failed**: 2025-12-27 08:06:57Z → 09:11:26Z
- **5 lint errors blocking**: errcheck, gosec, unparam violations
- **1 Nix build failure**: `bd help via Nix` fails in flake environment

---

## Immediate Work Items (3 Sub-Agents)

### AGENT 1: Fix Lint Errors (bd-cbg6) - Priority P0

**Assigned to**: Sub-agent (lint fixer)
**Time estimate**: 30 minutes
**Status**: `in_progress`

#### Errors to Fix

1. **cmd/bd/create.go:489** - Missing `targetStore.Close()` error check
   ```go
   defer targetStore.Close()  // Add: _ = or check error
   ```

2. **cmd/bd/wisp.go:86** - Missing `cmd.Help()` error check
   ```go
   cmd.Help()  // Add error handling or _ =
   ```

3. **internal/storage/sqlite/migrations/028_tombstone_closed_at.go:111** - gosec G104
   ```go
   rows.Close()  // Add error check
   ```

4. **internal/storage/sqlite/migrations/028_tombstone_closed_at.go:119** - gosec G104
   ```go
   rows.Close()  // Add error check
   ```

5. **cmd/bd/doctor/sqlite_open.go:10** - unparam: unused `readOnly` parameter
   ```go
   func sqliteConnString(path string, readOnly bool) string {
       // Either remove readOnly or use it
   ```

#### Verification

```bash
cd /var/home/matt/dev/beads/main
git checkout fix/ci-failure-analysis
golangci-lint run
# Should report 0 issues
go test ./...
# Should all pass
```

#### When Complete

- Run: `bd update bd-cbg6 --status in_progress`
- After fixes: `bd close bd-cbg6 --reason "All 5 lint errors fixed"`

---

### AGENT 2: Fix Nix Flake Build (bd-40cb) - Priority P0

**Assigned to**: Sub-agent (Nix debugger)
**Time estimate**: 1 hour
**Status**: `in_progress`

#### Failure Details

```
Test Nix Flake job failed at step 4: "Run bd help via Nix"
Error output: [Check GitHub Actions logs for full output]
```

#### Investigation Steps

1. Review `flake.nix` configuration
2. Check if recent code changes broke Nix environment assumptions
3. Look for:
   - Missing build dependencies
   - Incorrect Go version pins
   - Environment variable assumptions
   - Path issues in Nix sandbox

#### Testing Locally

```bash
cd /var/home/matt/dev/beads/main
nix flake check
nix develop
bd help  # Should succeed in Nix environment
```

#### When Complete

- Update issue with root cause and fix
- Verify: `nix flake check` passes
- Run: `bd close bd-40cb --reason "Nix build verified"`

---

### AGENT 3: Update Agent Instructions (bd-v1h6) - Priority P1

**Assigned to**: Sub-agent (documentation)
**Time estimate**: 1 hour
**Status**: `in_progress`

#### Scope

Add "Pre-Push Verification" section to prevent future CI failures.

#### Files to Update

1. **AGENTS.md** - Add new section:
```markdown
## Pre-Push Quality Gates

Before pushing to any branch, agents MUST verify:

1. Local lint check: `golangci-lint run`
2. All tests pass: `go test ./...`
3. Nix build works: `nix flake check` (if applicable)
4. Coverage threshold maintained: `go test -cover ./...`

NEVER push if:
- golangci-lint reports errors
- Tests are failing
- Build checks don't pass
```

2. **CLAUDE.md** - Add "Quality Gates" section before "Landing the Plane":
```markdown
## Quality Gates (Pre-Push)

When you've completed code changes and tests are passing locally:

1. Run lint: `golangci-lint run` (0 errors required)
2. Run tests: `go test ./...` (all must pass)
3. Check Nix: `nix flake check` (must succeed)
4. Only then proceed to "Landing the Plane" → push
```

3. Create **scripts/preflight.sh**:
```bash
#!/bin/bash
# Quick pre-push verification
set -e
echo "Running pre-push checks..."
golangci-lint run
go test ./...
nix flake check && echo "✓ All checks passed - safe to push"
```

#### Verification

- [ ] AGENTS.md has clear pre-push section
- [ ] CLAUDE.md has quality gates before landing
- [ ] scripts/preflight.sh exists and is executable
- [ ] Run `bd onboard` to publish instructions

#### When Complete

- Run: `bd update bd-v1h6 --status in_progress`
- After changes: `bd close bd-v1h6 --reason "Pre-push verification added to agent instructions"`

---

## Coordination

1. **Agent 1 & 2 can run in parallel** (lint + Nix are independent)
2. **Agent 3 waits for 1 & 2 to pass** (update instructions after fixes verified)
3. **All on branch**: `fix/ci-failure-analysis`
4. **Final PR**: Merge to main once all CI jobs pass

---

## Progress Tracking

```bash
# Check status
bd list --filter 'bd-cbg6 OR bd-40cb OR bd-v1h6'

# Update status while working
bd update bd-cbg6 --status in_progress

# Mark complete
bd close bd-cbg6 --reason "5 lint errors fixed, CI passing"
```

---

## Exit Criteria

✅ All 5 lint errors fixed and verified with golangci-lint  
✅ Nix flake build passes locally and in CI  
✅ AGENTS.md/CLAUDE.md updated with pre-push verification  
✅ CI passes on `fix/ci-failure-analysis` branch  
✅ All issues (bd-cbg6, bd-40cb, bd-v1h6) closed  
✅ Feature branch merged to main  

---

## Timeline

- **T+0** (now): Agents start work on parallel tasks
- **T+30min**: Agent 1 (lint) should have fixes in PR
- **T+1h**: Agent 2 (Nix) should have root cause + fix
- **T+1.5h**: Agent 3 updates instructions with verified fixes
- **T+2h**: All tasks complete, merge to main

---

## Resources

- Analysis document: `history/CI_FAILURE_ANALYSIS.md`
- Main branch CI logs: `gh run view <RUN_ID> --repo steveyegge/beads`
- Related issues: bd-ala4 (parent), bd-cbg6, bd-40cb, bd-v1h6
- Current branch: `fix/ci-failure-analysis`


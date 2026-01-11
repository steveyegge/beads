# Agent Instructions

See [CLAUDE.md](CLAUDE.md) for full instructions.

This file exists for compatibility with tools that look for AGENTS.md.

## Key Sections in CLAUDE.md

- **Issue Tracking** - How to use bd for work management
- **Development Guidelines** - Code standards and testing
- **Visual Design System** - Status icons, colors, and semantic styling for CLI output

## Visual Design Anti-Patterns

**NEVER use emoji-style icons** (🔴🟠🟡🔵⚪) in CLI output. They cause cognitive overload.

**ALWAYS use small Unicode symbols** with semantic colors:
- Status: `○ ◐ ● ✓ ❄`
- Priority: `● P0` (filled circle with color)

See CLAUDE.md "Visual Design System" section for full guidance.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Nix Package Maintenance

The `default.nix` file contains two fields that maintainers need to be aware of:

1. **version** - Automatically updated by `bump-version.sh`
2. **vendorHash** - Must be updated when Go dependencies change

### When to Update vendorHash

Update the vendorHash field in `default.nix` when:
- You update `go.mod` dependencies
- You run `go get` or `go mod tidy` with new packages
- Nix build fails with "hash mismatch" error

### How to Update vendorHash

**Automated Method (Recommended):**
```bash
./scripts/update-nix-vendorhash.sh
```

This script automatically:
1. Sets a temporary bad hash to trigger an error
2. Runs `nix build` to get the actual hash
3. Extracts the correct hash from the error message
4. Updates `default.nix` with the correct hash
5. Verifies the update with a clean build

**Requirements:** Either Nix installed locally OR Docker (will use `nixos/nix` image automatically)

**Manual Method (if script fails):**
```bash
# 1. Try building with Nix (will fail with correct hash if dependencies changed)
nix build

# 2. Copy the "got: sha256-..." hash from the error message
# 3. Update vendorHash in default.nix with the new hash
# 4. Re-run nix build to verify it works
```

**Alternative: Use nix-update (requires installation):**
```bash
# Install nix-update first: nix-env -iA nixpkgs.nix-update
nix-update beads --build
```

**Note:** Version bumps alone don't require vendorHash updates unless dependencies changed. The `bump-version.sh` script automatically updates the version field, but vendorHash must be updated when Go modules are modified.

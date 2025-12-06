# ADR-005: Documentation Placement and Nori Instructions

**Status:** Accepted  
**Date:** 2025-12-06  
**Deciders:** Matt (human user)

## Problem

Nori (the documentation agent) was placing generated documentation in the wrong directory:
- **Wrong placement:** `cmd/bd/docs.md` (code directory)
- **Correct placement:** `docs/GIT_INTEGRATION.md` (documentation directory)

This violated the project's established documentation structure where:
1. **`docs/` directory** contains all user and developer-facing documentation
2. **Code directories** (`cmd/`, `internal/`) contain only code and code-level comments
3. **`docs/adr/` directory** contains architectural decision records
4. **`docs/dev-notes/` directory** contains ephemeral development notes

## Context

The beads project has a dedicated documentation tree:
- User-facing guides: `docs/QUICKSTART.md`, `docs/INSTALLING.md`, etc.
- Technical documentation: `docs/GIT_INTEGRATION.md`, `docs/ARCHITECTURE.md`, `docs/INTERNALS.md`
- Developer guides: `docs/EXTENDING.md`, `docs/ADVANCED.md`
- Architectural decisions: `docs/adr/*.md`

When Nori generated documentation about the `cmd/bd` package structure and hook implementation, it should have:
1. Found the existing documentation tree (`docs/`)
2. Located the most relevant document (`docs/GIT_INTEGRATION.md` - hooks are a git integration feature)
3. Merged its content with existing sections rather than creating a new file
4. Never created documentation files in code directories

## Decision

**Nori will be instructed to:**

1. **Always check for existing documentation** in `docs/` before creating new files
2. **Prefer merging with existing documents** over creating new ones
3. **Respect project structure:**
   - Package-level implementation details go in relevant `docs/*.md` files
   - Never put documentation in code directories (`cmd/`, `internal/`, `lib/`, etc.)
4. **Use content placement heuristics:**
   - git-related code → `docs/GIT_INTEGRATION.md`
   - Hook implementation → `docs/GIT_INTEGRATION.md`
   - Database/storage → `docs/INTERNALS.md` or `docs/ARCHITECTURE.md`
   - Testing patterns → `docs/TESTING.md`
   - Package architecture → `docs/ARCHITECTURE.md`

## Implementation

### For Nori Instructions

Add to Nori change documenter prompt:

```
When documenting codebase changes, follow this placement strategy:

1. **Identify the topic** of the changes (e.g., git hooks, testing, database)
2. **Search docs/ directory** for relevant files using:
   - Glob patterns: docs/*.md
   - Grep for keywords in existing docs
3. **Prefer merging** over creating new files. Examples:
   - Hook changes → merge into docs/GIT_INTEGRATION.md
   - Storage changes → merge into docs/INTERNALS.md
   - Test patterns → merge into docs/TESTING.md
4. **Never create documentation in:**
   - Code directories (cmd/, internal/, lib/)
   - Package directories
5. **Structure new content** under the most relevant section heading in the target document
```

### For This Project

The hook implementation documentation was merged into `docs/GIT_INTEGRATION.md` under a new "Implementation Details" subsection that includes:
- Hook installation details (`installHooks()` function)
- Git directory resolution (`getGitDir()` helper)
- Hook detection logic (`detectExistingHooks()` function)
- Hook testing approach (worktree-aware test patterns)

## Benefits

✅ Single source of truth for each topic (no scattered docs)  
✅ Easy to maintain - one place to update hook docs  
✅ Cleaner code directories - no generated docs mixed in  
✅ Better discovery - users find all git integration info in one place  
✅ Easier for Nori to avoid duplication and merge conflicts

## See Also

- `docs/GIT_INTEGRATION.md` - Hook implementation section
- `.nori.md` - Nori instructions file (if created in future)
- Project documentation structure in `docs/README.md` (if it exists)

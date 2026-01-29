# Semantic Issue IDs

Semantic IDs provide human-readable aliases for beads, making it easier to reference and discuss issues in conversation, documentation, and CLI commands.

## Overview

Instead of cryptic random IDs like `gt-zfyl8`, you can use meaningful slugs like `gt-epc-semantic_idszfyl8`. The semantic slug embeds:

- **Prefix**: Your rig identifier (e.g., `gt`, `bd`, `hq`)
- **Type code**: Issue type abbreviation (e.g., `epc` for epic, `bug` for bug)
- **Title slug**: Slugified version of the issue title
- **Random**: The canonical ID's random component (ensuring uniqueness)

## Format

```
<prefix>-<type>-<title><random>[.<child>]
```

### Examples

| Canonical ID | Title | Semantic Slug |
|--------------|-------|---------------|
| `gt-zfyl8` | Semantic Issue IDs | `gt-epc-semantic_idszfyl8` |
| `gt-3q6a9` | Fix login timeout | `gt-bug-fix_login_timeout3q6a9` |
| `bd-x7m2` | Add validation | `bd-tsk-add_validationx7m2` |
| `gt-zfyl8.1` | Format specification | `gt-epc-semantic_idszfyl8.format_spec` |

### Type Codes

| Issue Type | Code | Example |
|------------|------|---------|
| epic | `epc` | `gt-epc-auth_redesignzfyl8` |
| bug | `bug` | `gt-bug-fix_login3q6a9` |
| task | `tsk` | `bd-tsk-add_validationx7m2` |
| feature | `feat` | `gt-feat-dark_mode9k4p` |
| chore | `chr` | `gt-chr-cleanup_temp2n5q` |
| merge-request | `mr` | `bd-mr-feature_branch5s2m` |
| molecule | `mol` | `gt-mol-deacon_patrol4j6v` |
| wisp | `wsp` | `gt-wsp-check_inbox1t8y` |
| agent | `agt` | `hq-agt-gastown_witness` |
| role | `rol` | `hq-rol-polecat` |
| convoy | `cvy` | `gt-cvy-fix_auth8r3w` |
| event | `evt` | `gt-evt-build_complete1a2b` |
| message | `msg` | `hq-msg-weekly_report3c4d` |

## Slug Generation Rules

### Title Slugification

1. Convert to lowercase
2. Replace non-alphanumeric characters with underscores
3. Collapse consecutive underscores
4. Trim leading/trailing underscores
5. Prefix with `n` if starting with a digit
6. Truncate to 40 characters at word boundary
7. Minimum 3 characters (pad with `x` if needed)

### Stop Words

Common words are removed to keep slugs concise:
- Articles: `a`, `an`, `the`
- Prepositions: `in`, `on`, `at`, `to`, `for`, `of`, `with`, `by`, `from`, `as`
- Conjunctions: `and`, `or`, `but`, `nor`
- Common verbs: `is`, `are`, `was`, `were`, `be`, `been`, `being`, etc.

### Priority Prefixes

Priority indicators are stripped from titles:
- `URGENT`, `CRITICAL`, `P0`, `P1`, `P2`, `P3`, `P4`, `BLOCKER`, `HOTFIX`

### Examples

| Title | Generated Slug |
|-------|----------------|
| "Fix login timeout" | `fix_login_timeout` |
| "The API returns an error" | `api_returns_error` |
| "URGENT: Fix the bug" | `fix_bug` |
| "P0 Database crash" | `database_crash` |
| "123 fix" | `n123_fix` |

## Child Issues (Hierarchy)

Child issues append their title slug after the parent's random component:

```
gt-epc-semantic_idszfyl8                    # Parent epic
gt-epc-semantic_idszfyl8.format_spec        # Child task
gt-epc-semantic_idszfyl8.validation         # Another child
gt-epc-semantic_idszfyl8.format_spec.regex  # Grandchild
```

The random component (`zfyl8`) anchors the entire tree. Children use dot-separated title-derived names, not numbers.

## CLI Usage

### Viewing Issues

Both canonical and semantic IDs work interchangeably:

```bash
# These are equivalent
bd show gt-zfyl8
bd show gt-epc-semantic_idszfyl8

# Show with slug display
bd show gt-zfyl8 --with-slug

# List issues with slugs
bd list --slugs
```

### Creating Issues

When you create an issue, a semantic slug is automatically generated:

```bash
bd create -t bug "Fix login timeout"
# Created: gt-bug-fix_login_timeout3q6a9 (slug) / gt-3q6a9 (canonical)

bd create -t epic "Auth Redesign"
# Created: gt-epc-auth_redesignx7m2 (slug) / gt-x7m2 (canonical)
```

## Migration

### Generating Slugs for Existing Issues

Use the migration command to generate semantic slugs for existing beads:

```bash
# Preview changes (recommended first step)
bd migrate semantic-ids --dry-run

# Generate slugs interactively (confirm each)
bd migrate semantic-ids --interactive

# Generate all slugs automatically
bd migrate semantic-ids

# Filter by issue type
bd migrate semantic-ids --filter=epic
bd migrate semantic-ids --filter=bug
```

### Migration Options

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview changes without modifying database |
| `--interactive` | Confirm each slug before applying |
| `--filter=TYPE` | Only process issues of specific type |
| `--json` | Output in JSON format |

### Backup

The migration command automatically creates a backup before making changes:
```
.beads/beads.backup-semantic-20260129-153043.db
```

A mapping file is also saved for reference:
```
.beads/semantic-slug-mapping.json
```

## Validation

### Valid Slugs

```
gt-epc-semantic_idszfyl8
gt-bug-fix_login_timeout3q6a9
gt-tsk-add_user_authx7m2
gt-epc-semantic_idszfyl8.format_spec
gt-epc-semantic_idszfyl8.format_spec.regex
```

### Invalid Slugs

```
semantic_idszfyl8                    # Missing prefix and type
gt-semantic_idszfyl8                 # Missing type
epc-semantic_idszfyl8                # Missing prefix
gt-epc-semantic_ids                  # Missing random
gt-epc-semantic_idszfyl8.1           # Numeric child (use names)
gt-epc-Fix_Loginzfyl8                # Uppercase
gt-epc-fix-loginzfyl8                # Hyphen in title
```

### Validation Regex

```regex
^[a-z]{2,3}-[a-z]{2,4}-[a-z][a-z0-9_]{2,39}[a-z0-9]{4,6}(\.[a-z][a-z0-9_]{2,39})*$
```

Components:
- `[a-z]{2,3}` - Prefix (2-3 lowercase letters)
- `[a-z]{2,4}` - Type code (2-4 lowercase letters)
- `[a-z][a-z0-9_]{2,39}` - Title slug (3-40 chars, starts with letter)
- `[a-z0-9]{4,6}` - Random component (4-6 alphanumeric)
- `(\.[a-z][a-z0-9_]{2,39})*` - Optional child segments

## Backward Compatibility

- **Canonical IDs remain the source of truth** - Random IDs like `gt-zfyl8` are never changed
- **Slugs are aliases** - They provide an alternative way to reference issues
- **All existing IDs continue to work** - No breaking changes
- **Both formats accepted everywhere** - CLI, API, and references in descriptions

## Best Practices

1. **Use descriptive titles** - They become your slug, so make them meaningful
2. **Keep titles concise** - Long titles get truncated at 40 characters
3. **Avoid special characters** - They're replaced with underscores
4. **Use consistent naming** - Similar issues should have similar title patterns
5. **Don't include type in title** - The type code handles this (e.g., "Fix bug" becomes `gt-bug-fix_bug...` which is redundant)

## API

### Programmatic Access

```go
import "github.com/steveyegge/beads/internal/idgen"

gen := idgen.NewSemanticIDGenerator()

// Generate slug with random from canonical ID
slug := gen.GenerateSlugWithRandom("gt", "epic", "Semantic Issue IDs", "zfyl8")
// Returns: "gt-epc-semantic_idszfyl8"

// Generate child slug
childSlug := gen.GenerateChildSlug("gt-epc-semantic_idszfyl8", "Format specification")
// Returns: "gt-epc-semantic_idszfyl8.format_spec"

// Extract random from canonical ID
random := idgen.ExtractRandomFromID("gt-zfyl8")
// Returns: "zfyl8"
```

### Validation

```go
import "github.com/steveyegge/beads/internal/validation"

// Check if ID is semantic format
if validation.IsSemanticID("gt-epc-semantic_idszfyl8") {
    // It's a semantic ID
}

// Parse semantic ID components
result, err := validation.ParseSemanticID("gt-epc-semantic_idszfyl8")
// result.Prefix = "gt"
// result.TypeAbbrev = "epc"
// result.Slug = "semantic_ids"
// result.Random = "zfyl8"
// result.FullType = "epic"
```

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 0.5 | 2026-01-29 | Final format: embed canonical random in slug |
| 0.4 | 2026-01-29 | Named children with dot separator |
| 0.3 | 2026-01-29 | Pivot: Slug as alias, not replacement |
| 0.2 | 2026-01-29 | Added type code and random suffix |
| 0.1 | 2026-01-29 | Initial draft: single-hyphen replacement format |

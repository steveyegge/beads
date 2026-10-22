---
title: Formulas
description: Writing declarative TOML or JSON workflow templates with steps, variables, dependencies, gates, and aspects, then cooking them into protos.
---

Formulas are declarative workflow templates.

## Formula Format

Formulas can be written in TOML (preferred) or JSON:

### TOML Format

```toml
formula = "feature-workflow"
description = "Standard feature development workflow"
version = 1
type = "workflow"

[vars.feature_name]
description = "Name of the feature"
required = true

[[steps]]
id = "design"
title = "Design {{feature_name}}"
type = "human"
description = "Create design document"

[[steps]]
id = "implement"
title = "Implement {{feature_name}}"
needs = ["design"]

[[steps]]
id = "review"
title = "Code review"
needs = ["implement"]
type = "human"

[[steps]]
id = "merge"
title = "Merge to main"
needs = ["review"]
```

### JSON Format

```json
{
  "formula": "feature-workflow",
  "description": "Standard feature development workflow",
  "version": 1,
  "type": "workflow",
  "vars": {
    "feature_name": {
      "description": "Name of the feature",
      "required": true
    }
  },
  "steps": [
    {
      "id": "design",
      "title": "Design {{feature_name}}",
      "type": "human"
    },
    {
      "id": "implement",
      "title": "Implement {{feature_name}}",
      "needs": ["design"]
    }
  ]
}
```

## Formula Types

| Type | Description |
|------|-------------|
| `workflow` | Standard step sequence |
| `expansion` | Template for expansion operator |
| `aspect` | Cross-cutting concerns |

## Variables

Define variables with defaults and constraints:

```toml
[vars.version]
description = "Release version"
required = true
pattern = "^\\d+\\.\\d+\\.\\d+$"

[vars.environment]
description = "Target environment"
default = "staging"
enum = ["staging", "production"]
```

Use variables in steps:

```toml
[[steps]]
title = "Deploy {{version}} to {{environment}}"
```

## Step Types

A step's `type` sets the issue type of the bead it creates: `task`
(default) plus the built-in issue types — `bug`, `feature`, `epic`,
`chore`, `gate`. A declared type is honored; a value that is neither
built-in nor registered with `bd types register` falls back to `task`
at pour time with a warning.

Async waits are declared with a `[steps.gate]` block — see
[Gates](/workflows/gates) — and there are two ways to combine one with
`type`:

- A `[steps.gate]` on a step of any other type is an **entry gate**: bd
  creates a sibling `gate` bead that blocks the step until the gate
  resolves. Use it for pre-delays (a timer soak or a `gh:run` CI wait
  before the step's work).
- A `[steps.gate]` on a step declared `type = "gate"` is a compound
  **exit gate**: the step bead becomes the prep task for the gate —
  dispatchable like any other task — the companion `gate` bead is
  unblocked only after that task closes, and steps that reference this
  step (`depends_on`, `needs`, `waits_for`) wait on the gate resolving,
  not on the task closing. Use it for prepare-then-approve shapes.

A step declared `type = "gate"` with **no** gate block cooks to a single
`gate`-typed bead with no companion: a wait-only bead hidden from
`bd ready`, for tooling to resolve with `bd gate resolve`.

## Dependencies

### Sequential

```toml
[[steps]]
id = "step1"
title = "First step"

[[steps]]
id = "step2"
title = "Second step"
needs = ["step1"]
```

### Parallel then Join

```toml
[[steps]]
id = "test-unit"
title = "Unit tests"

[[steps]]
id = "test-integration"
title = "Integration tests"

[[steps]]
id = "deploy"
title = "Deploy"
needs = ["test-unit", "test-integration"]  # Waits for both
```

## Gates

Add gates for async coordination. An entry gate holds its own step until
the gate resolves — a pre-delay:

```toml
[[steps]]
id = "deploy"
title = "Deploy build"

[steps.gate]
type = "timer"
await_id = "soak-30m"
timeout = "30m"
```

A compound exit gate (`type = "gate"` plus a `[steps.gate]` block) makes
the step the prep work for the gate: the prep task runs, then a human
approves, and only then may dependents proceed:

```toml
[[steps]]
id = "agree"
title = "Human approves"
type = "gate"
assignee = "pa-agent"
depends_on = ["prep"]

[steps.gate]
type = "human"
timeout = "24h"

[[steps]]
id = "deploy"
title = "Deploy to production"
needs = ["agree"]   # waits on the gate resolving, not on the prep task
```

The companion bead of a step's gate is also referenceable directly as
`gate-<step-id>`.

## Aspects (Cross-cutting)

Apply transformations to matching steps:

```toml
formula = "security-scan"
type = "aspect"

[[advice]]
target = "*.deploy"  # Match all deploy steps

[advice.before]
id = "security-scan-{step.id}"
title = "Security scan before {step.title}"
```

## Formula Locations

Formulas are searched in order:
1. `.beads/formulas/` (project-level)
2. `~/.beads/formulas/` (user-level)

`bd formula list` shows everything visible on the search paths.

## Using Formulas

```bash
# List available formulas
bd formula list

# Cook the formula into a proto, then pour it into a molecule
bd cook <formula-file>
bd mol pour <proto-id> --var key=value

# Preview what would be created
bd mol pour <proto-id> --dry-run
```

## Creating Custom Formulas

1. Create file: `.beads/formulas/my-workflow.formula.toml`
2. Define structure (see examples above)
3. Use with: `bd cook my-workflow` then `bd mol pour <proto-id>`

## Example: Release Formula

```toml
formula = "release"
description = "Standard release workflow"
version = 1

[vars.version]
required = true
pattern = "^\\d+\\.\\d+\\.\\d+$"

[[steps]]
id = "bump-version"
title = "Bump version to {{version}}"

[[steps]]
id = "changelog"
title = "Update CHANGELOG"
needs = ["bump-version"]

[[steps]]
id = "test"
title = "Run full test suite"
needs = ["changelog"]

[[steps]]
id = "build"
title = "Build release artifacts"
needs = ["test"]

[[steps]]
id = "tag"
title = "Create git tag v{{version}}"
needs = ["build"]

[[steps]]
id = "publish"
title = "Publish release"
needs = ["tag"]
type = "human"
```

# Beads Formula System: Deep Dive

**Status**: This documents existing beads capabilities that are **not yet fully exposed** in the public formulas.md documentation.

## Executive Summary

Beads **already has** most state machine features you want:
- ✅ Conditional branching (Condition field on steps)
- ✅ Fixed-count loops (Loop.Count)
- ✅ Range loops with expressions (Loop.Range with `1..10` or `1..2^{n}`)
- ✅ Conditional loops (Loop.Until with complex conditions)
- ✅ Nested loops (loops can contain steps that have loops)
- ✅ Parallel branches + join (existing needs/depends_on system)
- ✅ Complex conditions (field, aggregate, external)

**Missing** (future enhancements):
- ❌ Foreach loops over collections (partially implemented with OnComplete)
- ❌ Dynamic branching at runtime
- ❌ Loop variables in formulas (iterations create new step IDs but loop var not in scope)

---

## Formula Schema: Complete Structure

### Root Formula Object

```json
{
  "formula": "string",           // Unique identifier (required)
  "description": "string",       // What this does
  "version": 1,                  // Schema version
  "type": "workflow|expansion|aspect",
  "extends": ["parent-formula"], // Inheritance
  "vars": { ... },               // Variables section
  "steps": [ ... ],              // Main steps array
  "template": [ ... ],           // For expansion type
  "compose": { ... },            // Composition rules
  "advice": [ ... ],             // Aspect rules
  "pointcuts": [ ... ],          // Aspect targets
  "phase": "liquid|vapor"        // Recommended pour/wisp
}
```

### Variables Section (VarDef)

```json
{
  "description": "string",       // Help text
  "default": "string",           // Default value if not provided
  "required": true|false,        // Must be provided
  "enum": ["val1", "val2"],      // Allowed values (exclusive)
  "pattern": "regex",            // Regex validation
  "type": "string|int|bool"      // Value type (default: string)
}
```

**Variable Usage**:
- Syntax: `{{variable_name}}` in titles, descriptions, assignees
- Substitution happens at cook/pour time
- Required variables must be provided with `--var key=value`
- Defaults applied before required check
- Pattern validation on provided values

---

## Steps: The Core Unit

### Basic Step Structure

```json
{
  "id": "string",                // Unique within formula (required)
  "title": "string",             // Issue title (required, supports {{var}})
  "description": "string",       // Issue description (supports {{var}})
  "type": "task|bug|feature|epic|chore|human",
  "priority": 0-4,               // Issue priority
  "labels": ["label1", "label2"],// Applied to created issue
  "depends_on": ["step1", "step2"], // Blocking dependencies
  "needs": ["step1"],            // Alias for depends_on
  "assignee": "user@example.com",// Default assignee (supports {{var}})

  // Advanced features:
  "expand": "expansion-formula",  // Inline expansion template
  "expand_vars": {"key": "val"},  // Overrides for expansion
  "condition": "string",          // Optional - see Conditions section
  "children": [ ... ],            // Nested steps (epic hierarchy)
  "gate": { ... },                // Async wait gate - see Gates section
  "loop": { ... },                // Loop definition - see Loops section
  "on_complete": { ... },         // Runtime expansion - see OnComplete section
  "waits_for": "all-children|any-children"  // Gate label type
}
```

---

## Control Flow Features

### 1. Conditional Steps

**Field**: `condition` on Step

**Syntax**:
- `"{{variable_name}}"` - Truthy check (non-empty string)
- `"!{{variable_name}}"` - Falsy check
- `"{{variable_name}} == value"` - Equality
- `"{{variable_name}} != value"` - Inequality
- `"step.status == 'complete'"` - Step status check (runtime)
- `"step.output.approved == true"` - Step output check (runtime)
- `"children(step).all(status == 'complete')"` - Aggregate conditions

**Important**: Conditions are evaluated at **cook time** for variable-based conditions (compile-time filtering), or at **runtime** for step/output conditions.

**Example**:
```json
[
  {
    "id": "design",
    "title": "Design feature"
  },
  {
    "id": "standard-impl",
    "title": "Standard implementation",
    "condition": "{{implementation_type}} == standard",
    "needs": ["design"]
  },
  {
    "id": "advanced-impl",
    "title": "Advanced implementation",
    "condition": "{{implementation_type}} == advanced",
    "needs": ["design"]
  }
]
```

When poured with `--var implementation_type=standard`, only `design` and `standard-impl` steps are created.

---

### 2. Loops

**Field**: `loop` on Step (LoopSpec)

Loops create multiple iterations of a step's body. Three types:

#### A. Fixed-Count Loop

```json
{
  "id": "batch-process",
  "title": "Batch processor",
  "loop": {
    "count": 3,
    "body": [
      {"id": "fetch", "title": "Fetch batch item"},
      {"id": "process", "title": "Process", "needs": ["fetch"]}
    ]
  }
}
```

Creates:
```
batch-process.iter1.fetch
batch-process.iter1.process
batch-process.iter2.fetch
batch-process.iter2.process
batch-process.iter3.fetch
batch-process.iter3.process
```

With chaining: iter1.process → iter2.fetch (iterations sequence)

#### B. Range Loop (Variable-Based)

```json
{
  "id": "deploy-to-servers",
  "title": "Deploy to server cluster",
  "loop": {
    "range": "1..{{num_servers}}",
    "body": [
      {"id": "setup", "title": "Setup server {i}"},
      {"id": "start", "title": "Start server {i}", "needs": ["setup"]}
    ]
  }
}
```

With `--var num_servers=5`, creates 5 iterations (server 1-5).

**Range Syntax**:
- `"1..10"` - Simple range
- `"1..{{num_shards}}"` - Variable substitution
- `"1..2^{{exponent}}"` - Expressions (+ - * / ^ supported)
- `"{{min}}..{{max}}"` - Both bounds variables
- `"1..10^2"` - Pre-evaluated (100)

#### C. Conditional Loop (Until)

```json
{
  "id": "retry-logic",
  "title": "Retry with backoff",
  "loop": {
    "until": "step.output.success == true",
    "max": 5,
    "body": [
      {"id": "attempt", "title": "Attempt connection"}
    ]
  }
}
```

Expands body once initially; gate checks condition at runtime. If false, can retry (manual or with backoff logic).

#### D. Nested Loops

```json
{
  "id": "matrix-test",
  "loop": {
    "count": 3,
    "body": [
      {
        "id": "test-suite",
        "loop": {
          "count": 2,
          "body": [
            {"id": "test", "title": "Run test"}
          ]
        }
      }
    ]
  }
}
```

Creates: outer_iter1.inner_iter1, outer_iter1.inner_iter2, outer_iter2.inner_iter1, ...

---

### 3. Dependencies (DAG Construction)

**Fields**: `needs`, `depends_on` (equivalent)

Creates execution DAG. Multiple formats:

#### Sequential
```json
[
  {"id": "step1", "title": "First"},
  {"id": "step2", "title": "Second", "needs": ["step1"]},
  {"id": "step3", "title": "Third", "needs": ["step2"]}
]
```

#### Parallel + Join
```json
[
  {"id": "test-unit", "title": "Unit tests"},
  {"id": "test-int", "title": "Integration tests"},
  {"id": "deploy", "title": "Deploy", "needs": ["test-unit", "test-int"]}
]
```

#### Complex DAG
```json
[
  {"id": "a", ...},
  {"id": "b", "needs": ["a"]},
  {"id": "c", "needs": ["a"]},
  {"id": "d", "needs": ["b", "c"]},
  {"id": "e", "needs": ["d"]}
]
```

---

### 4. Gates (Async Coordination)

**Field**: `gate` on Step (Gate object)

Gates block step execution until external condition satisfied.

#### Gate Types

```json
{
  "gate": {
    "type": "human",
    "approvers": ["manager@company.com", "lead@company.com"]
  }
}
```

**Supported Gate Types**:
- `"human"` - Manual approval (list of approvers)
- `"gh:run"` - GitHub Actions workflow completion
- `"gh:pr"` - Pull request approval
- `"timer"` - Time-based delay
- `"mail"` - Email trigger

#### Example: Multi-Gate Workflow

```json
[
  {"id": "code", "title": "Write code"},
  {
    "id": "approval",
    "title": "Manager approval",
    "needs": ["code"],
    "gate": {
      "type": "human",
      "approvers": ["manager@company.com"]
    }
  },
  {
    "id": "ci",
    "title": "Run CI pipeline",
    "needs": ["approval"],
    "gate": {
      "type": "gh:run",
      "workflow": "CI"
    }
  },
  {"id": "deploy", "title": "Deploy", "needs": ["ci"]}
]
```

---

### 5. Inline Expansions

**Fields**: `expand`, `expand_vars` on Step

Inline another formula's template into a step location.

#### Example

**expansion-cicd.json**:
```json
{
  "formula": "expansion-cicd",
  "type": "expansion",
  "vars": {
    "target": {
      "description": "Target step to apply to",
      "required": true
    }
  },
  "template": [
    {"id": "{target}.lint", "title": "Lint {target.title}"},
    {"id": "{target}.test", "title": "Test {target.title}", "needs": ["{target}.lint"]},
    {"id": "{target}.build", "title": "Build {target.title}", "needs": ["{target}.test"]}
  ]
}
```

**Using it**:
```json
{
  "id": "deploy-backend",
  "title": "Deploy backend",
  "expand": "expansion-cicd",
  "expand_vars": {"target": "backend"}
}
```

Expands to:
```
deploy-backend.lint
deploy-backend.test (needs lint)
deploy-backend.build (needs test)
```

---

### 6. Aspects (Cross-Cutting Concerns)

**Field**: `advice` array (AspectRule), type="aspect"

Aspects insert steps before/after/around matching targets.

**Example**: Add security scan to all deploy steps

```json
{
  "formula": "aspect-security",
  "type": "aspect",
  "advice": [
    {
      "target": "*.deploy",
      "before": {
        "id": "security-scan-{step.id}",
        "title": "Security scan before {step.title}"
      }
    }
  ]
}
```

Targets all steps matching `*.deploy` pattern. Before each, inserts security-scan step.

---

### 7. Conditions (Complex Evaluation)

The condition system is more powerful than the simple variable checks. Three categories:

#### A. Field Conditions (Runtime)

Check step state:
- `"step.status == 'complete'"` - Status values: pending, in_progress, complete, failed
- `"step.output.approved == true"` - Access step output fields
- `"step.output.tests_passed > 0"` - Numeric comparisons
- `"{{var}} == value"` - Variable (cooked at cook-time)

#### B. Aggregate Conditions

```
"children(step).all(status == 'complete')"    // All children done
"children(step).any(status == 'failed')"      // Any child failed
"descendants(step).count > 5"                 // Count descendants
```

#### C. External Conditions

```
"file.exists('go.mod')"                       // File existence check
"env.CI == 'true'"                            // Environment variable
"env.BRANCH == 'main'"                        // Conditional based on environment
```

---

## Example: State Machine Workflow

Here's a release workflow showing state machine-like behavior:

```json
{
  "formula": "release-workflow",
  "description": "Production release with conditional approval",
  "version": 1,
  "type": "workflow",
  "phase": "vapor",

  "vars": {
    "version": {
      "description": "Release version (e.g., 1.2.3)",
      "required": true,
      "pattern": "^\\d+\\.\\d+\\.\\d+$"
    },
    "release_type": {
      "description": "Type of release",
      "required": true,
      "enum": ["patch", "minor", "major"]
    },
    "num_regions": {
      "description": "Number of regions to deploy to",
      "default": "3"
    }
  },

  "steps": [
    {
      "id": "bump",
      "title": "Bump version to {{version}}"
    },
    {
      "id": "changelog",
      "title": "Update CHANGELOG.md",
      "needs": ["bump"]
    },
    {
      "id": "test",
      "title": "Run test suite",
      "needs": ["changelog"],
      "gate": {
        "type": "gh:run",
        "workflow": "test"
      }
    },
    {
      "id": "security-check",
      "title": "Security audit",
      "condition": "{{release_type}} == major",
      "needs": ["test"]
    },
    {
      "id": "approval",
      "title": "Release approval",
      "type": "human",
      "condition": "{{release_type}} == major",
      "needs": ["security-check"]
    },
    {
      "id": "fast-approve",
      "title": "Automatic approval for patch",
      "condition": "{{release_type}} == patch",
      "needs": ["test"]
    },
    {
      "id": "build",
      "title": "Build release artifacts",
      "needs": ["approval", "fast-approve"]  // Whichever completes
    },
    {
      "id": "deploy-region",
      "title": "Deploy to region",
      "loop": {
        "range": "1..{{num_regions}}",
        "body": [
          {
            "id": "deploy",
            "title": "Deploy region {i}",
            "needs": ["build"]
          }
        ]
      }
    },
    {
      "id": "tag",
      "title": "Create git tag v{{version}}",
      "needs": ["deploy-region"]
    }
  ]
}
```

**Workflow**:
1. Bump version → changelog
2. changelog → test (waits for CI)
3. If major: test → security-check → approval; else test → fast-approve
4. approval/fast-approve → build
5. build → deploy to N regions (loop 1..num_regions)
6. deploy-region → tag

---

## Implementation Files

Key source files in beads:

| File | Purpose |
|------|---------|
| `internal/formula/types.go` | Schema definitions (Formula, Step, LoopSpec, Gate, etc.) |
| `internal/formula/parser.go` | Parse TOML/JSON into Formula objects |
| `internal/formula/controlflow.go` | Loop/branch/gate expansion logic |
| `internal/formula/condition.go` | Condition evaluation (field, aggregate, external) |
| `internal/formula/range.go` | Range expression evaluation (1..10, 1..2^{n}) |
| `internal/formula/expand.go` | Formula expansion into proto issues |
| `internal/formula/advice.go` | Aspect application logic |
| `internal/formula/stepcondition.go` | Step-level condition filtering |
| `cmd/bd/formula.go` | CLI commands (bd formula list/show) |
| `cmd/bd/mol.go` | Molecule instantiation (pour/wisp) |

---

## Cooking Process

Formulas go through a multi-stage cooking process:

```
1. Parse TOML/JSON
   ↓
2. Apply variable substitution
   ↓
3. Filter steps by conditions
   ↓
4. Apply inline expansions (expand field)
   ↓
5. Apply aspects
   ↓
6. Expand loops (controlflow)
   ↓
7. Create proto issues in database
   ↓
8. Create molecules.jsonl entry
```

Each stage transforms the step graph before the next.

---

## Current Limitations

Based on codebase analysis:

### 1. Loop Variables Not In Scope

Loop iteration variables (`{i}` in range loops) can be used in step titles/descriptions but are **not available as formula variables**.

```json
{
  "loop": {
    "range": "1..3",
    "body": [
      {"id": "step-{i}", "title": "Step {i}"}  // {i} substituted OK
    ]
  },
  "steps": [
    {"title": "Later step {{loop_var}}"}  // {{loop_var}} NOT available
  ]
}
```

### 2. OnComplete Not Yet Fully Integrated

The `on_complete` field exists on Step but full integration with agent output processing is incomplete.

### 3. Dynamic Runtime Branching Limited

Conditions are primarily evaluated at cook-time (for variables) or as gates (for step state). True dynamic branching based on runtime output is not fully supported.

### 4. No Loop Variables with Foreach

Collections/arrays cannot be iterated in formulas. Range must be numeric.

---

## Testing Formulas

### Cook (Validate)
```bash
bd cook my-workflow.formula.json
```

Validates syntax and shows what would be created (without creating it).

### Dry-Run Pour
```bash
bd pour my-workflow --var key=value --dry-run
```

Preview exact steps that will be created.

### Wisp (Test Execution)
```bash
bd mol wisp my-workflow --var key=value
```

Create ephemeral workflow for testing. Auto-cleanup after 24h or manual `bd mol burn <id>`.

---

## Next Steps for State Machine Extension

Given what exists in beads:

1. **Document Existing Features**
   - Loop syntax with examples
   - Condition syntax and evaluation model
   - Complex DAG patterns

2. **Create Reference Formulas**
   - State machine template (conditional branches + gates)
   - Loop patterns (fixed, range, conditional)
   - Complex coordination (parallel + aggregate conditions)

3. **Identify Gaps**
   - Runtime dynamic branching (if not needed, don't build)
   - Foreach over collections (if critical, build)
   - Loop variable scope (lower priority)

4. **Consider Preprocessor**
   - Only if major gaps identified
   - Otherwise: document existing + contribute upstream

---

## References

- **Source**: `/Users/randlee/Documents/github/beads/internal/formula/`
- **Website Docs**: `/Users/randlee/Documents/github/beads/website/docs/workflows/`
- **Tests**: `*_test.go` files show all supported syntax
- **Architecture**: `./beads/molecules-architecture.md` (Section 7)

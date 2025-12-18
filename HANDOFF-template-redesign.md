# Handoff: Template System Redesign (bd-r6a)

## Status

- **bd-r6a.1**: DONE - Reverted YAML workflow code, deleted workflow.go and types
- **bd-r6a.2**: IN PROGRESS - Implementing subgraph cloning with variable substitution

## What Was Removed

- `cmd/bd/workflow.go` - entire file
- `cmd/bd/templates/workflows/` - YAML templates directory
- `internal/types/workflow.go` - WorkflowTemplate types

Build passes. Tests pass.

## The New Design

### Core Principle

**Templates are just Beads.** An epic with the `template` label and `{{variable}}` placeholders in titles/descriptions.

Beads provides **primitives**:
- Clone a subgraph (epic + children + dependencies)
- Substitute `{{variables}}`
- Return ID mapping (old → new)

Orchestrators (Gas Town) provide **composition**:
- Multiple instantiations
- Cross-template dependencies
- Dynamic task generation

### Commands

```bash
bd template list                              # List templates (label=template)
bd template instantiate <id> --var key=value  # Clone + substitute
bd template instantiate <id> --dry-run        # Preview
```

### Gas Town Use Case: Witness

The Witness manages polecat lifecycles with a dynamic DAG:

```
Witness Round (1 instance)
├── Check context
├── Initialize tracking
├── [polecat tasks wired in by Gas Town]
├── Submit to merge queue
└── Finalize

Polecat Lifecycle (N instances, one per polecat)
├── Verify startup
├── Monitor progress
├── Verify shutdown
└── Decommission
```

Gas Town instantiates templates in a loop and wires dependencies between them.

## Implementation Started

Was creating `cmd/bd/template.go` with:

- `templateCmd` - parent command
- `templateListCmd` - list templates (issues with template label)
- `templateInstantiateCmd` - clone subgraph with substitution
- `TemplateSubgraph` struct - holds issues + dependencies
- `loadTemplateSubgraph()` - recursive load of epic + descendants
- `cloneSubgraph()` - create new issues with ID remapping
- `extractVariables()` - find `{{name}}` patterns
- `substituteVariables()` - replace patterns with values

File was not written yet (got interrupted).

## Key Functions Needed

```go
// Load template and all descendants
func loadTemplateSubgraph(templateID string) (*TemplateSubgraph, error)

// Clone with substitution, return new epic ID and ID mapping
func cloneSubgraph(subgraph *TemplateSubgraph, vars map[string]string) (string, map[string]string, error)

// Extract {{variable}} patterns
func extractVariables(text string) []string

// Replace {{variable}} with values
func substituteVariables(text string, vars map[string]string) string
```

## Remaining Tasks

1. **bd-r6a.2**: Implement subgraph cloning (in progress)
2. **bd-r6a.3**: Create version-bump as native Beads template
3. **bd-r6a.4**: Add `bd template list` command
4. **bd-r6a.5**: Update documentation

## HOP Context

Templates feed into HOP vision:
- Work is fractal (templates are reusable work patterns)
- Beads IS the ledger (templates are ledger entries)
- Gas Town is execution engine (composes templates into swarms)

See `~/gt/hop/CONTEXT.md` for full HOP context.

## To Resume

```bash
cd /Users/stevey/src/dave/beads
bd show bd-r6a          # See epic and tasks
bd ready                # bd-r6a.2 should be ready
bd update bd-r6a.2 --status=in_progress
# Create cmd/bd/template.go with the design above
```

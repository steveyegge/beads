---
id: routing
title: Routing
sidebar_position: 2
---

# Multi-Rig Routing

Prefix-based issue routing across multiple beads databases.

## Overview

Routing enables:
- Issues with specific prefixes routed to different beads directories
- Multi-rig setups where different projects share a common workspace (Gas Town)
- Transparent cross-database operations using issue prefixes

## Configuration

Create `.beads/routes.jsonl` at the town root:

```jsonl
{"prefix": "gt-", "path": "gastown/mayor/rig"}
{"prefix": "bd-", "path": "beads/mayor/rig"}
{"prefix": "ol-", "path": "."}
```

## Route Fields

| Field | Description |
|-------|-------------|
| `prefix` | Issue ID prefix including hyphen (e.g., `"gt-"`) |
| `path` | Relative path from town root to the rig directory, or `"."` for town-level beads |

## How Routing Works

When you reference an issue ID like `gt-abc123`:

1. The prefix `gt-` is extracted from the ID
2. Routes are loaded from `.beads/routes.jsonl`
3. If a route matches the prefix, the operation is routed to that rig's beads directory
4. If no route matches, the local beads directory is used

**Example routing:**
```
ID: gt-abc123
Prefix: gt-
Route: {"prefix": "gt-", "path": "gastown/mayor/rig"}
Target: ~/town/gastown/mayor/rig/.beads/
```

## Multi-Rig Setup (Gas Town)

A Gas Town is a workspace containing multiple rigs that share a common routing configuration:

```
~/gt/                         # Town root (contains mayor/town.json)
├── .beads/
│   └── routes.jsonl          # Routing configuration
├── mayor/
│   └── town.json             # Marks this as a town root
├── gastown/
│   └── mayor/
│       └── rig/
│           └── .beads/       # Gastown rig beads (prefix: gt-)
└── beads/
    └── mayor/
        └── rig/
            └── .beads/       # Beads rig beads (prefix: bd-)
```

**Example routes.jsonl for this setup:**
```jsonl
{"prefix": "gt-", "path": "gastown/mayor/rig"}
{"prefix": "bd-", "path": "beads/mayor/rig"}
```

### Using the --rig Flag

When creating issues, you can specify a target rig:

```bash
# Create an issue in the gastown rig (will get gt- prefix)
bd create "Fix frontend bug" --rig gastown

# Create an issue in the beads rig (will get bd- prefix)
bd create "Update documentation" --rig beads
```

The `--rig` flag accepts:
- Rig names: `gastown`, `beads`
- Prefixes with hyphen: `gt-`, `bd-`
- Prefixes without hyphen: `gt`, `bd`

### Auto-Routing

If you're working in a rig with a configured prefix, issue creation automatically routes to the correct beads directory based on the rig's prefix. No `--rig` flag is needed.

## Symlinked .beads Directories

Routes work correctly with symlinked `.beads` directories. If your town-level `.beads` is a symlink:

```bash
~/gt/.beads -> ~/gt/olympus/.beads
```

Routing will still resolve paths correctly relative to the town root (`~/gt/`), not the symlink target.

## Redirect Files

A beads directory can contain a `redirect` file that points to another location:

```
# Contents of .beads/redirect
../other-location/.beads
```

This is useful for consolidating beads storage or handling special directory structures.

## Troubleshooting

### Enable Debug Logging

Set `BD_DEBUG_ROUTING=1` to see detailed routing decisions:

```bash
BD_DEBUG_ROUTING=1 bd create "test" --dry-run
```

This outputs:
- Which routes.jsonl file is being loaded
- Number of routes parsed and any skipped lines
- Prefix matching decisions
- Target path resolution

**Example output:**
```
[routing] LoadRoutes: loading from /home/user/gt/.beads/routes.jsonl
[routing] LoadRoutes: parsed 3 valid routes, skipped 0 lines
[routing] AutoDetectTargetRig called: beadsDir=/home/user/gt/gastown/mayor/rig/.beads, prefix=gt-
[routing] Found 3 routes, townRoot=/home/user/gt
```

### Common Issues

**Routes not loading:**
- Ensure `routes.jsonl` exists in the town-level `.beads/` directory
- Check that each line is valid JSON
- Verify both `prefix` and `path` fields are present and non-empty

**Prefix not matching:**
- Prefixes must include the hyphen (e.g., `"gt-"` not `"gt"`)
- Check for typos in the prefix field

**Target directory not found:**
- Verify the path is relative to the town root
- Ensure the target `.beads/` directory exists
- Use `"."` for town-level beads, not `"./"`

## Manual Configuration

Routes are configured by manually editing `.beads/routes.jsonl`. There are no CLI commands for route management.

**Adding a route:**
1. Open `.beads/routes.jsonl` in your editor
2. Add a new line with the route JSON
3. Save the file

**Example:**
```jsonl
{"prefix": "gt-", "path": "gastown/mayor/rig"}
{"prefix": "bd-", "path": "beads/mayor/rig"}
{"prefix": "new-", "path": "newproject/mayor/rig"}
```

**Note:** Lines starting with `#` are treated as comments and blank lines are ignored.

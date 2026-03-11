---
id: agent
title: bd agent
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc agent` (bd version 0.59.0)

## bd agent

Manage state on agent beads for ZFC-compliant state reporting.

Agent beads (labeled gt:agent) can self-report their state using these commands.
This enables the Witness and other monitoring systems to track agent health.

States:
  idle      - Agent is waiting for work
  spawning  - Agent is starting up
  running   - Agent is executing (general)
  working   - Agent is actively working on a task
  stuck     - Agent is blocked and needs help
  done      - Agent completed its current work
  stopped   - Agent has cleanly shut down
  dead      - Agent died without clean shutdown (set by Witness via timeout)

Examples:
  bd agent state gt-emma running     # Set emma's state to running
  bd agent heartbeat gt-emma         # Update emma's last_activity timestamp
  bd agent show gt-emma              # Show emma's agent details

```
bd agent
```

### bd agent backfill-labels

Backfill role_type and rig labels on existing agent beads.

This command scans all agent beads and:
1. Extracts role_type and rig from description text if fields are empty
2. Sets the role_type and rig fields on the agent bead
3. Adds role_type:<value> and rig:<value> labels for filtering

This enables queries like:
  bd list --type=agent --label=role_type:witness
  bd list --type=agent --label=rig:gastown

Use --dry-run to see what would be changed without making changes.

Examples:
  bd agent backfill-labels           # Backfill all agent beads
  bd agent backfill-labels --dry-run # Preview changes without applying

```
bd agent backfill-labels [flags]
```

**Flags:**

```
      --dry-run   Preview changes without applying them
```

### bd agent heartbeat

Update the last_activity timestamp of an agent bead without changing state.

Use this for periodic heartbeats to indicate the agent is still alive.
The Witness can use this to detect dead agents via timeout.

Examples:
  bd agent heartbeat gt-emma   # Update emma's last_activity
  bd agent heartbeat gt-mayor  # Update mayor's last_activity

```
bd agent heartbeat <agent>
```

### bd agent show

Show detailed information about an agent bead.

Displays agent-specific fields including state, last_activity, hook, and role.

Examples:
  bd agent show gt-emma   # Show emma's agent details
  bd agent show gt-mayor  # Show mayor's agent details

```
bd agent show <agent>
```

### bd agent state

Set the state of an agent bead.

This updates both the agent_state field and the last_activity timestamp.
Use this for ZFC-compliant state reporting.

Valid states: idle, spawning, running, working, stuck, done, stopped, dead

Examples:
  bd agent state gt-emma running   # Set state to running
  bd agent state gt-mayor idle     # Set state to idle

```
bd agent state <agent> <state>
```


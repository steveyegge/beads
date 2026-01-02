---
id: daemon
title: Daemon Architecture
sidebar_position: 3
---

# Daemon Architecture

Beads runs a background daemon for auto-sync and performance.

## Overview

Each workspace gets its own daemon process:
- Auto-starts on first `bd` command
- Handles database ↔ JSONL synchronization
- Listens on `.beads/bd.sock` (Unix) or `.beads/bd.pipe` (Windows)
- Version checking prevents mismatches after upgrades

## How It Works

```
CLI Command
    ↓
RPC to Daemon
    ↓
Daemon executes
    ↓
Auto-sync to JSONL (5s debounce)
```

Without daemon, commands access the database directly (slower, no auto-sync).

## Managing Daemons

```bash
# List all running daemons
bd daemons list
bd daemons list --json

# Check health and version mismatches
bd daemons health
bd daemons health --json

# View daemon logs
bd daemons logs . -n 100

# Restart all daemons
bd daemons killall
bd daemons killall --json
```

## Daemon Info

```bash
bd info
```

Shows:
- Daemon status (running/stopped)
- Daemon version vs CLI version
- Socket location
- Auto-sync status

## Disabling Daemon

Use `--no-daemon` flag to bypass the daemon:

```bash
bd --no-daemon ready
bd --no-daemon list
```

**When to disable:**
- Git worktrees (required)
- CI/CD pipelines
- Resource-constrained environments
- Debugging sync issues

## Event-Driven Mode (Experimental)

Event-driven mode replaces 5-second polling with instant reactivity:

```bash
# Enable globally
export BEADS_DAEMON_MODE=events
bd daemons killall  # Restart to apply
```

**Benefits:**
- Less than 500ms latency (vs 5s polling)
- ~60% less CPU usage
- Instant sync after changes

**How to verify:**
```bash
bd info | grep "daemon mode"
```

## Troubleshooting

### Daemon not starting

```bash
# Check if socket exists
ls -la .beads/bd.sock

# Try direct mode
bd --no-daemon info

# Restart daemon
bd daemons killall
bd info
```

### Version mismatch

After upgrading bd:

```bash
bd daemons killall
bd info  # Should show matching versions
```

### Sync not happening

```bash
# Force sync
bd sync

# Check daemon logs
bd daemons logs . -n 50

# Verify git status
git status .beads/
```

### Port/socket conflicts

```bash
# Kill all daemons
bd daemons killall

# Remove stale socket
rm -f .beads/bd.sock

# Restart
bd info
```

## Configuration

Daemon behavior can be configured:

```bash
# Set sync debounce interval
bd config set daemon.sync_interval 10s

# Disable auto-start
bd config set daemon.auto_start false

# Set log level
bd config set daemon.log_level debug
```

See [Configuration](/reference/configuration) for all options.

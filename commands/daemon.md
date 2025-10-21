---
description: Run background sync daemon
argument-hint: [--global] [--stop] [--status] [--health]
---

Run a background daemon that manages database connections and optionally syncs with git.

## Daemon Modes

- **Local daemon**: Socket at `.beads/bd.sock` (per-repository)
- **Global daemon**: Socket at `~/.beads/bd.sock` (all repositories)

> On Windows these files store the daemon’s loopback TCP endpoint metadata—leave them in place so bd can reconnect.

## Common Operations

- **Start**: `bd daemon` or `bd daemon --global`
- **Stop**: `bd daemon --stop` or `bd daemon --global --stop`
- **Status**: `bd daemon --status` or `bd daemon --global --status`
- **Health**: `bd daemon --health` - shows uptime, cache stats, performance metrics
- **Metrics**: `bd daemon --metrics` - detailed operational telemetry

## Sync Options

- **--auto-commit**: Automatically commit JSONL changes
- **--auto-push**: Automatically push commits to remote
- **--interval**: Sync check interval (default: 5m)

## Migration

- **--migrate-to-global**: Migrate from local to global daemon

The daemon provides:
- Connection pooling and caching
- Better performance for frequent operations
- Automatic JSONL sync
- Optional git sync

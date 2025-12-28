---
id: troubleshooting
title: Troubleshooting
sidebar_position: 4
---

# Troubleshooting

Common issues and solutions.

## Installation Issues

### `bd: command not found`

```bash
# Check if installed
which bd
go list -f {{.Target}} github.com/steveyegge/beads/cmd/bd

# Add Go bin to PATH
export PATH="$PATH:$(go env GOPATH)/bin"

# Or reinstall
go install github.com/steveyegge/beads/cmd/bd@latest
```

### `zsh: killed bd` on macOS

CGO/SQLite compatibility issue:

```bash
CGO_ENABLED=1 go install github.com/steveyegge/beads/cmd/bd@latest
```

### Permission denied

```bash
chmod +x $(which bd)
```

## Database Issues

### Database not found

```bash
# Initialize beads
bd init --quiet

# Or specify database
bd --db .beads/beads.db list
```

### Database locked

```bash
# Stop daemon
bd daemons killall

# Try again
bd list
```

### Corrupted database

```bash
# Restore from JSONL
rm .beads/beads.db
bd import -i .beads/issues.jsonl
```

## Daemon Issues

### Daemon not starting

```bash
# Check status
bd info

# Remove stale socket
rm -f .beads/bd.sock

# Restart
bd daemons killall
bd info
```

### Version mismatch

After upgrading bd:

```bash
bd daemons killall
bd info
```

### High CPU usage

```bash
# Switch to event-driven mode
export BEADS_DAEMON_MODE=events
bd daemons killall
```

## Sync Issues

### Changes not syncing

```bash
# Force sync
bd sync

# Check daemon
bd info | grep daemon

# Check hooks
bd hooks status
```

### Import errors

```bash
# Allow orphans
bd import -i .beads/issues.jsonl --orphan-handling allow

# Check for duplicates after
bd duplicates
```

### Merge conflicts

```bash
# Use merge driver
bd init  # Setup merge driver

# Or manual resolution
git checkout --ours .beads/issues.jsonl
bd import -i .beads/issues.jsonl
bd sync
```

## Git Hook Issues

### Hooks not running

```bash
# Check if installed
ls -la .git/hooks/

# Reinstall
bd hooks install
```

### Hook errors

```bash
# Check hook script
cat .git/hooks/pre-commit

# Run manually
.git/hooks/pre-commit
```

## Dependency Issues

### Circular dependencies

```bash
# Detect cycles
bd dep cycles

# Remove one dependency
bd dep remove bd-A bd-B
```

### Missing dependencies

```bash
# Check orphan handling
bd config get import.orphan_handling

# Allow orphans
bd config set import.orphan_handling allow
```

## Performance Issues

### Slow queries

```bash
# Check database size
ls -lh .beads/beads.db

# Compact if large
bd admin compact --analyze
```

### High memory usage

```bash
# Reduce cache
bd config set database.cache_size 1000
```

## Getting Help

### Debug output

```bash
bd --verbose list
```

### Logs

```bash
bd daemons logs . -n 100
```

### System info

```bash
bd info --json
```

### File an issue

```bash
# Include this info
bd version
bd info --json
uname -a
```

Report at: https://github.com/steveyegge/beads/issues

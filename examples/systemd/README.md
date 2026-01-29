# bd Daemon Systemd Integration

This directory contains prototype systemd user service files for running the bd daemon as a managed service.

## Files

- `bd-daemon@.service` - Template systemd user service unit
- `install-bd-daemon.sh` - Installation script
- `uninstall-bd-daemon.sh` - Removal script

## Quick Start

```bash
# Install for current workspace
./install-bd-daemon.sh

# Or for a specific workspace
./install-bd-daemon.sh /path/to/workspace
```

## What This Does

1. **Installs the service template** to `~/.config/systemd/user/bd-daemon@.service`
2. **Enables lingering** so the daemon runs after logout (`loginctl enable-linger`)
3. **Creates a service instance** for your workspace
4. **Starts the daemon** under systemd management

## Benefits

- **Automatic restart** on failure (with backoff)
- **Persistent operation** - survives logout/login cycles
- **Centralized logging** via journald
- **Clean shutdown** on system reboot
- **Standard management** via `systemctl --user` commands

## Manual Installation

If you prefer to install manually:

```bash
# 1. Copy service file
mkdir -p ~/.config/systemd/user
cp bd-daemon@.service ~/.config/systemd/user/

# 2. Enable lingering (optional, for persistent operation)
loginctl enable-linger

# 3. Reload systemd
systemctl --user daemon-reload

# 4. Generate unit name for your workspace
UNIT_NAME=$(systemd-escape --path /path/to/workspace)

# 5. Enable and start
systemctl --user enable "bd-daemon@${UNIT_NAME}.service"
systemctl --user start "bd-daemon@${UNIT_NAME}.service"
```

## Management Commands

```bash
# Check status
systemctl --user status bd-daemon@*.service

# View logs
journalctl --user -u bd-daemon@*.service

# Follow logs in real-time
journalctl --user -u bd-daemon@*.service -f

# Restart (e.g., after bd upgrade)
systemctl --user restart bd-daemon@*.service

# Stop
systemctl --user stop bd-daemon@*.service

# List all bd daemon instances
systemctl --user list-units 'bd-daemon@*'
```

## Notes

- Each workspace gets its own service instance
- The workspace path is encoded in the service name using `systemd-escape --path`
- Services run in your user context (not as root)
- Socket path is at `${XDG_RUNTIME_DIR}/bd/bd.sock`

## Troubleshooting

### Service won't start

Check logs:
```bash
journalctl --user -u bd-daemon@*.service -n 50
```

### "No upstream configured" error

If the daemon fails with upstream error, add this to your `.beads/config.yaml`:
```yaml
auto-push: false
```

### Socket permission issues

Ensure `XDG_RUNTIME_DIR` is set (usually `/run/user/<uid>`). This is automatic on login but may need to be set manually in cron jobs.

## Future Work

- Socket activation (on-demand startup)
- Watchdog integration
- Integration with `bd init --systemd`

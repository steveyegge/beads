# Systemctl-Managed bd Daemon Service Design

**Issue:** bd-0lh64.1 (EPIC)
**Author:** obsidian (polecat)
**Date:** 2026-01-29
**Status:** Research Complete - Recommended Approach Identified

---

## Executive Summary

This document proposes using **systemd user services** to manage the `bd daemon` lifecycle, replacing the current ad-hoc process management. The recommended approach uses standard service units (not socket activation) with `loginctl enable-linger` for persistent background operation.

**Key recommendation:** Start with simple Type=simple user service, add socket activation in Phase 2 only if on-demand startup proves valuable.

---

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Research Findings](#research-findings)
3. [Recommended Approach](#recommended-approach)
4. [Service Unit Design](#service-unit-design)
5. [Database Path Configuration](#database-path-configuration)
6. [Integration with bd init](#integration-with-bd-init)
7. [Error Handling and Restart Policies](#error-handling-and-restart-policies)
8. [Socket Activation Analysis](#socket-activation-analysis)
9. [Implementation Phases](#implementation-phases)
10. [Open Questions](#open-questions)

---

## Problem Statement

The current bd daemon has several lifecycle management challenges:

1. **Ad-hoc process management**: PID files, socket cleanup, stale lock detection
2. **Version mismatch handling**: Complex logic to detect and restart incompatible daemons
3. **Auto-start reliability**: 5-second timeout, exponential backoff, failure tracking
4. **No system integration**: Daemon doesn't survive logout, no integration with system boot
5. **Manual intervention required**: Users must `bd daemon restart` after upgrades

systemd can solve these problems with:
- Automatic restart on failure
- Clean shutdown on logout/reboot
- Proper dependency ordering
- Centralized logging (journald)
- Resource management (cgroups)

---

## Research Findings

### systemd User Services

User services run in the context of a specific user (not root) and are placed in `~/.config/systemd/user/`.

**Key characteristics:**
- Services managed with `systemctl --user` commands
- Default behavior: start on login, stop on logout
- With `loginctl enable-linger`: services persist after logout

**XDG_RUNTIME_DIR requirement:**
- Set automatically by `pam_systemd` on login
- Located at `/run/user/<UID>`
- Used for socket files, PID files, runtime state
- **Caveat:** Not set when using `su` or `sudo` without full login

### Enable-Linger for Persistent Services

For bd daemon to run after user logout (e.g., for CI/CD, server environments):

```bash
# Enable lingering for current user
loginctl enable-linger

# Or as root for specific user
sudo loginctl enable-linger username
```

**Effect:** User's systemd instance starts at boot and stays running regardless of login sessions.

**Verification:**
```bash
loginctl show-user $USER | grep Linger
# Or check file existence
ls /var/lib/systemd/linger/
```

### Socket Activation Analysis

Socket activation allows on-demand service startup. systemd listens on a socket and starts the service when a connection arrives.

**How it works:**
1. `.socket` unit defines listening parameters
2. systemd passes open socket FD to service via `LISTEN_FDS` env var
3. Service uses `socket.fromfd(3, ...)` to accept the pre-opened socket

**Benefits:**
- Zero boot-time resource usage
- Automatic startup on first connection
- Clean dependency handling

**Drawbacks for bd:**
- Startup latency on first command (~500ms-1s for Dolt)
- bd daemon already auto-starts efficiently
- Adds complexity to daemon startup code
- Must handle FD inheritance from systemd

**MariaDB socket activation use case:**
- Hosting providers with many user databases
- Each user gets dedicated instance, started on-demand
- bd has different usage pattern (continuous background sync)

### Service Dependencies and Ordering

For bd daemon with Dolt backend, relevant dependencies:

```ini
[Unit]
After=network-online.target
Wants=network-online.target
```

**Note:** `network-online.target` ensures network is up before git operations. Not strictly required for local-only mode.

---

## Recommended Approach

### Phase 1: Simple User Service (Recommended)

Use a standard `Type=simple` user service without socket activation.

**Rationale:**
1. bd daemon already handles auto-start well
2. Socket activation adds complexity without clear benefit
3. Simple service covers 95% of use cases
4. Can add socket activation later if needed

### Service Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    SYSTEMD USER SERVICE                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ~/.config/systemd/user/bd-daemon@.service (template)            │
│      │                                                           │
│      └─► bd-daemon@%i.service (instance per workspace)           │
│              │                                                   │
│              └─► bd daemon start --foreground                    │
│                       │                                          │
│                       └─► Unix socket at $XDG_RUNTIME_DIR/bd/    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Key insight:** Use systemd's template unit feature (`@.service`) to support multiple workspaces.

---

## Service Unit Design

### Template Service Unit

`~/.config/systemd/user/bd-daemon@.service`:

```ini
[Unit]
Description=bd daemon for workspace %i
After=default.target

# Don't start automatically - bd commands trigger via systemctl
RefuseManualStart=no
RefuseManualStop=no

[Service]
Type=simple
Environment=BEADS_WORKSPACE=%I
ExecStart=/usr/local/bin/bd daemon start --foreground --workspace %I
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
TimeoutStopSec=10

# Graceful shutdown
KillSignal=SIGTERM
SendSIGKILL=yes
FinalKillSignal=SIGKILL

# Socket path in XDG_RUNTIME_DIR
RuntimeDirectory=bd
RuntimeDirectoryMode=0700

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=bd-daemon-%i

[Install]
WantedBy=default.target
```

### Instance Naming Convention

Workspace paths need encoding for systemd unit names:

```bash
# Encode workspace path for unit name
# /home/user/projects/myapp → home-user-projects-myapp
systemd-escape --path /home/user/projects/myapp
# Output: home-user-projects-myapp

# Start daemon for specific workspace
systemctl --user start bd-daemon@home-user-projects-myapp.service
```

### Alternative: Single Daemon Per User

For simpler setups, a single daemon managing all workspaces:

`~/.config/systemd/user/bd-daemon.service`:

```ini
[Unit]
Description=bd daemon
After=default.target

[Service]
Type=simple
ExecStart=/usr/local/bin/bd daemon start --foreground --all-workspaces
Restart=on-failure
RestartSec=5
TimeoutStopSec=10

KillSignal=SIGTERM
SendSIGKILL=no

RuntimeDirectory=bd
RuntimeDirectoryMode=0700

StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
```

---

## Database Path Configuration

### Current Behavior

bd daemon discovers database path via:
1. `BEADS_DB` environment variable
2. `--db` flag
3. Search upward for `.beads/` directory

### systemd Integration

**Option A: Environment file per workspace**

`~/.config/systemd/user/bd-daemon@.service.d/workspace.conf`:

```ini
[Service]
EnvironmentFile=-%h/.config/bd/workspaces/%i.env
```

Where `~/.config/bd/workspaces/home-user-projects-myapp.env`:

```bash
BEADS_WORKSPACE=/home/user/projects/myapp
BEADS_DB=/home/user/projects/myapp/.beads/beads.db
```

**Option B: Instance specifier contains path**

Use `%I` (unescaped instance name) directly as workspace path:

```ini
[Service]
ExecStart=/usr/local/bin/bd daemon start --foreground
WorkingDirectory=%I
```

**Recommendation:** Option B is simpler and self-documenting.

---

## Integration with bd init

### Proposed Workflow

```bash
# bd init creates .beads/ and optionally registers systemd service
bd init --systemd

# What it does:
# 1. Creates .beads/beads.db (or dolt directory)
# 2. Generates systemd unit file (if --systemd)
# 3. Enables and starts the service
```

### bd init --systemd Implementation

```bash
# Encode workspace path
UNIT_NAME=$(systemd-escape --path "$(pwd)")

# Create user service directory
mkdir -p ~/.config/systemd/user

# Generate service file (if not using template)
cat > ~/.config/systemd/user/bd-daemon@${UNIT_NAME}.service << EOF
[Unit]
Description=bd daemon for $(pwd)
After=default.target

[Service]
Type=simple
WorkingDirectory=$(pwd)
ExecStart=$(which bd) daemon start --foreground
Restart=on-failure
RestartSec=5

KillSignal=SIGTERM
RuntimeDirectory=bd

[Install]
WantedBy=default.target
EOF

# Reload and enable
systemctl --user daemon-reload
systemctl --user enable bd-daemon@${UNIT_NAME}.service
systemctl --user start bd-daemon@${UNIT_NAME}.service
```

### bd daemon commands integration

```bash
# bd daemon status → systemctl --user status bd-daemon@...
# bd daemon start  → systemctl --user start bd-daemon@...
# bd daemon stop   → systemctl --user stop bd-daemon@...
# bd daemon logs   → journalctl --user -u bd-daemon@...
```

---

## Error Handling and Restart Policies

### Restart Configuration

```ini
[Service]
# Restart only on failure (exit code != 0)
Restart=on-failure

# Wait 5 seconds before restart
RestartSec=5

# Give up after 5 failures in 60 seconds
StartLimitIntervalSec=60
StartLimitBurst=5

# After exhausting retries, mark as failed (not auto-restart)
StartLimitAction=none
```

### Failure Notification

```ini
[Service]
# Notify systemd of failure state for monitoring
NotifyAccess=main
Type=notify  # Alternative: use sd_notify() for health checks
```

### Graceful Shutdown

```ini
[Service]
# Send SIGTERM first
KillSignal=SIGTERM

# Wait 10 seconds for graceful shutdown
TimeoutStopSec=10

# Then send SIGKILL if still running
SendSIGKILL=yes
```

### Watchdog Integration (Optional)

For enhanced reliability, enable systemd watchdog:

```ini
[Service]
Type=notify
WatchdogSec=30
```

Daemon must call `sd_notify("WATCHDOG=1")` periodically.

---

## Socket Activation Analysis

### When Socket Activation Makes Sense

| Use Case | Socket Activation? | Rationale |
|----------|-------------------|-----------|
| Single user, one workspace | No | Simple service is sufficient |
| Multiple workspaces | Maybe | On-demand per workspace |
| Hosting provider (many users) | Yes | Resource efficiency critical |
| Server with many daemons | Yes | Avoid idle resource consumption |
| Developer workstation | No | Startup latency annoying |

### Socket Activation Implementation (Phase 2)

If socket activation proves valuable:

`~/.config/systemd/user/bd-daemon@.socket`:

```ini
[Unit]
Description=bd daemon socket for %i

[Socket]
# Socket in runtime directory
ListenStream=%t/bd/%i.sock
SocketMode=0600
Accept=no  # Single service handles all connections

[Install]
WantedBy=sockets.target
```

`~/.config/systemd/user/bd-daemon@.service`:

```ini
[Unit]
Description=bd daemon for %i
Requires=bd-daemon@%i.socket
After=bd-daemon@%i.socket

[Service]
Type=simple
ExecStart=/usr/local/bin/bd daemon start --foreground --systemd-socket
StandardInput=socket

[Install]
WantedBy=default.target
```

**Daemon code changes required:**
1. Check `LISTEN_FDS` environment variable
2. Use file descriptor 3 instead of creating socket
3. Call `sd_notify("READY=1")` when ready to accept

---

## Implementation Phases

### Phase 1: Basic systemd Integration (Recommended First)

1. **Add `--systemd` flag to bd init**
   - Generate template service file
   - Enable and start service
   - Update docs

2. **Add systemd-aware daemon mode**
   - `bd daemon start --foreground` already works
   - Add `--systemd` flag for sd_notify integration (optional)
   - Log to stdout/stderr for journald

3. **Update bd daemon subcommands**
   - `bd daemon status` uses `systemctl --user status`
   - `bd daemon logs` uses `journalctl --user`
   - `bd daemon restart` uses `systemctl --user restart`

**Estimated effort:** 2-3 days

### Phase 2: Socket Activation (If Needed)

1. **Add socket unit template**
2. **Modify daemon startup to accept inherited socket**
3. **Add sd_notify support**

**Estimated effort:** 1-2 days

### Phase 3: Watchdog and Health Monitoring (Optional)

1. **Integrate sd_watchdog**
2. **Add Type=notify with ready notification**
3. **Health check endpoint for monitoring systems**

**Estimated effort:** 1 day

---

## Open Questions

1. **Template vs individual units?**
   - Template (`@.service`) is cleaner for multiple workspaces
   - Individual units allow per-workspace customization
   - **Recommendation:** Start with template

2. **Enable-linger by default?**
   - Required for daemon to persist after logout
   - Some users may not want this
   - **Recommendation:** Prompt during `bd init --systemd`

3. **Graceful migration from current daemon?**
   - Need to stop ad-hoc daemon before starting systemd version
   - **Recommendation:** `bd init --systemd` handles migration

4. **Non-systemd platforms?**
   - macOS: launchd (similar concepts)
   - Windows: Windows Service
   - **Recommendation:** Document systemd first, add others later

5. **Multi-workspace vs single daemon?**
   - Current: one daemon per workspace
   - Alternative: single daemon managing all workspaces
   - **Recommendation:** Keep current model, template units handle it

---

## References

- [systemd User Services - ArchWiki](https://wiki.archlinux.org/title/Systemd/User)
- [systemd Socket Activation - freedesktop.org](https://www.freedesktop.org/software/systemd/man/latest/systemd.socket.html)
- [MariaDB systemd Socket Activation](https://mariadb.com/docs/server/server-management/starting-and-stopping-mariadb/systemd)
- [Dolt Server systemd Configuration](https://docs.dolthub.com/introduction/installation/application-server)
- [Socket Activation Explained - ilManzo's blog](https://ilmanzo.github.io/post/systemd-socket-activated-services/)

---

## Conclusion

**Recommended approach:** Start with simple Type=simple user service using template units. This provides:

1. Clean lifecycle management via systemd
2. Automatic restart on failure
3. Proper logging via journald
4. Easy management via familiar systemctl commands
5. Path to socket activation if resource optimization becomes necessary

Socket activation is deferred to Phase 2 because bd's usage pattern (continuous background sync) differs from the on-demand model where socket activation excels.

The implementation is straightforward: add `--systemd` flag to `bd init`, generate appropriate service files, and update daemon subcommands to use systemctl.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
)

func singleProcessBackendHelp(backend string) string {
	b := strings.TrimSpace(backend)
	if b == "" {
		b = "unknown"
	}
	// Keep this short; Cobra will prefix with "Error:".
	return fmt.Sprintf("daemon mode is not supported with the %q backend (single-process only). To use daemon mode, initialize with %q (e.g. `bd init --backend sqlite`). Otherwise run commands in direct mode (default for dolt)", b, configfile.BackendSQLite)
}

// guardDaemonStartForDolt blocks daemon start/restart commands when:
// 1. The workspace backend is embedded Dolt (unless --federation is specified)
// 2. The systemd bd-daemon service is actively running (unless run via systemd)
//
// Rationale for Dolt guard: embedded Dolt is effectively single-writer at the OS-process
// level. The daemon architecture relies on multiple processes (CLI + daemon + helper spawns),
// which can trigger lock contention and transient "read-only" failures.
//
// Rationale for systemd guard: prevents multiple daemon instances when systemd is
// managing the daemon. We check if the service is actually running (via systemctl
// is-active) rather than relying on config settings that can be bypassed.
//
// Exception: --federation flag enables dolt sql-server mode which is multi-writer.
//
// Note: This guard should only be attached to commands that START a daemon process
// (start, restart). Read-only commands (status, stop, logs, health, list) are allowed
// even with Dolt backend.
//
// We still allow help output so users can discover the command surface.
func guardDaemonStartForDolt(cmd *cobra.Command, _ []string) error {
	// Allow `--help` for any daemon subcommand.
	if helpFlag := cmd.Flags().Lookup("help"); helpFlag != nil {
		if help, _ := cmd.Flags().GetBool("help"); help {
			return nil
		}
	}

	// Allow `--federation` flag which enables dolt sql-server (multi-writer) mode.
	if fedFlag := cmd.Flags().Lookup("federation"); fedFlag != nil {
		if federation, _ := cmd.Flags().GetBool("federation"); federation {
			return nil
		}
	}

	// Check if running via systemd (BD_DAEMON_SYSTEMD=1 set by systemd unit)
	isSystemdInvocation := os.Getenv("BD_DAEMON_SYSTEMD") == "1"

	// If we're already running under systemd, allow the command
	if isSystemdInvocation {
		return nil
	}

	// Check if systemd service is actively managing a daemon for this workspace.
	// This prevents agents from accidentally starting a second daemon instance
	// when systemd is already managing one.
	if isSystemdServiceActive() {
		return fmt.Errorf("daemon is managed by systemctl (bd-daemon service is active).\n\n" +
			"To restart: systemctl --user restart bd-daemon@...\n" +
			"To stop:    systemctl --user stop bd-daemon@...\n" +
			"To status:  systemctl --user status bd-daemon@...\n\n" +
			"Use 'systemctl --user list-units bd-daemon@*' to list all instances.")
	}

	// Best-effort determine the active workspace backend. If we can't determine it,
	// don't block (the command will likely fail later anyway).
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		// Fall back to configured dbPath if set; daemon commands often run from workspace root,
		// but tests may set BEADS_DB explicitly.
		if dbPath != "" {
			beadsDir = filepath.Dir(dbPath)
		} else if found := beads.FindDatabasePath(); found != "" {
			beadsDir = filepath.Dir(found)
		}
	}
	if beadsDir == "" {
		return nil
	}

	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return nil
	}

	// Use GetCapabilities() to properly handle Dolt server mode
	if cfg.GetCapabilities().SingleProcessOnly {
		return fmt.Errorf("%s", singleProcessBackendHelp(cfg.GetBackend()))
	}

	return nil
}

// isSystemdServiceActive checks if any bd-daemon systemd user service is active.
// This is used to prevent manual daemon starts when systemd is managing the daemon.
// We check for both the template instances (bd-daemon@*.service) and the simple
// service (bd-daemon.service) to cover both service styles.
func isSystemdServiceActive() bool {
	// First check for template-style service instances using list-units
	// The --state=active filter ensures we only see running instances
	cmd := exec.Command("systemctl", "--user", "--state=active", "--no-legend", "--no-pager", "list-units", "bd-daemon@*.service")
	output, err := cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		return true
	}

	// Check for simple service (bd-daemon.service)
	// Using is-active which returns 0 if the unit is active
	cmd = exec.Command("systemctl", "--user", "is-active", "--quiet", "bd-daemon.service")
	if err := cmd.Run(); err == nil {
		return true
	}

	return false
}


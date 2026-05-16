//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/daemon"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/ui"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the bdd background daemon",
	Long: `Commands for managing the bdd background storage daemon.

The daemon (bdd) runs in the background and serves storage requests to bd
processes via a Unix socket, enabling fast parallel access without
relocking the Dolt embedded database on every call.

Subcommands:
  status  Show daemon status (running, idle, RPC counts)
  kill    Stop the running daemon
  stats   Show per-method RPC call statistics`,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Run: func(cmd *cobra.Command, _ []string) {
		jsonOut, _ := cmd.Flags().GetBool("json")
		beadsDir := resolveBeadsDirForDaemon()
		showDaemonStatus(beadsDir, jsonOut)
	},
}

var daemonKillCmd = &cobra.Command{
	Use:   "kill",
	Short: "Stop the bdd daemon",
	Run: func(cmd *cobra.Command, _ []string) {
		force, _ := cmd.Flags().GetBool("force")
		beadsDir := resolveBeadsDirForDaemon()
		killDaemon(beadsDir, force)
	},
}

var daemonStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show per-method RPC statistics",
	Run: func(_ *cobra.Command, _ []string) {
		beadsDir := resolveBeadsDirForDaemon()
		showDaemonStats(beadsDir)
	},
}

func init() {
	daemonStatusCmd.Flags().Bool("json", false, "Output as JSON")
	daemonKillCmd.Flags().Bool("force", false, "Send SIGKILL immediately without waiting for SIGTERM")
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonKillCmd)
	daemonCmd.AddCommand(daemonStatsCmd)
	rootCmd.AddCommand(daemonCmd)
}

// resolveBeadsDirForDaemon returns the beads directory to use for daemon
// commands. Daemon commands don't open the store, so we resolve the path
// directly rather than using the store's locator.
func resolveBeadsDirForDaemon() string {
	if dbPath != "" {
		return dbPath
	}
	if envDB := os.Getenv("BEADS_DB"); envDB != "" {
		return envDB
	}
	return ".beads"
}

// daemonStatusJSON is the JSON schema for bd daemon status --json.
// Matches design §5.3.
type daemonStatusJSON struct {
	Status        string         `json:"status"`
	Pid           int            `json:"pid,omitempty"`
	Socket        string         `json:"socket,omitempty"`
	Version       string         `json:"version,omitempty"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	UptimeSeconds float64        `json:"uptime_seconds,omitempty"`
	RPCCallTotal  map[string]int `json:"rpc_call_total,omitempty"`
	RPCInFlight   int            `json:"rpc_in_flight,omitempty"`
}

func showDaemonStatus(beadsDir string, jsonOut bool) {
	pf, err := pidfile.Read(beadsDir, "bdd.pid")
	if err != nil {
		if jsonOut {
			printJSON(daemonStatusJSON{Status: "error"})
			return
		}
		fmt.Fprintf(os.Stderr, "daemon  %s error reading pid file: %v\n", ui.StatusBlockedStyle.Render(ui.StatusIconBlocked), err)
		os.Exit(1)
	}

	if pf == nil {
		// No pidfile — check if daemon_mode is configured.
		if jsonOut {
			printJSON(daemonStatusJSON{Status: "not_running"})
			return
		}
		fmt.Printf("daemon  %s not running  (auto — starts on next bd call)\n", ui.StatusIconOpen)
		return
	}

	// Check if the process is actually alive.
	proc, err := os.FindProcess(pf.Pid)
	alive := err == nil && proc.Signal(syscall.Signal(0)) == nil

	if !alive {
		if jsonOut {
			printJSON(daemonStatusJSON{Status: "stale"})
			return
		}
		fmt.Printf("daemon  %s stale  (pid %d exited; run 'bd daemon kill' to clean up)\n",
			ui.StatusIconOpen, pf.Pid)
		return
	}

	var uptimeSecs float64
	if pf.StartedAt != nil {
		uptimeSecs = time.Since(*pf.StartedAt).Seconds()
	}

	if jsonOut {
		j := daemonStatusJSON{
			Status:        "running",
			Pid:           pf.Pid,
			Socket:        pf.SocketPath,
			Version:       pf.Version,
			StartedAt:     pf.StartedAt,
			UptimeSeconds: uptimeSecs,
		}
		printJSON(j)
		return
	}

	fmt.Printf("daemon  %s running\n", ui.StatusInProgressStyle.Render(ui.StatusIconInProgress))
	fmt.Printf("  pid      %d\n", pf.Pid)
	if pf.SocketPath != "" {
		fmt.Printf("  socket   %s\n", pf.SocketPath)
	}
	if pf.StartedAt != nil {
		fmt.Printf("  uptime   %s\n", fmtElapsed(time.Since(*pf.StartedAt)))
	}
	if pf.Version != "" {
		fmt.Printf("  version  %s\n", pf.Version)
	}
}

func killDaemon(beadsDir string, force bool) {
	pf, err := pidfile.Read(beadsDir, "bdd.pid")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading pid file: %v\n", err)
		os.Exit(1)
	}
	if pf == nil {
		fmt.Printf("%s no daemon running\n", ui.StatusIconOpen)
		return
	}

	proc, findErr := os.FindProcess(pf.Pid)
	alive := findErr == nil && proc.Signal(syscall.Signal(0)) == nil
	if !alive {
		fmt.Printf("%s no daemon running (stale pid %d)\n", ui.StatusIconOpen, pf.Pid)
		_ = daemon.Kill(beadsDir) // clean up stale files
		return
	}

	if force {
		fmt.Printf("sending SIGKILL to pid %d...\n", pf.Pid)
		start := time.Now()
		if err := proc.Signal(syscall.SIGKILL); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		_ = daemon.Kill(beadsDir) // clean up files
		fmt.Printf("%s daemon killed in %s\n", ui.RenderPass(ui.IconPass), fmtElapsed(time.Since(start)))
		return
	}

	fmt.Printf("sending SIGTERM to pid %d...\n", pf.Pid)
	start := time.Now()
	if err := daemon.Kill(beadsDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s daemon stopped in %s\n", ui.RenderPass(ui.IconPass), fmtElapsed(time.Since(start)))
}

func showDaemonStats(beadsDir string) {
	pf, err := pidfile.Read(beadsDir, "bdd.pid")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading pid file: %v\n", err)
		os.Exit(1)
	}
	if pf == nil {
		fmt.Printf("%s no daemon running\n", ui.StatusIconOpen)
		return
	}

	proc, findErr := os.FindProcess(pf.Pid)
	alive := findErr == nil && proc.Signal(syscall.Signal(0)) == nil
	if !alive {
		fmt.Printf("%s daemon not running (stale pid %d)\n", ui.StatusIconOpen, pf.Pid)
		return
	}

	// Stats are not yet exposed via RPC; show what we know from the pidfile.
	fmt.Printf("daemon pid %d — connect to %s to query live stats\n", pf.Pid, pf.SocketPath)
	fmt.Printf("  (bd daemon stats --rpc is not yet implemented; use bd daemon status for process info)\n")
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func fmtElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

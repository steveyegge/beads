//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/rpc"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	bdRPC "github.com/steveyegge/beads/internal/storage/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

var daemonCmd = &cobra.Command{
	Use:     "daemon",
	GroupID: "maint",
	Short:   "Daemon management commands",
	Long:    `Inspect and manage the bdd background daemon (bd daemon stats).`,
}

var daemonStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show bdd daemon runtime statistics",
	Long: `Show runtime statistics for the bdd background daemon.

When the daemon is not running, the command exits with a non-zero status
and a diagnostic message.

Examples:
  bd daemon stats          # Human-readable output
  bd daemon stats --json   # JSON output for scripting
`,
	// Skip root PersistentPreRun — daemon stats needs no store.
	PersistentPreRun:  func(cmd *cobra.Command, args []string) {},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {},

	RunE: func(cmd *cobra.Command, _ []string) error {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			return fmt.Errorf("no beads workspace found")
		}

		sock := sockPath(beadsDir)
		conn, err := net.Dial("unix", sock)
		if err != nil {
			if jsonOutput {
				outputJSON(map[string]string{
					"error":   "daemon_not_running",
					"message": "bdd daemon is not running",
					"socket":  sock,
				})
				return nil
			}
			fmt.Fprintf(os.Stderr, "bdd daemon is not running (socket: %s)\n", sock)
			os.Exit(1)
		}
		defer func() { _ = conn.Close() }()

		client := rpc.NewClient(conn)
		defer func() { _ = client.Close() }()

		stats, err := bdRPC.GetStats(client)
		if err != nil {
			return fmt.Errorf("getting daemon stats: %w", err)
		}

		if jsonOutput {
			out := map[string]interface{}{
				"iter_sessions_active":      stats.IterSessionsActive,
				"iter_session_capacity":     stats.IterSessionCapacity,
				"iter_session_starts_total": stats.IterSessionStartsTotal,
				"iter_session_reaped_total": stats.IterSessionReapedTotal,
				"iter_rows_streamed_total":  stats.IterRowsStreamedTotal,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		printDaemonStats(stats)
		return nil
	},
}

func printDaemonStats(s bdRPC.DaemonStats) {
	fmt.Println("iterators")

	active := s.IterSessionsActive
	cap := int64(s.IterSessionCapacity)

	var icon string
	switch {
	case active == 0:
		icon = "○"
	case cap > 0 && active*10 >= cap*9: // >= 90%
		icon = ui.StatusBlockedStyle.Render("●")
	case cap > 0 && active*2 > cap: // > 50%
		icon = ui.StatusInProgressStyle.Render("◐")
	default:
		icon = ""
	}

	if icon != "" {
		fmt.Printf("  active    %d / %d cap     %s\n", active, cap, icon)
	} else {
		fmt.Printf("  active    %d / %d cap\n", active, cap)
	}

	fmt.Printf("  started   %d\n", s.IterSessionStartsTotal)
	fmt.Printf("  reaped    %d\n", s.IterSessionReapedTotal)
	fmt.Printf("  rows      %s\n", formatComma(s.IterRowsStreamedTotal))
}

func formatComma(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	result := make([]byte, 0, len(s)+(len(s)-1)/3)
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	if neg {
		return "-" + string(result)
	}
	return string(result)
}

func init() {
	daemonCmd.AddCommand(daemonStatsCmd)
	rootCmd.AddCommand(daemonCmd)
}

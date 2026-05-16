//go:build !unix

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the bdd background daemon",
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("daemon  %s not supported on Windows\n", ui.StatusIconOpen)
	},
}

var daemonKillCmd = &cobra.Command{
	Use:   "kill",
	Short: "Stop the bdd daemon",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Fprintln(os.Stderr, "bd daemon kill is not supported on Windows")
		os.Exit(1)
	},
}

var daemonStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show per-method RPC statistics",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Fprintln(os.Stderr, "bd daemon stats is not supported on Windows")
		os.Exit(1)
	},
}

func init() {
	daemonStatusCmd.Flags().Bool("json", false, "Output as JSON")
	daemonKillCmd.Flags().Bool("force", false, "Send SIGKILL immediately")
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonKillCmd)
	daemonCmd.AddCommand(daemonStatsCmd)
	rootCmd.AddCommand(daemonCmd)
}

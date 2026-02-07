package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/daemon"
	"github.com/steveyegge/beads/internal/rpc"
)

var busCmd = &cobra.Command{
	Use:     "bus",
	GroupID: "advanced",
	Short:   "Event bus commands",
	Long: `Event bus commands for the NATS-powered hook event system.

The event bus replaces individual shell commands in Claude Code hooks
with a single 'bd bus emit' command that dispatches events through
registered handlers.

Commands:
  bd bus emit --hook=<type>     Dispatch a hook event
  bd bus status                 Show bus health and status
  bd bus handlers               List registered handlers`,
}

var busStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show event bus status",
	Long:  `Show the current state of the event bus: NATS connectivity, handler count, JetStream health.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonClient != nil {
			resp, err := daemonClient.Execute(rpc.OpBusStatus, nil)
			if err != nil {
				return fmt.Errorf("bus status RPC failed: %w", err)
			}
			if !resp.Success {
				return fmt.Errorf("bus status: %s", resp.Error)
			}

			var result rpc.BusStatusResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				return fmt.Errorf("parse bus status: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Printf("Event Bus Status\n")
			fmt.Printf("  NATS:       %s\n", busStatusLabel(result.NATSEnabled, result.NATSStatus))
			if result.NATSEnabled {
				fmt.Printf("  Port:       %d\n", result.NATSPort)
				fmt.Printf("  Connections: %d\n", result.Connections)
				fmt.Printf("  JetStream:  %v\n", result.JetStream)
				if result.JetStream {
					fmt.Printf("  Streams:    %d\n", result.Streams)
				}
			}
			fmt.Printf("  Handlers:   %d\n", result.HandlerCount)
			return nil
		}

		// No daemon — report local-only mode.
		fmt.Println("Event Bus Status")
		fmt.Println("  NATS:       disabled (no daemon)")
		fmt.Println("  Handlers:   0 (local dispatch)")
		return nil
	},
}

var busHandlersCmd = &cobra.Command{
	Use:   "handlers",
	Short: "List registered event bus handlers",
	Long:  `Show all registered handlers with their ID, priority, and handled event types.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonClient != nil {
			resp, err := daemonClient.Execute(rpc.OpBusHandlers, nil)
			if err != nil {
				return fmt.Errorf("bus handlers RPC failed: %w", err)
			}
			if !resp.Success {
				return fmt.Errorf("bus handlers: %s", resp.Error)
			}

			var result rpc.BusHandlersResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				return fmt.Errorf("parse bus handlers: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Handlers) == 0 {
				fmt.Println("No handlers registered")
				return nil
			}

			for _, h := range result.Handlers {
				fmt.Printf("  %s (priority: %d)\n", h.ID, h.Priority)
				for _, e := range h.Handles {
					fmt.Printf("    - %s\n", e)
				}
			}
			return nil
		}

		// No daemon.
		fmt.Println("No handlers registered (no daemon)")
		return nil
	},
}

var busNATSInfoCmd = &cobra.Command{
	Use:   "nats-info",
	Short: "Show NATS connection details for sidecar processes",
	Long: `Print NATS connection info for external consumers (e.g., Coop sidecar).

Reads the nats-info.json file written by the daemon at startup.
Output includes the NATS URL, port, auth token, stream name, and subject pattern.

Use --json for machine-readable output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Try daemon RPC first for live info.
		if daemonClient != nil {
			resp, err := daemonClient.Execute(rpc.OpBusStatus, nil)
			if err == nil && resp.Success {
				var result rpc.BusStatusResult
				if err := json.Unmarshal(resp.Data, &result); err == nil && result.NATSEnabled {
					info := daemon.NATSConnectionInfo{
						URL:       fmt.Sprintf("nats://127.0.0.1:%d", result.NATSPort),
						Port:      result.NATSPort,
						JetStream: result.JetStream,
						Stream:    "HOOK_EVENTS",
						Subjects:  "hooks.>",
					}
					if jsonOutput {
						enc := json.NewEncoder(os.Stdout)
						enc.SetIndent("", "  ")
						return enc.Encode(info)
					}
					fmt.Printf("NATS Connection Info\n")
					fmt.Printf("  URL:      %s\n", info.URL)
					fmt.Printf("  Port:     %d\n", info.Port)
					fmt.Printf("  Stream:   %s\n", info.Stream)
					fmt.Printf("  Subjects: %s\n", info.Subjects)
					fmt.Printf("  Auth:     %s\n", tokenHint(info.Token))
					return nil
				}
			}
		}

		return fmt.Errorf("NATS not available (daemon not running or NATS disabled)")
	},
}

func tokenHint(token string) string {
	if token == "" {
		return "none (no auth required)"
	}
	if len(token) > 8 {
		return token[:4] + "..." + token[len(token)-4:]
	}
	return "***"
}

func busStatusLabel(enabled bool, status string) string {
	if !enabled {
		return "disabled"
	}
	if status == "" {
		return "enabled"
	}
	return status
}

func init() {
	busCmd.AddCommand(busStatusCmd)
	busCmd.AddCommand(busHandlersCmd)
	busCmd.AddCommand(busNATSInfoCmd)
	rootCmd.AddCommand(busCmd)
}

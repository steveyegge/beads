package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
)

var busRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register an external event bus handler",
	Long: `Register a shell command as an event bus handler.

The handler will be executed for each matching event with the event JSON on stdin.
The handler should output a JSON result on stdout (see 'bd bus' for protocol details).

Examples:
  bd bus register --id=my-notify --events=Stop,SessionEnd --command="python notify.py"
  bd bus register --id=my-gate --events=PreToolUse --command="./check-policy.sh" --priority=25
  bd bus register --id=my-hook --events=Stop --command="curl -X POST http://hooks.example.com/stop" --persist`,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		eventsStr, _ := cmd.Flags().GetString("events")
		command, _ := cmd.Flags().GetString("command")
		priority, _ := cmd.Flags().GetInt("priority")
		persist, _ := cmd.Flags().GetBool("persist")

		if id == "" {
			return fmt.Errorf("--id is required")
		}
		if eventsStr == "" {
			return fmt.Errorf("--events is required")
		}
		if command == "" {
			return fmt.Errorf("--command is required")
		}

		events := strings.Split(eventsStr, ",")
		for i, e := range events {
			events[i] = strings.TrimSpace(e)
		}

		if daemonClient == nil {
			return fmt.Errorf("daemon not running — bus register requires a running daemon")
		}

		registerArgs := rpc.BusRegisterArgs{
			ID:       id,
			Command:  command,
			Events:   events,
			Priority: priority,
			Persist:  persist,
		}

		argsJSON, err := json.Marshal(registerArgs)
		if err != nil {
			return fmt.Errorf("marshal args: %w", err)
		}

		resp, err := daemonClient.Execute(rpc.OpBusRegister, argsJSON)
		if err != nil {
			return fmt.Errorf("bus register RPC failed: %w", err)
		}
		if !resp.Success {
			return fmt.Errorf("bus register: %s", resp.Error)
		}

		var result rpc.BusRegisterResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			return fmt.Errorf("parse result: %w", err)
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		fmt.Printf("Registered handler %q\n", result.ID)
		if result.Persisted {
			fmt.Println("  (persisted — will survive daemon restart)")
		}
		return nil
	},
}

var busUnregisterCmd = &cobra.Command{
	Use:   "unregister <handler-id>",
	Short: "Remove an event bus handler",
	Long: `Remove a registered event bus handler by ID.

If the handler was persisted, it is also removed from the config table.

Examples:
  bd bus unregister my-notify
  bd bus unregister my-gate`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		if daemonClient == nil {
			return fmt.Errorf("daemon not running — bus unregister requires a running daemon")
		}

		unregArgs := rpc.BusUnregisterArgs{ID: id}
		argsJSON, err := json.Marshal(unregArgs)
		if err != nil {
			return fmt.Errorf("marshal args: %w", err)
		}

		resp, err := daemonClient.Execute(rpc.OpBusUnregister, argsJSON)
		if err != nil {
			return fmt.Errorf("bus unregister RPC failed: %w", err)
		}
		if !resp.Success {
			return fmt.Errorf("bus unregister: %s", resp.Error)
		}

		var result rpc.BusUnregisterResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			return fmt.Errorf("parse result: %w", err)
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		if result.Removed {
			fmt.Printf("Removed handler %q\n", id)
			if result.Persisted {
				fmt.Println("  (also removed from persistent config)")
			}
		} else {
			fmt.Printf("Handler %q not found\n", id)
		}
		return nil
	},
}

func init() {
	busRegisterCmd.Flags().String("id", "", "Unique handler ID (required)")
	busRegisterCmd.Flags().String("events", "", "Comma-separated event types to handle (required)")
	busRegisterCmd.Flags().String("command", "", "Shell command to execute (required)")
	busRegisterCmd.Flags().Int("priority", 50, "Handler priority (lower = earlier)")
	busRegisterCmd.Flags().Bool("persist", false, "Save to config table (survive daemon restart)")

	busCmd.AddCommand(busRegisterCmd)
	busCmd.AddCommand(busUnregisterCmd)
}

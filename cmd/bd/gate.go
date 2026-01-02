package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// gateCmd is the parent command for gate operations
var gateCmd = &cobra.Command{
	Use:     "gate",
	GroupID: "issues",
	Short:   "Manage async coordination gates",
	Long: `Gates are async wait conditions that block workflow steps.

Gates are created automatically when a formula step has a gate field.
They must be closed (manually or via watchers) for the blocked step to proceed.

Gate types:
  human   - Requires manual bd close (Phase 1)
  timer   - Expires after timeout (Phase 2)
  gh:run  - Waits for GitHub workflow (Phase 3)
  gh:pr   - Waits for PR merge (Phase 3)

Examples:
  bd gate list           # Show all open gates
  bd gate list --all     # Show all gates including closed
  bd gate resolve <id>   # Close a gate manually`,
}

// gateListCmd lists gate issues
var gateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List gate issues",
	Long: `List all gate issues in the current beads database.

By default, shows only open gates. Use --all to include closed gates.`,
	Run: func(cmd *cobra.Command, args []string) {
		allFlag, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")

		// Build filter for gate type issues
		gateType := types.TypeGate
		filter := types.IssueFilter{
			IssueType: &gateType,
			Limit:     limit,
		}

		// By default, exclude closed gates
		if !allFlag {
			filter.ExcludeStatus = []types.Status{types.StatusClosed}
		}

		ctx := rootCtx

		// If daemon is running, use RPC
		if daemonClient != nil {
			listArgs := &rpc.ListArgs{
				IssueType: "gate",
				Limit:     limit,
			}
			if !allFlag {
				listArgs.ExcludeStatus = []string{"closed"}
			}

			resp, err := daemonClient.List(listArgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var issues []*types.Issue
			if err := json.Unmarshal(resp.Data, &issues); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				outputJSON(issues)
				return
			}

			displayGates(issues)
			return
		}

		// Direct mode
		issues, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(issues)
			return
		}

		displayGates(issues)
	},
}

// displayGates formats and displays gate issues
func displayGates(gates []*types.Issue) {
	if len(gates) == 0 {
		fmt.Println("No gates found.")
		return
	}

	fmt.Printf("\n%s Open Gates (%d):\n\n", ui.RenderAccent("⏳"), len(gates))

	for _, gate := range gates {
		statusSym := "○"
		if gate.Status == types.StatusClosed {
			statusSym = "●"
		}

		// Format gate info
		gateInfo := gate.AwaitType
		if gate.AwaitID != "" {
			gateInfo = fmt.Sprintf("%s %s", gate.AwaitType, gate.AwaitID)
		}

		// Format timeout if present
		timeoutStr := ""
		if gate.Timeout > 0 {
			timeoutStr = fmt.Sprintf(" (timeout: %s)", gate.Timeout)
		}

		// Find blocked step from ID (gate ID format: parent.gate-stepid)
		blockedStep := ""
		if strings.Contains(gate.ID, ".gate-") {
			parts := strings.Split(gate.ID, ".gate-")
			if len(parts) == 2 {
				blockedStep = fmt.Sprintf("%s.%s", parts[0], parts[1])
			}
		}

		fmt.Printf("%s %s - %s%s\n", statusSym, ui.RenderID(gate.ID), gateInfo, timeoutStr)
		if blockedStep != "" {
			fmt.Printf("  Blocks: %s\n", blockedStep)
		}
		fmt.Println()
	}

	fmt.Printf("To resolve a gate: bd close <gate-id>\n")
}

// gateResolveCmd manually closes a gate
var gateResolveCmd = &cobra.Command{
	Use:   "resolve <gate-id>",
	Short: "Manually resolve (close) a gate",
	Long: `Close a gate issue to unblock the step waiting on it.

This is equivalent to 'bd close <gate-id>' but with a more explicit name.
Use --reason to provide context for why the gate was resolved.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("gate resolve")

		gateID := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		// Verify it's a gate issue
		ctx := rootCtx
		var issue *types.Issue
		var err error

		if daemonClient != nil {
			resp, rerr := daemonClient.Show(&rpc.ShowArgs{ID: gateID})
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", rerr)
				os.Exit(1)
			}
			if !resp.Success {
				fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
				os.Exit(1)
			}
			var details types.IssueDetails
			if uerr := json.Unmarshal(resp.Data, &details); uerr != nil {
				fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", uerr)
				os.Exit(1)
			}
			issue = &details.Issue
		} else {
			issue, err = store.GetIssue(ctx, gateID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: gate not found: %s\n", gateID)
				os.Exit(1)
			}
		}

		if issue.IssueType != types.TypeGate {
			fmt.Fprintf(os.Stderr, "Error: %s is not a gate issue (type=%s)\n", gateID, issue.IssueType)
			os.Exit(1)
		}

		// Close the gate
		if daemonClient != nil {
			closeArgs := &rpc.CloseArgs{
				ID:     gateID,
				Reason: reason,
			}
			resp, cerr := daemonClient.CloseIssue(closeArgs)
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", cerr)
				os.Exit(1)
			}
			if !resp.Success {
				fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
				os.Exit(1)
			}
		} else {
			if err := store.CloseIssue(ctx, gateID, reason, actor, ""); err != nil {
				fmt.Fprintf(os.Stderr, "Error closing gate: %v\n", err)
				os.Exit(1)
			}
			markDirtyAndScheduleFlush()
		}

		fmt.Printf("%s Gate resolved: %s\n", ui.RenderPass("✓"), gateID)
		if reason != "" {
			fmt.Printf("  Reason: %s\n", reason)
		}
	},
}

func init() {
	// gate list flags
	gateListCmd.Flags().BoolP("all", "a", false, "Show all gates including closed")
	gateListCmd.Flags().IntP("limit", "n", 50, "Limit results (default 50)")

	// gate resolve flags
	gateResolveCmd.Flags().StringP("reason", "r", "", "Reason for resolving the gate")

	// Add subcommands
	gateCmd.AddCommand(gateListCmd)
	gateCmd.AddCommand(gateResolveCmd)

	rootCmd.AddCommand(gateCmd)
}

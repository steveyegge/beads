package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

// decisionRemindCmd sends a reminder for a pending decision point
var decisionRemindCmd = &cobra.Command{
	Use:   "remind <decision-id>",
	Short: "Send a reminder for a pending decision point",
	Long: `Re-send notification for a pending decision point.

This increments the reminder count and re-dispatches notifications to configured
channels. Respects the max_reminders limit from escalation config.

Examples:
  # Send reminder for a decision
  bd decision remind gt-abc123.decision-1

  # Force reminder even at max count
  bd decision remind gt-abc123.decision-1 --force`,
	Args: cobra.ExactArgs(1),
	Run:  runDecisionRemind,
}

func init() {
	decisionRemindCmd.Flags().Bool("force", false, "Send reminder even if at max_reminders limit")

	decisionCmd.AddCommand(decisionRemindCmd)
}

func runDecisionRemind(cmd *cobra.Command, args []string) {
	CheckReadonly("decision remind")

	decisionID := args[0]
	force, _ := cmd.Flags().GetBool("force")

	requireDaemon("decision remind")
	remindViaDaemon(decisionID, force)
}

// remindViaDaemon sends a decision reminder via the RPC daemon
func remindViaDaemon(decisionID string, force bool) {
	// Resolve ID via daemon
	resolveResp, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: decisionID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var resolvedID string
	if resolveResp != nil && resolveResp.Data != nil {
		// ResolveID returns the resolved ID as a JSON string
		_ = json.Unmarshal(resolveResp.Data, &resolvedID)
	}
	if resolvedID == "" {
		resolvedID = decisionID
	}

	result, err := daemonClient.DecisionRemind(&rpc.DecisionRemindArgs{
		IssueID: resolvedID,
		Force:   force,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printRemindResult(result.IssueID, result.ReminderCount, result.MaxReminders, result.Prompt)
}

func printRemindResult(id string, reminderCount, maxReminders int, prompt string) {
	if jsonOutput {
		result := map[string]interface{}{
			"id":             id,
			"reminder_count": reminderCount,
			"max_reminders":  maxReminders,
			"prompt":         prompt,
		}
		outputJSON(result)
		return
	}

	fmt.Printf("%s Reminder sent for decision: %s\n\n", ui.RenderPass("✓"), ui.RenderID(id))
	fmt.Printf("  Prompt: %s\n", prompt)
	fmt.Printf("  Reminders: %d/%d\n", reminderCount, maxReminders)

	if reminderCount >= maxReminders {
		fmt.Printf("\n  %s Max reminders reached — escalation event emitted\n", ui.RenderWarn("⚠"))
	}
}

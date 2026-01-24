package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// Default max reminders if not configured
const defaultMaxReminders = 3

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

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	decisionID := args[0]
	force, _ := cmd.Flags().GetBool("force")

	ctx := rootCtx

	// Resolve partial ID
	resolvedID, err := utils.ResolvePartialID(ctx, store, decisionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get the issue to verify it's a decision gate
	issue, err := store.GetIssue(ctx, resolvedID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting issue: %v\n", err)
		os.Exit(1)
	}
	if issue == nil {
		fmt.Fprintf(os.Stderr, "Error: issue %s not found\n", resolvedID)
		os.Exit(1)
	}

	// Verify it's a decision gate
	if issue.IssueType != types.TypeGate || issue.AwaitType != "decision" {
		fmt.Fprintf(os.Stderr, "Error: %s is not a decision point (type=%s, await_type=%s)\n",
			resolvedID, issue.IssueType, issue.AwaitType)
		os.Exit(1)
	}

	// Get the decision point data
	dp, err := store.GetDecisionPoint(ctx, resolvedID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting decision point: %v\n", err)
		os.Exit(1)
	}
	if dp == nil {
		fmt.Fprintf(os.Stderr, "Error: no decision point data for %s\n", resolvedID)
		os.Exit(1)
	}

	// Check if already responded
	if dp.RespondedAt != nil {
		fmt.Fprintf(os.Stderr, "Error: decision %s already responded at %s\n",
			resolvedID, dp.RespondedAt.Format("2006-01-02 15:04"))
		os.Exit(1)
	}

	// Get max_reminders from config (default to 3)
	maxReminders := defaultMaxReminders
	// TODO: Read from escalation.json config when notification system is implemented

	// Check reminder limit
	if dp.ReminderCount >= maxReminders && !force {
		fmt.Fprintf(os.Stderr, "Error: decision %s has reached max reminders (%d/%d)\n",
			resolvedID, dp.ReminderCount, maxReminders)
		fmt.Fprintf(os.Stderr, "  Use --force to send reminder anyway\n")
		os.Exit(1)
	}

	// Increment reminder count
	dp.ReminderCount++

	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating decision point: %v\n", err)
		os.Exit(1)
	}

	markDirtyAndScheduleFlush()

	// Output
	if jsonOutput {
		result := map[string]interface{}{
			"id":             resolvedID,
			"reminder_count": dp.ReminderCount,
			"max_reminders":  maxReminders,
			"prompt":         dp.Prompt,
		}
		outputJSON(result)
		return
	}

	// Human-readable output
	fmt.Printf("%s Reminder sent for decision: %s\n\n", ui.RenderPass("✓"), ui.RenderID(resolvedID))
	fmt.Printf("  Prompt: %s\n", dp.Prompt)
	fmt.Printf("  Reminders: %d/%d\n", dp.ReminderCount, maxReminders)

	if dp.ReminderCount >= maxReminders {
		fmt.Printf("\n  %s Max reminders reached\n", ui.RenderWarn("⚠"))
	}

	// TODO: Dispatch actual notifications when notification system is implemented
	fmt.Println("\n  (Notification dispatch not yet implemented)")
}

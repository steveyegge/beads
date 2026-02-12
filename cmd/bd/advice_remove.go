package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// adviceRemoveCmd removes an advice bead
var adviceRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove an advice bead",
	Long: `Remove an advice bead by closing it.

Closing advice marks it as no longer active. Closed advice won't appear
in agent priming. Use --delete to permanently delete instead of closing.

Examples:
  # Close advice (mark as inactive)
  bd advice remove gt-abc123

  # Permanently delete advice
  bd advice remove gt-abc123 --delete

  # Remove with reason
  bd advice remove gt-abc123 -r "No longer relevant"`,
	Args: cobra.ExactArgs(1),
	Run:  runAdviceRemove,
}

func init() {
	adviceRemoveCmd.Flags().StringP("reason", "r", "Removed", "Reason for removal")
	adviceRemoveCmd.Flags().Bool("delete", false, "Permanently delete instead of closing")
	adviceRemoveCmd.ValidArgsFunction = issueIDCompletion

	adviceCmd.AddCommand(adviceRemoveCmd)
}

func runAdviceRemove(cmd *cobra.Command, args []string) {
	CheckReadonly("advice remove")

	id := args[0]
	reason, _ := cmd.Flags().GetString("reason")
	permanentDelete, _ := cmd.Flags().GetBool("delete")

	// Resolve partial ID via daemon RPC
	var resolvedID string
	resolveArgs := &rpc.ResolveIDArgs{ID: id}
	resolveResp, err := daemonClient.ResolveID(resolveArgs)
	if err != nil {
		FatalError("resolving ID %s: %v", id, err)
	}
	if err := json.Unmarshal(resolveResp.Data, &resolvedID); err != nil {
		FatalError("unmarshaling resolved ID: %v", err)
	}

	// Verify it's an advice issue
	var issue *types.Issue
	showArgs := &rpc.ShowArgs{ID: resolvedID}
	showResp, err := daemonClient.Show(showArgs)
	if err != nil {
		FatalError("getting issue %s: %v", resolvedID, err)
	}
	if err := json.Unmarshal(showResp.Data, &issue); err != nil {
		FatalError("parsing issue: %v", err)
	}

	if issue == nil {
		FatalError("issue %s not found", resolvedID)
	}

	if issue.IssueType != types.IssueType("advice") {
		FatalError("issue %s is not an advice bead (type: %s)", resolvedID, issue.IssueType)
	}

	// Handle permanent delete
	if permanentDelete {
		deleteArgs := &rpc.DeleteArgs{IDs: []string{resolvedID}}
		_, err := daemonClient.Delete(deleteArgs)
		if err != nil {
			FatalError("deleting advice: %v", err)
		}

		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"deleted": resolvedID,
			})
		} else {
			fmt.Printf("%s Deleted advice: %s\n", ui.RenderPass("✓"), ui.RenderID(resolvedID))
		}
		return
	}

	// Close the advice issue
	closeArgs := &rpc.CloseArgs{
		ID:     resolvedID,
		Reason: reason,
	}
	closeResp, err := daemonClient.CloseIssue(closeArgs)
	if err != nil {
		FatalError("closing advice: %v", err)
	}

	var closedIssue types.Issue
	if err := json.Unmarshal(closeResp.Data, &closedIssue); err == nil {
		if hookRunner != nil {
			hookRunner.Run(hooks.EventClose, &closedIssue)
		}
	}

	if jsonOutput {
		fmt.Println(string(closeResp.Data))
	} else {
		fmt.Printf("%s Removed advice: %s (%s)\n", ui.RenderPass("✓"), ui.RenderID(resolvedID), reason)
	}

	SetLastTouchedID(resolvedID)
}

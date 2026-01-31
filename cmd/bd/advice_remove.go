package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
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

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	// Resolve partial ID
	var resolvedID string
	if daemonClient != nil {
		resolveArgs := &rpc.ResolveIDArgs{ID: id}
		resp, err := daemonClient.ResolveID(resolveArgs)
		if err != nil {
			FatalError("resolving ID %s: %v", id, err)
		}
		if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
			FatalError("unmarshaling resolved ID: %v", err)
		}
	} else {
		resolved, err := utils.ResolvePartialID(ctx, store, id)
		if err != nil {
			FatalError("resolving ID %s: %v", id, err)
		}
		resolvedID = resolved
	}

	// Verify it's an advice issue
	var issue *types.Issue
	if daemonClient != nil {
		showArgs := &rpc.ShowArgs{ID: resolvedID}
		resp, err := daemonClient.Show(showArgs)
		if err != nil {
			FatalError("getting issue %s: %v", resolvedID, err)
		}
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			FatalError("parsing issue: %v", err)
		}
	} else {
		var err error
		issue, err = store.GetIssue(ctx, resolvedID)
		if err != nil {
			FatalError("getting issue %s: %v", resolvedID, err)
		}
	}

	if issue == nil {
		FatalError("issue %s not found", resolvedID)
	}

	if issue.IssueType != types.IssueType("advice") {
		FatalError("issue %s is not an advice bead (type: %s)", resolvedID, issue.IssueType)
	}

	// Handle permanent delete
	if permanentDelete {
		if daemonClient != nil {
			deleteArgs := &rpc.DeleteArgs{IDs: []string{resolvedID}}
			_, err := daemonClient.Delete(deleteArgs)
			if err != nil {
				FatalError("deleting advice: %v", err)
			}
		} else {
			if err := store.DeleteIssue(ctx, resolvedID); err != nil {
				FatalError("deleting advice: %v", err)
			}
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
	if daemonClient != nil {
		closeArgs := &rpc.CloseArgs{
			ID:     resolvedID,
			Reason: reason,
		}
		resp, err := daemonClient.CloseIssue(closeArgs)
		if err != nil {
			FatalError("closing advice: %v", err)
		}

		var closedIssue types.Issue
		if err := json.Unmarshal(resp.Data, &closedIssue); err == nil {
			if hookRunner != nil {
				hookRunner.Run(hooks.EventClose, &closedIssue)
			}
		}

		if jsonOutput {
			fmt.Println(string(resp.Data))
		} else {
			fmt.Printf("%s Removed advice: %s (%s)\n", ui.RenderPass("✓"), ui.RenderID(resolvedID), reason)
		}
	} else {
		// Direct mode
		if err := store.CloseIssue(ctx, resolvedID, reason, actor, ""); err != nil {
			FatalError("closing advice: %v", err)
		}

		markDirtyAndScheduleFlush()

		// Get updated issue for hook
		closedIssue, _ := store.GetIssue(ctx, resolvedID)
		if closedIssue != nil && hookRunner != nil {
			hookRunner.Run(hooks.EventClose, closedIssue)
		}

		if jsonOutput {
			outputJSON(closedIssue)
		} else {
			fmt.Printf("%s Removed advice: %s (%s)\n", ui.RenderPass("✓"), ui.RenderID(resolvedID), reason)
		}
	}

	SetLastTouchedID(resolvedID)
}

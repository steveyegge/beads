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

var promoteCmd = &cobra.Command{
	Use:     "promote [id]",
	GroupID: "issues",
	Short:   "Move an issue to a different branch",
	Long: `Move an issue to a different branch (branch-based namespace promotion).

Examples:
  bd promote fix-bug-a3f2 --to main      # Move from feature branch to main
  bd promote a3f2 --to staging           # Move to staging branch`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("promote")

		if len(args) != 1 {
			FatalErrorRespectJSON("promote requires exactly one issue ID")
		}

		id := args[0]
		targetBranch, _ := cmd.Flags().GetString("to")

		if targetBranch == "" {
			FatalErrorRespectJSON("--to flag is required (specify target branch)")
		}

		ctx := rootCtx

		// Resolve partial ID
		var resolvedID string
		var routedArgs []string

		if daemonClient != nil {
			// Check if this ID needs routing to a different beads directory
			if needsRouting(id) {
				routedArgs = append(routedArgs, id)
			} else {
				resolveArgs := &rpc.ResolveIDArgs{ID: id}
				resp, err := daemonClient.ResolveID(resolveArgs)
				if err != nil {
					FatalErrorRespectJSON("resolving ID %s: %v", id, err)
				}
				if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
					FatalErrorRespectJSON("unmarshaling resolved ID: %v", err)
				}
			}
		} else {
			// Direct mode - check routing for the ID
			if needsRouting(id) {
				routedArgs = append(routedArgs, id)
			} else {
				resolved, err := utils.ResolvePartialID(ctx, store, id)
				if err != nil {
					FatalErrorRespectJSON("resolving ID %s: %v", id, err)
				}
				resolvedID = resolved
			}
		}

		// If daemon is running, use RPC
		if daemonClient != nil {
			var promotedIssues []*types.Issue

			// Handle local ID
			if resolvedID != "" {
				// Get current issue to display branch change
				showArgs := &rpc.ShowArgs{ID: resolvedID}
				showResp, err := daemonClient.Show(showArgs)
				if err != nil {
					FatalErrorRespectJSON("fetching issue %s: %v", resolvedID, err)
				}

				var issue types.Issue
				if err := json.Unmarshal(showResp.Data, &issue); err != nil {
					FatalErrorRespectJSON("parsing issue: %v", err)
				}

				fromBranch := issue.Branch
				if fromBranch == "" {
					fromBranch = "main"
				}

				// Update branch via RPC
				updateArgs := &rpc.UpdateArgs{
					ID:     resolvedID,
					Branch: &targetBranch,
				}
				resp, err := daemonClient.Update(updateArgs)
				if err != nil {
					FatalErrorRespectJSON("promoting issue: %v", err)
				}

				var updatedIssue types.Issue
				if err := json.Unmarshal(resp.Data, &updatedIssue); err != nil {
					FatalErrorRespectJSON("parsing updated issue: %v", err)
				}

				// Run promote hook (reuse close event for now, or create new hook type)
				if hookRunner != nil {
					hookRunner.Run(hooks.EventClose, &updatedIssue)
				}

				if jsonOutput {
					promotedIssues = append(promotedIssues, &updatedIssue)
				} else {
					fmt.Printf("%s Promoted %s from %q to %q: %s\n",
						ui.RenderPass("✓"), resolvedID,
						fromBranch, targetBranch,
						updatedIssue.Title)
				}
			}

			// Handle routed IDs via direct mode (cross-rig)
			for _, routedID := range routedArgs {
				result, err := resolveAndGetIssueWithRouting(ctx, store, routedID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", routedID, err)
					continue
				}
				if result == nil || result.Issue == nil {
					if result != nil {
						result.Close()
					}
					fmt.Fprintf(os.Stderr, "Issue %s not found\n", routedID)
					continue
				}

				fromBranch := result.Issue.Branch
				if fromBranch == "" {
					fromBranch = "main"
				}

				// Update branch
				updates := map[string]interface{}{
					"branch": targetBranch,
				}
				if err := result.Store.UpdateIssue(ctx, result.ResolvedID, updates, actor); err != nil {
					result.Close()
					fmt.Fprintf(os.Stderr, "Error promoting %s: %v\n", routedID, err)
					continue
				}

				// Get updated issue for hook
				updatedIssue, _ := result.Store.GetIssue(ctx, result.ResolvedID)
				if updatedIssue != nil && hookRunner != nil {
					hookRunner.Run(hooks.EventClose, updatedIssue)
				}

				if jsonOutput {
					if updatedIssue != nil {
						promotedIssues = append(promotedIssues, updatedIssue)
					}
				} else {
					title := ""
					if updatedIssue != nil {
						title = updatedIssue.Title
					}
					fmt.Printf("%s Promoted %s from %q to %q: %s\n",
						ui.RenderPass("✓"), result.ResolvedID,
						fromBranch, targetBranch,
						title)
				}
				result.Close()
			}

			if jsonOutput && len(promotedIssues) > 0 {
				outputJSON(promotedIssues)
			}
			return
		}

		// Direct mode
		var promotedIssues []*types.Issue

		// Handle local ID
		if resolvedID != "" {
			issue, err := store.GetIssue(ctx, resolvedID)
			if err != nil {
				FatalErrorRespectJSON("fetching issue %s: %v", resolvedID, err)
			}
			if issue == nil {
				FatalErrorRespectJSON("issue %s not found", resolvedID)
			}

			fromBranch := issue.Branch
			if fromBranch == "" {
				fromBranch = "main"
			}

			// Update branch
			updates := map[string]interface{}{
				"branch": targetBranch,
			}
			if err := store.UpdateIssue(ctx, resolvedID, updates, actor); err != nil {
				FatalErrorRespectJSON("promoting issue: %v", err)
			}

			// Get updated issue for hook
			updatedIssue, _ := store.GetIssue(ctx, resolvedID)
			if updatedIssue != nil && hookRunner != nil {
				hookRunner.Run(hooks.EventClose, updatedIssue)
			}

			if jsonOutput {
				if updatedIssue != nil {
					promotedIssues = append(promotedIssues, updatedIssue)
				}
			} else {
				fmt.Printf("%s Promoted %s from %q to %q: %s\n",
					ui.RenderPass("✓"), resolvedID,
					fromBranch, targetBranch,
					issue.Title)
			}
		}

		// Handle routed IDs (cross-rig)
		for _, routedID := range routedArgs {
			result, err := resolveAndGetIssueWithRouting(ctx, store, routedID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", routedID, err)
				continue
			}
			if result == nil || result.Issue == nil {
				if result != nil {
					result.Close()
				}
				fmt.Fprintf(os.Stderr, "Issue %s not found\n", routedID)
				continue
			}

			fromBranch := result.Issue.Branch
			if fromBranch == "" {
				fromBranch = "main"
			}

			// Update branch
			updates := map[string]interface{}{
				"branch": targetBranch,
			}
			if err := result.Store.UpdateIssue(ctx, result.ResolvedID, updates, actor); err != nil {
				result.Close()
				fmt.Fprintf(os.Stderr, "Error promoting %s: %v\n", routedID, err)
				continue
			}

			// Get updated issue for hook
			updatedIssue, _ := result.Store.GetIssue(ctx, result.ResolvedID)
			if updatedIssue != nil && hookRunner != nil {
				hookRunner.Run(hooks.EventClose, updatedIssue)
			}

			if jsonOutput {
				if updatedIssue != nil {
					promotedIssues = append(promotedIssues, updatedIssue)
				}
			} else {
				title := ""
				if updatedIssue != nil {
					title = updatedIssue.Title
				}
				fmt.Printf("%s Promoted %s from %q to %q: %s\n",
					ui.RenderPass("✓"), result.ResolvedID,
					fromBranch, targetBranch,
					title)
			}
			result.Close()
		}

		// Schedule auto-flush if any issues were promoted
		if len(args) > 0 {
			markDirtyAndScheduleFlush()
		}

		if jsonOutput && len(promotedIssues) > 0 {
			outputJSON(promotedIssues)
		}
	},
}

func init() {
	promoteCmd.Flags().StringP("to", "t", "", "Target branch (required)")
	_ = promoteCmd.MarkFlagRequired("to")
	promoteCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(promoteCmd)
}

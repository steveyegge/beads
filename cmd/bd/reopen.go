package main
import (
	"encoding/json"
	"fmt"
	"os"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)
var reopenCmd = &cobra.Command{
	Use:     "reopen [id...]",
	GroupID: "issues",
	Short:   "Reopen one or more closed issues",
	Long: `Reopen closed issues by setting status to 'open' and clearing the closed_at timestamp.
This is more explicit than 'bd update --status open' and emits a Reopened event.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("reopen")
		reason, _ := cmd.Flags().GetString("reason")
		// Use global jsonOutput set by PersistentPreRun
		requireDaemon("reopen")
		{
			// Resolve partial IDs via daemon
			var resolvedIDs []string
			for _, id := range args {
				resolveArgs := &rpc.ResolveIDArgs{ID: id}
				resp, err := daemonClient.ResolveID(resolveArgs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolving ID %s: %v\n", id, err)
					os.Exit(1)
				}
				var resolvedID string
				if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
					fmt.Fprintf(os.Stderr, "Error unmarshaling resolved ID: %v\n", err)
					os.Exit(1)
				}
				resolvedIDs = append(resolvedIDs, resolvedID)
			}
			reopenedIssues := []*types.Issue{}
			for _, id := range resolvedIDs {
				openStatus := string(types.StatusOpen)
				// Use atomic UpdateWithComment to update status and add reason in a single transaction
				updateArgs := &rpc.UpdateWithCommentArgs{
					UpdateArgs: rpc.UpdateArgs{
						ID:     id,
						Status: &openStatus,
					},
					CommentText:   reason,
					CommentAuthor: actor,
				}
				resp, err := daemonClient.UpdateWithComment(updateArgs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reopening %s: %v\n", id, err)
					continue
				}
				if jsonOutput {
					var issue types.Issue
					if err := json.Unmarshal(resp.Data, &issue); err == nil {
						reopenedIssues = append(reopenedIssues, &issue)
					}
				} else {
					reasonMsg := ""
					if reason != "" {
						reasonMsg = ": " + reason
					}
					fmt.Printf("%s Reopened %s%s\n", ui.RenderAccent("↻"), id, reasonMsg)
				}
			}
			if jsonOutput && len(reopenedIssues) > 0 {
				outputJSON(reopenedIssues)
			}
		}
	},
}
func init() {
	reopenCmd.Flags().StringP("reason", "r", "", "Reason for reopening")
	reopenCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(reopenCmd)
}

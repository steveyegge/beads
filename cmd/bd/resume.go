package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume blocked issues",
	Run: func(cmd *cobra.Command, _ []string) {
		specID, _ := cmd.Flags().GetString("spec")
		specAlias, _ := cmd.Flags().GetString("spec-id")
		if specID == "" {
			specID = specAlias
		} else if specAlias != "" && specAlias != specID {
			FatalErrorRespectJSON("--spec and --spec-id must match if both are provided")
		}
		if specID == "" {
			FatalErrorRespectJSON("spec id is required (use --spec or --spec-id)")
		}

		status, _ := cmd.Flags().GetString("status")
		if status == "" {
			status = string(types.StatusOpen)
		}

		filter := types.IssueFilter{
			SpecID: &specID,
		}

		var issues []*types.Issue
		var err error

		if daemonClient != nil {
			resp, rpcErr := daemonClient.List(&rpc.ListArgs{})
			if rpcErr != nil {
				FatalErrorRespectJSON("resume failed: %v", rpcErr)
			}
			if unmarshalErr := json.Unmarshal(resp.Data, &issues); unmarshalErr != nil {
				FatalErrorRespectJSON("invalid resume response: %v", unmarshalErr)
			}
		} else {
			if err := ensureDatabaseFresh(rootCtx); err != nil {
				FatalErrorRespectJSON("%v", err)
			}
			issues, err = store.SearchIssues(rootCtx, "", filter)
			if err != nil {
				FatalErrorRespectJSON("resume failed: %v", err)
			}
		}

		updated := 0
		for _, issue := range issues {
			if issue.SpecID != specID || issue.Status != types.StatusBlocked {
				continue
			}
			if daemonClient != nil {
				statusCopy := status
				if _, err := daemonClient.Update(&rpc.UpdateArgs{ID: issue.ID, Status: &statusCopy}); err != nil {
					FatalErrorRespectJSON("resume failed: %v", err)
				}
			} else {
				if err := store.UpdateIssue(rootCtx, issue.ID, map[string]interface{}{"status": status}, getActorWithGit()); err != nil {
					FatalErrorRespectJSON("resume failed: %v", err)
				}
			}
			updated++
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"spec_id": specID,
				"updated": updated,
			})
			return
		}

		fmt.Printf("%s Resumed %d issue(s) linked to %s\n", ui.RenderPass("✓"), updated, specID)
	},
}

func init() {
	resumeCmd.Flags().String("spec", "", "Spec identifier/path to resume issues for")
	resumeCmd.Flags().String("spec-id", "", "Alias for --spec")
	resumeCmd.Flags().String("status", string(types.StatusOpen), "Status to set when resuming (default: open)")
	rootCmd.AddCommand(resumeCmd)
}

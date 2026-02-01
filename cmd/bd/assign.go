package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
)

type assignResult struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee"`
}

var assignCmd = &cobra.Command{
	Use:     "assign <id...>",
	GroupID: "issues",
	Short:   "Assign issues to an agent",
	Args:    cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("assign")

		assignee, _ := cmd.Flags().GetString("to")
		if assignee == "" {
			FatalErrorRespectJSON("--to is required")
		}

		var results []assignResult

		if daemonClient != nil {
			var routedArgs []string
			for _, id := range args {
				if needsRouting(id) {
					routedArgs = append(routedArgs, id)
					continue
				}
				resolveArgs := &rpc.ResolveIDArgs{ID: id}
				resp, err := daemonClient.ResolveID(resolveArgs)
				if err != nil {
					FatalErrorRespectJSON("resolving ID %s: %v", id, err)
				}
				var resolvedID string
				if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
					FatalErrorRespectJSON("unmarshaling resolved ID: %v", err)
				}
				updateArgs := &rpc.UpdateArgs{ID: resolvedID, Assignee: &assignee}
				if _, err := daemonClient.Update(updateArgs); err != nil {
					fmt.Fprintf(os.Stderr, "Error assigning %s: %v\n", id, err)
					continue
				}
				results = append(results, assignResult{ID: resolvedID, Assignee: assignee})
			}

			for _, id := range routedArgs {
				if err := assignDirect(id, assignee); err != nil {
					fmt.Fprintf(os.Stderr, "Error assigning %s: %v\n", id, err)
					continue
				}
				results = append(results, assignResult{ID: id, Assignee: assignee})
			}
		} else {
			for _, id := range args {
				if err := assignDirect(id, assignee); err != nil {
					fmt.Fprintf(os.Stderr, "Error assigning %s: %v\n", id, err)
					continue
				}
				results = append(results, assignResult{ID: id, Assignee: assignee})
			}
		}

		if jsonOutput {
			printJSON(results)
			return
		}

		for _, res := range results {
			fmt.Printf("Assigned %s to %s\n", res.ID, res.Assignee)
		}
	},
}

func init() {
	assignCmd.Flags().String("to", "", "Assignee name")
	rootCmd.AddCommand(assignCmd)
}

func assignDirect(id, assignee string) error {
	actor := getActorWithGit()
	result, err := resolveAndGetIssueWithRouting(rootCtx, store, id)
	if err != nil {
		return err
	}
	if result == nil || result.Issue == nil {
		if result != nil {
			result.Close()
		}
		return fmt.Errorf("issue not found: %s", id)
	}
	defer result.Close()
	updates := map[string]interface{}{"assignee": assignee}
	return result.Store.UpdateIssue(rootCtx, result.ResolvedID, updates, actor)
}

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

var refileCmd = &cobra.Command{
	Use:     "refile <source-id> <target-rig>",
	GroupID: "issues",
	Short:   "Move an issue to a different rig",
	Long: `Move an issue from one rig to another.

This creates a new issue in the target rig with the same content,
then closes the source issue with a reference to the new location.

The target rig can be specified as:
  - A rig name: beads, gastown
  - A prefix: bd-, gt-
  - A prefix without hyphen: bd, gt

Examples:
  bd refile bd-8hea gastown     # Move to gastown by rig name
  bd refile bd-8hea gt-         # Move to gastown by prefix
  bd refile bd-8hea gt          # Move to gastown (prefix without hyphen)`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("refile")

		sourceID := args[0]
		targetRig := args[1]

		keepOpen, _ := cmd.Flags().GetBool("keep-open")

		requireDaemon("refile")
		refileViaDaemon(sourceID, targetRig, keepOpen)
	},
}

// refileViaDaemon refiles an issue via the RPC daemon (bd-wj80).
func refileViaDaemon(sourceID, targetRig string, keepOpen bool) {
	args := &rpc.RefileArgs{
		IssueID:   sourceID,
		TargetRig: targetRig,
		KeepOpen:  keepOpen,
	}

	result, err := daemonClient.Refile(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"source": result.SourceID,
			"target": result.TargetID,
			"closed": result.Closed,
		})
	} else {
		fmt.Printf("%s Refiled %s → %s\n", ui.RenderPass("✓"), result.SourceID, result.TargetID)
		if result.Closed {
			fmt.Printf("  Source issue closed\n")
		}
	}
}

func init() {
	refileCmd.Flags().Bool("keep-open", false, "Keep the source issue open (don't close it)")
	refileCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(refileCmd)
}

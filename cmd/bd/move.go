package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

var moveCmd = &cobra.Command{
	Use:     "move <issue-id> --to <rig|prefix>",
	GroupID: "issues",
	Short:   "Move an issue to a different rig with dependency remapping",
	Long: `Move an issue from one rig to another, updating dependencies.

This command:
1. Creates a new issue in the target rig with the same content
2. Updates dependencies that reference the old ID (see below)
3. Closes the source issue with a redirect note

The target rig can be specified as:
  - A rig name: beads, gastown
  - A prefix: bd-, gt-
  - A prefix without hyphen: bd, gt

Dependency handling for cross-rig moves:
  - Issues that depend ON the moved issue: updated to external refs
  - Issues that the moved issue DEPENDS ON: removed (recreate manually in target)

Note: Labels are copied. Comments and event history are not transferred.

Examples:
  bd move hq-c21fj --to beads     # Move to beads by rig name
  bd move hq-q3tki --to gt-       # Move to gastown by prefix
  bd move hq-1h2to --to gt        # Move to gastown (prefix without hyphen)`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("move")

		sourceID := args[0]
		targetRig, _ := cmd.Flags().GetString("to")
		if targetRig == "" {
			FatalError("--to flag is required. Specify target rig (e.g., --to beads, --to gt-)")
		}

		keepOpen, _ := cmd.Flags().GetBool("keep-open")
		skipDeps, _ := cmd.Flags().GetBool("skip-deps")

		requireDaemon("move")
		moveViaDaemon(sourceID, targetRig, keepOpen, skipDeps)
	},
}

// moveViaDaemon moves an issue via the RPC daemon (bd-wj80).
func moveViaDaemon(sourceID, targetRig string, keepOpen, skipDeps bool) {
	args := &rpc.MoveArgs{
		IssueID:   sourceID,
		TargetRig: targetRig,
		KeepOpen:  keepOpen,
		SkipDeps:  skipDeps,
	}

	result, err := daemonClient.Move(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"source":        result.SourceID,
			"target":        result.TargetID,
			"closed":        result.Closed,
			"deps_remapped": result.DepsRemapped,
		})
	} else {
		fmt.Printf("%s Moved %s → %s\n", ui.RenderPass("✓"), result.SourceID, result.TargetID)
		if result.DepsRemapped > 0 {
			fmt.Printf("  Remapped %d dependencies\n", result.DepsRemapped)
		}
		if result.Closed {
			fmt.Printf("  Source issue closed\n")
		}
	}
}

func init() {
	moveCmd.Flags().String("to", "", "Target rig or prefix (required)")
	moveCmd.Flags().Bool("keep-open", false, "Keep the source issue open (don't close it)")
	moveCmd.Flags().Bool("skip-deps", false, "Skip dependency remapping")
	moveCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(moveCmd)
}

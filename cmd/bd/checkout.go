package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

var checkoutCmd = &cobra.Command{
	Use:     "checkout <branch>",
	GroupID: "sync",
	Short:   "Switch to a Dolt branch",
	Long: `Switch the Dolt database to the specified branch.

If the branch is registered with a merge strategy, the strategy will be
shown. Use 'bd branch' to see all branches and their strategies.

Examples:
  bd checkout feature-xyz    # Switch to feature-xyz branch
  bd checkout main           # Switch back to main`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		branchName := args[0]

		if err := store.Checkout(ctx, branchName); err != nil {
			FatalErrorRespectJSON("failed to checkout branch: %v", err)
		}

		if jsonOutput {
			info, _ := store.GetBranchInfo(ctx, branchName)
			result := map[string]interface{}{
				"branch": branchName,
			}
			if info != nil {
				result["strategy"] = info.MergeStrategy
			}
			outputJSON(result)
			return
		}

		fmt.Printf("Switched to branch %s", ui.RenderAccent(branchName))

		// Show strategy info if registered
		if info, err := store.GetBranchInfo(ctx, branchName); err == nil && info != nil {
			fmt.Printf(" (%s)", info.MergeStrategy)
		}
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
}

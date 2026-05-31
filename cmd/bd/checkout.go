package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

var checkoutCmd = &cobra.Command{
	Use:     "checkout <branch>",
	GroupID: "sync",
	Short:   "Switch to a different branch",
	Long: `Switch the Dolt database to a different branch.

This command requires the Dolt storage backend. The target branch
must already exist (create one with 'bd branch <name>').

Examples:
  bd checkout main             # Switch to the main branch
  bd checkout feature-xyz      # Switch to feature-xyz branch`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		branch := args[0]

		previousBranch, _ := store.CurrentBranch(ctx)

		if err := store.Checkout(ctx, branch); err != nil {
			FatalErrorRespectJSON("failed to checkout branch: %v", err)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"previous": previousBranch,
				"current":  branch,
			})
			return
		}

		fmt.Printf("Switched to branch: %s\n", ui.RenderAccent(branch))
	},
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
}

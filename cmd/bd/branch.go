package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

var branchDelete bool

var branchCmd = &cobra.Command{
	Use:     "branch [name]",
	GroupID: "sync",
	Short:   "List, create, or delete branches",
	Long: `List all branches, create a new branch, or delete an existing branch.

This command requires the Dolt storage backend. Without arguments,
it lists all branches. With an argument, it creates a new branch.
With -d, it deletes the named branch.

Examples:
  bd branch                    # List all branches
  bd branch feature-xyz        # Create a new branch named feature-xyz
  bd branch -d feature-xyz     # Delete branch feature-xyz`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		if branchDelete {
			if len(args) == 0 {
				FatalErrorRespectJSON("branch name required for deletion")
			}
			branchName := args[0]

			currentBranch, err := store.CurrentBranch(ctx)
			if err == nil && currentBranch == branchName {
				FatalErrorRespectJSON("cannot delete the currently checked-out branch %q", branchName)
			}

			if err := store.DeleteBranch(ctx, branchName); err != nil {
				FatalErrorRespectJSON("failed to delete branch: %v", err)
			}

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"deleted": branchName,
				})
				return
			}

			fmt.Printf("Deleted branch: %s\n", ui.RenderAccent(branchName))
			return
		}

		// If no args, list branches
		if len(args) == 0 {
			branches, err := store.ListBranches(ctx)
			if err != nil {
				FatalErrorRespectJSON("failed to list branches: %v", err)
			}

			currentBranch, err := store.CurrentBranch(ctx)
			if err != nil {
				currentBranch = ""
			}

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"current":  currentBranch,
					"branches": branches,
				})
				return
			}

			fmt.Printf("\n%s Branches:\n\n", ui.RenderAccent("🌿"))
			for _, branch := range branches {
				if branch == currentBranch {
					fmt.Printf("  * %s\n", ui.StatusInProgressStyle.Render(branch))
				} else {
					fmt.Printf("    %s\n", branch)
				}
			}
			fmt.Println()
			return
		}

		// Create new branch
		branchName := args[0]
		if err := store.Branch(ctx, branchName); err != nil {
			FatalErrorRespectJSON("failed to create branch: %v", err)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"created": branchName,
			})
			return
		}

		fmt.Printf("Created branch: %s\n", ui.RenderAccent(branchName))
	},
}

func init() {
	branchCmd.Flags().BoolVarP(&branchDelete, "delete", "d", false, "Delete the named branch")
	rootCmd.AddCommand(branchCmd)
}

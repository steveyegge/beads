package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

var vcCmd = &cobra.Command{
	Use:     "vc",
	GroupID: "sync",
	Short:   "Version control operations (requires Dolt backend)",
	Long: `Version control operations for the beads database.

These commands require the Dolt storage backend. They provide git-like
version control for your issue data, including branching, merging, and
viewing history.

Note: 'bd history', 'bd diff', and 'bd branch' also work for quick access.
This subcommand provides additional operations like merge and commit.`,
}

var vcMergeStrategy string

var vcMergeCmd = &cobra.Command{
	Use:   "merge [branch]",
	Short: "Merge a branch into the current branch",
	Long: `Merge the specified branch into the current branch.

If there are merge conflicts, they will be reported. You can resolve
conflicts with --strategy.

Use --from and --to for CI workflows where source and target branches are
explicit. When --to is specified, the store switches to the target branch
before merging. Use --cleanup to delete the source branch and mark it as
'merged' in the registry after a successful merge.

Examples:
  bd vc merge feature-xyz                    # Merge feature-xyz into current branch
  bd vc merge feature-xyz --strategy theirs  # Merge, preferring their changes on conflict
  bd vc merge --from=feature-xyz --to=main   # CI: merge feature-xyz into main
  bd vc merge --from=feature-xyz --to=main --cleanup  # CI: merge and clean up`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		fromBranch, _ := cmd.Flags().GetString("from")
		toBranch, _ := cmd.Flags().GetString("to")
		cleanup, _ := cmd.Flags().GetBool("cleanup")

		// Determine source branch
		branchName := fromBranch
		if branchName == "" && len(args) > 0 {
			branchName = args[0]
		}
		if branchName == "" {
			FatalErrorRespectJSON("source branch required: provide a positional argument or --from")
		}

		// If --to is specified, switch to target branch first
		if toBranch != "" {
			if err := store.Checkout(ctx, toBranch); err != nil {
				FatalErrorRespectJSON("failed to checkout target branch %s: %v", toBranch, err)
			}
		}

		// Perform merge
		conflicts, err := store.Merge(ctx, branchName)
		if err != nil {
			FatalErrorRespectJSON("failed to merge branch: %v", err)
		}

		// Handle conflicts
		if len(conflicts) > 0 {
			if vcMergeStrategy != "" {
				// Auto-resolve conflicts with specified strategy
				for _, conflict := range conflicts {
					table := conflict.Field // Field contains table name from GetConflicts
					if table == "" {
						table = "issues" // Default to issues table
					}
					if err := store.ResolveConflicts(ctx, table, vcMergeStrategy); err != nil {
						FatalErrorRespectJSON("failed to resolve conflicts: %v", err)
					}
				}
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"merged":        branchName,
						"conflicts":     len(conflicts),
						"resolved_with": vcMergeStrategy,
					})
				} else {
					fmt.Printf("Merged %s with %d conflicts resolved using '%s' strategy\n",
						ui.RenderAccent(branchName), len(conflicts), vcMergeStrategy)
				}
			} else {
				// Report conflicts without auto-resolution
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"merged":    branchName,
						"conflicts": conflicts,
					})
				} else {
					fmt.Printf("\n%s Merge completed with conflicts:\n\n", ui.RenderAccent("!!"))
					for _, conflict := range conflicts {
						fmt.Printf("  - %s\n", conflict.Field)
					}
					fmt.Printf("\nResolve conflicts with: bd vc merge %s --strategy [ours|theirs]\n\n", branchName)
				}
				os.Exit(1) // Non-zero for CI
				return
			}
		} else {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"merged":    branchName,
					"conflicts": 0,
				})
			} else {
				fmt.Printf("Successfully merged %s\n", ui.RenderAccent(branchName))
			}
		}

		// Commit the merge
		currentBranch, _ := store.CurrentBranch(ctx)
		commitMsg := fmt.Sprintf("merge: %s → %s", branchName, currentBranch)
		if err := store.Commit(ctx, commitMsg); err != nil {
			if !isDoltNothingToCommit(err) {
				FatalErrorRespectJSON("failed to commit merge: %v", err)
			}
		}
		commandDidExplicitDoltCommit = true

		// Update .beads/HEAD and refs after merge
		writeBeadsRefs(ctx, store)

		// Cleanup: delete source branch and mark as merged
		if cleanup {
			// Mark as merged in registry (best effort)
			_ = store.UnregisterBranch(ctx, branchName, "merged")

			// Delete the Dolt branch
			if err := store.DeleteBranch(ctx, branchName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not delete source branch %s: %v\n", branchName, err)
			} else if !jsonOutput {
				fmt.Printf("Cleaned up branch %s\n", ui.RenderMuted(branchName))
			}
		}
	},
}

var vcCommitMessage string
var vcCommitStdin bool

var vcCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Create a commit with all staged changes",
	Long: `Create a new Dolt commit with all current changes.

Examples:
  bd vc commit -m "Added new feature issues"
  bd vc commit --message "Fixed priority on several issues"
  echo "Multi-line message" | bd vc commit --stdin`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		if vcCommitStdin {
			if vcCommitMessage != "" {
				FatalErrorRespectJSON("cannot specify both --stdin and -m/--message")
			}
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				FatalErrorRespectJSON("failed to read commit message from stdin: %v", err)
			}
			vcCommitMessage = strings.TrimRight(string(b), "\n")
		}

		if vcCommitMessage == "" {
			FatalErrorRespectJSON("commit message is required (use -m, --message, or --stdin)")
		}

		// We are explicitly creating a Dolt commit; avoid redundant auto-commit in PersistentPostRun.
		commandDidExplicitDoltCommit = true
		if err := store.Commit(ctx, vcCommitMessage); err != nil {
			FatalErrorRespectJSON("failed to commit: %v", err)
		}

		// Get the new commit hash
		hash, err := store.GetCurrentCommit(ctx)
		if err != nil {
			hash = "(unknown)"
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"committed": true,
				"hash":      hash,
				"message":   vcCommitMessage,
			})
			return
		}

		fmt.Printf("Created commit %s\n", ui.RenderMuted(hash[:8]))
	},
}

var vcStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current branch and uncommitted changes",
	Long: `Show the current branch, commit hash, and any uncommitted changes.

Examples:
  bd vc status`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		currentBranch, err := store.CurrentBranch(ctx)
		if err != nil {
			FatalErrorRespectJSON("failed to get current branch: %v", err)
		}

		currentCommit, err := store.GetCurrentCommit(ctx)
		if err != nil {
			currentCommit = "(unknown)"
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"branch": currentBranch,
				"commit": currentCommit,
			})
			return
		}

		fmt.Printf("\n%s Version Control Status\n\n", ui.RenderAccent("📊"))
		fmt.Printf("  Branch: %s\n", ui.StatusInProgressStyle.Render(currentBranch))
		fmt.Printf("  Commit: %s\n", ui.RenderMuted(currentCommit[:8]))
		fmt.Println()
	},
}

func init() {
	vcMergeCmd.Flags().StringVar(&vcMergeStrategy, "strategy", "", "Conflict resolution strategy: 'ours' or 'theirs'")
	vcMergeCmd.Flags().String("from", "", "Source branch to merge (alternative to positional arg)")
	vcMergeCmd.Flags().String("to", "", "Target branch to merge into (switches to it before merge)")
	vcMergeCmd.Flags().Bool("cleanup", false, "Delete source branch and mark as merged after successful merge")
	vcCommitCmd.Flags().StringVarP(&vcCommitMessage, "message", "m", "", "Commit message")
	vcCommitCmd.Flags().BoolVar(&vcCommitStdin, "stdin", false, "Read commit message from stdin")

	vcCmd.AddCommand(vcMergeCmd)
	vcCmd.AddCommand(vcCommitCmd)
	vcCmd.AddCommand(vcStatusCmd)
	rootCmd.AddCommand(vcCmd)
}

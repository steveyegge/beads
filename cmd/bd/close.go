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
)

var closeCmd = &cobra.Command{
	Use:     "close [id...]",
	GroupID: "issues",
	Short:   "Close one or more issues",
	Long: `Close one or more issues.

If no issue ID is provided, closes the last touched issue (from most recent
create, update, show, or close operation).`,
	Args: cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("close")

		// If no IDs provided, use last touched issue
		if len(args) == 0 {
			lastTouched := GetLastTouchedID()
			if lastTouched == "" {
				FatalErrorRespectJSON("no issue ID provided and no last touched issue")
			}
			args = []string{lastTouched}
		}
		reason, _ := cmd.Flags().GetString("reason")
		if reason == "" {
			// Check --resolution alias (Jira CLI convention)
			reason, _ = cmd.Flags().GetString("resolution")
		}
		if reason == "" {
			// Check -m alias (git commit convention)
			reason, _ = cmd.Flags().GetString("message")
		}
		if reason == "" {
			reason = "Closed"
		}
		force, _ := cmd.Flags().GetBool("force")
		continueFlag, _ := cmd.Flags().GetBool("continue")
		noAuto, _ := cmd.Flags().GetBool("no-auto")
		suggestNext, _ := cmd.Flags().GetBool("suggest-next")

		// Get session ID from flag or environment variable
		session, _ := cmd.Flags().GetString("session")
		if session == "" {
			session = os.Getenv("CLAUDE_SESSION_ID")
		}

		ctx := rootCtx

		// --continue only works with a single issue
		if continueFlag && len(args) > 1 {
			FatalErrorRespectJSON("--continue only works when closing a single issue")
		}

		// --suggest-next only works with a single issue
		if suggestNext && len(args) > 1 {
			FatalErrorRespectJSON("--suggest-next only works when closing a single issue")
		}

		// Resolve partial IDs, splitting into local vs routed (bd-z344)
		batch, err := resolveIDsWithRouting(ctx, store, daemonClient, args)
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		resolvedIDs := batch.ResolvedIDs
		routedArgs := batch.RoutedArgs

		// If daemon is running, use RPC
		if daemonClient != nil {
			closedIssues := []*types.Issue{}
			for _, id := range resolvedIDs {
				// Get issue for template and pinned checks
				showArgs := &rpc.ShowArgs{ID: id}
				showResp, showErr := daemonClient.Show(showArgs)
				if showErr == nil {
					var issue types.Issue
					if json.Unmarshal(showResp.Data, &issue) == nil {
						if err := validateIssueClosable(id, &issue, force); err != nil {
							fmt.Fprintf(os.Stderr, "%s\n", err)
							continue
						}
					}
				}

				closeArgs := &rpc.CloseArgs{
					ID:          id,
					Reason:      reason,
					Session:     session,
					SuggestNext: suggestNext,
					Force:       force,
				}
				resp, err := daemonClient.CloseIssue(closeArgs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", id, err)
					continue
				}

				// Handle response based on whether SuggestNext was requested
				if suggestNext {
					var result rpc.CloseResult
					if err := json.Unmarshal(resp.Data, &result); err == nil {
						if result.Closed != nil {
							// Run close hook
							if hookRunner != nil {
								hookRunner.Run(hooks.EventClose, result.Closed)
							}
							if jsonOutput {
								closedIssues = append(closedIssues, result.Closed)
							}
						}
						if !jsonOutput {
							fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), id, reason)
							// Display newly unblocked issues
							if len(result.Unblocked) > 0 {
								fmt.Printf("\nNewly unblocked:\n")
								for _, issue := range result.Unblocked {
									fmt.Printf("  • %s %q (P%d)\n", issue.ID, issue.Title, issue.Priority)
								}
							}
						}
					}
				} else {
					var issue types.Issue
					if err := json.Unmarshal(resp.Data, &issue); err == nil {
						// Run close hook
						if hookRunner != nil {
							hookRunner.Run(hooks.EventClose, &issue)
						}
						if jsonOutput {
							closedIssues = append(closedIssues, &issue)
						}
					}
					if !jsonOutput {
						fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), id, reason)
					}
				}
			}

			// Handle routed IDs via centralized routing (bd-z344)
			forEachRoutedID(ctx, store, routedArgs, func(resolvedID string, routedClient *rpc.Client, directResult *RoutedResult) error {
				if routedClient != nil {
					routedClient.SetActor(actor)
					closeArgs := &rpc.CloseArgs{
						ID:      resolvedID,
						Reason:  reason,
						Session: session,
						Force:   force,
					}
					resp, closeErr := routedClient.CloseIssue(closeArgs)
					routedClient.Close()
					if closeErr != nil {
						return closeErr
					}
					var issue types.Issue
					if json.Unmarshal(resp.Data, &issue) == nil {
						if hookRunner != nil {
							hookRunner.Run(hooks.EventClose, &issue)
						}
						if jsonOutput {
							closedIssues = append(closedIssues, &issue)
						}
					}
					if !jsonOutput {
						fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), resolvedID, reason)
					}
					return nil
				}

				// Direct storage fallback
				if err := validateIssueClosable(resolvedID, directResult.Issue, force); err != nil {
					return err
				}
				if !force {
					blocked, blockers, err := directResult.Store.IsBlocked(ctx, resolvedID)
					if err != nil {
						return err
					}
					if blocked && len(blockers) > 0 {
						return fmt.Errorf("cannot close %s: blocked by open issues %v (use --force to override)", resolvedID, blockers)
					}
				}
				if err := directResult.Store.CloseIssue(ctx, resolvedID, reason, actor, session); err != nil {
					return err
				}
				closedIssue, _ := directResult.Store.GetIssue(ctx, resolvedID)
				if closedIssue != nil && hookRunner != nil {
					hookRunner.Run(hooks.EventClose, closedIssue)
				}
				if jsonOutput {
					if closedIssue != nil {
						closedIssues = append(closedIssues, closedIssue)
					}
				} else {
					fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), resolvedID, reason)
				}
				return nil
			})

			// Handle --continue flag in daemon mode (bd-ympw)
			if continueFlag && len(closedIssues) > 0 && len(resolvedIDs) == 1 {
				autoClaim := !noAuto
				continueArgs := &rpc.CloseContinueArgs{
					ClosedStepID: resolvedIDs[0],
					AutoClaim:    autoClaim,
					Actor:        actor,
				}
				continueResult, err := daemonClient.CloseContinue(continueArgs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not advance to next step: %v\n", err)
				} else if continueResult != nil {
					if jsonOutput {
						// Include continue result in JSON output
						outputJSON(map[string]interface{}{
							"closed":   closedIssues,
							"continue": continueResult,
						})
						return
					}
					PrintContinueResultFromRPC(continueResult)
				}
			}

			if jsonOutput && len(closedIssues) > 0 {
				outputJSON(closedIssues)
			}
			return
		}

		// Direct mode
		closedIssues := []*types.Issue{}
		closedCount := 0

		// Handle local IDs
		for _, id := range resolvedIDs {
			// Get issue for checks
			issue, _ := store.GetIssue(ctx, id)

			if err := validateIssueClosable(id, issue, force); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				continue
			}

			// Check if issue has open blockers (GH#962)
			if !force {
				blocked, blockers, err := store.IsBlocked(ctx, id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error checking blockers for %s: %v\n", id, err)
					continue
				}
				if blocked && len(blockers) > 0 {
					fmt.Fprintf(os.Stderr, "cannot close %s: blocked by open issues %v (use --force to override)\n", id, blockers)
					continue
				}
			}

			if err := store.CloseIssue(ctx, id, reason, actor, session); err != nil {
				fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", id, err)
				continue
			}

			closedCount++

			// Run close hook
			closedIssue, _ := store.GetIssue(ctx, id)
			if closedIssue != nil && hookRunner != nil {
				hookRunner.Run(hooks.EventClose, closedIssue)
			}

			if jsonOutput {
				if closedIssue != nil {
					closedIssues = append(closedIssues, closedIssue)
				}
			} else {
				fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), id, reason)
			}
		}

		// Handle routed IDs via centralized routing (bd-z344)
		forEachRoutedID(ctx, store, routedArgs, func(resolvedID string, routedClient *rpc.Client, directResult *RoutedResult) error {
			issueStore := directResult.Store
			issue := directResult.Issue
			// Note: in direct mode, routedClient is always nil — forEachRoutedID
			// falls through to resolveAndGetIssueWithRouting for all IDs.

			if err := validateIssueClosable(resolvedID, issue, force); err != nil {
				return err
			}
			if !force {
				blocked, blockers, err := issueStore.IsBlocked(ctx, resolvedID)
				if err != nil {
					return err
				}
				if blocked && len(blockers) > 0 {
					return fmt.Errorf("cannot close %s: blocked by open issues %v (use --force to override)", resolvedID, blockers)
				}
			}
			if err := issueStore.CloseIssue(ctx, resolvedID, reason, actor, session); err != nil {
				return err
			}
			closedCount++
			closedIssue, _ := issueStore.GetIssue(ctx, resolvedID)
			if closedIssue != nil && hookRunner != nil {
				hookRunner.Run(hooks.EventClose, closedIssue)
			}
			if jsonOutput {
				if closedIssue != nil {
					closedIssues = append(closedIssues, closedIssue)
				}
			} else {
				fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), resolvedID, reason)
			}
			return nil
		})

		// Handle --suggest-next flag in direct mode
		if suggestNext && len(resolvedIDs) == 1 && closedCount > 0 {
			unblocked, err := store.GetNewlyUnblockedByClose(ctx, resolvedIDs[0])
			if err == nil && len(unblocked) > 0 {
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"closed":    closedIssues,
						"unblocked": unblocked,
					})
					return
				}
				fmt.Printf("\nNewly unblocked:\n")
				for _, issue := range unblocked {
					fmt.Printf("  • %s %q (P%d)\n", issue.ID, issue.Title, issue.Priority)
				}
			}
		}

		// Schedule auto-flush if any issues were closed
		if len(args) > 0 {
			markDirtyAndScheduleFlush()
		}

		// Handle --continue flag
		if continueFlag && len(resolvedIDs) == 1 && closedCount > 0 {
			autoClaim := !noAuto
			result, err := AdvanceToNextStep(ctx, store, resolvedIDs[0], autoClaim, actor)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not advance to next step: %v\n", err)
			} else if result != nil {
				if jsonOutput {
					// Include continue result in JSON output
					outputJSON(map[string]interface{}{
						"closed":   closedIssues,
						"continue": result,
					})
					return
				}
				PrintContinueResult(result)
			}
		}

		if jsonOutput && len(closedIssues) > 0 {
			outputJSON(closedIssues)
		}
	},
}

// PrintContinueResultFromRPC prints the result of advancing to the next step from RPC
func PrintContinueResultFromRPC(result *rpc.CloseContinueResult) {
	if result == nil {
		return
	}

	if result.MolComplete {
		fmt.Printf("\n%s Molecule %s complete! All steps closed.\n", ui.RenderPass("✓"), result.MoleculeID)
		fmt.Println("Consider: bd mol squash " + result.MoleculeID + " --summary '...'")
		return
	}

	if result.NextStep == nil {
		fmt.Println("\nNo ready steps in molecule (may be blocked).")
		return
	}

	fmt.Printf("\nNext ready in molecule:\n")
	fmt.Printf("  %s: %s\n", result.NextStep.ID, result.NextStep.Title)

	if result.AutoAdvanced {
		fmt.Printf("\n%s Marked in_progress (use --no-auto to skip)\n", ui.RenderWarn("→"))
	} else {
		fmt.Printf("\nStart with: bd update %s --status in_progress\n", result.NextStep.ID)
	}
}

func init() {
	closeCmd.Flags().StringP("reason", "r", "", "Reason for closing")
	closeCmd.Flags().String("resolution", "", "Alias for --reason (Jira CLI convention)")
	_ = closeCmd.Flags().MarkHidden("resolution") // Hidden alias for agent/CLI ergonomics
	closeCmd.Flags().StringP("message", "m", "", "Alias for --reason (git commit convention)")
	_ = closeCmd.Flags().MarkHidden("message") // Hidden alias for agent/CLI ergonomics
	closeCmd.Flags().BoolP("force", "f", false, "Force close pinned issues")
	closeCmd.Flags().Bool("continue", false, "Auto-advance to next step in molecule")
	closeCmd.Flags().Bool("no-auto", false, "With --continue, show next step but don't claim it")
	closeCmd.Flags().Bool("suggest-next", false, "Show newly unblocked issues after closing")
	closeCmd.Flags().String("session", "", "Claude Code session ID (or set CLAUDE_SESSION_ID env var)")
	closeCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(closeCmd)
}

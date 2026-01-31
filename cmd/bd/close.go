package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
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
		compactSpec, _ := cmd.Flags().GetBool("compact-spec")
		compactSkills, _ := cmd.Flags().GetBool("compact-skills")

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

		// Resolve partial IDs first, handling cross-rig routing
		var resolvedIDs []string
		var routedArgs []string // IDs that need cross-repo routing (bypass daemon)
		if daemonClient != nil {
			for _, id := range args {
				// Check if this ID needs routing to a different beads directory
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
				resolvedIDs = append(resolvedIDs, resolvedID)
			}
		} else {
			// Direct mode - check routing for each ID
			for _, id := range args {
				if needsRouting(id) {
					routedArgs = append(routedArgs, id)
				} else {
					resolved, err := utils.ResolvePartialID(ctx, store, id)
					if err != nil {
						FatalErrorRespectJSON("resolving ID %s: %v", id, err)
					}
					resolvedIDs = append(resolvedIDs, resolved)
				}
			}
		}

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
							closedIssues = append(closedIssues, result.Closed)
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
						closedIssues = append(closedIssues, &issue)
					}
					if !jsonOutput {
						fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), id, reason)
					}
				}
			}

			// Handle routed IDs via direct mode (cross-rig)
			for _, id := range routedArgs {
				result, err := resolveAndGetIssueWithRouting(ctx, store, id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
					continue
				}
				if result == nil || result.Issue == nil {
					if result != nil {
						result.Close()
					}
					fmt.Fprintf(os.Stderr, "Issue %s not found\n", id)
					continue
				}

				if err := validateIssueClosable(result.ResolvedID, result.Issue, force); err != nil {
					result.Close()
					fmt.Fprintf(os.Stderr, "%s\n", err)
					continue
				}

				// Check if issue has open blockers (GH#962)
				if !force {
					blocked, blockers, err := result.Store.IsBlocked(ctx, result.ResolvedID)
					if err != nil {
						result.Close()
						fmt.Fprintf(os.Stderr, "Error checking blockers for %s: %v\n", id, err)
						continue
					}
					if blocked && len(blockers) > 0 {
						result.Close()
						fmt.Fprintf(os.Stderr, "cannot close %s: blocked by open issues %v (use --force to override)\n", id, blockers)
						continue
					}
				}

				if err := result.Store.CloseIssue(ctx, result.ResolvedID, reason, actor, session); err != nil {
					result.Close()
					fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", id, err)
					continue
				}

				// Get updated issue for hook
				closedIssue, _ := result.Store.GetIssue(ctx, result.ResolvedID)
				if closedIssue != nil && hookRunner != nil {
					hookRunner.Run(hooks.EventClose, closedIssue)
				}

				if closedIssue != nil {
					closedIssues = append(closedIssues, closedIssue)
				}
				if !jsonOutput {
					fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), result.ResolvedID, reason)
				}
				result.Close()
			}

			// Handle --continue flag in daemon mode
			// Note: --continue requires direct database access to walk parent-child chain
			if continueFlag && len(closedIssues) > 0 {
				fmt.Fprintf(os.Stderr, "\nNote: --continue requires direct database access\n")
				fmt.Fprintf(os.Stderr, "Hint: use --no-daemon flag: bd --no-daemon close %s --continue\n", resolvedIDs[0])
			}

			if len(closedIssues) > 0 {
				maybeAutoCompactDaemon(ctx, closedIssues, compactSpec, daemonClient)
				if compactSkills {
					compactSkillsForClosedIssues(ctx, closedIssues)
				}

				// Check for spec completion suggestions
				for _, issue := range closedIssues {
					if issue.SpecID == "" {
						continue
					}
					suggestSpecCompletion(ctx, issue.SpecID)
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

			if closedIssue != nil {
				closedIssues = append(closedIssues, closedIssue)
			}
			if !jsonOutput {
				fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), id, reason)
			}
		}

		// Handle routed IDs (cross-rig)
		for _, id := range routedArgs {
			result, err := resolveAndGetIssueWithRouting(ctx, store, id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
				continue
			}
			if result == nil || result.Issue == nil {
				if result != nil {
					result.Close()
				}
				fmt.Fprintf(os.Stderr, "Issue %s not found\n", id)
				continue
			}

			if err := validateIssueClosable(result.ResolvedID, result.Issue, force); err != nil {
				result.Close()
				fmt.Fprintf(os.Stderr, "%s\n", err)
				continue
			}

			// Check if issue has open blockers (GH#962)
			if !force {
				blocked, blockers, err := result.Store.IsBlocked(ctx, result.ResolvedID)
				if err != nil {
					result.Close()
					fmt.Fprintf(os.Stderr, "Error checking blockers for %s: %v\n", id, err)
					continue
				}
				if blocked && len(blockers) > 0 {
					result.Close()
					fmt.Fprintf(os.Stderr, "cannot close %s: blocked by open issues %v (use --force to override)\n", id, blockers)
					continue
				}
			}

			if err := result.Store.CloseIssue(ctx, result.ResolvedID, reason, actor, session); err != nil {
				result.Close()
				fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", id, err)
				continue
			}

			closedCount++

			// Get updated issue for hook
			closedIssue, _ := result.Store.GetIssue(ctx, result.ResolvedID)
			if closedIssue != nil && hookRunner != nil {
				hookRunner.Run(hooks.EventClose, closedIssue)
			}

			if closedIssue != nil {
				closedIssues = append(closedIssues, closedIssue)
			}
			if !jsonOutput {
				fmt.Printf("%s Closed %s: %s\n", ui.RenderPass("✓"), result.ResolvedID, reason)
			}
			result.Close()
		}

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

		if len(closedIssues) > 0 {
			specStore, err := getSpecRegistryStore()
			if err == nil {
				maybeAutoCompactDirect(ctx, closedIssues, compactSpec, store, specStore)
				if compactSkills {
					compactSkillsForClosedIssues(ctx, closedIssues)
				}
			}

			// Check for spec completion suggestions
			for _, issue := range closedIssues {
				if issue.SpecID == "" {
					continue
				}
				suggestSpecCompletion(ctx, issue.SpecID)
			}
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
	closeCmd.Flags().Bool("compact-spec", false, "If last linked issue closes, archive spec with auto summary")
	closeCmd.Flags().Bool("compact-skills", false, "Remove skills only used by this issue from all agents")
	closeCmd.Flags().String("session", "", "Claude Code session ID (or set CLAUDE_SESSION_ID env var)")
	closeCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(closeCmd)
}

// suggestSpecCompletion checks if all issues linked to a spec are now closed.
// If so, prints a suggestion to run `bd spec mark-done <spec_id>`.
// Only prints suggestion if not in --json mode.
func suggestSpecCompletion(ctx context.Context, specID string) {
	if specID == "" || jsonOutput {
		return
	}

	// Query for any remaining open issues with this spec_id
	openIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{
		SpecID:        &specID,
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	})
	if err != nil {
		// Silently ignore errors - this is just a suggestion
		return
	}

	// If no open issues remain, suggest marking the spec as done
	if len(openIssues) == 0 {
		fmt.Printf("\n%s All issues for spec %s are now closed.\n", ui.RenderPass("●"), specID)
		fmt.Printf("  Run: bd spec mark-done %s\n", specID)
	}
}

// compactSkillsForClosedIssues archives skills that are no longer used by any open issues.
// When an issue is closed with --compact-skills, this function checks each skill linked to
// those issues. If a skill is no longer linked to any open issue, it gets archived.
func compactSkillsForClosedIssues(ctx context.Context, closedIssues []*types.Issue) {
	if len(closedIssues) == 0 {
		return
	}

	// Need direct database access for skill operations
	if dbPath == "" {
		return
	}

	skillStore, err := sqlite.New(ctx, dbPath)
	if err != nil {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Warning: could not open store for skill compaction: %v\n", err)
		}
		return
	}
	defer func() { _ = skillStore.Close() }()

	db := skillStore.UnderlyingDB()
	if db == nil {
		return
	}

	archivedCount := 0
	for _, issue := range closedIssues {
		// Get skills linked to this issue
		rows, err := db.QueryContext(ctx, `
			SELECT skill_id FROM skill_bead_links WHERE bead_id = ?
		`, issue.ID)
		if err != nil {
			continue
		}

		var skillIDs []string
		for rows.Next() {
			var skillID string
			if scanErr := rows.Scan(&skillID); scanErr == nil {
				skillIDs = append(skillIDs, skillID)
			}
		}
		_ = rows.Close()

		// For each skill, check if it's still used by any open issues
		for _, skillID := range skillIDs {
			var openCount int
			err := db.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM skill_bead_links sbl
				JOIN issues i ON sbl.bead_id = i.id
				WHERE sbl.skill_id = ?
				  AND i.status NOT IN ('closed', 'tombstone')
			`, skillID).Scan(&openCount)
			if err != nil {
				continue
			}

			// If no open issues use this skill, archive it
			if openCount == 0 {
				_, err := db.ExecContext(ctx, `
					UPDATE skills_manifest SET status = 'archived', archived_at = ? WHERE id = ?
				`, time.Now(), skillID)
				if err == nil {
					archivedCount++
					if !jsonOutput {
						fmt.Printf("%s Archived unused skill: %s\n", ui.RenderMuted("○"), skillID)
					}
				}
			}
		}
	}

	if archivedCount > 0 && !jsonOutput {
		fmt.Printf("Compacted %d unused skills\n", archivedCount)
	}
}

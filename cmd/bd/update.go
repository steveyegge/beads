package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/util"
	"github.com/steveyegge/beads/internal/validation"
)

var updateCmd = &cobra.Command{
	Use:     "update [id...]",
	GroupID: "issues",
	Short:   "Update one or more issues",
	Long: `Update one or more issues.

If no issue ID is provided, updates the last touched issue (from most recent
create, update, show, or close operation).`,
	Args: cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("update")

		// If no IDs provided, use last touched issue
		if len(args) == 0 {
			lastTouched := GetLastTouchedID()
			if lastTouched == "" {
				FatalErrorRespectJSON("no issue ID provided and no last touched issue")
			}
			args = []string{lastTouched}
		}

		updates := make(map[string]interface{})

		if cmd.Flags().Changed("status") {
			status, _ := cmd.Flags().GetString("status")
			updates["status"] = status

			// If status is being set to closed, include session if provided
			if status == "closed" {
				session, _ := cmd.Flags().GetString("session")
				if session == "" {
					session = os.Getenv("CLAUDE_SESSION_ID")
				}
				if session != "" {
					updates["closed_by_session"] = session
				}
			}
		}
		if cmd.Flags().Changed("priority") {
			priorityStr, _ := cmd.Flags().GetString("priority")
			priority, err := validation.ValidatePriority(priorityStr)
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}
			updates["priority"] = priority
		}
		if cmd.Flags().Changed("title") {
			title, _ := cmd.Flags().GetString("title")
			updates["title"] = title
		}
		if cmd.Flags().Changed("assignee") {
			assignee, _ := cmd.Flags().GetString("assignee")
			updates["assignee"] = assignee
		}
		description, descChanged := getDescriptionFlag(cmd)
		if descChanged {
			updates["description"] = description
		}
		if cmd.Flags().Changed("design") {
			design, _ := cmd.Flags().GetString("design")
			updates["design"] = design
		}
		if cmd.Flags().Changed("notes") && cmd.Flags().Changed("append-notes") {
			FatalErrorRespectJSON("cannot specify both --notes and --append-notes")
		}
		if cmd.Flags().Changed("notes") {
			notes, _ := cmd.Flags().GetString("notes")
			updates["notes"] = notes
		}
		if cmd.Flags().Changed("append-notes") {
			appendNotes, _ := cmd.Flags().GetString("append-notes")
			updates["append_notes"] = appendNotes
		}
		if cmd.Flags().Changed("acceptance") || cmd.Flags().Changed("acceptance-criteria") {
			var acceptanceCriteria string
			if cmd.Flags().Changed("acceptance") {
				acceptanceCriteria, _ = cmd.Flags().GetString("acceptance")
			} else {
				acceptanceCriteria, _ = cmd.Flags().GetString("acceptance-criteria")
			}
			updates["acceptance_criteria"] = acceptanceCriteria
		}
		if cmd.Flags().Changed("external-ref") {
			externalRef, _ := cmd.Flags().GetString("external-ref")
			updates["external_ref"] = externalRef
		}
		if cmd.Flags().Changed("estimate") {
			estimate, _ := cmd.Flags().GetInt("estimate")
			if estimate < 0 {
				FatalErrorRespectJSON("estimate must be a non-negative number of minutes")
			}
			updates["estimated_minutes"] = estimate
		}
		if cmd.Flags().Changed("type") {
			issueType, _ := cmd.Flags().GetString("type")
			// Normalize aliases (e.g., "enhancement" -> "feature") before validating
			issueType = util.NormalizeIssueType(issueType)
			var customTypes []string
			if store != nil {
				if ct, err := store.GetCustomTypes(cmd.Context()); err == nil {
					customTypes = ct
				}
			}
			if !types.IssueType(issueType).IsValidWithCustom(customTypes) {
				validTypes := "bug, feature, task, epic, chore"
				if len(customTypes) > 0 {
					validTypes += ", " + joinStrings(customTypes, ", ")
				}
				FatalErrorRespectJSON("invalid issue type %q. Valid types: %s", issueType, validTypes)
			}
			updates["issue_type"] = issueType
		}
		if cmd.Flags().Changed("add-label") {
			addLabels, _ := cmd.Flags().GetStringSlice("add-label")
			updates["add_labels"] = addLabels
		}
		if cmd.Flags().Changed("remove-label") {
			removeLabels, _ := cmd.Flags().GetStringSlice("remove-label")
			updates["remove_labels"] = removeLabels
		}
		if cmd.Flags().Changed("set-labels") {
			setLabels, _ := cmd.Flags().GetStringSlice("set-labels")
			updates["set_labels"] = setLabels
		}
		if cmd.Flags().Changed("parent") {
			parent, _ := cmd.Flags().GetString("parent")
			updates["parent"] = parent
		}
		// Gate fields (bd-z6kw)
		if cmd.Flags().Changed("await-id") {
			awaitID, _ := cmd.Flags().GetString("await-id")
			updates["await_id"] = awaitID
		}
		// Time-based scheduling flags (GH#820)
		if cmd.Flags().Changed("due") {
			dueStr, _ := cmd.Flags().GetString("due")
			if dueStr == "" {
				// Empty string clears the due date
				updates["due_at"] = nil
			} else {
				t, err := timeparsing.ParseRelativeTime(dueStr, time.Now())
				if err != nil {
					FatalErrorRespectJSON("invalid --due format %q. Examples: +6h, tomorrow, next monday, 2025-01-15", dueStr)
				}
				updates["due_at"] = t
			}
		}
		if cmd.Flags().Changed("defer") {
			deferStr, _ := cmd.Flags().GetString("defer")
			if deferStr == "" {
				// Empty string clears the defer_until
				updates["defer_until"] = nil
			} else {
				t, err := timeparsing.ParseRelativeTime(deferStr, time.Now())
				if err != nil {
					FatalErrorRespectJSON("invalid --defer format %q. Examples: +1h, tomorrow, next monday, 2025-01-15", deferStr)
				}
				// Warn if defer date is in the past (user probably meant future)
				if t.Before(time.Now()) && !jsonOutput {
					fmt.Fprintf(os.Stderr, "%s Defer date %q is in the past. Issue will appear in bd ready immediately.\n",
						ui.RenderWarn("!"), t.Format("2006-01-02 15:04"))
					fmt.Fprintf(os.Stderr, "  Did you mean a future date? Use --defer=+1h or --defer=tomorrow\n")
				}
				updates["defer_until"] = t
			}
		}
		// Ephemeral/persistent flags
		// Note: storage layer uses "wisp" field name, maps to "ephemeral" column
		ephemeralChanged := cmd.Flags().Changed("ephemeral")
		persistentChanged := cmd.Flags().Changed("persistent")
		if ephemeralChanged && persistentChanged {
			FatalErrorRespectJSON("cannot specify both --ephemeral and --persistent flags")
		}
		if ephemeralChanged {
			updates["wisp"] = true
		}
		if persistentChanged {
			updates["wisp"] = false
		}
		// Advice subscription flags (gt-w2mh8a.6)
		if cmd.Flags().Changed("advice-subscriptions") {
			adviceSubs, _ := cmd.Flags().GetString("advice-subscriptions")
			if adviceSubs == "" {
				updates["advice_subscriptions"] = []string{}
			} else {
				updates["advice_subscriptions"] = splitCommaSeparated(adviceSubs)
			}
		}
		if cmd.Flags().Changed("advice-subscriptions-exclude") {
			adviceExclude, _ := cmd.Flags().GetString("advice-subscriptions-exclude")
			if adviceExclude == "" {
				updates["advice_subscriptions_exclude"] = []string{}
			} else {
				updates["advice_subscriptions_exclude"] = splitCommaSeparated(adviceExclude)
			}
		}

		// Get claim flag
		claimFlag, _ := cmd.Flags().GetBool("claim")

		if len(updates) == 0 && !claimFlag {
			fmt.Println("No updates specified")
			return
		}

		ctx := rootCtx

		// Resolve partial IDs, splitting into local vs routed (bd-z344)
		batch, err := resolveIDsWithRouting(ctx, store, daemonClient, args)
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		resolvedIDs := batch.ResolvedIDs
		routedArgs := batch.RoutedArgs

		// If daemon is running, use RPC
		if daemonClient != nil {
			updatedIssues := []*types.Issue{}
			var firstUpdatedID string // Track first successful update for last-touched
			for _, id := range resolvedIDs {
				updateArgs := buildUpdateArgs(id, updates, claimFlag, cmd)

				// Handle append_notes: fetch existing issue notes via daemon
				if appendNotes, ok := updates["append_notes"].(string); ok {
					showResp, showErr := daemonClient.Show(&rpc.ShowArgs{ID: id})
					if showErr == nil {
						var existingIssue types.Issue
						if json.Unmarshal(showResp.Data, &existingIssue) == nil {
							combined := existingIssue.Notes
							if combined != "" {
								combined += "\n"
							}
							combined += appendNotes
							updateArgs.Notes = &combined
						}
					}
				}

				resp, err := daemonClient.Update(updateArgs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error updating %s: %v\n", id, err)
					continue
				}

				var issue types.Issue
				if err := json.Unmarshal(resp.Data, &issue); err == nil {
					// Run update hook
					if hookRunner != nil {
						hookRunner.Run(hooks.EventUpdate, &issue)
					}
					if jsonOutput {
						updatedIssues = append(updatedIssues, &issue)
					}
				}
				if !jsonOutput {
					fmt.Printf("%s Updated issue: %s\n", ui.RenderPass("✓"), id)
				}

				// Track first successful update for last-touched
				if firstUpdatedID == "" {
					firstUpdatedID = id
				}
			}

			// Handle routed IDs via centralized routing (bd-z344)
			forEachRoutedID(ctx, store, routedArgs, func(resolvedID string, routedClient *rpc.Client, directResult *RoutedResult) error {
				if routedClient != nil {
					routedClient.SetActor(actor)
					updateArgs := buildUpdateArgs(resolvedID, updates, claimFlag, cmd)
					// Handle append_notes via RPC Show to get existing notes
					if appendNotes, ok := updates["append_notes"].(string); ok {
						showResp, err := routedClient.Show(&rpc.ShowArgs{ID: resolvedID})
						if err == nil {
							var existingIssue types.Issue
							if json.Unmarshal(showResp.Data, &existingIssue) == nil {
								combined := existingIssue.Notes
								if combined != "" {
									combined += "\n"
								}
								combined += appendNotes
								updateArgs.Notes = &combined
							}
						}
					}
					resp, updateErr := routedClient.Update(updateArgs)
					routedClient.Close()
					if updateErr != nil {
						return updateErr
					}
					var issue types.Issue
					if json.Unmarshal(resp.Data, &issue) == nil {
						if hookRunner != nil {
							hookRunner.Run(hooks.EventUpdate, &issue)
						}
						if jsonOutput {
							updatedIssues = append(updatedIssues, &issue)
						}
					}
					if !jsonOutput {
						fmt.Printf("%s Updated issue: %s\n", ui.RenderPass("✓"), resolvedID)
					}
					if firstUpdatedID == "" {
						firstUpdatedID = resolvedID
					}
					return nil
				}

				// Direct storage fallback
				issue := directResult.Issue
				issueStore := directResult.Store
				if err := validateIssueUpdatable(resolvedID, issue); err != nil {
					return err
				}
				if claimFlag {
					if issue.Assignee != "" {
						return fmt.Errorf("already claimed by %s", issue.Assignee)
					}
					claimUpdates := map[string]interface{}{
						"assignee": actor,
						"status":   "in_progress",
					}
					if err := issueStore.UpdateIssue(ctx, resolvedID, claimUpdates, actor); err != nil {
						return err
					}
				}
				if err := applyDirectUpdates(ctx, issueStore, resolvedID, issue, updates, actor); err != nil {
					return err
				}
				updatedIssue, _ := issueStore.GetIssue(ctx, resolvedID)
				if updatedIssue != nil && hookRunner != nil {
					hookRunner.Run(hooks.EventUpdate, updatedIssue)
				}
				if jsonOutput {
					if updatedIssue != nil {
						updatedIssues = append(updatedIssues, updatedIssue)
					}
				} else {
					fmt.Printf("%s Updated issue: %s\n", ui.RenderPass("✓"), resolvedID)
				}
				if firstUpdatedID == "" {
					firstUpdatedID = resolvedID
				}
				return nil
			})

			if jsonOutput && len(updatedIssues) > 0 {
				outputJSON(updatedIssues)
			}

			// Set last touched after all updates complete
			if firstUpdatedID != "" {
				SetLastTouchedID(firstUpdatedID)
			}
			return
		}

		// Direct mode - use routed resolution for cross-repo lookups
		updatedIssues := []*types.Issue{}
		var firstUpdatedID string // Track first successful update for last-touched
		for _, id := range args {
			// Resolve and get issue with routing (e.g., gt-xyz routes to gastown)
			result, err := resolveAndGetIssueWithRouting(ctx, store, id)
			if err != nil {
				if result != nil {
					result.Close()
				}
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
			issue := result.Issue
			issueStore := result.Store

			if err := validateIssueUpdatable(id, issue); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				result.Close()
				continue
			}

			// Handle claim operation atomically
			if claimFlag {
				if issue.Assignee != "" {
					fmt.Fprintf(os.Stderr, "Error claiming %s: already claimed by %s\n", id, issue.Assignee)
					result.Close()
					continue
				}
				claimUpdates := map[string]interface{}{
					"assignee": actor,
					"status":   "in_progress",
				}
				if err := issueStore.UpdateIssue(ctx, result.ResolvedID, claimUpdates, actor); err != nil {
					fmt.Fprintf(os.Stderr, "Error claiming %s: %v\n", id, err)
					result.Close()
					continue
				}
			}

			// Apply field updates, labels, and parent reparenting via centralized helper (bd-z344)
			if err := applyDirectUpdates(ctx, issueStore, result.ResolvedID, issue, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Error updating %s: %v\n", id, err)
				result.Close()
				continue
			}

			updatedIssue, _ := issueStore.GetIssue(ctx, result.ResolvedID)
			if updatedIssue != nil && hookRunner != nil {
				hookRunner.Run(hooks.EventUpdate, updatedIssue)
			}

			if jsonOutput {
				if updatedIssue != nil {
					updatedIssues = append(updatedIssues, updatedIssue)
				}
			} else {
				fmt.Printf("%s Updated issue: %s\n", ui.RenderPass("✓"), result.ResolvedID)
			}

			if firstUpdatedID == "" {
				firstUpdatedID = result.ResolvedID
			}
			result.Close()
		}

		// Set last touched after all updates complete
		if firstUpdatedID != "" {
			SetLastTouchedID(firstUpdatedID)
		}

		// Schedule auto-flush if any issues were updated
		if len(args) > 0 {
			markDirtyAndScheduleFlush()
		}

		if jsonOutput && len(updatedIssues) > 0 {
			outputJSON(updatedIssues)
		}
	},
}

func init() {
	updateCmd.Flags().StringP("status", "s", "", "New status")
	registerPriorityFlag(updateCmd, "")
	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().StringP("type", "t", "", "New type (bug|feature|task|epic|chore|merge-request|molecule|gate|agent|role|rig|convoy|event|slot)")
	registerCommonIssueFlags(updateCmd)
	updateCmd.Flags().String("acceptance-criteria", "", "DEPRECATED: use --acceptance")
	_ = updateCmd.Flags().MarkHidden("acceptance-criteria") // Only fails if flag missing (caught in tests)
	updateCmd.Flags().IntP("estimate", "e", 0, "Time estimate in minutes (e.g., 60 for 1 hour)")
	updateCmd.Flags().StringSlice("add-label", nil, "Add labels (repeatable)")
	updateCmd.Flags().StringSlice("remove-label", nil, "Remove labels (repeatable)")
	updateCmd.Flags().StringSlice("set-labels", nil, "Set labels, replacing all existing (repeatable)")
	updateCmd.Flags().String("parent", "", "New parent issue ID (reparents the issue, use empty string to remove parent)")
	updateCmd.Flags().Bool("claim", false, "Atomically claim the issue (sets assignee to you, status to in_progress; fails if already claimed)")
	updateCmd.Flags().String("session", "", "Claude Code session ID for status=closed (or set CLAUDE_SESSION_ID env var)")
	// Time-based scheduling flags (GH#820)
	// Examples:
	//   --due=+6h           Due in 6 hours
	//   --due=tomorrow      Due tomorrow
	//   --due="next monday" Due next Monday
	//   --due=2025-01-15    Due on specific date
	//   --due=""            Clear due date
	//   --defer=+1h         Hidden from bd ready for 1 hour
	//   --defer=""          Clear defer (show in bd ready immediately)
	updateCmd.Flags().String("due", "", "Due date/time (empty to clear). Formats: +6h, +1d, +2w, tomorrow, next monday, 2025-01-15")
	updateCmd.Flags().String("defer", "", "Defer until date (empty to clear). Issue hidden from bd ready until then")
	// Gate fields (bd-z6kw)
	updateCmd.Flags().String("await-id", "", "Set gate await_id (e.g., GitHub run ID for gh:run gates)")
	// Ephemeral/persistent flags
	updateCmd.Flags().Bool("ephemeral", false, "Mark issue as ephemeral (wisp) - not exported to JSONL")
	updateCmd.Flags().Bool("persistent", false, "Mark issue as persistent (promote wisp to regular issue)")
	// Advice subscription flags (gt-w2mh8a.6)
	updateCmd.Flags().String("advice-subscriptions", "", "Comma-separated labels to subscribe to for advice delivery (empty to clear)")
	updateCmd.Flags().String("advice-subscriptions-exclude", "", "Comma-separated labels to exclude from advice delivery (empty to clear)")
	updateCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(updateCmd)
}

// buildUpdateArgs creates an rpc.UpdateArgs from the updates map, mapping all
// fields except append_notes (which needs RPC Show to get existing notes).
// This centralizes the map→RPC mapping used by both local and routed daemon paths.
func buildUpdateArgs(id string, updates map[string]interface{}, claimFlag bool, cmd *cobra.Command) *rpc.UpdateArgs {
	updateArgs := &rpc.UpdateArgs{ID: id}
	if status, ok := updates["status"].(string); ok {
		updateArgs.Status = &status
	}
	if priority, ok := updates["priority"].(int); ok {
		updateArgs.Priority = &priority
	}
	if title, ok := updates["title"].(string); ok {
		updateArgs.Title = &title
	}
	if assignee, ok := updates["assignee"].(string); ok {
		updateArgs.Assignee = &assignee
	}
	if description, ok := updates["description"].(string); ok {
		updateArgs.Description = &description
	}
	if design, ok := updates["design"].(string); ok {
		updateArgs.Design = &design
	}
	if notes, ok := updates["notes"].(string); ok {
		updateArgs.Notes = &notes
	}
	if acceptanceCriteria, ok := updates["acceptance_criteria"].(string); ok {
		updateArgs.AcceptanceCriteria = &acceptanceCriteria
	}
	if externalRef, ok := updates["external_ref"].(string); ok {
		updateArgs.ExternalRef = &externalRef
	}
	if estimate, ok := updates["estimated_minutes"].(int); ok {
		updateArgs.EstimatedMinutes = &estimate
	}
	if issueType, ok := updates["issue_type"].(string); ok {
		updateArgs.IssueType = &issueType
	}
	if addLabels, ok := updates["add_labels"].([]string); ok {
		updateArgs.AddLabels = addLabels
	}
	if removeLabels, ok := updates["remove_labels"].([]string); ok {
		updateArgs.RemoveLabels = removeLabels
	}
	if setLabels, ok := updates["set_labels"].([]string); ok {
		updateArgs.SetLabels = setLabels
	}
	if parent, ok := updates["parent"].(string); ok {
		updateArgs.Parent = &parent
	}
	if awaitID, ok := updates["await_id"].(string); ok {
		updateArgs.AwaitID = &awaitID
	}
	if dueAt, ok := updates["due_at"].(time.Time); ok {
		s := dueAt.Format(time.RFC3339)
		updateArgs.DueAt = &s
	} else if updates["due_at"] == nil && cmd.Flags().Changed("due") {
		empty := ""
		updateArgs.DueAt = &empty
	}
	if deferUntil, ok := updates["defer_until"].(time.Time); ok {
		s := deferUntil.Format(time.RFC3339)
		updateArgs.DeferUntil = &s
	} else if updates["defer_until"] == nil && cmd.Flags().Changed("defer") {
		empty := ""
		updateArgs.DeferUntil = &empty
	}
	if wisp, ok := updates["wisp"].(bool); ok {
		updateArgs.Ephemeral = &wisp
	}
	if adviceSubs, ok := updates["advice_subscriptions"].([]string); ok {
		updateArgs.AdviceSubscriptions = adviceSubs
	}
	if adviceExclude, ok := updates["advice_subscriptions_exclude"].([]string); ok {
		updateArgs.AdviceSubscriptionsExclude = adviceExclude
	}
	updateArgs.Claim = claimFlag
	return updateArgs
}

// applyDirectUpdates applies field updates, label changes, and parent reparenting
// directly to storage. Used by both the direct mode main loop and the routed
// direct-storage fallback path.
func applyDirectUpdates(ctx context.Context, issueStore storage.Storage, resolvedID string, issue *types.Issue, updates map[string]interface{}, actorName string) error {
	// Apply regular field updates
	regularUpdates := make(map[string]interface{})
	for k, v := range updates {
		if k != "add_labels" && k != "remove_labels" && k != "set_labels" && k != "parent" && k != "append_notes" {
			regularUpdates[k] = v
		}
	}
	if appendNotes, ok := updates["append_notes"].(string); ok {
		combined := issue.Notes
		if combined != "" {
			combined += "\n"
		}
		combined += appendNotes
		regularUpdates["notes"] = combined
	}
	if len(regularUpdates) > 0 {
		if err := issueStore.UpdateIssue(ctx, resolvedID, regularUpdates, actorName); err != nil {
			return err
		}
	}

	// Apply label operations
	var setLabels, addLabels, removeLabels []string
	if v, ok := updates["set_labels"].([]string); ok {
		setLabels = v
	}
	if v, ok := updates["add_labels"].([]string); ok {
		addLabels = v
	}
	if v, ok := updates["remove_labels"].([]string); ok {
		removeLabels = v
	}
	if len(setLabels) > 0 || len(addLabels) > 0 || len(removeLabels) > 0 {
		if err := applyLabelUpdates(ctx, issueStore, resolvedID, actorName, setLabels, addLabels, removeLabels); err != nil {
			return err
		}
	}

	// Handle parent reparenting
	if newParent, ok := updates["parent"].(string); ok {
		if newParent != "" {
			parentIssue, err := issueStore.GetIssue(ctx, newParent)
			if err != nil {
				return fmt.Errorf("getting parent %s: %w", newParent, err)
			}
			if parentIssue == nil {
				return fmt.Errorf("parent issue %s not found", newParent)
			}
		}
		deps, err := issueStore.GetDependencyRecords(ctx, resolvedID)
		if err != nil {
			return fmt.Errorf("getting dependencies for %s: %w", resolvedID, err)
		}
		for _, dep := range deps {
			if dep.Type == types.DepParentChild {
				if err := issueStore.RemoveDependency(ctx, resolvedID, dep.DependsOnID, actorName); err != nil {
					fmt.Fprintf(os.Stderr, "Error removing old parent dependency: %v\n", err)
				}
				break
			}
		}
		if newParent != "" {
			newDep := &types.Dependency{
				IssueID:     resolvedID,
				DependsOnID: newParent,
				Type:        types.DepParentChild,
			}
			if err := issueStore.AddDependency(ctx, newDep, actorName); err != nil {
				return fmt.Errorf("adding parent dependency: %w", err)
			}
		}
	}

	return nil
}

// splitCommaSeparated splits a comma-separated string into a slice,
// trimming whitespace from each element.
func splitCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

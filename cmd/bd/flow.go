package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var (
	flowClaimParent    string
	flowClaimLabels    []string
	flowClaimLabelsAny []string
	flowClaimLimit     int
	flowClaimPriority  int

	flowCreateTitle       string
	flowCreateFromID      string
	flowCreateDescription string
	flowCreateIssueType   string
	flowCreatePriority    int
	flowCreateLabels      []string

	flowBlockIssueID     string
	flowBlockContextPack string
	flowBlockerID        string

	flowCloseIssueID             string
	flowCloseReason              string
	flowCloseVerificationEntries []string
	flowCloseNotes               []string
	flowCloseAllowFailureReason  bool
)

var flowCmd = &cobra.Command{
	Use:     "flow",
	GroupID: "issues",
	Short:   "Deterministic lifecycle flow commands",
	Long: `Deterministic lifecycle commands for claim/execute/verify/close workflows.

These commands return machine-readable JSON envelopes with deterministic result
states and explicit exit codes.`,
}

var flowClaimNextCmd = &cobra.Command{
	Use:   "claim-next",
	Short: "Claim next ready issue with WIP=1 gate",
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("flow claim-next")
		ctx := rootCtx

		claimActor := strings.TrimSpace(actor)
		if claimActor == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow claim-next",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": "actor is required (use --actor or set BD_ACTOR/BEADS_ACTOR)",
				},
				Events: []string{"claim_skipped"},
			}, 1)
			return
		}

		var resolvedParent *string
		if strings.TrimSpace(flowClaimParent) != "" {
			pid, err := utils.ResolvePartialID(ctx, store, flowClaimParent)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow claim-next",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to resolve parent ID %q: %v", flowClaimParent, err),
					},
					Events: []string{"claim_skipped"},
				}, 1)
				return
			}
			resolvedParent = &pid
		}

		statusInProgress := types.StatusInProgress
		wipFilter := types.IssueFilter{
			Status:   &statusInProgress,
			Assignee: &claimActor,
			Limit:    20,
		}
		currentWIP, err := store.SearchIssues(ctx, "", wipFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow claim-next",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("wip gate query failed: %v", err)},
				Events:  []string{"claim_skipped"},
			}, 1)
			return
		}
		if len(currentWIP) > 0 {
			wipIDs := make([]string, 0, len(currentWIP))
			for _, issue := range currentWIP {
				wipIDs = append(wipIDs, issue.ID)
			}
			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "flow claim-next",
				Result:  "wip_blocked",
				Details: map[string]interface{}{
					"actor":           claimActor,
					"in_progress_ids": wipIDs,
					"message":         "WIP gate blocked claim",
				},
				Events: []string{"claim_skipped"},
			}, 0)
			return
		}

		labels := utils.NormalizeLabels(flowClaimLabels)
		labelsAny := utils.NormalizeLabels(flowClaimLabelsAny)
		workFilter := types.WorkFilter{
			Status:    types.StatusOpen,
			Limit:     flowClaimLimit,
			Labels:    labels,
			LabelsAny: labelsAny,
			ParentID:  resolvedParent,
		}
		if cmd.Flags().Changed("priority") {
			priority := flowClaimPriority
			workFilter.Priority = &priority
		}

		candidates, err := store.GetReadyWork(ctx, workFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow claim-next",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("ready query failed: %v", err)},
				Events:  []string{"claim_skipped"},
			}, 1)
			return
		}
		if len(candidates) == 0 {
			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "flow claim-next",
				Result:  "no_ready",
				Details: map[string]interface{}{
					"actor":   claimActor,
					"message": "No ready work available",
				},
				Events: []string{"claim_skipped"},
			}, 0)
			return
		}

		contentionIDs := make([]string, 0)
		claimErrors := make([]string, 0)
		for _, candidate := range candidates {
			if candidate.Status != types.StatusOpen {
				continue
			}

			err := store.ClaimIssue(ctx, candidate.ID, claimActor)
			if err == nil {
				claimedIssue, getErr := store.GetIssue(ctx, candidate.ID)
				if getErr != nil || claimedIssue == nil {
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow claim-next",
						Result:  "system_error",
						IssueID: candidate.ID,
						Details: map[string]interface{}{
							"message": fmt.Sprintf("claim succeeded but issue reload failed: %v", getErr),
						},
						Events: []string{"claim_failed_reload"},
					}, 1)
					return
				}

				finishEnvelope(commandEnvelope{
					OK:      true,
					Command: "flow claim-next",
					Result:  "claimed",
					IssueID: claimedIssue.ID,
					Details: map[string]interface{}{
						"actor": claimActor,
						"issue": compactIssue(claimedIssue),
					},
					Events: []string{"claimed"},
				}, 0)
				return
			}

			if errors.Is(err, storage.ErrAlreadyClaimed) {
				contentionIDs = append(contentionIDs, candidate.ID)
				continue
			}
			claimErrors = append(claimErrors, fmt.Sprintf("%s: %v", candidate.ID, err))
		}

		if len(contentionIDs) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "flow claim-next",
				Result:  "contention",
				Details: map[string]interface{}{
					"actor":            claimActor,
					"contention_ids":   contentionIDs,
					"non_claim_errors": claimErrors,
					"message":          "Ready issues were contended during claim",
				},
				Events: []string{"claim_contention"},
			}, 0)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "flow claim-next",
			Result:  "system_error",
			Details: map[string]interface{}{
				"actor":   claimActor,
				"errors":  claimErrors,
				"message": "No claim succeeded due to non-contention errors",
			},
			Events: []string{"claim_failed"},
		}, 1)
	},
}

var flowCreateDiscoveredCmd = &cobra.Command{
	Use:   "create-discovered",
	Short: "Create discovered issue and link discovered-from dependency",
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("flow create-discovered")
		ctx := rootCtx

		if strings.TrimSpace(flowCreateTitle) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--title is required"},
				Events:  []string{"create_skipped"},
			}, 1)
			return
		}
		if strings.TrimSpace(flowCreateFromID) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--from is required"},
				Events:  []string{"create_skipped"},
			}, 1)
			return
		}

		fromResult, err := resolveAndGetIssueWithRouting(ctx, store, flowCreateFromID)
		if err != nil {
			if fromResult != nil {
				fromResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("failed to resolve --from issue %q: %v", flowCreateFromID, err),
				},
				Events: []string{"create_skipped"},
			}, 1)
			return
		}
		if fromResult == nil || fromResult.Issue == nil {
			if fromResult != nil {
				fromResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("--from issue %q not found", flowCreateFromID)},
				Events:  []string{"create_skipped"},
			}, 1)
			return
		}
		defer fromResult.Close()

		if fromResult.Routed {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": "--from must resolve to an issue in the current workspace",
				},
				Events: []string{"create_skipped"},
			}, 1)
			return
		}

		issueType, err := normalizeFlowIssueType(ctx, flowCreateIssueType)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"create_skipped"},
			}, 1)
			return
		}

		created := &types.Issue{
			Title:       strings.TrimSpace(flowCreateTitle),
			Description: flowCreateDescription,
			Status:      types.StatusOpen,
			Priority:    flowCreatePriority,
			IssueType:   issueType,
		}
		if created.IssueType == "" {
			created.IssueType = types.TypeTask
		}

		if err := store.CreateIssue(ctx, created, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "system_error",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("create failed: %v", err),
				},
				Events: []string{"create_failed"},
			}, 1)
			return
		}

		labels := utils.NormalizeLabels(flowCreateLabels)
		if len(labels) > 0 {
			if err := applyLabelUpdates(ctx, store, created.ID, actor, nil, labels, nil); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow create-discovered",
					Result:  "partial_state",
					IssueID: created.ID,
					Details: map[string]interface{}{
						"partial_state": "issue_created_without_requested_labels",
						"labels":        labels,
						"error":         err.Error(),
					},
					RecoveryCommand: fmt.Sprintf("bd update %s --add-label %s", created.ID, strings.Join(labels, " --add-label ")),
					Events:          []string{"created", "label_apply_failed"},
				}, exitCodePartialState)
				return
			}
		}

		link := &types.Dependency{
			IssueID:     created.ID,
			DependsOnID: fromResult.ResolvedID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := store.AddDependency(ctx, link, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow create-discovered",
				Result:  "partial_state",
				IssueID: created.ID,
				Details: map[string]interface{}{
					"partial_state":    "issue_created_without_discovered_from_link",
					"depends_on_id":    fromResult.ResolvedID,
					"dependency_error": err.Error(),
				},
				RecoveryCommand: fmt.Sprintf("bd dep add %s %s --type discovered-from", created.ID, fromResult.ResolvedID),
				Events:          []string{"created", "dependency_add_failed"},
			}, exitCodePartialState)
			return
		}

		createdIssue, _ := store.GetIssue(ctx, created.ID)
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow create-discovered",
			Result:  "created",
			IssueID: created.ID,
			Details: map[string]interface{}{
				"created": compactIssue(createdIssue),
				"from":    fromResult.ResolvedID,
			},
			Events: []string{"created", "dependency_added"},
		}, 0)
	},
}

var flowBlockWithContextCmd = &cobra.Command{
	Use:   "block-with-context",
	Short: "Block issue with context pack and optional blocker dependency",
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("flow block-with-context")
		ctx := rootCtx

		if strings.TrimSpace(flowBlockIssueID) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow block-with-context",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"block_skipped"},
			}, 1)
			return
		}
		if strings.TrimSpace(flowBlockContextPack) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow block-with-context",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--context-pack is required"},
				Events:  []string{"block_skipped"},
			}, 1)
			return
		}

		issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowBlockIssueID)
		if err != nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow block-with-context",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("failed to resolve issue %q: %v", flowBlockIssueID, err),
				},
				Events: []string{"block_skipped"},
			}, 1)
			return
		}
		if issueResult == nil || issueResult.Issue == nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow block-with-context",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("issue %q not found", flowBlockIssueID)},
				Events:  []string{"block_skipped"},
			}, 1)
			return
		}
		defer issueResult.Close()

		var blockerResolvedID string
		if strings.TrimSpace(flowBlockerID) != "" {
			blockerResult, err := resolveAndGetIssueWithRouting(ctx, store, flowBlockerID)
			if err != nil {
				if blockerResult != nil {
					blockerResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow block-with-context",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to resolve blocker %q: %v", flowBlockerID, err),
					},
					Events: []string{"block_skipped"},
				}, 1)
				return
			}
			if blockerResult == nil || blockerResult.Issue == nil {
				if blockerResult != nil {
					blockerResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow block-with-context",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("blocker issue %q not found", flowBlockerID),
					},
					Events: []string{"block_skipped"},
				}, 1)
				return
			}
			if issueResult.Store != blockerResult.Store {
				blockerResult.Close()
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow block-with-context",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": "issue and blocker must be in the same workspace for blocks dependency",
					},
					Events: []string{"block_skipped"},
				}, 1)
				return
			}
			blockerResolvedID = blockerResult.ResolvedID
			blockerResult.Close()
		}

		mergedNotes := appendNotesLine(issueResult.Issue.Notes, "Context pack: "+strings.TrimSpace(flowBlockContextPack))
		updates := map[string]interface{}{
			"status": types.StatusBlocked,
			"notes":  mergedNotes,
		}
		if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, updates, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow block-with-context",
				Result:  "system_error",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{
					"message": fmt.Sprintf("failed to update issue status/notes: %v", err),
				},
				Events: []string{"block_update_failed"},
			}, 1)
			return
		}

		if blockerResolvedID != "" {
			dep := &types.Dependency{
				IssueID:     issueResult.ResolvedID,
				DependsOnID: blockerResolvedID,
				Type:        types.DepBlocks,
			}
			if err := issueResult.Store.AddDependency(ctx, dep, actor); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow block-with-context",
					Result:  "partial_state",
					IssueID: issueResult.ResolvedID,
					Details: map[string]interface{}{
						"partial_state":    "issue_blocked_without_blocker_link",
						"depends_on_id":    blockerResolvedID,
						"dependency_error": err.Error(),
					},
					RecoveryCommand: fmt.Sprintf("bd dep add %s %s --type blocks", issueResult.ResolvedID, blockerResolvedID),
					Events:          []string{"blocked", "dependency_add_failed"},
				}, exitCodePartialState)
				return
			}
		}

		updatedIssue, _ := issueResult.Store.GetIssue(ctx, issueResult.ResolvedID)
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow block-with-context",
			Result:  "blocked",
			IssueID: issueResult.ResolvedID,
			Details: map[string]interface{}{
				"issue":        compactIssue(updatedIssue),
				"blocker_id":   blockerResolvedID,
				"context_pack": flowBlockContextPack,
			},
			Events: []string{"blocked"},
		}, 0)
	},
}

var flowCloseSafeCmd = &cobra.Command{
	Use:   "close-safe",
	Short: "Close issue with close-reason lint and verification evidence",
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("flow close-safe")
		ctx := rootCtx

		if strings.TrimSpace(flowCloseIssueID) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"close_skipped"},
			}, 1)
			return
		}
		if strings.TrimSpace(flowCloseReason) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "policy_violation",
				Details: map[string]interface{}{"message": "--reason is required"},
				Events:  []string{"close_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		verificationEntries := make([]string, 0, len(flowCloseVerificationEntries))
		for _, entry := range flowCloseVerificationEntries {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				verificationEntries = append(verificationEntries, entry)
			}
		}
		if len(verificationEntries) == 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "policy_violation",
				Details: map[string]interface{}{"message": "at least one --verified entry is required"},
				Events:  []string{"close_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		if err := lintCloseReason(flowCloseReason, flowCloseAllowFailureReason); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "policy_violation",
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"close_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		result, err := resolveAndGetIssueWithRouting(ctx, store, flowCloseIssueID)
		if err != nil {
			if result != nil {
				result.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve issue %q: %v", flowCloseIssueID, err)},
				Events:  []string{"close_skipped"},
			}, 1)
			return
		}
		if result == nil || result.Issue == nil {
			if result != nil {
				result.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("issue %q not found", flowCloseIssueID)},
				Events:  []string{"close_skipped"},
			}, 1)
			return
		}
		defer result.Close()

		if err := validateIssueClosable(result.ResolvedID, result.Issue, false); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "policy_violation",
				IssueID: result.ResolvedID,
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"close_skipped"},
			}, exitCodePolicyViolation)
			return
		}
		if err := checkGateSatisfaction(result.Issue); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "policy_violation",
				IssueID: result.ResolvedID,
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"close_skipped"},
			}, exitCodePolicyViolation)
			return
		}
		blocked, blockers, err := result.Store.IsBlocked(ctx, result.ResolvedID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "system_error",
				IssueID: result.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("blocker check failed: %v", err)},
				Events:  []string{"close_skipped"},
			}, 1)
			return
		}
		if blocked && len(blockers) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "policy_violation",
				IssueID: result.ResolvedID,
				Details: map[string]interface{}{
					"message":  "issue is blocked by open dependencies",
					"blockers": blockers,
				},
				Events: []string{"close_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		notes := result.Issue.Notes
		for _, entry := range verificationEntries {
			notes = appendNotesLine(notes, "Verified: "+entry)
		}
		for _, note := range flowCloseNotes {
			if strings.TrimSpace(note) != "" {
				notes = appendNotesLine(notes, note)
			}
		}
		if err := result.Store.UpdateIssue(ctx, result.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "system_error",
				IssueID: result.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to append verification notes: %v", err)},
				Events:  []string{"close_skipped"},
			}, 1)
			return
		}

		session := os.Getenv("CLAUDE_SESSION_ID")
		if err := result.Store.CloseIssue(ctx, result.ResolvedID, flowCloseReason, actor, session); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow close-safe",
				Result:  "system_error",
				IssueID: result.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("close failed: %v", err)},
				Events:  []string{"close_failed"},
			}, 1)
			return
		}

		closedIssue, _ := result.Store.GetIssue(ctx, result.ResolvedID)
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow close-safe",
			Result:  "closed",
			IssueID: result.ResolvedID,
			Details: map[string]interface{}{
				"issue":                compactIssue(closedIssue),
				"verification_entries": verificationEntries,
			},
			Events: []string{"verified_note_appended", "closed"},
		}, 0)
	},
}

func normalizeFlowIssueType(ctx context.Context, raw string) (types.IssueType, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = string(types.TypeTask)
	}
	normalized = utils.NormalizeIssueType(normalized)

	customTypes, err := store.GetCustomTypes(ctx)
	if err != nil {
		customTypes = config.GetCustomTypesFromYAML()
	}
	issueType := types.IssueType(normalized)
	if !issueType.IsValidWithCustom(customTypes) {
		validTypes := "bug, feature, task, epic, chore, decision"
		if len(customTypes) > 0 {
			validTypes += ", " + joinStrings(customTypes, ", ")
		}
		return "", fmt.Errorf("invalid issue type %q. Valid types: %s", normalized, validTypes)
	}
	return issueType, nil
}

func compactIssue(issue *types.Issue) map[string]interface{} {
	if issue == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":       issue.ID,
		"title":    issue.Title,
		"status":   issue.Status,
		"priority": issue.Priority,
	}
}

func init() {
	flowClaimNextCmd.Flags().StringVar(&flowClaimParent, "parent", "", "Filter ready queue by parent epic/issue")
	flowClaimNextCmd.Flags().StringSliceVar(&flowClaimLabels, "label", nil, "AND label filter (repeat flag)")
	flowClaimNextCmd.Flags().StringSliceVar(&flowClaimLabelsAny, "label-any", nil, "OR label filter (repeat flag)")
	flowClaimNextCmd.Flags().IntVar(&flowClaimPriority, "priority", 0, "Priority filter (0-4)")
	flowClaimNextCmd.Flags().IntVar(&flowClaimLimit, "limit", 10, "Maximum ready candidates to scan")
	flowClaimNextCmd.ValidArgsFunction = noCompletions

	flowCreateDiscoveredCmd.Flags().StringVar(&flowCreateTitle, "title", "", "Title for discovered issue")
	flowCreateDiscoveredCmd.Flags().StringVar(&flowCreateFromID, "from", "", "Source issue ID for discovered-from link")
	flowCreateDiscoveredCmd.Flags().StringVar(&flowCreateDescription, "description", "", "Issue description")
	flowCreateDiscoveredCmd.Flags().StringVar(&flowCreateIssueType, "type", string(types.TypeTask), "Issue type")
	flowCreateDiscoveredCmd.Flags().IntVar(&flowCreatePriority, "priority", 2, "Priority (0-4)")
	flowCreateDiscoveredCmd.Flags().StringSliceVar(&flowCreateLabels, "label", nil, "Labels to attach (repeat flag)")
	flowCreateDiscoveredCmd.ValidArgsFunction = noCompletions

	flowBlockWithContextCmd.Flags().StringVar(&flowBlockIssueID, "issue", "", "Issue ID to block")
	flowBlockWithContextCmd.Flags().StringVar(&flowBlockContextPack, "context-pack", "", "Context pack note text")
	flowBlockWithContextCmd.Flags().StringVar(&flowBlockerID, "blocker", "", "Optional blocker issue ID")
	flowBlockWithContextCmd.ValidArgsFunction = noCompletions

	flowCloseSafeCmd.Flags().StringVar(&flowCloseIssueID, "issue", "", "Issue ID to close")
	flowCloseSafeCmd.Flags().StringVar(&flowCloseReason, "reason", "", "Close reason")
	flowCloseSafeCmd.Flags().StringArrayVar(&flowCloseVerificationEntries, "verified", nil, "Verification evidence entry (repeat flag)")
	flowCloseSafeCmd.Flags().StringArrayVar(&flowCloseNotes, "note", nil, "Additional close notes (repeat flag)")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseAllowFailureReason, "allow-failure-reason", false, "Allow failed: close reasons")
	flowCloseSafeCmd.ValidArgsFunction = noCompletions

	flowCmd.AddCommand(flowClaimNextCmd)
	flowCmd.AddCommand(flowCreateDiscoveredCmd)
	flowCmd.AddCommand(flowBlockWithContextCmd)
	flowCmd.AddCommand(flowCloseSafeCmd)
	rootCmd.AddCommand(flowCmd)
}

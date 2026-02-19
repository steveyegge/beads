package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var (
	flowStateFrom                 string
	flowStateTo                   string
	flowClaimParent               string
	flowClaimLabels               []string
	flowClaimLabelsAny            []string
	flowClaimLimit                int
	flowClaimPriority             int
	flowClaimRequireAnchor        bool
	flowClaimAllowMissingAnchor   bool
	flowClaimAnchorLabel          string
	flowPreclaimIssue             string
	flowBaselineIssue             string
	flowBaselineCmd               string
	flowPriorityPollIssue         string
	flowSupersedeIssueID          string
	flowSupersedeIDs              []string
	flowSupersedeReason           string
	flowRollbackIssueID           string
	flowRollbackTitle             string
	flowRollbackVerifyCmd         string
	flowTransitionType            string
	flowTransitionIssueID         string
	flowTransitionBlocker         string
	flowTransitionContext         string
	flowTransitionReason          string
	flowTransitionAbortMD         string
	flowTransitionNoWrite         bool
	flowTransitionAttempt         int
	flowTransitionMaxRetry        int
	flowTransitionBackoff         string
	flowTransitionEscalate        string
	flowTransitionDecompThreshold int

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
	flowCloseRequireEvidence     bool
	flowCloseEvidenceMaxAge      string
	flowCloseRequireSpecDrift    bool
	flowCloseAllowSecretMarkers  bool
	flowCloseRequireTraceability bool
	flowCloseRequireParentCheck  bool
	flowCloseAllowOpenChildren   bool
	flowCloseForce               bool
	flowCloseRequirePriorityPoll bool
	flowClosePriorityPollMaxAge  string
	flowCloseNonHermetic         bool
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
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
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
		requireAnchor := effectiveRequireAnchor(flowClaimRequireAnchor, flowClaimAllowMissingAnchor)
		if requireAnchor {
			if resolvedParent == nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow claim-next",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message": "--require-anchor requires --parent (strict-control defaults can be bypassed with --allow-missing-anchor)",
					},
					Events: []string{"claim_skipped"},
				}, exitCodePolicyViolation)
				return
			}
			anchorLabel := resolveAnchorLabel(flowClaimAnchorLabel, flowClaimLabels)
			if anchorLabel == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow claim-next",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message": "--require-anchor requires --anchor-label or a module/* label",
					},
					Events: []string{"claim_skipped"},
				}, exitCodePolicyViolation)
				return
			}
			statusPinned := types.StatusPinned
			filter := types.IssueFilter{
				Status:   &statusPinned,
				ParentID: resolvedParent,
				Labels:   []string{anchorLabel},
				Limit:    20,
			}
			anchors, err := store.SearchIssues(ctx, "", filter)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow claim-next",
					Result:  "system_error",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("anchor guard query failed: %v", err),
					},
					Events: []string{"claim_skipped"},
				}, 1)
				return
			}
			if anchorCardinalityViolation(len(anchors)) {
				anchorIDs := make([]string, 0, len(anchors))
				for _, a := range anchors {
					anchorIDs = append(anchorIDs, a.ID)
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow claim-next",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message":      "anchor cardinality must be exactly one",
						"anchor_label": anchorLabel,
						"anchor_count": len(anchors),
						"anchor_ids":   anchorIDs,
					},
					RecoveryCommand: fmt.Sprintf("bd list --status pinned --label %s --parent %s --json", anchorLabel, *resolvedParent),
					Events:          []string{"claim_skipped"},
				}, exitCodePolicyViolation)
				return
			}
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
		intakeGateBlocked := make([]string, 0)
		for _, candidate := range candidates {
			if candidate.Status != types.StatusOpen {
				continue
			}
			parentIssue, parentErr := resolveParentForTraceability(ctx, candidate.ID)
			if parentErr != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow claim-next",
					Result:  "system_error",
					IssueID: candidate.ID,
					Details: map[string]interface{}{
						"message": fmt.Sprintf("intake claim-gate lookup failed: %v", parentErr),
					},
					Events: []string{"claim_skipped"},
				}, 1)
				return
			}
			if parentIssue != nil {
				planCount := extractPlanCountFromNotes(parentIssue.Notes)
				if intakeClaimGateRequired(planCount) && !intakeAuditPassed(parentIssue.Notes) {
					intakeGateBlocked = append(intakeGateBlocked, candidate.ID)
					continue
				}
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
				blockedNow, blockersNow, blockedErr := store.IsBlocked(ctx, claimedIssue.ID)
				if blockedErr != nil {
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow claim-next",
						Result:  "partial_state",
						IssueID: claimedIssue.ID,
						Details: map[string]interface{}{
							"partial_state": "claimed_without_viability_assessment",
							"message":       fmt.Sprintf("claim succeeded but blocker viability check failed: %v", blockedErr),
						},
						RecoveryCommand: fmt.Sprintf("bd dep tree %s --direction up", claimedIssue.ID),
						Events:          []string{"claimed", "viability_check_failed"},
					}, exitCodePartialState)
					return
				}
				deps, depsErr := store.GetDependenciesWithMetadata(ctx, claimedIssue.ID)
				if depsErr != nil {
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow claim-next",
						Result:  "partial_state",
						IssueID: claimedIssue.ID,
						Details: map[string]interface{}{
							"partial_state": "claimed_without_deferred_blocker_scan",
							"message":       fmt.Sprintf("claim succeeded but deferred-blocker scan failed: %v", depsErr),
						},
						RecoveryCommand: fmt.Sprintf("bd dep tree %s --direction up", claimedIssue.ID),
						Events:          []string{"claimed", "viability_check_failed"},
					}, exitCodePartialState)
					return
				}
				deferredBlockerIDs := detectDeferredBlockerIDs(deps)
				viability := summarizePostClaimViability(blockedNow, blockersNow, deferredBlockerIDs)

				finishEnvelope(commandEnvelope{
					OK:      true,
					Command: "flow claim-next",
					Result:  "claimed",
					IssueID: claimedIssue.ID,
					Details: map[string]interface{}{
						"actor":                claimActor,
						"issue":                compactIssue(claimedIssue),
						"post_claim_viability": viability,
						"blocker_ids":          uniqueSortedStrings(blockersNow),
						"deferred_blocker_ids": deferredBlockerIDs,
					},
					Events: []string{"claimed", "post_claim_viability_checked"},
				}, 0)
				return
			}

			if errors.Is(err, storage.ErrAlreadyClaimed) {
				contentionIDs = append(contentionIDs, candidate.ID)
				continue
			}
			claimErrors = append(claimErrors, fmt.Sprintf("%s: %v", candidate.ID, err))
		}

		if len(intakeGateBlocked) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow claim-next",
				Result:  "policy_violation",
				Details: map[string]interface{}{
					"message":                 "intake hard gate blocked claim (INTAKE_AUDIT=PASS required)",
					"intake_gate_blocked_ids": intakeGateBlocked,
				},
				RecoveryCommand: "bd intake audit --epic <parent-id> --write-proof",
				Events:          []string{"claim_skipped"},
			}, exitCodePolicyViolation)
			return
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

var flowPreclaimLintCmd = &cobra.Command{
	Use:   "preclaim-lint",
	Short: "Run deterministic pre-claim quality lint for one issue",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
		ctx := rootCtx

		if strings.TrimSpace(flowPreclaimIssue) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow preclaim-lint",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"preclaim_lint_failed"},
			}, 1)
			return
		}

		issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowPreclaimIssue)
		if err != nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow preclaim-lint",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("failed to resolve issue %q: %v", flowPreclaimIssue, err),
				},
				Events: []string{"preclaim_lint_failed"},
			}, 1)
			return
		}
		if issueResult == nil || issueResult.Issue == nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow preclaim-lint",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("issue %q not found", flowPreclaimIssue),
				},
				Events: []string{"preclaim_lint_failed"},
			}, 1)
			return
		}
		defer issueResult.Close()

		deps, err := issueResult.Store.GetDependenciesWithMetadata(ctx, issueResult.ResolvedID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow preclaim-lint",
				Result:  "system_error",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to inspect dependencies: %v", err)},
				Events:  []string{"preclaim_lint_failed"},
			}, 1)
			return
		}

		violations := collectPreclaimViolations(issueResult.Issue, deps)
		if len(violations) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow preclaim-lint",
				Result:  "policy_violation",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{
					"violations": violations,
					"message":    "pre-claim lint failed",
				},
				RecoveryCommand: fmt.Sprintf("bd show %s --json", issueResult.ResolvedID),
				Events:          []string{"preclaim_lint_failed"},
			}, exitCodePolicyViolation)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow preclaim-lint",
			Result:  "pass",
			IssueID: issueResult.ResolvedID,
			Details: map[string]interface{}{
				"issue": compactIssue(issueResult.Issue),
			},
			Events: []string{"preclaim_lint_passed"},
		}, 0)
	},
}

var flowBaselineVerifyCmd = &cobra.Command{
	Use:   "baseline-verify",
	Short: "Run deterministic baseline verify and no-code-close eligibility check",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
		ctx := rootCtx

		if strings.TrimSpace(flowBaselineIssue) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow baseline-verify",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"baseline_skipped"},
			}, 1)
			return
		}
		if strings.TrimSpace(flowBaselineCmd) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow baseline-verify",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--cmd is required"},
				Events:  []string{"baseline_skipped"},
			}, 1)
			return
		}

		issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowBaselineIssue)
		if err != nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow baseline-verify",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("failed to resolve issue %q: %v", flowBaselineIssue, err),
				},
				Events: []string{"baseline_skipped"},
			}, 1)
			return
		}
		if issueResult == nil || issueResult.Issue == nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow baseline-verify",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("issue %q not found", flowBaselineIssue),
				},
				Events: []string{"baseline_skipped"},
			}, 1)
			return
		}
		defer issueResult.Close()

		output, runErr := runBaselineCommand(flowBaselineCmd)
		state, noCodeEligible, classifyErr := baselineDecisionFromError(runErr)
		if classifyErr != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow baseline-verify",
				Result:  "system_error",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{"message": classifyErr.Error()},
				Events:  []string{"baseline_failed"},
			}, 1)
			return
		}

		note := fmt.Sprintf(
			"Baseline %s: %s -> %s",
			strings.ToUpper(state),
			strings.TrimSpace(flowBaselineCmd),
			compactBaselineOutput(output, runErr),
		)
		notes := appendNotesLine(issueResult.Issue.Notes, note)
		if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow baseline-verify",
				Result:  "system_error",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to append baseline note: %v", err)},
				Events:  []string{"baseline_failed"},
			}, 1)
			return
		}

		result := "baseline_fail"
		if state == "pass" {
			result = "baseline_pass"
		}
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow baseline-verify",
			Result:  result,
			IssueID: issueResult.ResolvedID,
			Details: map[string]interface{}{
				"baseline_state":         state,
				"no_code_close_eligible": noCodeEligible,
			},
			Events: []string{"baseline_recorded"},
		}, 0)
	},
}

var flowPriorityPollCmd = &cobra.Command{
	Use:   "priority-poll",
	Short: "Run deterministic P0 queue poll and optionally record poll evidence on an issue",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
		ctx := rootCtx

		priority := 0
		p0Issues, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:   types.StatusOpen,
			Priority: &priority,
			Limit:    1,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow priority-poll",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("priority poll failed: %v", err)},
				Events:  []string{"priority_poll_failed"},
			}, 1)
			return
		}

		p0IDs := make([]string, 0, len(p0Issues))
		for _, issue := range p0Issues {
			p0IDs = append(p0IDs, issue.ID)
		}
		polledAt := time.Now().UTC()
		noteAppended := false

		if strings.TrimSpace(flowPriorityPollIssue) != "" {
			issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowPriorityPollIssue)
			if err != nil {
				if issueResult != nil {
					issueResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow priority-poll",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve issue %q: %v", flowPriorityPollIssue, err)},
					Events:  []string{"priority_poll_failed"},
				}, 1)
				return
			}
			if issueResult == nil || issueResult.Issue == nil {
				if issueResult != nil {
					issueResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow priority-poll",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("issue %q not found", flowPriorityPollIssue)},
					Events:  []string{"priority_poll_failed"},
				}, 1)
				return
			}

			notes := appendPriorityPollNote(issueResult.Issue.Notes, polledAt, p0IDs)
			if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
				issueResult.Close()
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow priority-poll",
					Result:  "system_error",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to append priority poll note: %v", err)},
					Events:  []string{"priority_poll_failed"},
				}, 1)
				return
			}
			issueResult.Close()
			noteAppended = true
		}

		result := "priority_poll_clear"
		if len(p0IDs) > 0 {
			result = "priority_poll_p0_present"
		}
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow priority-poll",
			Result:  result,
			IssueID: strings.TrimSpace(flowPriorityPollIssue),
			Details: map[string]interface{}{
				"polled_at":     polledAt.Format(time.RFC3339),
				"p0_issue_ids":  p0IDs,
				"note_appended": noteAppended,
			},
			Events: []string{"priority_polled"},
		}, 0)
	},
}

var flowSupersedeCoarseCmd = &cobra.Command{
	Use:   "supersede-coarse",
	Short: "Apply deterministic supersession protocol for coarse tasks",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
		CheckReadonly("flow supersede-coarse")
		ctx := rootCtx

		issueID := strings.TrimSpace(flowSupersedeIssueID)
		if issueID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"supersede_skipped"},
			}, 1)
			return
		}
		replacementIDs := uniqueSortedStrings(flowSupersedeIDs)
		if len(replacementIDs) == 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "at least one --replacement is required"},
				Events:  []string{"supersede_skipped"},
			}, 1)
			return
		}

		coarseResult, err := resolveAndGetIssueWithRouting(ctx, store, issueID)
		if err != nil {
			if coarseResult != nil {
				coarseResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve issue %q: %v", issueID, err)},
				Events:  []string{"supersede_skipped"},
			}, 1)
			return
		}
		if coarseResult == nil || coarseResult.Issue == nil {
			if coarseResult != nil {
				coarseResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("issue %q not found", issueID)},
				Events:  []string{"supersede_skipped"},
			}, 1)
			return
		}
		defer coarseResult.Close()
		if coarseResult.Issue.Status == types.StatusClosed {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "policy_violation",
				IssueID: coarseResult.ResolvedID,
				Details: map[string]interface{}{"message": "coarse issue is already closed"},
				Events:  []string{"supersede_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		parentIssue, err := resolveParentForTraceability(ctx, coarseResult.ResolvedID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "system_error",
				IssueID: coarseResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve coarse parent: %v", err)},
				Events:  []string{"supersede_failed"},
			}, 1)
			return
		}
		parentID := ""
		if parentIssue != nil {
			parentID = parentIssue.ID
		}

		violations := make([]string, 0)
		for _, replacementID := range replacementIDs {
			if replacementID == coarseResult.ResolvedID {
				violations = append(violations, fmt.Sprintf("%s:self_reference", replacementID))
				continue
			}
			replacementResult, err := resolveAndGetIssueWithRouting(ctx, store, replacementID)
			if err != nil {
				violations = append(violations, fmt.Sprintf("%s:resolve_failed", replacementID))
				if replacementResult != nil {
					replacementResult.Close()
				}
				continue
			}
			if replacementResult == nil || replacementResult.Issue == nil {
				violations = append(violations, fmt.Sprintf("%s:not_found", replacementID))
				if replacementResult != nil {
					replacementResult.Close()
				}
				continue
			}
			if replacementResult.Issue.Status == types.StatusClosed {
				violations = append(violations, fmt.Sprintf("%s:closed", replacementID))
				replacementResult.Close()
				continue
			}
			if parentID != "" {
				replacementDeps, depsErr := replacementResult.Store.GetDependenciesWithMetadata(ctx, replacementResult.ResolvedID)
				if depsErr != nil {
					violations = append(violations, fmt.Sprintf("%s:deps_failed", replacementID))
					replacementResult.Close()
					continue
				}
				if !hasParentChildToID(replacementDeps, parentID) {
					violations = append(violations, fmt.Sprintf("%s:missing_parent_child_%s", replacementID, parentID))
				}
			}
			replacementResult.Close()
		}
		if len(violations) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "policy_violation",
				IssueID: coarseResult.ResolvedID,
				Details: map[string]interface{}{
					"message":    "supersession protocol checks failed",
					"violations": violations,
				},
				Events: []string{"supersede_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		note := "Superseded by: " + strings.Join(replacementIDs, ",")
		notes := appendNotesLine(coarseResult.Issue.Notes, note)
		if err := coarseResult.Store.UpdateIssue(ctx, coarseResult.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "system_error",
				IssueID: coarseResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to append supersession notes: %v", err)},
				Events:  []string{"supersede_failed"},
			}, 1)
			return
		}

		reason := strings.TrimSpace(flowSupersedeReason)
		if reason == "" {
			reason = fmt.Sprintf("Refactored coarse task into atomic replacements (%s); verified by supersession protocol checks", strings.Join(replacementIDs, ","))
		}
		if err := lintCloseReason(reason, false); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "policy_violation",
				IssueID: coarseResult.ResolvedID,
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"supersede_skipped"},
			}, exitCodePolicyViolation)
			return
		}

		session := os.Getenv("CLAUDE_SESSION_ID")
		if err := coarseResult.Store.CloseIssue(ctx, coarseResult.ResolvedID, reason, actor, session); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow supersede-coarse",
				Result:  "system_error",
				IssueID: coarseResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to close superseded issue: %v", err)},
				Events:  []string{"supersede_failed"},
			}, 1)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow supersede-coarse",
			Result:  "superseded",
			IssueID: coarseResult.ResolvedID,
			Details: map[string]interface{}{
				"replacement_ids": replacementIDs,
				"close_reason":    reason,
			},
			Events: []string{"superseded"},
		}, 0)
	},
}

var flowExecutionRollbackCmd = &cobra.Command{
	Use:   "execution-rollback",
	Short: "Create deterministic corrective task for a previously closed/incorrect issue",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
		CheckReadonly("flow execution-rollback")
		ctx := rootCtx

		if strings.TrimSpace(flowRollbackIssueID) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow execution-rollback",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"execution_rollback_failed"},
			}, 1)
			return
		}
		verifyCmd := strings.TrimSpace(flowRollbackVerifyCmd)
		if verifyCmd == "" {
			verifyCmd = "go test ./cmd/bd -count=1"
		}

		originalResult, err := resolveAndGetIssueWithRouting(ctx, store, flowRollbackIssueID)
		if err != nil {
			if originalResult != nil {
				originalResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow execution-rollback",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve original issue %q: %v", flowRollbackIssueID, err)},
				Events:  []string{"execution_rollback_failed"},
			}, 1)
			return
		}
		if originalResult == nil || originalResult.Issue == nil {
			if originalResult != nil {
				originalResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow execution-rollback",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("original issue %q not found", flowRollbackIssueID)},
				Events:  []string{"execution_rollback_failed"},
			}, 1)
			return
		}
		defer originalResult.Close()

		parentIssue, err := resolveParentForTraceability(ctx, originalResult.ResolvedID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow execution-rollback",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve original parent: %v", err)},
				Events:  []string{"execution_rollback_failed"},
			}, 1)
			return
		}

		title := strings.TrimSpace(flowRollbackTitle)
		if title == "" {
			title = fmt.Sprintf("Fix: rollback correction for %s", originalResult.ResolvedID)
		}
		corrective := &types.Issue{
			Title:              title,
			Description:        buildExecutionRollbackDescription(originalResult.ResolvedID, originalResult.Issue.Title, verifyCmd),
			AcceptanceCriteria: buildExecutionRollbackAcceptance(originalResult.ResolvedID),
			Status:             types.StatusOpen,
			Priority:           originalResult.Issue.Priority,
			IssueType:          types.TypeTask,
			Labels:             utils.NormalizeLabels(originalResult.Issue.Labels),
		}
		if err := originalResult.Store.CreateIssue(ctx, corrective, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow execution-rollback",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to create corrective issue: %v", err)},
				Events:  []string{"execution_rollback_failed"},
			}, 1)
			return
		}

		if parentIssue != nil {
			parentDep := &types.Dependency{
				IssueID:     corrective.ID,
				DependsOnID: parentIssue.ID,
				Type:        types.DepParentChild,
			}
			if err := originalResult.Store.AddDependency(ctx, parentDep, actor); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow execution-rollback",
					Result:  "partial_state",
					IssueID: corrective.ID,
					Details: map[string]interface{}{
						"partial_state": "corrective_created_without_parent_child_link",
						"message":       err.Error(),
					},
					RecoveryCommand: fmt.Sprintf("bd dep add %s %s --type parent-child", corrective.ID, parentIssue.ID),
					Events:          []string{"execution_rollback_created", "execution_rollback_partial"},
				}, exitCodePartialState)
				return
			}
		}

		discovered := &types.Dependency{
			IssueID:     corrective.ID,
			DependsOnID: originalResult.ResolvedID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := originalResult.Store.AddDependency(ctx, discovered, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow execution-rollback",
				Result:  "partial_state",
				IssueID: corrective.ID,
				Details: map[string]interface{}{
					"partial_state": "corrective_created_without_discovered_from_link",
					"message":       err.Error(),
				},
				RecoveryCommand: fmt.Sprintf("bd dep add %s %s --type discovered-from", corrective.ID, originalResult.ResolvedID),
				Events:          []string{"execution_rollback_created", "execution_rollback_partial"},
			}, exitCodePartialState)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow execution-rollback",
			Result:  "corrective_created",
			IssueID: corrective.ID,
			Details: map[string]interface{}{
				"original_issue_id": originalResult.ResolvedID,
				"corrective_issue":  compactIssue(corrective),
			},
			Events: []string{"execution_rollback_created", "dependency_added"},
		}, 0)
	},
}

var flowTransitionCmd = &cobra.Command{
	Use:   "transition",
	Short: "Apply deterministic transition handler primitive",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
		ctx := rootCtx

		transitionType := normalizeTransitionType(flowTransitionType)
		if !isSupportedTransitionType(transitionType) {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow transition",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message":         "unsupported --type",
					"supported_types": supportedTransitionTypes(),
				},
				Events: []string{"transition_skipped"},
			}, 1)
			return
		}
		if transitionRequiresIssue(transitionType) && strings.TrimSpace(flowTransitionIssueID) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow transition",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message":         "--issue is required for transition type",
					"transition_type": transitionType,
				},
				Events: []string{"transition_skipped"},
			}, 1)
			return
		}
		if !(transitionType == "session_abort" && flowTransitionNoWrite) {
			CheckReadonly("flow transition")
		}

		switch transitionType {
		case "claim_failed":
			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "flow transition",
				Result:  "claim_failed",
				IssueID: strings.TrimSpace(flowTransitionIssueID),
				Details: map[string]interface{}{
					"transition_type": transitionType,
					"message":         "claim failed; select next ready issue",
				},
				RecoveryCommand: "bd ready --limit 5",
				Events:          []string{"claim_failed", "ready_requeue"},
			}, 0)
			return

		case "transient_failure":
			contextPack := strings.TrimSpace(transitionContextOrReason(flowTransitionContext, flowTransitionReason))
			if contextPack == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--context or --reason is required for transient_failure",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}

			if flowTransitionMaxRetry <= 0 {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--max-attempts must be > 0",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}
			if flowTransitionAttempt <= 0 {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--attempt must be > 0",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}

			backoffs, err := parseTransientBackoffSchedule(flowTransitionBackoff, flowTransitionMaxRetry)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         err.Error(),
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}
			escalateType, err := resolveTransientEscalationType(flowTransitionEscalate)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         err.Error(),
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}

			if flowTransitionAttempt < flowTransitionMaxRetry {
				issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowTransitionIssueID)
				if err != nil {
					if issueResult != nil {
						issueResult.Close()
					}
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow transition",
						Result:  "invalid_input",
						Details: map[string]interface{}{
							"message": fmt.Sprintf("failed to resolve issue %q: %v", flowTransitionIssueID, err),
						},
						Events: []string{"transition_failed"},
					}, 1)
					return
				}
				if issueResult == nil || issueResult.Issue == nil {
					if issueResult != nil {
						issueResult.Close()
					}
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow transition",
						Result:  "invalid_input",
						Details: map[string]interface{}{
							"message": fmt.Sprintf("issue %q not found", flowTransitionIssueID),
						},
						Events: []string{"transition_failed"},
					}, 1)
					return
				}
				defer issueResult.Close()

				delay := backoffs[flowTransitionAttempt-1]
				note := fmt.Sprintf(
					"Transient failure: %s; attempt %d/%d; next backoff: %s",
					contextPack,
					flowTransitionAttempt,
					flowTransitionMaxRetry,
					delay.String(),
				)
				notes := appendNotesLine(issueResult.Issue.Notes, note)
				if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow transition",
						Result:  "system_error",
						IssueID: issueResult.ResolvedID,
						Details: map[string]interface{}{"message": fmt.Sprintf("failed to append transient failure note: %v", err)},
						Events:  []string{"transition_failed"},
					}, 1)
					return
				}

				finishEnvelope(commandEnvelope{
					OK:      true,
					Command: "flow transition",
					Result:  "transient_retry_scheduled",
					IssueID: issueResult.ResolvedID,
					Details: map[string]interface{}{
						"transition_type": transitionType,
						"attempt":         flowTransitionAttempt,
						"max_attempts":    flowTransitionMaxRetry,
						"next_backoff":    delay.String(),
						"escalate_to":     escalateType,
					},
					Events: []string{"transient_failure_recorded", "retry_scheduled"},
				}, 0)
				return
			}

			escalationNote := fmt.Sprintf(
				"transient retries exhausted; %s; attempt %d/%d; escalation: %s",
				contextPack,
				flowTransitionAttempt,
				flowTransitionMaxRetry,
				escalateType,
			)
			if escalateType == "test_failed" {
				applyBlockedTransition(ctx, "transient_failure_escalated", "FAIL: "+escalationNote, false)
			} else {
				applyBlockedTransition(ctx, "transient_failure_escalated", "Context pack: "+escalationNote, false)
			}
			return

		case "decomposition_invalid":
			contextPack := strings.TrimSpace(transitionContextOrReason(flowTransitionContext, flowTransitionReason))
			if contextPack == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--context or --reason is required for decomposition_invalid",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}
			if flowTransitionDecompThreshold == 0 {
				flowTransitionDecompThreshold = 3
			}

			issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowTransitionIssueID)
			if err != nil {
				if issueResult != nil {
					issueResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to resolve issue %q: %v", flowTransitionIssueID, err),
					},
					Events: []string{"transition_failed"},
				}, 1)
				return
			}
			if issueResult == nil || issueResult.Issue == nil {
				if issueResult != nil {
					issueResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("issue %q not found", flowTransitionIssueID),
					},
					Events: []string{"transition_failed"},
				}, 1)
				return
			}
			defer issueResult.Close()

			attempt := nextDecompositionInvalidAttempt(issueResult.Issue.Notes)
			note := fmt.Sprintf("Decomposition invalid (attempt %d): %s", attempt, contextPack)
			notes := appendNotesLine(issueResult.Issue.Notes, note)
			if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{
				"status": types.StatusBlocked,
				"notes":  notes,
			}, actor); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "system_error",
					IssueID: issueResult.ResolvedID,
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to record decomposition-invalid attempt: %v", err)},
					Events:  []string{"transition_failed"},
				}, 1)
				return
			}

			escalate := decompositionDamperEscalationRequired(attempt, flowTransitionDecompThreshold)
			resultName := "decomposition_replan_required"
			exitCode := 0
			recovery := "bd ready --limit 5"
			events := []string{"decomposition_invalid_recorded", "status_blocked"}
			if escalate {
				resultName = "decomposition_escalation_required"
				exitCode = exitCodePolicyViolation
				recovery = "Escalate using Decision Request format"
				events = append(events, "decomposition_escalation_required")
			}

			finishEnvelope(commandEnvelope{
				OK:      !escalate,
				Command: "flow transition",
				Result:  resultName,
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{
					"transition_type": transitionType,
					"attempt":         attempt,
					"threshold":       flowTransitionDecompThreshold,
					"context":         contextPack,
				},
				RecoveryCommand: recovery,
				Events:          events,
			}, exitCode)
			return

		case "claim_became_blocked":
			note := "Blocked by: " + transitionContextOrReason(flowTransitionContext, flowTransitionReason)
			applyBlockedTransition(ctx, transitionType, note, false)
			return

		case "exec_blocked":
			contextPack := strings.TrimSpace(transitionContextOrReason(flowTransitionContext, flowTransitionReason))
			if contextPack == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--context or --reason is required for exec_blocked",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}
			applyBlockedTransition(ctx, transitionType, "Context pack: "+contextPack, false)
			return

		case "test_failed":
			contextPack := strings.TrimSpace(transitionContextOrReason(flowTransitionContext, flowTransitionReason))
			if contextPack == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--context or --reason is required for test_failed",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}
			applyBlockedTransition(ctx, transitionType, "FAIL: "+contextPack, false)
			return

		case "conditional_fallback_activate":
			reason := normalizeFailureCloseReason(flowTransitionReason)
			if strings.TrimSpace(reason) == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--reason is required for conditional_fallback_activate",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}
			if err := lintCloseReason(reason, true); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message":         err.Error(),
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, exitCodePolicyViolation)
				return
			}

			issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowTransitionIssueID)
			if err != nil {
				if issueResult != nil {
					issueResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to resolve issue %q: %v", flowTransitionIssueID, err),
					},
					Events: []string{"transition_failed"},
				}, 1)
				return
			}
			if issueResult == nil || issueResult.Issue == nil {
				if issueResult != nil {
					issueResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("issue %q not found", flowTransitionIssueID),
					},
					Events: []string{"transition_failed"},
				}, 1)
				return
			}
			defer issueResult.Close()

			decisionNote := buildConditionalFallbackDecisionNote(strings.TrimSpace(flowTransitionContext))
			notes := appendNotesLine(issueResult.Issue.Notes, decisionNote)
			if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "system_error",
					IssueID: issueResult.ResolvedID,
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to append decision audit note: %v", err)},
					Events:  []string{"transition_failed"},
				}, 1)
				return
			}

			session := os.Getenv("CLAUDE_SESSION_ID")
			if err := issueResult.Store.CloseIssue(ctx, issueResult.ResolvedID, reason, actor, session); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "system_error",
					IssueID: issueResult.ResolvedID,
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to close fallback blocker: %v", err)},
					Events:  []string{"transition_failed"},
				}, 1)
				return
			}

			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "flow transition",
				Result:  "fallback_activated",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{
					"transition_type": transitionType,
					"close_reason":    reason,
				},
				Events: []string{"decision_audited", "fallback_activated"},
			}, 0)
			return

		case "session_abort":
			reason := strings.TrimSpace(flowTransitionReason)
			if reason == "" {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "invalid_input",
					Details: map[string]interface{}{
						"message":         "--reason is required for session_abort",
						"transition_type": transitionType,
					},
					Events: []string{"transition_skipped"},
				}, 1)
				return
			}

			abortPath := strings.TrimSpace(flowTransitionAbortMD)
			if abortPath == "" {
				abortPath = "ABORT_HANDOFF.md"
			}
			doc := buildAbortHandoffMarkdown(reason, strings.TrimSpace(flowTransitionIssueID), strings.TrimSpace(flowTransitionContext))
			if err := os.WriteFile(abortPath, []byte(doc), 0o644); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow transition",
					Result:  "system_error",
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to write abort handoff file %q: %v", abortPath, err),
					},
					Events: []string{"transition_failed"},
				}, 1)
				return
			}

			events := []string{"abort_handoff_written"}
			resolvedIssueID := strings.TrimSpace(flowTransitionIssueID)
			issueMutated := false
			if !flowTransitionNoWrite && resolvedIssueID != "" {
				issueResult, err := resolveAndGetIssueWithRouting(ctx, store, resolvedIssueID)
				if err != nil {
					if issueResult != nil {
						issueResult.Close()
					}
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow transition",
						Result:  "invalid_input",
						Details: map[string]interface{}{
							"message": fmt.Sprintf("failed to resolve issue %q: %v", resolvedIssueID, err),
						},
						Events: []string{"transition_failed"},
					}, 1)
					return
				}
				if issueResult == nil || issueResult.Issue == nil {
					if issueResult != nil {
						issueResult.Close()
					}
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow transition",
						Result:  "invalid_input",
						Details: map[string]interface{}{
							"message": fmt.Sprintf("issue %q not found", resolvedIssueID),
						},
						Events: []string{"transition_failed"},
					}, 1)
					return
				}
				defer issueResult.Close()

				contextPack := buildSessionAbortContextPack(reason, strings.TrimSpace(flowTransitionContext))
				notes := appendNotesLine(issueResult.Issue.Notes, contextPack)
				if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{
					"status": types.StatusBlocked,
					"notes":  notes,
				}, actor); err != nil {
					finishEnvelope(commandEnvelope{
						OK:      false,
						Command: "flow transition",
						Result:  "system_error",
						IssueID: issueResult.ResolvedID,
						Details: map[string]interface{}{"message": fmt.Sprintf("failed to append session-abort context pack: %v", err)},
						Events:  []string{"transition_failed"},
					}, 1)
					return
				}
				issueMutated = true
				resolvedIssueID = issueResult.ResolvedID
				events = append(events, "issue_blocked")
			}

			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "flow transition",
				Result:  "session_aborted",
				IssueID: resolvedIssueID,
				Details: map[string]interface{}{
					"transition_type": transitionType,
					"abort_handoff":   abortPath,
					"issue_mutated":   issueMutated,
				},
				RecoveryCommand: fmt.Sprintf("cat %s", abortPath),
				Events:          events,
			}, 0)
			return
		}
	},
}

func applyBlockedTransition(ctx context.Context, transitionType, noteLine string, requireBlocker bool) {
	issueResult, err := resolveAndGetIssueWithRouting(ctx, store, flowTransitionIssueID)
	if err != nil {
		if issueResult != nil {
			issueResult.Close()
		}
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "flow transition",
			Result:  "invalid_input",
			Details: map[string]interface{}{
				"message": fmt.Sprintf("failed to resolve issue %q: %v", flowTransitionIssueID, err),
			},
			Events: []string{"transition_failed"},
		}, 1)
		return
	}
	if issueResult == nil || issueResult.Issue == nil {
		if issueResult != nil {
			issueResult.Close()
		}
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "flow transition",
			Result:  "invalid_input",
			Details: map[string]interface{}{
				"message": fmt.Sprintf("issue %q not found", flowTransitionIssueID),
			},
			Events: []string{"transition_failed"},
		}, 1)
		return
	}
	defer issueResult.Close()

	blockerResolvedID := ""
	if strings.TrimSpace(flowTransitionBlocker) != "" {
		blockerResult, err := resolveAndGetIssueWithRouting(ctx, store, flowTransitionBlocker)
		if err != nil {
			if blockerResult != nil {
				blockerResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow transition",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("failed to resolve blocker %q: %v", flowTransitionBlocker, err),
				},
				Events: []string{"transition_failed"},
			}, 1)
			return
		}
		if blockerResult == nil || blockerResult.Issue == nil {
			if blockerResult != nil {
				blockerResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow transition",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": fmt.Sprintf("blocker issue %q not found", flowTransitionBlocker),
				},
				Events: []string{"transition_failed"},
			}, 1)
			return
		}
		if issueResult.Store != blockerResult.Store {
			blockerResult.Close()
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow transition",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": "issue and blocker must resolve to the same workspace",
				},
				Events: []string{"transition_failed"},
			}, 1)
			return
		}
		blockerResolvedID = blockerResult.ResolvedID
		blockerResult.Close()
	}

	if requireBlocker && blockerResolvedID == "" {
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "flow transition",
			Result:  "invalid_input",
			Details: map[string]interface{}{
				"message":         "--blocker is required for transition type",
				"transition_type": transitionType,
			},
			Events: []string{"transition_failed"},
		}, 1)
		return
	}

	notes := appendNotesLine(issueResult.Issue.Notes, noteLine)
	if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{
		"status": types.StatusBlocked,
		"notes":  notes,
	}, actor); err != nil {
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "flow transition",
			Result:  "system_error",
			IssueID: issueResult.ResolvedID,
			Details: map[string]interface{}{"message": fmt.Sprintf("failed to update issue status/notes: %v", err)},
			Events:  []string{"transition_failed"},
		}, 1)
		return
	}

	events := []string{"status_blocked", "context_recorded"}
	if blockerResolvedID != "" {
		dep := &types.Dependency{
			IssueID:     issueResult.ResolvedID,
			DependsOnID: blockerResolvedID,
			Type:        types.DepBlocks,
		}
		if err := issueResult.Store.AddDependency(ctx, dep, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "flow transition",
				Result:  "partial_state",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{
					"partial_state":    "issue_blocked_without_blocker_link",
					"depends_on_id":    blockerResolvedID,
					"dependency_error": err.Error(),
				},
				RecoveryCommand: fmt.Sprintf("bd dep add %s %s --type blocks", issueResult.ResolvedID, blockerResolvedID),
				Events:          append(events, "dependency_add_failed"),
			}, exitCodePartialState)
			return
		}
		events = append(events, "dependency_added")
	}

	finishEnvelope(commandEnvelope{
		OK:      true,
		Command: "flow transition",
		Result:  transitionType,
		IssueID: issueResult.ResolvedID,
		Details: map[string]interface{}{
			"transition_type": transitionType,
			"blocker_id":      blockerResolvedID,
			"note":            noteLine,
		},
		Events: events,
	}, 0)
}

var flowCreateDiscoveredCmd = &cobra.Command{
	Use:   "create-discovered",
	Short: "Create discovered issue and link discovered-from dependency",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
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
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
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
		if !enforceLifecycleStateTransitionGuard(cmd, flowStateFrom, flowStateTo) {
			return
		}
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
		requireEvidenceTuple := evidenceRequirementForVerificationFlow(flowCloseNonHermetic, flowCloseRequireEvidence)
		evidenceMaxAge := 24 * time.Hour
		if requireEvidenceTuple {
			parsedEvidenceAge, err := time.ParseDuration(strings.TrimSpace(flowCloseEvidenceMaxAge))
			if err != nil || parsedEvidenceAge <= 0 {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("invalid --evidence-max-age %q", flowCloseEvidenceMaxAge)},
					Events:  []string{"close_skipped"},
				}, 1)
				return
			}
			evidenceMaxAge = parsedEvidenceAge
		}
		priorityPollMaxAge := 30 * time.Minute
		if flowCloseRequirePriorityPoll {
			parsedPollAge, err := time.ParseDuration(strings.TrimSpace(flowClosePriorityPollMaxAge))
			if err != nil || parsedPollAge <= 0 {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("invalid --priority-poll-max-age %q", flowClosePriorityPollMaxAge)},
					Events:  []string{"close_skipped"},
				}, 1)
				return
			}
			priorityPollMaxAge = parsedPollAge
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

		if err := validateIssueClosable(result.ResolvedID, result.Issue, flowCloseForce); err != nil {
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
		if !flowCloseForce {
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
		}

		if flowCloseRequireTraceability {
			parentIssue, err := resolveParentForTraceability(ctx, result.ResolvedID)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "system_error",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{"message": fmt.Sprintf("traceability lookup failed: %v", err)},
					Events:  []string{"close_skipped"},
				}, 1)
				return
			}
			if !traceabilityChainSatisfied(result.Issue, parentIssue) {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "policy_violation",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message": "traceability check failed: issue acceptance/parent outcome chain is incomplete",
					},
					RecoveryCommand: fmt.Sprintf("bd update %s --acceptance \"<observable outcome>\"", result.ResolvedID),
					Events:          []string{"close_skipped"},
				}, exitCodePolicyViolation)
				return
			}
		}

		requireParentCascade := effectiveRequireParentCascade(flowCloseRequireParentCheck, flowCloseAllowOpenChildren)
		if requireParentCascade {
			unclosedChildren, err := unresolvedParentChildren(ctx, result.ResolvedID)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "system_error",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{"message": fmt.Sprintf("parent cascade check failed: %v", err)},
					Events:  []string{"close_skipped"},
				}, 1)
				return
			}
			if len(unclosedChildren) > 0 {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "policy_violation",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message":           "parent-close cascade check failed: child issues still open",
						"unclosed_children": unclosedChildren,
					},
					RecoveryCommand: fmt.Sprintf("bd children %s", result.ResolvedID),
					Events:          []string{"close_skipped"},
				}, exitCodePolicyViolation)
				return
			}
		}
		if !flowCloseForce {
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
		}
		if requireEvidenceTuple {
			if err := validateEvidenceTupleNotes(result.Issue.Notes, time.Now().UTC(), evidenceMaxAge); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "policy_violation",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message": err.Error(),
					},
					Events: []string{"close_skipped"},
				}, exitCodePolicyViolation)
				return
			}
		}
		if flowCloseRequirePriorityPoll {
			if !hasFreshPriorityPollNote(result.Issue.Notes, time.Now().UTC(), priorityPollMaxAge) {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "policy_violation",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message": "fresh priority-poll evidence missing (run flow priority-poll first)",
					},
					RecoveryCommand: fmt.Sprintf("bd flow priority-poll --issue %s", result.ResolvedID),
					Events:          []string{"close_skipped"},
				}, exitCodePolicyViolation)
				return
			}
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

		if !flowCloseAllowSecretMarkers {
			secretMarkers := detectSecretMarkers(flowCloseReason, notes)
			if len(secretMarkers) > 0 {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "policy_violation",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message":        "secret marker detected in close payload",
						"secret_markers": secretMarkers,
					},
					Events: []string{"close_skipped"},
				}, exitCodePolicyViolation)
				return
			}
		}

		if flowCloseRequireSpecDrift {
			docsChanged, diffErr := docsProofPresentInWorkingTree()
			if diffErr != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "system_error",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to inspect git diff for spec-drift proof: %v", diffErr),
					},
					Events: []string{"close_skipped"},
				}, 1)
				return
			}
			if !specDriftProofSatisfied(docsChanged, notes) {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "flow close-safe",
					Result:  "policy_violation",
					IssueID: result.ResolvedID,
					Details: map[string]interface{}{
						"message": "spec-drift proof missing (need docs diff or DOC-DRIFT tag in notes)",
					},
					RecoveryCommand: fmt.Sprintf("bd flow close-safe --issue %s --reason %q --verified <proof> --note \"DOC-DRIFT:<id>; Owner:<actor>; Next:<command>\"", result.ResolvedID, flowCloseReason),
					Events:          []string{"close_skipped"},
				}, exitCodePolicyViolation)
				return
			}
		}
		if flowCloseForce {
			if err := validateForceCloseAuditNotes(notes); err != nil {
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
		digestUpdated := false
		digestErr := ""
		if closedIssue != nil {
			if err := updateLivingStateDigestAfterClose(ctx, result.Store, closedIssue, flowCloseReason, actor); err != nil {
				digestErr = err.Error()
			} else {
				digestUpdated = true
			}
		}
		events := []string{"verified_note_appended", "closed"}
		if digestUpdated {
			events = append(events, "state_digest_updated")
		} else if digestErr != "" {
			events = append(events, "state_digest_update_failed")
		}
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "flow close-safe",
			Result:  "closed",
			IssueID: result.ResolvedID,
			Details: map[string]interface{}{
				"issue":                compactIssue(closedIssue),
				"verification_entries": verificationEntries,
				"evidence_required":    requireEvidenceTuple,
				"non_hermetic":         flowCloseNonHermetic,
				"force":                flowCloseForce,
				"digest_updated":       digestUpdated,
				"digest_error":         digestErr,
			},
			Events: events,
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

func collectPreclaimViolations(issue *types.Issue, deps []*types.IssueWithDependencyMetadata) []string {
	violations := make([]string, 0)
	if issue == nil {
		return append(violations, "issue.missing")
	}
	if strings.TrimSpace(issue.Description) == "" {
		violations = append(violations, "description.missing")
	}
	if strings.TrimSpace(issue.AcceptanceCriteria) == "" {
		violations = append(violations, "acceptance.missing")
	}
	if !strings.Contains(issue.Description, "## Verify") {
		violations = append(violations, "verify_section.missing")
	} else {
		verifyPathCount := countVerifyPaths(issue.Description)
		if verifyPathCount == 0 {
			violations = append(violations, "verify_path.missing")
		}
		if verifyPathCount > 1 {
			violations = append(violations, "verify_path.multiple")
		}
	}
	if !hasModuleAndAreaLabels(issue.Labels) {
		violations = append(violations, "labels.missing_module_or_area")
	}
	if !hasParentChildDependency(issue.IssueType, deps) {
		violations = append(violations, "dependency_shape.missing_parent_child")
	}
	hasSplitMarker := hasExplicitSplitMarker(issue)
	hasBoundedEstimate := issue.EstimatedMinutes != nil && *issue.EstimatedMinutes > 0 && *issue.EstimatedMinutes <= 180
	if !hasBoundedEstimate && !hasSplitMarker {
		if issue.EstimatedMinutes != nil && *issue.EstimatedMinutes > 180 {
			violations = append(violations, "estimate.exceeds_180")
		} else {
			violations = append(violations, "estimate_or_split.missing")
		}
	}
	return violations
}

func hasExplicitSplitMarker(issue *types.Issue) bool {
	if issue == nil {
		return false
	}
	haystack := strings.ToLower(strings.Join([]string{
		issue.Description,
		issue.AcceptanceCriteria,
		issue.Notes,
	}, "\n"))
	markers := []string{
		"split-required",
		"split_required",
		"split marker",
		"split:",
	}
	for _, marker := range markers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func effectiveRequireAnchor(requireAnchorFlag, allowMissingAnchor bool) bool {
	if requireAnchorFlag {
		return true
	}
	if allowMissingAnchor {
		return false
	}
	return strictControlExplicitIDsEnabled(false)
}

func effectiveRequireParentCascade(requireParentFlag, allowOpenChildren bool) bool {
	if requireParentFlag {
		return true
	}
	if allowOpenChildren {
		return false
	}
	return strictControlExplicitIDsEnabled(false)
}

func resolveAnchorLabel(explicit string, labels []string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	for _, label := range labels {
		if strings.HasPrefix(label, "module/") {
			return label
		}
	}
	return ""
}

func anchorCardinalityViolation(count int) bool {
	return count != 1
}

func extractPlanCountFromNotes(notes string) int {
	const prefix = "PLAN-COUNT:"
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if raw == "" {
			return 0
		}
		value := 0
		for _, r := range raw {
			if r < '0' || r > '9' {
				return 0
			}
			value = (value * 10) + int(r-'0')
		}
		return value
	}
	return 0
}

func intakeClaimGateRequired(planCount int) bool {
	return planCount >= 2
}

func intakeAuditPassed(notes string) bool {
	for _, line := range strings.Split(notes, "\n") {
		if strings.TrimSpace(line) == "INTAKE_AUDIT=PASS" {
			return true
		}
	}
	return false
}

func hasModuleAndAreaLabels(labels []string) bool {
	hasModule := false
	hasArea := false
	for _, label := range labels {
		if strings.HasPrefix(label, "module/") {
			hasModule = true
		}
		if strings.HasPrefix(label, "area/") {
			hasArea = true
		}
	}
	return hasModule && hasArea
}

func hasParentChildDependency(issueType types.IssueType, deps []*types.IssueWithDependencyMetadata) bool {
	if issueType == types.TypeEpic {
		return true
	}
	for _, dep := range deps {
		if dep.DependencyType == types.DepParentChild {
			return true
		}
	}
	return false
}

func hasParentChildToID(deps []*types.IssueWithDependencyMetadata, parentID string) bool {
	if strings.TrimSpace(parentID) == "" {
		return true
	}
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		if dep.DependencyType != types.DepParentChild {
			continue
		}
		if dep.ID == parentID {
			return true
		}
	}
	return false
}

func countVerifyPaths(description string) int {
	lines := strings.Split(description, "\n")
	inVerify := false
	count := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "## Verify" {
			inVerify = true
			continue
		}
		if strings.HasPrefix(line, "## ") {
			if inVerify {
				break
			}
			continue
		}
		if !inVerify || line == "" {
			continue
		}
		count++
	}
	return count
}

func detectDeferredBlockerIDs(deps []*types.IssueWithDependencyMetadata) []string {
	deferred := make([]string, 0)
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		if !dep.DependencyType.AffectsReadyWork() {
			continue
		}
		if dep.Status != types.StatusDeferred {
			continue
		}
		deferred = append(deferred, dep.ID)
	}
	return uniqueSortedStrings(deferred)
}

func summarizePostClaimViability(blocked bool, blockers []string, deferredBlockers []string) string {
	if blocked && len(deferredBlockers) > 0 {
		return "blocked_by_deferred"
	}
	if blocked {
		return "blocked"
	}
	if len(uniqueSortedStrings(blockers)) > 0 {
		return "blocked"
	}
	return "viable"
}

func runBaselineCommand(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command) // #nosec G204 -- operator-provided verification command
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func baselineDecisionFromError(err error) (string, bool, error) {
	if err == nil {
		return "pass", true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "fail", false, nil
	}
	return "", false, fmt.Errorf("baseline command failed to execute: %w", err)
}

func compactBaselineOutput(output string, err error) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		if err == nil {
			return "no output"
		}
		return "command exited non-zero"
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 4 {
		lines = lines[:4]
	}
	joined := strings.Join(lines, " | ")
	if len(joined) > 240 {
		joined = joined[:240]
	}
	return joined
}

func appendPriorityPollNote(existing string, polledAt time.Time, p0IDs []string) string {
	p0 := "none"
	if len(p0IDs) > 0 {
		p0 = strings.Join(uniqueSortedStrings(p0IDs), ",")
	}
	line := fmt.Sprintf("Priority poll: %s; p0=%s", polledAt.UTC().Format(time.RFC3339), p0)
	return appendNotesLine(existing, line)
}

func hasFreshPriorityPollNote(notes string, now time.Time, maxAge time.Duration) bool {
	last, ok := latestPriorityPollTimestamp(notes)
	if !ok {
		return false
	}
	age := now.Sub(last)
	if age < 0 {
		return false
	}
	return age <= maxAge
}

func latestPriorityPollTimestamp(notes string) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Priority poll:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Priority poll:"))
		parts := strings.SplitN(rest, ";", 2)
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		if !found || ts.After(latest) {
			latest = ts
			found = true
		}
	}
	return latest, found
}

func buildExecutionRollbackDescription(originalID, originalTitle, verifyCmd string) string {
	verifyCmd = strings.TrimSpace(verifyCmd)
	if verifyCmd == "" {
		verifyCmd = "go test ./cmd/bd -count=1"
	}
	lines := []string{
		"## Context",
		fmt.Sprintf("Corrective follow-up for %s (%s).", strings.TrimSpace(originalID), strings.TrimSpace(originalTitle)),
		"",
		"## Change",
		fmt.Sprintf("Restore intended behavior and correct rollback regression introduced by %s.", strings.TrimSpace(originalID)),
		"",
		"## Acceptance Criteria",
		"Correct behavior is restored and downstream work can proceed safely.",
		"",
		"## Verify",
		verifyCmd,
	}
	return strings.Join(lines, "\n")
}

func buildExecutionRollbackAcceptance(originalID string) string {
	return fmt.Sprintf("Correct behavior for %s is restored and verified.", strings.TrimSpace(originalID))
}

func normalizeTransitionType(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

func supportedTransitionTypes() []string {
	return []string{
		"claim_failed",
		"transient_failure",
		"decomposition_invalid",
		"claim_became_blocked",
		"exec_blocked",
		"test_failed",
		"conditional_fallback_activate",
		"session_abort",
	}
}

func isSupportedTransitionType(transitionType string) bool {
	switch transitionType {
	case "claim_failed",
		"transient_failure",
		"decomposition_invalid",
		"claim_became_blocked",
		"exec_blocked",
		"test_failed",
		"conditional_fallback_activate",
		"session_abort":
		return true
	default:
		return false
	}
}

func transitionRequiresIssue(transitionType string) bool {
	switch transitionType {
	case "claim_failed", "session_abort":
		return false
	case "transient_failure", "decomposition_invalid", "claim_became_blocked", "exec_blocked", "test_failed", "conditional_fallback_activate":
		return true
	default:
		return false
	}
}

func transitionContextOrReason(contextPack, reason string) string {
	contextPack = strings.TrimSpace(contextPack)
	if contextPack != "" {
		return contextPack
	}
	return strings.TrimSpace(reason)
}

func normalizeFailureCloseReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "failed:") {
		return trimmed
	}
	return "failed: " + trimmed
}

func buildConditionalFallbackDecisionNote(contextPack string) string {
	evidence := strings.TrimSpace(contextPack)
	if evidence == "" {
		evidence = "manual transition trigger"
	}
	return fmt.Sprintf(
		"Decision | Evidence | Risk | Follow-up ID: conditional_fallback_activate | %s | fallback branch activation accepted | none",
		evidence,
	)
}

func buildAbortHandoffMarkdown(reason, issueID, contextPack string) string {
	lines := []string{
		"# ABORT Handoff",
		fmt.Sprintf("Reason: %s", strings.TrimSpace(reason)),
	}
	if strings.TrimSpace(issueID) != "" {
		lines = append(lines, fmt.Sprintf("Issue: %s", strings.TrimSpace(issueID)))
	} else {
		lines = append(lines, "Issue: none")
	}
	if strings.TrimSpace(contextPack) != "" {
		lines = append(lines, fmt.Sprintf("State: %s", strings.TrimSpace(contextPack)))
	} else {
		lines = append(lines, "State: context not provided")
	}
	lines = append(lines, "Next: resolve safety conditions, then resume via `bd ready --limit 5`")
	return strings.Join(lines, "\n") + "\n"
}

func buildSessionAbortContextPack(reason, contextPack string) string {
	repro := strings.TrimSpace(contextPack)
	if repro == "" {
		repro = "n/a"
	}
	return fmt.Sprintf(
		"Context pack: session_abort (%s); Repro: %s; Next: bd ready --limit 5; Files: n/a; Blockers: n/a",
		strings.TrimSpace(reason),
		repro,
	)
}

func parseTransientBackoffSchedule(raw string, maxAttempts int) ([]time.Duration, error) {
	if maxAttempts <= 0 {
		return nil, fmt.Errorf("max attempts must be > 0")
	}

	defaults := []time.Duration{30 * time.Second, 90 * time.Second, 180 * time.Second}
	values := make([]time.Duration, 0, maxAttempts)
	if strings.TrimSpace(raw) == "" {
		values = append(values, defaults...)
	} else {
		for _, field := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(field)
			if trimmed == "" {
				continue
			}
			duration, err := time.ParseDuration(trimmed)
			if err != nil || duration <= 0 {
				return nil, fmt.Errorf("invalid backoff duration %q", trimmed)
			}
			values = append(values, duration)
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("backoff schedule is empty")
		}
	}

	if len(values) < maxAttempts {
		last := values[len(values)-1]
		for len(values) < maxAttempts {
			values = append(values, last)
		}
	}
	if len(values) > maxAttempts {
		values = values[:maxAttempts]
	}
	return values, nil
}

func resolveTransientEscalationType(raw string) (string, error) {
	normalized := normalizeTransitionType(raw)
	if normalized == "" {
		normalized = "test_failed"
	}
	switch normalized {
	case "test_failed", "exec_blocked":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid transient escalation type %q (allowed: test_failed, exec_blocked)", raw)
	}
}

func nextDecompositionInvalidAttempt(notes string) int {
	maxAttempt := 0
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Decomposition invalid (attempt ") {
			continue
		}
		start := len("Decomposition invalid (attempt ")
		if len(trimmed) <= start {
			continue
		}
		end := strings.Index(trimmed[start:], ")")
		if end <= 0 {
			continue
		}
		raw := trimmed[start : start+end]
		value := 0
		valid := true
		for _, ch := range raw {
			if ch < '0' || ch > '9' {
				valid = false
				break
			}
			value = value*10 + int(ch-'0')
		}
		if valid && value > maxAttempt {
			maxAttempt = value
		}
	}
	return maxAttempt + 1
}

func decompositionDamperEscalationRequired(attempt, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	return attempt >= threshold
}

func evidenceRequirementForVerificationFlow(nonHermetic bool, explicitRequire bool) bool {
	return nonHermetic || explicitRequire
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) <= 1 {
		return out
	}
	sort.Strings(out)
	return out
}

func detectSecretMarkers(parts ...string) []string {
	candidates := []string{"sk-", "ghp_", "gho_", "AKIA", "eyJ", "-----BEGIN"}
	seen := map[string]struct{}{}
	joined := strings.Join(parts, "\n")
	lowerJoined := strings.ToLower(joined)
	for _, marker := range candidates {
		if strings.Contains(lowerJoined, strings.ToLower(marker)) {
			seen[marker] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for _, marker := range candidates {
		if _, ok := seen[marker]; ok {
			out = append(out, marker)
		}
	}
	return out
}

func hasDocDriftTag(notes string) bool {
	for _, field := range strings.Fields(notes) {
		if strings.HasPrefix(field, "DOC-DRIFT:") && len(field) > len("DOC-DRIFT:") {
			return true
		}
	}
	return false
}

func specDriftProofSatisfied(docsChanged bool, notes string) bool {
	return docsChanged || hasDocDriftTag(notes)
}

func traceabilityChainSatisfied(issue *types.Issue, parent *types.Issue) bool {
	if issue == nil || parent == nil {
		return false
	}
	if strings.TrimSpace(issue.AcceptanceCriteria) == "" {
		return false
	}
	parentOutcome := strings.TrimSpace(parent.AcceptanceCriteria)
	if parentOutcome == "" {
		parentOutcome = strings.TrimSpace(parent.Description)
	}
	return parentOutcome != ""
}

func resolveParentForTraceability(ctx context.Context, issueID string) (*types.Issue, error) {
	deps, err := store.GetDependenciesWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	for _, dep := range deps {
		if dep.DependencyType == types.DepParentChild {
			return &dep.Issue, nil
		}
	}
	return nil, nil
}

func collectUnclosedParentChildren(dependents []*types.IssueWithDependencyMetadata) []string {
	ids := make([]string, 0)
	for _, dep := range dependents {
		if dep.DependencyType != types.DepParentChild {
			continue
		}
		if dep.Status == types.StatusClosed {
			continue
		}
		ids = append(ids, dep.ID)
	}
	return ids
}

func unresolvedParentChildren(ctx context.Context, issueID string) ([]string, error) {
	dependents, err := store.GetDependentsWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return collectUnclosedParentChildren(dependents), nil
}

type livingDigestStore interface {
	GetDependenciesWithMetadata(context.Context, string) ([]*types.IssueWithDependencyMetadata, error)
	SearchIssues(context.Context, string, types.IssueFilter) ([]*types.Issue, error)
	GetReadyWork(context.Context, types.WorkFilter) ([]*types.Issue, error)
	GetBlockedIssues(context.Context, types.WorkFilter) ([]*types.BlockedIssue, error)
	UpdateIssue(context.Context, string, map[string]interface{}, string) error
}

func updateLivingStateDigestAfterClose(ctx context.Context, issueStore livingDigestStore, closedIssue *types.Issue, closeReason string, closeActor string) error {
	if issueStore == nil || closedIssue == nil {
		return nil
	}
	module := firstModuleLabel(closedIssue.Labels)
	if module == "" {
		return nil
	}

	deps, err := issueStore.GetDependenciesWithMetadata(ctx, closedIssue.ID)
	if err != nil {
		return fmt.Errorf("living-digest parent lookup failed: %w", err)
	}
	parentID := ""
	for _, dep := range deps {
		if dep == nil || dep.DependencyType != types.DepParentChild {
			continue
		}
		parentID = dep.ID
		break
	}
	if parentID == "" {
		return nil
	}

	statusPinned := types.StatusPinned
	anchors, err := issueStore.SearchIssues(ctx, "", types.IssueFilter{
		Status:   &statusPinned,
		ParentID: &parentID,
		Labels:   []string{module},
		Limit:    20,
	})
	if err != nil {
		return fmt.Errorf("living-digest anchor query failed: %w", err)
	}
	if len(anchors) != 1 {
		return nil
	}
	anchor := anchors[0]

	wipSummary := "none"
	statusInProgress := types.StatusInProgress
	wip, err := issueStore.SearchIssues(ctx, "", types.IssueFilter{
		Status:   &statusInProgress,
		Assignee: &closeActor,
		Limit:    1,
	})
	if err == nil && len(wip) > 0 {
		wipSummary = fmt.Sprintf("%s %s", wip[0].ID, wip[0].Title)
	}

	readyIssues, err := issueStore.GetReadyWork(ctx, types.WorkFilter{Status: types.StatusOpen, Limit: 3})
	if err != nil {
		return fmt.Errorf("living-digest ready query failed: %w", err)
	}
	readyIDs := make([]string, 0, len(readyIssues))
	for _, issue := range readyIssues {
		readyIDs = append(readyIDs, issue.ID)
	}

	blockedIssues, err := issueStore.GetBlockedIssues(ctx, types.WorkFilter{Status: types.StatusOpen, Limit: 3})
	if err != nil {
		return fmt.Errorf("living-digest blocked query failed: %w", err)
	}
	blockerIDs := make([]string, 0, len(blockedIssues))
	for _, issue := range blockedIssues {
		blockerIDs = append(blockerIDs, issue.ID)
	}

	count := nextSessionCloseCount(anchor.Notes)
	lastClosed := fmt.Sprintf("%s %s", closedIssue.ID, strings.TrimSpace(closeReason))
	digest := buildLivingStateDigestBlock(time.Now().UTC(), wipSummary, lastClosed, readyIDs, blockerIDs, count)
	notes := appendNotesLine(anchor.Notes, digest)
	if err := issueStore.UpdateIssue(ctx, anchor.ID, map[string]interface{}{"notes": notes}, closeActor); err != nil {
		return fmt.Errorf("living-digest update failed: %w", err)
	}
	return nil
}

func firstModuleLabel(labels []string) string {
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if strings.HasPrefix(trimmed, "module/") {
			return trimmed
		}
	}
	return ""
}

func nextSessionCloseCount(notes string) int {
	current := 0
	const prefix = "Session tasks closed:"
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if raw == "" {
			continue
		}
		value := 0
		valid := true
		for _, ch := range raw {
			if ch < '0' || ch > '9' {
				valid = false
				break
			}
			value = value*10 + int(ch-'0')
		}
		if valid && value >= current {
			current = value
		}
	}
	return current + 1
}

func buildLivingStateDigestBlock(ts time.Time, wipSummary, lastClosed string, readyIDs, blockerIDs []string, closeCount int) string {
	if strings.TrimSpace(wipSummary) == "" {
		wipSummary = "none"
	}
	if strings.TrimSpace(lastClosed) == "" {
		lastClosed = "none"
	}
	readySummary := "none"
	if len(readyIDs) > 0 {
		readySummary = strings.Join(uniqueSortedStrings(readyIDs), ",")
	}
	blockerSummary := "none"
	if len(blockerIDs) > 0 {
		blockerSummary = strings.Join(uniqueSortedStrings(blockerIDs), ",")
	}
	lines := []string{
		fmt.Sprintf("State digest (%s):", ts.Format(time.RFC3339)),
		fmt.Sprintf("WIP: %s", wipSummary),
		fmt.Sprintf("Last closed: %s", lastClosed),
		fmt.Sprintf("Ready next: %s", readySummary),
		fmt.Sprintf("Blockers: %s", blockerSummary),
		fmt.Sprintf("Session tasks closed: %d", closeCount),
	}
	return strings.Join(lines, "\n")
}

func docsProofPresentInWorkingTree() (bool, error) {
	out, err := runSubprocess("git", "diff", "--name-only")
	if err != nil {
		return false, err
	}
	for _, raw := range strings.Split(out, "\n") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		up := strings.ToUpper(path)
		if strings.HasPrefix(path, "docs/") || strings.Contains(path, "/docs/") ||
			strings.Contains(up, "README") || strings.Contains(up, "ARCHITECTURE") || strings.Contains(up, "SPEC") {
			return true, nil
		}
	}
	return false, nil
}

func init() {
	flowCmd.PersistentFlags().StringVar(&flowStateFrom, "state-from", "", "Current session state for lifecycle transition validation")
	flowCmd.PersistentFlags().StringVar(&flowStateTo, "state-to", "", "Target session state for lifecycle transition validation")
	flowClaimNextCmd.Flags().StringVar(&flowClaimParent, "parent", "", "Filter ready queue by parent epic/issue")
	flowClaimNextCmd.Flags().StringSliceVar(&flowClaimLabels, "label", nil, "AND label filter (repeat flag)")
	flowClaimNextCmd.Flags().StringSliceVar(&flowClaimLabelsAny, "label-any", nil, "OR label filter (repeat flag)")
	flowClaimNextCmd.Flags().IntVar(&flowClaimPriority, "priority", 0, "Priority filter (0-4)")
	flowClaimNextCmd.Flags().IntVar(&flowClaimLimit, "limit", 10, "Maximum ready candidates to scan")
	flowClaimNextCmd.Flags().BoolVar(&flowClaimRequireAnchor, "require-anchor", false, "Require exactly one pinned anchor for module/milestone slice before claim")
	flowClaimNextCmd.Flags().BoolVar(&flowClaimAllowMissingAnchor, "allow-missing-anchor", false, "Bypass strict-control default anchor requirement")
	flowClaimNextCmd.Flags().StringVar(&flowClaimAnchorLabel, "anchor-label", "", "Anchor label to enforce when --require-anchor is set (for example module/<name>)")
	flowClaimNextCmd.ValidArgsFunction = noCompletions

	flowPreclaimLintCmd.Flags().StringVar(&flowPreclaimIssue, "issue", "", "Issue ID to lint before claim")
	flowPreclaimLintCmd.ValidArgsFunction = noCompletions
	flowBaselineVerifyCmd.Flags().StringVar(&flowBaselineIssue, "issue", "", "Issue ID to annotate with baseline verify state")
	flowBaselineVerifyCmd.Flags().StringVar(&flowBaselineCmd, "cmd", "", "Baseline verification command to run pre-edit")
	flowBaselineVerifyCmd.ValidArgsFunction = noCompletions
	flowPriorityPollCmd.Flags().StringVar(&flowPriorityPollIssue, "issue", "", "Optional issue ID to annotate with priority poll evidence")
	flowPriorityPollCmd.ValidArgsFunction = noCompletions
	flowSupersedeCoarseCmd.Flags().StringVar(&flowSupersedeIssueID, "issue", "", "Coarse issue ID to supersede")
	flowSupersedeCoarseCmd.Flags().StringArrayVar(&flowSupersedeIDs, "replacement", nil, "Replacement atomic issue ID (repeat flag)")
	flowSupersedeCoarseCmd.Flags().StringVar(&flowSupersedeReason, "reason", "", "Optional safe close reason for superseded coarse issue")
	flowSupersedeCoarseCmd.ValidArgsFunction = noCompletions
	flowExecutionRollbackCmd.Flags().StringVar(&flowRollbackIssueID, "issue", "", "Original issue ID requiring corrective rollback task")
	flowExecutionRollbackCmd.Flags().StringVar(&flowRollbackTitle, "title", "", "Optional corrective issue title")
	flowExecutionRollbackCmd.Flags().StringVar(&flowRollbackVerifyCmd, "verify", "go test ./cmd/bd -count=1", "Verification command for corrective task template")
	flowExecutionRollbackCmd.ValidArgsFunction = noCompletions
	flowTransitionCmd.Flags().StringVar(&flowTransitionType, "type", "", "Transition type (claim_failed, transient_failure, decomposition_invalid, claim_became_blocked, exec_blocked, test_failed, conditional_fallback_activate, session_abort)")
	flowTransitionCmd.Flags().StringVar(&flowTransitionIssueID, "issue", "", "Issue ID targeted by the transition handler")
	flowTransitionCmd.Flags().StringVar(&flowTransitionBlocker, "blocker", "", "Optional blocker issue ID for blocked transitions")
	flowTransitionCmd.Flags().StringVar(&flowTransitionContext, "context", "", "Transition context/details")
	flowTransitionCmd.Flags().StringVar(&flowTransitionReason, "reason", "", "Transition reason or close reason (required for fallback/abort)")
	flowTransitionCmd.Flags().StringVar(&flowTransitionAbortMD, "abort-handoff", "ABORT_HANDOFF.md", "Path to ABORT handoff markdown for session_abort")
	flowTransitionCmd.Flags().BoolVar(&flowTransitionNoWrite, "abort-no-bd-write", false, "Skip issue mutation during session_abort and only write abort handoff")
	flowTransitionCmd.Flags().IntVar(&flowTransitionAttempt, "attempt", 1, "Transient failure attempt number (1-based)")
	flowTransitionCmd.Flags().IntVar(&flowTransitionMaxRetry, "max-attempts", 3, "Transient failure max retry attempts")
	flowTransitionCmd.Flags().StringVar(&flowTransitionBackoff, "backoff", "30s,90s,180s", "Transient failure backoff schedule (comma-separated durations)")
	flowTransitionCmd.Flags().StringVar(&flowTransitionEscalate, "escalate", "test_failed", "Transient exhaustion escalation type (test_failed|exec_blocked)")
	flowTransitionCmd.Flags().IntVar(&flowTransitionDecompThreshold, "decomposition-threshold", 3, "Escalation threshold for decomposition_invalid attempts")
	flowTransitionCmd.ValidArgsFunction = noCompletions

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
	flowCloseSafeCmd.Flags().BoolVarP(&flowCloseForce, "force", "f", false, "Force close pinned/issues with open blockers (requires force-close audit fields)")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseAllowFailureReason, "allow-failure-reason", false, "Allow failed: close reasons")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseAllowSecretMarkers, "allow-secret-markers", false, "Allow secret-pattern markers in close reason/notes (manual/exceptional use)")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseRequireSpecDrift, "require-spec-drift-proof", false, "Require docs diff or DOC-DRIFT tag in notes before close")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseRequireTraceability, "require-traceability", false, "Require traceability chain to parent outcome before close")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseRequireParentCheck, "require-parent-cascade", false, "Require parent-close cascade check (no open parent-child dependents)")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseAllowOpenChildren, "allow-open-children", false, "Bypass strict-control default parent-cascade enforcement")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseRequireEvidence, "require-evidence-tuple", false, "Require a fresh EvidenceTuple note entry before close")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseNonHermetic, "non-hermetic", false, "Mark verification flow as non-hermetic and require EvidenceTuple automatically")
	flowCloseSafeCmd.Flags().StringVar(&flowCloseEvidenceMaxAge, "evidence-max-age", "24h", "Maximum allowed EvidenceTuple age when --require-evidence-tuple is set")
	flowCloseSafeCmd.Flags().BoolVar(&flowCloseRequirePriorityPoll, "require-priority-poll", false, "Require a fresh Priority poll note before close")
	flowCloseSafeCmd.Flags().StringVar(&flowClosePriorityPollMaxAge, "priority-poll-max-age", "30m", "Maximum age for Priority poll note when --require-priority-poll is set")
	flowCloseSafeCmd.ValidArgsFunction = noCompletions

	flowCmd.AddCommand(flowClaimNextCmd)
	flowCmd.AddCommand(flowPreclaimLintCmd)
	flowCmd.AddCommand(flowBaselineVerifyCmd)
	flowCmd.AddCommand(flowPriorityPollCmd)
	flowCmd.AddCommand(flowSupersedeCoarseCmd)
	flowCmd.AddCommand(flowExecutionRollbackCmd)
	flowCmd.AddCommand(flowTransitionCmd)
	flowCmd.AddCommand(flowCreateDiscoveredCmd)
	flowCmd.AddCommand(flowBlockWithContextCmd)
	flowCmd.AddCommand(flowCloseSafeCmd)
	rootCmd.AddCommand(flowCmd)
}

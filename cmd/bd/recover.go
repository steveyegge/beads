package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

type recoverSignature struct {
	BlockedIDs   []string `json:"blocked_ids"`
	BlockerEdges []string `json:"blocker_edges"`
	BlockedCount int      `json:"blocked_count"`
}

type recoverLoopSnapshot struct {
	Count int      `json:"count"`
	IDs   []string `json:"ids"`
}

var (
	recoverStateFrom              string
	recoverStateTo                string
	recoverSignatureParentID      string
	recoverSignatureLimit         int
	recoverSignaturePrevious      string
	recoverSignatureIteration     int
	recoverSignatureElapsedMinute int
	recoverSignatureMaxIterations int
	recoverSignatureMaxMinutes    int
	recoverSignatureAnchorID      string
	recoverSignatureWriteAnchor   bool
	recoverLoopParentID           string
	recoverLoopModuleLabel        string
	recoverLoopLimit              int
)

var recoverCmd = &cobra.Command{
	Use:     "recover",
	GroupID: "deps",
	Short:   "Recover-loop diagnostics and convergence tooling",
}

var recoverSignatureCmd = &cobra.Command{
	Use:   "signature",
	Short: "Compute recover-loop convergence signature and escalation state",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, recoverStateFrom, recoverStateTo) {
			return
		}
		ctx := rootCtx

		var parentID *string
		if strings.TrimSpace(recoverSignatureParentID) != "" {
			resolvedParent, err := utils.ResolvePartialID(ctx, store, recoverSignatureParentID)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve parent %q: %v", recoverSignatureParentID, err)},
					Events:  []string{"recover_signature_failed"},
				}, 1)
				return
			}
			parentID = &resolvedParent
		}

		filter := types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: parentID,
			Limit:    recoverSignatureLimit,
		}
		blockedIssues, err := store.GetBlockedIssues(ctx, filter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover signature",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("blocked query failed: %v", err)},
				Events:  []string{"recover_signature_failed"},
			}, 1)
			return
		}

		current := buildRecoverSignature(blockedIssues)
		converged := false
		if strings.TrimSpace(recoverSignaturePrevious) != "" {
			var previous recoverSignature
			if err := json.Unmarshal([]byte(recoverSignaturePrevious), &previous); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to parse --previous-signature JSON: %v", err)},
					Events:  []string{"recover_signature_failed"},
				}, 1)
				return
			}
			converged = recoverSignaturesEqual(previous, current)
		}

		elapsed := time.Duration(recoverSignatureElapsedMinute) * time.Minute
		escalationRequired := converged && recoverEscalationRequired(
			recoverSignatureIteration,
			elapsed,
			recoverSignatureMaxIterations,
			time.Duration(recoverSignatureMaxMinutes)*time.Minute,
		)

		result := "changed"
		if converged {
			result = "converged"
		}
		if escalationRequired {
			result = "escalation_required"
		}

		exitCode := 0
		if escalationRequired {
			exitCode = exitCodePolicyViolation
		}

		anchorWrite := map[string]interface{}{
			"attempted": false,
			"written":   false,
		}
		if strings.TrimSpace(recoverSignatureAnchorID) != "" {
			anchorWrite["attempted"] = true
			if !recoverSignatureWriteAnchor {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message": "anchor note writes require --write-anchor",
						"anchor":  strings.TrimSpace(recoverSignatureAnchorID),
					},
					RecoveryCommand: "rerun with --write-anchor to allow anchor note mutation",
					Events:          []string{"recover_signature_failed"},
				}, exitCodePolicyViolation)
				return
			}
			if readonlyMode {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message": "anchor note writes are not allowed in read-only mode",
						"anchor":  strings.TrimSpace(recoverSignatureAnchorID),
					},
					Events: []string{"recover_signature_failed"},
				}, exitCodePolicyViolation)
				return
			}
			anchorResult, err := resolveAndGetIssueWithRouting(ctx, store, recoverSignatureAnchorID)
			if err != nil {
				if anchorResult != nil {
					anchorResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve anchor %q: %v", recoverSignatureAnchorID, err)},
					Events:  []string{"recover_signature_failed"},
				}, 1)
				return
			}
			if anchorResult == nil || anchorResult.Issue == nil {
				if anchorResult != nil {
					anchorResult.Close()
				}
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("anchor %q not found", recoverSignatureAnchorID)},
					Events:  []string{"recover_signature_failed"},
				}, 1)
				return
			}
			note := buildRecoverSignatureAnchorNote(recoverSignatureIteration, current)
			updated := appendNotesLine(anchorResult.Issue.Notes, note)
			if err := anchorResult.Store.UpdateIssue(ctx, anchorResult.ResolvedID, map[string]interface{}{"notes": updated}, actor); err != nil {
				anchorResult.Close()
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover signature",
					Result:  "system_error",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to append anchor signature note: %v", err)},
					Events:  []string{"recover_signature_failed"},
				}, 1)
				return
			}
			anchorWrite["written"] = true
			anchorWrite["anchor_id"] = anchorResult.ResolvedID
			anchorResult.Close()
		}

		finishEnvelope(commandEnvelope{
			OK:      !escalationRequired,
			Command: "recover signature",
			Result:  result,
			Details: map[string]interface{}{
				"signature":           current,
				"converged":           converged,
				"iteration":           recoverSignatureIteration,
				"elapsed_minutes":     recoverSignatureElapsedMinute,
				"max_iterations":      recoverSignatureMaxIterations,
				"max_minutes":         recoverSignatureMaxMinutes,
				"escalation_required": escalationRequired,
				"anchor_write":        anchorWrite,
			},
			Events: []string{"recover_signature_computed"},
		}, exitCode)
	},
}

var recoverLoopCmd = &cobra.Command{
	Use:   "loop",
	Short: "Run deterministic recover-loop phase orchestration",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, recoverStateFrom, recoverStateTo) {
			return
		}
		ctx := rootCtx

		var parentID *string
		if strings.TrimSpace(recoverLoopParentID) != "" {
			resolvedParent, err := utils.ResolvePartialID(ctx, store, recoverLoopParentID)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover loop",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve parent %q: %v", recoverLoopParentID, err)},
					Events:  []string{"recover_loop_failed"},
				}, 1)
				return
			}
			parentID = &resolvedParent
		}

		scopedReadyIssues, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: parentID,
			Labels:   recoverLoopScopedLabels(),
			Limit:    recoverLoopLimit,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("ready query failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		phase1 := map[string]interface{}{
			"gate_check": map[string]interface{}{"status": "pass"},
		}
		nativeGateIDs, fallbackGateIDs, err := recoverOpenGateIDs(ctx, parentID, recoverLoopLimit)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("gate diagnosis failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		phase1["gate_check"] = map[string]interface{}{
			"status":              "pass",
			"native_open_count":   len(nativeGateIDs),
			"native_open_ids":     nativeGateIDs,
			"fallback_open_count": len(fallbackGateIDs),
			"fallback_open_ids":   fallbackGateIDs,
		}
		phase1["scoped_ready"] = recoverLoopSnapshot{Count: len(scopedReadyIssues), IDs: issueIDs(scopedReadyIssues)}

		phaseOutcomes := map[string]string{
			"phase_1": "continue",
			"phase_2": "skipped",
			"phase_3": "skipped",
			"phase_4": "skipped",
		}
		phase1Outcome, stopAfterPhase1 := recoverPhase1Outcome(len(scopedReadyIssues))
		phaseOutcomes["phase_1"] = phase1Outcome
		if stopAfterPhase1 {
			phase1["blocked"] = map[string]interface{}{"status": "skipped", "reason": "ready_found"}
			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "recover loop",
				Result:  "recover_ready_found",
				Details: map[string]interface{}{
					"phase_outcomes": phaseOutcomes,
					"phase_1":        phase1,
					"scope": map[string]interface{}{
						"parent_id":    strings.TrimSpace(recoverLoopParentID),
						"module_label": strings.TrimSpace(recoverLoopModuleLabel),
						"limit":        recoverLoopLimit,
					},
				},
				Events: []string{"recover_loop_phases_evaluated"},
			}, 0)
			return
		}

		blockedIssues, err := store.GetBlockedIssues(ctx, types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: parentID,
			Labels:   recoverLoopScopedLabels(),
			Limit:    recoverLoopLimit,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("blocked query failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		phase1["blocked"] = recoverLoopSnapshot{Count: len(blockedIssues), IDs: blockedIssueIDs(blockedIssues)}

		cycles, err := store.DetectCycles(ctx)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("cycle check failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		phaseOutcomes["phase_2"] = recoverPhase2Outcome(len(cycles), len(blockedIssues))

		staleCandidates, hookedIDs, err := recoverStructuralDiagnostics(ctx, parentID, recoverLoopLimit)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("phase 2 diagnostics failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		rootBlockerIDs := recoverRootBlockersFromBlocked(blockedIssues)
		phase2 := map[string]interface{}{
			"cycle_count":        len(cycles),
			"stale_wip_ids":      staleCandidates,
			"hooked_ids":         hookedIDs,
			"root_blocker_ids":   rootBlockerIDs,
			"native_gate_open":   nativeGateIDs,
			"fallback_gate_open": fallbackGateIDs,
		}

		statusOpen := types.StatusOpen
		openIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{
			Status:   &statusOpen,
			ParentID: parentID,
			Labels:   recoverLoopScopedLabels(),
			Limit:    recoverLoopLimit,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("open query failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}

		limboCandidates := detectRecoverLimboCandidates(openIssues, scopedReadyIssues, blockedIssues)
		phaseOutcomes["phase_3"] = "clear"
		if len(limboCandidates) > 0 {
			phaseOutcomes["phase_3"] = "limbo_detected"
		}
		phase3 := map[string]interface{}{
			"limbo_count":      len(limboCandidates),
			"limbo_candidates": limboCandidates,
		}

		moduleScopeEnabled := strings.TrimSpace(recoverLoopModuleLabel) != ""
		moduleOnlyReady := []*types.Issue{}
		if moduleScopeEnabled {
			moduleOnlyReady, err = store.GetReadyWork(ctx, types.WorkFilter{
				Status: types.StatusOpen,
				Labels: recoverLoopScopedLabels(),
				Limit:  recoverLoopLimit,
			})
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "recover loop",
					Result:  "system_error",
					Details: map[string]interface{}{"message": fmt.Sprintf("phase 4 module-only ready query failed: %v", err)},
					Events:  []string{"recover_loop_failed"},
				}, 1)
				return
			}
		}
		unscopedReady, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  recoverLoopLimit,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("phase 4 unscoped ready query failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		unassignedReady, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:     types.StatusOpen,
			Unassigned: true,
			Limit:      recoverLoopLimit,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "recover loop",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("phase 4 unassigned ready query failed: %v", err)},
				Events:  []string{"recover_loop_failed"},
			}, 1)
			return
		}
		phase4 := map[string]interface{}{
			"module_scope_enabled": moduleScopeEnabled,
			"module_only":          recoverLoopSnapshot{Count: len(moduleOnlyReady), IDs: issueIDs(moduleOnlyReady)},
			"unscoped":             recoverLoopSnapshot{Count: len(unscopedReady), IDs: issueIDs(unscopedReady)},
			"unassigned":           recoverLoopSnapshot{Count: len(unassignedReady), IDs: issueIDs(unassignedReady)},
		}
		phaseOutcomes["phase_4"] = resolveRecoverPhase4Outcome(moduleScopeEnabled, len(moduleOnlyReady), len(unscopedReady), len(unassignedReady))

		result := resolveRecoverLoopResult(phaseOutcomes)

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "recover loop",
			Result:  result,
			Details: map[string]interface{}{
				"phase_outcomes": phaseOutcomes,
				"phase_1":        phase1,
				"phase_2":        phase2,
				"phase_3":        phase3,
				"phase_4":        phase4,
				"scope": map[string]interface{}{
					"parent_id":    strings.TrimSpace(recoverLoopParentID),
					"module_label": strings.TrimSpace(recoverLoopModuleLabel),
					"limit":        recoverLoopLimit,
				},
			},
			Events: []string{"recover_loop_phases_evaluated"},
		}, 0)
	},
}

func buildRecoverSignature(blocked []*types.BlockedIssue) recoverSignature {
	blockedIDs := make([]string, 0, len(blocked))
	edges := make([]string, 0)
	for _, issue := range blocked {
		blockedIDs = append(blockedIDs, issue.ID)
		for _, blocker := range issue.BlockedBy {
			edges = append(edges, fmt.Sprintf("%s|%s", issue.ID, blocker))
		}
	}
	sort.Strings(blockedIDs)
	sort.Strings(edges)
	return recoverSignature{
		BlockedIDs:   blockedIDs,
		BlockerEdges: edges,
		BlockedCount: len(blocked),
	}
}

func recoverPhase1Outcome(scopedReadyCount int) (string, bool) {
	if scopedReadyCount > 0 {
		return "ready_found", true
	}
	return "continue", false
}

func recoverPhase2Outcome(cycleCount, blockedCount int) string {
	if cycleCount > 0 {
		return "cycles_detected"
	}
	if blockedCount > 0 {
		return "blocked_detected"
	}
	return "clean"
}

func buildRecoverSignatureAnchorNote(iteration int, sig recoverSignature) string {
	return fmt.Sprintf(
		"Recover iteration %d: blocked_ids=%s; blocker_edges=%s; blocked_count=%d",
		iteration,
		strings.Join(sig.BlockedIDs, ","),
		strings.Join(sig.BlockerEdges, ","),
		sig.BlockedCount,
	)
}

func recoverSignaturesEqual(a, b recoverSignature) bool {
	if a.BlockedCount != b.BlockedCount {
		return false
	}
	if len(a.BlockedIDs) != len(b.BlockedIDs) || len(a.BlockerEdges) != len(b.BlockerEdges) {
		return false
	}
	for i := range a.BlockedIDs {
		if a.BlockedIDs[i] != b.BlockedIDs[i] {
			return false
		}
	}
	for i := range a.BlockerEdges {
		if a.BlockerEdges[i] != b.BlockerEdges[i] {
			return false
		}
	}
	return true
}

func recoverEscalationRequired(iteration int, elapsed time.Duration, maxIterations int, maxDuration time.Duration) bool {
	if iteration >= maxIterations {
		return true
	}
	if elapsed >= maxDuration {
		return true
	}
	return false
}

func detectRecoverLimboCandidates(open []*types.Issue, ready []*types.Issue, blocked []*types.BlockedIssue) []string {
	readySet := make(map[string]struct{}, len(ready))
	for _, issue := range ready {
		readySet[issue.ID] = struct{}{}
	}
	blockedSet := make(map[string]struct{}, len(blocked))
	for _, issue := range blocked {
		blockedSet[issue.ID] = struct{}{}
	}

	limbo := make([]string, 0)
	for _, issue := range open {
		if issue == nil {
			continue
		}
		if _, ok := readySet[issue.ID]; ok {
			continue
		}
		if _, ok := blockedSet[issue.ID]; ok {
			continue
		}
		limbo = append(limbo, issue.ID)
	}
	sort.Strings(limbo)
	return limbo
}

func buildRecoverLoopPhaseOutcomes(readyCount, blockedCount, cycleCount, limboCount int) map[string]string {
	phase1 := "continue"
	if readyCount > 0 {
		phase1 = "ready_found"
	}

	phase2 := "clean"
	if cycleCount > 0 {
		phase2 = "cycles_detected"
	} else if blockedCount > 0 {
		phase2 = "blocked_detected"
	}

	phase3 := "clear"
	if readyCount == 0 && limboCount > 0 {
		phase3 = "limbo_detected"
	}

	return map[string]string{
		"phase_1": phase1,
		"phase_2": phase2,
		"phase_3": phase3,
	}
}

func recoverLoopScopedLabels() []string {
	label := strings.TrimSpace(recoverLoopModuleLabel)
	if label == "" {
		return nil
	}
	return []string{label}
}

func issueIDs(issues []*types.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	return ids
}

func blockedIssueIDs(issues []*types.BlockedIssue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	return ids
}

func resolveRecoverPhase4Outcome(moduleScopeEnabled bool, moduleReadyCount, unscopedReadyCount, unassignedReadyCount int) string {
	switch {
	case moduleScopeEnabled && moduleReadyCount > 0:
		return "module_ready_found"
	case unscopedReadyCount > 0:
		return "unscoped_ready_found"
	case unassignedReadyCount > 0:
		return "unassigned_ready_found"
	default:
		return "no_ready_after_widen"
	}
}

func resolveRecoverLoopResult(phaseOutcomes map[string]string) string {
	if phaseOutcomes["phase_3"] == "limbo_detected" {
		return "recover_limbo_detected"
	}
	if phaseOutcomes["phase_4"] != "no_ready_after_widen" {
		return "recover_ready_found_widened"
	}
	return "recover_continue"
}

func recoverOpenGateIDs(ctx context.Context, parentID *string, limit int) ([]string, []string, error) {
	statusOpen := types.StatusOpen
	typeGate := types.IssueType("gate")
	nativeGates, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Status:    &statusOpen,
		IssueType: &typeGate,
		ParentID:  parentID,
		Limit:     limit,
	})
	if err != nil {
		return nil, nil, err
	}
	nativeIDs := issueIDs(nativeGates)

	fallbackLabel := "type/gate"
	fallbackGates, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Status:   &statusOpen,
		Labels:   []string{fallbackLabel},
		ParentID: parentID,
		Limit:    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	fallbackIDs := issueIDs(fallbackGates)
	return nativeIDs, fallbackIDs, nil
}

func recoverStructuralDiagnostics(ctx context.Context, parentID *string, limit int) ([]string, []string, error) {
	inProgress := types.StatusInProgress
	inProgressIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Status:   &inProgress,
		ParentID: parentID,
		Limit:    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	stale := make([]string, 0)
	now := time.Now().UTC()
	for _, issue := range inProgressIssues {
		if issue == nil {
			continue
		}
		if issue.UpdatedAt.IsZero() {
			continue
		}
		if now.Sub(issue.UpdatedAt.UTC()) > 2*time.Hour {
			stale = append(stale, issue.ID)
		}
	}
	sort.Strings(stale)

	hooked := types.StatusHooked
	hookedIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Status:   &hooked,
		ParentID: parentID,
		Limit:    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	hookedIDs := issueIDs(hookedIssues)
	return stale, hookedIDs, nil
}

func recoverRootBlockersFromBlocked(blocked []*types.BlockedIssue) []string {
	if len(blocked) == 0 || blocked[0] == nil {
		return nil
	}
	root := make([]string, 0, len(blocked[0].BlockedBy))
	for _, blocker := range blocked[0].BlockedBy {
		root = append(root, blocker)
	}
	sort.Strings(root)
	return uniqueSortedStrings(root)
}

func init() {
	recoverCmd.PersistentFlags().StringVar(&recoverStateFrom, "state-from", "", "Current session state for lifecycle transition validation")
	recoverCmd.PersistentFlags().StringVar(&recoverStateTo, "state-to", "", "Target session state for lifecycle transition validation")
	recoverSignatureCmd.Flags().StringVar(&recoverSignatureParentID, "parent", "", "Optional parent scope for blocked-signature computation")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureLimit, "limit", 20, "Maximum blocked issues to include in signature")
	recoverSignatureCmd.Flags().StringVar(&recoverSignaturePrevious, "previous-signature", "", "Previous signature JSON for convergence comparison")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureIteration, "iteration", 0, "Recover-loop iteration count")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureElapsedMinute, "elapsed-minutes", 0, "Recover-loop elapsed minutes")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureMaxIterations, "max-iterations", 3, "Escalation iteration threshold")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureMaxMinutes, "max-minutes", 30, "Escalation elapsed-minutes threshold")
	recoverSignatureCmd.Flags().StringVar(&recoverSignatureAnchorID, "anchor", "", "Optional anchor issue ID to append convergence signature note")
	recoverSignatureCmd.Flags().BoolVar(&recoverSignatureWriteAnchor, "write-anchor", false, "Allow --anchor to append convergence notes (mutating)")
	recoverSignatureCmd.ValidArgsFunction = noCompletions
	recoverLoopCmd.Flags().StringVar(&recoverLoopParentID, "parent", "", "Optional parent scope for recover-loop phase checks")
	recoverLoopCmd.Flags().StringVar(&recoverLoopModuleLabel, "module-label", "", "Optional module/<name> label scope for scoped/module-only ready checks")
	recoverLoopCmd.Flags().IntVar(&recoverLoopLimit, "limit", 20, "Maximum issues per recover-loop phase query")
	recoverLoopCmd.ValidArgsFunction = noCompletions

	recoverCmd.AddCommand(recoverSignatureCmd)
	recoverCmd.AddCommand(recoverLoopCmd)
	rootCmd.AddCommand(recoverCmd)
}

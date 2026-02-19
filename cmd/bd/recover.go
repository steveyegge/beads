package main

import (
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

var (
	recoverSignatureParentID      string
	recoverSignatureLimit         int
	recoverSignaturePrevious      string
	recoverSignatureIteration     int
	recoverSignatureElapsedMinute int
	recoverSignatureMaxIterations int
	recoverSignatureMaxMinutes    int
	recoverLoopParentID           string
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
			},
			Events: []string{"recover_signature_computed"},
		}, exitCode)
	},
}

var recoverLoopCmd = &cobra.Command{
	Use:   "loop",
	Short: "Run deterministic recover-loop phase orchestration",
	Run: func(cmd *cobra.Command, args []string) {
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

		readyIssues, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: parentID,
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

		blockedIssues, err := store.GetBlockedIssues(ctx, types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: parentID,
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

		statusOpen := types.StatusOpen
		openIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{
			Status:   &statusOpen,
			ParentID: parentID,
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

		limboCandidates := detectRecoverLimboCandidates(openIssues, readyIssues, blockedIssues)
		phases := buildRecoverLoopPhaseOutcomes(len(readyIssues), len(blockedIssues), len(cycles), len(limboCandidates))

		result := "recover_continue"
		if phases["phase_1"] == "ready_found" {
			result = "recover_ready_found"
		} else if phases["phase_3"] == "limbo_detected" {
			result = "recover_limbo_detected"
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "recover loop",
			Result:  result,
			Details: map[string]interface{}{
				"phase_outcomes":    phases,
				"ready_count":       len(readyIssues),
				"blocked_count":     len(blockedIssues),
				"cycle_count":       len(cycles),
				"limbo_candidates":  limboCandidates,
				"limbo_count":       len(limboCandidates),
				"scope_parent_id":   recoverLoopParentID,
				"scope_issue_limit": recoverLoopLimit,
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

func init() {
	recoverSignatureCmd.Flags().StringVar(&recoverSignatureParentID, "parent", "", "Optional parent scope for blocked-signature computation")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureLimit, "limit", 20, "Maximum blocked issues to include in signature")
	recoverSignatureCmd.Flags().StringVar(&recoverSignaturePrevious, "previous-signature", "", "Previous signature JSON for convergence comparison")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureIteration, "iteration", 0, "Recover-loop iteration count")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureElapsedMinute, "elapsed-minutes", 0, "Recover-loop elapsed minutes")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureMaxIterations, "max-iterations", 3, "Escalation iteration threshold")
	recoverSignatureCmd.Flags().IntVar(&recoverSignatureMaxMinutes, "max-minutes", 30, "Escalation elapsed-minutes threshold")
	recoverSignatureCmd.ValidArgsFunction = noCompletions
	recoverLoopCmd.Flags().StringVar(&recoverLoopParentID, "parent", "", "Optional parent scope for recover-loop phase checks")
	recoverLoopCmd.Flags().IntVar(&recoverLoopLimit, "limit", 20, "Maximum issues per recover-loop phase query")
	recoverLoopCmd.ValidArgsFunction = noCompletions

	recoverCmd.AddCommand(recoverSignatureCmd)
	recoverCmd.AddCommand(recoverLoopCmd)
	rootCmd.AddCommand(recoverCmd)
}

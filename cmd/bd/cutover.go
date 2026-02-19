package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

type evidenceMatrix struct {
	Items []evidenceMatrixItem `json:"items"`
}

type evidenceMatrixItem struct {
	ChecklistItem      string `json:"checklist_item"`
	Status             string `json:"status"`
	RemediationIssueID string `json:"remediation_issue_id"`
	CommandBehavior    string `json:"command_behavior"`
	CodeLocation       string `json:"code_location"`
	TestProof          string `json:"test_proof"`
}

type unresolvedCutoverGap struct {
	ChecklistItem      string `json:"checklist_item"`
	RemediationIssueID string `json:"remediation_issue_id"`
	RemediationStatus  string `json:"remediation_status"`
}

var (
	cutoverMatrixPath string
)

var cutoverCmd = &cobra.Command{
	Use:     "cutover",
	GroupID: "sync",
	Short:   "Release cutover gate checks",
}

var cutoverGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Fail when evidence matrix has unresolved GAP remediation issues",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		path := strings.TrimSpace(cutoverMatrixPath)
		if path == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "cutover gate",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--matrix is required"},
				Events:  []string{"cutover_failed"},
			}, 1)
			return
		}

		matrix, err := loadEvidenceMatrix(path)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "cutover gate",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"cutover_failed"},
			}, 1)
			return
		}

		unresolved, err := evaluateUnresolvedCutoverGaps(matrix.Items, func(remediationID string) (*types.Issue, error) {
			resolvedID, resolveErr := utils.ResolvePartialID(ctx, store, remediationID)
			if resolveErr != nil {
				return nil, resolveErr
			}
			return store.GetIssue(ctx, resolvedID)
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "cutover gate",
				Result:  "policy_violation",
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"cutover_failed"},
			}, exitCodePolicyViolation)
			return
		}

		if len(unresolved) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "cutover gate",
				Result:  "gate_blocked",
				Details: map[string]interface{}{
					"matrix_path":      path,
					"unresolved_count": len(unresolved),
					"unresolved_gaps":  unresolved,
				},
				RecoveryCommand: "Close remediation issues listed in unresolved_gaps and rerun cutover gate",
				Events:          []string{"cutover_blocked"},
			}, exitCodePolicyViolation)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "cutover gate",
			Result:  "gate_passed",
			Details: map[string]interface{}{
				"matrix_path":     path,
				"evaluated_items": len(matrix.Items),
			},
			Events: []string{"cutover_passed"},
		}, 0)
	},
}

func loadEvidenceMatrix(path string) (*evidenceMatrix, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read evidence matrix %q: %w", path, err)
	}

	var matrix evidenceMatrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		return nil, fmt.Errorf("failed to parse evidence matrix %q: %w", path, err)
	}
	if len(matrix.Items) == 0 {
		return nil, fmt.Errorf("evidence matrix %q has no items", path)
	}
	return &matrix, nil
}

func evaluateUnresolvedCutoverGaps(items []evidenceMatrixItem, lookup func(remediationID string) (*types.Issue, error)) ([]unresolvedCutoverGap, error) {
	unresolved := make([]unresolvedCutoverGap, 0)
	for _, item := range items {
		if strings.TrimSpace(strings.ToUpper(item.Status)) != "GAP" {
			continue
		}
		remediationID := strings.TrimSpace(item.RemediationIssueID)
		if remediationID == "" {
			return nil, fmt.Errorf("GAP item %q is missing remediation_issue_id", strings.TrimSpace(item.ChecklistItem))
		}
		issue, err := lookup(remediationID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve remediation issue %q: %w", remediationID, err)
		}
		if issue == nil {
			unresolved = append(unresolved, unresolvedCutoverGap{
				ChecklistItem:      item.ChecklistItem,
				RemediationIssueID: remediationID,
				RemediationStatus:  "missing",
			})
			continue
		}
		if issue.Status != types.StatusClosed {
			unresolved = append(unresolved, unresolvedCutoverGap{
				ChecklistItem:      item.ChecklistItem,
				RemediationIssueID: remediationID,
				RemediationStatus:  string(issue.Status),
			})
		}
	}
	return unresolved, nil
}

func init() {
	cutoverGateCmd.Flags().StringVar(&cutoverMatrixPath, "matrix", "docs/control-plane/evidence-matrix.json", "Path to evidence matrix JSON file")
	cutoverGateCmd.ValidArgsFunction = noCompletions

	cutoverCmd.AddCommand(cutoverGateCmd)
	rootCmd.AddCommand(cutoverCmd)
}

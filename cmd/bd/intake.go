package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var (
	intakeAuditEpicID        string
	intakeAuditWriteProof    bool
	intakeGuardParentID      string
	intakeGuardLimit         int
	intakeGuardAllowEmpty    bool
	intakePlanningParentID   string
	intakePlanningReadyMin   int
	intakeMapSyncEpicID      string
	intakeMapSyncPlan        []string
	intakeMapSyncReadyWave   string
	intakeMapSyncHasFindings bool
	intakeMapSyncFinding     []string
)

type intakeMapData struct {
	PlanCount    int
	PlanMap      map[int]string
	ReadyWave1   []string
	HasFindings  bool
	FindingCount int
	FindingMap   map[int]string
}

const (
	intakeAuditModeOpenChildren = "open_children"
	intakeAuditModeClosedEpic   = "closed_epic"
)

var intakeCmd = &cobra.Command{
	Use:     "intake",
	GroupID: "deps",
	Short:   "Intake and plan-to-graph contract tools",
}

var intakeAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit intake mapping and dependency/readiness contract",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		epicID := strings.TrimSpace(intakeAuditEpicID)
		if epicID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--epic is required"},
				Events:  []string{"intake_audit_skipped"},
			}, 1)
			return
		}

		epicResult, err := resolveAndGetIssueWithRouting(ctx, store, epicID)
		if err != nil {
			if epicResult != nil {
				epicResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve epic %q: %v", epicID, err)},
				Events:  []string{"intake_audit_skipped"},
			}, 1)
			return
		}
		if epicResult == nil || epicResult.Issue == nil {
			if epicResult != nil {
				epicResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("epic %q not found", epicID)},
				Events:  []string{"intake_audit_skipped"},
			}, 1)
			return
		}
		defer epicResult.Close()

		checks := map[string]string{
			"PLAN_RECONCILE":    "FAIL",
			"CHILD_COUNT":       "FAIL",
			"FINDING_RECONCILE": "N/A",
			"CHILD_LINT":        "FAIL",
			"DEP_CYCLES":        "FAIL",
			"READY_SET":         "FAIL",
		}
		failedChecks := make([]string, 0)
		addFailure := func(name string) {
			failedChecks = append(failedChecks, name)
			checks[name] = "FAIL"
		}

		mapBlock, mapErr := extractIntakeMapBlock(epicResult.Issue.Notes)
		if mapErr != nil {
			addFailure("PLAN_RECONCILE")
			addFailure("CHILD_COUNT")
			addFailure("CHILD_LINT")
			addFailure("DEP_CYCLES")
			addFailure("READY_SET")
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "contract_violation",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{
					"message":       mapErr.Error(),
					"checks":        checks,
					"failed_checks": uniqueIntakeStrings(failedChecks),
				},
				Events: []string{"intake_audit_failed"},
			}, exitCodePolicyViolation)
			return
		}

		parsed, parseErrs := parseIntakeMap(mapBlock)
		if len(parseErrs) > 0 {
			addFailure("PLAN_RECONCILE")
			if parsed.HasFindings {
				addFailure("FINDING_RECONCILE")
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "contract_violation",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{
					"message":       "intake mapping parse failed",
					"errors":        parseErrs,
					"checks":        checks,
					"failed_checks": uniqueIntakeStrings(failedChecks),
				},
				Events: []string{"intake_audit_failed"},
			}, exitCodePolicyViolation)
			return
		}
		checks["PLAN_RECONCILE"] = "PASS"
		if parsed.HasFindings {
			checks["FINDING_RECONCILE"] = "PASS"
		}

		auditMode := resolveIntakeAuditMode(epicResult.Issue.Status)
		childrenFilter := intakeAuditChildrenFilter(epicResult.ResolvedID, auditMode)
		children, err := epicResult.Store.SearchIssues(ctx, "", childrenFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "system_error",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query epic children: %v", err)},
				Events:  []string{"intake_audit_failed"},
			}, 1)
			return
		}
		if len(children) == parsed.PlanCount {
			checks["CHILD_COUNT"] = "PASS"
		} else {
			addFailure("CHILD_COUNT")
		}

		childIDs := make(map[string]struct{}, len(children))
		for _, child := range children {
			childIDs[child.ID] = struct{}{}
		}

		planIDs := valuesFromNumberMap(parsed.PlanMap)
		if !allIDsInSet(planIDs, childIDs) {
			addFailure("PLAN_RECONCILE")
		}

		if parsed.HasFindings {
			findingIDs := valuesFromNumberMap(parsed.FindingMap)
			if !allIDsInSet(findingIDs, childIDs) {
				addFailure("FINDING_RECONCILE")
			}
		} else {
			checks["FINDING_RECONCILE"] = "N/A"
		}

		verifyHeadingRe := regexp.MustCompile(`(?m)^## Verify$`)
		childLintOK := true
		for _, child := range children {
			if strings.TrimSpace(child.Description) == "" {
				childLintOK = false
				break
			}
			if strings.TrimSpace(child.AcceptanceCriteria) == "" {
				childLintOK = false
				break
			}
			if !verifyHeadingRe.MatchString(child.Description) {
				childLintOK = false
				break
			}
		}
		if childLintOK {
			checks["CHILD_LINT"] = "PASS"
		} else {
			addFailure("CHILD_LINT")
		}

		cycles, err := epicResult.Store.DetectCycles(ctx)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "system_error",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("cycle check failed: %v", err)},
				Events:  []string{"intake_audit_failed"},
			}, 1)
			return
		}
		if len(cycles) == 0 {
			checks["DEP_CYCLES"] = "PASS"
		} else {
			addFailure("DEP_CYCLES")
		}

		expectedReady := uniqueSortedIntakeStrings(parsed.ReadyWave1)
		readyFilter := types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: &epicResult.ResolvedID,
			Limit:    0,
		}
		readyIssues, err := epicResult.Store.GetReadyWork(ctx, readyFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "system_error",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("ready set query failed: %v", err)},
				Events:  []string{"intake_audit_failed"},
			}, 1)
			return
		}
		actualReady := make([]string, 0, len(readyIssues))
		for _, issue := range readyIssues {
			actualReady = append(actualReady, issue.ID)
		}
		actualReady = uniqueSortedIntakeStrings(actualReady)
		if equalStringSets(expectedReady, actualReady) {
			checks["READY_SET"] = "PASS"
		} else if intakeAuditReadySetRequired(auditMode) {
			addFailure("READY_SET")
		} else {
			checks["READY_SET"] = "N/A"
		}

		failedChecks = uniqueIntakeStrings(failedChecks)
		if len(failedChecks) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake audit",
				Result:  "contract_violation",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{
					"checks":         checks,
					"failed_checks":  failedChecks,
					"expected_ready": expectedReady,
					"actual_ready":   actualReady,
					"child_count":    len(children),
					"plan_count":     parsed.PlanCount,
					"audit_mode":     auditMode,
				},
				Events: []string{"intake_audit_failed"},
			}, exitCodePolicyViolation)
			return
		}

		if intakeAuditWriteProof {
			proof := buildIntakeProofBlock(parsed.HasFindings)
			updatedNotes := appendNotesLine(epicResult.Issue.Notes, proof)
			if err := epicResult.Store.UpdateIssue(ctx, epicResult.ResolvedID, map[string]interface{}{"notes": updatedNotes}, actor); err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "intake audit",
					Result:  "system_error",
					IssueID: epicResult.ResolvedID,
					Details: map[string]interface{}{
						"message": fmt.Sprintf("failed to write intake proof: %v", err),
					},
					Events: []string{"intake_audit_failed"},
				}, 1)
				return
			}
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "intake audit",
			Result:  "pass",
			IssueID: epicResult.ResolvedID,
			Details: map[string]interface{}{
				"checks":         checks,
				"write_proof":    intakeAuditWriteProof,
				"child_count":    len(children),
				"plan_count":     parsed.PlanCount,
				"expected_ready": expectedReady,
				"audit_mode":     auditMode,
			},
			Events: []string{"intake_audit_passed"},
		}, 0)
	},
}

func resolveIntakeAuditMode(epicStatus types.Status) string {
	if epicStatus == types.StatusClosed {
		return intakeAuditModeClosedEpic
	}
	return intakeAuditModeOpenChildren
}

func intakeAuditChildrenFilter(parentID, mode string) types.IssueFilter {
	filter := types.IssueFilter{
		ParentID: &parentID,
		Limit:    0,
	}
	if mode == intakeAuditModeOpenChildren {
		statusOpen := types.StatusOpen
		filter.Status = &statusOpen
	}
	return filter
}

func intakeAuditReadySetRequired(mode string) bool {
	return mode == intakeAuditModeOpenChildren
}

var intakeBulkGuardCmd = &cobra.Command{
	Use:   "bulk-guard",
	Short: "Run deterministic bulk-write safety guard checks",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		parentID := strings.TrimSpace(intakeGuardParentID)
		if parentID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake bulk-guard",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--parent is required"},
				Events:  []string{"intake_guard_failed"},
			}, 1)
			return
		}
		resolvedParent, err := utils.ResolvePartialID(ctx, store, parentID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake bulk-guard",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve parent %q: %v", parentID, err)},
				Events:  []string{"intake_guard_failed"},
			}, 1)
			return
		}

		cycles, err := store.DetectCycles(ctx)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake bulk-guard",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("cycle check failed: %v", err)},
				Events:  []string{"intake_guard_failed"},
			}, 1)
			return
		}

		readyFilter := types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: &resolvedParent,
			Limit:    intakeGuardLimit,
		}
		readyIssues, err := store.GetReadyWork(ctx, readyFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake bulk-guard",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("ready query failed: %v", err)},
				Events:  []string{"intake_guard_failed"},
			}, 1)
			return
		}

		pass, violations := evaluateBulkWriteGuard(len(cycles), len(readyIssues), intakeGuardAllowEmpty)
		if !pass {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake bulk-guard",
				Result:  "policy_violation",
				IssueID: resolvedParent,
				Details: map[string]interface{}{
					"violations":  violations,
					"cycle_count": len(cycles),
					"ready_count": len(readyIssues),
				},
				RecoveryCommand: fmt.Sprintf("bd dep cycles && bd ready --parent %s --limit %d", resolvedParent, intakeGuardLimit),
				Events:          []string{"intake_guard_failed"},
			}, exitCodePolicyViolation)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "intake bulk-guard",
			Result:  "pass",
			IssueID: resolvedParent,
			Details: map[string]interface{}{
				"cycle_count": len(cycles),
				"ready_count": len(readyIssues),
			},
			Events: []string{"intake_guard_passed"},
		}, 0)
	},
}

var intakePlanningExitCmd = &cobra.Command{
	Use:   "planning-exit",
	Short: "Run deterministic planning-exit structure/readiness audit",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		parentID := strings.TrimSpace(intakePlanningParentID)
		if parentID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake planning-exit",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--parent is required"},
				Events:  []string{"planning_exit_failed"},
			}, 1)
			return
		}
		resolvedParent, err := utils.ResolvePartialID(ctx, store, parentID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake planning-exit",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve parent %q: %v", parentID, err)},
				Events:  []string{"planning_exit_failed"},
			}, 1)
			return
		}

		statusOpen := types.StatusOpen
		filter := types.IssueFilter{
			Status:   &statusOpen,
			ParentID: &resolvedParent,
			Limit:    0,
		}
		children, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake planning-exit",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query children: %v", err)},
				Events:  []string{"planning_exit_failed"},
			}, 1)
			return
		}

		checks := map[string]string{
			"CHILDREN_PRESENT": "FAIL",
			"CHILD_LINT":       "FAIL",
			"DEP_CYCLES":       "FAIL",
			"READY_WAVE":       "FAIL",
			"INTEGRATION_EDGE": "FAIL",
		}
		failed := make([]string, 0)
		recordFail := func(name string) {
			checks[name] = "FAIL"
			failed = append(failed, name)
		}

		if len(children) > 0 {
			checks["CHILDREN_PRESENT"] = "PASS"
		} else {
			recordFail("CHILDREN_PRESENT")
		}

		verifyHeadingRe := regexp.MustCompile(`(?m)^## Verify$`)
		childLintOK := true
		for _, child := range children {
			if strings.TrimSpace(child.Description) == "" ||
				strings.TrimSpace(child.AcceptanceCriteria) == "" ||
				!verifyHeadingRe.MatchString(child.Description) {
				childLintOK = false
				break
			}
		}
		if childLintOK {
			checks["CHILD_LINT"] = "PASS"
		} else {
			recordFail("CHILD_LINT")
		}

		cycles, err := store.DetectCycles(ctx)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake planning-exit",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("cycle check failed: %v", err)},
				Events:  []string{"planning_exit_failed"},
			}, 1)
			return
		}
		if len(cycles) == 0 {
			checks["DEP_CYCLES"] = "PASS"
		} else {
			recordFail("DEP_CYCLES")
		}

		ready, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status:   types.StatusOpen,
			ParentID: &resolvedParent,
			Limit:    0,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake planning-exit",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("ready query failed: %v", err)},
				Events:  []string{"planning_exit_failed"},
			}, 1)
			return
		}
		if len(ready) >= intakePlanningReadyMin {
			checks["READY_WAVE"] = "PASS"
		} else {
			recordFail("READY_WAVE")
		}

		depsByIssue := make(map[string][]*types.Dependency, len(children))
		childrenByID := make(map[string]*types.Issue, len(children))
		for _, child := range children {
			childrenByID[child.ID] = child
			deps, err := store.GetDependencyRecords(ctx, child.ID)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "intake planning-exit",
					Result:  "system_error",
					Details: map[string]interface{}{"message": fmt.Sprintf("dependency query failed for %s: %v", child.ID, err)},
					Events:  []string{"planning_exit_failed"},
				}, 1)
				return
			}
			depsByIssue[child.ID] = deps
		}
		edgeViolations := detectDirectCrossModuleBlocks(childrenByID, depsByIssue)
		if len(edgeViolations) == 0 {
			checks["INTEGRATION_EDGE"] = "PASS"
		} else {
			recordFail("INTEGRATION_EDGE")
		}

		failed = uniqueIntakeStrings(failed)
		if len(failed) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake planning-exit",
				Result:  "contract_violation",
				IssueID: resolvedParent,
				Details: map[string]interface{}{
					"checks":          checks,
					"failed_checks":   failed,
					"edge_violations": edgeViolations,
					"child_count":     len(children),
					"ready_count":     len(ready),
				},
				Events: []string{"planning_exit_failed"},
			}, exitCodePolicyViolation)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "intake planning-exit",
			Result:  "pass",
			IssueID: resolvedParent,
			Details: map[string]interface{}{
				"checks":      checks,
				"child_count": len(children),
				"ready_count": len(ready),
			},
			Events: []string{"planning_exit_passed"},
		}, 0)
	},
}

var intakeMapSyncCmd = &cobra.Command{
	Use:   "map-sync",
	Short: "Rewrite canonical INTAKE-MAP block in parent epic notes",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		epicID := strings.TrimSpace(intakeMapSyncEpicID)
		if epicID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--epic is required"},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}

		epicResult, err := resolveAndGetIssueWithRouting(ctx, store, epicID)
		if err != nil {
			if epicResult != nil {
				epicResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve epic %q: %v", epicID, err)},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}
		if epicResult == nil || epicResult.Issue == nil {
			if epicResult != nil {
				epicResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("epic %q not found", epicID)},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}
		defer epicResult.Close()

		planMap, err := parseIndexedIntakeMappings(intakeMapSyncPlan, "PLAN")
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}
		if len(planMap) == 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": "at least one --plan mapping is required"},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}

		findingMap, err := parseIndexedIntakeMappings(intakeMapSyncFinding, "FINDING")
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}
		if !intakeMapSyncHasFindings && len(findingMap) > 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": "--finding mappings require --has-findings"},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}

		readyWave := splitIntakeWaveCSV(intakeMapSyncReadyWave)
		if len(readyWave) == 0 {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "invalid_input",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": "--ready-wave is required and must include at least one issue ID"},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}

		mapBlock := buildCanonicalIntakeMapBlock(planMap, readyWave, intakeMapSyncHasFindings, findingMap)
		updatedNotes := upsertIntakeMapBlock(epicResult.Issue.Notes, mapBlock)
		if err := epicResult.Store.UpdateIssue(ctx, epicResult.ResolvedID, map[string]interface{}{"notes": updatedNotes}, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "intake map-sync",
				Result:  "system_error",
				IssueID: epicResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to update epic notes: %v", err)},
				Events:  []string{"intake_map_sync_failed"},
			}, 1)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "intake map-sync",
			Result:  "synced",
			IssueID: epicResult.ResolvedID,
			Details: map[string]interface{}{
				"plan_count":         len(planMap),
				"ready_wave_count":   len(readyWave),
				"input_has_findings": intakeMapSyncHasFindings,
				"finding_count":      len(findingMap),
			},
			Events: []string{"intake_map_synced"},
		}, 0)
	},
}

func extractIntakeMapBlock(notes string) (string, error) {
	lines := strings.Split(notes, "\n")
	beginCount := 0
	endCount := 0
	capturing := false
	captured := make([]string, 0)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "INTAKE-MAP-BEGIN" {
			beginCount++
			capturing = true
			captured = make([]string, 0)
			continue
		}
		if trimmed == "INTAKE-MAP-END" {
			endCount++
			capturing = false
			continue
		}
		if capturing {
			captured = append(captured, line)
		}
	}

	if beginCount != 1 || endCount != 1 {
		return "", fmt.Errorf("intake map must contain exactly one INTAKE-MAP-BEGIN/INTAKE-MAP-END block")
	}
	if len(captured) == 0 {
		return "", fmt.Errorf("intake map block is empty")
	}
	return strings.Join(captured, "\n"), nil
}

func parseIntakeMap(block string) (intakeMapData, []string) {
	data := intakeMapData{
		PlanMap:    map[int]string{},
		FindingMap: map[int]string{},
	}
	errorsList := make([]string, 0)

	planCountRe := regexp.MustCompile(`^PLAN-COUNT:\s*([0-9]+)\s*$`)
	planMapRe := regexp.MustCompile(`^PLAN-([0-9]+)\s*->\s*([^\s]+)\s*$`)
	readyWaveRe := regexp.MustCompile(`^READY-WAVE-1:\s*(.*)$`)
	hasFindingsRe := regexp.MustCompile(`^INPUT-HAS-FINDINGS:\s*(true|false)\s*$`)
	findingCountRe := regexp.MustCompile(`^FINDING-COUNT:\s*([0-9]+)\s*$`)
	findingMapRe := regexp.MustCompile(`^FINDING-([0-9]+)\s*->\s*([^\s]+)\s*$`)

	hasPlanCount := false
	hasFindingsFlag := false
	hasReadyWave := false

	lines := strings.Split(block, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if m := planCountRe.FindStringSubmatch(line); len(m) == 2 {
			n, _ := strconv.Atoi(m[1])
			data.PlanCount = n
			hasPlanCount = true
			continue
		}
		if m := planMapRe.FindStringSubmatch(line); len(m) == 3 {
			idx, _ := strconv.Atoi(m[1])
			data.PlanMap[idx] = m[2]
			continue
		}
		if m := readyWaveRe.FindStringSubmatch(line); len(m) == 2 {
			hasReadyWave = true
			parts := strings.Split(m[1], ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					data.ReadyWave1 = append(data.ReadyWave1, trimmed)
				}
			}
			continue
		}
		if m := hasFindingsRe.FindStringSubmatch(line); len(m) == 2 {
			hasFindingsFlag = true
			data.HasFindings = m[1] == "true"
			continue
		}
		if m := findingCountRe.FindStringSubmatch(line); len(m) == 2 {
			n, _ := strconv.Atoi(m[1])
			data.FindingCount = n
			continue
		}
		if m := findingMapRe.FindStringSubmatch(line); len(m) == 3 {
			idx, _ := strconv.Atoi(m[1])
			data.FindingMap[idx] = m[2]
			continue
		}
	}

	if !hasPlanCount {
		errorsList = append(errorsList, "missing PLAN-COUNT")
	}
	if !hasFindingsFlag {
		errorsList = append(errorsList, "missing INPUT-HAS-FINDINGS")
	}
	if !hasReadyWave {
		errorsList = append(errorsList, "missing READY-WAVE-1")
	}

	if len(data.PlanMap) != data.PlanCount {
		errorsList = append(errorsList, "PLAN mapping cardinality mismatch")
	}
	if !isContiguousMapKeys(data.PlanMap, data.PlanCount) {
		errorsList = append(errorsList, "PLAN keys must be contiguous PLAN-1..PLAN-N")
	}
	if !hasUniqueMapValues(data.PlanMap) {
		errorsList = append(errorsList, "PLAN values must be unique")
	}

	if data.HasFindings {
		if len(data.FindingMap) != data.FindingCount {
			errorsList = append(errorsList, "FINDING mapping cardinality mismatch")
		}
		if !isContiguousMapKeys(data.FindingMap, data.FindingCount) {
			errorsList = append(errorsList, "FINDING keys must be contiguous FINDING-1..FINDING-N")
		}
		if !hasUniqueMapValues(data.FindingMap) {
			errorsList = append(errorsList, "FINDING values must be unique")
		}
	} else {
		if data.FindingCount != 0 || len(data.FindingMap) > 0 {
			errorsList = append(errorsList, "FINDING lines must be omitted when INPUT-HAS-FINDINGS=false")
		}
	}

	data.ReadyWave1 = uniqueSortedIntakeStrings(data.ReadyWave1)
	if len(data.ReadyWave1) == 0 {
		errorsList = append(errorsList, "READY-WAVE-1 must list at least one issue ID")
	}

	return data, errorsList
}

func parseIndexedIntakeMappings(entries []string, kind string) (map[int]string, error) {
	parsed := make(map[int]string, len(entries))
	for _, entry := range entries {
		parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid %s mapping %q (expected <index>:<id>)", kind, entry)
		}
		indexRaw := strings.TrimSpace(parts[0])
		id := strings.TrimSpace(parts[1])
		if indexRaw == "" || id == "" {
			return nil, fmt.Errorf("invalid %s mapping %q (empty index or id)", kind, entry)
		}
		index, err := strconv.Atoi(indexRaw)
		if err != nil || index <= 0 {
			return nil, fmt.Errorf("invalid %s index %q (must be positive integer)", kind, indexRaw)
		}
		parsed[index] = id
	}
	return parsed, nil
}

func splitIntakeWaveCSV(raw string) []string {
	items := strings.Split(raw, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return uniqueSortedIntakeStrings(out)
}

func buildCanonicalIntakeMapBlock(planMap map[int]string, readyWave []string, hasFindings bool, findingMap map[int]string) string {
	lines := []string{"INTAKE-MAP-BEGIN"}
	lines = append(lines, fmt.Sprintf("PLAN-COUNT: %d", len(planMap)))
	for _, idx := range sortedIntakeMapKeys(planMap) {
		lines = append(lines, fmt.Sprintf("PLAN-%d -> %s", idx, planMap[idx]))
	}
	lines = append(lines, fmt.Sprintf("READY-WAVE-1: %s", strings.Join(uniqueSortedIntakeStrings(readyWave), ",")))
	lines = append(lines, fmt.Sprintf("INPUT-HAS-FINDINGS: %t", hasFindings))
	if hasFindings {
		lines = append(lines, fmt.Sprintf("FINDING-COUNT: %d", len(findingMap)))
		for _, idx := range sortedIntakeMapKeys(findingMap) {
			lines = append(lines, fmt.Sprintf("FINDING-%d -> %s", idx, findingMap[idx]))
		}
	}
	lines = append(lines, "INTAKE-MAP-END")
	return strings.Join(lines, "\n")
}

func upsertIntakeMapBlock(existingNotes, canonicalBlock string) string {
	lines := strings.Split(existingNotes, "\n")
	kept := make([]string, 0, len(lines))
	inMap := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "INTAKE-MAP-BEGIN" {
			inMap = true
			continue
		}
		if inMap && trimmed == "INTAKE-MAP-END" {
			inMap = false
			continue
		}
		if inMap {
			continue
		}
		kept = append(kept, line)
	}
	base := strings.TrimSpace(strings.Join(kept, "\n"))
	if base == "" {
		return canonicalBlock
	}
	return strings.TrimRight(base, "\n") + "\n" + canonicalBlock
}

func sortedIntakeMapKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func isContiguousMapKeys(m map[int]string, expectedCount int) bool {
	if expectedCount == 0 {
		return len(m) == 0
	}
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	if len(keys) != expectedCount {
		return false
	}
	for i := 0; i < expectedCount; i++ {
		if keys[i] != i+1 {
			return false
		}
	}
	return true
}

func hasUniqueMapValues(m map[int]string) bool {
	seen := map[string]struct{}{}
	for _, v := range m {
		if _, ok := seen[v]; ok {
			return false
		}
		seen[v] = struct{}{}
	}
	return true
}

func valuesFromNumberMap(m map[int]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func allIDsInSet(ids []string, set map[string]struct{}) bool {
	for _, id := range ids {
		if _, ok := set[id]; !ok {
			return false
		}
	}
	return true
}

func uniqueIntakeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueSortedIntakeStrings(values []string) []string {
	return uniqueIntakeStrings(values)
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}

func evaluateBulkWriteGuard(cycleCount, readyCount int, allowEmptyReady bool) (bool, []string) {
	violations := make([]string, 0)
	if cycleCount > 0 {
		violations = append(violations, "dep_cycles.present")
	}
	if readyCount == 0 && !allowEmptyReady {
		violations = append(violations, "ready_set.empty")
	}
	return len(violations) == 0, violations
}

func detectDirectCrossModuleBlocks(childrenByID map[string]*types.Issue, depsByIssue map[string][]*types.Dependency) []string {
	violations := make([]string, 0)
	seen := make(map[string]struct{})

	for issueID, deps := range depsByIssue {
		issue := childrenByID[issueID]
		if issue == nil || isIntegrationGateIssue(issue) {
			continue
		}
		issueModule := moduleLabel(issue)
		if issueModule == "" {
			continue
		}

		for _, dep := range deps {
			if dep == nil || dep.Type != types.DepBlocks {
				continue
			}
			blocker := childrenByID[dep.DependsOnID]
			if blocker == nil || isIntegrationGateIssue(blocker) {
				continue
			}
			blockerModule := moduleLabel(blocker)
			if blockerModule == "" || blockerModule == issueModule {
				continue
			}

			key := fmt.Sprintf("%s|%s", issue.ID, blocker.ID)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			violations = append(violations, key)
		}
	}

	sort.Strings(violations)
	return violations
}

func moduleLabel(issue *types.Issue) string {
	if issue == nil {
		return ""
	}
	for _, label := range issue.Labels {
		trimmed := strings.TrimSpace(label)
		if strings.HasPrefix(trimmed, "module/") {
			return trimmed
		}
	}
	return ""
}

func isIntegrationGateIssue(issue *types.Issue) bool {
	if issue == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(string(issue.IssueType)), "gate") {
		return true
	}
	for _, label := range issue.Labels {
		if strings.EqualFold(strings.TrimSpace(label), "type/gate") {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(issue.Title)), "gate:")
}

func buildIntakeProofBlock(hasFindings bool) string {
	finding := "FINDING_RECONCILE=N/A"
	if hasFindings {
		finding = "FINDING_RECONCILE=PASS"
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	lines := []string{
		fmt.Sprintf("INTAKE-PROOF: %s", ts),
		"PLAN_RECONCILE=PASS",
		"CHILD_COUNT=PASS",
		finding,
		"CHILD_LINT=PASS",
		"DEP_CYCLES=PASS",
		"READY_SET=PASS",
		"INTAKE_AUDIT=PASS",
	}
	return strings.Join(lines, "\n")
}

func init() {
	intakeAuditCmd.Flags().StringVar(&intakeAuditEpicID, "epic", "", "Parent epic ID to audit")
	intakeAuditCmd.Flags().BoolVar(&intakeAuditWriteProof, "write-proof", false, "Append INTAKE_AUDIT=PASS proof block on success")
	intakeAuditCmd.ValidArgsFunction = noCompletions
	intakeBulkGuardCmd.Flags().StringVar(&intakeGuardParentID, "parent", "", "Parent epic/slice ID to scope ready check")
	intakeBulkGuardCmd.Flags().IntVar(&intakeGuardLimit, "limit", 20, "Ready check limit used by guard")
	intakeBulkGuardCmd.Flags().BoolVar(&intakeGuardAllowEmpty, "allow-empty-ready", false, "Allow empty ready set without failing guard")
	intakeBulkGuardCmd.ValidArgsFunction = noCompletions
	intakePlanningExitCmd.Flags().StringVar(&intakePlanningParentID, "parent", "", "Parent epic/slice ID to audit")
	intakePlanningExitCmd.Flags().IntVar(&intakePlanningReadyMin, "ready-min", 1, "Minimum ready issue count required for planning-exit pass")
	intakePlanningExitCmd.ValidArgsFunction = noCompletions
	intakeMapSyncCmd.Flags().StringVar(&intakeMapSyncEpicID, "epic", "", "Epic ID to rewrite canonical INTAKE-MAP block for")
	intakeMapSyncCmd.Flags().StringArrayVar(&intakeMapSyncPlan, "plan", nil, "PLAN mapping entry in form <index>:<id> (repeat flag)")
	intakeMapSyncCmd.Flags().StringVar(&intakeMapSyncReadyWave, "ready-wave", "", "READY-WAVE-1 CSV list (for example: bd-1,bd-2)")
	intakeMapSyncCmd.Flags().BoolVar(&intakeMapSyncHasFindings, "has-findings", false, "Set INPUT-HAS-FINDINGS=true in canonical map")
	intakeMapSyncCmd.Flags().StringArrayVar(&intakeMapSyncFinding, "finding", nil, "FINDING mapping entry in form <index>:<id> (repeat flag)")
	intakeMapSyncCmd.ValidArgsFunction = noCompletions

	intakeCmd.AddCommand(intakeAuditCmd)
	intakeCmd.AddCommand(intakeBulkGuardCmd)
	intakeCmd.AddCommand(intakePlanningExitCmd)
	intakeCmd.AddCommand(intakeMapSyncCmd)
	rootCmd.AddCommand(intakeCmd)
}

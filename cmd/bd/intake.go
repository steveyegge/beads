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
)

var (
	intakeAuditEpicID     string
	intakeAuditWriteProof bool
)

type intakeMapData struct {
	PlanCount    int
	PlanMap      map[int]string
	ReadyWave1   []string
	HasFindings  bool
	FindingCount int
	FindingMap   map[int]string
}

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

		statusOpen := types.StatusOpen
		childrenFilter := types.IssueFilter{
			Status:   &statusOpen,
			ParentID: &epicResult.ResolvedID,
			Limit:    0,
		}
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
		} else {
			addFailure("READY_SET")
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
			},
			Events: []string{"intake_audit_passed"},
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

	intakeCmd.AddCommand(intakeAuditCmd)
	rootCmd.AddCommand(intakeCmd)
}

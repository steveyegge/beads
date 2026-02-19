package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func captureEmitEnvelopeJSON(t *testing.T, payload commandEnvelope) map[string]interface{} {
	t.Helper()

	oldJSON := jsonOutput
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	jsonOutput = true
	os.Stdout = w
	t.Cleanup(func() {
		jsonOutput = oldJSON
		os.Stdout = oldStdout
	})

	emitEnvelope(payload)

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read envelope output: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse envelope JSON: %v\nraw=%s", err, string(raw))
	}
	return out
}

func TestControlPlaneJSONEnvelope(t *testing.T) {
	envelopeType := reflect.TypeOf(commandEnvelope{})
	expectedTags := map[string]string{
		"OK":              "ok",
		"Command":         "command",
		"Result":          "result",
		"IssueID":         "issue_id",
		"Details":         "details",
		"RecoveryCommand": "recovery_command",
		"Events":          "events",
	}

	for fieldName, wantTag := range expectedTags {
		field, ok := envelopeType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("commandEnvelope missing field %s", fieldName)
		}
		gotTag := field.Tag.Get("json")
		if gotTag != wantTag {
			t.Fatalf("field %s json tag = %q, want %q", fieldName, gotTag, wantTag)
		}
		if strings.Contains(gotTag, "omitempty") {
			t.Fatalf("field %s must not use omitempty: %q", fieldName, gotTag)
		}
	}

	out := captureEmitEnvelopeJSON(t, commandEnvelope{
		OK:      true,
		Command: "reason lint",
		Result:  "ok",
	})

	requiredKeys := []string{"ok", "command", "result", "issue_id", "details", "recovery_command", "events"}
	for _, key := range requiredKeys {
		if _, ok := out[key]; !ok {
			t.Fatalf("envelope output missing key %q: %#v", key, out)
		}
	}
}

func TestCreateFileHonorsParentAssignment(t *testing.T) {
	root := findRepoRootForContractTest(t)
	createSource, err := os.ReadFile(filepath.Join(root, "cmd", "bd", "create.go"))
	if err != nil {
		t.Fatalf("read create.go: %v", err)
	}
	createText := string(createSource)
	if !strings.Contains(createText, `createIssuesFromMarkdown(cmd, file, strings.TrimSpace(parentID))`) {
		t.Fatalf("create --file path must pass --parent into markdown intake")
	}

	markdownSource, err := os.ReadFile(filepath.Join(root, "cmd", "bd", "markdown.go"))
	if err != nil {
		t.Fatalf("read markdown.go: %v", err)
	}
	markdownText := string(markdownSource)
	requiredTokens := []string{
		`resolvedParentID`,
		`types.DepParentChild`,
		`"parent_link_failures"`,
		`PARENT_LINK_FAILURE`,
	}
	for _, token := range requiredTokens {
		if !strings.Contains(markdownText, token) {
			t.Fatalf("markdown intake missing required parent-link token %q", token)
		}
	}
}

func TestEvidenceAddCommandAndTupleValidation(t *testing.T) {
	if evidenceCmd == nil {
		t.Fatalf("evidence command is nil")
	}
	if evidenceAddCmd == nil {
		t.Fatalf("evidence add command is nil")
	}
	if evidenceAddCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("evidence add missing --issue flag")
	}
	if evidenceAddCmd.Flags().Lookup("env-id") == nil {
		t.Fatalf("evidence add missing --env-id flag")
	}
	if evidenceAddCmd.Flags().Lookup("artifact-id") == nil {
		t.Fatalf("evidence add missing --artifact-id flag")
	}
	if evidenceAddCmd.Flags().Lookup("ts") == nil {
		t.Fatalf("evidence add missing --ts flag")
	}
	if closeCmd.Flags().Lookup("require-evidence-tuple") == nil {
		t.Fatalf("close command missing --require-evidence-tuple flag")
	}
	if closeCmd.Flags().Lookup("evidence-max-age") == nil {
		t.Fatalf("close command missing --evidence-max-age flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("require-evidence-tuple") == nil {
		t.Fatalf("flow close-safe missing --require-evidence-tuple flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("evidence-max-age") == nil {
		t.Fatalf("flow close-safe missing --evidence-max-age flag")
	}

	line, err := canonicalEvidenceTupleLine(evidenceTuple{
		TS:         "2026-02-19T00:00:00Z",
		EnvID:      "ci/linux",
		ArtifactID: "run-001",
	})
	if err != nil {
		t.Fatalf("canonicalEvidenceTupleLine failed: %v", err)
	}
	if !strings.HasPrefix(line, evidenceTuplePrefix) {
		t.Fatalf("tuple line missing prefix: %q", line)
	}

	notes := appendNotesLine("", line)
	freshNow := time.Date(2026, 2, 19, 0, 30, 0, 0, time.UTC)
	if err := validateEvidenceTupleNotes(notes, freshNow, time.Hour); err != nil {
		t.Fatalf("expected fresh tuple validation to pass: %v", err)
	}

	staleNow := time.Date(2026, 2, 19, 2, 30, 0, 0, time.UTC)
	if err := validateEvidenceTupleNotes(notes, staleNow, time.Hour); err == nil {
		t.Fatalf("expected stale tuple validation to fail")
	}
}

func TestPreflightGateEnforcement(t *testing.T) {
	if preflightGateCmd == nil {
		t.Fatalf("preflight gate command is nil")
	}
	if preflightGateCmd.Flags().Lookup("action") == nil {
		t.Fatalf("preflight gate missing --action flag")
	}
	if preflightGateCmd.Flags().Lookup("skip-wip-gate") == nil {
		t.Fatalf("preflight gate missing --skip-wip-gate flag")
	}
	if preflightGateCmd.Flags().Lookup("remediate-hardening") == nil {
		t.Fatalf("preflight gate missing --remediate-hardening flag")
	}

	pass := evaluatePreflightGate(
		"claim",
		"error",
		true,
		[]doctorCheck{{Name: "Repo Fingerprint", Status: statusOK}},
		0,
		true,
	)
	if !pass.Pass {
		t.Fatalf("expected healthy preflight to pass, blockers=%v", pass.Blockers)
	}

	hardeningBlocked := evaluatePreflightGate(
		"claim",
		"warn",
		false,
		[]doctorCheck{{Name: "Repo Fingerprint", Status: statusOK}},
		0,
		true,
	)
	if hardeningBlocked.Pass {
		t.Fatalf("expected hardening mismatch to block")
	}
	if !strings.Contains(strings.Join(hardeningBlocked.Blockers, ","), "hardening.validation.on-create") {
		t.Fatalf("expected validation hardening blocker, got %v", hardeningBlocked.Blockers)
	}

	criticalWarnBlocked := evaluatePreflightGate(
		"claim",
		"error",
		true,
		[]doctorCheck{{Name: "DB-JSONL Sync", Status: statusWarning}},
		0,
		true,
	)
	if criticalWarnBlocked.Pass {
		t.Fatalf("expected critical doctor warning to block")
	}

	wipBlocked := evaluatePreflightGate(
		"claim",
		"error",
		true,
		[]doctorCheck{{Name: "Repo Fingerprint", Status: statusOK}},
		1,
		true,
	)
	if wipBlocked.Pass {
		t.Fatalf("expected wip gate to block")
	}
}

func TestPreflightGateRecoveryCommand(t *testing.T) {
	assessment := preflightGateAssessment{Blockers: []string{"hardening.validation.on-create"}}
	if got := preflightRecoveryCommand(assessment, false); got != "bd preflight gate --action claim --remediate-hardening" {
		t.Fatalf("expected hardening remediation hint, got %q", got)
	}
	if got := preflightRecoveryCommand(assessment, true); got != "bd preflight gate --action claim" {
		t.Fatalf("expected plain recovery when remediation already requested, got %q", got)
	}
	if got := preflightRecoveryCommand(preflightGateAssessment{Blockers: []string{"wip.gate"}}, false); got != "bd preflight gate --action claim" {
		t.Fatalf("expected plain recovery for non-hardening blockers, got %q", got)
	}
}

func TestPreflightGateUsesBoundedDiagnostics(t *testing.T) {
	root := findRepoRootForContractTest(t)
	preflightPath := filepath.Join(root, "cmd", "bd", "preflight.go")
	data, err := os.ReadFile(preflightPath)
	if err != nil {
		t.Fatalf("read preflight.go: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "runPreflightGateChecks(ctx, workingPath)") {
		t.Fatalf("preflight gate must use bounded check helper")
	}
	if strings.Contains(text, "runDiagnostics(workingPath)") {
		t.Fatalf("preflight gate must not invoke full doctor diagnostics")
	}

	requiredTokens := []string{
		"preflightCheckRepoFingerprint(ctx)",
		"doctor.CheckDatabaseConfig(path)",
		"doctor.CheckSyncDivergence(path)",
		"doctor.CheckIssuesTracking()",
		"preflightCheckReadyQueue(ctx)",
	}
	for _, token := range requiredTokens {
		if !strings.Contains(text, token) {
			t.Fatalf("bounded preflight checks missing token %q", token)
		}
	}
}

func TestLandUsesBoundedCriticalDiagnostics(t *testing.T) {
	root := findRepoRootForContractTest(t)
	landPath := filepath.Join(root, "cmd", "bd", "land.go")
	data, err := os.ReadFile(landPath)
	if err != nil {
		t.Fatalf("read land.go: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "runPreflightGateChecks(ctx, workingPath)") {
		t.Fatalf("land critical warning evaluation must use bounded preflight checks")
	}
	if strings.Contains(text, "runDiagnostics(workingPath)") {
		t.Fatalf("land critical warning evaluation must not invoke full doctor diagnostics")
	}
}

func TestBootstrapAutoSetsHardeningInvariant(t *testing.T) {
	updates := make(map[string]string)
	setter := func(key, value string) error {
		updates[key] = value
		return nil
	}
	validation, requireDescription, remediated, err := remediateHardeningInvariant("warn", false, setter)
	if err != nil {
		t.Fatalf("expected hardening remediation to succeed, got %v", err)
	}
	if !remediated {
		t.Fatalf("expected remediation marker to be true")
	}
	if validation != "error" {
		t.Fatalf("expected validation.on-create to be normalized to error, got %q", validation)
	}
	if !requireDescription {
		t.Fatalf("expected create.require-description to be normalized to true")
	}
	if updates["validation.on-create"] != "error" {
		t.Fatalf("expected validation remediation write, got %v", updates)
	}
	if updates["create.require-description"] != "true" {
		t.Fatalf("expected description remediation write, got %v", updates)
	}

	updates = make(map[string]string)
	validation, requireDescription, remediated, err = remediateHardeningInvariant("error", true, func(key, value string) error {
		updates[key] = value
		return nil
	})
	if err != nil {
		t.Fatalf("expected no-op remediation to succeed, got %v", err)
	}
	if remediated {
		t.Fatalf("expected remediated=false for compliant hardening values")
	}
	if validation != "error" || !requireDescription {
		t.Fatalf("expected compliant values to remain unchanged, got validation=%q requireDescription=%v", validation, requireDescription)
	}
	if len(updates) != 0 {
		t.Fatalf("expected no writes for compliant values, got %v", updates)
	}
}

func TestDeferLivenessEnforcement(t *testing.T) {
	if deferCmd.Flags().Lookup("allow-unbounded") == nil {
		t.Fatalf("defer command missing --allow-unbounded opt-out")
	}

	if err := validateDeferLiveness("+1h", false); err != nil {
		t.Fatalf("expected bounded defer to pass, got %v", err)
	}
	if err := validateDeferLiveness("", true); err != nil {
		t.Fatalf("expected explicit opt-out to allow unbounded defer, got %v", err)
	}
	if err := validateDeferLiveness("", false); err == nil {
		t.Fatalf("expected unbounded defer without opt-out to fail")
	}
}

func TestCloseSafeSecretAndSpecDriftChecks(t *testing.T) {
	if flowCloseSafeCmd.Flags().Lookup("allow-secret-markers") == nil {
		t.Fatalf("flow close-safe missing --allow-secret-markers flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("require-spec-drift-proof") == nil {
		t.Fatalf("flow close-safe missing --require-spec-drift-proof flag")
	}

	markers := detectSecretMarkers("Implemented change", "token sk-live-example")
	if len(markers) == 0 {
		t.Fatalf("expected secret marker detection for sk- token")
	}

	if hasDocDriftTag("notes without drift tag") {
		t.Fatalf("unexpected DOC-DRIFT detection in plain notes")
	}
	if !hasDocDriftTag("DOC-DRIFT:bd-123 Owner:gwizz Next:bd ready") {
		t.Fatalf("expected DOC-DRIFT tag to be detected")
	}

	if !specDriftProofSatisfied(true, "") {
		t.Fatalf("expected docs-changed proof to satisfy spec-drift requirement")
	}
	if !specDriftProofSatisfied(false, "DOC-DRIFT:bd-123 Owner:gwizz Next:bd ready") {
		t.Fatalf("expected DOC-DRIFT tag to satisfy spec-drift requirement")
	}
	if specDriftProofSatisfied(false, "no proof present") {
		t.Fatalf("expected missing docs diff and missing DOC-DRIFT tag to fail proof check")
	}
}

func TestCloseSafeSpecDriftChecksStagedAndUnstagedDiffs(t *testing.T) {
	root := findRepoRootForContractTest(t)
	flowPath := filepath.Join(root, "cmd", "bd", "flow.go")
	data, err := os.ReadFile(flowPath)
	if err != nil {
		t.Fatalf("read flow.go: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"diff", "--name-only"`) {
		t.Fatalf("spec-drift proof must inspect unstaged diff paths")
	}
	if !strings.Contains(text, `"diff", "--cached", "--name-only"`) {
		t.Fatalf("spec-drift proof must inspect staged diff paths")
	}

	if !docsProofPresentInDiffOutput("docs/CONTROL_PLANE_CONTRACT.md\n") {
		t.Fatalf("expected docs path to satisfy spec-drift proof")
	}
	if !docsProofPresentInDiffOutput("README.md\n") {
		t.Fatalf("expected README path to satisfy spec-drift proof")
	}
	if docsProofPresentInDiffOutput("cmd/bd/flow.go\ninternal/types/types.go\n") {
		t.Fatalf("did not expect non-doc code paths to satisfy spec-drift proof")
	}
}

func TestCloseSafeTraceabilityAndParentCascade(t *testing.T) {
	if flowCloseSafeCmd.Flags().Lookup("require-traceability") == nil {
		t.Fatalf("flow close-safe missing --require-traceability flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("require-parent-cascade") == nil {
		t.Fatalf("flow close-safe missing --require-parent-cascade flag")
	}

	issue := &types.Issue{ID: "bd-1", AcceptanceCriteria: "observable outcome"}
	parent := &types.Issue{ID: "bd-0", AcceptanceCriteria: "parent milestone outcome"}
	if !traceabilityChainSatisfied(issue, parent) {
		t.Fatalf("expected traceability chain to pass with acceptance on issue and parent")
	}
	if traceabilityChainSatisfied(&types.Issue{ID: "bd-2"}, parent) {
		t.Fatalf("expected traceability chain to fail when issue acceptance is missing")
	}

	openChildren := collectUnclosedParentChildren([]*types.IssueWithDependencyMetadata{
		{
			Issue:          types.Issue{ID: "bd-c1", Status: types.StatusOpen},
			DependencyType: types.DepParentChild,
		},
		{
			Issue:          types.Issue{ID: "bd-c2", Status: types.StatusClosed},
			DependencyType: types.DepParentChild,
		},
		{
			Issue:          types.Issue{ID: "bd-rel", Status: types.StatusOpen},
			DependencyType: types.DepRelated,
		},
	})
	if len(openChildren) != 1 || openChildren[0] != "bd-c1" {
		t.Fatalf("expected only open parent-child dependent to be returned, got %v", openChildren)
	}
}

func TestPreClaimQualityLint(t *testing.T) {
	if flowPreclaimLintCmd == nil {
		t.Fatalf("flow preclaim-lint command is nil")
	}
	if flowPreclaimLintCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("flow preclaim-lint missing --issue flag")
	}

	estimate := 60
	okIssue := &types.Issue{
		ID:                 "bd-ok",
		IssueType:          types.TypeTask,
		Description:        "## Context\n...\n\n## Verify\ngo test ./...",
		AcceptanceCriteria: "observable behavior",
		Labels:             []string{"module/control-plane", "area/infra"},
		EstimatedMinutes:   &estimate,
	}
	okDeps := []*types.IssueWithDependencyMetadata{
		{Issue: types.Issue{ID: "bd-parent"}, DependencyType: types.DepParentChild},
	}
	if violations := collectPreclaimViolations(okIssue, okDeps); len(violations) != 0 {
		t.Fatalf("expected no violations for valid issue, got %v", violations)
	}

	badIssue := &types.Issue{
		ID:          "bd-bad",
		IssueType:   types.TypeTask,
		Description: "missing verify heading",
		Labels:      []string{"module/control-plane"},
	}
	if violations := collectPreclaimViolations(badIssue, nil); len(violations) == 0 {
		t.Fatalf("expected violations for invalid pre-claim issue")
	}
}

func TestPreclaimLintRequiresEstimateOrSplitMarker(t *testing.T) {
	deps := []*types.IssueWithDependencyMetadata{
		{Issue: types.Issue{ID: "bd-parent"}, DependencyType: types.DepParentChild},
	}

	withSplitMarker := &types.Issue{
		ID:                 "bd-split",
		IssueType:          types.TypeTask,
		Description:        "## Context\nsplit-required: follow-up atomization planned\n\n## Verify\ngo test ./cmd/bd -run TestX -count=1",
		AcceptanceCriteria: "observable behavior",
		Labels:             []string{"module/control-plane", "area/infra"},
	}
	if violations := collectPreclaimViolations(withSplitMarker, deps); len(violations) != 0 {
		t.Fatalf("expected split marker to satisfy atomicity estimate rule, got %v", violations)
	}

	missingBoth := &types.Issue{
		ID:                 "bd-missing-estimate",
		IssueType:          types.TypeTask,
		Description:        "## Context\nno estimate metadata present\n\n## Verify\ngo test ./cmd/bd -run TestX -count=1",
		AcceptanceCriteria: "observable behavior",
		Labels:             []string{"module/control-plane", "area/infra"},
	}
	violations := collectPreclaimViolations(missingBoth, deps)
	if !strings.Contains(strings.Join(violations, ","), "estimate_or_split.missing") {
		t.Fatalf("expected estimate_or_split.missing violation, got %v", violations)
	}
}

func TestMachineLintablePolicyMovedToCLI(t *testing.T) {
	if flowPreclaimLintCmd == nil {
		t.Fatalf("flow preclaim-lint command is nil")
	}
	if flowCloseSafeCmd == nil {
		t.Fatalf("flow close-safe command is nil")
	}
	if flowCloseSafeCmd.Flags().Lookup("reason") == nil {
		t.Fatalf("flow close-safe missing --reason flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("verified") == nil {
		t.Fatalf("flow close-safe missing --verified flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("require-spec-drift-proof") == nil {
		t.Fatalf("flow close-safe missing --require-spec-drift-proof flag")
	}

	if err := lintCloseReason("Implemented deterministic guardrail checks", false); err != nil {
		t.Fatalf("expected safe close reason to pass lint, got %v", err)
	}
	if err := lintCloseReason("Implemented corrected error handling path", false); err == nil {
		t.Fatalf("expected unsafe success reason containing failure keyword to fail lint")
	}
	if err := lintCloseReason("failed: deterministic validation failure", false); err == nil {
		t.Fatalf("expected failure reason without opt-in to fail lint")
	}
	if err := lintCloseReason("failed: deterministic validation failure", true); err != nil {
		t.Fatalf("expected failure reason with opt-in to pass lint, got %v", err)
	}

	if err := validateDeferLiveness("+2h", false); err != nil {
		t.Fatalf("expected bounded defer to pass liveness validation, got %v", err)
	}
	if err := validateDeferLiveness("", false); err == nil {
		t.Fatalf("expected unbounded defer without opt-out to fail validation")
	}

	validForceNotes := strings.Join([]string{
		"Force-close rationale: deterministic policy migration",
		"Open blockers at close: none",
		"Why bypass is safe now: fallback risk acknowledged",
	}, "\n")
	if err := validateForceCloseAuditNotes(validForceNotes); err != nil {
		t.Fatalf("expected valid force-close audit notes to pass, got %v", err)
	}

	if !specDriftProofSatisfied(false, "DOC-DRIFT:bd-1 Owner:gwizz Next:bd ready") {
		t.Fatalf("expected DOC-DRIFT tag to satisfy spec-drift proof")
	}

	if landCmd == nil {
		t.Fatalf("land command is nil")
	}
	if landCmd.Flags().Lookup("require-quality") == nil {
		t.Fatalf("land command missing --require-quality flag")
	}
	if landCmd.Flags().Lookup("require-handoff") == nil {
		t.Fatalf("land command missing --require-handoff flag")
	}
	if step := evaluateLandQualityGate(true, ""); step.Status != "fail" {
		t.Fatalf("expected required quality gate without summary to fail, got %+v", step)
	}
	if step := evaluateLandHandoffGate(true, "Continue work on bd-1", "none"); step.Status != "pass" {
		t.Fatalf("expected handoff gate with prompt+stash to pass, got %+v", step)
	}
}

func TestAtomicityLintEstimateAndVerifyPath(t *testing.T) {
	estimate120 := 120
	okIssue := &types.Issue{
		ID:                 "bd-ok-atomic",
		IssueType:          types.TypeTask,
		Description:        "## Context\n...\n\n## Verify\ngo test ./cmd/bd -run TestX -count=1\n",
		AcceptanceCriteria: "observable behavior",
		Labels:             []string{"module/control-plane", "area/infra"},
		EstimatedMinutes:   &estimate120,
	}
	okDeps := []*types.IssueWithDependencyMetadata{
		{Issue: types.Issue{ID: "bd-parent"}, DependencyType: types.DepParentChild},
	}
	if violations := collectPreclaimViolations(okIssue, okDeps); len(violations) != 0 {
		t.Fatalf("expected no violations for bounded estimate + single verify path, got %v", violations)
	}

	estimate240 := 240
	longIssue := &types.Issue{
		ID:                 "bd-long",
		IssueType:          types.TypeTask,
		Description:        "## Context\n...\n\n## Verify\ngo test ./cmd/bd -run TestX -count=1\n",
		AcceptanceCriteria: "observable behavior",
		Labels:             []string{"module/control-plane", "area/infra"},
		EstimatedMinutes:   &estimate240,
	}
	violations := collectPreclaimViolations(longIssue, okDeps)
	if !strings.Contains(strings.Join(violations, ","), "estimate.exceeds_180") {
		t.Fatalf("expected estimate.exceeds_180 violation, got %v", violations)
	}

	multiVerifyIssue := &types.Issue{
		ID:                 "bd-multi-verify",
		IssueType:          types.TypeTask,
		Description:        "## Context\n...\n\n## Verify\ngo test ./cmd/bd -run TestX -count=1\ngo test ./cmd/bd -run TestY -count=1\n",
		AcceptanceCriteria: "observable behavior",
		Labels:             []string{"module/control-plane", "area/infra"},
	}
	violations = collectPreclaimViolations(multiVerifyIssue, okDeps)
	if !strings.Contains(strings.Join(violations, ","), "verify_path.multiple") {
		t.Fatalf("expected verify_path.multiple violation, got %v", violations)
	}
}

func TestForceCloseAuditNotesEnforcement(t *testing.T) {
	if closeCmd.Flags().Lookup("force-audit-note") == nil {
		t.Fatalf("close command missing --force-audit-note flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("force") == nil {
		t.Fatalf("flow close-safe missing --force flag")
	}

	validNotes := strings.Join([]string{
		"Force-close rationale: unblock emergency rollout",
		"Open blockers at close: none",
		"Why bypass is safe now: downstream risk accepted and tracked",
	}, "\n")
	if err := validateForceCloseAuditNotes(validNotes); err != nil {
		t.Fatalf("expected valid force-close audit notes to pass, got %v", err)
	}

	invalidNotes := "Force-close rationale: present but missing other required fields"
	if err := validateForceCloseAuditNotes(invalidNotes); err == nil {
		t.Fatalf("expected missing force-close audit fields to fail validation")
	}
}

func TestAnchorBootstrapCardinalityGuard(t *testing.T) {
	if flowClaimNextCmd.Flags().Lookup("require-anchor") == nil {
		t.Fatalf("flow claim-next missing --require-anchor flag")
	}
	if flowClaimNextCmd.Flags().Lookup("anchor-label") == nil {
		t.Fatalf("flow claim-next missing --anchor-label flag")
	}

	if got := resolveAnchorLabel("module/core", nil); got != "module/core" {
		t.Fatalf("explicit anchor label should win, got %q", got)
	}
	if got := resolveAnchorLabel("", []string{"area/infra", "module/control-plane"}); got != "module/control-plane" {
		t.Fatalf("expected module label fallback, got %q", got)
	}
	if !anchorCardinalityViolation(0) {
		t.Fatalf("expected zero anchors to violate guard")
	}
	if anchorCardinalityViolation(1) {
		t.Fatalf("expected one anchor to pass guard")
	}
	if !anchorCardinalityViolation(2) {
		t.Fatalf("expected multiple anchors to violate guard")
	}
}

func TestBulkWriteGuardEnforcement(t *testing.T) {
	if intakeBulkGuardCmd == nil {
		t.Fatalf("intake bulk-guard command is nil")
	}
	if intakeBulkGuardCmd.Flags().Lookup("parent") == nil {
		t.Fatalf("intake bulk-guard missing --parent flag")
	}
	if intakeBulkGuardCmd.Flags().Lookup("limit") == nil {
		t.Fatalf("intake bulk-guard missing --limit flag")
	}
	if intakeBulkGuardCmd.Flags().Lookup("allow-empty-ready") == nil {
		t.Fatalf("intake bulk-guard missing --allow-empty-ready flag")
	}

	pass, violations := evaluateBulkWriteGuard(0, 2, false)
	if !pass || len(violations) != 0 {
		t.Fatalf("expected clean bulk guard to pass, got pass=%v violations=%v", pass, violations)
	}

	pass, violations = evaluateBulkWriteGuard(1, 2, false)
	if pass || len(violations) == 0 {
		t.Fatalf("expected cycle presence to fail bulk guard")
	}

	pass, violations = evaluateBulkWriteGuard(0, 0, false)
	if pass || len(violations) == 0 {
		t.Fatalf("expected empty ready set to fail bulk guard when allow-empty-ready=false")
	}

	pass, violations = evaluateBulkWriteGuard(0, 0, true)
	if !pass || len(violations) != 0 {
		t.Fatalf("expected allow-empty-ready to pass, got pass=%v violations=%v", pass, violations)
	}
}

func TestRecoverLoopConvergenceEnforcement(t *testing.T) {
	if recoverSignatureCmd == nil {
		t.Fatalf("recover signature command is nil")
	}
	if recoverSignatureCmd.Flags().Lookup("previous-signature") == nil {
		t.Fatalf("recover signature missing --previous-signature flag")
	}
	if recoverSignatureCmd.Flags().Lookup("iteration") == nil {
		t.Fatalf("recover signature missing --iteration flag")
	}
	if recoverSignatureCmd.Flags().Lookup("elapsed-minutes") == nil {
		t.Fatalf("recover signature missing --elapsed-minutes flag")
	}

	first := recoverSignature{
		BlockedIDs:   []string{"bd-2", "bd-3"},
		BlockerEdges: []string{"bd-2|bd-1"},
		BlockedCount: 2,
	}
	second := recoverSignature{
		BlockedIDs:   []string{"bd-2", "bd-3"},
		BlockerEdges: []string{"bd-2|bd-1"},
		BlockedCount: 2,
	}
	third := recoverSignature{
		BlockedIDs:   []string{"bd-2"},
		BlockerEdges: []string{"bd-2|bd-1"},
		BlockedCount: 1,
	}

	if !recoverSignaturesEqual(first, second) {
		t.Fatalf("expected equal signatures to compare true")
	}
	if recoverSignaturesEqual(first, third) {
		t.Fatalf("expected changed signatures to compare false")
	}

	if !recoverEscalationRequired(3, 10*time.Minute, 3, 30*time.Minute) {
		t.Fatalf("expected escalation at iteration threshold")
	}
	if !recoverEscalationRequired(1, 31*time.Minute, 3, 30*time.Minute) {
		t.Fatalf("expected escalation at elapsed-time threshold")
	}
	if recoverEscalationRequired(1, 10*time.Minute, 3, 30*time.Minute) {
		t.Fatalf("did not expect escalation below thresholds")
	}
}

func TestRecoverLoopDeterministicPhases(t *testing.T) {
	if recoverLoopCmd == nil {
		t.Fatalf("recover loop command is nil")
	}
	if recoverLoopCmd.Flags().Lookup("parent") == nil {
		t.Fatalf("recover loop missing --parent flag")
	}
	if recoverLoopCmd.Flags().Lookup("limit") == nil {
		t.Fatalf("recover loop missing --limit flag")
	}

	phases := buildRecoverLoopPhaseOutcomes(1, 0, 0, 0)
	if phases["phase_1"] != "ready_found" || phases["phase_2"] != "clean" || phases["phase_3"] != "clear" {
		t.Fatalf("unexpected phase outcomes for ready-found case: %#v", phases)
	}

	phases = buildRecoverLoopPhaseOutcomes(0, 2, 1, 0)
	if phases["phase_2"] != "cycles_detected" {
		t.Fatalf("expected cycle detection in phase_2, got %#v", phases)
	}

	open := []*types.Issue{
		{ID: "bd-1"},
		{ID: "bd-2"},
		{ID: "bd-3"},
	}
	ready := []*types.Issue{
		{ID: "bd-1"},
	}
	blocked := []*types.BlockedIssue{
		{Issue: types.Issue{ID: "bd-2"}},
	}
	limbo := detectRecoverLimboCandidates(open, ready, blocked)
	if len(limbo) != 1 || limbo[0] != "bd-3" {
		t.Fatalf("expected bd-3 as limbo candidate, got %v", limbo)
	}
}

func TestRecoverLoopPhase1Sequence(t *testing.T) {
	if recoverLoopCmd.Flags().Lookup("module-label") == nil {
		t.Fatalf("recover loop missing --module-label flag")
	}

	outcome, stop := recoverPhase1Outcome(1)
	if outcome != "ready_found" || !stop {
		t.Fatalf("expected phase 1 to stop on ready work, got outcome=%q stop=%v", outcome, stop)
	}

	outcome, stop = recoverPhase1Outcome(0)
	if outcome != "continue" || stop {
		t.Fatalf("expected phase 1 to continue when scoped ready is empty, got outcome=%q stop=%v", outcome, stop)
	}
}

func TestRecoverLoopPhase2StructuralDiagnostics(t *testing.T) {
	if got := recoverPhase2Outcome(1, 3); got != "cycles_detected" {
		t.Fatalf("expected cycles to dominate phase 2 outcome, got %q", got)
	}
	if got := recoverPhase2Outcome(0, 2); got != "blocked_detected" {
		t.Fatalf("expected blocked detection in phase 2 outcome, got %q", got)
	}
	if got := recoverPhase2Outcome(0, 0); got != "clean" {
		t.Fatalf("expected clean phase 2 outcome, got %q", got)
	}

	root := recoverRootBlockersFromBlocked([]*types.BlockedIssue{
		{BlockedBy: []string{"bd-b2", "bd-b1", "bd-b1"}},
	})
	if !reflect.DeepEqual(root, []string{"bd-b1", "bd-b2"}) {
		t.Fatalf("expected sorted unique root blockers, got %v", root)
	}
}

func TestRecoverLoopPhase4WidenScope(t *testing.T) {
	if got := resolveRecoverPhase4Outcome(true, 1, 0, 0); got != "module_ready_found" {
		t.Fatalf("expected module_ready_found, got %q", got)
	}
	if got := resolveRecoverPhase4Outcome(true, 0, 2, 0); got != "unscoped_ready_found" {
		t.Fatalf("expected unscoped_ready_found, got %q", got)
	}
	if got := resolveRecoverPhase4Outcome(true, 0, 0, 3); got != "unassigned_ready_found" {
		t.Fatalf("expected unassigned_ready_found, got %q", got)
	}
	if got := resolveRecoverPhase4Outcome(true, 0, 0, 0); got != "no_ready_after_widen" {
		t.Fatalf("expected no_ready_after_widen, got %q", got)
	}
	if got := resolveRecoverPhase4Outcome(false, 4, 2, 0); got != "unscoped_ready_found" {
		t.Fatalf("expected module-scope disabled to skip module outcome, got %q", got)
	}
}

func TestRecoverLoopResultSelection(t *testing.T) {
	if got := resolveRecoverLoopResult(map[string]string{
		"phase_3": "limbo_detected",
		"phase_4": "unscoped_ready_found",
	}); got != "recover_limbo_detected" {
		t.Fatalf("expected limbo to take precedence over widened ready, got %q", got)
	}
	if got := resolveRecoverLoopResult(map[string]string{
		"phase_3": "clear",
		"phase_4": "unassigned_ready_found",
	}); got != "recover_ready_found_widened" {
		t.Fatalf("expected widened-ready result, got %q", got)
	}
	if got := resolveRecoverLoopResult(map[string]string{
		"phase_3": "clear",
		"phase_4": "no_ready_after_widen",
	}); got != "recover_continue" {
		t.Fatalf("expected recover_continue when widen finds no ready work, got %q", got)
	}
}

func TestRecoverSignatureAnchorPersistence(t *testing.T) {
	if recoverSignatureCmd.Flags().Lookup("anchor") == nil {
		t.Fatalf("recover signature missing --anchor flag")
	}
	if recoverSignatureCmd.Flags().Lookup("write-anchor") == nil {
		t.Fatalf("recover signature missing --write-anchor flag")
	}

	note := buildRecoverSignatureAnchorNote(4, recoverSignature{
		BlockedIDs:   []string{"bd-1", "bd-2"},
		BlockerEdges: []string{"bd-1|bd-9"},
		BlockedCount: 2,
	})
	required := []string{
		"Recover iteration 4:",
		"blocked_ids=bd-1,bd-2",
		"blocker_edges=bd-1|bd-9",
		"blocked_count=2",
	}
	for _, token := range required {
		if !strings.Contains(note, token) {
			t.Fatalf("anchor note missing %q in %q", token, note)
		}
	}
}

func TestLandFullGateCoverage(t *testing.T) {
	if landCmd.Flags().Lookup("require-quality") == nil {
		t.Fatalf("land missing --require-quality flag")
	}
	if landCmd.Flags().Lookup("quality-summary") == nil {
		t.Fatalf("land missing --quality-summary flag")
	}
	if landCmd.Flags().Lookup("require-handoff") == nil {
		t.Fatalf("land missing --require-handoff flag")
	}
	if landCmd.Flags().Lookup("next-prompt") == nil {
		t.Fatalf("land missing --next-prompt flag")
	}
	if landCmd.Flags().Lookup("stash") == nil {
		t.Fatalf("land missing --stash flag")
	}

	if step := evaluateLandQualityGate(false, ""); step.Status != "skipped" {
		t.Fatalf("expected skipped quality gate when not required, got %+v", step)
	}
	if step := evaluateLandQualityGate(true, ""); step.Status != "fail" {
		t.Fatalf("expected fail quality gate when required summary missing, got %+v", step)
	}
	if step := evaluateLandQualityGate(true, "tests/lint/build passed"); step.Status != "pass" {
		t.Fatalf("expected pass quality gate with summary, got %+v", step)
	}

	if step := evaluateLandHandoffGate(false, "", ""); step.Status != "skipped" {
		t.Fatalf("expected skipped handoff gate when not required, got %+v", step)
	}
	if step := evaluateLandHandoffGate(true, "", "none"); step.Status != "fail" {
		t.Fatalf("expected fail handoff gate when prompt missing, got %+v", step)
	}
	if step := evaluateLandHandoffGate(true, "Continue work on bd-1", "none"); step.Status != "pass" {
		t.Fatalf("expected pass handoff gate when required fields present, got %+v", step)
	}
}

func TestLandGate3SyncChoreography(t *testing.T) {
	if landCmd.Flags().Lookup("sync-merge") == nil {
		t.Fatalf("land missing --sync-merge flag")
	}
	if landCmd.Flags().Lookup("pull-rebase") == nil {
		t.Fatalf("land missing --pull-rebase flag")
	}

	calls := make([]string, 0)
	runner := func(name string, args ...string) (string, error) {
		calls = append(calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
		return "ok", nil
	}

	steps, err := runLandGate3Choreography(false, true, true, true, true, runner)
	if err != nil {
		t.Fatalf("expected gate3 choreography to succeed, got %v", err)
	}
	expectedCalls := []string{
		"git pull --rebase",
		"bd sync --status",
		"bd sync --merge",
		"bd sync",
		"git push",
	}
	if !reflect.DeepEqual(calls, expectedCalls) {
		t.Fatalf("unexpected gate3 command call order: got %v want %v", calls, expectedCalls)
	}
	if len(steps) != 5 {
		t.Fatalf("expected 5 gate3 steps, got %d (%v)", len(steps), steps)
	}
	if skipped := skippedGate3Operations(steps); len(skipped) != 0 {
		t.Fatalf("expected no skipped Gate 3 operations in full choreography, got %v", skipped)
	}

	calls = calls[:0]
	steps, err = runLandGate3Choreography(true, false, true, false, false, runner)
	if err != nil {
		t.Fatalf("expected check-only choreography to succeed, got %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no subprocess calls in check-only mode, got %v", calls)
	}
	if steps[0].Status != "skipped" || steps[1].Status != "skipped" {
		t.Fatalf("expected initial check-only steps to be skipped, got %v", steps)
	}

	calls = calls[:0]
	steps, err = runLandGate3Choreography(false, false, false, false, false, runner)
	if err != nil {
		t.Fatalf("expected non-mutating choreography to succeed, got %v", err)
	}
	expectedCalls = []string{
		"bd sync --status",
	}
	if !reflect.DeepEqual(calls, expectedCalls) {
		t.Fatalf("unexpected non-mutating gate3 call order: got %v want %v", calls, expectedCalls)
	}
	if steps[0].Name != "gate3_pull_rebase" || steps[0].Status != "skipped" {
		t.Fatalf("expected pull-rebase to be skipped by default, got %v", steps[0])
	}
	if skipped := skippedGate3Operations(steps); !reflect.DeepEqual(skipped, []string{"gate3_pull_rebase", "gate3_push", "gate3_sync"}) {
		t.Fatalf("expected skipped Gate 3 operation set for non-mutating run, got %v", skipped)
	}
}

func TestLandGate3PromotesCriticalWarningsToBlocker(t *testing.T) {
	warnings := criticalWarningNamesFromChecks([]doctorCheck{
		{Name: "Repo Fingerprint", Status: statusWarning},
		{Name: "DB-JSONL Sync", Status: statusWarning},
		{Name: "non-critical", Status: statusWarning},
		{Name: "Repo Fingerprint", Status: statusOK},
	})
	if !reflect.DeepEqual(warnings, []string{"DB-JSONL Sync", "Repo Fingerprint"}) {
		t.Fatalf("expected sorted critical warning names, got %v", warnings)
	}

	none := criticalWarningNamesFromChecks([]doctorCheck{{Name: "Issues Tracking", Status: statusOK}})
	if len(none) != 0 {
		t.Fatalf("expected no promoted warnings for non-warning statuses, got %v", none)
	}
}

func TestPriorityPollDeterministicEnforcement(t *testing.T) {
	if flowPriorityPollCmd == nil {
		t.Fatalf("flow priority-poll command is nil")
	}
	if flowPriorityPollCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("flow priority-poll missing --issue flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("require-priority-poll") == nil {
		t.Fatalf("flow close-safe missing --require-priority-poll flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("priority-poll-max-age") == nil {
		t.Fatalf("flow close-safe missing --priority-poll-max-age flag")
	}

	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)
	freshNotes := appendPriorityPollNote("", now.Add(-10*time.Minute), []string{"bd-p0"})
	if !hasFreshPriorityPollNote(freshNotes, now, 30*time.Minute) {
		t.Fatalf("expected fresh priority poll note to satisfy max age")
	}

	staleNotes := appendPriorityPollNote("", now.Add(-45*time.Minute), []string{"bd-p0"})
	if hasFreshPriorityPollNote(staleNotes, now, 30*time.Minute) {
		t.Fatalf("expected stale priority poll note to fail max age")
	}

	if _, ok := latestPriorityPollTimestamp("no priority marker"); ok {
		t.Fatalf("expected no timestamp from malformed notes")
	}
}

func TestSupersedeCoarseTasksProtocol(t *testing.T) {
	if flowSupersedeCoarseCmd == nil {
		t.Fatalf("flow supersede-coarse command is nil")
	}
	if flowSupersedeCoarseCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("flow supersede-coarse missing --issue flag")
	}
	if flowSupersedeCoarseCmd.Flags().Lookup("replacement") == nil {
		t.Fatalf("flow supersede-coarse missing --replacement flag")
	}
	if flowSupersedeCoarseCmd.Flags().Lookup("reason") == nil {
		t.Fatalf("flow supersede-coarse missing --reason flag")
	}

	if !hasParentChildToID([]*types.IssueWithDependencyMetadata{
		{
			Issue:          types.Issue{ID: "bd-parent"},
			DependencyType: types.DepParentChild,
		},
	}, "bd-parent") {
		t.Fatalf("expected parent-child dependency to target parent to pass")
	}

	if hasParentChildToID([]*types.IssueWithDependencyMetadata{
		{
			Issue:          types.Issue{ID: "bd-other"},
			DependencyType: types.DepParentChild,
		},
	}, "bd-parent") {
		t.Fatalf("expected mismatched parent-child dependency to fail")
	}
}

func TestLivingStateDigestAutoUpdate(t *testing.T) {
	if got := nextSessionCloseCount("Session tasks closed: 2"); got != 3 {
		t.Fatalf("expected session close count increment to 3, got %d", got)
	}
	if got := nextSessionCloseCount("no prior digest"); got != 1 {
		t.Fatalf("expected initial session close count to be 1, got %d", got)
	}

	ts := time.Date(2026, 2, 19, 12, 34, 56, 0, time.UTC)
	digest := buildLivingStateDigestBlock(
		ts,
		"none",
		"bd-1 Implemented x",
		[]string{"bd-2", "bd-3"},
		[]string{"bd-4"},
		4,
	)
	required := []string{
		"State digest (2026-02-19T12:34:56Z):",
		"WIP: none",
		"Last closed: bd-1 Implemented x",
		"Ready next: bd-2,bd-3",
		"Blockers: bd-4",
		"Session tasks closed: 4",
	}
	for _, token := range required {
		if !strings.Contains(digest, token) {
			t.Fatalf("digest missing %q: %q", token, digest)
		}
	}
}

func TestResumeGuardDeterministicActions(t *testing.T) {
	actions := buildResumeGuardActions("bd-123")
	if len(actions) != 4 {
		t.Fatalf("expected 4 deterministic resume-guard actions, got %d", len(actions))
	}

	expectedClasses := []string{"resume", "close", "block", "relinquish"}
	for i, wantClass := range expectedClasses {
		gotClass := actions[i]["class"]
		if gotClass != wantClass {
			t.Fatalf("action[%d] class=%q, want %q", i, gotClass, wantClass)
		}
		if !strings.Contains(actions[i]["next_command"], "bd-123") {
			t.Fatalf("action[%d] next_command missing issue ID: %q", i, actions[i]["next_command"])
		}
	}
}

func TestContextFreshnessThresholdSignals(t *testing.T) {
	signals := evaluateContextFreshnessSignals(2, 4, false)
	if len(signals) != 0 {
		t.Fatalf("expected no freshness signals below thresholds, got %v", signals)
	}

	signals = evaluateContextFreshnessSignals(3, 1, false)
	if len(signals) != 1 || signals[0] != "session_close_threshold" {
		t.Fatalf("expected session-close threshold signal, got %v", signals)
	}

	signals = evaluateContextFreshnessSignals(1, 6, true)
	if len(signals) != 2 {
		t.Fatalf("expected reread + state-transition signals, got %v", signals)
	}
	if signals[0] != "file_reread_threshold" || signals[1] != "state_transition" {
		t.Fatalf("unexpected signal ordering/content: %v", signals)
	}
}

func TestNonHermeticEvidenceTupleEnforcement(t *testing.T) {
	if flowCloseSafeCmd.Flags().Lookup("non-hermetic") == nil {
		t.Fatalf("flow close-safe missing --non-hermetic flag")
	}
	if !evidenceRequirementForVerificationFlow(true, false) {
		t.Fatalf("expected non-hermetic flow to require evidence tuple")
	}
	if !evidenceRequirementForVerificationFlow(false, true) {
		t.Fatalf("expected explicit evidence requirement to be honored")
	}
	if evidenceRequirementForVerificationFlow(false, false) {
		t.Fatalf("expected hermetic flow without explicit requirement to skip tuple enforcement")
	}
}

func TestExecutionRollbackCorrectiveTaskPrimitive(t *testing.T) {
	if flowExecutionRollbackCmd == nil {
		t.Fatalf("flow execution-rollback command is nil")
	}
	if flowExecutionRollbackCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("flow execution-rollback missing --issue flag")
	}
	if flowExecutionRollbackCmd.Flags().Lookup("title") == nil {
		t.Fatalf("flow execution-rollback missing --title flag")
	}
	if flowExecutionRollbackCmd.Flags().Lookup("verify") == nil {
		t.Fatalf("flow execution-rollback missing --verify flag")
	}

	desc := buildExecutionRollbackDescription("bd-100", "Original Task", "go test ./cmd/bd -run TestX -count=1")
	if !strings.Contains(desc, "## Context") || !strings.Contains(desc, "## Verify") {
		t.Fatalf("rollback description missing required sections: %q", desc)
	}
	if !strings.Contains(desc, "bd-100") || !strings.Contains(desc, "go test ./cmd/bd -run TestX -count=1") {
		t.Fatalf("rollback description missing original ID or verify command: %q", desc)
	}

	acc := buildExecutionRollbackAcceptance("bd-100")
	if !strings.Contains(acc, "bd-100") {
		t.Fatalf("rollback acceptance missing original ID: %q", acc)
	}
}

func TestMechanicalTransitionHandlers(t *testing.T) {
	if flowTransitionCmd == nil {
		t.Fatalf("flow transition command is nil")
	}
	requiredFlags := []string{
		"type",
		"issue",
		"blocker",
		"context",
		"reason",
		"abort-handoff",
		"abort-no-bd-write",
	}
	for _, flagName := range requiredFlags {
		if flowTransitionCmd.Flags().Lookup(flagName) == nil {
			t.Fatalf("flow transition missing --%s flag", flagName)
		}
	}

	if got := normalizeTransitionType("claim-became-blocked"); got != "claim_became_blocked" {
		t.Fatalf("expected hyphen normalization to claim_became_blocked, got %q", got)
	}
	if !isSupportedTransitionType("test_failed") {
		t.Fatalf("expected test_failed to be a supported transition type")
	}
	if isSupportedTransitionType("unknown_transition") {
		t.Fatalf("unexpected support for unknown transition type")
	}

	if transitionRequiresIssue("claim_failed") {
		t.Fatalf("claim_failed should not require issue ID")
	}
	if !transitionRequiresIssue("exec_blocked") {
		t.Fatalf("exec_blocked should require issue ID")
	}

	if got := transitionContextOrReason("ctx", "reason"); got != "ctx" {
		t.Fatalf("expected context to win over reason, got %q", got)
	}
	if got := transitionContextOrReason("", "reason"); got != "reason" {
		t.Fatalf("expected reason fallback when context missing, got %q", got)
	}

	if got := normalizeFailureCloseReason("timeout in fallback path"); got != "failed: timeout in fallback path" {
		t.Fatalf("expected failure reason normalization, got %q", got)
	}
	if got := normalizeFailureCloseReason("failed: already normalized"); got != "failed: already normalized" {
		t.Fatalf("expected idempotent failure reason normalization, got %q", got)
	}

	decisionNote := buildConditionalFallbackDecisionNote("gate evidence")
	if !strings.Contains(decisionNote, "Decision | Evidence | Risk | Follow-up ID: conditional_fallback_activate") {
		t.Fatalf("decision note missing canonical marker: %q", decisionNote)
	}
	if !strings.Contains(decisionNote, "gate evidence") {
		t.Fatalf("decision note missing evidence payload: %q", decisionNote)
	}

	abortDoc := buildAbortHandoffMarkdown("wrong repo", "bd-1", "dirty worktree")
	requiredDocFields := []string{"# ABORT Handoff", "Reason: wrong repo", "Issue: bd-1", "State: dirty worktree"}
	for _, field := range requiredDocFields {
		if !strings.Contains(abortDoc, field) {
			t.Fatalf("abort handoff doc missing %q: %q", field, abortDoc)
		}
	}

	contextPack := buildSessionAbortContextPack("wrong repo", "dirty worktree")
	if !strings.Contains(contextPack, "Context pack: session_abort") {
		t.Fatalf("session abort context pack missing canonical prefix: %q", contextPack)
	}
}

func TestTransientFailureRetryPolicyDeterministic(t *testing.T) {
	if flowTransitionCmd == nil {
		t.Fatalf("flow transition command is nil")
	}
	requiredFlags := []string{"attempt", "max-attempts", "backoff", "escalate"}
	for _, flagName := range requiredFlags {
		if flowTransitionCmd.Flags().Lookup(flagName) == nil {
			t.Fatalf("flow transition missing --%s flag", flagName)
		}
	}

	defaultSchedule, err := parseTransientBackoffSchedule("", 3)
	if err != nil {
		t.Fatalf("default backoff schedule parse failed: %v", err)
	}
	if len(defaultSchedule) != 3 {
		t.Fatalf("expected 3 default backoff entries, got %d", len(defaultSchedule))
	}
	if defaultSchedule[0] != 30*time.Second || defaultSchedule[1] != 90*time.Second || defaultSchedule[2] != 180*time.Second {
		t.Fatalf("unexpected default schedule: %v", defaultSchedule)
	}

	customSchedule, err := parseTransientBackoffSchedule("15s,45s", 3)
	if err != nil {
		t.Fatalf("custom backoff schedule parse failed: %v", err)
	}
	if len(customSchedule) != 3 || customSchedule[0] != 15*time.Second || customSchedule[1] != 45*time.Second || customSchedule[2] != 45*time.Second {
		t.Fatalf("unexpected custom schedule expansion: %v", customSchedule)
	}

	if _, err := parseTransientBackoffSchedule("abc", 3); err == nil {
		t.Fatalf("expected invalid backoff duration parse to fail")
	}

	escalate, err := resolveTransientEscalationType("")
	if err != nil || escalate != "test_failed" {
		t.Fatalf("expected default escalation to test_failed, got %q err=%v", escalate, err)
	}
	escalate, err = resolveTransientEscalationType("exec-blocked")
	if err != nil || escalate != "exec_blocked" {
		t.Fatalf("expected escalation normalization to exec_blocked, got %q err=%v", escalate, err)
	}
	if _, err := resolveTransientEscalationType("invalid"); err == nil {
		t.Fatalf("expected invalid escalation type to fail")
	}
}

func TestDecompositionInvalidDamperThreshold(t *testing.T) {
	if flowTransitionCmd == nil {
		t.Fatalf("flow transition command is nil")
	}
	if flowTransitionCmd.Flags().Lookup("decomposition-threshold") == nil {
		t.Fatalf("flow transition missing --decomposition-threshold flag")
	}
	if !isSupportedTransitionType("decomposition_invalid") {
		t.Fatalf("decomposition_invalid should be a supported transition type")
	}

	notes := strings.Join([]string{
		"Decomposition invalid (attempt 1): missing integration gate",
		"Decomposition invalid (attempt 2): module boundary mismatch",
	}, "\n")
	if got := nextDecompositionInvalidAttempt(notes); got != 3 {
		t.Fatalf("expected next decomposition attempt to be 3, got %d", got)
	}
	if got := nextDecompositionInvalidAttempt(""); got != 1 {
		t.Fatalf("expected first decomposition attempt to be 1 on empty notes, got %d", got)
	}

	if decompositionDamperEscalationRequired(2, 3) {
		t.Fatalf("expected no escalation below threshold")
	}
	if !decompositionDamperEscalationRequired(3, 3) {
		t.Fatalf("expected escalation at threshold")
	}
	if decompositionDamperEscalationRequired(5, 0) {
		t.Fatalf("expected disabled threshold (0) to skip escalation")
	}
}

func TestConditionalFallbackActivateConformance(t *testing.T) {
	if flowTransitionCmd == nil {
		t.Fatalf("flow transition command is nil")
	}
	if !isSupportedTransitionType("conditional_fallback_activate") {
		t.Fatalf("conditional_fallback_activate should be a supported transition type")
	}
	if got := normalizeTransitionType("conditional-fallback-activate"); got != "conditional_fallback_activate" {
		t.Fatalf("expected transition type normalization, got %q", got)
	}

	reason := normalizeFailureCloseReason("dependency contract mismatch")
	if !strings.HasPrefix(strings.ToLower(reason), "failed:") {
		t.Fatalf("expected fallback close reason to normalize to failed: prefix, got %q", reason)
	}
	if err := lintCloseReason(reason, true); err != nil {
		t.Fatalf("expected normalized fallback reason to pass lint with allowFailure=true, got %v", err)
	}

	decisionNote := buildConditionalFallbackDecisionNote("dep tree and gate check evidence")
	requiredTokens := []string{
		"Decision | Evidence | Risk | Follow-up ID: conditional_fallback_activate",
		"dep tree and gate check evidence",
	}
	for _, token := range requiredTokens {
		if !strings.Contains(decisionNote, token) {
			t.Fatalf("decision note missing %q: %q", token, decisionNote)
		}
	}
}

func TestTransitionHandlerConformance(t *testing.T) {
	if flowTransitionCmd == nil {
		t.Fatalf("flow transition command is nil")
	}
	if flowTransitionCmd.Flags().Lookup("type") == nil {
		t.Fatalf("flow transition missing --type flag")
	}
	if flowTransitionCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("flow transition missing --issue flag")
	}
	if flowTransitionCmd.Flags().Lookup("blocker") == nil {
		t.Fatalf("flow transition missing --blocker flag")
	}
	if flowTransitionCmd.Flags().Lookup("context") == nil {
		t.Fatalf("flow transition missing --context flag")
	}
	if flowTransitionCmd.Flags().Lookup("reason") == nil {
		t.Fatalf("flow transition missing --reason flag")
	}

	expected := []struct {
		raw           string
		normalized    string
		requiresIssue bool
	}{
		{raw: "claim_failed", normalized: "claim_failed", requiresIssue: false},
		{raw: "transient_failure", normalized: "transient_failure", requiresIssue: true},
		{raw: "decomposition-invalid", normalized: "decomposition_invalid", requiresIssue: true},
		{raw: "claim-became-blocked", normalized: "claim_became_blocked", requiresIssue: true},
		{raw: "exec_blocked", normalized: "exec_blocked", requiresIssue: true},
		{raw: "test_failed", normalized: "test_failed", requiresIssue: true},
		{raw: "conditional-fallback-activate", normalized: "conditional_fallback_activate", requiresIssue: true},
		{raw: "session_abort", normalized: "session_abort", requiresIssue: false},
	}
	for _, tc := range expected {
		gotType := normalizeTransitionType(tc.raw)
		if gotType != tc.normalized {
			t.Fatalf("normalizeTransitionType(%q)=%q, want %q", tc.raw, gotType, tc.normalized)
		}
		if !isSupportedTransitionType(gotType) {
			t.Fatalf("expected %q to be supported", gotType)
		}
		if gotRequires := transitionRequiresIssue(gotType); gotRequires != tc.requiresIssue {
			t.Fatalf("transitionRequiresIssue(%q)=%v, want %v", gotType, gotRequires, tc.requiresIssue)
		}
	}

	supported := supportedTransitionTypes()
	if len(supported) < len(expected) {
		t.Fatalf("supported transition set too small: %v", supported)
	}

	envelope := captureEmitEnvelopeJSON(t, commandEnvelope{
		OK:      true,
		Command: "flow transition",
		Result:  "claim_failed",
		IssueID: "",
		Details: map[string]interface{}{
			"transition_type": "claim_failed",
			"message":         "claim failed; select next ready issue",
		},
		RecoveryCommand: "bd ready --limit 5",
		Events:          []string{"claim_failed", "ready_requeue"},
	})
	if envelope["command"] != "flow transition" {
		t.Fatalf("unexpected envelope command: %#v", envelope)
	}
	if envelope["result"] != "claim_failed" {
		t.Fatalf("unexpected envelope result: %#v", envelope)
	}
	if envelope["recovery_command"] != "bd ready --limit 5" {
		t.Fatalf("unexpected recovery command in envelope: %#v", envelope)
	}
}

func extractTransitionTypesFromDocMarkdown(markdown string) []string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		candidate := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		candidate = strings.Trim(candidate, "`")
		if candidate == "" {
			continue
		}
		out = append(out, candidate)
	}
	return uniqueIntakeStrings(out)
}

func TestDocsTransitionsMatchSupportedTransitionTypes(t *testing.T) {
	root := findRepoRootForContractTest(t)
	docPath := filepath.Join(root, "docs", "control-plane", "supported-transition-types.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read supported transition doc: %v", err)
	}

	documented := extractTransitionTypesFromDocMarkdown(string(data))
	supported := uniqueIntakeStrings(supportedTransitionTypes())
	if !reflect.DeepEqual(documented, supported) {
		t.Fatalf("documented transition set drifted from supportedTransitionTypes(): documented=%v supported=%v", documented, supported)
	}
}

func TestAgentsScriptReferencesCliCommands(t *testing.T) {
	root := findRepoRootForContractTest(t)
	agentsPath := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	text := string(data)

	required := []string{
		"docs/CONTROL_PLANE_CONTRACT.md",
		"CLI-owned",
		"bd update",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Fatalf("AGENTS.md missing required CLI-reference token %q", token)
		}
	}

	forbidden := []string{
		"scripts/bd_intake_audit.sh",
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"INTAKE-MAP-BEGIN",
		"INTAKE-MAP-END",
	}
	for _, marker := range forbidden {
		if strings.Contains(text, marker) {
			t.Fatalf("AGENTS.md still embeds deterministic script marker %q", marker)
		}
	}
}

func TestPolicyDriftGuard(t *testing.T) {
	root := findRepoRootForContractTest(t)
	pluginPath := filepath.Join(root, "docs", "PLUGIN.md")
	pluginData, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read docs/PLUGIN.md: %v", err)
	}
	pluginText := string(pluginData)

	requiredPluginTokens := []string{
		"CLI-owned",
		"Split-agent-owned",
		"MCP-owned",
		"`flow`",
	}
	for _, token := range requiredPluginTokens {
		if !strings.Contains(pluginText, token) {
			t.Fatalf("docs/PLUGIN.md missing policy-boundary token %q", token)
		}
	}

	serverPath := filepath.Join(root, "integrations", "beads-mcp", "src", "beads_mcp", "server.py")
	serverData, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read MCP server source: %v", err)
	}
	serverText := string(serverData)

	requiredServerTokens := []string{
		"_FLOW_ONLY_WRITES_ENV",
		"_enforce_flow_write_policy",
		`if op == "claim_next"`,
		`if op == "create_discovered"`,
		`if op == "block_with_context"`,
		`if op == "close_safe"`,
		`if op == "transition"`,
	}
	for _, token := range requiredServerTokens {
		if !strings.Contains(serverText, token) {
			t.Fatalf("MCP server missing flow policy token %q", token)
		}
	}

	forbiddenDriftMarkers := []string{
		"INTAKE-MAP-BEGIN",
		"INTAKE-MAP-END",
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"PLAN-COUNT:",
		"READY-WAVE-1:",
	}
	for _, marker := range forbiddenDriftMarkers {
		if strings.Contains(pluginText, marker) {
			t.Fatalf("policy drift: docs/PLUGIN.md contains deterministic script marker %q", marker)
		}
		if strings.Contains(serverText, marker) {
			t.Fatalf("policy drift: MCP server contains deterministic script marker %q", marker)
		}
	}
}

func TestCutoverGateBlocksUnresolvedGaps(t *testing.T) {
	if cutoverCmd == nil {
		t.Fatalf("cutover command is nil")
	}
	if cutoverGateCmd == nil {
		t.Fatalf("cutover gate command is nil")
	}
	if cutoverGateCmd.Flags().Lookup("matrix") == nil {
		t.Fatalf("cutover gate missing --matrix flag")
	}

	items := []evidenceMatrixItem{
		{
			ChecklistItem:      "Release cutover gate",
			Status:             "GAP",
			RemediationIssueID: "bd-rem-1",
		},
		{
			ChecklistItem:      "Final verification report",
			Status:             "GAP",
			RemediationIssueID: "bd-rem-2",
		},
	}

	lookupWithOpen := func(remediationID string) (*types.Issue, error) {
		if remediationID == "bd-rem-1" {
			return &types.Issue{ID: remediationID, Status: types.StatusOpen}, nil
		}
		return &types.Issue{ID: remediationID, Status: types.StatusClosed}, nil
	}
	unresolved, err := evaluateUnresolvedCutoverGaps(items, lookupWithOpen)
	if err != nil {
		t.Fatalf("evaluateUnresolvedCutoverGaps returned error: %v", err)
	}
	if len(unresolved) != 1 || unresolved[0].RemediationIssueID != "bd-rem-1" {
		t.Fatalf("expected unresolved gap for open remediation issue, got %v", unresolved)
	}

	lookupAllClosed := func(remediationID string) (*types.Issue, error) {
		return &types.Issue{ID: remediationID, Status: types.StatusClosed}, nil
	}
	unresolved, err = evaluateUnresolvedCutoverGaps(items, lookupAllClosed)
	if err != nil {
		t.Fatalf("evaluateUnresolvedCutoverGaps returned error for closed remediations: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("expected no unresolved gaps when all remediations are closed, got %v", unresolved)
	}

	_, err = evaluateUnresolvedCutoverGaps([]evidenceMatrixItem{
		{ChecklistItem: "bad gap row", Status: "GAP"},
	}, lookupAllClosed)
	if err == nil {
		t.Fatalf("expected missing remediation_issue_id on GAP row to fail")
	}
}

func TestSessionStateStageOrderValidation(t *testing.T) {
	if stateValidateTransitionCmd == nil {
		t.Fatalf("state validate-transition command is nil")
	}
	if stateValidateTransitionCmd.Flags().Lookup("from") == nil {
		t.Fatalf("state validate-transition missing --from flag")
	}
	if stateValidateTransitionCmd.Flags().Lookup("to") == nil {
		t.Fatalf("state validate-transition missing --to flag")
	}

	ok, from, to, _ := validateSessionStateTransition("boot", "planning")
	if !ok || from != "BOOT" || to != "PLANNING" {
		t.Fatalf("expected BOOT->PLANNING to be valid, got ok=%v from=%q to=%q", ok, from, to)
	}

	ok, _, _, allowed := validateSessionStateTransition("boot", "executing")
	if ok {
		t.Fatalf("expected BOOT->EXECUTING to be invalid")
	}
	if len(allowed) == 0 || allowed[0] != "PLANNING" {
		t.Fatalf("expected BOOT allowed-next list, got %v", allowed)
	}

	ok, _, _, _ = validateSessionStateTransition("executing", "recovering")
	if !ok {
		t.Fatalf("expected EXECUTING->RECOVERING to be valid")
	}
	ok, _, _, _ = validateSessionStateTransition("recovering", "executing")
	if !ok {
		t.Fatalf("expected RECOVERING->EXECUTING to be valid")
	}
}

func TestLifecycleCommandsEnforceStateTransitions(t *testing.T) {
	if flowCmd.PersistentFlags().Lookup("state-from") == nil {
		t.Fatalf("flow command missing --state-from flag")
	}
	if flowCmd.PersistentFlags().Lookup("state-to") == nil {
		t.Fatalf("flow command missing --state-to flag")
	}
	if intakeCmd.PersistentFlags().Lookup("state-from") == nil {
		t.Fatalf("intake command missing --state-from flag")
	}
	if intakeCmd.PersistentFlags().Lookup("state-to") == nil {
		t.Fatalf("intake command missing --state-to flag")
	}
	if landCmd.Flags().Lookup("state-from") == nil {
		t.Fatalf("land command missing --state-from flag")
	}
	if landCmd.Flags().Lookup("state-to") == nil {
		t.Fatalf("land command missing --state-to flag")
	}
	if recoverCmd.PersistentFlags().Lookup("state-from") == nil {
		t.Fatalf("recover command missing --state-from flag")
	}
	if recoverCmd.PersistentFlags().Lookup("state-to") == nil {
		t.Fatalf("recover command missing --state-to flag")
	}
	if resumeCmd.Flags().Lookup("state-from") == nil {
		t.Fatalf("resume command missing --state-from flag")
	}
	if resumeCmd.Flags().Lookup("state-to") == nil {
		t.Fatalf("resume command missing --state-to flag")
	}
	if preflightCmd.PersistentFlags().Lookup("state-from") == nil {
		t.Fatalf("preflight command missing --state-from flag")
	}
	if preflightCmd.PersistentFlags().Lookup("state-to") == nil {
		t.Fatalf("preflight command missing --state-to flag")
	}
	if reasonCmd.PersistentFlags().Lookup("state-from") == nil {
		t.Fatalf("reason command missing --state-from flag")
	}
	if reasonCmd.PersistentFlags().Lookup("state-to") == nil {
		t.Fatalf("reason command missing --state-to flag")
	}

	missingPair := assessLifecycleStateTransition("boot", "")
	if missingPair.Pass || missingPair.Result != "invalid_input" {
		t.Fatalf("expected missing state pair to be invalid_input, got %+v", missingPair)
	}

	invalid := assessLifecycleStateTransition("boot", "executing")
	if invalid.Pass {
		t.Fatalf("expected BOOT->EXECUTING to be blocked")
	}
	if invalid.Result != "policy_violation" {
		t.Fatalf("expected policy_violation for invalid lifecycle transition, got %q", invalid.Result)
	}
	if invalid.ExitCode != exitCodePolicyViolation {
		t.Fatalf("expected policy violation exit code %d, got %d", exitCodePolicyViolation, invalid.ExitCode)
	}
	if len(invalid.AllowedNext) == 0 || invalid.AllowedNext[0] != "PLANNING" {
		t.Fatalf("expected BOOT allowed-next guidance, got %v", invalid.AllowedNext)
	}

	valid := assessLifecycleStateTransition("executing", "recovering")
	if !valid.Pass || valid.Result != "pass" {
		t.Fatalf("expected EXECUTING->RECOVERING to pass, got %+v", valid)
	}
}

func TestStrictModeRequiresExplicitIDs(t *testing.T) {
	if updateCmd.Flags().Lookup("strict-control") == nil {
		t.Fatalf("update command missing --strict-control flag")
	}
	if closeCmd.Flags().Lookup("strict-control") == nil {
		t.Fatalf("close command missing --strict-control flag")
	}

	if !strictControlExplicitIDsEnabled(true) {
		t.Fatalf("explicit strict-control flag should enable strict mode")
	}

	t.Setenv("BD_STRICT_CONTROL", "true")
	if !strictControlExplicitIDsEnabled(false) {
		t.Fatalf("BD_STRICT_CONTROL=true should enable strict mode")
	}

	t.Setenv("BD_STRICT_CONTROL", "0")
	if strictControlExplicitIDsEnabled(false) {
		t.Fatalf("BD_STRICT_CONTROL=0 should disable strict mode without flag")
	}
}

func TestCapabilityProbeFailsUnknownHelpSubcommand(t *testing.T) {
	err := validateUnknownHelpSubcommandProbe(rootCmd, []string{"flow", "definitely-not-a-subcommand", "--help"})
	if err == nil {
		t.Fatalf("expected unknown help subcommand probe to fail")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("expected unknown-subcommand diagnostic, got %v", err)
	}

	if err := validateUnknownHelpSubcommandProbe(rootCmd, []string{"flow", "claim-next", "--help"}); err != nil {
		t.Fatalf("expected valid flow subcommand help probe to pass, got %v", err)
	}
	if err := validateUnknownHelpSubcommandProbe(rootCmd, []string{"flow", "--help"}); err != nil {
		t.Fatalf("expected top-level help probe to pass, got %v", err)
	}
}

func TestRuntimeBinaryCapabilityParityManifest(t *testing.T) {
	manifest := runtimeCapabilityManifest()
	helpByPath := make(map[string]string, len(manifest))
	for _, item := range manifest {
		key := strings.Join(item.CommandPath, " ")
		helpByPath[key] = strings.Join(item.MustContain, "\n")
	}

	failures := evaluateRuntimeCapabilityManifest(manifest, func(path []string) (string, error) {
		return helpByPath[strings.Join(path, " ")], nil
	})
	if len(failures) != 0 {
		t.Fatalf("expected manifest parity check to pass when all tokens are present, got %v", failures)
	}

	helpByPath["flow"] = "claim-next\nclose-safe"
	failures = evaluateRuntimeCapabilityManifest(manifest, func(path []string) (string, error) {
		return helpByPath[strings.Join(path, " ")], nil
	})
	if len(failures) == 0 {
		t.Fatalf("expected parity check to fail when required flow tokens are missing")
	}
	joined := strings.Join(failures, "\n")
	if !strings.Contains(joined, `flow: missing token "preclaim-lint"`) {
		t.Fatalf("expected missing-token diagnostics, got %v", failures)
	}
}

func TestRuntimeParityManifestCoversCriticalControlPlaneSurface(t *testing.T) {
	manifest := runtimeCapabilityManifest()
	hasProbeToken := func(path []string, token string) bool {
		for _, probe := range manifest {
			if !reflect.DeepEqual(probe.CommandPath, path) {
				continue
			}
			for _, candidate := range probe.MustContain {
				if candidate == token {
					return true
				}
			}
		}
		return false
	}

	required := []struct {
		path  []string
		token string
	}{
		{path: nil, token: "state"},
		{path: []string{"flow"}, token: "execution-rollback"},
		{path: []string{"flow", "claim-next"}, token: "--require-anchor"},
		{path: []string{"flow", "close-safe"}, token: "--require-parent-cascade"},
		{path: []string{"intake", "audit"}, token: "--write-proof"},
		{path: []string{"preflight"}, token: "--state-from"},
		{path: []string{"recover"}, token: "--state-from"},
		{path: []string{"resume"}, token: "--state-from"},
		{path: []string{"reason", "lint"}, token: "--state-from"},
		{path: []string{"land"}, token: "--state-from"},
	}
	for _, req := range required {
		if !hasProbeToken(req.path, req.token) {
			t.Fatalf("runtime parity manifest missing token %q for path %v", req.token, req.path)
		}
	}
}

func TestStrictControlMandatoryAnchorAndParentCascade(t *testing.T) {
	if flowClaimNextCmd.Flags().Lookup("allow-missing-anchor") == nil {
		t.Fatalf("flow claim-next missing --allow-missing-anchor flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("allow-open-children") == nil {
		t.Fatalf("flow close-safe missing --allow-open-children flag")
	}

	t.Setenv("BD_STRICT_CONTROL", "true")
	if !effectiveRequireAnchor(false, false) {
		t.Fatalf("strict control should require anchor by default")
	}
	if effectiveRequireAnchor(false, true) {
		t.Fatalf("--allow-missing-anchor should bypass strict default")
	}
	if !effectiveRequireParentCascade(false, false) {
		t.Fatalf("strict control should require parent cascade by default")
	}
	if effectiveRequireParentCascade(false, true) {
		t.Fatalf("--allow-open-children should bypass strict default")
	}

	t.Setenv("BD_STRICT_CONTROL", "0")
	if effectiveRequireAnchor(false, false) {
		t.Fatalf("strict defaults should disable when BD_STRICT_CONTROL=0")
	}
	if effectiveRequireParentCascade(false, false) {
		t.Fatalf("strict defaults should disable for parent cascade when BD_STRICT_CONTROL=0")
	}
}

func TestClaimBlockedWithoutIntakeAuditPass(t *testing.T) {
	notes := strings.Join([]string{
		"INTAKE-MAP-BEGIN",
		"PLAN-COUNT: 4",
		"PLAN-1 -> bd-1",
		"INTAKE-MAP-END",
	}, "\n")

	if got := extractPlanCountFromNotes(notes); got != 4 {
		t.Fatalf("expected PLAN-COUNT parse to return 4, got %d", got)
	}
	if !intakeClaimGateRequired(4) {
		t.Fatalf("expected plan-count>=2 to require intake gate")
	}
	if intakeAuditPassed(notes) {
		t.Fatalf("expected intake audit to be missing in notes")
	}
	if !intakeAuditPassed(notes + "\nINTAKE_AUDIT=PASS\n") {
		t.Fatalf("expected intake audit marker to be detected")
	}
}

func TestPostClaimViabilityDeferredBlockers(t *testing.T) {
	deps := []*types.IssueWithDependencyMetadata{
		{
			Issue:          types.Issue{ID: "bd-b1", Status: types.StatusDeferred},
			DependencyType: types.DepBlocks,
		},
		{
			Issue:          types.Issue{ID: "bd-p1", Status: types.StatusOpen},
			DependencyType: types.DepParentChild,
		},
		{
			Issue:          types.Issue{ID: "bd-r1", Status: types.StatusDeferred},
			DependencyType: types.DepRelated,
		},
	}
	deferred := detectDeferredBlockerIDs(deps)
	if len(deferred) != 1 || deferred[0] != "bd-b1" {
		t.Fatalf("expected exactly one deferred blocker ID, got %v", deferred)
	}

	if got := summarizePostClaimViability(false, nil, nil); got != "viable" {
		t.Fatalf("expected viable for unblocked issue, got %q", got)
	}
	if got := summarizePostClaimViability(true, []string{"bd-x"}, nil); got != "blocked" {
		t.Fatalf("expected blocked viability state, got %q", got)
	}
	if got := summarizePostClaimViability(true, []string{"bd-x"}, []string{"bd-b1"}); got != "blocked_by_deferred" {
		t.Fatalf("expected blocked_by_deferred viability state, got %q", got)
	}
}

func TestBaselineVerifyDeterministicStates(t *testing.T) {
	if flowBaselineVerifyCmd == nil {
		t.Fatalf("flow baseline-verify command is nil")
	}
	if flowBaselineVerifyCmd.Flags().Lookup("issue") == nil {
		t.Fatalf("flow baseline-verify missing --issue flag")
	}
	if flowBaselineVerifyCmd.Flags().Lookup("cmd") == nil {
		t.Fatalf("flow baseline-verify missing --cmd flag")
	}

	state, eligible, err := baselineDecisionFromError(nil)
	if err != nil {
		t.Fatalf("baselineDecisionFromError(nil) returned error: %v", err)
	}
	if state != "pass" || !eligible {
		t.Fatalf("expected pass/eligible=true, got state=%q eligible=%v", state, eligible)
	}

	state, eligible, err = baselineDecisionFromError(&exec.ExitError{})
	if err != nil {
		t.Fatalf("expected exit-error classification without hard error, got %v", err)
	}
	if state != "fail" || eligible {
		t.Fatalf("expected fail/eligible=false, got state=%q eligible=%v", state, eligible)
	}

	compact := compactBaselineOutput("line1\nline2\nline3\nline4\nline5", nil)
	if strings.Count(compact, "|") != 3 {
		t.Fatalf("expected output compaction to 4 lines, got %q", compact)
	}
}

func TestPlanningExitAuditDeterministicChecks(t *testing.T) {
	if intakePlanningExitCmd == nil {
		t.Fatalf("intake planning-exit command is nil")
	}
	if intakePlanningExitCmd.Flags().Lookup("parent") == nil {
		t.Fatalf("intake planning-exit missing --parent flag")
	}
	if intakePlanningExitCmd.Flags().Lookup("ready-min") == nil {
		t.Fatalf("intake planning-exit missing --ready-min flag")
	}

	children := map[string]*types.Issue{
		"bd-a": {
			ID:     "bd-a",
			Title:  "Task A",
			Labels: []string{"module/a", "area/infra"},
		},
		"bd-b": {
			ID:     "bd-b",
			Title:  "Task B",
			Labels: []string{"module/b", "area/infra"},
		},
		"bd-g": {
			ID:        "bd-g",
			Title:     "Gate: Integrate A/B",
			IssueType: types.IssueType("gate"),
			Labels:    []string{"module/a", "type/gate"},
		},
	}

	violations := detectDirectCrossModuleBlocks(children, map[string][]*types.Dependency{
		"bd-a": {
			{IssueID: "bd-a", DependsOnID: "bd-b", Type: types.DepBlocks},
		},
	})
	if len(violations) != 1 || violations[0] != "bd-a|bd-b" {
		t.Fatalf("expected one cross-module direct blocks violation, got %v", violations)
	}

	violations = detectDirectCrossModuleBlocks(children, map[string][]*types.Dependency{
		"bd-a": {
			{IssueID: "bd-a", DependsOnID: "bd-g", Type: types.DepBlocks},
		},
	})
	if len(violations) != 0 {
		t.Fatalf("expected gate-mediated edge to be exempt, got %v", violations)
	}
}

func TestIntakeMapSyncRewritesCanonicalBlock(t *testing.T) {
	if intakeMapSyncCmd == nil {
		t.Fatalf("intake map-sync command is nil")
	}
	if intakeMapSyncCmd.Flags().Lookup("epic") == nil {
		t.Fatalf("intake map-sync missing --epic flag")
	}
	if intakeMapSyncCmd.Flags().Lookup("plan") == nil {
		t.Fatalf("intake map-sync missing --plan flag")
	}
	if intakeMapSyncCmd.Flags().Lookup("ready-wave") == nil {
		t.Fatalf("intake map-sync missing --ready-wave flag")
	}

	block := buildCanonicalIntakeMapBlock(
		map[int]string{1: "bd-1", 2: "bd-2"},
		[]string{"bd-1", "bd-2"},
		true,
		map[int]string{1: "bd-9"},
	)
	if !strings.Contains(block, "INTAKE-MAP-BEGIN") || !strings.Contains(block, "INTAKE-MAP-END") {
		t.Fatalf("canonical intake map missing delimiters: %q", block)
	}
	if !strings.Contains(block, "PLAN-COUNT: 2") || !strings.Contains(block, "FINDING-COUNT: 1") {
		t.Fatalf("canonical intake map missing expected counts: %q", block)
	}

	existing := strings.Join([]string{
		"existing notes",
		"INTAKE-MAP-BEGIN",
		"PLAN-COUNT: 1",
		"PLAN-1 -> old",
		"INTAKE-MAP-END",
		"tail notes",
	}, "\n")
	updated := upsertIntakeMapBlock(existing, block)
	if strings.Count(updated, "INTAKE-MAP-BEGIN") != 1 || strings.Count(updated, "INTAKE-MAP-END") != 1 {
		t.Fatalf("expected exactly one canonical intake map block after rewrite, got %q", updated)
	}
	if !strings.Contains(updated, "PLAN-1 -> bd-1") || !strings.Contains(updated, "PLAN-2 -> bd-2") {
		t.Fatalf("updated notes missing new PLAN mappings: %q", updated)
	}
	if strings.Contains(updated, "PLAN-1 -> old") {
		t.Fatalf("old intake map content should be removed: %q", updated)
	}
}

func TestIntakeAuditClosedEpicMode(t *testing.T) {
	if got := resolveIntakeAuditMode(types.StatusClosed); got != intakeAuditModeClosedEpic {
		t.Fatalf("expected closed epic mode, got %q", got)
	}
	if got := resolveIntakeAuditMode(types.StatusOpen); got != intakeAuditModeOpenChildren {
		t.Fatalf("expected open-children mode for open epic, got %q", got)
	}

	openFilter := intakeAuditChildrenFilter("bd-parent", intakeAuditModeOpenChildren)
	if openFilter.Status == nil || *openFilter.Status != types.StatusOpen {
		t.Fatalf("open mode must enforce open-child filter, got %#v", openFilter)
	}

	closedFilter := intakeAuditChildrenFilter("bd-parent", intakeAuditModeClosedEpic)
	if closedFilter.Status != nil {
		t.Fatalf("closed-epic mode must query all children, got status filter %#v", closedFilter.Status)
	}

	if !intakeAuditReadySetRequired(intakeAuditModeOpenChildren) {
		t.Fatalf("open-children mode should require ready-set equality")
	}
	if intakeAuditReadySetRequired(intakeAuditModeClosedEpic) {
		t.Fatalf("closed-epic mode should not require ready-set equality")
	}
}

func TestIntakeAuditClosedEpicWarnsOnHistoricalReadyWaveDrift(t *testing.T) {
	warn := buildClosedEpicReadyWaveDriftWarning(
		intakeAuditModeClosedEpic,
		[]string{"bd-1", "bd-2"},
		[]string{"bd-1", "bd-3"},
	)
	if warn == nil {
		t.Fatalf("expected closed-epic mismatch to emit drift warning metadata")
	}
	if warn["warning"] != "historical_ready_wave_drift" {
		t.Fatalf("expected historical drift warning marker, got %v", warn["warning"])
	}

	missing, ok := warn["missing_from_actual"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "bd-2" {
		t.Fatalf("expected missing_from_actual=[bd-2], got %v", warn["missing_from_actual"])
	}
	unexpected, ok := warn["unexpected_in_actual"].([]string)
	if !ok || len(unexpected) != 1 || unexpected[0] != "bd-3" {
		t.Fatalf("expected unexpected_in_actual=[bd-3], got %v", warn["unexpected_in_actual"])
	}

	if got := buildClosedEpicReadyWaveDriftWarning(intakeAuditModeOpenChildren, []string{"bd-1"}, []string{}); got != nil {
		t.Fatalf("open-children mode should not emit closed-epic warning metadata, got %v", got)
	}
	if got := buildClosedEpicReadyWaveDriftWarning(intakeAuditModeClosedEpic, []string{"bd-1"}, []string{"bd-1"}); got != nil {
		t.Fatalf("matching ready sets should not emit drift warning metadata, got %v", got)
	}
}

func TestDepAddBatchDeterministicResults(t *testing.T) {
	if depAddBatchCmd == nil {
		t.Fatalf("dep add-batch command is nil")
	}
	if depAddBatchCmd.Flags().Lookup("edge") == nil {
		t.Fatalf("dep add-batch missing --edge flag")
	}
	if depAddBatchCmd.Flags().Lookup("type") == nil {
		t.Fatalf("dep add-batch missing --type flag")
	}
	if depAddBatchCmd.Flags().Lookup("stop-on-error") == nil {
		t.Fatalf("dep add-batch missing --stop-on-error flag")
	}
	if depAddBatchCmd.Flags().Lookup("guard-cycles") == nil {
		t.Fatalf("dep add-batch missing --guard-cycles flag")
	}

	from, to, err := parseDepBatchEdge("bd-1:bd-2")
	if err != nil || from != "bd-1" || to != "bd-2" {
		t.Fatalf("expected colon edge parse success, got from=%q to=%q err=%v", from, to, err)
	}

	from, to, err = parseDepBatchEdge("bd-3 -> bd-4")
	if err != nil || from != "bd-3" || to != "bd-4" {
		t.Fatalf("expected arrow edge parse success, got from=%q to=%q err=%v", from, to, err)
	}

	if _, _, err := parseDepBatchEdge("malformed"); err == nil {
		t.Fatalf("expected malformed edge parse failure")
	}

	if !depBatchShouldStop(true, true, false) {
		t.Fatalf("expected stop-on-error policy to stop on edge failure")
	}
	if depBatchShouldStop(false, true, true) {
		t.Fatalf("expected stop-on-error=false to continue despite failures")
	}
}

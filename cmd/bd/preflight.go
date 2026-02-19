package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/cmd/bd/doctor"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/types"
)

// CheckResult represents the result of a single preflight check.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped,omitempty"`
	Warning bool   `json:"warning,omitempty"`
	Output  string `json:"output,omitempty"`
	Command string `json:"command"`
}

// PreflightResult represents the overall preflight check results.
type PreflightResult struct {
	Checks  []CheckResult `json:"checks"`
	Passed  bool          `json:"passed"`
	Summary string        `json:"summary"`
}

type preflightGateAssessment struct {
	Pass                  bool                   `json:"pass"`
	Blockers              []string               `json:"blockers"`
	DoctorFailCount       int                    `json:"doctor_fail_count"`
	DoctorCriticalWarns   int                    `json:"doctor_critical_warn_count"`
	ValidationOnCreate    string                 `json:"validation_on_create"`
	RequireDescription    bool                   `json:"require_description"`
	HardeningBefore       map[string]interface{} `json:"hardening_before,omitempty"`
	HardeningAfter        map[string]interface{} `json:"hardening_after,omitempty"`
	HardeningRemediated   bool                   `json:"hardening_remediated,omitempty"`
	HardeningRemediateErr string                 `json:"hardening_remediation_error,omitempty"`
	WIPCount              int                    `json:"wip_count"`
	WIPGateEnforced       bool                   `json:"wip_gate_enforced"`
	Action                string                 `json:"action"`
}

var preflightCriticalDoctorWarnings = map[string]struct{}{
	"Repo Fingerprint": {},
	"DB-JSONL Sync":    {},
	"Dolt-JSONL Sync":  {},
	"Database Config":  {},
	"Sync Divergence":  {},
	"Issues Tracking":  {},
}

var (
	preflightGateAction    string
	preflightSkipWIPGate   bool
	preflightRuntimeBinary string
)

var preflightCmd = &cobra.Command{
	Use:     "preflight",
	GroupID: "maint",
	Short:   "Show PR readiness checklist",
	Long: `Display a checklist of common pre-PR checks for contributors.

This command helps catch common issues before pushing to CI:
- Tests not run locally
- Lint errors
- Stale nix vendorHash
- Version mismatches

Examples:
  bd preflight              # Show checklist
  bd preflight --check      # Run checks automatically
  bd preflight --check --json  # JSON output for programmatic use
`,
	Run: runPreflight,
}

func init() {
	preflightCmd.Flags().Bool("check", false, "Run checks automatically")
	preflightCmd.Flags().Bool("fix", false, "Auto-fix issues where possible (not yet implemented)")
	preflightCmd.Flags().Bool("json", false, "Output results as JSON")
	preflightGateCmd.Flags().StringVar(&preflightGateAction, "action", "claim", "Gate scope: claim or write")
	preflightGateCmd.Flags().BoolVar(&preflightSkipWIPGate, "skip-wip-gate", false, "Skip WIP gate (resume-remediation only)")
	preflightRuntimeParityCmd.Flags().StringVar(&preflightRuntimeBinary, "binary", "", "Path to runtime bd binary to verify (defaults to current executable)")
	preflightCmd.AddCommand(preflightGateCmd)
	preflightCmd.AddCommand(preflightRuntimeParityCmd)

	rootCmd.AddCommand(preflightCmd)
}

var preflightGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Run deterministic control-plane readiness gates",
	Run:   runPreflightGate,
}

type runtimeCapabilityProbe struct {
	CommandPath []string `json:"command_path"`
	MustContain []string `json:"must_contain"`
}

var preflightRuntimeParityCmd = &cobra.Command{
	Use:   "runtime-parity",
	Short: "Validate runtime binary capability surface against control-plane manifest",
	Run:   runPreflightRuntimeParity,
}

func runPreflightGate(cmd *cobra.Command, args []string) {
	ctx := rootCtx
	action := strings.ToLower(strings.TrimSpace(preflightGateAction))
	if action != "claim" && action != "write" {
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "preflight gate",
			Result:  "invalid_input",
			Details: map[string]interface{}{
				"message": "invalid --action value (expected claim or write)",
				"action":  preflightGateAction,
			},
			Events: []string{"preflight_invalid_input"},
		}, 1)
		return
	}

	validationOnCreate, _ := store.GetConfig(ctx, "validation.on-create")
	requireDescriptionRaw, _ := store.GetConfig(ctx, "create.require-description")
	requireDescription := parseConfigBool(requireDescriptionRaw)
	hardeningBefore := map[string]interface{}{
		"validation_on_create": strings.TrimSpace(validationOnCreate),
		"require_description":  requireDescription,
	}
	validationOnCreate, requireDescription, remediated, remediateErr := ensureHardeningInvariant(ctx)
	hardeningAfter := map[string]interface{}{
		"validation_on_create": strings.TrimSpace(validationOnCreate),
		"require_description":  requireDescription,
	}

	workingPath, err := os.Getwd()
	if err != nil {
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "preflight gate",
			Result:  "system_error",
			Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve working directory: %v", err)},
			Events:  []string{"preflight_failed"},
		}, 1)
		return
	}
	checks := runPreflightGateChecks(ctx, workingPath)

	wipGateEnforced := !preflightSkipWIPGate
	wipCount := 0
	wipIDs := []string{}
	if wipGateEnforced {
		preflightActor := strings.TrimSpace(actor)
		if preflightActor == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "preflight gate",
				Result:  "invalid_input",
				Details: map[string]interface{}{
					"message": "actor is required when WIP gate is enabled (use --actor or BD_ACTOR/BEADS_ACTOR)",
				},
				Events: []string{"preflight_invalid_input"},
			}, 1)
			return
		}
		inProgress := types.StatusInProgress
		filter := types.IssueFilter{
			Status:   &inProgress,
			Assignee: &preflightActor,
			Limit:    50,
		}
		wipIssues, qErr := store.SearchIssues(ctx, "", filter)
		if qErr != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "preflight gate",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("wip gate query failed: %v", qErr)},
				Events:  []string{"preflight_failed"},
			}, 1)
			return
		}
		wipCount = len(wipIssues)
		for _, issue := range wipIssues {
			wipIDs = append(wipIDs, issue.ID)
		}
	}

	assessment := evaluatePreflightGate(
		action,
		validationOnCreate,
		requireDescription,
		checks,
		wipCount,
		wipGateEnforced,
	)
	assessment.HardeningBefore = hardeningBefore
	assessment.HardeningAfter = hardeningAfter
	assessment.HardeningRemediated = remediated
	if remediateErr != nil {
		assessment.HardeningRemediateErr = remediateErr.Error()
		assessment.Pass = false
		assessment.Blockers = append(assessment.Blockers, "hardening.remediation_failed")
	}

	details := map[string]interface{}{
		"action":                      assessment.Action,
		"blockers":                    assessment.Blockers,
		"doctor_fail_count":           assessment.DoctorFailCount,
		"doctor_critical_warn_count":  assessment.DoctorCriticalWarns,
		"validation_on_create":        assessment.ValidationOnCreate,
		"require_description":         assessment.RequireDescription,
		"hardening_before":            assessment.HardeningBefore,
		"hardening_after":             assessment.HardeningAfter,
		"hardening_remediated":        assessment.HardeningRemediated,
		"hardening_remediation_error": assessment.HardeningRemediateErr,
		"wip_count":                   assessment.WIPCount,
		"wip_gate_enforced":           assessment.WIPGateEnforced,
		"wip_issue_ids":               wipIDs,
	}

	if !assessment.Pass {
		finishEnvelope(commandEnvelope{
			OK:              false,
			Command:         "preflight gate",
			Result:          "blocked",
			Details:         details,
			RecoveryCommand: "bd preflight gate --action claim",
			Events:          []string{"preflight_blocked"},
		}, exitCodePolicyViolation)
		return
	}

	finishEnvelope(commandEnvelope{
		OK:      true,
		Command: "preflight gate",
		Result:  "pass",
		Details: details,
		Events:  []string{"preflight_passed"},
	}, 0)
}

func runPreflightGateChecks(ctx context.Context, path string) []doctorCheck {
	// Keep preflight gate bounded and deterministic: only checks that contribute
	// to control-plane blockers are evaluated here.
	raw := []doctor.DoctorCheck{
		doctor.CheckInstallation(path),
		preflightCheckRepoFingerprint(ctx),
		doctor.CheckDatabaseConfig(path),
		doctor.CheckSyncDivergence(path),
		doctor.CheckIssuesTracking(),
	}
	out := make([]doctorCheck, 0, len(raw))
	for _, check := range raw {
		out = append(out, convertDoctorCheck(check))
	}
	return out
}

func preflightCheckRepoFingerprint(ctx context.Context) doctor.DoctorCheck {
	if store == nil {
		return doctor.DoctorCheck{
			Name:    "Repo Fingerprint",
			Status:  doctor.StatusWarning,
			Message: "Store unavailable",
		}
	}

	storedRepoID, err := store.GetMetadata(ctx, "repo_id")
	if err != nil {
		return doctor.DoctorCheck{
			Name:    "Repo Fingerprint",
			Status:  doctor.StatusWarning,
			Message: "Unable to read repo fingerprint",
			Detail:  err.Error(),
		}
	}
	if strings.TrimSpace(storedRepoID) == "" {
		return doctor.DoctorCheck{
			Name:    "Repo Fingerprint",
			Status:  doctor.StatusWarning,
			Message: "Missing repo fingerprint metadata",
		}
	}

	currentRepoID, err := beads.ComputeRepoID()
	if err != nil {
		return doctor.DoctorCheck{
			Name:    "Repo Fingerprint",
			Status:  doctor.StatusWarning,
			Message: "Unable to compute current repo ID",
			Detail:  err.Error(),
		}
	}

	if storedRepoID != currentRepoID {
		return doctor.DoctorCheck{
			Name:    "Repo Fingerprint",
			Status:  doctor.StatusError,
			Message: "Database belongs to different repository",
			Detail:  fmt.Sprintf("stored: %s, current: %s", shortRepoID(storedRepoID), shortRepoID(currentRepoID)),
		}
	}

	return doctor.DoctorCheck{
		Name:    "Repo Fingerprint",
		Status:  doctor.StatusOK,
		Message: fmt.Sprintf("Verified (%s)", shortRepoID(currentRepoID)),
	}
}

func shortRepoID(id string) string {
	trimmed := strings.TrimSpace(id)
	if len(trimmed) <= 8 {
		return trimmed
	}
	return trimmed[:8]
}

func runPreflightRuntimeParity(cmd *cobra.Command, args []string) {
	binary := strings.TrimSpace(preflightRuntimeBinary)
	if binary == "" {
		binary = os.Args[0]
	}

	manifest := runtimeCapabilityManifest()
	failures := evaluateRuntimeCapabilityManifest(manifest, func(path []string) (string, error) {
		return probeCapabilityHelp(binary, path)
	})
	if len(failures) > 0 {
		finishEnvelope(commandEnvelope{
			OK:      false,
			Command: "preflight runtime-parity",
			Result:  "capability_mismatch",
			Details: map[string]interface{}{
				"binary":         binary,
				"failure_count":  len(failures),
				"failures":       failures,
				"manifest_count": len(manifest),
			},
			RecoveryCommand: "Rebuild and refresh the pinned runtime binary from current source",
			Events:          []string{"runtime_parity_failed"},
		}, exitCodePolicyViolation)
		return
	}

	finishEnvelope(commandEnvelope{
		OK:      true,
		Command: "preflight runtime-parity",
		Result:  "capability_match",
		Details: map[string]interface{}{
			"binary":         binary,
			"manifest_count": len(manifest),
		},
		Events: []string{"runtime_parity_passed"},
	}, 0)
}

func parseConfigBool(raw string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && parsed
}

func runtimeCapabilityManifest() []runtimeCapabilityProbe {
	return []runtimeCapabilityProbe{
		{CommandPath: nil, MustContain: []string{"flow", "intake", "preflight", "recover", "land", "cutover", "state"}},
		{CommandPath: []string{"flow"}, MustContain: []string{"claim-next", "preclaim-lint", "baseline-verify", "transition", "close-safe", "priority-poll", "execution-rollback"}},
		{CommandPath: []string{"flow", "claim-next"}, MustContain: []string{"--require-anchor", "--allow-missing-anchor"}},
		{CommandPath: []string{"flow", "close-safe"}, MustContain: []string{"--require-parent-cascade", "--allow-open-children", "--require-priority-poll"}},
		{CommandPath: []string{"intake"}, MustContain: []string{"audit", "map-sync", "planning-exit", "bulk-guard"}},
		{CommandPath: []string{"intake", "audit"}, MustContain: []string{"--epic", "--write-proof"}},
		{CommandPath: []string{"preflight"}, MustContain: []string{"gate", "runtime-parity"}},
		{CommandPath: []string{"recover"}, MustContain: []string{"loop", "signature"}},
		{CommandPath: []string{"land"}, MustContain: []string{"--require-quality", "--require-handoff", "--state-from", "--state-to"}},
		{CommandPath: []string{"dep"}, MustContain: []string{"tree", "cycles", "add"}},
	}
}

func evaluateRuntimeCapabilityManifest(manifest []runtimeCapabilityProbe, probe func(path []string) (string, error)) []string {
	failures := make([]string, 0)
	for _, item := range manifest {
		helpText, err := probe(item.CommandPath)
		path := "root"
		if len(item.CommandPath) > 0 {
			path = strings.Join(item.CommandPath, " ")
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: help probe failed: %v", path, err))
			continue
		}
		for _, token := range item.MustContain {
			if !strings.Contains(helpText, token) {
				failures = append(failures, fmt.Sprintf("%s: missing token %q", path, token))
			}
		}
	}
	sort.Strings(failures)
	return failures
}

func probeCapabilityHelp(binary string, commandPath []string) (string, error) {
	args := make([]string, 0, len(commandPath)+1)
	args = append(args, commandPath...)
	args = append(args, "--help")
	out, err := exec.Command(binary, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %v | %s", binary, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func ensureHardeningInvariant(ctx context.Context) (string, bool, bool, error) {
	validationOnCreate, _ := store.GetConfig(ctx, "validation.on-create")
	requireDescriptionRaw, _ := store.GetConfig(ctx, "create.require-description")
	requireDescription := parseConfigBool(requireDescriptionRaw)
	return remediateHardeningInvariant(validationOnCreate, requireDescription, func(key, value string) error {
		return store.SetConfig(ctx, key, value)
	})
}

func remediateHardeningInvariant(validationOnCreate string, requireDescription bool, setter func(key, value string) error) (string, bool, bool, error) {
	remediated := false
	currentValidation := strings.TrimSpace(validationOnCreate)
	currentRequireDescription := requireDescription

	if currentValidation != "error" {
		if err := setter("validation.on-create", "error"); err != nil {
			return validationOnCreate, requireDescription, remediated, fmt.Errorf("failed to set validation.on-create=error: %w", err)
		}
		currentValidation = "error"
		remediated = true
	}
	if !currentRequireDescription {
		if err := setter("create.require-description", "true"); err != nil {
			return currentValidation, currentRequireDescription, remediated, fmt.Errorf("failed to set create.require-description=true: %w", err)
		}
		currentRequireDescription = true
		remediated = true
	}
	return currentValidation, currentRequireDescription, remediated, nil
}

func evaluatePreflightGate(action, validationOnCreate string, requireDescription bool, checks []doctorCheck, wipCount int, enforceWIP bool) preflightGateAssessment {
	failCount := 0
	criticalWarnCount := 0
	blockers := make([]string, 0)

	for _, check := range checks {
		switch strings.ToLower(strings.TrimSpace(check.Status)) {
		case "fail", statusError:
			failCount++
		case statusWarning:
			if _, critical := preflightCriticalDoctorWarnings[check.Name]; critical {
				criticalWarnCount++
			}
		}
	}

	if strings.TrimSpace(validationOnCreate) != "error" {
		blockers = append(blockers, "hardening.validation.on-create")
	}
	if !requireDescription {
		blockers = append(blockers, "hardening.create.require-description")
	}
	if failCount > 0 {
		blockers = append(blockers, "doctor.fail_or_error")
	}
	if criticalWarnCount > 0 {
		blockers = append(blockers, "doctor.critical_warning")
	}
	if enforceWIP && wipCount > 0 {
		blockers = append(blockers, "wip.gate")
	}

	return preflightGateAssessment{
		Pass:                len(blockers) == 0,
		Blockers:            blockers,
		DoctorFailCount:     failCount,
		DoctorCriticalWarns: criticalWarnCount,
		ValidationOnCreate:  strings.TrimSpace(validationOnCreate),
		RequireDescription:  requireDescription,
		WIPCount:            wipCount,
		WIPGateEnforced:     enforceWIP,
		Action:              action,
	}
}

func runPreflight(cmd *cobra.Command, args []string) {
	check, _ := cmd.Flags().GetBool("check")
	fix, _ := cmd.Flags().GetBool("fix")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if fix {
		fmt.Println("Note: --fix is not yet implemented.")
		fmt.Println("See bd-lfak.3 through bd-lfak.5 for implementation roadmap.")
		fmt.Println()
	}

	if check {
		runChecks(jsonOutput)
		return
	}

	// Static checklist mode
	fmt.Println("PR Readiness Checklist:")
	fmt.Println()
	fmt.Println("[ ] Tests pass: go test -short ./...")
	fmt.Println("[ ] Lint passes: golangci-lint run ./...")
	fmt.Println("[ ] No beads pollution: check .beads/issues.jsonl diff")
	fmt.Println("[ ] Nix hash current: go.sum unchanged or vendorHash updated")
	fmt.Println("[ ] Version sync: version.go matches default.nix")
	fmt.Println()
	fmt.Println("Run 'bd preflight --check' to validate automatically.")
}

// runChecks executes all preflight checks and reports results.
func runChecks(jsonOutput bool) {
	var results []CheckResult

	// Run test check
	testResult := runTestCheck()
	results = append(results, testResult)

	// Run lint check
	lintResult := runLintCheck()
	results = append(results, lintResult)

	// Run nix hash check
	nixResult := runNixHashCheck()
	results = append(results, nixResult)

	// Run version sync check
	versionResult := runVersionSyncCheck()
	results = append(results, versionResult)

	// Calculate overall result
	allPassed := true
	passCount := 0
	skipCount := 0
	warnCount := 0
	for _, r := range results {
		if r.Skipped {
			skipCount++
		} else if r.Warning {
			warnCount++
			// Warnings don't fail the overall result but count as "not passed"
		} else if r.Passed {
			passCount++
		} else {
			allPassed = false
		}
	}

	runCount := len(results) - skipCount
	summary := fmt.Sprintf("%d/%d checks passed", passCount, runCount)
	if warnCount > 0 {
		summary += fmt.Sprintf(", %d warning(s)", warnCount)
	}
	if skipCount > 0 {
		summary += fmt.Sprintf(" (%d skipped)", skipCount)
	}

	if jsonOutput {
		result := PreflightResult{
			Checks:  results,
			Passed:  allPassed,
			Summary: summary,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding preflight result: %v\n", err)
		}
	} else {
		// Human-readable output
		for _, r := range results {
			if r.Skipped {
				fmt.Printf("⚠ %s (skipped)\n", r.Name)
			} else if r.Warning {
				fmt.Printf("⚠ %s\n", r.Name)
			} else if r.Passed {
				fmt.Printf("✓ %s\n", r.Name)
			} else {
				fmt.Printf("✗ %s\n", r.Name)
			}
			fmt.Printf("  Command: %s\n", r.Command)
			if r.Skipped && r.Output != "" {
				// Show skip reason
				fmt.Printf("  Reason: %s\n", r.Output)
			} else if r.Warning && r.Output != "" {
				// Show warning message
				fmt.Printf("  Warning: %s\n", r.Output)
			} else if !r.Passed && r.Output != "" {
				// Truncate output for terminal display
				output := truncateOutput(r.Output, 500)
				fmt.Printf("  Output:\n")
				for _, line := range strings.Split(output, "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
			fmt.Println()
		}
		fmt.Println(summary)
	}

	if !allPassed {
		os.Exit(1)
	}
}

// runTestCheck runs go test -short ./... and returns the result.
func runTestCheck() CheckResult {
	command := "go test -short ./..."
	cmd := exec.Command("go", "test", "-short", "./...")
	output, err := cmd.CombinedOutput()

	return CheckResult{
		Name:    "Tests pass",
		Passed:  err == nil,
		Output:  string(output),
		Command: command,
	}
}

// runLintCheck runs golangci-lint and returns the result.
func runLintCheck() CheckResult {
	command := "golangci-lint run ./..."

	// Check if golangci-lint is available
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return CheckResult{
			Name:    "Lint passes",
			Passed:  false,
			Skipped: true,
			Output:  "golangci-lint not found in PATH",
			Command: command,
		}
	}

	cmd := exec.Command("golangci-lint", "run", "./...")
	output, err := cmd.CombinedOutput()

	return CheckResult{
		Name:    "Lint passes",
		Passed:  err == nil,
		Output:  string(output),
		Command: command,
	}
}

// runNixHashCheck checks if go.sum has uncommitted changes that may require vendorHash update.
func runNixHashCheck() CheckResult {
	command := "git diff HEAD -- go.sum"

	// Check for unstaged changes to go.sum
	cmd := exec.Command("git", "diff", "--name-only", "HEAD", "--", "go.sum")
	output, _ := cmd.Output()

	// Check for staged changes to go.sum
	stagedCmd := exec.Command("git", "diff", "--name-only", "--cached", "--", "go.sum")
	stagedOutput, _ := stagedCmd.Output()

	hasChanges := len(strings.TrimSpace(string(output))) > 0 || len(strings.TrimSpace(string(stagedOutput))) > 0

	if hasChanges {
		return CheckResult{
			Name:    "Nix hash current",
			Passed:  false,
			Warning: true,
			Output:  "go.sum has uncommitted changes - vendorHash in default.nix may need updating",
			Command: command,
		}
	}

	return CheckResult{
		Name:    "Nix hash current",
		Passed:  true,
		Output:  "",
		Command: command,
	}
}

// runVersionSyncCheck checks that version.go matches default.nix.
func runVersionSyncCheck() CheckResult {
	command := "Compare cmd/bd/version.go and default.nix"

	// Read version.go
	versionGoContent, err := os.ReadFile("cmd/bd/version.go")
	if err != nil {
		return CheckResult{
			Name:    "Version sync",
			Passed:  false,
			Skipped: true,
			Output:  fmt.Sprintf("Cannot read cmd/bd/version.go: %v", err),
			Command: command,
		}
	}

	// Extract version from version.go
	// Pattern: Version = "X.Y.Z"
	versionGoRe := regexp.MustCompile(`Version\s*=\s*"([^"]+)"`)
	versionGoMatch := versionGoRe.FindSubmatch(versionGoContent)
	if versionGoMatch == nil {
		return CheckResult{
			Name:    "Version sync",
			Passed:  false,
			Skipped: true,
			Output:  "Cannot parse version from version.go",
			Command: command,
		}
	}
	goVersion := string(versionGoMatch[1])

	// Read default.nix
	nixContent, err := os.ReadFile("default.nix")
	if err != nil {
		// No nix file = skip version check (not an error)
		return CheckResult{
			Name:    "Version sync",
			Passed:  true,
			Skipped: true,
			Output:  "default.nix not found (skipping nix version check)",
			Command: command,
		}
	}

	// Extract version from default.nix
	// Pattern: version = "X.Y.Z";
	nixRe := regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
	nixMatch := nixRe.FindSubmatch(nixContent)
	if nixMatch == nil {
		return CheckResult{
			Name:    "Version sync",
			Passed:  false,
			Skipped: true,
			Output:  "Cannot parse version from default.nix",
			Command: command,
		}
	}
	nixVersion := string(nixMatch[1])

	if goVersion != nixVersion {
		return CheckResult{
			Name:    "Version sync",
			Passed:  false,
			Output:  fmt.Sprintf("Version mismatch: version.go=%s, default.nix=%s", goVersion, nixVersion),
			Command: command,
		}
	}

	return CheckResult{
		Name:    "Version sync",
		Passed:  true,
		Output:  fmt.Sprintf("Versions match: %s", goVersion),
		Command: command,
	}
}

// truncateOutput truncates output to maxLen characters, adding ellipsis if truncated.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[:maxLen]) + "\n... (truncated)"
}

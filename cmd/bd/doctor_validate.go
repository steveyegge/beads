package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/beads/cmd/bd/doctor"
	"github.com/steveyegge/beads/cmd/bd/doctor/fix"
	"github.com/steveyegge/beads/internal/ui"
)

// validateCheckResult pairs a doctor check with whether it can be auto-fixed.
type validateCheckResult struct {
	check   doctorCheck
	fixable bool
}

// runValidateCheck runs focused data-integrity checks and exits non-zero on failure.
func runValidateCheck(path string) {
	if !runValidateCheckInner(path) {
		os.Exit(1)
	}
}

// runValidateCheckInner runs the checks and returns true if all passed.
// Separated from runValidateCheck so tests can call it without os.Exit.
func runValidateCheckInner(path string) bool {
	checks := collectValidateChecks(path)

	overallOK := true
	for _, cr := range checks {
		if cr.check.Status == statusError || cr.check.Status == statusWarning {
			overallOK = false
		}
	}

	// JSON output
	if jsonOutput {
		result := struct {
			Path      string        `json:"path"`
			Checks    []doctorCheck `json:"checks"`
			OverallOK bool          `json:"overall_ok"`
		}{
			Path:      path,
			OverallOK: overallOK,
		}
		for _, cr := range checks {
			result.Checks = append(result.Checks, cr.check)
		}
		outputJSON(result)
		return overallOK
	}

	// Human-readable output
	fmt.Println()
	fmt.Println(ui.RenderCategory("Data Integrity"))

	var passCount, warnCount, failCount int
	for _, cr := range checks {
		var statusIcon string
		switch cr.check.Status {
		case statusOK:
			statusIcon = ui.RenderPassIcon()
			passCount++
		case statusWarning:
			statusIcon = ui.RenderWarnIcon()
			warnCount++
		case statusError:
			statusIcon = ui.RenderFailIcon()
			failCount++
		}

		fmt.Printf("  %s  %s", statusIcon, cr.check.Name)
		if cr.check.Message != "" {
			fmt.Printf("%s", ui.RenderMuted(" "+cr.check.Message))
		}
		fmt.Println()
		if cr.check.Detail != "" {
			fmt.Printf("     %s%s\n", ui.MutedStyle.Render(ui.TreeLast), ui.RenderMuted(cr.check.Detail))
		}
	}

	fmt.Println()
	fmt.Println(ui.RenderSeparator())
	fmt.Printf("%s %d passed  %s %d warnings  %s %d failed\n",
		ui.RenderPassIcon(), passCount,
		ui.RenderWarnIcon(), warnCount,
		ui.RenderFailIcon(), failCount,
	)

	// Apply fixes if --fix is set
	if doctorFix {
		applyValidateFixes(path, checks)
	} else if !overallOK {
		// Suggest --fix if there are fixable issues
		for _, cr := range checks {
			if cr.fixable && cr.check.Status != statusOK {
				fmt.Printf("\n%s\n", ui.RenderMuted("Tip: Use 'bd doctor --check=validate --fix' to auto-repair fixable issues"))
				break
			}
		}
	}

	if overallOK {
		fmt.Println()
		fmt.Printf("%s\n", ui.RenderPass("✓ All data-integrity checks passed"))
	}

	return overallOK
}

// collectValidateChecks runs the four data-integrity checks.
func collectValidateChecks(path string) []validateCheckResult {
	return []validateCheckResult{
		{check: convertDoctorCheck(doctor.CheckDuplicateIssues(path, doctorGastown, gastownDuplicatesThreshold))},
		{check: convertDoctorCheck(doctor.CheckOrphanedDependencies(path)), fixable: true},
		{check: convertDoctorCheck(doctor.CheckTestPollution(path))},
		{check: convertDoctorCheck(doctor.CheckGitConflicts(path))},
	}
}

// applyValidateFixes auto-repairs fixable validation issues.
func applyValidateFixes(path string, checks []validateCheckResult) {
	var fixable []validateCheckResult
	for _, cr := range checks {
		if cr.fixable && cr.check.Status != statusOK {
			fixable = append(fixable, cr)
		}
	}

	if len(fixable) == 0 {
		fmt.Println("\nNo auto-fixable issues found.")
		return
	}

	// Confirm unless --yes
	if !doctorYes {
		fmt.Printf("\nWill fix %d issue(s):\n", len(fixable))
		for _, cr := range fixable {
			fmt.Printf("  - %s: %s\n", cr.check.Name, cr.check.Message)
		}
		fmt.Print("\nContinue? (Y/n): ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "" && response != "y" && response != "yes" {
			fmt.Println("Fix canceled.")
			return
		}
	}

	fmt.Println("\nApplying fixes...")
	for _, cr := range fixable {
		fmt.Printf("\nFixing %s...\n", cr.check.Name)
		var err error
		switch cr.check.Name {
		case "Orphaned Dependencies":
			err = fix.OrphanedDependencies(path, doctorVerbose)
		default:
			fmt.Printf("  No automatic fix for %s\n", cr.check.Name)
			continue
		}
		if err != nil {
			fmt.Printf("  %s Error: %v\n", ui.RenderFail("✗"), err)
		} else {
			fmt.Printf("  %s Fixed\n", ui.RenderPass("✓"))
		}
	}
}

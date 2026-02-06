package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/cmd/bd/doctor"
	"github.com/steveyegge/beads/cmd/bd/doctor/fix"
	"github.com/steveyegge/beads/internal/ui"
)

var (
	validateFixAll bool
	validateYes    bool
)

// validateResult holds the outcome of all validation checks
type validateResult struct {
	Path      string         `json:"path"`
	Checks    []doctorCheck  `json:"checks"`
	OverallOK bool           `json:"overall_ok"`
}

var validateCmd = &cobra.Command{
	Use:     "validate [path]",
	GroupID: "maint",
	Short:   "Run data-integrity health checks",
	Long: `Run focused data-integrity checks on the beads database.

Unlike 'bd doctor' which checks the full installation health (git hooks,
daemon status, CLI version, etc.), 'bd validate' focuses on data quality:

  - Duplicate issues (identical open issues)
  - Orphaned dependencies (references to non-existent issues)
  - Test pollution (test data leaked into production)
  - Git conflicts (unresolved merge markers in JSONL)

Use --fix-all to auto-repair fixable issues (orphaned dependencies).
Some issues require manual intervention (duplicates, git conflicts).

Examples:
  bd validate              # Check current directory
  bd validate /path/to/repo  # Check specific path
  bd validate --fix-all    # Auto-fix what can be fixed
  bd validate --fix-all -y # Auto-fix without confirmation
  bd validate --json       # Machine-readable output`,
	Run: func(cmd *cobra.Command, args []string) {
		var checkPath string
		if len(args) > 0 {
			checkPath = args[0]
		} else if beadsDir := os.Getenv("BEADS_DIR"); beadsDir != "" {
			checkPath = filepath.Dir(beadsDir)
		} else {
			checkPath = "."
		}

		absPath, err := filepath.Abs(checkPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve path: %v\n", err)
			os.Exit(1)
		}

		result := runValidation(absPath)

		if validateFixAll {
			applyValidationFixes(absPath, result)
			// Re-run to show updated state
			result = runValidation(absPath)
		}

		if jsonOutput {
			outputJSON(result)
		} else {
			printValidation(result)
		}

		if !result.OverallOK {
			os.Exit(1)
		}
	},
}

func init() {
	validateCmd.Flags().BoolVar(&validateFixAll, "fix-all", false, "Auto-fix all fixable issues")
	validateCmd.Flags().BoolVarP(&validateYes, "yes", "y", false, "Skip confirmation prompt")
	rootCmd.AddCommand(validateCmd)
}

func runValidation(path string) validateResult {
	result := validateResult{
		Path:      path,
		OverallOK: true,
	}

	// Check 1: Duplicate issues
	dupCheck := convertDoctorCheck(doctor.CheckDuplicateIssues(path, false, 0))
	result.Checks = append(result.Checks, dupCheck)
	if dupCheck.Status == statusWarning || dupCheck.Status == statusError {
		result.OverallOK = false
	}

	// Check 2: Orphaned dependencies
	orphanCheck := convertDoctorCheck(doctor.CheckOrphanedDependencies(path))
	result.Checks = append(result.Checks, orphanCheck)
	if orphanCheck.Status == statusWarning || orphanCheck.Status == statusError {
		result.OverallOK = false
	}

	// Check 3: Test pollution
	pollutionCheck := convertDoctorCheck(doctor.CheckTestPollution(path))
	result.Checks = append(result.Checks, pollutionCheck)
	if pollutionCheck.Status == statusWarning || pollutionCheck.Status == statusError {
		result.OverallOK = false
	}

	// Check 4: Git conflicts
	conflictCheck := convertDoctorCheck(doctor.CheckGitConflicts(path))
	result.Checks = append(result.Checks, conflictCheck)
	if conflictCheck.Status == statusError {
		result.OverallOK = false
	}

	return result
}

func printValidation(result validateResult) {
	fmt.Println()
	fmt.Println(ui.RenderCategory("Data Integrity"))

	var passCount, warnCount, failCount int

	for _, check := range result.Checks {
		var statusIcon string
		switch check.Status {
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

		fmt.Printf("  %s  %s", statusIcon, check.Name)
		if check.Message != "" {
			fmt.Printf("%s", ui.RenderMuted(" "+check.Message))
		}
		fmt.Println()

		if check.Detail != "" {
			fmt.Printf("     %s%s\n", ui.MutedStyle.Render(ui.TreeLast), ui.RenderMuted(check.Detail))
		}
	}

	fmt.Println()
	fmt.Println(ui.RenderSeparator())

	summary := fmt.Sprintf("%s %d passed  %s %d warnings  %s %d failed",
		ui.RenderPassIcon(), passCount,
		ui.RenderWarnIcon(), warnCount,
		ui.RenderFailIcon(), failCount,
	)
	fmt.Println(summary)

	if result.OverallOK {
		fmt.Println()
		fmt.Printf("%s\n", ui.RenderPass("✓ All checks passed"))
	}
}

func applyValidationFixes(path string, result validateResult) {
	// Collect fixable checks
	var fixable []doctorCheck
	for _, check := range result.Checks {
		if check.Status != statusOK && check.Name == "Orphaned Dependencies" {
			fixable = append(fixable, check)
		}
	}

	if len(fixable) == 0 {
		fmt.Println("\nNo auto-fixable issues found.")
		return
	}

	// Confirm unless --yes
	if !validateYes {
		fmt.Printf("\nWill fix %d issue(s):\n", len(fixable))
		for _, check := range fixable {
			fmt.Printf("  - %s: %s\n", check.Name, check.Message)
		}
		fmt.Print("\nContinue? (Y/n): ")

		var response string
		fmt.Scanln(&response)
		if response != "" && response != "y" && response != "Y" && response != "yes" {
			fmt.Println("Fix canceled.")
			return
		}
	}

	fmt.Println("\nApplying fixes...")

	for _, check := range fixable {
		fmt.Printf("\nFixing %s...\n", check.Name)
		var err error
		switch check.Name {
		case "Orphaned Dependencies":
			err = fix.OrphanedDependencies(path, false)
		}
		if err != nil {
			fmt.Printf("  %s Error: %v\n", ui.RenderFail("✗"), err)
		} else {
			fmt.Printf("  %s Fixed\n", ui.RenderPass("✓"))
		}
	}
}

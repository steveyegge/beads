package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check <folder-or-script>",
	Short: "Check prerequisites for a script or folder",
	Long: `Check if all prerequisites are met to run a script or folder of scripts.

Examples:
  bd-examples check bash-agent           # Check all scripts in bash-agent/
  bd-examples check bash-agent/agent.sh  # Check a specific script
  bd-examples check --json compaction    # Output as JSON`,
	Args: cobra.ExactArgs(1),
	RunE: runCheck,
}

// CheckResult represents the result of a prerequisite check
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass", "fail", "warn"
	Message string `json:"message,omitempty"`
}

func runCheck(cmd *cobra.Command, args []string) error {
	target := args[0]

	// Find matching scripts
	var scripts []Script

	// Try exact match first
	if s := GetScript(target); s != nil {
		scripts = []Script{*s}
	} else {
		// Try folder match
		scripts = GetScriptsByFolder(target)
	}

	if len(scripts) == 0 {
		return fmt.Errorf("no scripts found matching: %s", target)
	}

	// Collect all unique prerequisites
	prereqSet := make(map[string]bool)
	for _, s := range scripts {
		for _, p := range s.Prerequisites {
			prereqSet[p] = true
		}
	}

	// Check each prerequisite
	var results []CheckResult
	for prereq := range prereqSet {
		result := checkPrereq(prereq)
		results = append(results, result)
	}

	if jsonOutput {
		return checkJSON(target, scripts, results)
	}

	return checkTable(target, scripts, results)
}

func checkPrereq(prereq string) CheckResult {
	switch prereq {
	case "bd":
		return checkCommand("bd", "--version")
	case "jq":
		return checkCommand("jq", "--version")
	case "git":
		return checkCommand("git", "--version")
	case ".beads":
		return checkBeadsDir()
	case "ANTHROPIC_API_KEY":
		return checkEnvVar("ANTHROPIC_API_KEY")
	default:
		// Try as a command
		return checkCommand(prereq, "--version")
	}
}

func checkCommand(name string, versionFlag string) CheckResult {
	cmd := exec.Command(name, versionFlag)
	output, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    name + " installed",
			Status:  "fail",
			Message: "command not found",
		}
	}

	version := strings.TrimSpace(string(output))
	// Extract just the version number if output is verbose
	if lines := strings.Split(version, "\n"); len(lines) > 0 {
		version = strings.TrimSpace(lines[0])
	}
	// Truncate long version strings
	if len(version) > 40 {
		version = version[:40] + "..."
	}

	return CheckResult{
		Name:    name + " installed",
		Status:  "pass",
		Message: version,
	}
}

func checkBeadsDir() CheckResult {
	if _, err := os.Stat(".beads"); err == nil {
		return CheckResult{
			Name:    ".beads exists",
			Status:  "pass",
			Message: "beads project found",
		}
	}
	return CheckResult{
		Name:    ".beads exists",
		Status:  "fail",
		Message: "not a beads project (run bd init)",
	}
}

func checkEnvVar(name string) CheckResult {
	val := os.Getenv(name)
	if val == "" {
		return CheckResult{
			Name:    name,
			Status:  "fail",
			Message: "not set",
		}
	}
	// Don't expose the actual value
	return CheckResult{
		Name:    name,
		Status:  "pass",
		Message: "set (" + fmt.Sprintf("%d chars", len(val)) + ")",
	}
}

func checkJSON(target string, scripts []Script, results []CheckResult) error {
	type jsonOutput struct {
		Target  string        `json:"target"`
		Scripts []string      `json:"scripts"`
		Results []CheckResult `json:"results"`
		Ready   bool          `json:"ready"`
	}

	var scriptPaths []string
	for _, s := range scripts {
		scriptPaths = append(scriptPaths, s.Path)
	}

	ready := true
	for _, r := range results {
		if r.Status == "fail" {
			ready = false
			break
		}
	}

	out := jsonOutput{
		Target:  target,
		Scripts: scriptPaths,
		Results: results,
		Ready:   ready,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func checkTable(target string, scripts []Script, results []CheckResult) error {
	fmt.Printf("Checking prerequisites for %s...\n\n", boldStyle.Render(target))

	// Show which scripts will be covered
	if len(scripts) > 1 {
		fmt.Println(mutedStyle.Render("Scripts:"))
		for _, s := range scripts {
			fmt.Printf("  %s\n", s.Path)
		}
		fmt.Println()
	}

	// Show results
	hasFailures := false
	hasWarnings := false

	for _, r := range results {
		var statusStr string
		switch r.Status {
		case "pass":
			statusStr = passStyle.Render("PASS")
		case "warn":
			statusStr = warnStyle.Render("WARN")
			hasWarnings = true
		case "fail":
			statusStr = failStyle.Render("FAIL")
			hasFailures = true
		}

		fmt.Printf("  %-25s %s", r.Name, statusStr)
		if r.Message != "" {
			fmt.Printf("  %s", mutedStyle.Render(r.Message))
		}
		fmt.Println()
	}

	fmt.Println()

	// Summary
	if hasFailures {
		fmt.Printf("Overall: %s\n", failStyle.Render("NOT READY"))
		return fmt.Errorf("prerequisites not met")
	} else if hasWarnings {
		fmt.Printf("Overall: %s\n", warnStyle.Render("READY (with warnings)"))
	} else {
		fmt.Printf("Overall: %s\n", passStyle.Render("READY"))
	}

	return nil
}

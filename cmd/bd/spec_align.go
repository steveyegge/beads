package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/ui"
)

type codeCheck struct {
	ID          string
	Description string
	File        string
	Contains    string
}

type codeCheckResult struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	File        string `json:"file"`
	Contains    string `json:"contains,omitempty"`
	Passed      bool   `json:"passed"`
	Error       string `json:"error,omitempty"`
}

type specAlignEntry struct {
	SpecID     string            `json:"spec_id"`
	Title      string            `json:"title"`
	Lifecycle  string            `json:"lifecycle"`
	BeadCount  int               `json:"bead_count"`
	CodeChecks []codeCheckResult `json:"code_checks,omitempty"`
	Status     string            `json:"status"`
}

type specAlignReport struct {
	GeneratedAt string           `json:"generated_at"`
	Entries     []specAlignEntry `json:"entries"`
}

var specAlignCmd = &cobra.Command{
	Use:   "align",
	Short: "Report spec ↔ bead ↔ code alignment",
	Run: func(cmd *cobra.Command, _ []string) {
		if daemonClient != nil {
			FatalErrorRespectJSON("spec align requires direct access (run with --no-daemon)")
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		entries, err := specStore.ListSpecRegistryWithCounts(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}

		repoRoot := findSpecRepoRoot()
		if repoRoot == "" {
			FatalErrorRespectJSON("could not locate repo root")
		}

		report := buildSpecAlignmentReport(entries, repoRoot)

		if jsonOutput {
			outputJSON(report)
			return
		}

		renderSpecAlignmentReport(report)
	},
}

func init() {
	specCmd.AddCommand(specAlignCmd)
}

func buildSpecAlignmentReport(entries []spec.SpecRegistryCount, repoRoot string) specAlignReport {
	items := make([]specAlignEntry, 0, len(entries))
	for _, entry := range entries {
		specID := entry.Spec.SpecID
		checks := specCodeChecks(specID)
		results := runCodeChecks(repoRoot, checks)
		status := computeAlignmentStatus(entry.BeadCount, results, len(checks) > 0)
		items = append(items, specAlignEntry{
			SpecID:     specID,
			Title:      entry.Spec.Title,
			Lifecycle:  entry.Spec.Lifecycle,
			BeadCount:  entry.BeadCount,
			CodeChecks: results,
			Status:     status,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].SpecID < items[j].SpecID
	})
	return specAlignReport{
		GeneratedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		Entries:     items,
	}
}

func computeAlignmentStatus(beadCount int, checks []codeCheckResult, hasChecks bool) string {
	beadsOK := beadCount > 0
	codeOK := true
	for _, check := range checks {
		if !check.Passed {
			codeOK = false
			break
		}
	}

	switch {
	case !hasChecks && beadsOK:
		return "beads-only"
	case !hasChecks && !beadsOK:
		return "missing-beads"
	case beadsOK && codeOK:
		return "aligned"
	case beadsOK && !codeOK:
		return "code-mismatch"
	case !beadsOK && codeOK:
		return "missing-beads"
	default:
		return "missing-beads-and-code"
	}
}

func specCodeChecks(specID string) []codeCheck {
	switch specID {
	case "specs/active/PACMAN_MODE_SPEC.md":
		return []codeCheck{
			{
				ID:          "assign-command",
				Description: "assign command exists",
				File:        "cmd/bd/assign.go",
				Contains:    "assignCmd",
			},
			{
				ID:          "ready-mine",
				Description: "ready --mine filter",
				File:        "cmd/bd/ready.go",
				Contains:    "\"mine\"",
			},
			{
				ID:          "close-auto-score",
				Description: "close increments pacman score",
				File:        "cmd/bd/close.go",
				Contains:    "maybeIncrementPacmanScore",
			},
			{
				ID:          "pause-check",
				Description: "root pause signal check",
				File:        "cmd/bd/main.go",
				Contains:    "checkPauseSignal",
			},
		}
	default:
		return nil
	}
}

func runCodeChecks(repoRoot string, checks []codeCheck) []codeCheckResult {
	if len(checks) == 0 {
		return nil
	}
	results := make([]codeCheckResult, 0, len(checks))
	for _, check := range checks {
		result := codeCheckResult{
			ID:          check.ID,
			Description: check.Description,
			File:        check.File,
			Contains:    check.Contains,
			Passed:      false,
		}
		path := check.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoRoot, path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		if check.Contains == "" {
			result.Passed = true
			results = append(results, result)
			continue
		}
		if strings.Contains(string(content), check.Contains) {
			result.Passed = true
		} else {
			result.Error = "missing expected content"
		}
		results = append(results, result)
	}
	return results
}

func renderSpecAlignmentReport(report specAlignReport) {
	fmt.Printf("Spec alignment report (%s)\n", report.GeneratedAt)
	if len(report.Entries) == 0 {
		fmt.Println("No specs found.")
		return
	}
	fmt.Println()
	for _, entry := range report.Entries {
		statusIcon := "○"
		switch entry.Status {
		case "aligned":
			statusIcon = ui.RenderPass("✓")
		case "code-mismatch":
			statusIcon = ui.RenderWarn("◐")
		case "missing-beads", "missing-beads-and-code":
			statusIcon = ui.RenderWarn("○")
		}
		line := fmt.Sprintf("%s %s (beads: %d)", statusIcon, entry.SpecID, entry.BeadCount)
		if entry.Title != "" {
			line = fmt.Sprintf("%s — %s", line, entry.Title)
		}
		fmt.Fprintln(os.Stdout, line)
		if len(entry.CodeChecks) == 0 {
			continue
		}
		for _, check := range entry.CodeChecks {
			checkIcon := "○"
			if check.Passed {
				checkIcon = ui.RenderPass("✓")
			} else {
				checkIcon = ui.RenderWarn("◐")
			}
			note := check.Description
			if note == "" {
				note = check.ID
			}
			if check.Error != "" {
				note = fmt.Sprintf("%s (%s)", note, check.Error)
			}
			fmt.Fprintf(os.Stdout, "  %s %s\n", checkIcon, note)
		}
	}
}

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

var specConsolidateCmd = &cobra.Command{
	Use:   "consolidate",
	Short: "Generate a report of older specs for consolidation",
	Run: func(cmd *cobra.Command, _ []string) {
		reportPath, _ := cmd.Flags().GetString("report")
		olderThanDays, _ := cmd.Flags().GetInt("older-than")
		includeMissing, _ := cmd.Flags().GetBool("include-missing")
		if olderThanDays <= 0 {
			FatalErrorRespectJSON("--older-than must be > 0 days")
		}

		if daemonClient != nil {
			FatalErrorRespectJSON("spec consolidate requires direct access (run with --no-daemon)")
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		store, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		entries, err := store.ListSpecRegistry(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}

		cutoff := time.Now().UTC().Add(-time.Duration(olderThanDays) * 24 * time.Hour)
		candidates := make([]spec.SpecRegistryEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.MissingAt != nil && !includeMissing {
				continue
			}
			if entry.Mtime.IsZero() {
				continue
			}
			if entry.Mtime.Before(cutoff) {
				candidates = append(candidates, entry)
			}
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"cutoff":     cutoff.Format(time.RFC3339),
				"count":      len(candidates),
				"candidates": candidates,
			})
			return
		}

		if reportPath == "" {
			reportPath = "docs/SHADOWBOOK_CONSOLIDATION_REPORT.md"
		}
		if err := writeConsolidationReport(reportPath, candidates, cutoff); err != nil {
			FatalErrorRespectJSON("write report: %v", err)
		}

		fmt.Printf("%s Wrote consolidation report: %s\n", ui.RenderPass("✓"), reportPath)
		fmt.Printf("Candidates: %d (older than %d days)\n", len(candidates), olderThanDays)
	},
}

func init() {
	specConsolidateCmd.Flags().String("report", "docs/SHADOWBOOK_CONSOLIDATION_REPORT.md", "Output report path")
	specConsolidateCmd.Flags().Int("older-than", 180, "Only include specs older than N days")
	specConsolidateCmd.Flags().Bool("include-missing", false, "Include missing specs in report")

	specCmd.AddCommand(specConsolidateCmd)
}

func writeConsolidationReport(path string, specs []spec.SpecRegistryEntry, cutoff time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	fmt.Fprintf(f, "# Shadowbook Consolidation Report\n\n")
	fmt.Fprintf(f, "- Generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(f, "- Cutoff: %s\n", cutoff.Format(time.RFC3339))
	fmt.Fprintf(f, "- Candidates: %d\n\n", len(specs))

	if len(specs) == 0 {
		fmt.Fprintln(f, "No specs matched the cutoff.")
		return nil
	}

	grouped := map[string][]spec.SpecRegistryEntry{}
	for _, entry := range specs {
		dir := filepath.Dir(entry.SpecID)
		if dir == "." {
			dir = "specs"
		}
		grouped[dir] = append(grouped[dir], entry)
	}

	dirs := make([]string, 0, len(grouped))
	for dir := range grouped {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		items := grouped[dir]
		sort.Slice(items, func(i, j int) bool {
			return items[i].SpecID < items[j].SpecID
		})

		fmt.Fprintf(f, "## %s\n\n", dir)
		fmt.Fprintf(f, "Suggested consolidation target: `%s/CONSOLIDATED.md`\n\n", dir)
		for _, entry := range items {
			title := strings.TrimSpace(entry.Title)
			if title == "" {
				title = "(no title)"
			}
			fmt.Fprintf(f, "- `%s` — %s (mtime: %s)\n", entry.SpecID, title, entry.Mtime.Format("2006-01-02"))
		}
		fmt.Fprintln(f)
	}

	return nil
}

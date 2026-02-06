package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/ui"
)

type specCleanupResult struct {
	Cutoff    string              `json:"cutoff"`
	ToDelete  int                 `json:"to_delete"`
	Protected int                 `json:"protected"`
	Applied   bool                `json:"applied"`
	Deleted   int                 `json:"deleted,omitempty"`
	Purged    int                 `json:"purged,omitempty"`
	Errors    int                 `json:"errors,omitempty"`
	Entries   []specCleanupEntry  `json:"entries,omitempty"`
}

type specCleanupEntry struct {
	SpecID    string `json:"spec_id"`
	Path      string `json:"path"`
	AgeDays   int    `json:"age_days"`
	BeadCount int    `json:"bead_count"`
}

var specCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Delete old specs by age with protection rules",
	Long: `Delete spec files older than a given duration. Protects specs linked to beads,
README files, and templates. Dry-run by default; use --apply to actually delete.

Protection rules (skipped unless overridden):
  - Specs linked to beads (use --include-linked to override)
  - README.md files
  - Files containing "template" in the name
  - Files containing "rules_detailed" in the name
  - Custom glob patterns via --protect

Examples:
  sbd spec cleanup --older-than 90d                  # Preview specs older than 90 days
  sbd spec cleanup --older-than 30d --apply           # Delete specs older than 30 days
  sbd spec cleanup --older-than 7d --protect "*.yaml" # Protect YAML files too`,
	Run: func(cmd *cobra.Command, _ []string) {
		olderThan, _ := cmd.Flags().GetString("older-than")
		apply, _ := cmd.Flags().GetBool("apply")
		protectGlobs, _ := cmd.Flags().GetStringSlice("protect")
		includeLinked, _ := cmd.Flags().GetBool("include-linked")

		if olderThan == "" {
			FatalErrorRespectJSON("--older-than is required")
		}

		duration, err := parseDurationWithDays(olderThan)
		if err != nil {
			FatalErrorRespectJSON("invalid duration: %v", err)
		}
		if duration <= 0 {
			FatalErrorRespectJSON("duration must be > 0")
		}

		cutoff := time.Now().Add(-duration)

		if daemonClient != nil {
			FatalErrorRespectJSON("spec cleanup requires direct access (run with --no-daemon)")
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
			FatalErrorRespectJSON("list specs: %v", err)
		}

		var toDelete []spec.SpecRegistryCount
		var protected int

		for _, entry := range entries {
			// Skip specs newer than cutoff
			if entry.Spec.Mtime.After(cutoff) {
				continue
			}
			// Skip missing specs (already gone)
			if entry.Spec.MissingAt != nil {
				continue
			}
			// Protection: bead-linked
			if !includeLinked && entry.BeadCount > 0 {
				protected++
				continue
			}
			// Protection: README, templates, rules_detailed
			base := filepath.Base(entry.Spec.Path)
			lower := strings.ToLower(base)
			pathLower := strings.ToLower(entry.Spec.Path)
			if lower == "readme.md" || strings.Contains(lower, "template") || strings.Contains(pathLower, "/templates/") || strings.Contains(lower, "rules_detailed") {
				protected++
				continue
			}
			// Protection: custom globs
			matched := false
			for _, glob := range protectGlobs {
				if m, _ := filepath.Match(glob, base); m {
					matched = true
					break
				}
			}
			if matched {
				protected++
				continue
			}

			toDelete = append(toDelete, entry)
		}

		// JSON output
		if jsonOutput {
			result := specCleanupResult{
				Cutoff:    cutoff.Format(time.RFC3339),
				ToDelete:  len(toDelete),
				Protected: protected,
				Applied:   apply,
			}
			for _, entry := range toDelete {
				ageDays := int(time.Since(entry.Spec.Mtime).Hours() / 24)
				result.Entries = append(result.Entries, specCleanupEntry{
					SpecID:    entry.Spec.SpecID,
					Path:      entry.Spec.Path,
					AgeDays:   ageDays,
					BeadCount: entry.BeadCount,
				})
			}
			outputJSON(result)
			return
		}

		if len(toDelete) == 0 {
			fmt.Printf("0 specs to clean (%d protected)\n", protected)
			return
		}

		// Render table
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "ACTION\tSPEC\tAGE\tBEADS\n")
		for _, entry := range toDelete {
			ageDays := int(time.Since(entry.Spec.Mtime).Hours() / 24)
			action := "WOULD DELETE"
			if apply {
				action = "DELETE"
			}
			fmt.Fprintf(w, "%s\t%s\t%dd\t%d\n", action, entry.Spec.Path, ageDays, entry.BeadCount)
		}
		_ = w.Flush()

		if !apply {
			fmt.Printf("\n%d specs to clean (%d protected). Run with --apply to delete.\n", len(toDelete), protected)
			return
		}

		// Actually delete
		var specIDs []string
		var deleteErrors int
		for _, entry := range toDelete {
			if err := os.Remove(entry.Spec.Path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warn: %s: %v\n", entry.Spec.Path, err)
				deleteErrors++
				continue // Don't remove registry row if file delete failed
			}
			specIDs = append(specIDs, entry.Spec.SpecID)
		}

		if len(specIDs) > 0 {
			purged, err := specStore.DeleteSpecRegistryByIDs(rootCtx, specIDs)
			if err != nil {
				FatalErrorRespectJSON("purge registry: %v", err)
			}
			fmt.Printf("\n%s Deleted %d specs, purged %d registry entries (%d protected, %d errors)\n",
				ui.RenderPass("✓"), len(specIDs), purged, protected, deleteErrors)
		}
	},
}

func parseDurationWithDays(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day count: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func init() {
	specCleanupCmd.Flags().String("older-than", "", "Delete specs older than this duration (e.g., 7d, 30d, 48h)")
	specCleanupCmd.Flags().Bool("apply", false, "Actually delete files (default: dry-run)")
	specCleanupCmd.Flags().StringSlice("protect", nil, "Additional glob patterns to protect from deletion")
	specCleanupCmd.Flags().Bool("include-linked", false, "Include specs linked to beads (normally protected)")
	specCmd.AddCommand(specCleanupCmd)
}

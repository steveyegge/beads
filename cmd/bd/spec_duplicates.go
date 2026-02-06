package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/ui"
)

type specDuplicatesResult struct {
	Threshold   float64              `json:"threshold"`
	Count       int                  `json:"count"`
	Pairs       []spec.DuplicatePair `json:"pairs"`
	Fix         bool                 `json:"fix,omitempty"`
	Applied     bool                 `json:"applied,omitempty"`
	Resolutions []duplicateResolution `json:"resolutions,omitempty"`
	Deleted     int                  `json:"deleted,omitempty"`
	Skipped     int                  `json:"skipped,omitempty"`
	Errors      int                  `json:"errors,omitempty"`
}

type duplicateResolution struct {
	Keep       string  `json:"keep"`
	Delete     string  `json:"delete"`
	Similarity float64 `json:"similarity"`
	Action     string  `json:"action"` // "delete" or "skip"
}

var specDuplicatesCmd = &cobra.Command{
	Use:   "duplicates",
	Short: "Find duplicate or overlapping specs",
	Long: `Find duplicate or overlapping specs using Jaccard similarity.

Use --fix to resolve duplicates by canonical directory priority:
  archive > active > reference > root > ideas

Use --fix --apply to actually delete non-canonical duplicates.

Examples:
  sbd spec duplicates                          # Report only
  sbd spec duplicates --fix                    # Dry-run: show KEEP/DELETE/SKIP
  sbd spec duplicates --fix --apply            # Delete non-canonical duplicates`,
	Run: func(cmd *cobra.Command, _ []string) {
		threshold, _ := cmd.Flags().GetFloat64("threshold")
		fix, _ := cmd.Flags().GetBool("fix")
		apply, _ := cmd.Flags().GetBool("apply")

		if apply && !fix {
			FatalErrorRespectJSON("--apply requires --fix")
		}

		if daemonClient != nil {
			FatalErrorRespectJSON("spec duplicates requires direct access (run with --no-daemon)")
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

		pairs := spec.FindDuplicates(entries, threshold)

		// Without --fix: existing report-only behavior
		if !fix {
			result := specDuplicatesResult{
				Threshold: threshold,
				Count:     len(pairs),
				Pairs:     pairs,
			}

			if jsonOutput {
				outputJSON(result)
				return
			}

			if len(pairs) == 0 {
				fmt.Println("No duplicate hints found.")
				return
			}

			fmt.Printf("Duplicate Hints (similarity >= %.2f)\n", threshold)
			fmt.Println("------------------------------------")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SCORE\tSPEC A\tSPEC B\tKEY")
			for _, pair := range pairs {
				fmt.Fprintf(w, "%.2f\t%s\t%s\t%s\n", pair.Similarity, pair.SpecA, pair.SpecB, pair.Key)
			}
			_ = w.Flush()
			return
		}

		// --fix mode: resolve canonical and optionally delete
		alreadyDeleted := make(map[string]bool)
		var resolutions []duplicateResolution
		var skipped int

		for _, pair := range pairs {
			// Skip pairs where one side was already resolved (deleted) by a prior pair
			if alreadyDeleted[pair.SpecA] || alreadyDeleted[pair.SpecB] {
				continue
			}

			keep, del, skip := spec.ResolveCanonical(pair)
			if skip {
				skipped++
				resolutions = append(resolutions, duplicateResolution{
					Keep:       pair.SpecA,
					Delete:     pair.SpecB,
					Similarity: pair.Similarity,
					Action:     "skip",
				})
				continue
			}

			alreadyDeleted[del] = true
			resolutions = append(resolutions, duplicateResolution{
				Keep:       keep,
				Delete:     del,
				Similarity: pair.Similarity,
				Action:     "delete",
			})
		}

		// Count actionable deletions
		deleteCount := 0
		for _, r := range resolutions {
			if r.Action == "delete" {
				deleteCount++
			}
		}

		result := specDuplicatesResult{
			Threshold:   threshold,
			Count:       len(pairs),
			Pairs:       pairs,
			Fix:         true,
			Applied:     apply,
			Resolutions: resolutions,
			Skipped:     skipped,
		}

		if !apply {
			// Dry-run: show table and exit
			if jsonOutput {
				outputJSON(result)
				return
			}

			if len(resolutions) == 0 {
				fmt.Println("No duplicate hints found.")
				return
			}

			fmt.Printf("Duplicate Fix Plan (similarity >= %.2f)\n", threshold)
			fmt.Println("----------------------------------------")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ACTION\tKEEP\tDELETE\tSCORE")
			for _, r := range resolutions {
				if r.Action == "skip" {
					fmt.Fprintf(w, "SKIP\t%s\t%s\t%.2f\n", r.Keep, r.Delete, r.Similarity)
				} else {
					fmt.Fprintf(w, "WOULD DELETE\t%s\t%s\t%.2f\n", r.Keep, r.Delete, r.Similarity)
				}
			}
			_ = w.Flush()
			fmt.Printf("\n%d to delete, %d skipped. Run with --apply to execute.\n", deleteCount, skipped)
			return
		}

		// --apply mode: actually delete files and registry entries
		var deleteSpecIDs []string
		var deleteErrors int

		// Resolve workspace root for absolute file paths
		wsRoot, err := os.Getwd()
		if err != nil {
			FatalErrorRespectJSON("getwd: %v", err)
		}

		for _, r := range resolutions {
			if r.Action != "delete" {
				continue
			}

			absPath := r.Delete
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(wsRoot, r.Delete)
			}

			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warn: %s: %v\n", r.Delete, err)
				deleteErrors++
				continue
			}
			deleteSpecIDs = append(deleteSpecIDs, r.Delete)
		}

		result.Errors = deleteErrors

		if len(deleteSpecIDs) > 0 {
			purged, err := store.DeleteSpecRegistryByIDs(rootCtx, deleteSpecIDs)
			if err != nil {
				FatalErrorRespectJSON("purge registry: %v", err)
			}
			result.Deleted = purged
		}

		if jsonOutput {
			outputJSON(result)
			return
		}

		if !jsonOutput {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ACTION\tKEEP\tDELETE\tSCORE")
			for _, r := range resolutions {
				if r.Action == "skip" {
					fmt.Fprintf(w, "SKIP\t%s\t%s\t%.2f\n", r.Keep, r.Delete, r.Similarity)
				} else {
					fmt.Fprintf(w, "DELETE\t%s\t%s\t%.2f\n", r.Keep, r.Delete, r.Similarity)
				}
			}
			_ = w.Flush()
			fmt.Printf("\n%s Fixed %d duplicates (%d skipped, %d errors)\n",
				ui.RenderPass("done"), len(deleteSpecIDs), skipped, deleteErrors)
		}
	},
}

func init() {
	specDuplicatesCmd.Flags().Float64("threshold", 0.85, "Similarity threshold (0.0-1.0)")
	specDuplicatesCmd.Flags().Bool("fix", false, "Resolve duplicates by canonical directory priority")
	specDuplicatesCmd.Flags().Bool("apply", false, "Actually delete non-canonical duplicates (requires --fix)")
	specCmd.AddCommand(specDuplicatesCmd)
}

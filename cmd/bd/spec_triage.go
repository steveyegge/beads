package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
)

type specTriageEntry struct {
	Spec    spec.SpecRegistryEntry `json:"spec"`
	AgeDays int                    `json:"age_days"`
}

type specTriageResult struct {
	GeneratedAt string            `json:"generated_at"`
	Entries     []specTriageEntry `json:"entries"`
	Summary     map[string]int    `json:"summary"`
}

var specTriageCmd = &cobra.Command{
	Use:   "triage",
	Short: "Triage specs/ideas with git status and age",
	Run: func(cmd *cobra.Command, _ []string) {
		sortMode, _ := cmd.Flags().GetString("sort")
		limit, _ := cmd.Flags().GetInt("limit")
		if limit <= 0 {
			FatalErrorRespectJSON("--limit must be > 0")
		}
		if sortMode != "age" && sortMode != "status" {
			FatalErrorRespectJSON("--sort must be age or status")
		}

		if daemonClient != nil {
			FatalErrorRespectJSON("spec triage requires direct access (run with --no-daemon)")
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

		now := time.Now().UTC().Truncate(time.Second)
		triage := make([]specTriageEntry, 0)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.SpecID, "specs/ideas/") {
				continue
			}
			ageDays, ok := specAgeDays(entry, now)
			if !ok {
				continue
			}
			triage = append(triage, specTriageEntry{
				Spec:    entry,
				AgeDays: ageDays,
			})
		}

		sort.Slice(triage, func(i, j int) bool {
			switch sortMode {
			case "status":
				rankI := triageStatusRank(triage[i].Spec.GitStatus)
				rankJ := triageStatusRank(triage[j].Spec.GitStatus)
				if rankI != rankJ {
					return rankI < rankJ
				}
			}
			if triage[i].AgeDays != triage[j].AgeDays {
				return triage[i].AgeDays > triage[j].AgeDays
			}
			return triage[i].Spec.SpecID < triage[j].Spec.SpecID
		})

		if len(triage) > limit {
			triage = triage[:limit]
		}

		summary := map[string]int{
			"untracked": 0,
			"modified":  0,
			"tracked":   0,
			"unknown":   0,
		}
		for _, entry := range triage {
			key := entry.Spec.GitStatus
			if key == "" {
				key = "unknown"
			}
			if _, ok := summary[key]; ok {
				summary[key]++
			} else {
				summary["unknown"]++
			}
		}

		result := specTriageResult{
			GeneratedAt: now.Format(time.RFC3339),
			Entries:     triage,
			Summary:     summary,
		}

		if jsonOutput {
			outputJSON(result)
			return
		}

		fmt.Println("Ideas Triage (specs/ideas/)")
		fmt.Println("───────────────────────────")
		if len(triage) == 0 {
			fmt.Println("No ideas found.")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tAGE\tSPEC ID\tTITLE")
		for _, entry := range triage {
			fmt.Fprintf(w, "%s\t%dd\t%s\t%s\n",
				strings.ToUpper(entry.Spec.GitStatus),
				entry.AgeDays,
				entry.Spec.SpecID,
				entry.Spec.Title,
			)
		}
		_ = w.Flush()

		fmt.Printf("\nSummary: %d ideas (%d untracked, %d modified, %d tracked, %d unknown)\n",
			len(triage),
			summary["untracked"],
			summary["modified"],
			summary["tracked"],
			summary["unknown"],
		)
	},
}

func init() {
	specTriageCmd.Flags().String("sort", "age", "Sort by age or status")
	specTriageCmd.Flags().Int("limit", 50, "Maximum specs to show")
	specCmd.AddCommand(specTriageCmd)
}

func triageStatusRank(status string) int {
	switch status {
	case "untracked":
		return 0
	case "modified":
		return 1
	case "tracked":
		return 2
	default:
		return 3
	}
}

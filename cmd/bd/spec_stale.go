package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/ui"
)

type specStaleEntry struct {
	Spec    spec.SpecRegistryEntry `json:"spec"`
	AgeDays int                    `json:"age_days"`
}

type specStaleBucket struct {
	Count   int              `json:"count"`
	Entries []specStaleEntry `json:"entries"`
}

type specStaleResult struct {
	GeneratedAt string                       `json:"generated_at"`
	Buckets     map[string]specStaleBucket   `json:"buckets"`
}

var specStaleCmd = &cobra.Command{
	Use:   "stale",
	Short: "Show specs by staleness bucket",
	Run: func(cmd *cobra.Command, _ []string) {
		limit, _ := cmd.Flags().GetInt("limit")
		if limit <= 0 {
			FatalErrorRespectJSON("--limit must be > 0")
		}

		if daemonClient != nil {
			FatalErrorRespectJSON("spec stale requires direct access (run with --no-daemon)")
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
		buckets := map[string][]specStaleEntry{
			"fresh":   {},
			"aging":   {},
			"stale":   {},
			"ancient": {},
		}

		for _, entry := range entries {
			ageDays, ok := specAgeDays(entry, now)
			if !ok {
				continue
			}
			target := "ancient"
			switch {
			case ageDays <= 7:
				target = "fresh"
			case ageDays <= 30:
				target = "aging"
			case ageDays <= 90:
				target = "stale"
			}
			buckets[target] = append(buckets[target], specStaleEntry{
				Spec:    entry,
				AgeDays: ageDays,
			})
		}

		result := specStaleResult{
			GeneratedAt: now.Format(time.RFC3339),
			Buckets:     map[string]specStaleBucket{},
		}
		for _, key := range []string{"fresh", "aging", "stale", "ancient"} {
			entries := buckets[key]
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].AgeDays != entries[j].AgeDays {
					return entries[i].AgeDays > entries[j].AgeDays
				}
				return entries[i].Spec.SpecID < entries[j].Spec.SpecID
			})
			limited := entries
			if len(limited) > limit {
				limited = limited[:limit]
			}
			result.Buckets[key] = specStaleBucket{
				Count:   len(entries),
				Entries: limited,
			}
		}

		if jsonOutput {
			outputJSON(result)
			return
		}

		fmt.Println("Staleness Buckets")
		fmt.Println("─────────────────")
		fmt.Printf("Fresh (0-7d):    %d specs\n", result.Buckets["fresh"].Count)
		fmt.Printf("Aging (8-30d):   %d specs\n", result.Buckets["aging"].Count)
		fmt.Printf("Stale (31-90d):  %d specs\n", result.Buckets["stale"].Count)
		fmt.Printf("Ancient (90+d):  %d specs\n", result.Buckets["ancient"].Count)

		renderStaleBucket("Top Ancient Specs", result.Buckets["ancient"])
	},
}

func init() {
	specStaleCmd.Flags().Int("limit", 5, "Maximum specs to show per bucket")
	specCmd.AddCommand(specStaleCmd)
}

func specAgeDays(entry spec.SpecRegistryEntry, now time.Time) (int, bool) {
	ts := entry.Mtime
	if ts.IsZero() {
		ts = entry.LastScannedAt
	}
	if ts.IsZero() {
		return 0, false
	}
	if ts.After(now) {
		return 0, true
	}
	return int(now.Sub(ts).Hours() / 24), true
}

func renderStaleBucket(title string, bucket specStaleBucket) {
	if len(bucket.Entries) == 0 {
		return
	}

	fmt.Println()
	fmt.Println(title + ":")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC ID\tAGE\tGIT\tTITLE")
	for _, entry := range bucket.Entries {
		fmt.Fprintf(w, "%s\t%dd\t%s\t%s\n",
			entry.Spec.SpecID,
			entry.AgeDays,
			entry.Spec.GitStatus,
			entry.Spec.Title,
		)
	}
	_ = w.Flush()
	fmt.Printf("%s Run with --json for full bucket details.\n", ui.RenderInfoIcon())
}

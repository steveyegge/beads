package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
)

type specDuplicatesResult struct {
	Threshold float64             `json:"threshold"`
	Count     int                 `json:"count"`
	Pairs     []spec.DuplicatePair `json:"pairs"`
}

var specDuplicatesCmd = &cobra.Command{
	Use:   "duplicates",
	Short: "Find duplicate or overlapping specs",
	Run: func(cmd *cobra.Command, _ []string) {
		threshold, _ := cmd.Flags().GetFloat64("threshold")

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
		fmt.Println("────────────────────────────────────")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SCORE\tSPEC A\tSPEC B\tKEY")
		for _, pair := range pairs {
			fmt.Fprintf(w, "%.2f\t%s\t%s\t%s\n", pair.Similarity, pair.SpecA, pair.SpecB, pair.Key)
		}
		_ = w.Flush()
	},
}

func init() {
	specDuplicatesCmd.Flags().Float64("threshold", 0.85, "Similarity threshold (0.0-1.0)")
	specCmd.AddCommand(specDuplicatesCmd)
}

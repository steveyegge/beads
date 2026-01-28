package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
)

var specRiskCmd = &cobra.Command{
	Use:   "risk",
	Short: "Show spec volatility and risk signals",
	Run: func(cmd *cobra.Command, _ []string) {
		sinceInput, _ := cmd.Flags().GetString("since")
		minChanges, _ := cmd.Flags().GetInt("min-changes")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")

		var since time.Time
		if strings.TrimSpace(sinceInput) != "" {
			duration, err := parseDurationString(sinceInput)
			if err != nil {
				FatalErrorRespectJSON("invalid since duration: %v", err)
			}
			since = time.Now().UTC().Add(-duration).Truncate(time.Second)
		}

		if daemonClient != nil {
			sinceStr := ""
			if !since.IsZero() {
				sinceStr = since.Format(time.RFC3339)
			}
			resp, err := daemonClient.SpecRisk(&rpc.SpecRiskArgs{
				Since:      sinceStr,
				MinChanges: minChanges,
				Limit:      limit,
			})
			if err != nil {
				FatalErrorRespectJSON("spec risk failed: %v", err)
			}
			var entries []spec.SpecRiskEntry
			if err := json.Unmarshal(resp.Data, &entries); err != nil {
				FatalErrorRespectJSON("invalid spec risk response: %v", err)
			}
			renderSpecRisk(entries, format)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if store == nil {
			FatalErrorRespectJSON("storage not available")
		}

		entries, err := computeSpecRisk(rootCtx, specStore, since, minChanges, limit)
		if err != nil {
			FatalErrorRespectJSON("spec risk failed: %v", err)
		}
		renderSpecRisk(entries, format)
	},
}

func init() {
	specRiskCmd.Flags().String("since", "30d", "Only count changes since this duration (e.g., 10d, 24h)")
	specRiskCmd.Flags().Int("min-changes", 1, "Minimum change count to include")
	specRiskCmd.Flags().Int("limit", 20, "Maximum specs to show (0 = no limit)")
	specRiskCmd.Flags().String("format", "table", "Output format: table or list")

	specCmd.AddCommand(specRiskCmd)
}

func computeSpecRisk(ctx context.Context, specStore spec.SpecRegistryStore, since time.Time, minChanges, limit int) ([]spec.SpecRiskEntry, error) {
	entries, err := specStore.ListSpecRegistry(ctx)
	if err != nil {
		return nil, err
	}

	openIssues := make(map[string]int)
	openFilter := types.IssueFilter{
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	}
	issues, err := store.SearchIssues(ctx, "", openFilter)
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		if issue.SpecID == "" {
			continue
		}
		openIssues[issue.SpecID]++
	}

	results := make([]spec.SpecRiskEntry, 0)
	for _, entry := range entries {
		if entry.MissingAt != nil {
			continue
		}
		events, err := specStore.ListSpecScanEvents(ctx, entry.SpecID, since)
		if err != nil {
			return nil, err
		}
		changeCount, lastChangedAt := spec.SummarizeScanEvents(events, time.Time{})
		if changeCount < minChanges {
			continue
		}
		results = append(results, spec.SpecRiskEntry{
			SpecID:        entry.SpecID,
			Title:         entry.Title,
			ChangeCount:   changeCount,
			LastChangedAt: lastChangedAt,
			OpenIssues:    openIssues[entry.SpecID],
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].ChangeCount != results[j].ChangeCount {
			return results[i].ChangeCount > results[j].ChangeCount
		}
		if results[i].OpenIssues != results[j].OpenIssues {
			return results[i].OpenIssues > results[j].OpenIssues
		}
		return results[i].SpecID < results[j].SpecID
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func renderSpecRisk(entries []spec.SpecRiskEntry, format string) {
	if jsonOutput {
		outputJSON(entries)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No specs meet the risk criteria.")
		return
	}

	switch strings.ToLower(format) {
	case "list":
		for _, entry := range entries {
			lastChanged := "-"
			if entry.LastChangedAt != nil {
				lastChanged = entry.LastChangedAt.Local().Format("2006-01-02")
			}
			fmt.Printf("%s  changes=%d  open=%d  last=%s\n", entry.SpecID, entry.ChangeCount, entry.OpenIssues, lastChanged)
			if entry.Title != "" {
				fmt.Printf("  %s\n", entry.Title)
			}
		}
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SPEC ID\tCHANGES\tLAST CHANGED\tOPEN\tTITLE")
		for _, entry := range entries {
			lastChanged := "-"
			if entry.LastChangedAt != nil {
				lastChanged = entry.LastChangedAt.Local().Format("2006-01-02")
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%s\n",
				entry.SpecID, entry.ChangeCount, lastChanged, entry.OpenIssues, entry.Title)
		}
		_ = w.Flush()
	}
}

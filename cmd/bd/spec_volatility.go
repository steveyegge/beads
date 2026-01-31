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
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var specVolatilityCmd = &cobra.Command{
	Use:     "volatility [spec-id]",
	Aliases: []string{"risk"},
	Short:   "Show spec volatility signals",
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if cmd.CalledAs() == "risk" && !jsonOutput && !debug.IsQuiet() {
			fmt.Fprintf(os.Stderr, "%s `bd spec risk` is deprecated; use `bd spec volatility`.\n", ui.RenderWarn("◐"))
		}
		specIDArg := ""
		if len(args) > 0 {
			specIDArg = args[0]
		}
		sinceInput, _ := cmd.Flags().GetString("since")
		minChanges, _ := cmd.Flags().GetInt("min-changes")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")
		failOnHigh, _ := cmd.Flags().GetBool("fail-on-high")
		failOnMedium, _ := cmd.Flags().GetBool("fail-on-medium")
		withDependents, _ := cmd.Flags().GetBool("with-dependents")
		recommendations, _ := cmd.Flags().GetBool("recommendations")
		trendSpec, _ := cmd.Flags().GetString("trend")

		var since time.Time
		if strings.TrimSpace(sinceInput) != "" {
			duration, err := parseDurationString(sinceInput)
			if err != nil {
				FatalErrorRespectJSON("invalid since duration: %v", err)
			}
			since = time.Now().UTC().Add(-duration).Truncate(time.Second)
		}

		if trendSpec != "" && (withDependents || recommendations) {
			FatalErrorRespectJSON("--trend cannot be combined with --with-dependents or --recommendations")
		}
		if trendSpec != "" {
			specIDArg = trendSpec
		}
		if (withDependents || trendSpec != "") && specIDArg == "" {
			FatalErrorRespectJSON("spec id required for --with-dependents or --trend")
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
				FatalErrorRespectJSON("spec volatility failed: %v", err)
			}
			var entries []spec.SpecRiskEntry
			if err := json.Unmarshal(resp.Data, &entries); err != nil {
				FatalErrorRespectJSON("invalid spec volatility response: %v", err)
			}
			if trendSpec != "" {
				if err := renderSpecTrend(rootCtx, specIDArg); err != nil {
					FatalErrorRespectJSON("spec trend failed: %v", err)
				}
			} else if withDependents {
				if err := renderSpecDependents(rootCtx, specIDArg, since); err != nil {
					FatalErrorRespectJSON("spec dependents failed: %v", err)
				}
			} else if recommendations {
				if err := renderSpecRecommendations(rootCtx, entries, since); err != nil {
					FatalErrorRespectJSON("spec recommendations failed: %v", err)
				}
			} else {
				renderSpecRisk(entries, format)
			}
			maybeFailOnVolatility(entries, failOnHigh, failOnMedium)
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
			FatalErrorRespectJSON("spec volatility failed: %v", err)
		}
		if trendSpec != "" {
			if err := renderSpecTrend(rootCtx, specIDArg); err != nil {
				FatalErrorRespectJSON("spec trend failed: %v", err)
			}
		} else if withDependents {
			if err := renderSpecDependents(rootCtx, specIDArg, since); err != nil {
				FatalErrorRespectJSON("spec dependents failed: %v", err)
			}
		} else if recommendations {
			if err := renderSpecRecommendations(rootCtx, entries, since); err != nil {
				FatalErrorRespectJSON("spec recommendations failed: %v", err)
			}
		} else {
			renderSpecRisk(entries, format)
		}
		maybeFailOnVolatility(entries, failOnHigh, failOnMedium)
	},
}

func init() {
	specVolatilityCmd.Flags().String("since", "30d", "Only count changes since this duration (e.g., 10d, 24h)")
	specVolatilityCmd.Flags().Int("min-changes", 1, "Minimum change count to include")
	specVolatilityCmd.Flags().Int("limit", 20, "Maximum specs to show (0 = no limit)")
	specVolatilityCmd.Flags().String("format", "table", "Output format: table or list")
	specVolatilityCmd.Flags().Bool("fail-on-high", false, "Exit 1 if any spec is HIGH volatility")
	specVolatilityCmd.Flags().Bool("fail-on-medium", false, "Exit 1 if any spec is MEDIUM or HIGH volatility")
	specVolatilityCmd.Flags().Bool("with-dependents", false, "Show dependent issues for a specific spec")
	specVolatilityCmd.Flags().Bool("recommendations", false, "Show stabilization recommendations")
	specVolatilityCmd.Flags().String("trend", "", "Show volatility trend for a spec")

	specCmd.AddCommand(specVolatilityCmd)
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
		fmt.Println("No specs meet the volatility criteria.")
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

func maybeFailOnVolatility(entries []spec.SpecRiskEntry, failOnHigh, failOnMedium bool) {
	if !failOnHigh && !failOnMedium {
		return
	}
	if failOnHigh && failOnMedium {
		FatalErrorRespectJSON("--fail-on-high and --fail-on-medium cannot both be set")
	}

	threshold := specVolatilityHigh
	if failOnMedium {
		threshold = specVolatilityMedium
	}

	var offenders []spec.SpecRiskEntry
	for _, entry := range entries {
		level := classifySpecVolatility(entry.ChangeCount, entry.OpenIssues)
		if level == specVolatilityHigh || (threshold == specVolatilityMedium && level == specVolatilityMedium) {
			offenders = append(offenders, entry)
		}
	}

	if len(offenders) == 0 {
		return
	}

	if jsonOutput {
		os.Exit(1)
	}

	lines := make([]string, 0, len(offenders))
	for _, entry := range offenders {
		lines = append(lines, fmt.Sprintf("%s (%d changes, %d open issues)", entry.SpecID, entry.ChangeCount, entry.OpenIssues))
	}
	fmt.Fprintf(os.Stderr, "%s Volatile specs detected: %s\n", ui.RenderWarn("◐"), strings.Join(lines, ", "))
	os.Exit(1)
}

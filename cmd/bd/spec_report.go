package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
)

type specReportResult struct {
	Summary     map[string]int        `json:"summary"`
	Triage      []specTriageEntry      `json:"triage"`
	Staleness   specStaleResult        `json:"staleness"`
	Duplicates  []spec.DuplicatePair   `json:"duplicates"`
	Delta       specDeltaResult        `json:"delta"`
	Volatility  []spec.SpecRiskEntry   `json:"volatility"`
	GeneratedAt string                `json:"generated_at"`
}

var specReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate full spec radar report",
	Run: func(cmd *cobra.Command, _ []string) {
		format, _ := cmd.Flags().GetString("format")
		outDir, _ := cmd.Flags().GetString("out")
		threshold, _ := cmd.Flags().GetFloat64("threshold")

		if format != "md" && format != "json" {
			FatalErrorRespectJSON("--format must be md or json")
		}

		if daemonClient != nil {
			FatalErrorRespectJSON("spec report requires direct access (run with --no-daemon)")
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
		summary := buildSpecSummary(entries)
		triage := buildSpecTriage(entries, now)
		staleness := buildSpecStaleness(entries, now, 5)
		duplicates := spec.FindDuplicates(entries, threshold)
		delta, err := buildSpecDelta(entries)
		if err != nil {
			FatalErrorRespectJSON("delta: %v", err)
		}
		volatility, _ := computeSpecRisk(rootCtx, store, now.Add(-30*24*time.Hour), 1, 5)

		result := specReportResult{
			Summary:     summary,
			Triage:      triage,
			Staleness:   staleness,
			Duplicates:  duplicates,
			Delta:       delta,
			Volatility:  volatility,
			GeneratedAt: now.Format(time.RFC3339),
		}

		if format == "json" {
			if outDir == "" {
				outputJSON(result)
				return
			}
			if err := os.MkdirAll(outDir, 0755); err != nil {
				FatalErrorRespectJSON("mkdir out: %v", err)
			}
			path := filepath.Join(outDir, "spec_radar_report.json")
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				FatalErrorRespectJSON("marshal report: %v", err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				FatalErrorRespectJSON("write report: %v", err)
			}
			fmt.Printf("Wrote report: %s\n", path)
			return
		}

		if outDir == "" {
			outDir = ".beads/reports"
		}
		if err := os.MkdirAll(outDir, 0755); err != nil {
			FatalErrorRespectJSON("mkdir out: %v", err)
		}
		path := filepath.Join(outDir, "spec_radar_report.md")
		if err := os.WriteFile(path, []byte(renderSpecReportMarkdown(result)), 0644); err != nil {
			FatalErrorRespectJSON("write report: %v", err)
		}
		fmt.Printf("Wrote report: %s\n", path)
	},
}

func init() {
	specReportCmd.Flags().String("out", ".beads/reports", "Output directory")
	specReportCmd.Flags().String("format", "md", "Output format: md or json")
	specReportCmd.Flags().Float64("threshold", 0.85, "Duplicate similarity threshold")
	specCmd.AddCommand(specReportCmd)
}

func buildSpecSummary(entries []spec.SpecRegistryEntry) map[string]int {
	summary := map[string]int{
		"total":   len(entries),
		"missing": 0,
	}
	for _, entry := range entries {
		if entry.MissingAt != nil {
			summary["missing"]++
		}
		if entry.Lifecycle != "" {
			key := "lifecycle_" + entry.Lifecycle
			summary[key]++
		}
	}
	return summary
}

func buildSpecTriage(entries []spec.SpecRegistryEntry, now time.Time) []specTriageEntry {
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
		if triage[i].AgeDays != triage[j].AgeDays {
			return triage[i].AgeDays > triage[j].AgeDays
		}
		return triage[i].Spec.SpecID < triage[j].Spec.SpecID
	})
	return triage
}

func buildSpecStaleness(entries []spec.SpecRegistryEntry, now time.Time, limit int) specStaleResult {
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
	return result
}

func buildSpecDelta(entries []spec.SpecRegistryEntry) (specDeltaResult, error) {
	cachePath, err := specDeltaCachePath()
	if err != nil {
		return specDeltaResult{}, err
	}
	prevCache, _ := loadSpecDeltaCache(cachePath)

	current := make([]spec.SpecSnapshot, 0, len(entries))
	for _, entry := range entries {
		current = append(current, spec.SpecSnapshot{
			SpecID:    entry.SpecID,
			Title:     entry.Title,
			Lifecycle: entry.Lifecycle,
			SHA256:    entry.SHA256,
			Mtime:     entry.Mtime,
		})
	}

	delta := spec.DeltaResult{}
	since := ""
	if prevCache != nil {
		delta = spec.ComputeDelta(prevCache.Specs, current)
		since = prevCache.GeneratedAt
	} else {
		delta = spec.ComputeDelta(nil, current)
	}

	if err := writeSpecDeltaCache(cachePath, current); err != nil {
		return specDeltaResult{}, err
	}

	return specDeltaResult{
		Since: since,
		Delta: delta,
	}, nil
}

func renderSpecReportMarkdown(result specReportResult) string {
	var b strings.Builder
	b.WriteString("# Spec Radar Report\n\n")
	b.WriteString("Generated: " + result.GeneratedAt + "\n\n")

	b.WriteString("## Summary\n\n")
	for key, value := range result.Summary {
		b.WriteString(fmt.Sprintf("- %s: %d\n", key, value))
	}
	b.WriteString("\n")

	b.WriteString("## Ideas Triage\n\n")
	if len(result.Triage) == 0 {
		b.WriteString("No ideas found.\n\n")
	} else {
		for _, entry := range result.Triage {
			b.WriteString(fmt.Sprintf("- %s (%dd, %s)\n", entry.Spec.SpecID, entry.AgeDays, entry.Spec.GitStatus))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Staleness Buckets\n\n")
	labels := map[string]string{
		"fresh":   "Fresh",
		"aging":   "Aging",
		"stale":   "Stale",
		"ancient": "Ancient",
	}
	for _, key := range []string{"fresh", "aging", "stale", "ancient"} {
		bucket := result.Staleness.Buckets[key]
		b.WriteString(fmt.Sprintf("- %s: %d\n", labels[key], bucket.Count))
	}
	b.WriteString("\n")

	b.WriteString("## Duplicate Hints\n\n")
	if len(result.Duplicates) == 0 {
		b.WriteString("No duplicate hints found.\n\n")
	} else {
		for _, pair := range result.Duplicates {
			b.WriteString(fmt.Sprintf("- %.2f %s ↔ %s\n", pair.Similarity, pair.SpecA, pair.SpecB))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Delta\n\n")
	if len(result.Delta.Delta.Added) == 0 && len(result.Delta.Delta.Removed) == 0 && len(result.Delta.Delta.Changed) == 0 {
		b.WriteString("No changes detected.\n\n")
	} else {
		if len(result.Delta.Delta.Added) > 0 {
			b.WriteString("### Added\n")
			for _, entry := range result.Delta.Delta.Added {
				b.WriteString(fmt.Sprintf("- %s\n", entry.SpecID))
			}
			b.WriteString("\n")
		}
		if len(result.Delta.Delta.Removed) > 0 {
			b.WriteString("### Removed\n")
			for _, entry := range result.Delta.Delta.Removed {
				b.WriteString(fmt.Sprintf("- %s\n", entry.SpecID))
			}
			b.WriteString("\n")
		}
		if len(result.Delta.Delta.Changed) > 0 {
			b.WriteString("### Changed\n")
			for _, change := range result.Delta.Delta.Changed {
				b.WriteString(fmt.Sprintf("- %s (%s)\n", change.SpecID, change.Field))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## Volatility Leaders\n\n")
	if len(result.Volatility) == 0 {
		b.WriteString("No volatility data.\n")
	} else {
		for _, entry := range result.Volatility {
			b.WriteString(fmt.Sprintf("- %s (%d changes)\n", entry.SpecID, entry.ChangeCount))
		}
	}
	return b.String()
}

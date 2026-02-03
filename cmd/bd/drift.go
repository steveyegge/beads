package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

type wobbleDriftSummary struct {
	LastScanAt        string `json:"last_scan_at,omitempty"`
	Stable            int    `json:"stable"`
	Wobbly            int    `json:"wobbly"`
	Unstable          int    `json:"unstable"`
	SkillsFixed       int    `json:"skills_fixed"`
	SpecsWithoutBeads int    `json:"specs_without_beads"`
	BeadsWithoutSpecs int    `json:"beads_without_specs"`
}

var driftCmd = &cobra.Command{
	Use:     "drift",
	Short:   "Show wobble and spec drift summary",
	GroupID: GroupMaintenance,
	Run:     runDrift,
}

func init() {
	rootCmd.AddCommand(driftCmd)
}

func runDrift(_ *cobra.Command, _ []string) {
	if daemonClient != nil {
		FatalErrorRespectJSON("drift requires direct access (run with --no-daemon)")
	}
	if err := ensureDatabaseFresh(rootCtx); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	skillsPath, historyPath, err := wobbleStorePaths()
	if err != nil {
		FatalErrorRespectJSON("wobble store: %v", err)
	}
	storeSnapshot, history, err := loadWobbleStore(skillsPath, historyPath)
	if err != nil {
		FatalErrorRespectJSON("wobble store: %v", err)
	}

	stableCount, wobblyCount, unstableCount := countWobbleVerdicts(storeSnapshot.Skills)
	actorName := getPacmanAgentName()
	skillsFixed := skillsFixedFromHistory(history, actorName)

	specsWithoutBeads, beadsWithoutSpecs := 0, 0
	if store != nil {
		specStore, err := getSpecRegistryStore()
		if err == nil {
			entries, err := specStore.ListSpecRegistryWithCounts(rootCtx)
			if err == nil {
				specIDs := make(map[string]struct{}, len(entries))
				for _, entry := range entries {
					specIDs[entry.Spec.SpecID] = struct{}{}
					if entry.BeadCount == 0 {
						specsWithoutBeads++
					}
				}

				issues, err := store.SearchIssues(rootCtx, "", types.IssueFilter{})
				if err == nil {
					for _, issue := range issues {
						if issue.SpecID == "" {
							beadsWithoutSpecs++
							continue
						}
						if _, ok := specIDs[issue.SpecID]; !ok {
							beadsWithoutSpecs++
						}
					}
				}
			}
		}
	}

	summary := wobbleDriftSummary{
		Stable:            stableCount,
		Wobbly:            wobblyCount,
		Unstable:          unstableCount,
		SkillsFixed:       skillsFixed,
		SpecsWithoutBeads: specsWithoutBeads,
		BeadsWithoutSpecs: beadsWithoutSpecs,
	}
	if !storeSnapshot.GeneratedAt.IsZero() {
		summary.LastScanAt = storeSnapshot.GeneratedAt.UTC().Format(time.RFC3339)
	}

	if jsonOutput {
		outputJSON(summary)
		return
	}

	renderWobbleDriftSummary(summary)
}

func countWobbleVerdicts(skills []wobbleSkill) (int, int, int) {
	stableCount := 0
	wobblyCount := 0
	unstableCount := 0
	for _, skill := range skills {
		switch normalizeWobbleVerdict(skill.Verdict) {
		case "stable":
			stableCount++
		case "wobbly":
			wobblyCount++
		case "unstable":
			unstableCount++
		}
	}
	return stableCount, wobblyCount, unstableCount
}

func skillsFixedFromHistory(history []wobbleHistoryEntry, actor string) int {
	entries := make([]wobbleHistoryEntry, 0, len(history))
	for _, entry := range history {
		if actor != "" && entry.Actor != actor {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) < 2 {
		return 0
	}
	
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})

	prev := entries[len(entries)-2]
	curr := entries[len(entries)-1]
	stableNow := make(map[string]struct{}, len(curr.Skills))
	for _, skill := range curr.Skills {
		stableNow[skill] = struct{}{}
	}

	fixed := 0
	for _, skill := range prev.UnstableSkills {
		if _, ok := stableNow[skill]; ok {
			fixed++
		}
	}
	for _, skill := range prev.WobblySkills {
		if _, ok := stableNow[skill]; ok {
			fixed++
		}
	}

	return fixed
}

func renderWobbleDriftSummary(summary wobbleDriftSummary) {
	fmt.Println("Wobble drift summary")
	if summary.LastScanAt != "" {
		fmt.Printf("Last scan: %s\n", summary.LastScanAt)
	}
	fmt.Printf("Stable: %d\n", summary.Stable)
	fmt.Printf("Wobbly: %d\n", summary.Wobbly)
	fmt.Printf("Unstable: %d\n", summary.Unstable)
	fmt.Printf("Skills fixed: %d\n", summary.SkillsFixed)
	fmt.Printf("Specs without beads: %d\n", summary.SpecsWithoutBeads)
	fmt.Printf("Beads without specs: %d\n", summary.BeadsWithoutSpecs)

	if summary.LastScanAt == "" {
		fmt.Fprintln(os.Stderr, "Note: no wobble scan history found.")
	}
}

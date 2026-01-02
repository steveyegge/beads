package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

func progressBar(current, total int) string {
	const width = 40
	if total == 0 {
		return "[" + string(make([]byte, width)) + "]"
	}
	filled := (current * width) / total
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += " "
		}
	}
	return "[" + bar + "]"
}

//nolint:unparam // ctx may be used in future for cancellation
func runCompactRPC(_ context.Context) {
	if compactID != "" && compactAll {
		fmt.Fprintf(os.Stderr, "Error: cannot use --id and --all together\n")
		os.Exit(1)
	}

	if compactForce && compactID == "" {
		fmt.Fprintf(os.Stderr, "Error: --force requires --id\n")
		os.Exit(1)
	}

	if compactID == "" && !compactAll && !compactDryRun {
		fmt.Fprintf(os.Stderr, "Error: must specify --all, --id, or --dry-run\n")
		os.Exit(1)
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" && !compactDryRun {
		fmt.Fprintf(os.Stderr, "Error: ANTHROPIC_API_KEY environment variable not set\n")
		os.Exit(1)
	}

	args := map[string]interface{}{
		"tier":       compactTier,
		"dry_run":    compactDryRun,
		"force":      compactForce,
		"all":        compactAll,
		"api_key":    apiKey,
		"workers":    compactWorkers,
		"batch_size": compactBatch,
	}
	if compactID != "" {
		args["issue_id"] = compactID
	}

	resp, err := daemonClient.Execute("compact", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.Success {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	if jsonOutput {
		fmt.Println(string(resp.Data))
		return
	}

	var result struct {
		Success       bool   `json:"success"`
		IssueID       string `json:"issue_id,omitempty"`
		OriginalSize  int    `json:"original_size,omitempty"`
		CompactedSize int    `json:"compacted_size,omitempty"`
		Reduction     string `json:"reduction,omitempty"`
		Duration      string `json:"duration,omitempty"`
		DryRun        bool   `json:"dry_run,omitempty"`
		Results       []struct {
			IssueID       string `json:"issue_id"`
			Success       bool   `json:"success"`
			Error         string `json:"error,omitempty"`
			OriginalSize  int    `json:"original_size,omitempty"`
			CompactedSize int    `json:"compacted_size,omitempty"`
			Reduction     string `json:"reduction,omitempty"`
		} `json:"results,omitempty"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if compactID != "" {
		if result.DryRun {
			fmt.Printf("DRY RUN - Tier %d compaction\n\n", compactTier)
			fmt.Printf("Issue: %s\n", compactID)
			fmt.Printf("Original size: %d bytes\n", result.OriginalSize)
			fmt.Printf("Estimated reduction: %s\n", result.Reduction)
		} else {
			fmt.Printf("Successfully compacted %s\n", result.IssueID)
			fmt.Printf("Original size: %d bytes\n", result.OriginalSize)
			fmt.Printf("Compacted size: %d bytes\n", result.CompactedSize)
			fmt.Printf("Reduction: %s\n", result.Reduction)
			fmt.Printf("Duration: %s\n", result.Duration)
		}
	} else if compactAll {
		if result.DryRun {
			fmt.Printf("DRY RUN - Found %d candidates for Tier %d compaction\n", len(result.Results), compactTier)
		} else {
			successCount := 0
			for _, r := range result.Results {
				if r.Success {
					successCount++
				}
			}
			fmt.Printf("Compacted %d/%d issues in %s\n", successCount, len(result.Results), result.Duration)
			for _, r := range result.Results {
				if r.Success {
					fmt.Printf("  ✓ %s: %d → %d bytes (%s)\n", r.IssueID, r.OriginalSize, r.CompactedSize, r.Reduction)
				} else {
					fmt.Printf("  ✗ %s: %s\n", r.IssueID, r.Error)
				}
			}
		}
	}
}

func runCompactStatsRPC() {
	args := map[string]interface{}{
		"tier": compactTier,
	}

	resp, err := daemonClient.Execute("compact_stats", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.Success {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	if jsonOutput {
		fmt.Println(string(resp.Data))
		return
	}

	var result struct {
		Success bool `json:"success"`
		Stats   struct {
			Tier1Candidates  int    `json:"tier1_candidates"`
			Tier2Candidates  int    `json:"tier2_candidates"`
			TotalClosed      int    `json:"total_closed"`
			Tier1MinAge      string `json:"tier1_min_age"`
			Tier2MinAge      string `json:"tier2_min_age"`
			EstimatedSavings string `json:"estimated_savings,omitempty"`
		} `json:"stats"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nCompaction Statistics\n")
	fmt.Printf("=====================\n\n")
	fmt.Printf("Total closed issues: %d\n\n", result.Stats.TotalClosed)
	fmt.Printf("Tier 1 (30+ days closed, not compacted):\n")
	fmt.Printf("  Candidates: %d\n", result.Stats.Tier1Candidates)
	fmt.Printf("  Min age: %s\n\n", result.Stats.Tier1MinAge)
	fmt.Printf("Tier 2 (90+ days closed, Tier 1 compacted):\n")
	fmt.Printf("  Candidates: %d\n", result.Stats.Tier2Candidates)
	fmt.Printf("  Min age: %s\n", result.Stats.Tier2MinAge)
}

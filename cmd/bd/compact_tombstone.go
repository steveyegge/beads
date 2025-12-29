package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TombstonePruneResult contains the results of tombstone pruning
type TombstonePruneResult struct {
	PrunedCount int
	PrunedIDs   []string
	TTLDays     int
}

// pruneExpiredTombstones reads issues.jsonl, removes expired tombstones,
// and writes back the pruned file. Returns the prune result.
// If customTTL is > 0, it overrides the default TTL (bypasses MinTombstoneTTL safety).
// If customTTL is 0, uses DefaultTombstoneTTL.
func pruneExpiredTombstones(customTTL time.Duration) (*TombstonePruneResult, error) {
	beadsDir := filepath.Dir(dbPath)
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")

	// Check if issues.jsonl exists
	if _, err := os.Stat(issuesPath); os.IsNotExist(err) {
		return &TombstonePruneResult{}, nil
	}

	// Read all issues
	// nolint:gosec // G304: issuesPath is controlled from beadsDir
	file, err := os.Open(issuesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open issues.jsonl: %w", err)
	}

	var allIssues []*types.Issue
	decoder := json.NewDecoder(file)
	for {
		var issue types.Issue
		if err := decoder.Decode(&issue); err != nil {
			if err.Error() == "EOF" {
				break
			}
			// Skip corrupt lines
			continue
		}
		allIssues = append(allIssues, &issue)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close issues file: %w", err)
	}

	// Determine TTL - customTTL > 0 overrides default (for --hard mode)
	ttl := types.DefaultTombstoneTTL
	if customTTL > 0 {
		ttl = customTTL
	}
	ttlDays := int(ttl.Hours() / 24)

	// Filter out expired tombstones
	var kept []*types.Issue
	var prunedIDs []string
	for _, issue := range allIssues {
		if issue.IsExpired(ttl) {
			prunedIDs = append(prunedIDs, issue.ID)
		} else {
			kept = append(kept, issue)
		}
	}

	if len(prunedIDs) == 0 {
		return &TombstonePruneResult{TTLDays: ttlDays}, nil
	}

	// Write back the pruned file atomically
	dir := filepath.Dir(issuesPath)
	base := filepath.Base(issuesPath)
	tempFile, err := os.CreateTemp(dir, base+".prune.*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	encoder := json.NewEncoder(tempFile)
	for _, issue := range kept {
		if err := encoder.Encode(issue); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
			return nil, fmt.Errorf("failed to write issue %s: %w", issue.ID, err)
		}
	}

	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomically replace
	if err := os.Rename(tempPath, issuesPath); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to replace issues.jsonl: %w", err)
	}

	return &TombstonePruneResult{
		PrunedCount: len(prunedIDs),
		PrunedIDs:   prunedIDs,
		TTLDays:     ttlDays,
	}, nil
}

// previewPruneTombstones checks what tombstones would be pruned without modifying files.
// Used for dry-run mode in cleanup command.
// If customTTL is > 0, it overrides the default TTL (bypasses MinTombstoneTTL safety).
// If customTTL is 0, uses DefaultTombstoneTTL.
func previewPruneTombstones(customTTL time.Duration) (*TombstonePruneResult, error) {
	beadsDir := filepath.Dir(dbPath)
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")

	// Check if issues.jsonl exists
	if _, err := os.Stat(issuesPath); os.IsNotExist(err) {
		return &TombstonePruneResult{}, nil
	}

	// Read all issues
	// nolint:gosec // G304: issuesPath is controlled from beadsDir
	file, err := os.Open(issuesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open issues.jsonl: %w", err)
	}
	defer file.Close()

	var allIssues []*types.Issue
	decoder := json.NewDecoder(file)
	for {
		var issue types.Issue
		if err := decoder.Decode(&issue); err != nil {
			if err.Error() == "EOF" {
				break
			}
			// Skip corrupt lines
			continue
		}
		allIssues = append(allIssues, &issue)
	}

	// Determine TTL - customTTL > 0 overrides default (for --hard mode)
	ttl := types.DefaultTombstoneTTL
	if customTTL > 0 {
		ttl = customTTL
	}
	ttlDays := int(ttl.Hours() / 24)

	// Count expired tombstones
	var prunedIDs []string
	for _, issue := range allIssues {
		if issue.IsExpired(ttl) {
			prunedIDs = append(prunedIDs, issue.ID)
		}
	}

	return &TombstonePruneResult{
		PrunedCount: len(prunedIDs),
		PrunedIDs:   prunedIDs,
		TTLDays:     ttlDays,
	}, nil
}

// runCompactPrune handles the --prune mode for standalone tombstone pruning.
// This mode only prunes expired tombstones from issues.jsonl without doing
// any semantic compaction. It's useful for reducing sync overhead.
func runCompactPrune() {
	start := time.Now()

	// Calculate TTL from --older-than flag (0 means use default 30 days)
	var customTTL time.Duration
	if compactOlderThan > 0 {
		customTTL = time.Duration(compactOlderThan) * 24 * time.Hour
	}

	if compactDryRun {
		// Preview mode - show what would be pruned
		result, err := previewPruneTombstones(customTTL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to preview tombstones: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			output := map[string]interface{}{
				"dry_run":       true,
				"prune_count":   result.PrunedCount,
				"ttl_days":      result.TTLDays,
				"tombstone_ids": result.PrunedIDs,
			}
			outputJSON(output)
			return
		}

		fmt.Printf("DRY RUN - Tombstone Pruning\n\n")
		fmt.Printf("TTL: %d days\n", result.TTLDays)
		fmt.Printf("Tombstones that would be pruned: %d\n", result.PrunedCount)
		if len(result.PrunedIDs) > 0 && len(result.PrunedIDs) <= 20 {
			fmt.Println("\nTombstone IDs:")
			for _, id := range result.PrunedIDs {
				fmt.Printf("  - %s\n", id)
			}
		} else if len(result.PrunedIDs) > 20 {
			fmt.Printf("\nFirst 20 tombstone IDs:\n")
			for _, id := range result.PrunedIDs[:20] {
				fmt.Printf("  - %s\n", id)
			}
			fmt.Printf("  ... and %d more\n", len(result.PrunedIDs)-20)
		}
		return
	}

	// Actually prune tombstones
	result, err := pruneExpiredTombstones(customTTL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to prune tombstones: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(start)

	if jsonOutput {
		output := map[string]interface{}{
			"success":       true,
			"pruned_count":  result.PrunedCount,
			"ttl_days":      result.TTLDays,
			"tombstone_ids": result.PrunedIDs,
			"elapsed_ms":    elapsed.Milliseconds(),
		}
		outputJSON(output)
		return
	}

	if result.PrunedCount == 0 {
		fmt.Printf("No expired tombstones to prune (TTL: %d days)\n", result.TTLDays)
		return
	}

	fmt.Printf("✓ Pruned %d expired tombstone(s)\n", result.PrunedCount)
	fmt.Printf("  TTL: %d days\n", result.TTLDays)
	fmt.Printf("  Time: %v\n", elapsed)
	if len(result.PrunedIDs) <= 10 {
		fmt.Println("\nPruned IDs:")
		for _, id := range result.PrunedIDs {
			fmt.Printf("  - %s\n", id)
		}
	}
}

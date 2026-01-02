package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/types"
)

// detectLinearConflicts finds issues that have been modified both locally and in Linear
// since the last sync. This is a more expensive operation as it fetches individual
// issue timestamps from Linear.
func detectLinearConflicts(ctx context.Context) ([]linear.Conflict, error) {
	lastSyncStr, _ := store.GetConfig(ctx, "linear.last_sync")
	if lastSyncStr == "" {
		return nil, nil
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp: %w", err)
	}

	config := loadLinearMappingConfig(ctx)

	client, err := getLinearClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Linear client: %w", err)
	}

	// Get all local issues with Linear external refs
	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	var conflicts []linear.Conflict

	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !linear.IsLinearExternalRef(*issue.ExternalRef) {
			continue
		}

		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		linearIdentifier := linear.ExtractLinearIdentifier(*issue.ExternalRef)
		if linearIdentifier == "" {
			continue
		}

		linearIssue, err := client.FetchIssueByIdentifier(ctx, linearIdentifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch Linear issue %s for conflict check: %v\n",
				linearIdentifier, err)
			continue
		}
		if linearIssue == nil {
			continue
		}

		linearUpdatedAt, err := time.Parse(time.RFC3339, linearIssue.UpdatedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse Linear UpdatedAt for %s: %v\n",
				linearIdentifier, err)
			continue
		}

		if !linearUpdatedAt.After(lastSync) {
			continue
		}

		localComparable := linear.NormalizeIssueForLinearHash(issue)
		linearComparable := linear.IssueToBeads(linearIssue, config).Issue.(*types.Issue)
		if localComparable.ComputeContentHash() == linearComparable.ComputeContentHash() {
			continue
		}

		conflicts = append(conflicts, linear.Conflict{
			IssueID:           issue.ID,
			LocalUpdated:      issue.UpdatedAt,
			LinearUpdated:     linearUpdatedAt,
			LinearExternalRef: *issue.ExternalRef,
			LinearIdentifier:  linearIdentifier,
			LinearInternalID:  linearIssue.ID,
		})
	}

	return conflicts, nil
}

// reimportLinearConflicts re-imports conflicting issues from Linear (Linear wins).
// For each conflict, fetches the current state from Linear and updates the local copy.
func reimportLinearConflicts(ctx context.Context, conflicts []linear.Conflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	client, err := getLinearClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Linear client: %w", err)
	}

	config := loadLinearMappingConfig(ctx)
	resolved := 0
	failed := 0

	for _, conflict := range conflicts {
		linearIssue, err := client.FetchIssueByIdentifier(ctx, conflict.LinearIdentifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to fetch %s for resolution: %v\n",
				conflict.LinearIdentifier, err)
			failed++
			continue
		}
		if linearIssue == nil {
			fmt.Fprintf(os.Stderr, "  Warning: Linear issue %s not found, skipping\n",
				conflict.LinearIdentifier)
			failed++
			continue
		}

		updates := linear.BuildLinearToLocalUpdates(linearIssue, config)

		err = store.UpdateIssue(ctx, conflict.IssueID, updates, actor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to update local issue %s: %v\n",
				conflict.IssueID, err)
			failed++
			continue
		}

		fmt.Printf("  Resolved: %s <- %s (Linear wins)\n", conflict.IssueID, conflict.LinearIdentifier)
		resolved++
	}

	if failed > 0 {
		return fmt.Errorf("%d conflict(s) failed to resolve", failed)
	}

	fmt.Printf("  Resolved %d conflict(s) by keeping Linear version\n", resolved)
	return nil
}

// resolveLinearConflictsByTimestamp resolves conflicts by keeping the newer version.
// For each conflict, compares local and Linear UpdatedAt timestamps.
// If Linear is newer, re-imports from Linear. If local is newer, push will overwrite.
func resolveLinearConflictsByTimestamp(ctx context.Context, conflicts []linear.Conflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	var linearWins []linear.Conflict
	var localWins []linear.Conflict

	for _, conflict := range conflicts {
		if conflict.LinearUpdated.After(conflict.LocalUpdated) {
			linearWins = append(linearWins, conflict)
		} else {
			localWins = append(localWins, conflict)
		}
	}

	if len(linearWins) > 0 {
		fmt.Printf("  %d conflict(s): Linear is newer, will re-import\n", len(linearWins))
	}
	if len(localWins) > 0 {
		fmt.Printf("  %d conflict(s): Local is newer, will push to Linear\n", len(localWins))
	}

	if len(linearWins) > 0 {
		err := reimportLinearConflicts(ctx, linearWins)
		if err != nil {
			return fmt.Errorf("failed to re-import Linear-wins conflicts: %w", err)
		}
	}

	if len(localWins) > 0 {
		for _, conflict := range localWins {
			fmt.Printf("  Resolved: %s -> %s (local wins, will push)\n",
				conflict.IssueID, conflict.LinearIdentifier)
		}
	}

	return nil
}

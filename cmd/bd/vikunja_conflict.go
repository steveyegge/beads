// cmd/bd/vikunja_conflict.go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/vikunja"
)

// detectVikunjaConflicts finds issues modified both locally and in Vikunja since last sync.
func detectVikunjaConflicts(ctx context.Context) ([]vikunja.Conflict, error) {
	lastSyncStr, _ := store.GetConfig(ctx, "vikunja.last_sync")
	if lastSyncStr == "" {
		return nil, nil // No previous sync, no conflicts possible
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp: %w", err)
	}

	apiURL, _ := getVikunjaConfig(ctx, "vikunja.api_url")
	config := loadVikunjaMappingConfig(ctx)

	client, err := getVikunjaClient(ctx)
	if err != nil {
		return nil, err
	}

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	var conflicts []vikunja.Conflict

	for _, issue := range allIssues {
		// Only check issues with Vikunja refs modified locally since last sync
		if issue.ExternalRef == nil || !vikunja.IsVikunjaExternalRef(*issue.ExternalRef, apiURL) {
			continue
		}
		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		taskID, ok := vikunja.ExtractVikunjaTaskID(*issue.ExternalRef)
		if !ok {
			continue
		}

		vikunjaTask, err := client.FetchTask(ctx, taskID)
		if err != nil || vikunjaTask == nil {
			continue
		}

		// Check if Vikunja task was also modified since last sync
		if !vikunjaTask.Updated.After(lastSync) {
			continue
		}

		// Compare content hashes to check if actually different
		localNorm := vikunja.NormalizeIssueForVikunjaHash(issue)
		vikunjaConverted := vikunja.TaskToBeads(vikunjaTask, apiURL, config)
		remoteNorm := vikunja.NormalizeIssueForVikunjaHash(vikunjaConverted.Issue.(*types.Issue))

		if localNorm.ComputeContentHash() == remoteNorm.ComputeContentHash() {
			continue // Same content, not a real conflict
		}

		conflicts = append(conflicts, vikunja.Conflict{
			IssueID:            issue.ID,
			LocalUpdated:       issue.UpdatedAt,
			VikunjaUpdated:     vikunjaTask.Updated,
			VikunjaExternalRef: *issue.ExternalRef,
			VikunjaTaskID:      taskID,
		})
	}

	return conflicts, nil
}

// resolveVikunjaConflictsByTimestamp resolves conflicts by keeping newer version.
func resolveVikunjaConflictsByTimestamp(conflicts []vikunja.Conflict) (
	forceUpdateIDs map[string]bool, skipUpdateIDs map[string]bool) {

	forceUpdateIDs = make(map[string]bool)
	skipUpdateIDs = make(map[string]bool)

	for _, conflict := range conflicts {
		if conflict.VikunjaUpdated.After(conflict.LocalUpdated) {
			// Vikunja wins - skip pushing this issue
			skipUpdateIDs[conflict.IssueID] = true
		} else {
			// Local wins - force push this issue
			forceUpdateIDs[conflict.IssueID] = true
		}
	}

	return forceUpdateIDs, skipUpdateIDs
}

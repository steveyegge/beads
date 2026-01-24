// cmd/bd/vikunja_sync.go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/vikunja"
)

// doPullFromVikunja imports tasks from Vikunja.
func doPullFromVikunja(ctx context.Context, dryRun bool, state string) (*vikunja.PullStats, error) {
	stats := &vikunja.PullStats{}

	client, err := getVikunjaClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Vikunja client: %w", err)
	}

	// Check for incremental sync
	var vikunjaTasks []vikunja.Task
	lastSyncStr, _ := store.GetConfig(ctx, "vikunja.last_sync")

	if lastSyncStr != "" {
		lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid vikunja.last_sync timestamp, doing full sync\n")
			vikunjaTasks, err = client.FetchTasks(ctx, state)
		} else {
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			vikunjaTasks, err = client.FetchTasksSince(ctx, state, lastSync)
			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		vikunjaTasks, err = client.FetchTasks(ctx, state)
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	if err != nil {
		return stats, fmt.Errorf("failed to fetch tasks from Vikunja: %w", err)
	}

	if len(vikunjaTasks) == 0 {
		fmt.Println("  No tasks to import")
		return stats, nil
	}

	mappingConfig := loadVikunjaMappingConfig(ctx)
	apiURL, _ := getVikunjaConfig(ctx, "vikunja.api_url")

	// Convert tasks to beads issues
	var beadsIssues []*types.Issue
	var allDeps []vikunja.DependencyInfo
	vikunjaIDToBeadsID := make(map[int64]string)

	for i := range vikunjaTasks {
		conversion := vikunja.TaskToBeads(&vikunjaTasks[i], apiURL, mappingConfig)
		beadsIssues = append(beadsIssues, conversion.Issue.(*types.Issue))
		allDeps = append(allDeps, conversion.Dependencies...)
	}

	// Generate IDs for new issues
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "bd"
	}

	idMode, _ := store.GetConfig(ctx, "vikunja.id_mode")
	if idMode == "" {
		idMode = "hash"
	}

	if idMode == "hash" {
		existingIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
		if err != nil {
			return stats, fmt.Errorf("failed to fetch existing issues: %w", err)
		}
		usedIDs := make(map[string]bool, len(existingIssues))
		for _, issue := range existingIssues {
			if issue.ID != "" {
				usedIDs[issue.ID] = true
			}
		}

		// Generate hash-based IDs
		hashLength := 6
		if hashLenStr, _ := store.GetConfig(ctx, "vikunja.hash_length"); hashLenStr != "" {
			fmt.Sscanf(hashLenStr, "%d", &hashLength)
		}

		for _, issue := range beadsIssues {
			if issue.ID == "" {
				// Generate ID based on content hash
				issue.ID = generateHashID(issue, prefix, hashLength, usedIDs)
				usedIDs[issue.ID] = true
			}
		}
	}

	// Import issues
	opts := ImportOptions{
		DryRun:     dryRun,
		SkipUpdate: false,
	}

	result, err := importIssuesCore(ctx, dbPath, store, beadsIssues, opts)
	if err != nil {
		return stats, fmt.Errorf("import failed: %w", err)
	}

	stats.Created = result.Created
	stats.Updated = result.Updated
	stats.Skipped = result.Skipped

	if dryRun {
		fmt.Printf("  Would import %d tasks from Vikunja\n", len(vikunjaTasks))
		return stats, nil
	}

	// Build ID mapping for dependencies
	allBeadsIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch issues for dependency mapping: %v\n", err)
		return stats, nil
	}

	for _, issue := range allBeadsIssues {
		if issue.ExternalRef != nil && vikunja.IsVikunjaExternalRef(*issue.ExternalRef, apiURL) {
			taskID, ok := vikunja.ExtractVikunjaTaskID(*issue.ExternalRef)
			if ok {
				vikunjaIDToBeadsID[taskID] = issue.ID
			}
		}
	}

	// Create dependencies
	depsCreated := 0
	for _, dep := range allDeps {
		fromID, fromOK := vikunjaIDToBeadsID[dep.FromVikunjaID]
		toID, toOK := vikunjaIDToBeadsID[dep.ToVikunjaID]

		if !fromOK || !toOK {
			continue
		}

		dependency := &types.Dependency{
			IssueID:     fromID,
			DependsOnID: toID,
			Type:        types.DependencyType(dep.Type),
			CreatedAt:   time.Now(),
		}
		err := store.AddDependency(ctx, dependency, actor)
		if err != nil {
			if !strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "duplicate") {
				fmt.Fprintf(os.Stderr, "Warning: failed to create dependency %s -> %s: %v\n",
					fromID, toID, err)
			}
		} else {
			depsCreated++
		}
	}

	if depsCreated > 0 {
		fmt.Printf("  Created %d dependencies from Vikunja relations\n", depsCreated)
	}

	// Update last sync timestamp
	if err := store.SetConfig(ctx, "vikunja.last_sync", time.Now().Format(time.RFC3339)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update last_sync timestamp: %v\n", err)
	}

	return stats, nil
}

// doPushToVikunja exports issues to Vikunja.
func doPushToVikunja(ctx context.Context, dryRun bool, createOnly bool, updateRefs bool,
	forceUpdateIDs map[string]bool, skipUpdateIDs map[string]bool,
	typeFilters []string, excludeTypes []string) (*vikunja.PushStats, error) {

	stats := &vikunja.PushStats{}

	client, err := getVikunjaClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Vikunja client: %w", err)
	}

	apiURL, _ := getVikunjaConfig(ctx, "vikunja.api_url")

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return stats, fmt.Errorf("failed to get local issues: %w", err)
	}

	// Apply type filters
	if len(typeFilters) > 0 || len(excludeTypes) > 0 {
		typeSet := make(map[string]bool, len(typeFilters))
		for _, t := range typeFilters {
			typeSet[strings.ToLower(t)] = true
		}
		excludeSet := make(map[string]bool, len(excludeTypes))
		for _, t := range excludeTypes {
			excludeSet[strings.ToLower(t)] = true
		}

		var filtered []*types.Issue
		for _, issue := range allIssues {
			issueType := strings.ToLower(string(issue.IssueType))
			if len(typeFilters) > 0 && !typeSet[issueType] {
				continue
			}
			if excludeSet[issueType] {
				continue
			}
			filtered = append(filtered, issue)
		}
		allIssues = filtered
	}

	// Separate issues to create vs update
	var toCreate []*types.Issue
	var toUpdate []*types.Issue

	for _, issue := range allIssues {
		if issue.IsTombstone() {
			continue
		}

		if issue.ExternalRef != nil && vikunja.IsVikunjaExternalRef(*issue.ExternalRef, apiURL) {
			if !createOnly {
				toUpdate = append(toUpdate, issue)
			}
		} else if issue.ExternalRef == nil {
			toCreate = append(toCreate, issue)
		}
	}

	mappingConfig := loadVikunjaMappingConfig(ctx)

	// Create new tasks
	for _, issue := range toCreate {
		if dryRun {
			stats.Created++
			continue
		}

		taskData := vikunja.BeadsToVikunjaTask(issue, mappingConfig)
		task := &vikunja.Task{
			Title:       taskData["title"].(string),
			Description: taskData["description"].(string),
			Priority:    taskData["priority"].(int),
			Done:        taskData["done"].(bool),
		}

		created, err := client.CreateTask(ctx, task)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create task '%s': %v\n", issue.Title, err)
			stats.Errors++
			continue
		}

		stats.Created++
		fmt.Printf("  Created: %s -> %d\n", issue.ID, created.ID)

		// Update local issue with external ref
		if updateRefs {
			externalRef := vikunja.BuildVikunjaExternalRef(apiURL, created.ID)
			updates := map[string]any{"external_ref": externalRef}
			if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update external_ref: %v\n", err)
			}
		}
	}

	// Update existing tasks
	if len(toUpdate) > 0 && !createOnly {
		for _, issue := range toUpdate {
			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			taskID, ok := vikunja.ExtractVikunjaTaskID(*issue.ExternalRef)
			if !ok {
				fmt.Fprintf(os.Stderr, "Warning: could not extract task ID from %s\n", *issue.ExternalRef)
				stats.Errors++
				continue
			}

			// Fetch current Vikunja task for comparison
			vikunjaTask, err := client.FetchTask(ctx, taskID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch Vikunja task %d: %v\n", taskID, err)
				stats.Errors++
				continue
			}

			// Check if update needed
			forcedUpdate := forceUpdateIDs != nil && forceUpdateIDs[issue.ID]
			if !forcedUpdate && !issue.UpdatedAt.After(vikunjaTask.Updated) {
				stats.Skipped++
				continue
			}

			// Compare content hashes
			if !forcedUpdate {
				localNorm := vikunja.NormalizeIssueForVikunjaHash(issue)
				vikunjaConverted := vikunja.TaskToBeads(vikunjaTask, apiURL, mappingConfig)
				remoteNorm := vikunja.NormalizeIssueForVikunjaHash(vikunjaConverted.Issue.(*types.Issue))
				if localNorm.ComputeContentHash() == remoteNorm.ComputeContentHash() {
					stats.Skipped++
					continue
				}
			}

			if dryRun {
				stats.Updated++
				continue
			}

			taskData := vikunja.BeadsToVikunjaTask(issue, mappingConfig)
			_, err = client.UpdateTask(ctx, taskID, taskData)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update Vikunja task %d: %v\n", taskID, err)
				stats.Errors++
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated: %s -> %d\n", issue.ID, taskID)
		}
	}

	if dryRun {
		fmt.Printf("  Would create %d tasks in Vikunja\n", stats.Created)
		if !createOnly {
			fmt.Printf("  Would update %d tasks in Vikunja\n", stats.Updated)
		}
	}

	return stats, nil
}

// generateHashID creates a hash-based ID for an issue.
func generateHashID(issue *types.Issue, prefix string, length int, usedIDs map[string]bool) string {
	hash := issue.ComputeContentHash()
	for l := length; l <= 8; l++ {
		id := fmt.Sprintf("%s-%s", prefix, hash[:l])
		if !usedIDs[id] {
			return id
		}
	}
	// Fallback with nonce
	for nonce := 0; nonce < 1000; nonce++ {
		id := fmt.Sprintf("%s-%s%d", prefix, hash[:length], nonce)
		if !usedIDs[id] {
			return id
		}
	}
	return fmt.Sprintf("%s-%s", prefix, hash)
}

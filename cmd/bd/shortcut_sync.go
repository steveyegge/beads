package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/shortcut"
	"github.com/steveyegge/beads/internal/types"
)

// doPullFromShortcut imports stories from Shortcut using the REST API.
// Supports incremental sync by checking shortcut.last_sync config and only fetching
// stories updated since that timestamp.
func doPullFromShortcut(ctx context.Context, dryRun bool, state string, skipStoryIDs map[int64]bool) (*shortcut.PullStats, error) {
	stats := &shortcut.PullStats{}

	client, err := getShortcutClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Shortcut client: %w", err)
	}

	var stories []shortcut.Story
	lastSyncStr, _ := store.GetConfig(ctx, "shortcut.last_sync")

	if lastSyncStr != "" {
		lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid shortcut.last_sync timestamp, doing full sync\n")
			stories, err = client.FetchStories(ctx, state)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch stories from Shortcut: %w", err)
			}
		} else {
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			stories, err = client.FetchStoriesSince(ctx, state, lastSync)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch stories from Shortcut (incremental): %w", err)
			}
			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		stories, err = client.FetchStories(ctx, state)
		if err != nil {
			return stats, fmt.Errorf("failed to fetch stories from Shortcut: %w", err)
		}
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	// Build state cache for workflow state mapping
	stateCache, err := shortcut.BuildStateCache(ctx, client)
	if err != nil {
		return stats, fmt.Errorf("failed to fetch workflows: %w", err)
	}

	mappingConfig := loadShortcutMappingConfig(ctx)

	idMode := getShortcutIDMode(ctx)
	hashLength := getShortcutHashLength(ctx)

	var beadsIssues []*types.Issue
	var allDeps []shortcut.DependencyInfo
	storyIDToBeadsID := make(map[int64]string)

	for i := range stories {
		// Skip stories that are in our skip list (for conflict resolution)
		if skipStoryIDs != nil && skipStoryIDs[stories[i].ID] {
			stats.Skipped++
			continue
		}

		conversion := shortcut.StoryToBeads(&stories[i], stateCache, mappingConfig)
		beadsIssues = append(beadsIssues, conversion.Issue.(*types.Issue))
		allDeps = append(allDeps, conversion.Dependencies...)
	}

	if len(beadsIssues) == 0 {
		fmt.Println("  No stories to import")
		return stats, nil
	}

	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "bd"
	}

	if idMode == "hash" {
		existingIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
		if err != nil {
			return stats, fmt.Errorf("failed to fetch existing issues for ID collision avoidance: %w", err)
		}
		usedIDs := make(map[string]bool, len(existingIssues))
		for _, issue := range existingIssues {
			if issue.ID != "" {
				usedIDs[issue.ID] = true
			}
		}

		idOpts := shortcut.IDGenerationOptions{
			BaseLength: hashLength,
			MaxLength:  8,
			UsedIDs:    usedIDs,
		}
		if err := shortcut.GenerateIssueIDs(beadsIssues, prefix, "shortcut-import", idOpts); err != nil {
			return stats, fmt.Errorf("failed to generate issue IDs: %w", err)
		}
	} else if idMode != "db" {
		return stats, fmt.Errorf("unsupported shortcut.id_mode %q (expected \"hash\" or \"db\")", idMode)
	}

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
	stats.Skipped += result.Skipped

	if dryRun {
		if stats.Incremental {
			fmt.Printf("  Would import %d stories from Shortcut (incremental since %s)\n",
				len(stories), stats.SyncedSince)
		} else {
			fmt.Printf("  Would import %d stories from Shortcut (full sync)\n", len(stories))
		}
		return stats, nil
	}

	// Build mapping of Shortcut story IDs to beads IDs for dependency resolution
	allBeadsIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch issues for dependency mapping: %v\n", err)
		return stats, nil
	}

	for _, issue := range allBeadsIssues {
		if issue.ExternalRef != nil && shortcut.IsShortcutExternalRef(*issue.ExternalRef) {
			storyID, ok := shortcut.ExtractStoryID(*issue.ExternalRef)
			if ok {
				storyIDToBeadsID[storyID] = issue.ID
			}
		}
	}

	// Create dependencies from Shortcut story links
	depsCreated := 0
	for _, dep := range allDeps {
		fromID, fromOK := storyIDToBeadsID[dep.FromStoryID]
		toID, toOK := storyIDToBeadsID[dep.ToStoryID]

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
				fmt.Fprintf(os.Stderr, "Warning: failed to create dependency %s -> %s (%s): %v\n",
					fromID, toID, dep.Type, err)
			}
		} else {
			depsCreated++
		}
	}

	if depsCreated > 0 {
		fmt.Printf("  Created %d dependencies from Shortcut story links\n", depsCreated)
	}

	return stats, nil
}

// doPushToShortcut exports issues to Shortcut using the REST API.
func doPushToShortcut(ctx context.Context, dryRun bool, createOnly bool, updateRefs bool, forceUpdateIDs map[string]bool, skipUpdateIDs map[string]bool) (*shortcut.PushStats, error) {
	stats := &shortcut.PushStats{}

	client, err := getShortcutClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Shortcut client: %w", err)
	}

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return stats, fmt.Errorf("failed to get local issues: %w", err)
	}

	var toCreate []*types.Issue
	var toUpdate []*types.Issue

	for _, issue := range allIssues {
		if issue.IsTombstone() {
			continue
		}

		if issue.ExternalRef != nil && shortcut.IsShortcutExternalRef(*issue.ExternalRef) {
			if !createOnly {
				toUpdate = append(toUpdate, issue)
			}
		} else if issue.ExternalRef == nil {
			toCreate = append(toCreate, issue)
		}
	}

	var stateCache *shortcut.StateCache
	if !dryRun && (len(toCreate) > 0 || (!createOnly && len(toUpdate) > 0)) {
		stateCache, err = shortcut.BuildStateCache(ctx, client)
		if err != nil {
			return stats, fmt.Errorf("failed to fetch workflows: %w", err)
		}
	}

	mappingConfig := loadShortcutMappingConfig(ctx)

	// Create new stories in Shortcut
	for _, issue := range toCreate {
		if dryRun {
			stats.Created++
			continue
		}

		params := shortcut.BeadsToStoryParams(issue, stateCache, mappingConfig)

		story, err := client.CreateStory(ctx, params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create story '%s' in Shortcut: %v\n", issue.Title, err)
			stats.Errors++
			continue
		}

		stats.Created++
		fmt.Printf("  Created: %s -> sc-%d\n", issue.ID, story.ID)

		if updateRefs && story.AppURL != "" {
			externalRef := story.AppURL
			if canonical, ok := shortcut.CanonicalizeShortcutExternalRef(externalRef); ok {
				externalRef = canonical
			}
			updates := map[string]interface{}{
				"external_ref": externalRef,
			}
			if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update external_ref for %s: %v\n", issue.ID, err)
				stats.Errors++
			}
		}
	}

	// Update existing stories in Shortcut
	if len(toUpdate) > 0 && !createOnly {
		for _, issue := range toUpdate {
			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			storyID, ok := shortcut.ExtractStoryID(*issue.ExternalRef)
			if !ok {
				fmt.Fprintf(os.Stderr, "Warning: could not extract Shortcut story ID from %s: %s\n",
					issue.ID, *issue.ExternalRef)
				stats.Errors++
				continue
			}

			story, err := client.GetStory(ctx, storyID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch Shortcut story %d: %v\n",
					storyID, err)
				stats.Errors++
				continue
			}
			if story == nil {
				fmt.Fprintf(os.Stderr, "Warning: Shortcut story %d not found (may have been deleted)\n",
					storyID)
				stats.Skipped++
				continue
			}

			shortcutUpdatedAt, err := time.Parse(time.RFC3339, story.UpdatedAt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse Shortcut UpdatedAt for story %d: %v\n",
					storyID, err)
				stats.Errors++
				continue
			}

			forcedUpdate := forceUpdateIDs != nil && forceUpdateIDs[issue.ID]
			if !forcedUpdate && !issue.UpdatedAt.After(shortcutUpdatedAt) {
				stats.Skipped++
				continue
			}

			// Compare content hashes to detect actual changes
			if !forcedUpdate {
				localComparable := normalizeIssueForShortcutHash(issue)
				shortcutComparable := shortcut.StoryToBeads(story, stateCache, mappingConfig).Issue.(*types.Issue)
				if localComparable.ComputeContentHash() == shortcutComparable.ComputeContentHash() {
					stats.Skipped++
					continue
				}
			}

			if dryRun {
				stats.Updated++
				continue
			}

			params := shortcut.BeadsToStoryUpdateParams(issue, stateCache, mappingConfig)

			updatedStory, err := client.UpdateStory(ctx, storyID, params)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update Shortcut story %d: %v\n",
					storyID, err)
				stats.Errors++
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated: %s -> sc-%d\n", issue.ID, updatedStory.ID)
		}
	}

	if dryRun {
		fmt.Printf("  Would create %d stories in Shortcut\n", stats.Created)
		if !createOnly {
			fmt.Printf("  Would update %d stories in Shortcut\n", stats.Updated)
		}
	}

	return stats, nil
}

// normalizeIssueForShortcutHash returns a copy of the issue with fields normalized
// for hash comparison with Shortcut. This strips fields that don't sync to Shortcut.
func normalizeIssueForShortcutHash(issue *types.Issue) *types.Issue {
	return &types.Issue{
		Title:       issue.Title,
		Description: issue.Description,
		Status:      issue.Status,
		IssueType:   issue.IssueType,
		Priority:    issue.Priority,
		Assignee:    issue.Assignee,
		Labels:      issue.Labels,
	}
}

// detectShortcutConflicts finds issues that have been modified both locally and in Shortcut
// since the last sync.
func detectShortcutConflicts(ctx context.Context) ([]shortcut.Conflict, error) {
	var conflicts []shortcut.Conflict

	lastSyncStr, _ := store.GetConfig(ctx, "shortcut.last_sync")
	if lastSyncStr == "" {
		// No previous sync, no conflicts possible
		return nil, nil
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid shortcut.last_sync timestamp: %w", err)
	}

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to get local issues: %w", err)
	}

	client, err := getShortcutClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Shortcut client: %w", err)
	}

	// Find issues with Shortcut refs that were modified locally since last sync
	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !shortcut.IsShortcutExternalRef(*issue.ExternalRef) {
			continue
		}

		// Check if modified locally since last sync
		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		storyID, ok := shortcut.ExtractStoryID(*issue.ExternalRef)
		if !ok {
			continue
		}

		// Fetch current Shortcut story
		story, err := client.GetStory(ctx, storyID)
		if err != nil || story == nil {
			// Story may have been deleted, not a conflict
			continue
		}

		shortcutUpdated, err := time.Parse(time.RFC3339, story.UpdatedAt)
		if err != nil {
			continue
		}

		// Check if Shortcut story was also modified since last sync
		if shortcutUpdated.After(lastSync) {
			conflicts = append(conflicts, shortcut.Conflict{
				IssueID:             issue.ID,
				LocalUpdated:        issue.UpdatedAt,
				ShortcutUpdated:     shortcutUpdated,
				ShortcutExternalRef: *issue.ExternalRef,
				ShortcutStoryID:     storyID,
			})
		}
	}

	return conflicts, nil
}

// reimportShortcutConflicts re-imports Shortcut stories to resolve conflicts
// by preferring the Shortcut version.
func reimportShortcutConflicts(ctx context.Context, conflicts []shortcut.Conflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	client, err := getShortcutClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Shortcut client: %w", err)
	}

	stateCache, err := shortcut.BuildStateCache(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to fetch workflows: %w", err)
	}

	mappingConfig := loadShortcutMappingConfig(ctx)

	for _, conflict := range conflicts {
		story, err := client.GetStory(ctx, conflict.ShortcutStoryID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch Shortcut story %d for conflict resolution: %v\n",
				conflict.ShortcutStoryID, err)
			continue
		}
		if story == nil {
			continue
		}

		// Build updates from Shortcut story
		updates := shortcut.BuildShortcutToLocalUpdates(story, stateCache, mappingConfig)

		if err := store.UpdateIssue(ctx, conflict.IssueID, updates, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update issue %s from Shortcut: %v\n",
				conflict.IssueID, err)
			continue
		}

		fmt.Printf("  Resolved conflict: %s (Shortcut wins)\n", conflict.IssueID)
	}

	return nil
}

// resolveShortcutConflictsByTimestamp resolves conflicts by using the newer version.
func resolveShortcutConflictsByTimestamp(ctx context.Context, conflicts []shortcut.Conflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	client, err := getShortcutClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Shortcut client: %w", err)
	}

	stateCache, err := shortcut.BuildStateCache(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to fetch workflows: %w", err)
	}

	mappingConfig := loadShortcutMappingConfig(ctx)

	for _, conflict := range conflicts {
		if conflict.ShortcutUpdated.After(conflict.LocalUpdated) {
			// Shortcut is newer, import the Shortcut version
			story, err := client.GetStory(ctx, conflict.ShortcutStoryID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch Shortcut story %d: %v\n",
					conflict.ShortcutStoryID, err)
				continue
			}
			if story == nil {
				continue
			}

			updates := shortcut.BuildShortcutToLocalUpdates(story, stateCache, mappingConfig)

			if err := store.UpdateIssue(ctx, conflict.IssueID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update issue %s: %v\n",
					conflict.IssueID, err)
				continue
			}

			fmt.Printf("  Resolved conflict: %s (Shortcut is newer)\n", conflict.IssueID)
		} else {
			// Local is newer, it will be pushed during the push phase
			fmt.Printf("  Resolved conflict: %s (local is newer)\n", conflict.IssueID)
		}
	}

	return nil
}

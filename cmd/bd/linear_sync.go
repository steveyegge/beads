package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/types"
)

// doPullFromLinear imports issues from Linear using the GraphQL API.
// Supports incremental sync by checking linear.last_sync config and only fetching
// issues updated since that timestamp.
func doPullFromLinear(ctx context.Context, dryRun bool, state string, skipLinearIDs map[string]bool) (*linear.PullStats, error) {
	stats := &linear.PullStats{}

	client, err := getLinearClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Linear client: %w", err)
	}

	var linearIssues []linear.Issue
	var linearProjects []linear.Project
	lastSyncStr, _ := store.GetConfig(ctx, "linear.last_sync")

	if lastSyncStr != "" {
		lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid linear.last_sync timestamp, doing full sync\n")
			linearIssues, err = client.FetchIssues(ctx, state)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch issues from Linear: %w", err)
			}
			linearProjects, err = client.FetchProjects(ctx, state)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch projects from Linear: %w", err)
			}
		} else {
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			linearIssues, err = client.FetchIssuesSince(ctx, state, lastSync)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch issues from Linear (incremental): %w", err)
			}
			// Note: Linear API doesn't support FetchProjectsSince easily, so we fetch all for now
			// or we could implement FetchProjectsSince if needed. For now, full fetch of projects
			// is usually acceptable as there are fewer projects than issues.
			// Ideally we would filter by updatedAt in the client.
			// Let's assume FetchProjects fetches all relevant projects for now.
			linearProjects, err = client.FetchProjects(ctx, state)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch projects from Linear: %w", err)
			}

			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		linearIssues, err = client.FetchIssues(ctx, state)
		if err != nil {
			return stats, fmt.Errorf("failed to fetch issues from Linear: %w", err)
		}
		linearProjects, err = client.FetchProjects(ctx, state)
		if err != nil {
			return stats, fmt.Errorf("failed to fetch projects from Linear: %w", err)
		}
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	mappingConfig := loadLinearMappingConfig(ctx)

	idMode := getLinearIDMode(ctx)
	hashLength := getLinearHashLength(ctx)

	var beadsIssues []*types.Issue
	var allDeps []linear.DependencyInfo
	linearIDToBeadsID := make(map[string]string)

	// Process Projects first (Epics)
	for i := range linearProjects {
		epic := linear.ProjectToEpic(&linearProjects[i])
		beadsIssues = append(beadsIssues, epic)
		// No dependencies from Project -> Epic conversion directly here,
		// but we track the ID mapping.
		// Note: Project IDs are UUIDs, not user-facing IDs like TEAM-123.
		// We use the URL or ID as the external ref.
	}

	for i := range linearIssues {
		conversion := linear.IssueToBeads(&linearIssues[i], mappingConfig)
		beadsIssues = append(beadsIssues, conversion.Issue.(*types.Issue))
		allDeps = append(allDeps, conversion.Dependencies...)
	}

	if len(beadsIssues) == 0 {
		fmt.Println("  No issues to import")
		return stats, nil
	}

	if len(skipLinearIDs) > 0 {
		var filteredIssues []*types.Issue
		skipped := 0
		for _, issue := range beadsIssues {
			if issue.ExternalRef == nil {
				filteredIssues = append(filteredIssues, issue)
				continue
			}
			// Check if it's a linear issue or project URL
			if !linear.IsLinearExternalRef(*issue.ExternalRef) {
				// Might be a project URL, check logic here or just allow if not standard issue ref
				// For now, if we can't extract an ID, we assume it's not skippable by ID
				filteredIssues = append(filteredIssues, issue)
				continue
			}

			linearID := linear.ExtractLinearIdentifier(*issue.ExternalRef)
			if linearID != "" && skipLinearIDs[linearID] {
				skipped++
				continue
			}
			filteredIssues = append(filteredIssues, issue)
		}
		if skipped > 0 {
			stats.Skipped += skipped
		}
		beadsIssues = filteredIssues

		if len(allDeps) > 0 {
			var filteredDeps []linear.DependencyInfo
			for _, dep := range allDeps {
				if skipLinearIDs[dep.FromLinearID] || skipLinearIDs[dep.ToLinearID] {
					continue
				}
				filteredDeps = append(filteredDeps, dep)
			}
			allDeps = filteredDeps
		}
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

		idOpts := linear.IDGenerationOptions{
			BaseLength: hashLength,
			MaxLength:  8,
			UsedIDs:    usedIDs,
		}
		if err := linear.GenerateIssueIDs(beadsIssues, prefix, "linear-import", idOpts); err != nil {
			return stats, fmt.Errorf("failed to generate issue IDs: %w", err)
		}
	} else if idMode != "db" {
		return stats, fmt.Errorf("unsupported linear.id_mode %q (expected \"hash\" or \"db\")", idMode)
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
	stats.Skipped = result.Skipped

	if dryRun {
		if stats.Incremental {
			fmt.Printf("  Would import %d issues and %d projects from Linear (incremental since %s)\n",
				len(linearIssues), len(linearProjects), stats.SyncedSince)
		} else {
			fmt.Printf("  Would import %d issues and %d projects from Linear (full sync)\n", len(linearIssues), len(linearProjects))
		}
		return stats, nil
	}

	allBeadsIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch issues for dependency mapping: %v\n", err)
		return stats, nil
	}

	// Re-scan for mapping, including Projects which might use UUIDs
	// We need to fetch all issues again to be safe, or just iterate what we have in memory if we trust it
	// But `store.SearchIssues` is authoritative.
	for _, issue := range allBeadsIssues {
		if issue.ExternalRef == nil {
			continue
		}
		// Check for standard Linear Issue
		if linear.IsLinearExternalRef(*issue.ExternalRef) {
			linearID := linear.ExtractLinearIdentifier(*issue.ExternalRef)
			if linearID != "" {
				linearIDToBeadsID[linearID] = issue.ID
			}
		} else {
			// Check if it's a Project
			// Project URLs: https://linear.app/team/project/name-slug-uuid
			// We can try to match the URL against the projects we just fetched to find the UUID
			for _, proj := range linearProjects {
				if proj.URL == *issue.ExternalRef {
					linearIDToBeadsID[proj.ID] = issue.ID
					break
				}
			}
		}
	}

	// Prioritize parent-child dependencies to avoid cycle detection failures.
	// We consider parent-child relationships structural and more important than blocking links.
	sort.Slice(allDeps, func(i, j int) bool {
		isParentChildI := allDeps[i].Type == "parent-child"
		isParentChildJ := allDeps[j].Type == "parent-child"
		if isParentChildI && !isParentChildJ {
			return true
		}
		if !isParentChildI && isParentChildJ {
			return false
		}
		return false
	})

	depsCreated := 0
	for _, dep := range allDeps {
		fromID, fromOK := linearIDToBeadsID[dep.FromLinearID]
		toID, toOK := linearIDToBeadsID[dep.ToLinearID]

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
		fmt.Printf("  Created %d dependencies from Linear relations\n", depsCreated)
	}

	return stats, nil
}

// doPushToLinear exports issues to Linear using the GraphQL API.
// typeFilters includes only issues matching these types (empty means all).
// excludeTypes excludes issues matching these types.
// includeEphemeral: if false (default), ephemeral issues (wisps, etc.) are excluded from push.
func doPushToLinear(ctx context.Context, dryRun bool, createOnly bool, updateRefs bool, forceUpdateIDs map[string]bool, skipUpdateIDs map[string]bool, typeFilters []string, excludeTypes []string, includeEphemeral bool) (*linear.PushStats, error) {
	stats := &linear.PushStats{}

	client, err := getLinearClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Linear client: %w", err)
	}

	filter := types.IssueFilter{}
	if !includeEphemeral {
		filter.Ephemeral = &includeEphemeral
	}
	allIssues, err := store.SearchIssues(ctx, "", filter)
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

			// If type filters specified, issue must match one
			if len(typeFilters) > 0 && !typeSet[issueType] {
				continue
			}
			// If exclude types specified, issue must not match any
			if excludeSet[issueType] {
				continue
			}
			filtered = append(filtered, issue)
		}
		allIssues = filtered
	}

	var toCreate []*types.Issue
	var toUpdate []*types.Issue
	var epicsToCreate []*types.Issue
	var epicsToUpdate []*types.Issue

	for _, issue := range allIssues {
		if issue.IsTombstone() {
			continue
		}

		// Separate Epics (Projects) from other issues
		if issue.IssueType == types.TypeEpic {
			if issue.ExternalRef != nil {
				// Assume it's a linear project if it has an external ref (weak check, but ok for now)
				// We really should check if it's a linear URL
				if !createOnly {
					epicsToUpdate = append(epicsToUpdate, issue)
				}
			} else {
				epicsToCreate = append(epicsToCreate, issue)
			}
			continue
		}

		if issue.ExternalRef != nil && linear.IsLinearExternalRef(*issue.ExternalRef) {
			if !createOnly {
				toUpdate = append(toUpdate, issue)
			}
		} else if issue.ExternalRef == nil {
			toCreate = append(toCreate, issue)
		}
	}

	var stateCache *linear.StateCache
	if !dryRun && (len(toCreate) > 0 || (!createOnly && len(toUpdate) > 0)) {
		stateCache, err = linear.BuildStateCache(ctx, client)
		if err != nil {
			return stats, fmt.Errorf("failed to fetch team states: %w", err)
		}
	}

	mappingConfig := loadLinearMappingConfig(ctx)

	// Map to track created/existing Project IDs for linking tasks
	// Map: Beads Epic ID -> Linear Project ID
	epicIDToProjectID := make(map[string]string)

	// Phase 1: Sync Epics (Projects)
	// 1a. Create new Projects
	for _, issue := range epicsToCreate {
		if dryRun {
			stats.Created++
			continue
		}

		projectState := "planned" // default
		switch issue.Status {
		case types.StatusInProgress:
			projectState = "started"
		case types.StatusBlocked:
			projectState = "paused"
		case types.StatusClosed:
			if issue.ClosedAt != nil {
				projectState = "completed"
			} else {
				projectState = "canceled"
			}
		}

		proj, err := client.CreateProject(ctx, issue.Title, issue.Description, projectState)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create project '%s' in Linear: %v\n", issue.Title, err)
			stats.Errors++
			continue
		}

		stats.Created++
		fmt.Printf("  Created Project: %s -> %s\n", issue.ID, proj.Name)
		epicIDToProjectID[issue.ID] = proj.ID

		if updateRefs && proj.URL != "" {
			updates := map[string]interface{}{
				"external_ref": proj.URL,
			}
			if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update external_ref for %s: %v\n", issue.ID, err)
				stats.Errors++
			}
		}
	}

	// 1b. Update existing Projects
	if !createOnly && len(epicsToUpdate) > 0 {
		// Need to know the Project ID for existing epics to update them.
		// Fetch all projects to map URL -> ID.
		var projects []linear.Project
		var err error
		if !dryRun {
			projects, err = client.FetchProjects(ctx, "all")
			if err != nil {
				return stats, fmt.Errorf("failed to fetch projects for update mapping: %w", err)
			}
		}

		projectURLToID := make(map[string]string)
		for _, p := range projects {
			projectURLToID[p.URL] = p.ID
		}

		for _, issue := range epicsToUpdate {
			if dryRun {
				stats.Updated++
				continue
			}

			projectID, ok := projectURLToID[*issue.ExternalRef]
			if !ok {
				// Try to extract ID if URL format allows, or skip
				// For now, warn and skip
				fmt.Fprintf(os.Stderr, "Warning: could not resolve Project ID for %s (ref: %s), skipping update\n",
					issue.ID, *issue.ExternalRef)
				stats.Skipped++
				continue
			}

			// Add to map for task linking (in case we didn't fetch it in 1b setup)
			epicIDToProjectID[issue.ID] = projectID

			// Build project updates (no helper for Local->Project yet, doing manually)
			projectUpdates := map[string]interface{}{
				"name":        issue.Title,
				"description": issue.Description,
			}

			// Map State
			var projectState string
			switch issue.Status {
			case types.StatusOpen:
				projectState = "planned"
			case types.StatusInProgress:
				projectState = "started"
			case types.StatusBlocked:
				projectState = "paused"
			case types.StatusClosed:
				if issue.ClosedAt != nil {
					projectState = "completed"
				} else {
					projectState = "canceled"
				}
			}
			projectUpdates["state"] = projectState

			// We should check if update is needed (timestamps/hash), but for now force update
			// as hash logic for projects isn't fully robust yet.
			// TODO: Add hash check for projects to reduce API calls.

			_, err := client.UpdateProject(ctx, projectID, projectUpdates)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update project '%s' in Linear: %v\n", issue.Title, err)
				stats.Errors++
				continue
			}
			stats.Updated++
			fmt.Printf("  Updated Project: %s -> %s\n", issue.ID, issue.Title)
		}
	} else if !dryRun {
		// Populate epicIDToProjectID even if we're not updating projects, so we can link tasks
		projects, err := client.FetchProjects(ctx, "all")
		if err == nil {
			projectURLToID := make(map[string]string)
			for _, p := range projects {
				projectURLToID[p.URL] = p.ID
			}
			for _, issue := range allIssues {
				if issue.IssueType == types.TypeEpic && issue.ExternalRef != nil {
					if pid, ok := projectURLToID[*issue.ExternalRef]; ok {
						epicIDToProjectID[issue.ID] = pid
					}
				}
			}
		}
	}

	// Phase 2: Sync Tasks/Bugs (Issues)
	// We separate creation from linking to handle dependencies robustly.

	// Track issues processed in this batch to support linking in Phase 3
	// Map: BeadsID -> Linear Issue (with Identifier)
	processedIssues := make(map[string]*linear.Issue)

	// Phase 2a: Create new issues (without parent link)
	for _, issue := range toCreate {
		if dryRun {
			stats.Created++
			continue
		}

		linearPriority := linear.PriorityToLinear(issue.Priority, mappingConfig)
		stateID := stateCache.FindStateForBeadsStatus(issue.Status)
		description := linear.BuildLinearDescription(issue)

		// Resolve Project ID (Epic) - OK to do here as Epics are already processed
		var projectID string
		deps, err := store.GetDependencyRecords(ctx, issue.ID)
		if err == nil {
			for _, dep := range deps {
				if dep.Type == types.DepParentChild {
					parentIssue, err := store.GetIssue(ctx, dep.DependsOnID)
					if err == nil && parentIssue.IssueType == types.TypeEpic {
						if pid, ok := epicIDToProjectID[parentIssue.ID]; ok {
							projectID = pid
						}
					}
				}
			}
		}

		// Create without parentID (will link in Phase 3)
		linearIssue, err := client.CreateIssue(ctx, issue.Title, description, linearPriority, stateID, nil, projectID, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create issue '%s' in Linear: %v\n", issue.Title, err)
			stats.Errors++
			continue
		}

		stats.Created++
		fmt.Printf("  Created: %s -> %s\n", issue.ID, linearIssue.Identifier)
		processedIssues[issue.ID] = linearIssue

		if updateRefs && linearIssue.URL != "" {
			externalRef := linearIssue.URL
			if canonical, ok := linear.CanonicalizeLinearExternalRef(externalRef); ok {
				externalRef = canonical
			}
			updates := map[string]interface{}{
				"external_ref": externalRef,
			}
			// Update local issue so Phase 3 can find the ref
			if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update external_ref for %s: %v\n", issue.ID, err)
				stats.Errors++
			}
			issue.ExternalRef = &externalRef // Update in-memory object too
		}
	}

	// Phase 2b: Update existing issues (content + project link)
	if len(toUpdate) > 0 && !createOnly {
		for _, issue := range toUpdate {
			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			linearIdentifier := linear.ExtractLinearIdentifier(*issue.ExternalRef)
			if linearIdentifier == "" {
				fmt.Fprintf(os.Stderr, "Warning: could not extract Linear identifier from %s: %s\n",
					issue.ID, *issue.ExternalRef)
				stats.Errors++
				continue
			}

			linearIssue, err := client.FetchIssueByIdentifier(ctx, linearIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch Linear issue %s: %v\n",
					linearIdentifier, err)
				stats.Errors++
				continue
			}
			if linearIssue == nil {
				fmt.Fprintf(os.Stderr, "Warning: Linear issue %s not found (may have been deleted)\n",
					linearIdentifier)
				stats.Skipped++
				continue
			}

			processedIssues[issue.ID] = linearIssue

			linearUpdatedAt, err := time.Parse(time.RFC3339, linearIssue.UpdatedAt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse Linear UpdatedAt for %s: %v\n",
					linearIdentifier, err)
				stats.Errors++
				continue
			}

			forcedUpdate := forceUpdateIDs != nil && forceUpdateIDs[issue.ID]
			// We check dependencies for project change, which might not be reflected in UpdatedAt.
			// But for now, we rely on standard freshness check unless forced.
			if !forcedUpdate && !issue.UpdatedAt.After(linearUpdatedAt) {
				// Even if content matches, we might need to link subtasks in Phase 3.
				// So we continue to next issue but still add to processedIssues (done above).
				stats.Skipped++
				continue
			}

			if !forcedUpdate {
				localComparable := linear.NormalizeIssueForLinearHash(issue)
				linearComparable := linear.IssueToBeads(linearIssue, mappingConfig).Issue.(*types.Issue)
				if localComparable.ComputeContentHash() == linearComparable.ComputeContentHash() {
					stats.Skipped++
					continue
				}
			}

			if dryRun {
				stats.Updated++
				continue
			}

			description := linear.BuildLinearDescription(issue)
			updatePayload := map[string]interface{}{
				"title":       issue.Title,
				"description": description,
			}

			linearPriority := linear.PriorityToLinear(issue.Priority, mappingConfig)
			if linearPriority > 0 {
				updatePayload["priority"] = linearPriority
			}

			stateID := stateCache.FindStateForBeadsStatus(issue.Status)
			if stateID != "" {
				updatePayload["stateId"] = stateID
			}

			// Update Project if changed
			var projectID string
			deps, err := store.GetDependencyRecords(ctx, issue.ID)
			if err == nil {
				for _, dep := range deps {
					if dep.Type == types.DepParentChild {
						parentIssue, err := store.GetIssue(ctx, dep.DependsOnID)
						if err == nil && parentIssue.IssueType == types.TypeEpic {
							if pid, ok := epicIDToProjectID[parentIssue.ID]; ok {
								projectID = pid
							}
						}
					}
				}
			}
			if projectID != "" {
				updatePayload["projectId"] = projectID
			} else if linearIssue.Project != nil {
				// If issue has project in Linear but not locally (or dependency removed),
				// we should clear it. Linear API allows null?
				// "projectId": nil
				updatePayload["projectId"] = nil
			}

			updatedLinearIssue, err := client.UpdateIssue(ctx, linearIssue.ID, updatePayload)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update Linear issue %s: %v\n",
					linearIdentifier, err)
				stats.Errors++
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated: %s -> %s\n", issue.ID, updatedLinearIssue.Identifier)
		}
	}

	// Phase 3: Link Subtasks (Parent-Child relationships)
	// Iterate over all processed issues and check if they need to be linked to a parent Task.
	if !dryRun && !createOnly {
		// Identify issues that need parent linking
		// We can check allIssues (that were processed)
		for beadsID, linearIssue := range processedIssues {
			// Find parent dependency
			deps, err := store.GetDependencyRecords(ctx, beadsID)
			if err != nil {
				continue
			}

			var targetParentID string
			for _, dep := range deps {
				if dep.Type == types.DepParentChild {
					// Check if parent is a Task (not Epic)
					parentIssue, err := store.GetIssue(ctx, dep.DependsOnID)
					if err == nil && parentIssue.IssueType != types.TypeEpic {
						// This is a subtask relationship
						// Need parent's Linear ID (UUID)
						// Check if parent is in processedIssues (created/updated in this batch)
						if pLinear, ok := processedIssues[parentIssue.ID]; ok {
							targetParentID = pLinear.ID
						} else if parentIssue.ExternalRef != nil && linear.IsLinearExternalRef(*parentIssue.ExternalRef) {
							// Parent exists but wasn't processed in this batch
							// Need to fetch/resolve its ID.
							// IsLinearExternalRef gives URL. Extract identifier.
							pIdentifier := linear.ExtractLinearIdentifier(*parentIssue.ExternalRef)
							if pIdentifier != "" {
								// Optimization: We could cache these lookups
								pIssue, err := client.FetchIssueByIdentifier(ctx, pIdentifier)
								if err == nil && pIssue != nil {
									targetParentID = pIssue.ID
								}
							}
						}
						break // Only one parent allowed in Linear
					}
				}
			}

			// Check if we need to update
			currentParentID := ""
			if linearIssue.Parent != nil {
				currentParentID = linearIssue.Parent.ID
			}

			if targetParentID != currentParentID {
				if targetParentID == "" && currentParentID == "" {
					continue
				}

				fmt.Printf("  Linking subtask %s to parent %s...\n", linearIssue.Identifier, targetParentID)

				updatePayload := map[string]interface{}{}
				if targetParentID != "" {
					updatePayload["parentId"] = targetParentID
				} else {
					updatePayload["parentId"] = nil
				}

				_, err := client.UpdateIssue(ctx, linearIssue.ID, updatePayload)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to link subtask %s: %v\n", linearIssue.Identifier, err)
					// Don't count as error in stats to avoid double counting if issue was already counted
				}
			}
		}
	}

	if dryRun {
		fmt.Printf("  Would create %d issues in Linear\n", stats.Created)
		if !createOnly {
			fmt.Printf("  Would update %d issues in Linear\n", stats.Updated)
		}
	}

	return stats, nil
}

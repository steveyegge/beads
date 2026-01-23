// Package main provides the bd CLI commands.
package main

// Error Handling Contract:
// - Functions return error for fatal failures that should stop the operation
// - Non-fatal issues (single issue update failure, dependency creation failure)
//   are logged as warnings and operation continues
// - Stats track error counts for reporting

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/steveyegge/beads/internal/gitlab"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// issueIDCounter is used to generate unique issue IDs.
var issueIDCounter uint64

// ConflictStrategy defines how to resolve conflicts between local and GitLab versions.
type ConflictStrategy string

const (
	// ConflictStrategyPreferNewer uses the most recently updated version (default).
	ConflictStrategyPreferNewer ConflictStrategy = "prefer-newer"
	// ConflictStrategyPreferLocal always keeps the local beads version.
	ConflictStrategyPreferLocal ConflictStrategy = "prefer-local"
	// ConflictStrategyPreferGitLab always uses the GitLab version.
	ConflictStrategyPreferGitLab ConflictStrategy = "prefer-gitlab"
)

// getConflictStrategy determines the conflict strategy from flag values.
// Returns error if multiple conflicting flags are set.
func getConflictStrategy(preferLocal, preferGitLab, preferNewer bool) (ConflictStrategy, error) {
	flagsSet := 0
	if preferLocal {
		flagsSet++
	}
	if preferGitLab {
		flagsSet++
	}
	if preferNewer {
		flagsSet++
	}
	if flagsSet > 1 {
		return "", fmt.Errorf("cannot use multiple conflict resolution flags")
	}

	if preferLocal {
		return ConflictStrategyPreferLocal, nil
	}
	if preferGitLab {
		return ConflictStrategyPreferGitLab, nil
	}
	return ConflictStrategyPreferNewer, nil
}

// doPullFromGitLab imports issues from GitLab using the REST API.
// Supports incremental sync by checking gitlab.last_sync config and only fetching
// issues updated since that timestamp.
func doPullFromGitLab(ctx context.Context, client *gitlab.Client, config *gitlab.MappingConfig, dryRun bool, state string, skipGitLabIIDs map[int]bool) (*gitlab.PullStats, error) {
	stats := &gitlab.PullStats{}

	// Check for incremental sync
	var gitlabIssues []gitlab.Issue
	var err error

	lastSyncStr := ""
	if store != nil {
		lastSyncStr, _ = store.GetConfig(ctx, "gitlab.last_sync")
	}

	if lastSyncStr != "" {
		lastSync, parseErr := time.Parse(time.RFC3339, lastSyncStr)
		if parseErr != nil {
			fmt.Printf("Warning: invalid gitlab.last_sync timestamp, doing full sync\n")
			gitlabIssues, err = client.FetchIssues(ctx, state)
		} else {
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			gitlabIssues, err = client.FetchIssuesSince(ctx, state, lastSync)
			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		gitlabIssues, err = client.FetchIssues(ctx, state)
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	if err != nil {
		return stats, fmt.Errorf("failed to fetch issues from GitLab: %w", err)
	}

	// Convert GitLab issues to beads issues
	var beadsIssues []*types.Issue
	var allDeps []gitlab.DependencyInfo
	gitlabIIDToBeadsID := make(map[int]string)

	for i := range gitlabIssues {
		// Skip issues if requested
		if skipGitLabIIDs != nil && skipGitLabIIDs[gitlabIssues[i].IID] {
			stats.Skipped++
			continue
		}

		conversion := gitlab.GitLabIssueToBeads(&gitlabIssues[i], config)
		issue := conversion.Issue.(*types.Issue)
		beadsIssues = append(beadsIssues, issue)
		allDeps = append(allDeps, conversion.Dependencies...)
	}

	if len(beadsIssues) == 0 {
		fmt.Println("  No issues to import")
		return stats, nil
	}

	if dryRun {
		if stats.Incremental {
			fmt.Printf("  Would import %d issues from GitLab (incremental since %s)\n",
				len(beadsIssues), stats.SyncedSince)
		} else {
			fmt.Printf("  Would import %d issues from GitLab (full sync)\n", len(beadsIssues))
		}
		return stats, nil
	}

	// Get issue prefix from config
	prefix := "bd"
	if store != nil {
		if p, err := store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
			prefix = p
		}
	}

	// Generate IDs for new issues
	for _, issue := range beadsIssues {
		if issue.ID == "" {
			issue.ID = generateIssueID(prefix)
		}
	}

	// Import issues to beads
	if store != nil {
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

		// Build mapping from GitLab IID to beads ID for dependencies
		allBeadsIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err == nil {
			for _, issue := range allBeadsIssues {
				if issue.SourceSystem != "" && strings.HasPrefix(issue.SourceSystem, "gitlab:") {
					_, iid, ok := parseGitLabSourceSystem(issue.SourceSystem)
					if ok {
						gitlabIIDToBeadsID[iid] = issue.ID
					}
				}
			}
		}

		// Create dependencies
		depsCreated := 0
		for _, dep := range allDeps {
			fromID, fromOK := gitlabIIDToBeadsID[dep.FromGitLabIID]
			toID, toOK := gitlabIIDToBeadsID[dep.ToGitLabIID]

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
					fmt.Printf("Warning: failed to create dependency %s -> %s (%s): %v\n",
						fromID, toID, dep.Type, err)
				}
			} else {
				depsCreated++
			}
		}

		if depsCreated > 0 {
			fmt.Printf("  Created %d dependencies\n", depsCreated)
		}

		// Update last sync timestamp
		if err := store.SetConfig(ctx, "gitlab.last_sync", time.Now().UTC().Format(time.RFC3339)); err != nil {
			warning := fmt.Sprintf("failed to save gitlab.last_sync: %v (next sync will be full instead of incremental)", err)
			stats.Warnings = append(stats.Warnings, warning)
			fmt.Printf("Warning: %s\n", warning)
		}
	} else {
		// No store - just count what would be created
		stats.Created = len(beadsIssues)
	}

	return stats, nil
}

// doPushToGitLab pushes local beads issues to GitLab.
// Creates new issues in GitLab for issues without external refs, and updates
// existing issues that have GitLab external refs.
func doPushToGitLab(ctx context.Context, client *gitlab.Client, config *gitlab.MappingConfig, localIssues []*types.Issue, dryRun, createOnly bool, forceUpdateIDs, skipUpdateIDs map[string]bool) (*gitlab.PushStats, error) {
	stats := &gitlab.PushStats{}

	for _, issue := range localIssues {
		// Check if this is a GitLab-linked issue
		projectID, iid, isGitLab := parseGitLabSourceSystem(issue.SourceSystem)

		if !isGitLab || iid == 0 {
			// New issue - create in GitLab
			if dryRun {
				fmt.Printf("  Would create: %s - %s\n", issue.ID, issue.Title)
				continue
			}

			fields := gitlab.BeadsIssueToGitLabFields(issue, config)
			labels, _ := fields["labels"].([]string)

			created, err := client.CreateIssue(ctx, issue.Title, issue.Description, labels)
			if err != nil {
				stats.Errors++
				fmt.Printf("Error creating issue %s: %v\n", issue.ID, err)
				continue
			}

			// Update local issue with GitLab reference
			if store != nil {
				webURL := created.WebURL
				sourceSystem := fmt.Sprintf("gitlab:%d:%d", created.ProjectID, created.IID)
				updates := map[string]interface{}{
					"external_ref":  webURL,
					"source_system": sourceSystem,
				}
				if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
					fmt.Printf("Warning: failed to update local issue %s with GitLab ref: %v\n", issue.ID, err)
				}
			}

			stats.Created++
			fmt.Printf("  Created GitLab #%d: %s\n", created.IID, issue.Title)
		} else {
			// Existing issue - update in GitLab
			if createOnly {
				stats.Skipped++
				continue
			}

			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			if dryRun {
				fmt.Printf("  Would update: %s - %s (GitLab #%d)\n", issue.ID, issue.Title, iid)
				continue
			}

			// Verify we're updating the right project (only for numeric project IDs)
			// Skip comparison when client uses path-based ID (contains "/") since we can't
			// reliably compare path "group/project" with numeric 789. The API will reject
			// updates to wrong projects anyway.
			if projectID != 0 && !strings.Contains(client.ProjectID, "/") {
				if strconv.Itoa(projectID) != client.ProjectID {
					stats.Skipped++
					continue
				}
			}

			fields := gitlab.BeadsIssueToGitLabFields(issue, config)
			_, err := client.UpdateIssue(ctx, iid, fields)
			if err != nil {
				stats.Errors++
				fmt.Printf("Error updating issue %s: %v\n", issue.ID, err)
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated GitLab #%d: %s\n", iid, issue.Title)
		}
	}

	return stats, nil
}

// detectGitLabConflicts finds issues where both local and GitLab have changes.
// Returns conflicts where both sides have been modified since last sync.
func detectGitLabConflicts(ctx context.Context, client *gitlab.Client, localIssues []*types.Issue) ([]gitlab.Conflict, error) {
	var conflicts []gitlab.Conflict

	// Get all GitLab issues
	gitlabIssues, err := client.FetchIssues(ctx, "all")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitLab issues: %w", err)
	}

	// Build map of GitLab IID to issue
	gitlabByIID := make(map[int]*gitlab.Issue)
	for i := range gitlabIssues {
		gitlabByIID[gitlabIssues[i].IID] = &gitlabIssues[i]
	}

	// Check each local issue for conflicts
	for _, local := range localIssues {
		_, iid, isGitLab := parseGitLabSourceSystem(local.SourceSystem)
		if !isGitLab || iid == 0 {
			continue
		}

		gitlabIssue, exists := gitlabByIID[iid]
		if !exists {
			continue
		}

		// Check for conflict: both sides updated since last known state
		// Simple heuristic: if local.UpdatedAt != gitlab.UpdatedAt, it's a potential conflict
		if gitlabIssue.UpdatedAt != nil && !local.UpdatedAt.IsZero() {
			localTime := local.UpdatedAt
			gitlabTime := *gitlabIssue.UpdatedAt

			// If times differ by more than a second, consider it a conflict
			diff := localTime.Sub(gitlabTime)
			if diff < -time.Second || diff > time.Second {
				conflict := gitlab.Conflict{
					IssueID:           local.ID,
					LocalUpdated:      localTime,
					GitLabUpdated:     gitlabTime,
					GitLabExternalRef: gitlabIssue.WebURL,
					GitLabIID:         iid,
					GitLabID:          gitlabIssue.ID,
				}
				conflicts = append(conflicts, conflict)
			}
		}
	}

	return conflicts, nil
}

// generateIssueID creates a unique issue ID with the given prefix.
// Uses atomic counter combined with timestamp and random bytes to ensure uniqueness
// even when called rapidly or after process restart.
func generateIssueID(prefix string) string {
	counter := atomic.AddUint64(&issueIDCounter, 1)
	timestamp := time.Now().UnixNano() / 1000000 // milliseconds
	// Add random bytes to prevent collision on restart
	randBytes := make([]byte, 4)
	rand.Read(randBytes)
	return fmt.Sprintf("%s-%d-%d-%x", prefix, timestamp, counter, randBytes)
}

// parseGitLabSourceSystem parses a source system string like "gitlab:123:42"
// Returns projectID, iid, and ok (whether it's a valid GitLab source).
func parseGitLabSourceSystem(sourceSystem string) (projectID, iid int, ok bool) {
	if !strings.HasPrefix(sourceSystem, "gitlab:") {
		return 0, 0, false
	}

	parts := strings.Split(sourceSystem, ":")
	if len(parts) != 3 {
		return 0, 0, false
	}

	var err error
	projectID, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}

	iid, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, false
	}

	return projectID, iid, true
}

// resolveGitLabConflicts resolves conflicts using the specified strategy.
func resolveGitLabConflicts(ctx context.Context, client *gitlab.Client, config *gitlab.MappingConfig, conflicts []gitlab.Conflict, strategy ConflictStrategy) error {
	for _, conflict := range conflicts {
		var useGitLab bool

		switch strategy {
		case ConflictStrategyPreferLocal:
			// Always keep local version - nothing to do
			useGitLab = false
		case ConflictStrategyPreferGitLab:
			// Always use GitLab version
			useGitLab = true
		case ConflictStrategyPreferNewer:
			// Use whichever is newer
			useGitLab = conflict.GitLabUpdated.After(conflict.LocalUpdated)
		default:
			// Default to prefer-newer
			useGitLab = conflict.GitLabUpdated.After(conflict.LocalUpdated)
		}

		if useGitLab {
			// Fetch and apply GitLab version
			issue, err := client.FetchIssueByIID(ctx, conflict.GitLabIID)
			if err != nil {
				fmt.Printf("Warning: failed to fetch GitLab issue #%d: %v\n", conflict.GitLabIID, err)
				continue
			}

			conversion := gitlab.GitLabIssueToBeads(issue, config)
			beadsIssue := conversion.Issue.(*types.Issue)

			if store != nil {
				updates := map[string]interface{}{
					"title":       beadsIssue.Title,
					"description": beadsIssue.Description,
					"status":      string(beadsIssue.Status),
					"priority":    beadsIssue.Priority,
					"issue_type":  string(beadsIssue.IssueType),
					"assignee":    beadsIssue.Assignee,
				}
				if err := store.UpdateIssue(ctx, conflict.IssueID, updates, actor); err != nil {
					fmt.Printf("Warning: failed to update local issue %s: %v\n", conflict.IssueID, err)
				}
			}
		}
		// If not useGitLab, local version is kept (no action needed)
	}

	return nil
}

// =============================================================================
// SyncContext - Thread-safe context for GitLab sync operations (P0 Fix)
// =============================================================================

// SyncContext holds all state needed for GitLab sync operations.
// Using this instead of global variables prevents race conditions when
// multiple sync operations run concurrently.
type SyncContext struct {
	store          storage.Storage
	actor          string
	dbPath         string
	issueIDCounter uint64
}

// NewSyncContext creates a new SyncContext instance.
func NewSyncContext() *SyncContext {
	return &SyncContext{}
}

// SetStore sets the storage backend for this sync context.
func (sc *SyncContext) SetStore(s storage.Storage) {
	sc.store = s
}

// Store returns the storage backend.
func (sc *SyncContext) Store() storage.Storage {
	return sc.store
}

// SetActor sets the actor (user) for audit trails.
func (sc *SyncContext) SetActor(a string) {
	sc.actor = a
}

// Actor returns the actor for audit trails.
func (sc *SyncContext) Actor() string {
	return sc.actor
}

// SetDBPath sets the database path.
func (sc *SyncContext) SetDBPath(path string) {
	sc.dbPath = path
}

// DBPath returns the database path.
func (sc *SyncContext) DBPath() string {
	return sc.dbPath
}

// globalContextIDCounter provides cross-context uniqueness for issue ID generation.
// Each SyncContext has its own issueIDCounter, but we need cross-context uniqueness
// when multiple contexts generate IDs at the same timestamp.
var globalContextIDCounter uint64

// GenerateIssueID creates a unique issue ID with the given prefix.
// Uses an atomic counter combined with timestamp and global counter to ensure uniqueness
// even when multiple SyncContext instances generate IDs at the same time.
func (sc *SyncContext) GenerateIssueID(prefix string) string {
	localCounter := atomic.AddUint64(&sc.issueIDCounter, 1)
	globalCounter := atomic.AddUint64(&globalContextIDCounter, 1)
	timestamp := time.Now().UnixNano() / 1000000 // milliseconds
	return fmt.Sprintf("%s-%d-%d-%d", prefix, timestamp, globalCounter, localCounter)
}

// =============================================================================
// WithContext variants of sync functions (P0 Fix)
// These functions use SyncContext instead of global variables.
// =============================================================================

// doPullFromGitLabWithContext imports issues from GitLab using SyncContext.
func doPullFromGitLabWithContext(ctx context.Context, syncCtx *SyncContext, client *gitlab.Client, config *gitlab.MappingConfig, dryRun bool, state string, skipGitLabIIDs map[int]bool) (*gitlab.PullStats, error) {
	stats := &gitlab.PullStats{}

	// Check for incremental sync
	var gitlabIssues []gitlab.Issue
	var err error

	lastSyncStr := ""
	if syncCtx.store != nil {
		lastSyncStr, _ = syncCtx.store.GetConfig(ctx, "gitlab.last_sync")
	}

	if lastSyncStr != "" {
		lastSync, parseErr := time.Parse(time.RFC3339, lastSyncStr)
		if parseErr != nil {
			fmt.Printf("Warning: invalid gitlab.last_sync timestamp, doing full sync\n")
			gitlabIssues, err = client.FetchIssues(ctx, state)
		} else {
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			gitlabIssues, err = client.FetchIssuesSince(ctx, state, lastSync)
			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		gitlabIssues, err = client.FetchIssues(ctx, state)
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	if err != nil {
		return stats, fmt.Errorf("failed to fetch issues from GitLab: %w", err)
	}

	// Convert GitLab issues to beads issues
	var beadsIssues []*types.Issue
	var allDeps []gitlab.DependencyInfo
	gitlabIIDToBeadsID := make(map[int]string)

	for i := range gitlabIssues {
		// Skip issues if requested
		if skipGitLabIIDs != nil && skipGitLabIIDs[gitlabIssues[i].IID] {
			stats.Skipped++
			continue
		}

		conversion := gitlab.GitLabIssueToBeads(&gitlabIssues[i], config)
		issue := conversion.Issue.(*types.Issue)
		beadsIssues = append(beadsIssues, issue)
		allDeps = append(allDeps, conversion.Dependencies...)
	}

	if len(beadsIssues) == 0 {
		fmt.Println("  No issues to import")
		return stats, nil
	}

	if dryRun {
		if stats.Incremental {
			fmt.Printf("  Would import %d issues from GitLab (incremental since %s)\n",
				len(beadsIssues), stats.SyncedSince)
		} else {
			fmt.Printf("  Would import %d issues from GitLab (full sync)\n", len(beadsIssues))
		}
		return stats, nil
	}

	// Get issue prefix from config
	prefix := "bd"
	if syncCtx.store != nil {
		if p, err := syncCtx.store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
			prefix = p
		}
	}

	// Generate IDs for new issues using SyncContext
	for _, issue := range beadsIssues {
		if issue.ID == "" {
			issue.ID = syncCtx.GenerateIssueID(prefix)
		}
	}

	// Import issues to beads
	if syncCtx.store != nil {
		opts := ImportOptions{
			DryRun:     dryRun,
			SkipUpdate: false,
		}

		result, err := importIssuesCore(ctx, syncCtx.dbPath, syncCtx.store, beadsIssues, opts)
		if err != nil {
			return stats, fmt.Errorf("import failed: %w", err)
		}

		stats.Created = result.Created
		stats.Updated = result.Updated

		// Build mapping from GitLab IID to beads ID for dependencies
		allBeadsIssues, err := syncCtx.store.SearchIssues(ctx, "", types.IssueFilter{})
		if err == nil {
			for _, issue := range allBeadsIssues {
				if issue.SourceSystem != "" && strings.HasPrefix(issue.SourceSystem, "gitlab:") {
					_, iid, ok := parseGitLabSourceSystem(issue.SourceSystem)
					if ok {
						gitlabIIDToBeadsID[iid] = issue.ID
					}
				}
			}
		}

		// Create dependencies
		depsCreated := 0
		for _, dep := range allDeps {
			fromID, fromOK := gitlabIIDToBeadsID[dep.FromGitLabIID]
			toID, toOK := gitlabIIDToBeadsID[dep.ToGitLabIID]

			if !fromOK || !toOK {
				continue
			}

			dependency := &types.Dependency{
				IssueID:     fromID,
				DependsOnID: toID,
				Type:        types.DependencyType(dep.Type),
				CreatedAt:   time.Now(),
			}
			err := syncCtx.store.AddDependency(ctx, dependency, syncCtx.actor)
			if err != nil {
				if !strings.Contains(err.Error(), "already exists") &&
					!strings.Contains(err.Error(), "duplicate") {
					fmt.Printf("Warning: failed to create dependency %s -> %s (%s): %v\n",
						fromID, toID, dep.Type, err)
				}
			} else {
				depsCreated++
			}
		}

		if depsCreated > 0 {
			fmt.Printf("  Created %d dependencies\n", depsCreated)
		}

		// Update last sync timestamp
		if err := syncCtx.store.SetConfig(ctx, "gitlab.last_sync", time.Now().UTC().Format(time.RFC3339)); err != nil {
			warning := fmt.Sprintf("failed to save gitlab.last_sync: %v (next sync will be full instead of incremental)", err)
			stats.Warnings = append(stats.Warnings, warning)
			fmt.Printf("Warning: %s\n", warning)
		}
	} else {
		// No store - just count what would be created
		stats.Created = len(beadsIssues)
	}

	return stats, nil
}

// doPushToGitLabWithContext pushes local beads issues to GitLab using SyncContext.
func doPushToGitLabWithContext(ctx context.Context, syncCtx *SyncContext, client *gitlab.Client, config *gitlab.MappingConfig, localIssues []*types.Issue, dryRun, createOnly bool, forceUpdateIDs, skipUpdateIDs map[string]bool) (*gitlab.PushStats, error) {
	stats := &gitlab.PushStats{}

	for _, issue := range localIssues {
		// Check if this is a GitLab-linked issue
		projectID, iid, isGitLab := parseGitLabSourceSystem(issue.SourceSystem)

		if !isGitLab || iid == 0 {
			// New issue - create in GitLab
			if dryRun {
				fmt.Printf("  Would create: %s - %s\n", issue.ID, issue.Title)
				continue
			}

			fields := gitlab.BeadsIssueToGitLabFields(issue, config)
			labels, _ := fields["labels"].([]string)

			created, err := client.CreateIssue(ctx, issue.Title, issue.Description, labels)
			if err != nil {
				stats.Errors++
				fmt.Printf("Error creating issue %s: %v\n", issue.ID, err)
				continue
			}

			// Update local issue with GitLab reference
			if syncCtx.store != nil {
				webURL := created.WebURL
				sourceSystem := fmt.Sprintf("gitlab:%d:%d", created.ProjectID, created.IID)
				updates := map[string]interface{}{
					"external_ref":  webURL,
					"source_system": sourceSystem,
				}
				if err := syncCtx.store.UpdateIssue(ctx, issue.ID, updates, syncCtx.actor); err != nil {
					fmt.Printf("Warning: failed to update local issue %s with GitLab ref: %v\n", issue.ID, err)
				}
			}

			stats.Created++
			fmt.Printf("  Created GitLab #%d: %s\n", created.IID, issue.Title)
		} else {
			// Existing issue - update in GitLab
			if createOnly {
				stats.Skipped++
				continue
			}

			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			if dryRun {
				fmt.Printf("  Would update: %s - %s (GitLab #%d)\n", issue.ID, issue.Title, iid)
				continue
			}

			// Verify we're updating the right project (only for numeric project IDs)
			if projectID != 0 && !strings.Contains(client.ProjectID, "/") {
				if strconv.Itoa(projectID) != client.ProjectID {
					stats.Skipped++
					continue
				}
			}

			fields := gitlab.BeadsIssueToGitLabFields(issue, config)
			_, err := client.UpdateIssue(ctx, iid, fields)
			if err != nil {
				stats.Errors++
				fmt.Printf("Error updating issue %s: %v\n", issue.ID, err)
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated GitLab #%d: %s\n", iid, issue.Title)
		}
	}

	return stats, nil
}

// detectGitLabConflictsWithContext finds conflicts using SyncContext.
func detectGitLabConflictsWithContext(ctx context.Context, syncCtx *SyncContext, client *gitlab.Client, localIssues []*types.Issue) ([]gitlab.Conflict, error) {
	var conflicts []gitlab.Conflict

	// Get all GitLab issues
	gitlabIssues, err := client.FetchIssues(ctx, "all")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitLab issues: %w", err)
	}

	// Build map of GitLab IID to issue
	gitlabByIID := make(map[int]*gitlab.Issue)
	for i := range gitlabIssues {
		gitlabByIID[gitlabIssues[i].IID] = &gitlabIssues[i]
	}

	// Check each local issue for conflicts
	for _, local := range localIssues {
		_, iid, isGitLab := parseGitLabSourceSystem(local.SourceSystem)
		if !isGitLab || iid == 0 {
			continue
		}

		gitlabIssue, exists := gitlabByIID[iid]
		if !exists {
			continue
		}

		// Check for conflict: both sides updated since last known state
		if gitlabIssue.UpdatedAt != nil && !local.UpdatedAt.IsZero() {
			localTime := local.UpdatedAt
			gitlabTime := *gitlabIssue.UpdatedAt

			// If times differ by more than a second, consider it a conflict
			diff := localTime.Sub(gitlabTime)
			if diff < -time.Second || diff > time.Second {
				conflict := gitlab.Conflict{
					IssueID:           local.ID,
					LocalUpdated:      localTime,
					GitLabUpdated:     gitlabTime,
					GitLabExternalRef: gitlabIssue.WebURL,
					GitLabIID:         iid,
					GitLabID:          gitlabIssue.ID,
				}
				conflicts = append(conflicts, conflict)
			}
		}
	}

	return conflicts, nil
}

// resolveGitLabConflictsWithContext resolves conflicts using SyncContext.
func resolveGitLabConflictsWithContext(ctx context.Context, syncCtx *SyncContext, client *gitlab.Client, config *gitlab.MappingConfig, conflicts []gitlab.Conflict, strategy ConflictStrategy) error {
	for _, conflict := range conflicts {
		var useGitLab bool

		switch strategy {
		case ConflictStrategyPreferLocal:
			useGitLab = false
		case ConflictStrategyPreferGitLab:
			useGitLab = true
		case ConflictStrategyPreferNewer:
			useGitLab = conflict.GitLabUpdated.After(conflict.LocalUpdated)
		default:
			useGitLab = conflict.GitLabUpdated.After(conflict.LocalUpdated)
		}

		if useGitLab {
			// Fetch and apply GitLab version
			issue, err := client.FetchIssueByIID(ctx, conflict.GitLabIID)
			if err != nil {
				fmt.Printf("Warning: failed to fetch GitLab issue #%d: %v\n", conflict.GitLabIID, err)
				continue
			}

			conversion := gitlab.GitLabIssueToBeads(issue, config)
			beadsIssue := conversion.Issue.(*types.Issue)

			if syncCtx.store != nil {
				updates := map[string]interface{}{
					"title":       beadsIssue.Title,
					"description": beadsIssue.Description,
					"status":      string(beadsIssue.Status),
					"priority":    beadsIssue.Priority,
					"issue_type":  string(beadsIssue.IssueType),
					"assignee":    beadsIssue.Assignee,
				}
				if err := syncCtx.store.UpdateIssue(ctx, conflict.IssueID, updates, syncCtx.actor); err != nil {
					fmt.Printf("Warning: failed to update local issue %s: %v\n", conflict.IssueID, err)
				}
			}
		}
	}

	return nil
}

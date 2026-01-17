package tracker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// SyncOptions configures sync behavior.
type SyncOptions struct {
	// Pull imports issues from the external tracker
	Pull bool
	// Push exports issues to the external tracker
	Push bool
	// DryRun previews sync without making changes
	DryRun bool
	// CreateOnly only creates new issues, doesn't update existing
	CreateOnly bool
	// UpdateRefs updates external_ref after creating issues
	UpdateRefs bool
	// State filters issues: "open", "closed", or "all"
	State string
	// ConflictResolution specifies how to handle conflicts
	ConflictResolution ConflictResolution
}

// SyncEngine orchestrates synchronization between Beads and an external tracker.
// It handles both pull (import) and push (export) operations, including
// conflict detection and resolution.
type SyncEngine struct {
	// Tracker is the external tracker plugin
	Tracker IssueTracker

	// Config provides access to tracker configuration
	Config *Config

	// Store provides access to the Beads storage layer
	Store SyncStore

	// Actor is the actor name for audit logging
	Actor string

	// Callbacks for UI feedback (optional)
	OnMessage func(msg string)
	OnWarning func(msg string)
}

// SyncStore defines the storage operations needed by the sync engine.
type SyncStore interface {
	// Issue operations
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error)
	SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error)
	CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
	UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error

	// Dependency operations
	AddDependency(ctx context.Context, dep *types.Dependency, actor string) error

	// Config operations
	GetConfig(ctx context.Context, key string) (string, error)
	SetConfig(ctx context.Context, key, value string) error
}

// NewSyncEngine creates a new sync engine for the given tracker.
func NewSyncEngine(tracker IssueTracker, config *Config, store SyncStore, actor string) *SyncEngine {
	return &SyncEngine{
		Tracker: tracker,
		Config:  config,
		Store:   store,
		Actor:   actor,
	}
}

// Sync performs a sync operation with the given options.
func (e *SyncEngine) Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	result := &SyncResult{Success: true}

	// Default to bidirectional sync
	if !opts.Pull && !opts.Push {
		opts.Pull = true
		opts.Push = true
	}

	var forceUpdateIDs map[string]bool
	var skipUpdateIDs map[string]bool
	var prePullSkipExternalIDs map[string]bool
	var prePullConflicts []Conflict

	// Pre-pull conflict detection for --prefer-local or --prefer-external
	if opts.Pull && (opts.ConflictResolution == ConflictResolutionLocal ||
		opts.ConflictResolution == ConflictResolutionExternal) {
		conflicts, err := e.DetectConflicts(ctx)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("conflict detection failed: %v", err))
		} else if len(conflicts) > 0 {
			prePullConflicts = conflicts
			if opts.ConflictResolution == ConflictResolutionLocal {
				prePullSkipExternalIDs = make(map[string]bool, len(conflicts))
				forceUpdateIDs = make(map[string]bool, len(conflicts))
				for _, conflict := range conflicts {
					prePullSkipExternalIDs[conflict.ExternalID] = true
					forceUpdateIDs[conflict.IssueID] = true
				}
			} else if opts.ConflictResolution == ConflictResolutionExternal {
				skipUpdateIDs = make(map[string]bool, len(conflicts))
				for _, conflict := range conflicts {
					skipUpdateIDs[conflict.IssueID] = true
				}
			}
		}
	}

	// Step 1: Pull from tracker
	if opts.Pull {
		e.log(ctx, opts.DryRun, "Pulling issues from %s...", e.Tracker.DisplayName())

		pullStats, err := e.doPull(ctx, opts, prePullSkipExternalIDs)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, err
		}

		result.Stats.Pulled = pullStats.Created + pullStats.Updated
		result.Stats.Created += pullStats.Created
		result.Stats.Updated += pullStats.Updated
		result.Stats.Skipped += pullStats.Skipped

		if !opts.DryRun {
			e.log(ctx, false, "Pulled %d issues (%d created, %d updated)",
				result.Stats.Pulled, pullStats.Created, pullStats.Updated)
		}
	}

	// Step 2: Handle conflicts (if bidirectional)
	if opts.Pull && opts.Push {
		conflicts := prePullConflicts
		var err error
		if conflicts == nil {
			conflicts, err = e.DetectConflicts(ctx)
		}
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("conflict detection failed: %v", err))
		} else if len(conflicts) > 0 {
			result.Stats.Conflicts = len(conflicts)

			if opts.DryRun {
				e.logConflictsDryRun(ctx, opts, conflicts, &forceUpdateIDs, &skipUpdateIDs)
			} else {
				err := e.resolveConflicts(ctx, opts, conflicts, &forceUpdateIDs, &skipUpdateIDs, prePullConflicts)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("conflict resolution failed: %v", err))
				}
			}
		}
	}

	// Step 3: Push to tracker
	if opts.Push {
		e.log(ctx, opts.DryRun, "Pushing issues to %s...", e.Tracker.DisplayName())

		pushStats, err := e.doPush(ctx, opts, forceUpdateIDs, skipUpdateIDs)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, err
		}

		result.Stats.Pushed = pushStats.Created + pushStats.Updated
		result.Stats.Created += pushStats.Created
		result.Stats.Updated += pushStats.Updated
		result.Stats.Skipped += pushStats.Skipped
		result.Stats.Errors += pushStats.Errors

		if !opts.DryRun {
			e.log(ctx, false, "Pushed %d issues (%d created, %d updated)",
				result.Stats.Pushed, pushStats.Created, pushStats.Updated)
		}
	}

	// Update last sync timestamp
	if !opts.DryRun && result.Success {
		result.LastSync = time.Now().Format(time.RFC3339)
		key := e.Config.Prefix + ".last_sync"
		if err := e.Store.SetConfig(ctx, key, result.LastSync); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to update last_sync: %v", err))
		}
	}

	return result, nil
}

// DetectConflicts finds issues that have been modified both locally and in the tracker.
func (e *SyncEngine) DetectConflicts(ctx context.Context) ([]Conflict, error) {
	// Get last sync time
	key := e.Config.Prefix + ".last_sync"
	lastSyncStr, _ := e.Store.GetConfig(ctx, key)
	if lastSyncStr == "" {
		return nil, nil // No previous sync - no conflicts possible
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp: %w", err)
	}

	// Get all local issues with external refs for this tracker
	allIssues, err := e.Store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	var conflicts []Conflict
	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !e.Tracker.IsExternalRef(*issue.ExternalRef) {
			continue
		}

		// Check if local issue was updated since last sync
		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		// Local was updated - now check if tracker was also updated
		externalID := e.Tracker.ExtractIdentifier(*issue.ExternalRef)
		if externalID == "" {
			// Can't extract ID - treat as potential conflict
			conflicts = append(conflicts, Conflict{
				IssueID:     issue.ID,
				LocalUpdated: issue.UpdatedAt,
				ExternalRef: *issue.ExternalRef,
			})
			continue
		}

		// Fetch tracker issue to get its timestamp
		trackerIssue, err := e.Tracker.FetchIssue(ctx, externalID)
		if err != nil {
			e.warn("couldn't fetch %s %s for conflict check: %v",
				e.Tracker.DisplayName(), externalID, err)
			conflicts = append(conflicts, Conflict{
				IssueID:     issue.ID,
				LocalUpdated: issue.UpdatedAt,
				ExternalRef: *issue.ExternalRef,
				ExternalID:  externalID,
			})
			continue
		}
		if trackerIssue == nil {
			continue // Issue was deleted in tracker
		}

		// Only a conflict if tracker was ALSO updated since last sync
		if trackerIssue.UpdatedAt.After(lastSync) {
			conflicts = append(conflicts, Conflict{
				IssueID:           issue.ID,
				LocalUpdated:      issue.UpdatedAt,
				ExternalUpdated:   trackerIssue.UpdatedAt,
				ExternalRef:       *issue.ExternalRef,
				ExternalID:        externalID,
				ExternalInternalID: trackerIssue.ID,
			})
		}
	}

	return conflicts, nil
}

// doPull imports issues from the external tracker.
func (e *SyncEngine) doPull(ctx context.Context, opts SyncOptions, skipExternalIDs map[string]bool) (*PullStats, error) {
	stats := &PullStats{}

	// Build fetch options
	fetchOpts := FetchOptions{
		State: opts.State,
	}

	// Check for incremental sync
	key := e.Config.Prefix + ".last_sync"
	lastSyncStr, _ := e.Store.GetConfig(ctx, key)
	if lastSyncStr != "" {
		lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
		if err == nil {
			fetchOpts.Since = &lastSync
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			if !opts.DryRun {
				e.log(ctx, false, "Incremental sync since %s", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	}

	// Fetch issues from tracker
	trackerIssues, err := e.Tracker.FetchIssues(ctx, fetchOpts)
	if err != nil {
		return stats, fmt.Errorf("failed to fetch issues from %s: %w", e.Tracker.DisplayName(), err)
	}

	if len(trackerIssues) == 0 {
		e.log(ctx, opts.DryRun, "No issues to import")
		return stats, nil
	}

	// Filter out skipped issues
	if len(skipExternalIDs) > 0 {
		var filtered []TrackerIssue
		for _, ti := range trackerIssues {
			if skipExternalIDs[ti.Identifier] {
				stats.Skipped++
				continue
			}
			filtered = append(filtered, ti)
		}
		trackerIssues = filtered
	}

	if opts.DryRun {
		e.log(ctx, true, "Would import %d issues from %s", len(trackerIssues), e.Tracker.DisplayName())
		return stats, nil
	}

	// Convert and import issues
	mapper := e.Tracker.FieldMapper()
	var allDeps []DependencyInfo
	externalIDToBeadsID := make(map[string]string)

	for _, ti := range trackerIssues {
		conversion := mapper.IssueToBeads(&ti)
		issue := conversion.Issue.(*types.Issue)
		allDeps = append(allDeps, conversion.Dependencies...)

		// Check if issue already exists
		var existing *types.Issue
		if ti.URL != "" {
			existing, _ = e.Store.GetIssueByExternalRef(ctx, ti.URL)
		}

		if existing != nil {
			// Update existing issue
			updates := e.buildUpdatesFromConversion(issue)
			if err := e.Store.UpdateIssue(ctx, existing.ID, updates, e.Actor); err != nil {
				e.warn("failed to update issue %s: %v", existing.ID, err)
				continue
			}
			stats.Updated++
			externalIDToBeadsID[ti.Identifier] = existing.ID
		} else {
			// Create new issue
			if err := e.Store.CreateIssue(ctx, issue, e.Actor); err != nil {
				e.warn("failed to create issue: %v", err)
				continue
			}
			stats.Created++
			externalIDToBeadsID[ti.Identifier] = issue.ID
		}
	}

	// Create dependencies
	e.createDependencies(ctx, allDeps, externalIDToBeadsID)

	return stats, nil
}

// doPush exports issues to the external tracker.
func (e *SyncEngine) doPush(ctx context.Context, opts SyncOptions, forceUpdateIDs, skipUpdateIDs map[string]bool) (*PushStats, error) {
	stats := &PushStats{}

	// Get all local issues
	allIssues, err := e.Store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return stats, fmt.Errorf("failed to get local issues: %w", err)
	}

	var toCreate []*types.Issue
	var toUpdate []*types.Issue

	for _, issue := range allIssues {
		if issue.IsTombstone() {
			continue
		}

		if issue.ExternalRef != nil && e.Tracker.IsExternalRef(*issue.ExternalRef) {
			if !opts.CreateOnly {
				toUpdate = append(toUpdate, issue)
			}
		} else if issue.ExternalRef == nil {
			toCreate = append(toCreate, issue)
		}
	}

	// Create new issues
	for _, issue := range toCreate {
		if opts.DryRun {
			stats.Created++
			continue
		}

		trackerIssue, err := e.Tracker.CreateIssue(ctx, issue)
		if err != nil {
			e.warn("failed to create issue '%s' in %s: %v", issue.Title, e.Tracker.DisplayName(), err)
			stats.Errors++
			continue
		}

		stats.Created++
		e.log(ctx, false, "Created: %s -> %s", issue.ID, trackerIssue.Identifier)

		// Update external_ref
		if opts.UpdateRefs && trackerIssue.URL != "" {
			externalRef := e.Tracker.CanonicalizeRef(trackerIssue.URL)
			if externalRef == "" {
				externalRef = trackerIssue.URL
			}
			updates := map[string]interface{}{
				"external_ref": externalRef,
			}
			if err := e.Store.UpdateIssue(ctx, issue.ID, updates, e.Actor); err != nil {
				e.warn("failed to update external_ref for %s: %v", issue.ID, err)
				stats.Errors++
			}
		}
	}

	// Update existing issues
	if !opts.CreateOnly && len(toUpdate) > 0 {
		for _, issue := range toUpdate {
			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			externalID := e.Tracker.ExtractIdentifier(*issue.ExternalRef)
			if externalID == "" {
				e.warn("could not extract identifier from %s: %s", issue.ID, *issue.ExternalRef)
				stats.Errors++
				continue
			}

			// Fetch current tracker version to check if update needed
			trackerIssue, err := e.Tracker.FetchIssue(ctx, externalID)
			if err != nil {
				e.warn("failed to fetch %s issue %s: %v", e.Tracker.DisplayName(), externalID, err)
				stats.Errors++
				continue
			}
			if trackerIssue == nil {
				e.warn("%s issue %s not found (may have been deleted)", e.Tracker.DisplayName(), externalID)
				stats.Skipped++
				continue
			}

			// Skip if tracker is newer (unless forced)
			forcedUpdate := forceUpdateIDs != nil && forceUpdateIDs[issue.ID]
			if !forcedUpdate && !issue.UpdatedAt.After(trackerIssue.UpdatedAt) {
				stats.Skipped++
				continue
			}

			if opts.DryRun {
				stats.Updated++
				continue
			}

			// Update in tracker
			updatedIssue, err := e.Tracker.UpdateIssue(ctx, trackerIssue.ID, issue)
			if err != nil {
				e.warn("failed to update %s issue %s: %v", e.Tracker.DisplayName(), externalID, err)
				stats.Errors++
				continue
			}

			stats.Updated++
			e.log(ctx, false, "Updated: %s -> %s", issue.ID, updatedIssue.Identifier)
		}
	}

	if opts.DryRun {
		e.log(ctx, true, "Would create %d issues in %s", stats.Created, e.Tracker.DisplayName())
		if !opts.CreateOnly {
			e.log(ctx, true, "Would update %d issues in %s", stats.Updated, e.Tracker.DisplayName())
		}
	}

	return stats, nil
}

// resolveConflicts handles conflict resolution during sync.
func (e *SyncEngine) resolveConflicts(ctx context.Context, opts SyncOptions, conflicts []Conflict,
	forceUpdateIDs, skipUpdateIDs *map[string]bool, prePullConflicts []Conflict) error {

	switch opts.ConflictResolution {
	case ConflictResolutionLocal:
		e.log(ctx, false, "Resolving %d conflicts (preferring local)", len(conflicts))
		if *forceUpdateIDs == nil {
			*forceUpdateIDs = make(map[string]bool, len(conflicts))
			for _, conflict := range conflicts {
				(*forceUpdateIDs)[conflict.IssueID] = true
			}
		}

	case ConflictResolutionExternal:
		e.log(ctx, false, "Resolving %d conflicts (preferring %s)", len(conflicts), e.Tracker.DisplayName())
		if *skipUpdateIDs == nil {
			*skipUpdateIDs = make(map[string]bool, len(conflicts))
			for _, conflict := range conflicts {
				(*skipUpdateIDs)[conflict.IssueID] = true
			}
		}
		// Re-import conflicts if we haven't already
		if prePullConflicts == nil {
			if err := e.reimportConflicts(ctx, conflicts); err != nil {
				return err
			}
		}

	default: // Timestamp-based
		e.log(ctx, false, "Resolving %d conflicts (newer wins)", len(conflicts))
		return e.resolveConflictsByTimestamp(ctx, conflicts, forceUpdateIDs, skipUpdateIDs)
	}

	return nil
}

// resolveConflictsByTimestamp resolves conflicts by keeping the newer version.
func (e *SyncEngine) resolveConflictsByTimestamp(ctx context.Context, conflicts []Conflict,
	forceUpdateIDs, skipUpdateIDs *map[string]bool) error {

	var externalWins []Conflict
	var localWins []Conflict

	for _, c := range conflicts {
		if c.ExternalUpdated.After(c.LocalUpdated) {
			externalWins = append(externalWins, c)
		} else {
			localWins = append(localWins, c)
		}
	}

	if len(externalWins) > 0 {
		e.log(ctx, false, "%d conflict(s): %s is newer, will re-import", len(externalWins), e.Tracker.DisplayName())
	}
	if len(localWins) > 0 {
		e.log(ctx, false, "%d conflict(s): Local is newer, will push", len(localWins))
	}

	// Re-import tracker-wins
	if len(externalWins) > 0 {
		if err := e.reimportConflicts(ctx, externalWins); err != nil {
			return fmt.Errorf("failed to re-import %s-wins conflicts: %w", e.Tracker.DisplayName(), err)
		}
		if *skipUpdateIDs == nil {
			*skipUpdateIDs = make(map[string]bool)
		}
		for _, c := range externalWins {
			(*skipUpdateIDs)[c.IssueID] = true
		}
	}

	// Mark local-wins for forced update
	if len(localWins) > 0 {
		if *forceUpdateIDs == nil {
			*forceUpdateIDs = make(map[string]bool)
		}
		for _, c := range localWins {
			(*forceUpdateIDs)[c.IssueID] = true
		}
	}

	return nil
}

// reimportConflicts re-imports conflicting issues from the tracker.
func (e *SyncEngine) reimportConflicts(ctx context.Context, conflicts []Conflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	mapper := e.Tracker.FieldMapper()
	resolved := 0
	failed := 0

	for _, conflict := range conflicts {
		trackerIssue, err := e.Tracker.FetchIssue(ctx, conflict.ExternalID)
		if err != nil {
			e.warn("failed to fetch %s for resolution: %v", conflict.ExternalID, err)
			failed++
			continue
		}
		if trackerIssue == nil {
			e.warn("%s issue %s not found, skipping", e.Tracker.DisplayName(), conflict.ExternalID)
			failed++
			continue
		}

		conversion := mapper.IssueToBeads(trackerIssue)
		issue := conversion.Issue.(*types.Issue)
		updates := e.buildUpdatesFromConversion(issue)

		if err := e.Store.UpdateIssue(ctx, conflict.IssueID, updates, e.Actor); err != nil {
			e.warn("failed to update local issue %s: %v", conflict.IssueID, err)
			failed++
			continue
		}

		e.log(ctx, false, "Resolved: %s <- %s (%s wins)", conflict.IssueID, conflict.ExternalID, e.Tracker.DisplayName())
		resolved++
	}

	if failed > 0 {
		return fmt.Errorf("%d conflict(s) failed to resolve", failed)
	}

	e.log(ctx, false, "Resolved %d conflict(s) by keeping %s version", resolved, e.Tracker.DisplayName())
	return nil
}

// logConflictsDryRun logs conflict resolution actions for dry run mode.
func (e *SyncEngine) logConflictsDryRun(ctx context.Context, opts SyncOptions, conflicts []Conflict,
	forceUpdateIDs, skipUpdateIDs *map[string]bool) {

	switch opts.ConflictResolution {
	case ConflictResolutionLocal:
		e.log(ctx, true, "Would resolve %d conflicts (preferring local)", len(conflicts))
		*forceUpdateIDs = make(map[string]bool, len(conflicts))
		for _, c := range conflicts {
			(*forceUpdateIDs)[c.IssueID] = true
		}

	case ConflictResolutionExternal:
		e.log(ctx, true, "Would resolve %d conflicts (preferring %s)", len(conflicts), e.Tracker.DisplayName())
		*skipUpdateIDs = make(map[string]bool, len(conflicts))
		for _, c := range conflicts {
			(*skipUpdateIDs)[c.IssueID] = true
		}

	default:
		e.log(ctx, true, "Would resolve %d conflicts (newer wins)", len(conflicts))
		var externalWins, localWins []Conflict
		for _, c := range conflicts {
			if c.ExternalUpdated.After(c.LocalUpdated) {
				externalWins = append(externalWins, c)
			} else {
				localWins = append(localWins, c)
			}
		}
		if len(localWins) > 0 {
			*forceUpdateIDs = make(map[string]bool, len(localWins))
			for _, c := range localWins {
				(*forceUpdateIDs)[c.IssueID] = true
			}
		}
		if len(externalWins) > 0 {
			*skipUpdateIDs = make(map[string]bool, len(externalWins))
			for _, c := range externalWins {
				(*skipUpdateIDs)[c.IssueID] = true
			}
		}
	}
}

// buildUpdatesFromConversion builds an updates map from a converted issue.
func (e *SyncEngine) buildUpdatesFromConversion(issue *types.Issue) map[string]interface{} {
	updates := map[string]interface{}{
		"title":       issue.Title,
		"description": issue.Description,
		"priority":    issue.Priority,
		"status":      string(issue.Status),
	}

	if issue.Assignee != "" {
		updates["assignee"] = issue.Assignee
	}
	if len(issue.Labels) > 0 {
		updates["labels"] = issue.Labels
	}
	if !issue.UpdatedAt.IsZero() {
		updates["updated_at"] = issue.UpdatedAt
	}
	if issue.ClosedAt != nil {
		updates["closed_at"] = *issue.ClosedAt
	}

	return updates
}

// createDependencies creates dependencies from import conversion.
func (e *SyncEngine) createDependencies(ctx context.Context, deps []DependencyInfo, idMap map[string]string) {
	created := 0
	for _, dep := range deps {
		fromID, fromOK := idMap[dep.FromExternalID]
		toID, toOK := idMap[dep.ToExternalID]

		if !fromOK || !toOK {
			continue
		}

		dependency := &types.Dependency{
			IssueID:     fromID,
			DependsOnID: toID,
			Type:        types.DependencyType(dep.Type),
			CreatedAt:   time.Now(),
		}
		err := e.Store.AddDependency(ctx, dependency, e.Actor)
		if err != nil {
			// Ignore duplicate errors
			if !strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "duplicate") {
				e.warn("failed to create dependency %s -> %s (%s): %v",
					fromID, toID, dep.Type, err)
			}
		} else {
			created++
		}
	}

	if created > 0 {
		e.log(ctx, false, "Created %d dependencies from %s relations", created, e.Tracker.DisplayName())
	}
}

// log sends a message to the OnMessage callback or does nothing.
func (e *SyncEngine) log(ctx context.Context, dryRun bool, format string, args ...interface{}) {
	if e.OnMessage != nil {
		prefix := ""
		if dryRun {
			prefix = "[DRY RUN] "
		}
		e.OnMessage(prefix + fmt.Sprintf(format, args...))
	}
}

// warn sends a warning to the OnWarning callback or does nothing.
func (e *SyncEngine) warn(format string, args ...interface{}) {
	if e.OnWarning != nil {
		e.OnWarning(fmt.Sprintf("Warning: "+format, args...))
	}
}

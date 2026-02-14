package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// Engine orchestrates synchronization between beads and an external tracker.
// It implements the shared Pull→Detect→Resolve→Push pattern that all tracker
// integrations follow, eliminating duplication between Linear, GitLab, etc.
type Engine struct {
	Tracker IssueTracker
	Store   storage.Storage
	Actor   string

	// Callbacks for UI feedback (optional).
	OnMessage func(msg string)
	OnWarning func(msg string)
}

// NewEngine creates a new sync engine for the given tracker and storage.
func NewEngine(tracker IssueTracker, store storage.Storage, actor string) *Engine {
	return &Engine{
		Tracker: tracker,
		Store:   store,
		Actor:   actor,
	}
}

// Sync performs a complete synchronization operation based on the given options.
func (e *Engine) Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	result := &SyncResult{Success: true}
	now := time.Now().UTC()

	// Default to bidirectional if neither specified
	if !opts.Pull && !opts.Push {
		opts.Pull = true
		opts.Push = true
	}

	// Track IDs to skip/force during push based on conflict resolution
	skipPushIDs := make(map[string]bool)
	forcePushIDs := make(map[string]bool)

	// Phase 1: Pull
	if opts.Pull {
		pullStats, err := e.doPull(ctx, opts)
		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("pull failed: %v", err)
			return result, err
		}
		result.Stats.Pulled = pullStats.Created + pullStats.Updated
		result.Stats.Created += pullStats.Created
		result.Stats.Updated += pullStats.Updated
		result.Stats.Skipped += pullStats.Skipped
	}

	// Phase 2: Detect conflicts (only for bidirectional sync)
	if opts.Pull && opts.Push {
		conflicts, err := e.DetectConflicts(ctx)
		if err != nil {
			e.warn("Failed to detect conflicts: %v", err)
		} else if len(conflicts) > 0 {
			result.Stats.Conflicts = len(conflicts)
			e.resolveConflicts(ctx, opts, conflicts, skipPushIDs, forcePushIDs)
		}
	}

	// Phase 3: Push
	if opts.Push {
		pushStats, err := e.doPush(ctx, opts, skipPushIDs, forcePushIDs)
		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("push failed: %v", err)
			return result, err
		}
		result.Stats.Pushed = pushStats.Created + pushStats.Updated
		result.Stats.Created += pushStats.Created
		result.Stats.Updated += pushStats.Updated
		result.Stats.Skipped += pushStats.Skipped
		result.Stats.Errors += pushStats.Errors
	}

	// Update last_sync timestamp
	if !opts.DryRun {
		lastSync := now.Format(time.RFC3339)
		key := e.Tracker.ConfigPrefix() + ".last_sync"
		if err := e.Store.SetConfig(ctx, key, lastSync); err != nil {
			e.warn("Failed to update last_sync: %v", err)
		}
		result.LastSync = lastSync
	}

	return result, nil
}

// DetectConflicts identifies issues that were modified both locally and externally
// since the last sync.
func (e *Engine) DetectConflicts(ctx context.Context) ([]Conflict, error) {
	// Get last sync time
	key := e.Tracker.ConfigPrefix() + ".last_sync"
	lastSyncStr, err := e.Store.GetConfig(ctx, key)
	if err != nil || lastSyncStr == "" {
		return nil, nil // No previous sync, no conflicts possible
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp %q: %w", lastSyncStr, err)
	}

	// Find local issues with external refs for this tracker
	filter := types.IssueFilter{}
	issues, err := e.Store.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("searching issues: %w", err)
	}

	var conflicts []Conflict
	for _, issue := range issues {
		extRef := derefStr(issue.ExternalRef)
		if extRef == "" || !e.Tracker.IsExternalRef(extRef) {
			continue
		}

		// Check if locally modified since last sync
		if issue.UpdatedAt.Before(lastSync) || issue.UpdatedAt.Equal(lastSync) {
			continue
		}

		// Fetch external version to check if also modified
		extID := e.Tracker.ExtractIdentifier(extRef)
		if extID == "" {
			continue
		}

		extIssue, err := e.Tracker.FetchIssue(ctx, extID)
		if err != nil || extIssue == nil {
			continue
		}

		if extIssue.UpdatedAt.After(lastSync) {
			conflicts = append(conflicts, Conflict{
				IssueID:            issue.ID,
				LocalUpdated:       issue.UpdatedAt,
				ExternalUpdated:    extIssue.UpdatedAt,
				ExternalRef:        extRef,
				ExternalIdentifier: extIssue.Identifier,
				ExternalInternalID: extIssue.ID,
			})
		}
	}

	return conflicts, nil
}

// doPull imports issues from the external tracker into beads.
func (e *Engine) doPull(ctx context.Context, opts SyncOptions) (*PullStats, error) {
	stats := &PullStats{}

	// Determine if incremental sync is possible
	fetchOpts := FetchOptions{State: opts.State}
	key := e.Tracker.ConfigPrefix() + ".last_sync"
	if lastSyncStr, err := e.Store.GetConfig(ctx, key); err == nil && lastSyncStr != "" {
		if t, err := time.Parse(time.RFC3339, lastSyncStr); err == nil {
			fetchOpts.Since = &t
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
		}
	}

	// Fetch issues from external tracker
	extIssues, err := e.Tracker.FetchIssues(ctx, fetchOpts)
	if err != nil {
		return nil, fmt.Errorf("fetching issues: %w", err)
	}

	e.msg("Fetched %d issues from %s", len(extIssues), e.Tracker.DisplayName())

	mapper := e.Tracker.FieldMapper()
	var pendingDeps []DependencyInfo

	for _, extIssue := range extIssues {
		if opts.DryRun {
			e.msg("[dry-run] Would import: %s - %s", extIssue.Identifier, extIssue.Title)
			stats.Created++
			continue
		}

		// Check if we already have this issue
		ref := e.Tracker.BuildExternalRef(&extIssue)
		existing, _ := e.Store.GetIssueByExternalRef(ctx, ref)

		conv := mapper.IssueToBeads(&extIssue)
		if conv == nil || conv.Issue == nil {
			stats.Skipped++
			continue
		}

		if existing != nil {
			// Update existing issue
			updates := make(map[string]interface{})
			updates["title"] = conv.Issue.Title
			updates["description"] = conv.Issue.Description
			updates["priority"] = conv.Issue.Priority
			updates["status"] = string(conv.Issue.Status)

			// Preserve metadata from tracker
			if extIssue.Metadata != nil {
				if raw, err := json.Marshal(extIssue.Metadata); err == nil {
					updates["metadata"] = json.RawMessage(raw)
				}
			}

			if err := e.Store.UpdateIssue(ctx, existing.ID, updates, e.Actor); err != nil {
				e.warn("Failed to update %s: %v", existing.ID, err)
				continue
			}
			stats.Updated++
		} else {
			// Create new issue
			conv.Issue.ExternalRef = strPtr(ref)
			if extIssue.Metadata != nil {
				if raw, err := json.Marshal(extIssue.Metadata); err == nil {
					conv.Issue.Metadata = json.RawMessage(raw)
				}
			}
			if err := e.Store.CreateIssue(ctx, conv.Issue, e.Actor); err != nil {
				e.warn("Failed to create issue for %s: %v", extIssue.Identifier, err)
				continue
			}
			stats.Created++
		}

		pendingDeps = append(pendingDeps, conv.Dependencies...)
	}

	// Create dependencies after all issues are imported
	e.createDependencies(ctx, pendingDeps)

	return stats, nil
}

// doPush exports beads issues to the external tracker.
func (e *Engine) doPush(ctx context.Context, opts SyncOptions, skipIDs, forceIDs map[string]bool) (*PushStats, error) {
	stats := &PushStats{}

	// Fetch local issues
	filter := types.IssueFilter{}
	issues, err := e.Store.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("searching local issues: %w", err)
	}

	for _, issue := range issues {
		// Skip filtered types
		if !e.shouldPushIssue(issue, opts) {
			stats.Skipped++
			continue
		}

		// Skip conflict-excluded issues
		if skipIDs[issue.ID] {
			stats.Skipped++
			continue
		}

		extRef := derefStr(issue.ExternalRef)

		if opts.DryRun {
			if extRef == "" {
				e.msg("[dry-run] Would create in %s: %s", e.Tracker.DisplayName(), issue.Title)
				stats.Created++
			} else {
				e.msg("[dry-run] Would update in %s: %s", e.Tracker.DisplayName(), issue.Title)
				stats.Updated++
			}
			continue
		}

		if extRef == "" || !e.Tracker.IsExternalRef(extRef) {
			// Create in external tracker
			created, err := e.Tracker.CreateIssue(ctx, issue)
			if err != nil {
				e.warn("Failed to create %s in %s: %v", issue.ID, e.Tracker.DisplayName(), err)
				stats.Errors++
				continue
			}

			// Update local issue with external ref
			ref := e.Tracker.BuildExternalRef(created)
			updates := map[string]interface{}{"external_ref": ref}
			if err := e.Store.UpdateIssue(ctx, issue.ID, updates, e.Actor); err != nil {
				e.warn("Failed to update external_ref for %s: %v", issue.ID, err)
			}
			stats.Created++
		} else if !opts.CreateOnly || forceIDs[issue.ID] {
			// Update existing external issue
			extID := e.Tracker.ExtractIdentifier(extRef)
			if extID == "" {
				stats.Skipped++
				continue
			}

			// Fetch external version to check if update needed
			if !forceIDs[issue.ID] {
				extIssue, err := e.Tracker.FetchIssue(ctx, extID)
				if err == nil && extIssue != nil && !extIssue.UpdatedAt.Before(issue.UpdatedAt) {
					stats.Skipped++ // External is same or newer
					continue
				}
			}

			if _, err := e.Tracker.UpdateIssue(ctx, extID, issue); err != nil {
				e.warn("Failed to update %s in %s: %v", issue.ID, e.Tracker.DisplayName(), err)
				stats.Errors++
				continue
			}
			stats.Updated++
		} else {
			stats.Skipped++
		}
	}

	return stats, nil
}

// resolveConflicts applies the configured conflict resolution strategy.
func (e *Engine) resolveConflicts(ctx context.Context, opts SyncOptions, conflicts []Conflict, skipIDs, forceIDs map[string]bool) {
	for _, c := range conflicts {
		switch opts.ConflictResolution {
		case ConflictLocal:
			forceIDs[c.IssueID] = true
			e.msg("Conflict on %s: keeping local version", c.IssueID)

		case ConflictExternal:
			skipIDs[c.IssueID] = true
			e.reimportIssue(ctx, c)
			e.msg("Conflict on %s: keeping external version", c.IssueID)

		default: // ConflictTimestamp or unset
			if c.LocalUpdated.After(c.ExternalUpdated) {
				forceIDs[c.IssueID] = true
				e.msg("Conflict on %s: local is newer, pushing", c.IssueID)
			} else {
				skipIDs[c.IssueID] = true
				e.reimportIssue(ctx, c)
				e.msg("Conflict on %s: external is newer, importing", c.IssueID)
			}
		}
	}
}

// reimportIssue fetches the external version and updates the local issue.
func (e *Engine) reimportIssue(ctx context.Context, c Conflict) {
	extIssue, err := e.Tracker.FetchIssue(ctx, c.ExternalIdentifier)
	if err != nil || extIssue == nil {
		e.warn("Failed to re-import %s: %v", c.IssueID, err)
		return
	}

	conv := e.Tracker.FieldMapper().IssueToBeads(extIssue)
	if conv == nil || conv.Issue == nil {
		return
	}

	updates := map[string]interface{}{
		"title":       conv.Issue.Title,
		"description": conv.Issue.Description,
		"priority":    conv.Issue.Priority,
		"status":      string(conv.Issue.Status),
	}
	if extIssue.Metadata != nil {
		if raw, err := json.Marshal(extIssue.Metadata); err == nil {
			updates["metadata"] = json.RawMessage(raw)
		}
	}

	if err := e.Store.UpdateIssue(ctx, c.IssueID, updates, e.Actor); err != nil {
		e.warn("Failed to update %s during reimport: %v", c.IssueID, err)
	}
}

// createDependencies creates dependencies from the pending list, matching
// external IDs to local issue IDs.
func (e *Engine) createDependencies(ctx context.Context, deps []DependencyInfo) {
	if len(deps) == 0 {
		return
	}

	for _, dep := range deps {
		fromIssue, _ := e.Store.GetIssueByExternalRef(ctx, dep.FromExternalID)
		toIssue, _ := e.Store.GetIssueByExternalRef(ctx, dep.ToExternalID)

		if fromIssue == nil || toIssue == nil {
			continue
		}

		d := &types.Dependency{
			IssueID:     fromIssue.ID,
			DependsOnID: toIssue.ID,
			Type:        types.DependencyType(dep.Type),
		}
		if err := e.Store.AddDependency(ctx, d, e.Actor); err != nil {
			e.warn("Failed to create dependency %s -> %s: %v", fromIssue.ID, toIssue.ID, err)
		}
	}
}

// shouldPushIssue checks if an issue should be included in push based on filters.
func (e *Engine) shouldPushIssue(issue *types.Issue, opts SyncOptions) bool {
	if len(opts.TypeFilter) > 0 {
		found := false
		for _, t := range opts.TypeFilter {
			if issue.IssueType == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, t := range opts.ExcludeTypes {
		if issue.IssueType == t {
			return false
		}
	}

	if opts.State == "open" && issue.Status == types.StatusClosed {
		return false
	}

	return true
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string { return &s }

// derefStr safely dereferences a *string, returning "" for nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (e *Engine) msg(format string, args ...interface{}) {
	if e.OnMessage != nil {
		e.OnMessage(fmt.Sprintf(format, args...))
	}
}

func (e *Engine) warn(format string, args ...interface{}) {
	if e.OnWarning != nil {
		e.OnWarning(fmt.Sprintf(format, args...))
	}
}

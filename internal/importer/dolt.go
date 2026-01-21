// Dolt backend support for import operations
package importer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// importIssuesDolt handles import for Dolt backend
func importIssuesDolt(ctx context.Context, store *dolt.DoltStore, issues []*types.Issue, opts Options, result *Result) (*Result, error) {
	if opts.DryRun {
		// For dry run, just count what would happen
		existingIDs := make(map[string]bool)
		existing, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
		if err != nil {
			return nil, fmt.Errorf("failed to get existing issues: %w", err)
		}
		for _, e := range existing {
			existingIDs[e.ID] = true
		}

		for _, issue := range issues {
			if existingIDs[issue.ID] {
				result.Updated++
			} else {
				result.Created++
			}
		}
		return result, nil
	}

	// Get existing issues for comparison
	existingMap := make(map[string]*types.Issue)
	existing, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
	if err != nil {
		return nil, fmt.Errorf("failed to get existing issues: %w", err)
	}
	for _, e := range existing {
		existingMap[e.ID] = e
	}

	// Process issues in batches
	batchSize := 100
	for i := 0; i < len(issues); i += batchSize {
		end := i + batchSize
		if end > len(issues) {
			end = len(issues)
		}
		batch := issues[i:end]

		var toCreate []*types.Issue
		var toUpdate []*types.Issue

		for _, issue := range batch {
			if existingIssue, exists := existingMap[issue.ID]; exists {
				// Check if update is needed
				if issue.ContentHash != existingIssue.ContentHash {
					toUpdate = append(toUpdate, issue)
				} else {
					result.Unchanged++
				}
			} else {
				toCreate = append(toCreate, issue)
			}
		}

		// Create new issues
		if len(toCreate) > 0 {
			batchOpts := dolt.BatchCreateOptions{
				SkipValidation:    true,
				PreserveDates:     true,
				SkipDirtyTracking: true,
				SkipPrefixCheck:   opts.SkipPrefixValidation,
			}
			if err := store.CreateIssuesWithFullOptions(ctx, toCreate, "import", batchOpts); err != nil {
				return nil, fmt.Errorf("failed to create issues: %w", err)
			}
			result.Created += len(toCreate)
		}

		// Update existing issues
		for _, issue := range toUpdate {
			if opts.SkipUpdate {
				result.Skipped++
				continue
			}

			updates := map[string]interface{}{
				"title":       issue.Title,
				"description": issue.Description,
				"status":      issue.Status,
				"priority":    issue.Priority,
				"issue_type":  issue.IssueType,
				"assignee":    issue.Assignee,
				"notes":       issue.Notes,
				"design":      issue.Design,
			}
			if issue.ClosedAt != nil {
				updates["closed_at"] = issue.ClosedAt
			}
			if issue.CloseReason != "" {
				updates["close_reason"] = issue.CloseReason
			}

			if err := store.UpdateIssue(ctx, issue.ID, updates, "import"); err != nil {
				if opts.Strict {
					return nil, fmt.Errorf("failed to update issue %s: %w", issue.ID, err)
				}
				// Log warning but continue
				fmt.Printf("Warning: failed to update issue %s: %v\n", issue.ID, err)
				continue
			}
			result.Updated++

			// Sync labels
			if err := syncLabels(ctx, store, issue); err != nil && opts.Strict {
				return nil, fmt.Errorf("failed to sync labels for %s: %w", issue.ID, err)
			}

			// Sync dependencies
			if err := syncDependencies(ctx, store, issue); err != nil && opts.Strict {
				return nil, fmt.Errorf("failed to sync dependencies for %s: %w", issue.ID, err)
			}
		}
	}

	return result, nil
}

// syncLabels syncs labels between the incoming issue and the store
func syncLabels(ctx context.Context, store *dolt.DoltStore, issue *types.Issue) error {
	currentLabels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		return err
	}

	currentSet := make(map[string]bool)
	for _, l := range currentLabels {
		currentSet[l] = true
	}

	incomingSet := make(map[string]bool)
	for _, l := range issue.Labels {
		incomingSet[l] = true
	}

	// Add missing labels
	for _, label := range issue.Labels {
		if !currentSet[label] {
			if err := store.AddLabel(ctx, issue.ID, label, "import"); err != nil {
				if !strings.Contains(err.Error(), "Duplicate") {
					return err
				}
			}
		}
	}

	// Remove extra labels
	for _, label := range currentLabels {
		if !incomingSet[label] {
			if err := store.RemoveLabel(ctx, issue.ID, label, "import"); err != nil {
				return err
			}
		}
	}

	return nil
}

// syncDependencies syncs dependencies between the incoming issue and the store
func syncDependencies(ctx context.Context, store *dolt.DoltStore, issue *types.Issue) error {
	currentDeps, err := store.GetDependencyRecords(ctx, issue.ID)
	if err != nil {
		return err
	}

	currentSet := make(map[string]bool)
	for _, d := range currentDeps {
		currentSet[d.DependsOnID] = true
	}

	// Add missing dependencies from the issue's Dependencies slice
	if issue.Dependencies != nil {
		for _, dep := range issue.Dependencies {
			if !currentSet[dep.DependsOnID] {
				newDep := &types.Dependency{
					IssueID:     issue.ID,
					DependsOnID: dep.DependsOnID,
					Type:        dep.Type,
					CreatedAt:   time.Now().UTC(),
				}
				if err := store.AddDependency(ctx, newDep, "import"); err != nil {
					if !strings.Contains(err.Error(), "Duplicate") {
						return err
					}
				}
			}
		}
	}

	return nil
}

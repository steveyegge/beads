package jira

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// ConfigGetter provides read access to configuration values.
type ConfigGetter interface {
	GetConfig(ctx context.Context, key string) (string, error)
}

// IssueSearcher provides read access to issues.
type IssueSearcher interface {
	SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error)
}

// DetectConflicts finds issues that have been modified both locally and in Jira
// since the last sync. It fetches each potentially conflicting issue from Jira
// to compare timestamps.
func DetectConflicts(ctx context.Context, client *Client, store interface {
	ConfigGetter
	IssueSearcher
}, stderr io.Writer) ([]Conflict, error) {
	// Get last sync time
	lastSyncStr, _ := store.GetConfig(ctx, "jira.last_sync")
	if lastSyncStr == "" {
		// No previous sync - no conflicts possible
		return nil, nil
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp: %w", err)
	}

	// Get all issues with Jira refs that were updated since last sync
	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	// Get jiraURL for validation
	jiraURL, _ := store.GetConfig(ctx, "jira.url")

	var conflicts []Conflict
	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !IsJiraExternalRef(*issue.ExternalRef, jiraURL) {
			continue
		}

		// Check if local issue was updated since last sync
		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		// Local was updated - now check if Jira was also updated
		jiraKey := ExtractJiraKey(*issue.ExternalRef)
		if jiraKey == "" {
			// Can't extract key - treat as potential conflict for safety
			conflicts = append(conflicts, Conflict{
				IssueID:         issue.ID,
				LocalUpdated:    issue.UpdatedAt,
				JiraExternalRef: *issue.ExternalRef,
			})
			continue
		}

		// Fetch Jira issue timestamp
		jiraUpdated, err := client.FetchIssueTimestamp(ctx, jiraKey)
		if err != nil {
			// Can't fetch from Jira - log warning and treat as potential conflict
			fmt.Fprintf(stderr, "Warning: couldn't fetch Jira issue %s: %v\n", jiraKey, err)
			conflicts = append(conflicts, Conflict{
				IssueID:         issue.ID,
				LocalUpdated:    issue.UpdatedAt,
				JiraExternalRef: *issue.ExternalRef,
			})
			continue
		}

		// Only a conflict if Jira was ALSO updated since last sync
		if jiraUpdated.After(lastSync) {
			conflicts = append(conflicts, Conflict{
				IssueID:         issue.ID,
				LocalUpdated:    issue.UpdatedAt,
				JiraUpdated:     jiraUpdated,
				JiraExternalRef: *issue.ExternalRef,
			})
		}
	}

	return conflicts, nil
}

// ReimportConflicts re-imports conflicting issues from Jira (Jira wins).
// NOTE: Full implementation would fetch the complete Jira issue and update local copy.
// Currently shows detailed conflict info for manual review.
func ReimportConflicts(conflicts []Conflict, stderr io.Writer) error {
	if len(conflicts) == 0 {
		return nil
	}
	fmt.Fprintf(stderr, "Warning: conflict resolution (--prefer-jira) not fully implemented\n")
	fmt.Fprintf(stderr, "  %d issue(s) have conflicts - Jira version would win:\n", len(conflicts))
	for _, c := range conflicts {
		if !c.JiraUpdated.IsZero() {
			fmt.Fprintf(stderr, "    - %s (local: %s, jira: %s)\n",
				c.IssueID,
				c.LocalUpdated.Format(time.RFC3339),
				c.JiraUpdated.Format(time.RFC3339))
		} else {
			fmt.Fprintf(stderr, "    - %s (local: %s, jira: unknown)\n",
				c.IssueID,
				c.LocalUpdated.Format(time.RFC3339))
		}
	}
	return nil
}

// ResolveConflictsByTimestamp resolves conflicts by keeping the newer version.
func ResolveConflictsByTimestamp(conflicts []Conflict, stderr io.Writer) error {
	if len(conflicts) == 0 {
		return nil
	}

	var localWins, jiraWins, unknown int
	for _, c := range conflicts {
		if c.JiraUpdated.IsZero() {
			unknown++
		} else if c.LocalUpdated.After(c.JiraUpdated) {
			localWins++
		} else {
			jiraWins++
		}
	}

	fmt.Fprintf(stderr, "Conflict resolution by timestamp:\n")
	fmt.Fprintf(stderr, "  Local wins (newer): %d\n", localWins)
	fmt.Fprintf(stderr, "  Jira wins (newer):  %d\n", jiraWins)
	if unknown > 0 {
		fmt.Fprintf(stderr, "  Unknown (couldn't fetch): %d\n", unknown)
	}

	// Show details
	for _, c := range conflicts {
		if c.JiraUpdated.IsZero() {
			fmt.Fprintf(stderr, "    - %s: local version kept (couldn't fetch Jira timestamp)\n", c.IssueID)
		} else if c.LocalUpdated.After(c.JiraUpdated) {
			fmt.Fprintf(stderr, "    - %s: local wins (local: %s > jira: %s)\n",
				c.IssueID,
				c.LocalUpdated.Format(time.RFC3339),
				c.JiraUpdated.Format(time.RFC3339))
		} else {
			fmt.Fprintf(stderr, "    - %s: jira wins (jira: %s >= local: %s)\n",
				c.IssueID,
				c.JiraUpdated.Format(time.RFC3339),
				c.LocalUpdated.Format(time.RFC3339))
		}
	}

	// NOTE: Full implementation would actually re-import the Jira version for jiraWins issues
	if jiraWins > 0 {
		fmt.Fprintf(stderr, "Warning: %d issue(s) should be re-imported from Jira (not yet implemented)\n", jiraWins)
	}

	return nil
}

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	gh "github.com/steveyegge/beads/internal/github"
	"github.com/steveyegge/beads/internal/types"
)

// GitHubConflictStrategy defines how to resolve conflicts between local and GitHub versions.
type GitHubConflictStrategy string

const (
	GitHubConflictPreferNewer  GitHubConflictStrategy = "prefer-newer"
	GitHubConflictPreferLocal  GitHubConflictStrategy = "prefer-local"
	GitHubConflictPreferGitHub GitHubConflictStrategy = "prefer-github"
)

// getGitHubConflictStrategy determines the conflict strategy from flag values.
func getGitHubConflictStrategy(preferLocal, preferGitHub, preferNewer bool) (GitHubConflictStrategy, error) {
	flagsSet := 0
	if preferLocal {
		flagsSet++
	}
	if preferGitHub {
		flagsSet++
	}
	if preferNewer {
		flagsSet++
	}
	if flagsSet > 1 {
		return "", fmt.Errorf("cannot use multiple conflict resolution flags")
	}

	if preferLocal {
		return GitHubConflictPreferLocal, nil
	}
	if preferGitHub {
		return GitHubConflictPreferGitHub, nil
	}
	return GitHubConflictPreferNewer, nil
}

// doPullFromGitHubWithContext imports issues from GitHub using SyncContext.
func doPullFromGitHubWithContext(ctx context.Context, syncCtx *SyncContext, client *gh.Client, owner, repo string, config *gh.MappingConfig, dryRun bool, state string, skipGitHubNumbers map[int]bool) (*gh.PullStats, error) {
	stats := &gh.PullStats{}

	var githubIssues []gh.Issue
	var err error

	lastSyncStr := ""
	if syncCtx.store != nil {
		lastSyncStr, _ = syncCtx.store.GetConfig(ctx, "github.last_sync")
	}

	if lastSyncStr != "" {
		lastSync, parseErr := time.Parse(time.RFC3339, lastSyncStr)
		if parseErr != nil {
			fmt.Printf("Warning: invalid github.last_sync timestamp, doing full sync\n")
			githubIssues, err = client.FetchIssues(ctx, state)
		} else {
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			githubIssues, err = client.FetchIssuesSince(ctx, state, lastSync)
			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		githubIssues, err = client.FetchIssues(ctx, state)
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	if err != nil {
		return stats, fmt.Errorf("failed to fetch issues from GitHub: %w", err)
	}

	var beadsIssues []*types.Issue
	githubNumToBeadsID := make(map[int]string)

	for _, ghIssue := range githubIssues {
		if skipGitHubNumbers != nil && skipGitHubNumbers[ghIssue.Number] {
			stats.Skipped++
			continue
		}

		conversion := gh.GitHubIssueToBeads(&ghIssue, owner, repo, config)
		beadsIssue := conversion.Issue

		// Check if this issue already exists in local DB
		if syncCtx.store != nil {
			existingIssues, searchErr := syncCtx.store.SearchIssues(ctx, "", types.IssueFilter{})
			if searchErr == nil {
				for _, existing := range existingIssues {
					if existing.SourceSystem == beadsIssue.SourceSystem {
						beadsIssue.ID = existing.ID
						break
					}
				}
			}
		}

		// Generate ID if new
		if beadsIssue.ID == "" {
			prefix := "bd"
			if syncCtx.store != nil {
				if p, err := syncCtx.store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
					prefix = p
				}
			}
			beadsIssue.ID = syncCtx.GenerateIssueID(prefix)
		}

		beadsIssues = append(beadsIssues, beadsIssue)
		githubNumToBeadsID[ghIssue.Number] = beadsIssue.ID
	}

	if dryRun {
		for _, issue := range beadsIssues {
			fmt.Printf("  Would import: %s - %s\n", issue.ID, issue.Title)
		}
		stats.Created = len(beadsIssues)
		return stats, nil
	}

	// Import issues into store
	if syncCtx.store != nil {
		for _, issue := range beadsIssues {
			existingIssue, getErr := syncCtx.store.GetIssue(ctx, issue.ID)
			if getErr != nil || existingIssue == nil {
				if err := syncCtx.store.CreateIssue(ctx, issue, syncCtx.actor); err != nil {
					if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
						stats.Skipped++
					} else {
						fmt.Printf("Warning: failed to create issue %s: %v\n", issue.ID, err)
						stats.Skipped++
					}
					continue
				}
				stats.Created++
			} else {
				updates := map[string]interface{}{
					"title":         issue.Title,
					"description":   issue.Description,
					"status":        string(issue.Status),
					"priority":      issue.Priority,
					"issue_type":    string(issue.IssueType),
					"assignee":      issue.Assignee,
					"external_ref":  ptrToString(issue.ExternalRef),
					"source_system": issue.SourceSystem,
				}
				if err := syncCtx.store.UpdateIssue(ctx, issue.ID, updates, syncCtx.actor); err != nil {
					fmt.Printf("Warning: failed to update issue %s: %v\n", issue.ID, err)
				} else {
					stats.Updated++
				}
			}
		}

		// Build mapping for existing issues
		allIssues, searchErr := syncCtx.store.SearchIssues(ctx, "", types.IssueFilter{})
		if searchErr == nil {
			for _, issue := range allIssues {
				if issue.SourceSystem != "" && strings.HasPrefix(issue.SourceSystem, "github:") {
					_, _, num, ok := parseGitHubSourceSystem(issue.SourceSystem)
					if ok {
						githubNumToBeadsID[num] = issue.ID
					}
				}
			}
		}

		// Update last sync timestamp
		if err := syncCtx.store.SetConfig(ctx, "github.last_sync", time.Now().UTC().Format(time.RFC3339)); err != nil {
			warning := fmt.Sprintf("failed to save github.last_sync: %v (next sync will be full instead of incremental)", err)
			stats.Warnings = append(stats.Warnings, warning)
			fmt.Printf("Warning: %s\n", warning)
		}
	} else {
		stats.Created = len(beadsIssues)
	}

	return stats, nil
}

// doPushToGitHubWithContext pushes local beads issues to GitHub using SyncContext.
func doPushToGitHubWithContext(ctx context.Context, syncCtx *SyncContext, client *gh.Client, owner, repo string, config *gh.MappingConfig, localIssues []*types.Issue, dryRun, createOnly bool, _ /* forceUpdateIDs */, skipUpdateIDs map[string]bool) (*gh.PushStats, error) {
	stats := &gh.PushStats{}

	for _, issue := range localIssues {
		_, _, number, isGitHub := parseGitHubSourceSystem(issue.SourceSystem)

		if !isGitHub || number == 0 {
			// New issue - create in GitHub
			if dryRun {
				fmt.Printf("  Would create: %s - %s\n", issue.ID, issue.Title)
				continue
			}

			fields := gh.BeadsIssueToGitHubFields(issue, config)
			labels, _ := fields["labels"].([]string)

			created, err := client.CreateIssue(ctx, issue.Title, issue.Description, labels)
			if err != nil {
				stats.Errors++
				fmt.Printf("Error creating issue %s: %v\n", issue.ID, err)
				continue
			}

			// Update local issue with GitHub reference
			if syncCtx.store != nil {
				sourceSystem := fmt.Sprintf("github:%s/%s:%d", owner, repo, created.Number)
				updates := map[string]interface{}{
					"external_ref":  created.HTMLURL,
					"source_system": sourceSystem,
				}
				if err := syncCtx.store.UpdateIssue(ctx, issue.ID, updates, syncCtx.actor); err != nil {
					fmt.Printf("Warning: failed to update local issue %s with GitHub ref: %v\n", issue.ID, err)
				}
			}

			stats.Created++
			fmt.Printf("  Created GitHub #%d: %s\n", created.Number, issue.Title)
		} else {
			// Existing issue - update in GitHub
			if createOnly {
				stats.Skipped++
				continue
			}

			if skipUpdateIDs != nil && skipUpdateIDs[issue.ID] {
				stats.Skipped++
				continue
			}

			if dryRun {
				fmt.Printf("  Would update: %s - %s (GitHub #%d)\n", issue.ID, issue.Title, number)
				continue
			}

			// Verify we're updating the right repo
			srcOwner, srcRepo, _, _ := parseGitHubSourceSystem(issue.SourceSystem)
			if srcOwner != owner || srcRepo != repo {
				stats.Skipped++
				continue
			}

			fields := gh.BeadsIssueToGitHubFields(issue, config)
			_, err := client.UpdateIssue(ctx, number, fields)
			if err != nil {
				stats.Errors++
				fmt.Printf("Error updating issue %s: %v\n", issue.ID, err)
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated GitHub #%d: %s\n", number, issue.Title)
		}
	}

	return stats, nil
}

// detectGitHubConflictsWithContext finds conflicts using SyncContext.
func detectGitHubConflictsWithContext(ctx context.Context, _ *SyncContext, client *gh.Client, localIssues []*types.Issue) ([]gh.Conflict, error) {
	var conflicts []gh.Conflict

	githubIssues, err := client.FetchIssues(ctx, "all")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub issues: %w", err)
	}

	githubByNumber := make(map[int]*gh.Issue)
	for i := range githubIssues {
		githubByNumber[githubIssues[i].Number] = &githubIssues[i]
	}

	for _, local := range localIssues {
		_, _, number, isGitHub := parseGitHubSourceSystem(local.SourceSystem)
		if !isGitHub || number == 0 {
			continue
		}

		githubIssue, exists := githubByNumber[number]
		if !exists {
			continue
		}

		if githubIssue.UpdatedAt != nil && !local.UpdatedAt.IsZero() {
			localTime := local.UpdatedAt
			githubTime := *githubIssue.UpdatedAt

			diff := localTime.Sub(githubTime)
			if diff < -time.Second || diff > time.Second {
				conflict := gh.Conflict{
					IssueID:           local.ID,
					LocalUpdated:      localTime,
					GitHubUpdated:     githubTime,
					GitHubExternalRef: githubIssue.HTMLURL,
					GitHubNumber:      number,
					GitHubID:          githubIssue.ID,
				}
				conflicts = append(conflicts, conflict)
			}
		}
	}

	return conflicts, nil
}

// resolveGitHubConflictsWithContext resolves conflicts using SyncContext.
//
//nolint:unparam // error return kept for API consistency
func resolveGitHubConflictsWithContext(ctx context.Context, syncCtx *SyncContext, client *gh.Client, owner, repo string, config *gh.MappingConfig, conflicts []gh.Conflict, strategy GitHubConflictStrategy) error {
	for _, conflict := range conflicts {
		var useGitHub bool

		switch strategy {
		case GitHubConflictPreferLocal:
			useGitHub = false
		case GitHubConflictPreferGitHub:
			useGitHub = true
		case GitHubConflictPreferNewer:
			useGitHub = conflict.GitHubUpdated.After(conflict.LocalUpdated)
		default:
			useGitHub = conflict.GitHubUpdated.After(conflict.LocalUpdated)
		}

		if useGitHub {
			issue, err := client.FetchIssueByNumber(ctx, conflict.GitHubNumber)
			if err != nil {
				fmt.Printf("Warning: failed to fetch GitHub issue #%d: %v\n", conflict.GitHubNumber, err)
				continue
			}

			conversion := gh.GitHubIssueToBeads(issue, owner, repo, config)
			beadsIssue := conversion.Issue

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

// ptrToString safely dereferences a string pointer.
func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Convenience wrappers using global variables (for tests).

func doPullFromGitHub(ctx context.Context, client *gh.Client, owner, repo string, config *gh.MappingConfig, dryRun bool, state string, skipNumbers map[int]bool) (*gh.PullStats, error) {
	syncCtx := NewSyncContext()
	syncCtx.SetStore(store)
	syncCtx.SetActor(actor)
	syncCtx.SetDBPath(dbPath)
	return doPullFromGitHubWithContext(ctx, syncCtx, client, owner, repo, config, dryRun, state, skipNumbers)
}

func doPushToGitHub(ctx context.Context, client *gh.Client, owner, repo string, config *gh.MappingConfig, localIssues []*types.Issue, dryRun, createOnly bool, forceUpdateIDs, skipUpdateIDs map[string]bool) (*gh.PushStats, error) {
	syncCtx := NewSyncContext()
	syncCtx.SetStore(store)
	syncCtx.SetActor(actor)
	syncCtx.SetDBPath(dbPath)
	return doPushToGitHubWithContext(ctx, syncCtx, client, owner, repo, config, localIssues, dryRun, createOnly, forceUpdateIDs, skipUpdateIDs)
}

func detectGitHubConflicts(ctx context.Context, client *gh.Client, localIssues []*types.Issue) ([]gh.Conflict, error) {
	syncCtx := NewSyncContext()
	syncCtx.SetStore(store)
	syncCtx.SetActor(actor)
	syncCtx.SetDBPath(dbPath)
	return detectGitHubConflictsWithContext(ctx, syncCtx, client, localIssues)
}



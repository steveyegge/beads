package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// showIssueChildren displays only the children of the specified issue(s)
func showIssueChildren(ctx context.Context, args []string, resolvedIDs []string, routedArgs []string, jsonOut bool, shortMode bool) {
	// Collect all children for all issues
	allChildren := make(map[string][]*types.IssueWithDependencyMetadata)

	// Process each issue to get its children
	processIssue := func(issueID string, issueStore storage.Storage) error {
		// Initialize entry so "no children" message can be shown
		if _, exists := allChildren[issueID]; !exists {
			allChildren[issueID] = []*types.IssueWithDependencyMetadata{}
		}

		// Get all dependents with metadata so we can filter for children
		refs, err := issueStore.GetDependentsWithMetadata(ctx, issueID)
		if err != nil {
			return err
		}
		// Filter for only parent-child relationships
		for _, ref := range refs {
			if ref.DependencyType == types.DepParentChild {
				allChildren[issueID] = append(allChildren[issueID], ref)
			}
		}
		return nil
	}

	// Handle routed IDs via direct mode
	for _, id := range routedArgs {
		result, err := resolveAndGetIssueWithRouting(ctx, store, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
			continue
		}
		if result == nil || result.Issue == nil {
			if result != nil {
				result.Close()
			}
			fmt.Fprintf(os.Stderr, "Issue %s not found\n", id)
			continue
		}
		if err := processIssue(result.ResolvedID, result.Store); err != nil {
			fmt.Fprintf(os.Stderr, "Error getting children for %s: %v\n", id, err)
		}
		result.Close()
	}

	// Handle resolved IDs (daemon mode)
	if daemonClient != nil {
		for _, id := range resolvedIDs {
			// Need to open direct connection for GetDependentsWithMetadata
			// Use factory to respect backend configuration (bd-m2jr: SQLite fallback fix)
			dbStore, err := factory.NewFromConfig(ctx, filepath.Dir(dbPath))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
				continue
			}
			if err := processIssue(id, dbStore); err != nil {
				fmt.Fprintf(os.Stderr, "Error getting children for %s: %v\n", id, err)
			}
			_ = dbStore.Close()
		}
	} else {
		// Direct mode - process each arg
		for _, id := range args {
			if containsStr(routedArgs, id) {
				continue // Already processed above
			}
			result, err := resolveAndGetIssueWithRouting(ctx, store, id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
				continue
			}
			if result == nil || result.Issue == nil {
				if result != nil {
					result.Close()
				}
				fmt.Fprintf(os.Stderr, "Issue %s not found\n", id)
				continue
			}
			if err := processIssue(result.ResolvedID, result.Store); err != nil {
				fmt.Fprintf(os.Stderr, "Error getting children for %s: %v\n", id, err)
			}
			result.Close()
		}
	}

	// Output results
	if jsonOut {
		outputJSON(allChildren)
		return
	}

	// Display children
	for issueID, children := range allChildren {
		if len(children) == 0 {
			fmt.Printf("%s: No children found\n", ui.RenderAccent(issueID))
			continue
		}

		fmt.Printf("%s Children of %s (%d):\n", ui.RenderAccent("↳"), issueID, len(children))
		for _, child := range children {
			if shortMode {
				fmt.Printf("  %s\n", formatShortIssue(&child.Issue))
			} else {
				fmt.Println(formatDependencyLine("↳", child))
			}
		}
		fmt.Println()
	}
}

// containsStr checks if a string slice contains a value
func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// showIssueAsOf displays issues as they existed at a specific commit or branch ref.
// This requires a versioned storage backend (e.g., Dolt).
func showIssueAsOf(ctx context.Context, args []string, ref string, shortMode bool) {
	// Check if storage supports versioning
	vs, ok := storage.AsVersioned(store)
	if !ok {
		FatalErrorRespectJSON("--as-of requires Dolt backend (current backend does not support versioning)")
	}

	var allIssues []*types.Issue
	for idx, id := range args {
		issue, err := vs.AsOf(ctx, id, ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching %s as of %s: %v\n", id, ref, err)
			continue
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Issue %s did not exist at %s\n", id, ref)
			continue
		}

		if shortMode {
			fmt.Println(formatShortIssue(issue))
			continue
		}

		if jsonOutput {
			allIssues = append(allIssues, issue)
			continue
		}

		if idx > 0 {
			fmt.Println("\n" + ui.RenderMuted(strings.Repeat("-", 60)))
		}

		// Display header with ref indicator
		fmt.Printf("\n%s (as of %s)\n", formatIssueHeader(issue), ui.RenderMuted(ref))
		fmt.Println(formatIssueMetadata(issue))

		if issue.Description != "" {
			fmt.Printf("\n%s\n%s\n", ui.RenderBold("DESCRIPTION"), ui.RenderMarkdown(issue.Description))
		}
		fmt.Println()
	}

	if jsonOutput && len(allIssues) > 0 {
		outputJSON(allIssues)
	}
}

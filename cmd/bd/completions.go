package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// issueIDCompletion provides shell completion for issue IDs by querying the storage
// and returning a list of IDs with their titles as descriptions
func issueIDCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Initialize storage if not already initialized
	ctx := context.Background()
	if rootCtx != nil {
		ctx = rootCtx
	}

	// Get database path - use same logic as in PersistentPreRun
	currentDBPath := dbPath
	if currentDBPath == "" {
		// Try to find database path
		foundDB := beads.FindDatabasePath()
		if foundDB != "" {
			currentDBPath = foundDB
		} else {
			// Default path
			currentDBPath = filepath.Join(".beads", beads.CanonicalDatabaseName)
		}
	}

	// Open database if store is not initialized
	currentStore := store
	if currentStore == nil {
		var err error
		timeout := 30 * time.Second
		if lockTimeout > 0 {
			timeout = lockTimeout
		}
		currentStore, err = sqlite.NewReadOnlyWithTimeout(ctx, currentDBPath, timeout)
		if err != nil {
			// If we can't open database, return empty completion
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		defer currentStore.Close()
	}

	// Use SearchIssues with empty query and default filter to get all issues
	filter := types.IssueFilter{}
	issues, err := currentStore.SearchIssues(ctx, "", filter)
	if err != nil {
		// If we can't list issues, return empty completion
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Build completion list
	completions := make([]string, 0, len(issues))
	for _, issue := range issues {
		// Filter based on what's already typed
		if toComplete != "" && !strings.HasPrefix(issue.ID, toComplete) {
			continue
		}

		// Format: ID\tTitle (shown during completion)
		completions = append(completions, fmt.Sprintf("%s\t%s", issue.ID, issue.Title))
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

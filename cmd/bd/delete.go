package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <issue-id> [issue-id...]",
	Short: "Delete one or more issues and clean up references",
	Long: `Delete one or more issues and clean up all references to them.

This command will:
1. Remove all dependency links (any type, both directions) involving the issues
2. Update text references to "[deleted:ID]" in directly connected issues
3. Delete the issues from the database

This is a destructive operation that cannot be undone. Use with caution.

BATCH DELETION:

Delete multiple issues at once:
  bd delete bd-1 bd-2 bd-3 --force

Delete from file (one ID per line):
  bd delete --from-file deletions.txt --force

Preview before deleting:
  bd delete --from-file deletions.txt --dry-run

DEPENDENCY HANDLING:

Default: Fails if any issue has dependents not in deletion set
  bd delete bd-1 bd-2

Cascade: Recursively delete all dependents
  bd delete bd-1 --cascade --force

Force: Delete and orphan dependents
  bd delete bd-1 --force`,
	Args: cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		fromFile, _ := cmd.Flags().GetString("from-file")
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		cascade, _ := cmd.Flags().GetBool("cascade")

		// Collect issue IDs from args and/or file
		issueIDs := make([]string, 0, len(args))
		issueIDs = append(issueIDs, args...)

		if fromFile != "" {
			fileIDs, err := readIssueIDsFromFile(fromFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
				os.Exit(1)
			}
			issueIDs = append(issueIDs, fileIDs...)
		}

		if len(issueIDs) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no issue IDs provided\n")
			cmd.Usage()
			os.Exit(1)
		}

		// Remove duplicates
		issueIDs = uniqueStrings(issueIDs)

		// Handle batch deletion
		if len(issueIDs) > 1 {
			deleteBatch(cmd, issueIDs, force, dryRun, cascade)
			return
		}

		// Single issue deletion (legacy behavior)
		issueID := issueIDs[0]

		// Ensure we have a direct store when daemon lacks delete support
		if daemonClient != nil {
			if err := ensureDirectMode("daemon does not support delete command"); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else if store == nil {
			if err := ensureStoreActive(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		ctx := context.Background()

		// Get the issue to be deleted
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Error: issue %s not found\n", issueID)
			os.Exit(1)
		}

		// Find all connected issues (dependencies in both directions)
		connectedIssues := make(map[string]*types.Issue)

		// Get dependencies (issues this one depends on)
		deps, err := store.GetDependencies(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting dependencies: %v\n", err)
			os.Exit(1)
		}
		for _, dep := range deps {
			connectedIssues[dep.ID] = dep
		}

		// Get dependents (issues that depend on this one)
		dependents, err := store.GetDependents(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting dependents: %v\n", err)
			os.Exit(1)
		}
		for _, dependent := range dependents {
			connectedIssues[dependent.ID] = dependent
		}

		// Get dependency records (outgoing) to count how many we'll remove
		depRecords, err := store.GetDependencyRecords(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting dependency records: %v\n", err)
			os.Exit(1)
		}

		// Build the regex pattern for matching issue IDs (handles hyphenated IDs properly)
		// Pattern: (^|non-word-char)(issueID)($|non-word-char) where word-char includes hyphen
		idPattern := `(^|[^A-Za-z0-9_-])(` + regexp.QuoteMeta(issueID) + `)($|[^A-Za-z0-9_-])`
		re := regexp.MustCompile(idPattern)
		replacementText := `$1[deleted:` + issueID + `]$3`

		// Preview mode
		if !force {
			red := color.New(color.FgRed).SprintFunc()
			yellow := color.New(color.FgYellow).SprintFunc()

			fmt.Printf("\n%s\n", red("⚠️  DELETE PREVIEW"))
			fmt.Printf("\nIssue to delete:\n")
			fmt.Printf("  %s: %s\n", issueID, issue.Title)

			totalDeps := len(depRecords) + len(dependents)
			if totalDeps > 0 {
				fmt.Printf("\nDependency links to remove: %d\n", totalDeps)
				for _, dep := range depRecords {
					fmt.Printf("  %s → %s (%s)\n", dep.IssueID, dep.DependsOnID, dep.Type)
				}
				for _, dep := range dependents {
					fmt.Printf("  %s → %s (inbound)\n", dep.ID, issueID)
				}
			}

			if len(connectedIssues) > 0 {
				fmt.Printf("\nConnected issues where text references will be updated:\n")
				issuesWithRefs := 0
				for id, connIssue := range connectedIssues {
					// Check if there are actually text references using the fixed regex
					hasRefs := re.MatchString(connIssue.Description) ||
						(connIssue.Notes != "" && re.MatchString(connIssue.Notes)) ||
						(connIssue.Design != "" && re.MatchString(connIssue.Design)) ||
						(connIssue.AcceptanceCriteria != "" && re.MatchString(connIssue.AcceptanceCriteria))

					if hasRefs {
						fmt.Printf("  %s: %s\n", id, connIssue.Title)
						issuesWithRefs++
					}
				}
				if issuesWithRefs == 0 {
					fmt.Printf("  (none have text references)\n")
				}
			}

			fmt.Printf("\n%s\n", yellow("This operation cannot be undone!"))
			fmt.Printf("To proceed, run: %s\n\n", yellow("bd delete "+issueID+" --force"))
			return
		}

		// Actually delete

		// 1. Update text references in connected issues (all text fields)
		updatedIssueCount := 0
		for id, connIssue := range connectedIssues {
			updates := make(map[string]interface{})

			// Replace in description
			if re.MatchString(connIssue.Description) {
				newDesc := re.ReplaceAllString(connIssue.Description, replacementText)
				updates["description"] = newDesc
			}

			// Replace in notes
			if connIssue.Notes != "" && re.MatchString(connIssue.Notes) {
				newNotes := re.ReplaceAllString(connIssue.Notes, replacementText)
				updates["notes"] = newNotes
			}

			// Replace in design
			if connIssue.Design != "" && re.MatchString(connIssue.Design) {
				newDesign := re.ReplaceAllString(connIssue.Design, replacementText)
				updates["design"] = newDesign
			}

			// Replace in acceptance_criteria
			if connIssue.AcceptanceCriteria != "" && re.MatchString(connIssue.AcceptanceCriteria) {
				newAC := re.ReplaceAllString(connIssue.AcceptanceCriteria, replacementText)
				updates["acceptance_criteria"] = newAC
			}

			if len(updates) > 0 {
				if err := store.UpdateIssue(ctx, id, updates, actor); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to update references in %s: %v\n", id, err)
				} else {
					updatedIssueCount++
				}
			}
		}

		// 2. Remove all dependency links (outgoing)
		outgoingRemoved := 0
		for _, dep := range depRecords {
			if err := store.RemoveDependency(ctx, dep.IssueID, dep.DependsOnID, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to remove dependency %s → %s: %v\n",
					dep.IssueID, dep.DependsOnID, err)
			} else {
				outgoingRemoved++
			}
		}

		// 3. Remove inbound dependency links (issues that depend on this one)
		inboundRemoved := 0
		for _, dep := range dependents {
			if err := store.RemoveDependency(ctx, dep.ID, issueID, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to remove dependency %s → %s: %v\n",
					dep.ID, issueID, err)
			} else {
				inboundRemoved++
			}
		}

		// 4. Delete the issue itself from database
		if err := deleteIssue(ctx, issueID); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting issue: %v\n", err)
			os.Exit(1)
		}

		// 5. Remove from JSONL (auto-flush can't see deletions)
		if err := removeIssueFromJSONL(issueID); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to remove from JSONL: %v\n", err)
		}

		// Schedule auto-flush to update neighbors
		markDirtyAndScheduleFlush()

		totalDepsRemoved := outgoingRemoved + inboundRemoved
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"deleted":              issueID,
				"dependencies_removed": totalDepsRemoved,
				"references_updated":   updatedIssueCount,
			})
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Deleted %s\n", green("✓"), issueID)
			fmt.Printf("  Removed %d dependency link(s)\n", totalDepsRemoved)
			fmt.Printf("  Updated text references in %d issue(s)\n", updatedIssueCount)
		}
	},
}

// deleteIssue removes an issue from the database
// Note: This is a direct database operation since Storage interface doesn't have Delete
func deleteIssue(ctx context.Context, issueID string) error {
	// We need to access the SQLite storage directly
	// Check if store is SQLite storage
	type deleter interface {
		DeleteIssue(ctx context.Context, id string) error
	}

	if d, ok := store.(deleter); ok {
		return d.DeleteIssue(ctx, issueID)
	}

	return fmt.Errorf("delete operation not supported by this storage backend")
}

// removeIssueFromJSONL removes a deleted issue from the JSONL file
// Auto-flush cannot see deletions because the dirty_issues row is deleted with the issue
func removeIssueFromJSONL(issueID string) error {
	path := findJSONLPath()
	if path == "" {
		return nil // No JSONL file yet
	}

	// Read all issues except the deleted one
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file, nothing to clean
		}
		return fmt.Errorf("failed to open JSONL: %w", err)
	}

	var issues []*types.Issue
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var iss types.Issue
		if err := json.Unmarshal([]byte(line), &iss); err != nil {
			// Skip malformed lines
			continue
		}
		if iss.ID != issueID {
			issues = append(issues, &iss)
		}
	}
	if err := scanner.Err(); err != nil {
		f.Close()
		return fmt.Errorf("failed to read JSONL: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close JSONL: %w", err)
	}

	// Write to temp file atomically
	temp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	out, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	enc := json.NewEncoder(out)
	for _, iss := range issues {
		if err := enc.Encode(iss); err != nil {
			out.Close()
			os.Remove(temp)
			return fmt.Errorf("failed to write issue: %w", err)
		}
	}

	if err := out.Close(); err != nil {
		os.Remove(temp)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// deleteBatch handles deletion of multiple issues
func deleteBatch(cmd *cobra.Command, issueIDs []string, force bool, dryRun bool, cascade bool) {
	// Ensure we have a direct store when daemon lacks delete support
	if daemonClient != nil {
		if err := ensureDirectMode("daemon does not support delete command"); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else if store == nil {
		if err := ensureStoreActive(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	ctx := context.Background()

	// Type assert to SQLite storage
	d, ok := store.(*sqlite.SQLiteStorage)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: batch delete not supported by this storage backend\n")
		os.Exit(1)
	}

	// Verify all issues exist
	issues := make(map[string]*types.Issue)
	notFound := []string{}
	for _, id := range issueIDs {
		issue, err := d.GetIssue(ctx, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting issue %s: %v\n", id, err)
			os.Exit(1)
		}
		if issue == nil {
			notFound = append(notFound, id)
		} else {
			issues[id] = issue
		}
	}

	if len(notFound) > 0 {
		fmt.Fprintf(os.Stderr, "Error: issues not found: %s\n", strings.Join(notFound, ", "))
		os.Exit(1)
	}

	// Dry-run or preview mode
	if dryRun || !force {
		result, err := d.DeleteIssues(ctx, issueIDs, cascade, false, true)
		if err != nil {
			// Try to show preview even if there are dependency issues
			showDeletionPreview(issueIDs, issues, cascade, err)
			os.Exit(1)
		}

		showDeletionPreview(issueIDs, issues, cascade, nil)
		fmt.Printf("\nWould delete: %d issues\n", result.DeletedCount)
		fmt.Printf("Would remove: %d dependencies, %d labels, %d events\n",
			result.DependenciesCount, result.LabelsCount, result.EventsCount)
		if len(result.OrphanedIssues) > 0 {
			fmt.Printf("Would orphan: %d issues\n", len(result.OrphanedIssues))
		}

		if dryRun {
			fmt.Printf("\n(Dry-run mode - no changes made)\n")
		} else {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("\n%s\n", yellow("This operation cannot be undone!"))
			if cascade {
				fmt.Printf("To proceed with cascade deletion, run: %s\n",
					yellow("bd delete "+strings.Join(issueIDs, " ")+" --cascade --force"))
			} else {
				fmt.Printf("To proceed, run: %s\n",
					yellow("bd delete "+strings.Join(issueIDs, " ")+" --force"))
			}
		}
		return
	}

	// Pre-collect connected issues before deletion (so we can update their text references)
	connectedIssues := make(map[string]*types.Issue)
	idSet := make(map[string]bool)
	for _, id := range issueIDs {
		idSet[id] = true
	}

	for _, id := range issueIDs {
		// Get dependencies (issues this one depends on)
		deps, err := store.GetDependencies(ctx, id)
		if err == nil {
			for _, dep := range deps {
				if !idSet[dep.ID] {
					connectedIssues[dep.ID] = dep
				}
			}
		}

		// Get dependents (issues that depend on this one)
		dependents, err := store.GetDependents(ctx, id)
		if err == nil {
			for _, dep := range dependents {
				if !idSet[dep.ID] {
					connectedIssues[dep.ID] = dep
				}
			}
		}
	}

	// Actually delete
	result, err := d.DeleteIssues(ctx, issueIDs, cascade, force, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Update text references in connected issues (using pre-collected issues)
	updatedCount := updateTextReferencesInIssues(ctx, issueIDs, connectedIssues)

	// Remove from JSONL
	for _, id := range issueIDs {
		if err := removeIssueFromJSONL(id); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to remove %s from JSONL: %v\n", id, err)
		}
	}

	// Schedule auto-flush
	markDirtyAndScheduleFlush()

	// Output results
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"deleted":              issueIDs,
			"deleted_count":        result.DeletedCount,
			"dependencies_removed": result.DependenciesCount,
			"labels_removed":       result.LabelsCount,
			"events_removed":       result.EventsCount,
			"references_updated":   updatedCount,
			"orphaned_issues":      result.OrphanedIssues,
		})
	} else {
		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Deleted %d issue(s)\n", green("✓"), result.DeletedCount)
		fmt.Printf("  Removed %d dependency link(s)\n", result.DependenciesCount)
		fmt.Printf("  Removed %d label(s)\n", result.LabelsCount)
		fmt.Printf("  Removed %d event(s)\n", result.EventsCount)
		fmt.Printf("  Updated text references in %d issue(s)\n", updatedCount)
		if len(result.OrphanedIssues) > 0 {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("  %s Orphaned %d issue(s): %s\n",
				yellow("⚠"), len(result.OrphanedIssues), strings.Join(result.OrphanedIssues, ", "))
		}
	}
}

// showDeletionPreview shows what would be deleted
func showDeletionPreview(issueIDs []string, issues map[string]*types.Issue, cascade bool, depError error) {
	red := color.New(color.FgRed).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Printf("\n%s\n", red("⚠️  DELETE PREVIEW"))
	fmt.Printf("\nIssues to delete (%d):\n", len(issueIDs))
	for _, id := range issueIDs {
		if issue := issues[id]; issue != nil {
			fmt.Printf("  %s: %s\n", id, issue.Title)
		}
	}

	if cascade {
		fmt.Printf("\n%s Cascade mode enabled - will also delete all dependent issues\n", yellow("⚠"))
	}

	if depError != nil {
		fmt.Printf("\n%s\n", red(depError.Error()))
	}
}

// updateTextReferencesInIssues updates text references to deleted issues in pre-collected connected issues
func updateTextReferencesInIssues(ctx context.Context, deletedIDs []string, connectedIssues map[string]*types.Issue) int {
	updatedCount := 0

	// For each deleted issue, update references in all connected issues
	for _, id := range deletedIDs {
		// Build regex pattern
		idPattern := `(^|[^A-Za-z0-9_-])(` + regexp.QuoteMeta(id) + `)($|[^A-Za-z0-9_-])`
		re := regexp.MustCompile(idPattern)
		replacementText := `$1[deleted:` + id + `]$3`

		for connID, connIssue := range connectedIssues {
			updates := make(map[string]interface{})

			if re.MatchString(connIssue.Description) {
				updates["description"] = re.ReplaceAllString(connIssue.Description, replacementText)
			}
			if connIssue.Notes != "" && re.MatchString(connIssue.Notes) {
				updates["notes"] = re.ReplaceAllString(connIssue.Notes, replacementText)
			}
			if connIssue.Design != "" && re.MatchString(connIssue.Design) {
				updates["design"] = re.ReplaceAllString(connIssue.Design, replacementText)
			}
			if connIssue.AcceptanceCriteria != "" && re.MatchString(connIssue.AcceptanceCriteria) {
				updates["acceptance_criteria"] = re.ReplaceAllString(connIssue.AcceptanceCriteria, replacementText)
			}

			if len(updates) > 0 {
				if err := store.UpdateIssue(ctx, connID, updates, actor); err == nil {
					updatedCount++
					// Update the in-memory issue to avoid double-replacing
					if desc, ok := updates["description"].(string); ok {
						connIssue.Description = desc
					}
					if notes, ok := updates["notes"].(string); ok {
						connIssue.Notes = notes
					}
					if design, ok := updates["design"].(string); ok {
						connIssue.Design = design
					}
					if ac, ok := updates["acceptance_criteria"].(string); ok {
						connIssue.AcceptanceCriteria = ac
					}
				}
			}
		}
	}

	return updatedCount
}

// readIssueIDsFromFile reads issue IDs from a file (one per line)
func readIssueIDsFromFile(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ids []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

// uniqueStrings removes duplicates from a slice of strings
func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func init() {
	deleteCmd.Flags().BoolP("force", "f", false, "Actually delete (without this flag, shows preview)")
	deleteCmd.Flags().String("from-file", "", "Read issue IDs from file (one per line)")
	deleteCmd.Flags().Bool("dry-run", false, "Preview what would be deleted without making changes")
	deleteCmd.Flags().Bool("cascade", false, "Recursively delete all dependent issues")
	rootCmd.AddCommand(deleteCmd)
}

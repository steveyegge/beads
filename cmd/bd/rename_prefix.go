package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

var renamePrefixCmd = &cobra.Command{
	Use:   "rename-prefix <new-prefix>",
	Short: "Rename the issue prefix for all issues",
	Long: `Rename the issue prefix for all issues in the database.
This will update all issue IDs and all text references across all fields.

Prefix validation rules:
- Max length: 8 characters
- Allowed characters: lowercase letters, numbers, hyphens
- Must start with a letter
- Must end with a hyphen (e.g., 'kw-', 'work-')
- Cannot be empty or just a hyphen

Example:
  bd rename-prefix kw-         # Rename from 'knowledge-work-' to 'kw-'`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		newPrefix := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		ctx := context.Background()

		if err := validatePrefix(newPrefix); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		oldPrefix, err := store.GetConfig(ctx, "issue_prefix")
		if err != nil || oldPrefix == "" {
			fmt.Fprintf(os.Stderr, "Error: failed to get current prefix: %v\n", err)
			os.Exit(1)
		}

		newPrefix = strings.TrimRight(newPrefix, "-")

		if oldPrefix == newPrefix {
			fmt.Fprintf(os.Stderr, "Error: new prefix is the same as current prefix: %s\n", oldPrefix)
			os.Exit(1)
		}

		issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to list issues: %v\n", err)
			os.Exit(1)
		}

		if len(issues) == 0 {
			fmt.Printf("No issues to rename. Updating prefix to %s\n", newPrefix)
			if !dryRun {
				if err := store.SetConfig(ctx, "issue_prefix", newPrefix); err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to update prefix: %v\n", err)
					os.Exit(1)
				}
			}
			return
		}

		if dryRun {
			cyan := color.New(color.FgCyan).SprintFunc()
			fmt.Printf("DRY RUN: Would rename %d issues from prefix '%s' to '%s'\n\n", len(issues), oldPrefix, newPrefix)
			fmt.Printf("Sample changes:\n")
			for i, issue := range issues {
				if i >= 5 {
					fmt.Printf("... and %d more issues\n", len(issues)-5)
					break
				}
				oldID := fmt.Sprintf("%s-%s", oldPrefix, strings.TrimPrefix(issue.ID, oldPrefix+"-"))
				newID := fmt.Sprintf("%s-%s", newPrefix, strings.TrimPrefix(issue.ID, oldPrefix+"-"))
				fmt.Printf("  %s -> %s\n", cyan(oldID), cyan(newID))
			}
			return
		}

		green := color.New(color.FgGreen).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()

		fmt.Printf("Renaming %d issues from prefix '%s' to '%s'...\n", len(issues), oldPrefix, newPrefix)

		if err := renamePrefixInDB(ctx, oldPrefix, newPrefix, issues); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to rename prefix: %v\n", err)
			os.Exit(1)
		}

		// Schedule full export (IDs changed, incremental won't work)
		markDirtyAndScheduleFullExport()

		fmt.Printf("%s Successfully renamed prefix from %s to %s\n", green("✓"), cyan(oldPrefix), cyan(newPrefix))

		if jsonOutput {
			result := map[string]interface{}{
				"old_prefix":   oldPrefix,
				"new_prefix":   newPrefix,
				"issues_count": len(issues),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(result)
		}
	},
}

func validatePrefix(prefix string) error {
	prefix = strings.TrimRight(prefix, "-")

	if prefix == "" {
		return fmt.Errorf("prefix cannot be empty")
	}

	if len(prefix) > 8 {
		return fmt.Errorf("prefix too long (max 8 characters): %s", prefix)
	}

	matched, _ := regexp.MatchString(`^[a-z][a-z0-9-]*$`, prefix)
	if !matched {
		return fmt.Errorf("prefix must start with a lowercase letter and contain only lowercase letters, numbers, and hyphens: %s", prefix)
	}

	if strings.HasPrefix(prefix, "-") || strings.HasSuffix(prefix, "--") {
		return fmt.Errorf("prefix has invalid hyphen placement: %s", prefix)
	}

	return nil
}

func renamePrefixInDB(ctx context.Context, oldPrefix, newPrefix string, issues []*types.Issue) error {
	// NOTE: Each issue is updated in its own transaction. A failure mid-way could leave
	// the database in a mixed state with some issues renamed and others not.
	// For production use, consider implementing a single atomic RenamePrefix() method
	// in the storage layer that wraps all updates in one transaction.

	oldPrefixPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldPrefix) + `-(\d+)\b`)

	replaceFunc := func(match string) string {
		return strings.Replace(match, oldPrefix+"-", newPrefix+"-", 1)
	}

	for _, issue := range issues {
		oldID := issue.ID
		numPart := strings.TrimPrefix(oldID, oldPrefix+"-")
		newID := fmt.Sprintf("%s-%s", newPrefix, numPart)

		issue.ID = newID

		issue.Title = oldPrefixPattern.ReplaceAllStringFunc(issue.Title, replaceFunc)
		issue.Description = oldPrefixPattern.ReplaceAllStringFunc(issue.Description, replaceFunc)
		if issue.Design != "" {
			issue.Design = oldPrefixPattern.ReplaceAllStringFunc(issue.Design, replaceFunc)
		}
		if issue.AcceptanceCriteria != "" {
			issue.AcceptanceCriteria = oldPrefixPattern.ReplaceAllStringFunc(issue.AcceptanceCriteria, replaceFunc)
		}
		if issue.Notes != "" {
			issue.Notes = oldPrefixPattern.ReplaceAllStringFunc(issue.Notes, replaceFunc)
		}

		if err := store.UpdateIssueID(ctx, oldID, newID, issue, actor); err != nil {
			return fmt.Errorf("failed to update issue %s: %w", oldID, err)
		}
	}

	if err := store.RenameDependencyPrefix(ctx, oldPrefix, newPrefix); err != nil {
		return fmt.Errorf("failed to update dependencies: %w", err)
	}

	if err := store.RenameCounterPrefix(ctx, oldPrefix, newPrefix); err != nil {
		return fmt.Errorf("failed to update counter: %w", err)
	}

	if err := store.SetConfig(ctx, "issue_prefix", newPrefix); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	return nil
}

func init() {
	renamePrefixCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	rootCmd.AddCommand(renamePrefixCmd)
}

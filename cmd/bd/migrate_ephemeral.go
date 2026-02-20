package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

// isUniqueConstraintError checks if an error is a SQLite UNIQUE constraint violation.
func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

var migrateEphemeralCmd = &cobra.Command{
	Use:   "ephemeral",
	Short: "Migrate ephemeral issues from Dolt to SQLite ephemeral store",
	Long: `Moves all ephemeral issues (wisps, molecules with ephemeral=1) from the Dolt
database to the SQLite ephemeral store.

This reduces Dolt history bloat by removing transient data that accumulates
from patrol cycles, heartbeats, and molecule workflows.

After migration:
- Ephemeral issues are in .beads/ephemeral.sqlite3
- Dolt no longer tracks these issues in its commit history
- New ephemeral issues are automatically routed to SQLite`,
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("migrate ephemeral")

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if store == nil {
			fmt.Fprintln(os.Stderr, "Error: store not initialized")
			os.Exit(1)
		}

		es := store.EphemeralStore()
		if es == nil {
			fmt.Fprintln(os.Stderr, "Error: ephemeral store not available")
			os.Exit(1)
		}

		ctx := context.Background()

		// Find all ephemeral issues in Dolt
		ephTrue := true
		filter := types.IssueFilter{
			Ephemeral: &ephTrue,
			Limit:     0, // unlimited
		}

		// Search Dolt directly (bypass ephemeral routing to find issues still in Dolt)
		issues, err := store.SearchIssuesDoltOnly(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error searching for ephemeral issues: %v\n", err)
			os.Exit(1)
		}

		if len(issues) == 0 {
			fmt.Println("No ephemeral issues found in Dolt. Nothing to migrate.")
			return
		}

		fmt.Printf("Found %d ephemeral issues in Dolt to migrate.\n", len(issues))

		if dryRun {
			fmt.Println("\nDry run - would migrate:")
			for i, issue := range issues {
				if i >= 20 {
					fmt.Printf("  ... and %d more\n", len(issues)-20)
					break
				}
				fmt.Printf("  %s: %s (type=%s, wisp_type=%s)\n",
					issue.ID, issue.Title, issue.IssueType, issue.WispType)
			}
			return
		}

		// Collect all dependency and label data BEFORE migration starts
		// (routing might send dep/label reads to ephemeral store after
		// issue is created there, so we read from Dolt first)
		type migrationData struct {
			issue  *types.Issue
			labels []string
			deps   []*types.Dependency
		}
		var toMigrate []migrationData
		for _, issue := range issues {
			md := migrationData{issue: issue}
			// Read labels from Dolt (no ephemeral routing on GetLabels)
			labels, err := store.GetLabels(ctx, issue.ID)
			if err == nil {
				md.labels = labels
			}
			// Read deps directly from Dolt DB to avoid ephemeral routing
			// (GetDependencyRecords routes by ID prefix)
			deps, err := store.GetDependencyRecords(ctx, issue.ID)
			if err == nil {
				md.deps = deps
			}
			toMigrate = append(toMigrate, md)
		}

		// Migrate in batches
		migrated := 0
		failed := 0
		start := time.Now()

		for _, md := range toMigrate {
			// Insert into ephemeral store (OR IGNORE for idempotency)
			if err := es.CreateIssue(ctx, md.issue, "migrate"); err != nil {
				// If already exists (idempotent retry), just skip
				if !isUniqueConstraintError(err) {
					fmt.Fprintf(os.Stderr, "  Warning: failed to migrate %s: %v\n", md.issue.ID, err)
					failed++
					continue
				}
			}

			// Copy labels
			for _, label := range md.labels {
				if err := es.AddLabel(ctx, md.issue.ID, label, "migrate"); err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: failed to copy label %q for %s: %v\n", label, md.issue.ID, err)
				}
			}

			// Copy dependencies
			for _, dep := range md.deps {
				if err := es.AddDependency(ctx, dep, "migrate"); err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: failed to copy dependency for %s: %v\n", md.issue.ID, err)
				}
			}

			// Delete from Dolt
			if err := store.DeleteIssue(ctx, md.issue.ID); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to delete %s from Dolt: %v\n", md.issue.ID, err)
			}

			migrated++
		}

		elapsed := time.Since(start)
		fmt.Printf("\nMigration complete in %v:\n", elapsed.Round(time.Millisecond))
		fmt.Printf("  Migrated: %d\n", migrated)
		if failed > 0 {
			fmt.Printf("  Failed:   %d\n", failed)
		}
		fmt.Printf("  Total:    %d\n", len(issues))

		commandDidWrite.Store(true)
	},
}

func init() {
	migrateEphemeralCmd.Flags().Bool("dry-run", false, "Show what would be migrated without making changes")
	migrateCmd.AddCommand(migrateEphemeralCmd)
}

package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var dirtyCmd = &cobra.Command{
	Use:     "dirty",
	GroupID: "advanced",
	Short:   "Manage the dirty_issues tracking table",
	Long: `Manage the dirty_issues table used for incremental JSONL export.

Issues are marked dirty when created, updated, or when dependencies change.
Dirty issues are cleared after successful export to JSONL. Over time, stale
entries can accumulate (orphaned references, already-exported issues) and
inflate query scan times.

Subcommands:
  count   Show the number of dirty issues
  flush   Remove stale entries from the dirty_issues table`,
}

var dirtyCountCmd = &cobra.Command{
	Use:   "count",
	Short: "Show the number of dirty issues",
	Run: func(cmd *cobra.Command, args []string) {
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: dirty count requires database access; ensure daemon is running\n")
			os.Exit(1)
		}

		s := getStore()
		db := s.UnderlyingDB()
		if db == nil {
			fmt.Fprintf(os.Stderr, "Error: no underlying database available\n")
			os.Exit(1)
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM dirty_issues`).Scan(&count); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]int{"dirty_count": count})
		} else {
			fmt.Printf("Dirty issues: %d\n", count)
		}
	},
}

var dirtyFlushCmd = &cobra.Command{
	Use:   "flush",
	Short: "Remove stale entries from the dirty_issues table",
	Long: `Remove stale dirty_issues entries that inflate query scan times.

This cleans up two categories of stale entries:
1. Orphaned entries — issue no longer exists in the issues table
2. Already-exported entries — content hash matches the last export

The daemon runs this automatically every 5 minutes. Use this command
for manual cleanup or when the daemon is not running.`,
	Run: func(cmd *cobra.Command, args []string) {
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: dirty flush requires database access; ensure daemon is running\n")
			os.Exit(1)
		}

		ctx := getRootContext()
		s := getStore()
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		db := s.UnderlyingDB()
		if db == nil {
			fmt.Fprintf(os.Stderr, "Error: no underlying database available\n")
			os.Exit(1)
		}

		backend := s.BackendName()

		// Get count before flush
		var beforeCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM dirty_issues`).Scan(&beforeCount); err != nil {
			fmt.Fprintf(os.Stderr, "Error getting dirty count: %v\n", err)
			os.Exit(1)
		}

		if dryRun {
			// Preview: count orphans and already-exported
			orphanCount := countOrphans(db)
			exportedCount := countAlreadyExported(db)

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"dirty_count":    beforeCount,
					"orphaned":       orphanCount,
					"already_exported": exportedCount,
					"would_remove":   orphanCount + exportedCount,
					"dry_run":        true,
				})
			} else {
				fmt.Printf("Dirty issues: %d\n", beforeCount)
				fmt.Printf("  Orphaned (issue deleted):  %d\n", orphanCount)
				fmt.Printf("  Already exported:          %d\n", exportedCount)
				fmt.Printf("  Would remove:              %d\n", orphanCount+exportedCount)
				fmt.Println("  (dry run — no changes made)")
			}
			return
		}

		// Step 1: Remove orphaned dirty entries
		orphanQuery := `DELETE FROM dirty_issues WHERE issue_id NOT IN (SELECT id FROM issues)`
		result, err := db.ExecContext(ctx, orphanQuery)
		var orphanRemoved int64
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove orphaned entries: %v\n", err)
		} else {
			orphanRemoved, _ = result.RowsAffected()
		}

		// Step 2: Remove dirty entries for issues already exported with current content
		var exportFlushQuery string
		if backend == "sqlite" {
			exportFlushQuery = `
				DELETE FROM dirty_issues WHERE issue_id IN (
					SELECT d.issue_id FROM dirty_issues d
					JOIN issues i ON d.issue_id = i.id
					JOIN export_hashes e ON e.issue_id = i.id
					WHERE i.content_hash = e.content_hash
				)
			`
		} else {
			// MySQL/Dolt
			exportFlushQuery = `
				DELETE d FROM dirty_issues d
				JOIN issues i ON d.issue_id = i.id
				JOIN export_hashes e ON e.issue_id = i.id
				WHERE i.content_hash = e.content_hash
			`
		}
		result, err = db.ExecContext(ctx, exportFlushQuery)
		var exportRemoved int64
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove already-exported entries: %v\n", err)
		} else {
			exportRemoved, _ = result.RowsAffected()
		}

		totalRemoved := orphanRemoved + exportRemoved

		// Get count after flush
		var afterCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM dirty_issues`).Scan(&afterCount); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get post-flush count: %v\n", err)
			afterCount = beforeCount - int(totalRemoved)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"before":           beforeCount,
				"after":            afterCount,
				"orphaned_removed": orphanRemoved,
				"exported_removed": exportRemoved,
				"total_removed":    totalRemoved,
			})
		} else {
			fmt.Printf("Dirty issues before: %d\n", beforeCount)
			fmt.Printf("  Orphaned removed:  %d\n", orphanRemoved)
			fmt.Printf("  Exported removed:  %d\n", exportRemoved)
			fmt.Printf("  Total removed:     %d\n", totalRemoved)
			fmt.Printf("Dirty issues after:  %d\n", afterCount)
		}
	},
}

// countOrphans counts dirty_issues entries where the issue no longer exists.
func countOrphans(db *sql.DB) int {
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM dirty_issues WHERE issue_id NOT IN (SELECT id FROM issues)`).Scan(&count)
	return count
}

// countAlreadyExported counts dirty_issues entries where the export hash matches.
func countAlreadyExported(db *sql.DB) int {
	var count int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM dirty_issues d
		JOIN issues i ON d.issue_id = i.id
		JOIN export_hashes e ON e.issue_id = i.id
		WHERE i.content_hash = e.content_hash
	`).Scan(&count)
	return count
}

func init() {
	dirtyFlushCmd.Flags().Bool("dry-run", false, "Preview what would be removed without making changes")

	dirtyCmd.AddCommand(dirtyCountCmd)
	dirtyCmd.AddCommand(dirtyFlushCmd)
	rootCmd.AddCommand(dirtyCmd)
}

package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

var repairCmd = &cobra.Command{
	Use:     "repair",
	GroupID: GroupMaintenance,
	Short:   "Repair corrupted database by cleaning orphaned references",
	// Note: This command is in noDbCommands list (main.go) to skip normal db init.
	// We open SQLite directly, bypassing migration invariant checks.
	Long: `Repair a database that won't open due to orphaned foreign key references.

When the database has orphaned dependencies or labels, the migration invariant
check fails and prevents the database from opening. This creates a chicken-and-egg
problem where 'bd doctor --fix' can't run because it can't open the database.

This command opens SQLite directly (bypassing invariant checks) and cleans:
  - Orphaned dependencies (issue_id not in issues)
  - Orphaned dependencies (depends_on_id not in issues, excluding external refs)
  - Orphaned labels (issue_id not in issues)

After repair, normal bd commands should work again.

Examples:
  bd repair              # Repair database in current directory
  bd repair --dry-run    # Show what would be cleaned without making changes
  bd repair --path /other/repo  # Repair database in another location`,
	Run: runRepair,
}

var (
	repairDryRun bool
	repairPath   string
)

func init() {
	repairCmd.Flags().BoolVar(&repairDryRun, "dry-run", false, "Show what would be cleaned without making changes")
	repairCmd.Flags().StringVar(&repairPath, "path", ".", "Path to repository with .beads directory")
	rootCmd.AddCommand(repairCmd)
}

func runRepair(cmd *cobra.Command, args []string) {
	// Find .beads directory
	beadsDir := filepath.Join(repairPath, ".beads")
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: .beads directory not found at %s\n", beadsDir)
		os.Exit(1)
	}

	// Find database file
	dbPath := filepath.Join(beadsDir, "beads.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: database not found at %s\n", dbPath)
		os.Exit(1)
	}

	fmt.Printf("Repairing database: %s\n", dbPath)
	if repairDryRun {
		fmt.Println("[DRY-RUN] No changes will be made")
	}
	fmt.Println()

	// Open database directly, bypassing beads storage layer
	db, err := openRepairDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Collect repair statistics
	stats := repairStats{}

	// 1. Find and clean orphaned dependencies (issue_id not in issues)
	orphanedIssueID, err := findOrphanedDepsIssueID(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking orphaned deps (issue_id): %v\n", err)
		os.Exit(1)
	}
	stats.orphanedDepsIssueID = len(orphanedIssueID)

	// 2. Find and clean orphaned dependencies (depends_on_id not in issues)
	orphanedDependsOn, err := findOrphanedDepsDependsOn(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking orphaned deps (depends_on_id): %v\n", err)
		os.Exit(1)
	}
	stats.orphanedDepsDependsOn = len(orphanedDependsOn)

	// 3. Find and clean orphaned labels
	orphanedLabels, err := findOrphanedLabels(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking orphaned labels: %v\n", err)
		os.Exit(1)
	}
	stats.orphanedLabels = len(orphanedLabels)

	// Print findings
	if stats.total() == 0 {
		fmt.Printf("%s No orphaned references found - database is clean\n", ui.RenderPass("✓"))
		return
	}

	fmt.Printf("Found %d orphaned reference(s):\n", stats.total())
	if stats.orphanedDepsIssueID > 0 {
		fmt.Printf("  • %d dependencies with missing issue_id\n", stats.orphanedDepsIssueID)
		for _, dep := range orphanedIssueID {
			fmt.Printf("    - %s → %s\n", dep.issueID, dep.dependsOnID)
		}
	}
	if stats.orphanedDepsDependsOn > 0 {
		fmt.Printf("  • %d dependencies with missing depends_on_id\n", stats.orphanedDepsDependsOn)
		for _, dep := range orphanedDependsOn {
			fmt.Printf("    - %s → %s\n", dep.issueID, dep.dependsOnID)
		}
	}
	if stats.orphanedLabels > 0 {
		fmt.Printf("  • %d labels with missing issue_id\n", stats.orphanedLabels)
		for _, label := range orphanedLabels {
			fmt.Printf("    - %s: %s\n", label.issueID, label.label)
		}
	}
	fmt.Println()

	if repairDryRun {
		fmt.Printf("[DRY-RUN] Would delete %d orphaned reference(s)\n", stats.total())
		return
	}

	// Create backup before destructive operations
	backupPath := dbPath + ".pre-repair"
	fmt.Printf("Creating backup: %s\n", filepath.Base(backupPath))
	if err := copyFile(dbPath, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating backup: %v\n", err)
		fmt.Fprintf(os.Stderr, "Aborting repair. Fix backup issue and retry.\n")
		os.Exit(1)
	}
	fmt.Printf("  %s Backup created\n\n", ui.RenderPass("✓"))

	// Apply repairs in a transaction
	fmt.Println("Cleaning orphaned references...")

	tx, err := db.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting transaction: %v\n", err)
		os.Exit(1)
	}

	var repairErr error

	// Delete orphaned deps (issue_id) and mark affected issues dirty
	if len(orphanedIssueID) > 0 && repairErr == nil {
		// Note: orphanedIssueID contains deps where issue_id doesn't exist,
		// so we can't mark them dirty (the issue is gone). But for depends_on orphans,
		// the issue_id still exists and should be marked dirty.
		result, err := tx.Exec(`
			DELETE FROM dependencies
			WHERE NOT EXISTS (SELECT 1 FROM issues WHERE id = dependencies.issue_id)
		`)
		if err != nil {
			repairErr = fmt.Errorf("deleting orphaned deps (issue_id): %w", err)
		} else {
			deleted, _ := result.RowsAffected()
			fmt.Printf("  %s Deleted %d dependencies with missing issue_id\n", ui.RenderPass("✓"), deleted)
		}
	}

	// Delete orphaned deps (depends_on_id) and mark parent issues dirty
	if len(orphanedDependsOn) > 0 && repairErr == nil {
		// Mark parent issues as dirty for export
		for _, dep := range orphanedDependsOn {
			_, _ = tx.Exec("INSERT OR IGNORE INTO dirty_issues (issue_id) VALUES (?)", dep.issueID)
		}

		result, err := tx.Exec(`
			DELETE FROM dependencies
			WHERE NOT EXISTS (SELECT 1 FROM issues WHERE id = dependencies.depends_on_id)
			  AND dependencies.depends_on_id NOT LIKE 'external:%'
		`)
		if err != nil {
			repairErr = fmt.Errorf("deleting orphaned deps (depends_on_id): %w", err)
		} else {
			deleted, _ := result.RowsAffected()
			fmt.Printf("  %s Deleted %d dependencies with missing depends_on_id\n", ui.RenderPass("✓"), deleted)
		}
	}

	// Delete orphaned labels
	if len(orphanedLabels) > 0 && repairErr == nil {
		// Labels reference non-existent issues, so no dirty marking needed
		result, err := tx.Exec(`
			DELETE FROM labels
			WHERE NOT EXISTS (SELECT 1 FROM issues WHERE id = labels.issue_id)
		`)
		if err != nil {
			repairErr = fmt.Errorf("deleting orphaned labels: %w", err)
		} else {
			deleted, _ := result.RowsAffected()
			fmt.Printf("  %s Deleted %d labels with missing issue_id\n", ui.RenderPass("✓"), deleted)
		}
	}

	// Commit or rollback
	if repairErr != nil {
		_ = tx.Rollback()
		fmt.Fprintf(os.Stderr, "\n%s Error: %v\n", ui.RenderFail("✗"), repairErr)
		fmt.Fprintf(os.Stderr, "Transaction rolled back. Database unchanged.\n")
		fmt.Fprintf(os.Stderr, "Backup available at: %s\n", backupPath)
		os.Exit(1)
	}

	if err := tx.Commit(); err != nil {
		fmt.Fprintf(os.Stderr, "\n%s Error committing transaction: %v\n", ui.RenderFail("✗"), err)
		fmt.Fprintf(os.Stderr, "Backup available at: %s\n", backupPath)
		os.Exit(1)
	}

	// Run WAL checkpoint to persist changes
	fmt.Print("  Running WAL checkpoint... ")
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		fmt.Printf("%s %v\n", ui.RenderFail("✗"), err)
	} else {
		fmt.Printf("%s\n", ui.RenderPass("✓"))
	}

	fmt.Println()
	fmt.Printf("%s Repair complete. Try running 'bd doctor' to verify.\n", ui.RenderPass("✓"))
	fmt.Printf("Backup preserved at: %s\n", filepath.Base(backupPath))
}

// repairStats tracks what was found/cleaned
type repairStats struct {
	orphanedDepsIssueID   int
	orphanedDepsDependsOn int
	orphanedLabels        int
}

func (s repairStats) total() int {
	return s.orphanedDepsIssueID + s.orphanedDepsDependsOn + s.orphanedLabels
}

// orphanedDep represents an orphaned dependency
type orphanedDep struct {
	issueID     string
	dependsOnID string
}

// orphanedLabel represents an orphaned label
type orphanedLabel struct {
	issueID string
	label   string
}

// openRepairDB opens SQLite directly for repair, bypassing all beads layer code
func openRepairDB(dbPath string) (*sql.DB, error) {
	// Build connection string with pragmas
	busyMs := int64(30 * time.Second / time.Millisecond)
	if v := strings.TrimSpace(os.Getenv("BD_LOCK_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			busyMs = int64(d / time.Millisecond)
		}
	}

	connStr := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=foreign_keys(OFF)&_time_format=sqlite",
		dbPath, busyMs)

	return sql.Open("sqlite3", connStr)
}

// findOrphanedDepsIssueID finds dependencies where issue_id doesn't exist
func findOrphanedDepsIssueID(db *sql.DB) ([]orphanedDep, error) {
	rows, err := db.Query(`
		SELECT d.issue_id, d.depends_on_id
		FROM dependencies d
		WHERE NOT EXISTS (SELECT 1 FROM issues WHERE id = d.issue_id)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orphans []orphanedDep
	for rows.Next() {
		var dep orphanedDep
		if err := rows.Scan(&dep.issueID, &dep.dependsOnID); err != nil {
			return nil, err
		}
		orphans = append(orphans, dep)
	}
	return orphans, rows.Err()
}

// findOrphanedDepsDependsOn finds dependencies where depends_on_id doesn't exist
func findOrphanedDepsDependsOn(db *sql.DB) ([]orphanedDep, error) {
	rows, err := db.Query(`
		SELECT d.issue_id, d.depends_on_id
		FROM dependencies d
		WHERE NOT EXISTS (SELECT 1 FROM issues WHERE id = d.depends_on_id)
		  AND d.depends_on_id NOT LIKE 'external:%'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orphans []orphanedDep
	for rows.Next() {
		var dep orphanedDep
		if err := rows.Scan(&dep.issueID, &dep.dependsOnID); err != nil {
			return nil, err
		}
		orphans = append(orphans, dep)
	}
	return orphans, rows.Err()
}

// findOrphanedLabels finds labels where issue_id doesn't exist
func findOrphanedLabels(db *sql.DB) ([]orphanedLabel, error) {
	rows, err := db.Query(`
		SELECT l.issue_id, l.label
		FROM labels l
		WHERE NOT EXISTS (SELECT 1 FROM issues WHERE id = l.issue_id)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []orphanedLabel
	for rows.Next() {
		var label orphanedLabel
		if err := rows.Scan(&label.issueID, &label.label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

